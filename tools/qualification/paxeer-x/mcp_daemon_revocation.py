#!/usr/bin/env python3
import copy
from contextlib import contextmanager
from concurrent.futures import ThreadPoolExecutor
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import socket
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
COMMAND = 'timeout 30m python3 tools/qualification/paxeer-x/mcp_daemon_revocation.py'
ROLES = ('closed', 'revoked', 'narrowed', 'expiring', 'same_tenant', 'other_tenant')
BOUNDARIES = ('reads', 'subscriptions', 'prepare', 'approvals', 'writes')
CASES = ('baseline_two_process_observation', 'candidate_live_boundary', 'connected_close',
         'connected_revocation', 'connected_narrowing', 'expired_distinct_from_revoked',
         'full_credential_isolation', 'effect_commit_linearization', 'result_release_linearization',
         'durable_unknown_reconciliation', 'daemon_restart', 'mcp_restart',
         'authority_unavailable', 'tenant_and_session_isolation', 'no_secret_logging')
INCOMPLETE_CASES = {}


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


HTTP = module('mcp_revocation_http', Path(__file__).with_name('agent_http_bounds.py'))
READINESS = module('mcp_revocation_evidence', Path(__file__).with_name('agent_tenant_readiness.py'))
require = READINESS.require
private_write = READINESS.private_write


def baseline_artifacts():
    path = os.environ.get('PAXEER_X_MCP_BASELINE_ARTIFACTS')
    document = HTTP._FIXTURE.private_json(path, 'PAXEER_X_MCP_BASELINE_ARTIFACTS')
    require(document.get('schema') == 'paxeer-x.agentd-http-fixture.v1',
            'baseline must retain the existing protected actual process artifact schema')
    rows = document.get('artifacts', {})
    require({'layerx-agentd', 'layerx-mcp'} <= set(rows), 'actual baseline Agentd and MCP executables required')
    revisions = set()
    selected = {}
    for name in ('layerx-agentd', 'layerx-mcp'):
        row = rows[name]
        binary = Path(row.get('path', ''))
        require(binary.is_absolute() and binary.resolve() == binary and binary.is_file()
                and os.access(binary, os.X_OK) and re.fullmatch('[0-9a-f]{40}', row.get('source_revision', ''))
                and row.get('sha256') == HTTP._FIXTURE.digest(binary),
                'baseline binary source identity or digest unavailable: ' + name)
        with binary.open('rb') as stream:
            require(stream.read(4) == b'\x7fELF', 'baseline must be an actual native executable')
        revisions.add(row['source_revision'])
        selected[name] = row
    require(len(revisions) == 1, 'baseline Agentd and MCP must share their actual built source revision')
    revision = next(iter(revisions))
    subprocess.run(['git', '-C', str(ROOT), 'cat-file', '-e', revision + '^{commit}'], check=True)
    subprocess.run(['git', '-C', str(ROOT), 'merge-base', '--is-ancestor', revision, 'HEAD'], check=True)
    return {'path': str(path), 'revision': revision, 'artifacts': selected}


def substitute(value, values):
    if isinstance(value, dict):
        return {key: substitute(item, values) for key, item in value.items()}
    if isinstance(value, list):
        return [substitute(item, values) for item in value]
    return value.format_map(values) if isinstance(value, str) else value


def u64(value):
    return type(value) is int and 0 <= value <= 18_446_744_073_709_551_615


def bytes32(value):
    return isinstance(value, list) and len(value) == 32 and any(value) and all(
        type(item) is int and 0 <= item <= 255 for item in value)


