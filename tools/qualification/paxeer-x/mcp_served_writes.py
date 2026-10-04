#!/usr/bin/env python3
import copy
from concurrent.futures import ThreadPoolExecutor
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import select
import signal
import socket
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
COMMAND = 'timeout 30m python3 tools/qualification/paxeer-x/mcp_served_writes.py'
FAMILIES = ('wallet.send', 'token.create', 'token.mint', 'token.transfer', 'grant.issue', 'grant.draw')
TRANSPORTS = ('socket', 'stdio')
LEVELS = ('StateProven', 'CheckpointFinalised', 'SettlementAnchored')


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


OWNERS = module('served_write_owner_helpers', Path(__file__).with_name('mcp_daemon_revocation.py'))
HTTP = OWNERS.HTTP
require = OWNERS.require
private_write = OWNERS.private_write


class Client:
    def __init__(self, fixture, row, credential, directory, label, transport):
        self.fixture, self.credential, self.directory = fixture, credential, directory
        self.label, self.transport = label, transport
        self.sequence = self.starts = 0
        self.process = self.connection = self.input = self.output = self.log = None
        binding = OWNERS.substitute(json.loads(Path(fixture.values[row['binding_seed']]).read_text()), fixture.values)
        require(binding.get('tenant') == credential['tenant']
                and binding.get('session_id') == credential['session_id']
                and str(binding.get('session_generation')) == credential['generation']
                and re.fullmatch('[0-9a-f]{64}', binding.get('capability_id', '')),
                'MCP binding must retain the genuine full daemon credential and capability')
        paths = [binding.get('store'), binding.get('audit_root'), binding.get('session_token_file'),
                 binding.get('agent', {}).get('bearer_file')]
        if transport == 'socket':
            require(isinstance(binding.get('listener'), dict), 'socket transport requires a genuine listener binding')
            paths.append(binding['listener'].get('socket'))
        else:
            require('listener' not in binding, 'stdio binding must use the actual absent-listener binary path')
        for raw in paths:
            require(isinstance(raw, str) and Path(raw).is_absolute() and Path(raw).resolve() == Path(raw)
                    and fixture.directory in Path(raw).parents, 'binding paths must stay in disposable private state')
        require(Path(binding['session_token_file']).read_text().strip() == credential['token_id'],
                'MCP token differs from real daemon credential')
        self.binding = binding
        self.path = directory / (label + '-binding.json')
        private_write(self.path, binding)

    def start(self):
        self.starts += 1
        self.log = (self.directory / (self.label + '-' + str(self.starts) + '.log')).open('xb')
        environment = {key: value for key, value in os.environ.items()
                       if not key.startswith(('LAYERX_', 'PAXEER_X_'))}
        self.process = subprocess.Popen([self.fixture.artifacts['layerx-mcp']['path'], str(self.path)],
            cwd=self.fixture.directory, env=environment,
            stdin=subprocess.PIPE if self.transport == 'stdio' else subprocess.DEVNULL,
            stdout=subprocess.PIPE if self.transport == 'stdio' else self.log, stderr=self.log)
        if self.transport == 'stdio':
            self.input, self.output = self.process.stdin, self.process.stdout
        else:
            deadline = time.monotonic() + 30
            while True:
                require(self.process.poll() is None and time.monotonic() < deadline,
                        'actual candidate MCP did not serve its provisioned authority')
                try:
                    self.connection = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                    self.connection.settimeout(30)
                    self.connection.connect(self.binding['listener']['socket'])
                    self.input = self.output = self.connection.makefile('rwb')
                    break
                except OSError:
                    self.connection.close()
                    self.connection = None
                    time.sleep(0.05)
        self.message('initialize', {})

    def message(self, method, params):
        self.sequence += 1
        request = {'jsonrpc': '2.0', 'id': self.sequence, 'method': method, 'params': params}
        self.input.write(json.dumps(request, separators=(',', ':')).encode() + b'\n')
        self.input.flush()
        if self.transport == 'stdio':
            require(bool(select.select([self.output], [], [], 30)[0]), 'actual stdio response deadline')
        raw = self.output.readline(1_048_577)
        require(raw.endswith(b'\n') and len(raw) <= 1_048_576, 'bounded real MCP JSON line required')
        response = json.loads(raw)
        require(response.get('jsonrpc') == '2.0' and response.get('id') == self.sequence,
                'real MCP response lost invocation identity')
        private_write(self.directory / (self.label + '-response-' + str(self.starts) + '-' + str(self.sequence) + '.json'), response)
        require('error' not in response, 'real MCP operation unavailable: ' + method)
        return response['result']

    def call(self, call):
        require(isinstance(call, dict) and set(call) == {'tool', 'arguments', 'idempotency_key'}
                and re.fullmatch('[0-9a-f]{64}', call['idempotency_key']), 'closed actual MCP invocation required')
        result = self.message('tools/call', {'name': call['tool'], 'arguments': call['arguments'],
            '_meta': {'layerx/idempotency_key': call['idempotency_key']}})
        require(isinstance(result, dict) and type(result.get('isError')) is bool
                and len(result.get('content', [])) == 1 and result['content'][0].get('type') == 'text',
                'actual MCP canonical result framing required')
        value = json.loads(result['content'][0]['text'])
        require(result.get('structuredContent') == value, 'served structured result differs from canonical content')
        return result['isError'], value

    def stop(self):
        for stream in {self.input, self.output} - {None}:
            stream.close()
        self.input = self.output = None
        if self.connection is not None:
            self.connection.close()
            self.connection = None
        if self.process is not None and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
        if self.log is not None:
            self.log.close()
            self.log = None