def invocation_evidence(record, credential, call):
    key = call['idempotency_key']
    fields = {'schema', 'tenant', 'actor', 'session', 'generation', 'idempotency_key',
              'request_digest', 'core_sequence', 'phase', 'response', 'events',
              'tool_name', 'native_preparation', 'reconciled_receipt'}
    require(isinstance(record, dict) and set(record) == fields
            and record['schema'] == 'layerx.mcp.invocation.v1'
            and record['tool_name'] == call['tool']
            and record['tenant'] == credential['tenant']
            and bytes32(record['session']) and bytes(record['session']).hex() == credential['session_id']
            and bytes32(record['idempotency_key']) and bytes(record['idempotency_key']).hex() == key
            and record['generation'] == int(credential['generation'])
            and u64(record['generation']) and record['generation'] > 0
            and bytes32(record['request_digest']) and u64(record['core_sequence'])
            and isinstance(record['actor'], list) and 0 < len(record['actor']) <= 4096
            and all(type(item) is int and 0 <= item <= 255 for item in record['actor'])
            and record['phase'] in ('begun', 'effect_started', 'settled'),
            'offline serializer did not return the exact actual invocation identity')
    response = record['response']
    require((record['phase'] == 'settled') == (response is not None)
            and (response is None or isinstance(response, list) and len(response) <= 4 * 1024 * 1024
                 and all(type(item) is int and 0 <= item <= 255 for item in response)),
            'durable phase and response bytes disagree')
    native = record['native_preparation']
    require(native is None or isinstance(native, dict) and set(native) == {
        'preparation_id', 'idempotency_key'} and bytes32(native['preparation_id'])
        and bytes32(native['idempotency_key']), 'actual native preparation association is malformed')
    receipt = record['reconciled_receipt']
    if receipt is not None:
        require(native is not None and isinstance(receipt, dict) and set(receipt) == {
            'activity_id', 'receipt_digest', 'receipt_bytes', 'global_sequence', 'verification_rank', 'result_code'}
            and bytes32(receipt['activity_id']) and bytes32(receipt['receipt_digest'])
            and isinstance(receipt['receipt_bytes'], list)
            and all(type(item) is int and 0 <= item <= 255 for item in receipt['receipt_bytes'])
            and u64(receipt['global_sequence']) and receipt['global_sequence'] > 0
            and type(receipt['verification_rank']) is int and 3 <= receipt['verification_rank'] <= 5
            and receipt['result_code'] == 0 and type(receipt['result_code']) is int,
            'native reconciliation lacks actual StateProven successful receipt evidence')
        facts = READINESS.receipt_facts(bytes(receipt['receipt_bytes']).hex(), bytes(receipt['activity_id']).hex())
        require(facts['global_sequence'] == str(receipt['global_sequence'])
                and facts['receipt_sha256'] == bytes(receipt['receipt_digest']).hex(),
                'native reconciliation receipt digest or sequence does not match canonical protocol bytes')
    events = record['events']
    require(isinstance(events, list) and events, 'durable invocation has no owner ordering evidence')
    previous = 0
    for event in events:
        require(isinstance(event, dict) and set(event) == {
            'order', 'kind', 'current_generation', 'core_sequence'}
            and u64(event['order']) and event['order'] > previous
            and event['kind'] in ('begin', 'effect_start', 'effect_settled', 'effect_unknown',
                'effect_refused', 'release_refused', 'release_generation_readback',
                'response_write_flush_ok', 'response_write_flush_failed', 'session_close',
                'session_revoke', 'session_narrow', 'session_refresh', 'native_association',
                'native_transmission_started', 'native_receipt_reconciled')
            and u64(event['current_generation']) and event['current_generation'] > 0
            and u64(event['core_sequence']), 'actual durable event sequence is malformed')
        previous = event['order']
    return record