class Corpus(OWNERS.Phase):
    def __init__(self, evidence):
        self.directory = evidence
        self.clients, self.credentials, self.prepared, self.aliases = {}, {}, {}, {}
        self.daemon = None
        self.fixture = HTTP.AgentdFixture(evidence / 'fixture')
        config = HTTP.daemon_config(self.fixture)
        self.profile, credentials, self.signers = self.fixture.provision_mcp_served_writes()
        for transport in TRANSPORTS:
            for role, row in self.profile['participants'][transport].items():
                key = transport + '-' + role
                self.credentials[key] = credentials[transport][role]
                self.clients[key] = Client(self.fixture, row, credentials[transport][role], evidence, key, transport)
        config['LAYERX_AGENTD_MCP_BINDINGS'] = ','.join(str(client.path) for client in self.clients.values())
        require('agent_program_port' in self.fixture.values, 'real disposable daemon listener port required')
        self.daemon = HTTP.Daemon(Path(self.fixture.artifacts['layerx-agentd']['path']), config,
            evidence, int(self.fixture.values['agent_program_port']), self.fixture)
        self.serial = 700_000

    def close(self):
        for client in self.clients.values():
            client.stop()
        if self.daemon is not None:
            self.daemon.stop()
        if hasattr(self, 'fixture'):
            self.fixture.cleanup()

    def start(self):
        self.daemon.start()
        for client in self.clients.values():
            client.start()

    def success(self, client, call):
        refused, value = client.call(call)
        require(not refused and value.get('tool') == call['tool'] and 'result' in value,
                'actual served write/stage unavailable: ' + call['tool'])
        return value['result']

    def refusal(self, client, call, reason):
        refused, value = client.call(call)
        detail = value.get('refusal', {})
        require(refused and detail.get('reason') == reason
                and detail.get('state') in ('refused', 'unknown') and 'result' not in value,
                'actual served boundary did not return exact typed refusal')
        return {'reason': detail['reason'], 'class': detail.get('class'), 'state': detail['state']}

    def prepare(self, transport, row):
        envelope = self.fixture.envelope(row['prepare_request'])
        request = envelope['request']
        require(envelope.get('operation') == 'prepare'
                and envelope['credential'] == self.credentials[transport + '-full']
                and request.get('purpose', {}).get('purpose', {}).get('tenant') == envelope['credential']['tenant']
                and request['purpose']['purpose'].get('session_id') == envelope['credential']['session_id']
                and request['purpose']['purpose'].get('generation') == envelope['credential']['generation'],
                'actual immutable preparation must retain full authenticated native owner context')
        key = request['idempotency_key']
        call = {'tool': 'activity.prepare', 'arguments': request, 'idempotency_key': key}
        result = self.success(self.clients[transport + '-full'], call)
        require(set(result) == {'version', 'preparation_id', 'canonical_bytes', 'signing_preimage',
                              'activity', 'approval_required', 'approval_id'}
                and result['version'] == '1' and result['activity'] == request['activity']
                and result['preparation_id'] == request['purpose']['purpose']['preparation_id']
                and result['approval_required'] is False and result['approval_id'] is None
                and re.fullmatch('[0-9a-f]{64}', result['preparation_id'])
                and re.fullmatch('[0-9a-f]{64}', result['signing_preimage'])
                and re.fullmatch('[0-9a-f]+', result['canonical_bytes']),
                'real complete immutable non-held native preparation required')
        return request, result

    def alias(self, transport, family, row=None):
        row = self.profile['families'][transport][family] if row is None else row
        request, prepared = self.prepare(transport, row)
        signer = self.signers[transport][family]
        if row is not self.profile['families'][transport][family]:
            from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
            from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
            seed = self.fixture.values.get(row['signer_seed'])
            require(seed is not None and Path(seed).stat().st_size == 32, 'genuine interruption owner seed required')
            signer = Ed25519PrivateKey.from_private_bytes(Path(seed).read_bytes())
            require(signer.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
                    == row['signer_public_key'], 'interruption signer public identity mismatch')
        signature = signer.sign(bytes.fromhex(prepared['signing_preimage'])).hex()
        call = {'tool': family, 'idempotency_key': request['idempotency_key'], 'arguments': {
            'variant': 'native_write_v1', 'intent': copy.deepcopy(row['intent']),
            'preparation': request, 'signature': signature, 'signer_public_key': row['signer_public_key']}}
        self.aliases[(transport, family)] = call
        self.prepared[(transport, family)] = prepared
        return call

    def verified(self, result):
        require(isinstance(result, dict) and isinstance(result.get('submission'), dict)
                and result['submission'].get('state') == 'Executed'
                and result['submission'].get('verification_level') in LEVELS
                and isinstance(result.get('receipt'), dict)
                and result['receipt'].get('verification_level') in LEVELS,
                'monetary success requires actual StateProven executed receipt')
        facts = OWNERS.READINESS.receipt_facts(result['receipt'].get('canonical_bytes'), result.get('activity_id'))
        return {'activity_id': facts['activity_id'], 'receipt_sha256': facts['receipt_sha256'],
                'global_sequence': facts['global_sequence'], 'submission_ref': result['submission']['submission_ref']}

    def await_executed(self, transport, key, observed):
        require(isinstance(observed, dict) and isinstance(observed.get('submission'), dict),
                'actual tracked submission required before waiting')
        reference = observed['submission']['submission_ref']
        activity = observed['activity_id']
        deadline = time.monotonic() + 120
        while observed['submission'].get('state') != 'Executed':
            require(observed['submission'].get('state') in ('Prepared', 'Signed', 'Queued',
                        'Submitted', 'Acknowledged', 'Unknown') and observed.get('receipt') is None,
                    'pending/unknown was promoted to verified success or carries fake receipt')
            require(time.monotonic() < deadline, 'real monetary operation did not execute within bounded wait')
            observed = self.success(self.clients[transport + '-full'], {
                'tool': 'activity.wait', 'idempotency_key': key,
                'arguments': {'submission_ref': reference, 'timeout_ms': 30000}})
            require(observed['activity_id'] == activity
                    and observed['submission']['submission_ref'] == reference,
                    'real Wait changed original monetary identity')
        self.verified(observed)
        return observed

    def family(self, transport, family):
        call = self.alias(transport, family)
        result = self.success(self.clients[transport + '-full'], call)
        executed = self.await_executed(transport, call['idempotency_key'], result)
        receipt = self.verified(executed)
        durable, path = self.snapshot_evidence(transport + '-full', call,
            transport + '-' + family.replace('.', '-') + '-completed')
        require(durable['native_preparation'] is not None
                and bytes(durable['native_preparation']['preparation_id']).hex()
                    == self.prepared[(transport, family)]['preparation_id'],
                'economic alias lacks genuine durable native preparation association')
        again = self.success(self.clients[transport + '-full'], call)
        again = self.await_executed(transport, call['idempotency_key'], again)
        require(self.verified(again) == receipt, 'same-key same-bytes monetary invocation did not deduplicate')
        changed = copy.deepcopy(call)
        field = 'supply_cap' if family == 'token.create' else 'amount'
        changed['arguments']['intent'][field] = str(int(changed['arguments']['intent'][field]) + 1)
        self.refusal(self.clients[transport + '-full'], changed, 'mcp.invocation_conflict')
        after, after_path = self.snapshot_evidence(transport + '-full', call,
            transport + '-' + family.replace('.', '-') + '-deduplicated')
        require(after['request_digest'] == durable['request_digest']
                and after['native_preparation'] == durable['native_preparation']
                and sum(e['kind'] == 'effect_start' for e in after['events']) == 1,
                'repeat-key write changed canonical bytes or repeated the effect')
        return dict(receipt, invocation_evidence=str(path), dedup_evidence=str(after_path))

    def activity(self, transport):
        row = self.profile['activity'][transport]
        request, prepared = self.prepare(transport, row)
        client = self.clients[transport + '-full']
        key = request['idempotency_key']
        disclosure = self.success(client, {'tool': 'activity.disclose', 'idempotency_key': key,
            'arguments': {'canonical_bytes': prepared['canonical_bytes']}})
        require(set(disclosure) == {'version', 'preparation_id', 'canonical_bytes', 'disclosure_digest',
                    'activity_type', 'actor', 'authority', 'asset', 'fee_limit', 'not_before',
                    'not_after', 'payload_expires_at', 'idempotency_key'}
                and disclosure['version'] == '1'
                and disclosure['preparation_id'] == prepared['preparation_id']
                and disclosure['canonical_bytes'] == prepared['canonical_bytes']
                and disclosure['idempotency_key'] == key
                and disclosure['fee_limit'] == request['fee_limit']
                and re.fullmatch('[0-9a-f]{64}', disclosure['disclosure_digest']),
                'actual owned canonical disclosure does not match immutable preparation')
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
        seed = self.fixture.values.get(row['signer_seed'])
        require(seed is not None and Path(seed).stat().st_size == 32, 'actual provisioned activity signer required')
        signer = Ed25519PrivateKey.from_private_bytes(Path(seed).read_bytes())
        public = signer.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
        require(public == row['signer_public_key'], 'activity signer differs from registered identity')
        signature = signer.sign(bytes.fromhex(prepared['signing_preimage'])).hex()
        self.success(client, {'tool': 'activity.sign', 'idempotency_key': key, 'arguments': {
            'variant': 'external_signature_v1', 'preparation_ref': prepared['preparation_id'], 'signature': signature}})
        values = {'preparation_id': prepared['preparation_id'], 'signature': signature, 'signer_public_key': public}
        submit = OWNERS.substitute(copy.deepcopy(row['submit_template']), values)
        require(submit.get('preparation_ref') == prepared['preparation_id']
                and submit.get('signature') == signature and submit.get('signer_public_key') == public,
                'actual Submit must retain exact original preparation and external signature')
        observed = self.success(client, {'tool': 'activity.submit', 'idempotency_key': key, 'arguments': submit})
        submission = observed['submission']['submission_ref']
        tracked = self.success(client, {'tool': 'activity.track', 'idempotency_key': key,
            'arguments': {'submission_ref': submission}})
        waited = self.await_executed(transport, key, tracked)
        require(tracked['activity_id'] == waited['activity_id'], 'Track/Wait changed original activity identity')
        return self.verified(waited)

    def negatives(self, transport):
        results = {}
        for name, row in self.profile['negative_cases'][transport].items():
            require(isinstance(row, dict) and set(row) == {'participant', 'call', 'reason'}
                    and row['participant'] in ('full', 'narrowed', 'foreign')
                    and isinstance(row['reason'], str) and row['reason'],
                    'genuine exact negative call and typed refusal required')
            results[name] = self.refusal(self.clients[transport + '-' + row['participant']], row['call'], row['reason'])
        return results

    def advertisements(self, transport):
        full = self.clients[transport + '-full'].message('tools/list', {})
        narrowed = self.clients[transport + '-narrowed'].message('tools/list', {})
        full_names = {row['name'] for row in full['tools']}
        narrow_names = {row['name'] for row in narrowed['tools']}
        require(set(FAMILIES) <= full_names and not set(FAMILIES) & narrow_names,
                'advertisement differs from actual capability/mode/scope dispatch availability')
        return {'full_write_families': sorted(set(FAMILIES) & full_names), 'narrowed_write_families': []}

    def transitions(self, transport):
        result = {}
        for name, row in self.profile['transitions'][transport].items():
            require(isinstance(row, dict) and set(row) == {'requests', 'participant', 'call', 'reason'}
                    and isinstance(row['requests'], list) and row['requests'],
                    'real authorized authority transition producer required')
            for request_name in row['requests']:
                envelope = self.fixture.envelope(request_name)
                status, response = self.rpc(envelope)
                require(status == 200 and 'value' in response, 'actual authorized authority transition refused')
            result[name] = self.refusal(self.clients[transport + '-' + row['participant']], row['call'], row['reason'])
        return result

    def interruption(self, transport):
        row = self.profile['interruption'][transport]
        family = row['family']
        call = self.alias(transport, family, row['write'])
        role = transport + '-full'
        with self.paused_service(row['paused_artifact']):
            with ThreadPoolExecutor(max_workers=1) as executor:
                invocation = executor.submit(self.clients[role].call, call)
                before, before_path = self.await_event(role, call, 'native_transmission_started', transport + '-pending')
                require(before['phase'] == 'effect_started' and before['native_preparation'] is not None,
                        'real native economic transmission did not enter durable pending state')
                self.daemon.process.kill()
                self.daemon.process.wait(timeout=10)
                refused, response = invocation.result(timeout=60)
        require(refused and response.get('refusal', {}).get('state') == 'unknown'
                and response['refusal'].get('reason') == 'outcome.unknown' and 'result' not in response,
                'transport interruption was reported as economic success')
        self.clients[role].stop()
        self.daemon.start()
        self.clients[role].start()
        deadline = time.monotonic() + 120
        serial = 0
        while True:
            serial += 1
            refused, response = self.clients[role].call(call)
            after, path = self.snapshot_evidence(role, call, transport + '-reconcile-' + str(serial))
            require(after['native_preparation'] == before['native_preparation']
                    and after['request_digest'] == before['request_digest']
                    and sum(e['kind'] == 'effect_start' for e in after['events']) == 1
                    and sum(e['kind'] == 'native_transmission_started' for e in after['events']) == 1,
                    'restart replayed or re-signed changed economic bytes')
            if not refused:
                verified = self.verified(response['result'])
                require(after['phase'] == 'settled' and after['reconciled_receipt'] is not None,
                        'restart completion has no genuine receipt-backed durable reconciliation')
                break
            require(response.get('refusal', {}).get('state') == 'unknown' and time.monotonic() < deadline,
                    'durable Unknown did not reconcile through its real receipt owner')
            time.sleep(0.1)
        self.clients[role].stop()
        self.daemon.stop()
        self.daemon.start()
        self.clients[role].start()
        restored = self.success(self.clients[role], call)
        require(self.verified(restored) == verified, 'daemon/MCP second restart changed real receipt identity')
        return dict(verified, before=str(before_path), reconciled=str(path))


def main():
    record = {'revision': None, 'command': COMMAND, 'cases': [], 'exit_code': 1}
    corpus = None
    evidence = None
    try:
        evidence = HTTP.evidence_dir() / ('mcp-served-writes-' + str(time.time_ns()))
        evidence.mkdir(mode=0o700)
        head, dirty = HTTP.revision()
        record['revision'] = head
        require(not dirty, 'candidate source is dirty')
        record['candidate_manifest'] = str(HTTP.load_manifest(head))
        corpus = Corpus.__new__(Corpus)
        Corpus.__init__(corpus, evidence)
        corpus.start()
        for transport in TRANSPORTS:
            cases = [('advertisement', lambda t=transport: corpus.advertisements(t))]
            cases += [(family, lambda t=transport, f=family: corpus.family(t, f)) for family in FAMILIES]
            cases += [('ordinary_activity_stages', lambda t=transport: corpus.activity(t)),
                      ('mandatory_refusals', lambda t=transport: corpus.negatives(t)),
                      ('interruption_restart_reconciliation', lambda t=transport: corpus.interruption(t)),
                      ('authority_transitions', lambda t=transport: corpus.transitions(t))]
            for name, action in cases:
                result = action()
                record['cases'].append({'transport': transport, 'case': name, 'result': 'PASS', 'evidence': result})
        require(len(record['cases']) == 22, 'mandatory real served write case skipped')
        secrets = corpus.fixture.secret_values()
        for path in evidence.rglob('*.log'):
            text = path.read_text(errors='replace')
            require(not any(value and value in text for value in secrets), 'secret value appeared in process log')
        record['exit_code'] = 0
    except (HTTP.Failure, HTTP.FixtureRefused, OSError, ValueError, KeyError, TypeError,
            subprocess.SubprocessError) as error:
        record['failure'] = corpus.fixture.redact(str(error)) if corpus and hasattr(corpus, 'fixture') else str(error)
    finally:
        if corpus is not None and hasattr(corpus, 'clients'):
            corpus.close()
        if evidence is not None:
            private_write(evidence / 'qualification.json', record)
    print(json.dumps({'revision': record['revision'], 'command': COMMAND, 'exit_code': record['exit_code'],
                      'cases_completed': len(record['cases']), 'evidence': str(evidence) if evidence else None}))
    return record['exit_code']


if __name__ == '__main__':
    os.umask(0o077)
    if sys.argv[1:] == ['--worker']:
        def interrupted(signum, _frame):
            raise HTTP.Failure('qualification interrupted by signal ' + str(signum))
        signal.signal(signal.SIGTERM, interrupted)
        signal.signal(signal.SIGINT, interrupted)
        subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True)
        sys.exit(main())
    require(not sys.argv[1:], 'unexpected qualification arguments')
    environment = dict(os.environ)
    for namespace in ('net', 'pid', 'mnt'):
        environment['PAXEER_X_HTTP_PARENT_' + namespace.upper()] = os.readlink('/proc/self/ns/' + namespace)
    sys.exit(subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--mount-proc',
        sys.executable, str(Path(__file__).resolve()), '--worker'], env=environment).returncode)