class McpProcess:
    def __init__(self, fixture, binary, binding_seed, credential, directory, label, candidate):
        self.fixture, self.binary, self.credential = fixture, binary, credential
        self.directory, self.label = directory, label
        self.candidate = candidate
        self.sequence = 0
        self.process = self.connection = self.stream = None
        require(binding_seed in fixture.document.get('seeds', {}),
                'genuine provisioned MCP binding seed required: ' + label)
        source = Path(fixture.values[binding_seed])
        binding = substitute(json.loads(source.read_text()), fixture.values)
        require(binding.get('tenant') == credential['tenant']
                and binding.get('session_id') == credential['session_id']
                and str(binding.get('session_generation')) == credential['generation']
                and isinstance(binding.get('capability_id'), str)
                and re.fullmatch('[0-9a-f]{64}', binding['capability_id'])
                and binding['capability_id'] != '0' * 64,
                'MCP requires the genuine retained session and actual legacy capability')
        for raw in (binding.get('store'), binding.get('audit_root'), binding.get('session_token_file'),
                    binding.get('agent', {}).get('bearer_file'), binding.get('listener', {}).get('socket')):
            require(isinstance(raw, str) and Path(raw).is_absolute() and Path(raw).resolve() == Path(raw)
                    and fixture.directory in Path(raw).parents,
                    'MCP authority and writable paths must remain in copied disposable state')
        require(Path(binding['session_token_file']).read_text().strip() == credential['token_id'],
                'MCP raw token differs from its genuine daemon credential')
        self.binding = binding
        self.path = directory / (label + '-binding.json')
        private_write(self.path, binding)
        self.starts = 0

    def start(self):
        self.starts += 1
        log = self.directory / (self.label + '-' + str(self.starts) + '.log')
        with log.open('xb') as output:
            self.process = subprocess.Popen([str(self.binary), str(self.path)], cwd=self.fixture.directory,
                stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT)
        endpoint = self.binding['listener']['socket']
        deadline = time.monotonic() + 30
        while True:
            require(self.process.poll() is None and time.monotonic() < deadline,
                    'actual MCP refused provisioned authority or did not serve: ' + self.label)
            try:
                self.connection = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                self.connection.settimeout(20)
                self.connection.connect(endpoint)
                break
            except OSError:
                self.connection.close()
                self.connection = None
                time.sleep(0.1)
        self.stream = self.connection.makefile('rwb')
        self.message('initialize', {})

    def message(self, method, params):
        self.sequence += 1
        request = {'jsonrpc': '2.0', 'id': self.sequence, 'method': method, 'params': params}
        self.stream.write(json.dumps(request, separators=(',', ':')).encode() + b'\n')
        self.stream.flush()
        raw = self.stream.readline(1_048_577)
        require(raw.endswith(b'\n') and len(raw) <= 1_048_576, 'MCP response framing or bound failed')
        response = json.loads(raw)
        require(response.get('jsonrpc') == '2.0' and response.get('id') == self.sequence,
                'MCP response lost invocation identity')
        private_write(self.directory / (self.label + '-response-' + str(self.starts) + '-' + str(self.sequence) + '.json'), response)
        require('error' not in response, 'actual MCP protocol operation is unavailable: ' + method)
        return response['result']

    def call(self, body):
        require(isinstance(body, dict) and set(body) == {'tool', 'arguments', 'idempotency_key'}
                and isinstance(body['tool'], str) and isinstance(body['arguments'], dict),
                'canonical real MCP tool invocation required')
        key = body['idempotency_key']
        require(isinstance(key, str) and re.fullmatch('[0-9a-f]{64}', key) and key != '0' * 64
                and body['arguments'].get('idempotency_key', key) == key,
                'actual caller idempotency must retain its immutable invocation purpose')
        result = self.message('tools/call', {'name': body['tool'], 'arguments': body['arguments'],
            '_meta': {'layerx/idempotency_key': key}})
        require(isinstance(result, dict) and type(result.get('isError')) is bool
                and len(result.get('content', [])) == 1 and result['content'][0].get('type') == 'text',
                'MCP did not return its complete canonical tool result')
        value = json.loads(result['content'][0]['text'])
        if self.candidate:
            require(result.get('structuredContent') == value,
                    'candidate MCP structured result differs from its canonical content')
        return result['isError'], value

    def stop(self):
        if self.stream is not None:
            self.stream.close()
            self.stream = None
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

    def rejected_restart(self, reason):
        require(self.candidate, 'typed restart refusal requires the actual candidate MCP binary')
        self.stop()
        self.starts += 1
        completed = subprocess.run([str(self.binary), str(self.path)], cwd=self.fixture.directory,
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
        path = self.directory / (self.label + '-rejected-' + str(self.starts) + '.log')
        path.write_bytes(completed.stdout + completed.stderr)
        path.chmod(0o600)
        lines = completed.stderr.splitlines()
        prefix = b'layerx-mcp: '
        require(completed.returncode == 1 and completed.stdout == b'' and len(lines) == 1
                and lines[0].startswith(prefix), 'MCP restart did not fail through its typed authority constructor')
        detail = json.loads(lines[0][len(prefix):])
        require(detail == {'class': 'PolicyRefusal', 'reason': reason, 'state': 'refused'},
                'MCP restart accepted stale authority or returned a different constructor refusal')
        return {'exit_code': completed.returncode, 'refusal': detail, 'log': str(path)}


class Phase:
    def __init__(self, evidence, label, selected=None):
        self.directory = evidence / label
        self.directory.mkdir(mode=0o700)
        self.fixture = HTTP.AgentdFixture(self.directory / 'fixture')
        self.clients = {}
        self.daemon = None
        try:
            self.configure(selected)
        except (HTTP.Failure, HTTP.FixtureRefused, OSError, ValueError, KeyError, TypeError,
                subprocess.SubprocessError):
            self.close()
            raise

    def configure(self, selected):
        if selected is not None:
            self.fixture.artifacts = dict(self.fixture.artifacts, **selected['artifacts'])
        config = HTTP.daemon_config(self.fixture)
        require('agent_program_port' in self.fixture.values,
                'provisioned disposable program listener port required')
        self.clients = {}
        self.serial = 800_000
        self.profile = self.fixture.document.get('mcp_revocation')
        require(isinstance(self.profile, dict) and set(self.profile) == {
            'participants', 'transitions', 'unknown_track_request', 'boundary_evidence'},
            'missing genuine MCP revocation fixture profile')
        require(isinstance(self.profile['boundary_evidence'], dict)
                and set(self.profile['boundary_evidence']) == {
                    'effect_commit_linearization', 'result_release_linearization', 'durable_unknown_reconciliation'},
                'every mandatory boundary needs its genuine process fault declaration')
        require(isinstance(self.profile['participants'], dict)
                and set(self.profile['participants']) == set(ROLES),
                'six genuinely provisioned session roles are required')
        self.credentials = {}
        for role, row in self.profile['participants'].items():
            require(isinstance(row, dict) and set(row) == {'binding_seed', 'credential_request', 'calls'}
                    and isinstance(row['calls'], dict) and set(row['calls']) == set(BOUNDARIES),
                    'every live session must provision all five actual tool boundary calls')
            credential = self.fixture.envelope(row['credential_request'])['credential']
            require(set(credential) == {'tenant', 'session_id', 'token_id', 'generation'}
                    and all(isinstance(value, str) for value in credential.values())
                    and all(re.fullmatch('[0-9a-f]{64}', credential[key]) for key in ('session_id', 'token_id'))
                    and re.fullmatch('[1-9][0-9]{0,19}', credential['generation']),
                    'full actual daemon session credential required')
            self.credentials[role] = credential
        require(self.credentials['closed']['tenant'] == self.credentials['same_tenant']['tenant']
                and self.credentials['closed']['session_id'] != self.credentials['same_tenant']['session_id']
                and self.credentials['closed']['tenant'] != self.credentials['other_tenant']['tenant'],
                'same-tenant session and cross-tenant isolation are not provisioned')
        require('layerx-mcp' in self.fixture.artifacts,
                'actual source-bound MCP process artifact is missing')
        for role in ROLES:
            row = self.profile['participants'][role]
            self.clients[role] = McpProcess(self.fixture,
                Path(self.fixture.artifacts['layerx-mcp']['path']), row['binding_seed'],
                self.credentials[role], self.directory, role, selected is None)
        config['LAYERX_AGENTD_MCP_BINDINGS'] = ','.join(
            str(self.clients[role].path) for role in ROLES)
        store = Path(config['LAYERX_AGENT_HUMAN_STORE'])
        require(store.is_absolute() and store.resolve() == store and self.fixture.directory in store.parents,
                'genuine daemon store root must remain canonical and disposable')
        store.mkdir(mode=0o700, parents=True, exist_ok=True)
        store.chmod(0o700)
        self.daemon = HTTP.Daemon(Path(self.fixture.artifacts['layerx-agentd']['path']), config,
            self.directory, int(self.fixture.values['agent_program_port']), self.fixture)

    def start(self, roles=ROLES):
        self.daemon.start()
        for role in roles:
            self.open_mcp(role)

    def open_mcp(self, role):
        self.clients[role].start()

    def rpc(self, envelope, verified=False):
        status, response = HTTP.rpc_request(self.daemon, envelope)
        self.serial += 1
        private_write(self.directory / ('rpc-' + str(self.serial) + '.json'), {
            'operation': envelope['operation'], 'request_id': envelope['request_id'],
            'status': status, 'response': response})
        require(isinstance(response, dict) and response.get('request_id') == envelope['request_id'],
                'actual common resolver response lost request identity')
        if verified:
            require(status == 200 and response.get('verification_status', {}).get('state') == 'achieved'
                    and response['verification_status'].get('level') in (
                        'StateProven', 'CheckpointFinalised', 'SettlementAnchored'),
                    'actual outcome lacks StateProven protocol evidence')
        return status, response

    def live(self, role, boundary='reads'):
        call = self.profile['participants'][role]['calls'][boundary]
        refused, value = self.clients[role].call(call)
        require(not refused and value.get('tool') == call['tool'] and 'result' in value,
                'actual baseline tool boundary is not usable: ' + role + '/' + boundary)
        return value

    def transition(self, name):
        requests = self.profile['transitions'].get(name)
        require(isinstance(requests, list) and requests,
                'missing actual authorized transition producer: ' + name)
        responses = []
        for case in requests:
            envelope = self.fixture.envelope(case)
            require(envelope.get('idempotency_key') is not None,
                    'actual transition must retain its supplied durable idempotency key')
            status, response = self.rpc(envelope)
            require(status == 200 and set(response) == {'request_id', 'value', 'verification_status'},
                    'actual daemon refused authorized transition: ' + name)
            responses.append(response)
        return responses

    def denied(self, role, reason, boundaries=BOUNDARIES):
        results = []
        for boundary in boundaries:
            call = self.profile['participants'][role]['calls'][boundary]
            refused, value = self.clients[role].call(call)
            detail = value.get('refusal', {})
            require(refused and isinstance(detail, dict) and detail.get('reason') == reason
                    and detail.get('class') == 'PolicyRefusal' and detail.get('state') == 'refused'
                    and 'result' not in value,
                    'connected MCP did not return the exact typed refusal at ' + boundary)
            results.append({'boundary': boundary, 'class': detail['class'], 'reason': detail['reason']})
        return results

    def snapshot_evidence(self, role, call, label, allow_absent=False):
        store = Path(self.daemon.config['LAYERX_AGENT_HUMAN_STORE'])
        require(store.is_absolute() and store.resolve() == store and self.fixture.directory in store.parents,
                'snapshot evidence store must be the real disposable daemon owner')
        credential = self.credentials[role]
        key = call['idempotency_key']
        require(isinstance(key, str) and re.fullmatch('[0-9a-f]{64}', key) and key != '0' * 64,
                'actual caller invocation key required for offline evidence')
        environment = {name: value for name, value in os.environ.items()
                       if not name.startswith(('LAYERX_', 'PAXEER_X_'))}
        completed = subprocess.run([str(self.daemon.binary), '--mcp-evidence', str(store),
            credential['tenant'], key, call['tool']], cwd=self.fixture.directory, env=environment,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
        log = self.directory / (label + '-serializer.log')
        log.write_bytes(completed.stderr)
        log.chmod(0o600)
        require(completed.returncode == 0, 'actual daemon evidence serializer refused the durable owner')
        record = json.loads(completed.stdout)
        if record is None:
            require(allow_absent, 'actual owner invocation record is missing')
        else:
            record = invocation_evidence(record, credential, call)
        path = self.directory / (label + '-invocation.json')
        private_write(path, record)
        return record, path

    def await_event(self, role, call, kind, label):
        deadline = time.monotonic() + 30
        serial = 0
        while True:
            serial += 1
            record, path = self.snapshot_evidence(role, call, label + '-' + str(serial), allow_absent=True)
            if record is not None and any(event['kind'] == kind for event in record['events']):
                return record, path
            require(self.daemon.alive() and time.monotonic() < deadline,
                    'actual owner never produced required durable event: ' + kind)
            time.sleep(0.05)

    @contextmanager
    def paused_service(self, artifact):
        rows = self.fixture.document['services']
        selected = [self.fixture.processes[index][0] for index, row in enumerate(rows)
                    if row.get('artifact') == artifact and row.get('oneshot') is not True]
        require(artifact in self.fixture.artifacts and len(selected) == 1
                and selected[0].poll() is None,
                'fault injection requires one genuine running upstream service artifact')
        process = selected[0]
        os.kill(process.pid, signal.SIGSTOP)
        try:
            deadline = time.monotonic() + 5
            while True:
                status = Path('/proc/' + str(process.pid) + '/status').read_text()
                state = next((line.split()[1] for line in status.splitlines() if line.startswith('State:')), None)
                if state == 'T':
                    break
                require(process.poll() is None and time.monotonic() < deadline,
                        'genuine upstream did not enter the required kernel stopped state')
                time.sleep(0.01)
            yield process
        finally:
            if process.poll() is None:
                os.kill(process.pid, signal.SIGCONT)

    def release_race(self):
        row = self.profile['boundary_evidence'].get('result_release_linearization')
        require(isinstance(row, dict) and set(row) == {'role', 'boundary', 'transition', 'fault_service'}
                and row['role'] == 'closed' and row['boundary'] == 'reads' and row['transition'] == 'close',
                'genuine sensitive-read release race configuration is missing')
        role = row['role']
        call = self.profile['participants'][role]['calls'][row['boundary']]
        self.live('same_tenant')
        with ThreadPoolExecutor(max_workers=2) as pool:
            with self.paused_service(row['fault_service']):
                invocation = pool.submit(self.clients[role].call, call)
                pending, pending_path = self.await_event(role, call, 'effect_start', 'release-pending')
                require(pending['phase'] == 'effect_started' and not invocation.done(),
                        'the genuine sensitive-read invocation was not in flight at the actual owner boundary')
                mutation = pool.submit(self.transition, row['transition'])
            refused, value = invocation.result(timeout=60)
            changed = mutation.result(timeout=60)
        self.daemon.stop()
        record, path = self.snapshot_evidence(role, call, 'release-final')
        mutations = [event for event in record['events'] if event['kind'] == 'session_close']
        require(len(mutations) == 1 and mutations[0]['current_generation'] > record['generation'],
                'actual release evidence lacks the atomic generation-close marker')
        transition = mutations[0]
        for event in record['events']:
            if event['kind'] in ('release_generation_readback', 'response_write_flush_ok', 'response_write_flush_failed'):
                require(event['order'] < transition['order']
                        and event['current_generation'] == record['generation'],
                        'sensitive response writer crossed a committed revocation boundary')
            if event['kind'] == 'release_refused':
                require(event['order'] > transition['order']
                        and event['current_generation'] == transition['current_generation'],
                        'result-release refusal is not bound to actual current generation')
        if refused:
            detail = value.get('refusal', {})
            require(isinstance(detail, dict) and detail.get('reason') == 'session.revoked'
                    and detail.get('class') == 'PolicyRefusal' and detail.get('state') == 'refused'
                    and 'result' not in value
                    and any(event['kind'] == 'release_refused' for event in record['events']),
                    'cancelled sensitive result lacks exact typed release refusal')
        else:
            require(value.get('tool') == call['tool'] and 'result' in value
                    and any(event['kind'] == 'response_write_flush_ok' for event in record['events']),
                    'sensitive result has no actual authorized write-and-flush evidence')
        self.daemon.start()
        stale = self.denied(role, 'session.revoked', ('reads',))
        self.live('same_tenant')
        self.live('other_tenant')
        self.daemon.stop()
        after, after_path = self.snapshot_evidence(role, call, 'release-after-stale')
        require(after == record, 'stale caller changed or released the durable sensitive result')
        return {'pending_evidence': str(pending_path), 'release_evidence': str(path),
                'after_stale_evidence': str(after_path), 'mutation': changed,
                'revoked': refused, 'stale_refusal': stale}

    def effect_race(self):
        row = self.profile['boundary_evidence'].get('effect_commit_linearization')
        require(isinstance(row, dict) and set(row) == {'role', 'boundary', 'transition', 'fault_service'}
                and row['role'] == 'closed' and row['boundary'] == 'writes' and row['transition'] == 'close',
                'genuine native effect-commit race configuration is missing')
        role = row['role']
        call = self.profile['participants'][role]['calls'][row['boundary']]
        self.live('same_tenant')
        with ThreadPoolExecutor(max_workers=2) as pool:
            with self.paused_service(row['fault_service']):
                invocation = pool.submit(self.clients[role].call, call)
                pending, pending_path = self.await_event(role, call, 'effect_start', 'effect-pending')
                require(pending['phase'] == 'effect_started' and not invocation.done(),
                        'the genuine native write was not in flight at the actual owner boundary')
                mutation = pool.submit(self.transition, row['transition'])
            refused, value = invocation.result(timeout=60)
            changed = mutation.result(timeout=60)
        self.daemon.stop()
        record, path = self.snapshot_evidence(role, call, 'effect-final')
        mutations = [event for event in record['events'] if event['kind'] == 'session_close']
        require(len(mutations) == 1 and mutations[0]['current_generation'] > record['generation'],
                'actual effect evidence lacks the atomic generation-close marker')
        transition = mutations[0]
        committed = [event for event in record['events'] if event['kind'] == 'native_transmission_started']
        require(len(committed) <= 1, 'one native caller key caused repeated economic transmission')
        for event in committed:
            require(event['order'] < transition['order']
                    and event['current_generation'] == record['generation'],
                    'actual native transmission crossed a committed revocation boundary')
        if not committed:
            detail = value.get('refusal', {})
            require(refused and isinstance(detail, dict) and detail.get('class') == 'PolicyRefusal'
                    and detail.get('reason') == 'session.revoked' and detail.get('state') == 'refused'
                    and 'result' not in value,
                    'uncommitted in-flight write lacks the exact typed generation refusal')
        else:
            require(record['native_preparation'] is not None,
                    'actual transmission is not associated with its genuine native preparation')
        self.daemon.start()
        stale = self.denied(role, 'session.revoked', ('writes',))
        self.live('same_tenant')
        self.live('other_tenant')
        self.daemon.stop()
        after, after_path = self.snapshot_evidence(role, call, 'effect-after-stale')
        require(after == record, 'stale caller replayed or altered the durable economic invocation')
        return {'pending_evidence': str(pending_path), 'effect_evidence': str(path),
                'after_stale_evidence': str(after_path), 'mutation': changed,
                'transmissions': len(committed), 'stale_refusal': stale}

    def unknown_fault(self):
        row = self.profile['boundary_evidence'].get('durable_unknown_reconciliation')
        require(isinstance(row, dict) and set(row) == {'role', 'boundary', 'fault_service'}
                and row['role'] == 'same_tenant' and row['boundary'] == 'writes',
                'genuine unknown-submission fault configuration is missing')
        role = row['role']
        call = self.profile['participants'][role]['calls'][row['boundary']]
        with ThreadPoolExecutor(max_workers=1) as pool:
            with self.paused_service(row['fault_service']):
                invocation = pool.submit(self.clients[role].call, call)
                pending, pending_path = self.await_event(role, call, 'native_transmission_started', 'unknown-pending')
                require(pending['phase'] == 'effect_started' and pending['response'] is None
                        and pending['native_preparation'] is not None and not invocation.done(),
                        'actual native submission was not durably pending at the fault boundary')
                self.daemon.process.kill()
                self.daemon.process.wait(timeout=10)
                refused, interrupted = invocation.result(timeout=60)
        detail = interrupted.get('refusal', {})
        require(refused and isinstance(detail, dict) and detail.get('reason') == 'outcome.unknown'
                and detail.get('state') == 'unknown' and 'result' not in interrupted,
                'interrupted real economic invocation was falsely reported as terminal')
        before, before_path = self.snapshot_evidence(role, call, 'unknown-after-kill')
        require(before['phase'] == 'effect_started' and before['response'] is None
                and before['native_preparation'] == pending['native_preparation'],
                'crash erased or synthetically settled the genuine unknown preparation')

        def no_replay(record):
            require(record['native_preparation'] == before['native_preparation']
                    and record['request_digest'] == before['request_digest']
                    and sum(event['kind'] == 'effect_start' for event in record['events']) == 1
                    and sum(event['kind'] == 'native_transmission_started' for event in record['events']) == 1,
                    'reconciliation replayed the already-durable native economic submission')

        no_replay(before)
        self.clients[role].stop()
        self.daemon.start()
        self.clients[role].start()
        deadline = time.monotonic() + 120
        serial = 0
        while True:
            serial += 1
            refused, value = self.clients[role].call(call)
            after, path = self.snapshot_evidence(role, call, 'unknown-reconcile-' + str(serial))
            no_replay(after)
            if after['phase'] == 'settled':
                require(after['reconciled_receipt'] is not None
                        and sum(event['kind'] == 'native_receipt_reconciled' for event in after['events']) == 1,
                        'actual unknown was settled without genuine stored StateProven receipt evidence')
                durable = json.loads(bytes(after['response']))
                require(not refused and isinstance(durable, dict) and set(durable) == {'ok'}
                        and value.get('tool') == call['tool'] and value.get('result') == durable['ok'],
                        'served reconciliation differs from the genuine durable outcome')
                break
            detail = value.get('refusal', {})
            require(after['phase'] == 'effect_started' and after['response'] is None
                    and refused and isinstance(detail, dict) and detail.get('reason') == 'outcome.unknown'
                    and detail.get('state') == 'unknown' and 'result' not in value,
                    'unresolved durable economic outcome was replayed or fabricated as completed')
            require(time.monotonic() < deadline, 'genuine unknown receipt reconciliation did not complete within the bound')
            time.sleep(0.1)
        self.clients[role].stop()
        self.daemon.stop()
        self.daemon.start()
        self.clients[role].start()
        refused, restored = self.clients[role].call(call)
        require(not refused and restored.get('result') == durable['ok'],
                'daemon/MCP restart changed the genuine reconciled economic outcome')
        self.daemon.stop()
        recovered, recovered_path = self.snapshot_evidence(role, call, 'unknown-after-restart')
        no_replay(recovered)
        require(recovered['reconciled_receipt'] == after['reconciled_receipt']
                and recovered['response'] == after['response'],
                'restart changed the actual canonical receipt or durable response')
        self.daemon.start()
        legacy = self.unknown()
        return {'pending_evidence': str(pending_path), 'crash_evidence': str(before_path),
                'reconciliation_evidence': str(path), 'restart_evidence': str(recovered_path),
                'receipt_activity': bytes(after['reconciled_receipt']['activity_id']).hex(),
                'legacy_unknown_tracking': legacy}

    def mediated(self, boundaries=('reads',)):
        role = 'same_tenant'
        credential = self.credentials[role]
        rows = []
        for boundary in boundaries:
            call = self.profile['participants'][role]['calls'][boundary]
            self.serial += 1
            request = {'version': 1, 'request_id': str(self.serial), 'operation': 'mcp.invoke',
                       'request': {'tool': call['tool'], 'arguments': copy.deepcopy(call['arguments'])},
                       'credential': copy.deepcopy(credential), 'idempotency_key': call['idempotency_key']}
            if boundary == 'reads':
                status, response = self.rpc(request)
                require(status == 200 and set(response) == {'request_id', 'value', 'verification_status'},
                        'actual source-bound daemon-host mcp.invoke intake is missing')
            for field in ('tenant', 'session_id', 'token_id', 'generation'):
                changed = copy.deepcopy(request)
                changed['request_id'] = str(self.serial + 1)
                if field == 'generation':
                    changed['credential'][field] = str(int(credential[field]) + 1)
                elif field == 'tenant':
                    changed['credential'][field] = self.credentials['other_tenant']['tenant']
                else:
                    value = credential[field]
                    changed['credential'][field] = ('0' if value[0] != '0' else '1') + value[1:]
                status, refusal = self.rpc(changed)
                require(status in (401, 403) and refusal.get('class') == 'PolicyRefusal'
                        and 'value' not in refusal,
                        'common resolver omitted a full credential coordinate: ' + boundary + '/' + field)
                rows.append({'boundary': boundary, 'credential_coordinate': field})
        return {'checked': rows}

    def unknown(self):
        envelope = self.fixture.envelope(self.profile['unknown_track_request'])
        require(envelope['operation'] == 'track', 'durable unknown must use actual authorized tracking')
        status, response = self.rpc(envelope)
        value = response.get('value', {})
        require(status == 200 and value.get('submission', {}).get('state') == 'Unknown'
                and re.fullmatch('[0-9a-f]{64}', value.get('activity_id', '')),
                'genuine already-durable unknown submission is missing')
        self.unknown_before = value
        self.daemon.stop()
        self.daemon.start()
        status, response = self.rpc(envelope)
        after = response.get('value', {})
        require(status == 200 and after.get('activity_id') == value['activity_id']
                and after.get('submission', {}).get('submission_ref') == value['submission']['submission_ref']
                and after['submission']['state'] in ('Unknown', 'Acknowledged', 'Executed'),
                'restart dropped or replaced an already-durable unknown submission')
        if after['submission']['state'] == 'Executed':
            require(response.get('verification_status', {}).get('state') == 'achieved'
                    and response['verification_status'].get('level') in (
                        'StateProven', 'CheckpointFinalised', 'SettlementAnchored')
                    and after.get('receipt', {}).get('verification_level') in (
                        'StateProven', 'CheckpointFinalised', 'SettlementAnchored'),
                    'unknown reconciliation invented an executed receipt')
            READINESS.receipt_facts(after['receipt']['canonical_bytes'], after['activity_id'])
        return {'activity_id': after['activity_id'], 'state': after['submission']['state'],
                'submission_ref': after['submission']['submission_ref']}

    def close(self):
        for client in self.clients.values():
            client.stop()
        if self.daemon is not None:
            self.daemon.stop()
        self.fixture.cleanup()


def main():
    record = {'command': COMMAND, 'revision': None, 'cases': [], 'exit_code': 1,
              'incomplete_cases': INCOMPLETE_CASES}
    evidence = baseline = candidate = None
    try:
        evidence = HTTP.evidence_dir() / ('mcp-revocation-' + str(time.time_ns()))
        evidence.mkdir(mode=0o700)
        head, dirty = HTTP.revision()
        record['revision'] = head
        require(not dirty, 'candidate tree is dirty')
        record['candidate_manifest'] = str(HTTP.load_manifest(head))
        require(not INCOMPLETE_CASES,
                'mandatory source acceptance cases remain incomplete: ' + ', '.join(INCOMPLETE_CASES))
        selected = baseline_artifacts()
        record['baseline'] = selected
        baseline = Phase(evidence, 'baseline', selected)
        baseline.start(('closed', 'revoked', 'narrowed'))
        observations = []
        for role in ('closed', 'revoked', 'narrowed'):
            before = baseline.live(role)
            transition = baseline.transition('close' if role == 'closed' else role)
            observed_refused, observed = baseline.clients[role].call(
                baseline.profile['participants'][role]['calls']['reads'])
            observations.append({'role': role, 'before_transition': before, 'actual_transition': transition,
                'observed_after_transition': observed, 'observed_refusal': observed_refused,
                'exploit_observed': not observed_refused})
        record['cases'].append({'case': CASES[0], 'result': 'PASS', 'source_revision': selected['revision'],
            'observations': observations})
        baseline.close()
        baseline = None
        candidate = Phase(evidence, 'candidate')
        require('layerx-mcp' in candidate.fixture.artifacts,
                'actual source-bound candidate MCP process artifact is missing')
        candidate.start()

        def transitioned(role, reason):
            before = {boundary: candidate.live(role, boundary) for boundary in BOUNDARIES}
            mutation = candidate.transition(role if role != 'closed' else 'close')
            return {'before': before, 'mutation': mutation, 'refusals': candidate.denied(role, reason)}

        def restart_daemon():
            candidate.daemon.stop()
            candidate.daemon.start()
            return {'refusals': candidate.denied('closed', 'session.revoked'),
                    'unaffected_session': candidate.live('same_tenant'),
                    'unaffected_tenant': candidate.live('other_tenant')}

        def restart_mcp():
            candidate.clients['same_tenant'].stop()
            candidate.clients['same_tenant'].start()
            unaffected = candidate.live('same_tenant')
            connected = candidate.denied('revoked', 'session.revoked')
            call = candidate.profile['participants']['revoked']['calls']['reads']
            durable, path = candidate.snapshot_evidence('revoked', call, 'revoked-before-mcp-restart')
            require(any(event['kind'] == 'session_revoke'
                        and event['current_generation'] > durable['generation'] for event in durable['events']),
                    'restarted MCP revocation is not backed by the real durable generation transition')
            revoked = candidate.clients['revoked'].rejected_restart('session.revoked')
            expired = candidate.clients['expiring'].rejected_restart('session.expired')
            return {'unaffected_session': unaffected, 'revoked_connection': connected,
                    'revoked_startup': revoked, 'expired_startup': expired, 'durable_evidence': str(path)}

        def unavailable():
            candidate.daemon.stop()
            try:
                refused, response = candidate.clients['same_tenant'].call(
                    candidate.profile['participants']['same_tenant']['calls']['reads'])
                detail = response.get('refusal', {})
                require(refused and isinstance(detail, dict)
                        and detail.get('reason') == 'mcp.transport_unavailable'
                        and detail.get('state') == 'refused'
                        and 'result' not in response, 'MCP authorized from stale local state while daemon unavailable')
                return response
            finally:
                candidate.daemon.start()

        def isolation():
            return {'same_tenant': candidate.live('same_tenant'), 'other_tenant': candidate.live('other_tenant'),
                    'closed': candidate.denied('closed', 'session.revoked')}

        def fresh_case(name, method):
            phase = Phase(evidence, name)
            try:
                phase.start()
                return method(phase)
            finally:
                phase.close()

        def logs():
            needles = [value.encode() for value in candidate.fixture.secret_values() if value]
            paths = list(evidence.rglob('*.log'))
            for path in paths:
                require(not any(value in path.read_bytes() for value in needles),
                        'actual process log exposed protected authorization material')
            return {'logs_checked': len(paths)}

        actions = {
            'candidate_live_boundary': candidate.mediated,
            'connected_close': lambda: transitioned('closed', 'session.revoked'),
            'connected_revocation': lambda: transitioned('revoked', 'session.revoked'),
            'connected_narrowing': lambda: transitioned('narrowed', 'session.revoked'),
            'expired_distinct_from_revoked': lambda: transitioned('expiring', 'session.expired'),
            'full_credential_isolation': lambda: candidate.mediated(BOUNDARIES),
            'effect_commit_linearization': lambda: fresh_case(
                'effect-commit', lambda phase: phase.effect_race()),
            'result_release_linearization': lambda: fresh_case(
                'result-release', lambda phase: phase.release_race()),
            'durable_unknown_reconciliation': lambda: fresh_case(
                'unknown-reconciliation', lambda phase: phase.unknown_fault()),
            'daemon_restart': restart_daemon,
            'mcp_restart': restart_mcp,
            'authority_unavailable': unavailable,
            'tenant_and_session_isolation': isolation,
            'no_secret_logging': logs,
        }
        require(set(actions) == set(CASES[1:]), 'every mandatory case must have a real acceptance implementation')
        for name in CASES[1:]:
            action = actions[name]
            started = time.monotonic()
            try:
                result = action()
                record['cases'].append({'case': name, 'result': 'PASS', 'evidence': result,
                    'elapsed_s': round(time.monotonic() - started, 3)})
                print('CASE ' + name + ' PASS', flush=True)
            except (HTTP.Failure, HTTP.FixtureRefused, OSError, ValueError, KeyError, TypeError,
                    subprocess.SubprocessError) as error:
                record['cases'].append({'case': name, 'result': 'FAIL',
                    'error': candidate.fixture.redact(str(error))})
                raise
        record['exit_code'] = 0
    except (HTTP.Failure, HTTP.FixtureRefused, OSError, ValueError, KeyError, TypeError,
            subprocess.SubprocessError) as error:
        fixture = candidate.fixture if candidate is not None else baseline.fixture if baseline is not None else None
        message = fixture.redact(str(error)) if fixture is not None else str(error)
        record['error'] = message
        print('mcp_daemon_revocation: ' + message, file=sys.stderr, flush=True)
    finally:
        for phase in (candidate, baseline):
            if phase is not None:
                try:
                    phase.close()
                except (HTTP.Failure, HTTP.FixtureRefused, OSError, subprocess.SubprocessError) as error:
                    record['exit_code'] = 1
                    record.setdefault('error', phase.fixture.redact(str(error)))
    passed = sum(case['result'] == 'PASS' for case in record['cases'])
    skipped = len(CASES) - len(record['cases'])
    require(record['exit_code'] != 0 or passed == len(CASES), 'required MCP acceptance cases were skipped')
    if evidence is not None:
        result = evidence / 'result.json'
        private_write(result, record)
        print('mcp_daemon_revocation: evidence ' + str(result), flush=True)
    print('PAXEER_X_GATE tests=' + str(len(record['cases'])) + ' skipped=' + str(skipped), flush=True)
    print('mcp_daemon_revocation: revision=' + str(record['revision']) + ' command=' + repr(COMMAND)
          + ' exit_code=' + str(record['exit_code']), flush=True)
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
                             sys.executable, str(Path(__file__).resolve()), '--worker'],
                            env=environment).returncode)
