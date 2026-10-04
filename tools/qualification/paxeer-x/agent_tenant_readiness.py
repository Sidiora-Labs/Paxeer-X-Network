#!/usr/bin/env python3
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import socket
import stat
import struct
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
COMMAND = 'timeout 30m python3 tools/qualification/paxeer-x/agent_tenant_readiness.py'
REASONS = {'recovery_pending', 'store_unavailable', 'store_refused', 'budget_state_unverified',
           'receipt_evidence_missing', 'durable_recovery_failed', 'spend_unreconciled',
           'transport_unavailable', 'verified_read_unavailable'}
CASES = ('candidate_artifacts', 'two_authenticated_tenants', 'real_native_send_preparation',
         'failed_tenant_recovery', 'healthy_tenant_verified_write', 'read_write_operator_probe',
         'real_mcp_tenant_readiness', 'failed_recovery_survives_restart', 'durable_recovery_restoration',
         'recovered_tenant_verified_write', 'verified_outcomes_survive_restart',
         'no_secret_logging')


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


HTTP = module('agent_readiness_http', Path(__file__).with_name('agent_http_bounds.py'))
FixtureRefused = HTTP.FixtureRefused


def require(condition, reason):
    if not condition:
        raise HTTP.Failure(reason)


def private_write(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def store_entries(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
            and not info.st_mode & 0o077 and info.st_nlink == 1,
            'disposable daemon store must be an owned private regular file')
    raw = path.read_bytes()
    require(len(raw) <= 256 * 1024 * 1024 and raw[:8] == b'LXAS\0\0\0\3',
            'actual schema3 durable daemon store required')
    offset = 8

    def take(size):
        nonlocal offset
        require(0 <= size <= len(raw) - offset, 'truncated disposable daemon store')
        result = raw[offset:offset + size]
        offset += size
        return result

    def blob():
        return take(int.from_bytes(take(4), 'big'))

    count = int.from_bytes(take(4), 'big')
    require(count <= 1_000_000, 'daemon store entry count exceeds qualification bound')
    entries = []
    keys = set()
    for _ in range(count):
        tenant = blob()
        kind, storage = take(2)
        identifier = blob()
        value = blob()
        key = (tenant, kind, identifier)
        require(key not in keys and 1 <= kind <= 13 and storage in (1, 2),
                'invalid or duplicate actual durable object')
        keys.add(key)
        entries.append((tenant, kind, storage, identifier, value))
    require(offset == len(raw), 'trailing disposable store bytes')
    return entries


def replace_store_entry(path, key, expected, replacement):
    entries = store_entries(path)
    matches = [index for index, row in enumerate(entries)
               if (row[0], row[1], row[3]) == key]
    require(len(matches) == 1, 'exactly one retained tenant recovery owner required')
    index = matches[0]
    row = entries[index]
    require(row[4] == expected, 'tenant recovery owner changed before surgical restoration')
    entries[index] = (*row[:4], replacement)
    output = bytearray(b'LXAS\0\0\0\3' + len(entries).to_bytes(4, 'big'))
    for tenant, kind, storage, identifier, value in entries:
        for blob in (tenant,):
            output.extend(len(blob).to_bytes(4, 'big'))
            output.extend(blob)
        output.extend(bytes((kind, storage)))
        for blob in (identifier, value):
            output.extend(len(blob).to_bytes(4, 'big'))
            output.extend(blob)
    pending = path.with_name('tenant-readiness-store.pending')
    fd = os.open(pending, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(output)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(pending, path)
    fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
    require(store_entries(path) == entries, 'durable surgical fault publication changed another entry')


def receipt_facts(encoded, activity_id):
    require(isinstance(encoded, str) and re.fullmatch('[0-9a-f]+', encoded)
            and len(encoded) <= 2 * 1_212_416, 'missing bounded canonical kernel receipt')
    raw = bytes.fromhex(encoded)
    require(len(raw) > 166, 'actual complete protocol receipt required')
    version, tag, protocol = struct.unpack('>HHH', raw[:6])
    require(version == protocol and 1 <= protocol <= 3 and tag in (0x5201, 0x5202),
            'kernel protocol receipt framing mismatch')
    offset = 6

    def fixed32():
        nonlocal offset
        require(len(raw) >= offset + 36 and raw[offset:offset + 4] == b'\0\0\0\x20',
                'kernel receipt digest framing mismatch')
        result = raw[offset + 4:offset + 36]
        offset += 36
        return result

    require(fixed32().hex() == activity_id, 'receipt belongs to another submitted activity')
    sequence = int.from_bytes(raw[offset:offset + 8], 'big')
    offset += 8
    previous, resulting, activity_root = fixed32(), fixed32(), fixed32()
    code = int.from_bytes(raw[offset:offset + 4], 'big', signed=True)
    require(sequence > 0 and code == 0 and previous != resulting
            and activity_root != bytes(32), 'kernel write did not produce a successful actual state change')
    return {'activity_id': activity_id, 'global_sequence': str(sequence), 'result_code': code,
            'receipt_sha256': hashlib.sha256(raw).hexdigest(),
            'previous_state_root': previous.hex(), 'resulting_state_root': resulting.hex()}


class Corpus:
    def __init__(self, fixture, daemon, evidence):
        self.fixture, self.daemon, self.evidence = fixture, daemon, evidence
        self.next_request = 100_000
        self.calls = 0
        self.tenants, self.probe = fixture.provision_tenant_readiness()
        self.outcomes = {}
        self.store = Path(daemon.config['LAYERX_AGENT_HUMAN_STORE']) / 'store.bin'
        require(self.fixture.directory in self.store.parents, 'recovery fault must remain disposable')

    def envelope(self, tenant, operation, request, mutating=False):
        self.next_request += 1
        result = {'version': 1, 'request_id': str(self.next_request), 'operation': operation,
                  'request': copy.deepcopy(request), 'credential': copy.deepcopy(tenant['credential'])}
        if mutating:
            result['idempotency_key'] = (tenant['prepare'].get('idempotency_key')
                if operation in ('prepare', 'submit') else hashlib.sha256(
                b'LayerX/tenant-readiness/case/v1\0' + str(os.getpid()).encode()
                + self.next_request.to_bytes(8, 'big')).hexdigest())
            require(isinstance(result['idempotency_key'], str)
                    and re.fullmatch('[0-9a-f]{64}', result['idempotency_key'])
                    and (operation not in ('prepare', 'submit') or result['idempotency_key'] ==
                         tenant['prepare']['request']['idempotency_key']),
                    'immutable native preparation and submission keys must match actual signed intent')
        return result

    def call(self, envelope):
        status, response = HTTP.rpc_request(self.daemon, envelope)
        self.calls += 1
        private_write(self.evidence / ('rpc-' + str(self.calls) + '.json'), {
            'operation': envelope['operation'], 'request_id': envelope['request_id'],
            'http_status': status, 'response': response})
        require(isinstance(response, dict) and response.get('request_id') == envelope['request_id'],
                'authenticated operation lost request identity')
        return status, response

    def success(self, envelope, verified=False):
        status, response = self.call(envelope)
        require(status == 200 and set(response) == {'request_id', 'value', 'verification_status'},
                'real authenticated operation refused: ' + envelope['operation'])
        if verified:
            value = response['verification_status']
            require(value.get('state') == 'achieved' and value.get('level') in (
                'StateProven', 'CheckpointFinalised', 'SettlementAnchored'),
                'actual authenticated operation did not achieve StateProven evidence')
        return response

    def readiness(self, tenant, admitted, reason=None):
        response = self.success(self.envelope(tenant, 'tenant.readiness', {}))
        require(response['verification_status'] == {'state': 'achieved', 'level': 'Unverified'},
                'local readiness projection claimed protocol verification for itself')
        value = response['value']
        require(isinstance(value, dict) and set(value) == {
            'transport_ready', 'verified_reads_ready', 'writes_admitted', 'recovery_reason'}
            and all(type(value[name]) is bool for name in (
                'transport_ready', 'verified_reads_ready', 'writes_admitted'))
            and value['transport_ready'] and value['verified_reads_ready']
            and value['writes_admitted'] is admitted
            and value['recovery_reason'] == reason
            and (reason is None or reason in REASONS)
            and admitted == (reason is None), 'actual tenant admission projection mismatch')
        return value

    def read(self, tenant):
        source = tenant['read']
        response = self.success(self.envelope(tenant, 'read.account', source['request']), verified=True)
        value = response['value']
        require(isinstance(value, dict) and value.get('account_id') == source['request']['account_id']
                and value.get('canonical_value') and value.get('proof')
                and int(value.get('sequence', '0')) > 0, 'read lacks actual bound account evidence')
        return {'account_id': value['account_id'], 'sequence': value['sequence'],
                'verification_status': response['verification_status']}

    def baseline(self):
        health_status, health_body, _ = HTTP.request(self.daemon.port, HTTP.health(self.daemon.config), 5)
        health = json.loads(health_body)
        require(health_status == 200 and health.get('transport_ready') is True
                and health.get('verified_reads_ready') is True and health.get('tenant_writes') == 'unknown'
                and 'writes_admitted' not in health,
                'transport health implied global tenant write admission')
        status, transport = HTTP.rpc_request(self.daemon)
        require(status == 200 and transport.get('transport_ready') is True
                and transport.get('verified_reads_ready') is None
                and transport.get('tenant_writes') == 'unknown',
                'RPC transport-only health invented verified reads or tenant write readiness')
        values = [self.readiness(tenant, True) for tenant in self.tenants]
        reads = [self.read(tenant) for tenant in self.tenants]
        require(reads[0]['account_id'] != reads[1]['account_id'], 'two different actual owner accounts required')
        foreign = self.envelope(self.tenants[0], 'tenant.readiness', {'tenant': self.tenants[1]['credential']['tenant']})
        status, refusal = self.call(foreign)
        require(status == 403 and refusal.get('class') == 'PolicyRefusal'
                and refusal.get('reason') == 'envelope.coordinate_mismatch',
                'tenant readiness accepted a caller-selected foreign tenant')
        unknown = self.envelope(self.tenants[0], 'tenant.readiness', {'undeclared_field': True})
        status, refusal = self.call(unknown)
        require(status == 400 and refusal.get('class') == 'ProtocolIncompatibility'
                and refusal.get('reason') == 'envelope.unknown_field',
                'closed readiness request accepted an undeclared field')
        invalid = self.envelope(self.tenants[0], 'tenant.readiness', {})
        invalid['credential']['token_id'] = ('1' if invalid['credential']['token_id'][0] == '0' else '0') + invalid['credential']['token_id'][1:]
        status, refusal = self.call(invalid)
        require(status == 401 and refusal.get('class') == 'PolicyRefusal'
                and refusal.get('reason') == 'session.not_authorized', 'unowned tenant status was disclosed')
        return {'readiness': values, 'verified_reads': reads, 'foreign_tenant_refused': True,
                'transport_health': transport, 'verified_read_health': health}

    def prepare(self):
        prepared = []
        for tenant in self.tenants:
            request = tenant['prepare']['request']
            response = self.success(self.envelope(tenant, 'prepare', request, True))['value']
            require(isinstance(response, dict) and response.get('version') == '1'
                    and response.get('activity') == request['activity']
                    and response.get('approval_required') is False and response.get('approval_id') is None
                    and re.fullmatch('[0-9a-f]{64}', response.get('preparation_id', ''))
                    and re.fullmatch('[0-9a-f]{64}', response.get('signing_preimage', ''))
                    and response.get('canonical_bytes')
                    and hashlib.sha256(bytes.fromhex(response['canonical_bytes'])).hexdigest() == response['preparation_id']
                    and request['purpose']['purpose']['canonical_digest'] == response['preparation_id'],
                    'real native preparation changed signed consent or requires unresolved approval')
            signature = tenant['signer'].sign(bytes.fromhex(response['signing_preimage'])).hex()
            tenant['submit'] = {'preparation_ref': response['preparation_id'], 'signature': signature,
                                'signer_public_key': tenant['public_key'], 'approval_release_ref': None}
            tenant['prepared'] = response
            prepared.append({'preparation_id': response['preparation_id'],
                             'canonical_digest': hashlib.sha256(bytes.fromhex(response['canonical_bytes'])).hexdigest()})
        return {'preparations': prepared}

    def inject(self):
        self.daemon.stop()
        tenant = self.tenants[0]['credential']['tenant'].encode()
        identifier = b'managed-agent-v1:' + self.tenants[0]['managed_agent_id'].encode()
        rows = [row for row in store_entries(self.store)
                if row[0] == tenant and row[1] == 12 and row[3] == identifier]
        require(len(rows) == 1 and rows[0][2] == 1 and len(rows[0][4]) > 32
                and rows[0][4][0] in (3, 4) and rows[0][4][-32:] != bytes(32),
                'actual assigned tenant budget owner is missing from provisioned durable state')
        self.fault_key = (tenant, 12, identifier)
        self.original_owner = rows[0][4]
        changed = bytearray(self.original_owner)
        changed[-1] ^= 1
        require(changed[-32:] != bytes(32), 'fault must name a missing nonzero budget rather than omit recovery')
        self.changed_owner = bytes(changed)
        replace_store_entry(self.store, self.fault_key, self.original_owner, self.changed_owner)
        self.daemon.start()
        failed = self.readiness(self.tenants[0], False, 'budget_state_unverified')
        healthy = self.readiness(self.tenants[1], True)
        reads = [self.read(tenant) for tenant in self.tenants]
        status, response = self.call(self.envelope(self.tenants[0], 'submit', self.tenants[0]['submit'], True))
        require(status == 403 and response.get('class') == 'PolicyRefusal'
                and response.get('reason') == 'owner.refused', 'failed tenant was admitted for a real signed write')
        return {'affected': failed, 'healthy': healthy, 'verified_reads': reads,
                'write_refusal': {'http_status': status, 'reason': response['reason']}}

    def write(self, index):
        tenant = self.tenants[index]
        response = self.success(self.envelope(tenant, 'submit', tenant['submit'], True))
        value = response['value']
        require(isinstance(value, dict) and re.fullmatch('[0-9a-f]{64}', value.get('activity_id', ''))
                and isinstance(value.get('submission'), dict)
                and isinstance(value['submission'].get('submission_ref'), str), 'actual submit outcome missing')
        activity = value['activity_id']
        reference = value['submission']['submission_ref']
        deadline = time.monotonic() + 180
        while value['submission'].get('state') != 'Executed':
            require(value['submission'].get('state') in ('Queued', 'Submitted', 'Acknowledged', 'Unknown')
                    and time.monotonic() < deadline, 'actual write did not reach a verified kernel receipt')
            time.sleep(0.25)
            response = self.success(self.envelope(tenant, 'track', {'submission_ref': reference}))
            value = response['value']
            require(value.get('activity_id') == activity, 'durable write changed activity identity')
        require(value['submission']['submission_ref'] == reference,
                'tracked actual write changed submission identity')
        verification = response['verification_status']
        receipt = value.get('receipt')
        require(verification.get('state') == 'achieved' and verification.get('level') in (
            'StateProven', 'CheckpointFinalised', 'SettlementAnchored')
            and isinstance(receipt, dict) and receipt.get('verification_level') in (
                'StateProven', 'CheckpointFinalised', 'SettlementAnchored'),
                'executed write lacks actual StateProven receipt evidence')
        facts = receipt_facts(receipt.get('canonical_bytes'), activity)
        self.outcomes[index] = {'activity_id': activity, 'submission_ref': reference, 'receipt': receipt,
                                'facts': facts}
        self.read(tenant)
        return facts

    def operator(self, tenant, journey, expected):
        credential = self.evidence / ('probe-credential-' + str(self.next_request) + '-' + journey + '.json')
        private_write(credential, tenant['credential'])
        command = ['sh', str(ROOT / 'platform/hosted/agentd/probe.sh'), '--url', self.probe['url'],
                   '--ca', self.probe['ca_seed'], '--client-cert', self.probe['client_cert_seed'],
                   '--client-key', self.probe['client_key_seed'], '--bearer-file', self.probe['bearer_seed'],
                   '--journey', journey, '--rpc-url', self.probe['rpc_url'],
                   '--gateway-key-file', self.probe['gateway_key_seed'], '--credential-file', str(credential)]
        log = self.evidence / (credential.stem + '.log')
        with log.open('xb') as output:
            code = subprocess.run(command, cwd=ROOT, stdout=output, stderr=subprocess.STDOUT,
                                  timeout=45).returncode
        require(code == expected, 'actual operator journey probe returned the wrong readiness outcome')
        if expected:
            require('tenant writes are not admitted: budget_state_unverified' in log.read_text(),
                    'write operator failed for an unrelated transport or authority reason')
        return {'journey': journey, 'exit_code': code, 'log': str(log)}

    def operator_cases(self):
        from urllib.parse import urlsplit, urlunsplit
        sys.path.insert(0, str(ROOT / 'agent/sdk/python'))
        from layerx_sdk.agent_http import AgentEnvelopeTransport, AgentSessionCredential, LayerXKeyCredential
        from layerx_sdk.production import SecretBytes, PlatformSdkError
        url = urlsplit(self.probe['rpc_url'])
        endpoint = urlunsplit((url.scheme, url.netloc, '', '', ''))
        encoded = Path(self.probe['gateway_key_seed']).read_text().removesuffix('\n')
        key_id, separator, secret = encoded.partition(':')
        require(separator and key_id and secret, 'actual registered SDK gateway key missing')
        values = []
        for index, tenant in enumerate(self.tenants):
            credential = tenant['credential']
            transport = AgentEnvelopeTransport(endpoint,
                gateway_key=LayerXKeyCredential(key_id, SecretBytes(secret.encode())),
                session=AgentSessionCredential(credential['tenant'], credential['session_id'],
                    credential['token_id'], int(credential['generation'])),
                ca_file=self.probe['ca_seed'], timeout=10)
            try:
                response = transport.tenant_readiness()
            except PlatformSdkError as error:
                raise HTTP.Failure('real Python SDK refused tenant readiness: ' + error.code.value) from None
            expected = self.readiness(tenant, index == 1,
                                      None if index == 1 else 'budget_state_unverified')
            require(response.value == expected
                    and response.verification_status == {'state': 'achieved', 'level': 'Unverified'},
                    'real Python SDK projected different requested tenant readiness')
            values.append(response.value)
        return {'python_sdk': values,
                'probes': [self.operator(self.tenants[0], 'read', 0),
                           self.operator(self.tenants[0], 'write', 1),
                           self.operator(self.tenants[1], 'write', 0)]}

    def restart_failure(self):
        self.daemon.stop()
        self.daemon.start()
        affected = self.readiness(self.tenants[0], False, 'budget_state_unverified')
        self.readiness(self.tenants[1], True)
        self.read(self.tenants[0])
        status, response = self.call(self.envelope(self.tenants[0], 'submit', self.tenants[0]['submit'], True))
        require(status == 403 and response.get('reason') == 'owner.refused',
                'restart admitted the affected tenant before durable recovery')
        self.retained(1)
        return {'affected': affected, 'write_refusal': response['reason']}

    def mcp(self):
        names = self.fixture.document['tenant_readiness'].get('mcp_binding_seeds')
        require(isinstance(names, list) and len(names) == 2
                and all(name in self.fixture.document['seeds'] for name in names),
                'missing genuine two-tenant MCP binding seeds with provisioned legacy capability, session and raw token')
        require('layerx-mcp' in self.fixture.artifacts,
                'missing source-bound actual layerx-mcp process artifact')
        results = []
        for index, name in enumerate(names):
            source = Path(self.fixture.values[name])
            binding = json.loads(source.read_text())
            def substitute(value):
                if isinstance(value, dict):
                    return {key: substitute(item) for key, item in value.items()}
                if isinstance(value, list):
                    return [substitute(item) for item in value]
                return value.format_map(self.fixture.values) if isinstance(value, str) else value
            binding = substitute(binding)
            credential = self.tenants[index]['credential']
            require(binding.get('tenant') == credential['tenant']
                    and binding.get('session_id') == credential['session_id']
                    and str(binding.get('session_generation')) == credential['generation']
                    and isinstance(binding.get('capability_id'), str)
                    and re.fullmatch('[0-9a-f]{64}', binding['capability_id'])
                    and binding['capability_id'] != '0' * 64,
                    'MCP binding must retain the provisioned session and real legacy capability')
            for raw in (binding.get('store'), binding.get('audit_root'), binding.get('session_token_file'),
                        binding.get('agent', {}).get('bearer_file'),
                        binding.get('agent', {}).get('readiness', {}).get('gateway_key_file'),
                        binding.get('listener', {}).get('socket')):
                require(isinstance(raw, str) and Path(raw).is_absolute()
                        and Path(raw).resolve() == Path(raw) and self.fixture.directory in Path(raw).parents,
                        'MCP process authority and mutable paths must remain in copied disposable state')
            token = Path(binding['session_token_file']).read_text().strip()
            require(token == credential['token_id'], 'MCP raw token is not the genuine borrowed credential')
            path = self.evidence / ('mcp-binding-' + str(index) + '.json')
            private_write(path, binding)
            log = self.evidence / ('mcp-' + str(index) + '.log')
            with log.open('xb') as output:
                process = subprocess.Popen([self.fixture.artifacts['layerx-mcp']['path'], str(path)],
                    cwd=self.fixture.directory, stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT)
                try:
                    endpoint = binding['listener']['socket']
                    deadline = time.monotonic() + 15
                    while not Path(endpoint).exists():
                        require(process.poll() is None and time.monotonic() < deadline,
                                'real MCP refused the provisioned binding or did not bind')
                        time.sleep(0.1)
                    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
                        connection.settimeout(15)
                        connection.connect(endpoint)
                        stream = connection.makefile('rwb')
                        for request in (
                            {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {}},
                            {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/call',
                             'params': {'name': 'tenant.readiness', 'arguments': {}}}):
                            stream.write(json.dumps(request).encode() + b'\n')
                            stream.flush()
                            response = json.loads(stream.readline(65537))
                            require(response.get('id') == request['id'] and 'error' not in response,
                                    'real MCP refused its authenticated readiness tool')
                        result = response['result']
                        require(result.get('isError') is False and len(result.get('content', [])) == 1,
                                'MCP readiness did not report a complete genuine owner response')
                        value = json.loads(result['content'][0]['text'])['result']
                        require(value.pop('verification_status') == {'state': 'achieved', 'level': 'unverified'}
                                and value == self.readiness(self.tenants[index], index == 1,
                                    None if index == 1 else 'budget_state_unverified'),
                                'MCP reported different requested tenant readiness')
                        results.append(value)
                finally:
                    process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)
        return {'readiness': results}

    def restore(self):
        self.daemon.stop()
        replace_store_entry(self.store, self.fault_key, self.changed_owner, self.original_owner)
        self.daemon.start()
        return {'readiness': [self.readiness(tenant, True) for tenant in self.tenants],
                'verified_reads': [self.read(tenant) for tenant in self.tenants]}

    def retained(self, index):
        expected = self.outcomes[index]
        response = self.success(self.envelope(self.tenants[index], 'track', {
            'submission_ref': expected['submission_ref']}), verified=True)
        actual = response['value']
        require(actual.get('activity_id') == expected['activity_id']
                and actual.get('submission', {}).get('state') == 'Executed'
                and actual.get('submission', {}).get('submission_ref') == expected['submission_ref']
                and actual.get('receipt') == expected['receipt'],
                'restart lost or changed the verified actual economic outcome')
        return expected['facts']

    def restart_outcomes(self):
        self.daemon.stop()
        self.daemon.start()
        return {'readiness': [self.readiness(tenant, True) for tenant in self.tenants],
                'verified_outcomes': [self.retained(index) for index in (0, 1)],
                'operator_write': self.operator(self.tenants[0], 'write', 0)}

    def logs(self):
        needles = [value.encode() for value in self.fixture.secret_values() if value]
        gateway = Path(self.probe['gateway_key_seed']).read_bytes().strip()
        needles.extend((gateway, gateway.partition(b':')[2]))
        for tenant in self.tenants:
            name = next(row['signer_seed'] for row in self.fixture.document['tenant_readiness']['tenants']
                        if row['signer_public_key'] == tenant['public_key'])
            needles.append(Path(self.fixture.values[name]).read_bytes().hex().encode())
        paths = list(self.evidence.glob('*.log')) + list(self.fixture.directory.glob('service-*.log'))
        for path in paths:
            data = path.read_bytes()
            require(not any(value in data for value in needles if value),
                    'service or operator log exposed protected material')
        return {'logs_checked': len(paths)}


def main():
    os.umask(0o077)
    record = {'command': COMMAND, 'revision': None, 'cases': [], 'exit_code': 1}
    fixture = daemon = evidence = None
    try:
        evidence = HTTP.evidence_dir() / ('tenant-readiness-' + str(time.time_ns()))
        evidence.mkdir(mode=0o700)
        head, dirty = HTTP.revision()
        record['revision'] = head
        require(not dirty, 'candidate source tree is dirty')
        manifest = HTTP.load_manifest(head)
        fixture = HTTP.AgentdFixture(evidence / 'fixture')
        config = HTTP.daemon_config(fixture)
        binary = HTTP.daemon_binary()
        require(binary.resolve() == Path(fixture.artifacts['layerx-agentd']['path']).resolve(),
                'built Agentd artifact is not the protected source-bound candidate executable')
        require('agent_program_port' in fixture.values,
                'actual routed program listener port must be in disposable fixture configuration')
        daemon = HTTP.Daemon(binary, config, evidence, int(fixture.values['agent_program_port']), fixture)
        corpus = Corpus(fixture, daemon, evidence)
        record['candidate_manifest'] = str(manifest)
        record['artifacts'] = fixture.artifacts
        record['evidence'] = str(evidence)
        record['cases'].append({'case': CASES[0], 'result': 'PASS'})
        daemon.start()
        operations = [corpus.baseline, corpus.prepare, corpus.inject, lambda: corpus.write(1),
                      corpus.operator_cases, corpus.mcp, corpus.restart_failure, corpus.restore,
                      lambda: corpus.write(0), corpus.restart_outcomes, corpus.logs]
        for name, action in zip(CASES[1:], operations):
            started = time.monotonic()
            try:
                result = action()
                require(daemon.alive(), 'actual Agentd process stopped during acceptance case')
                record['cases'].append({'case': name, 'result': 'PASS',
                    'elapsed_s': round(time.monotonic() - started, 3), 'evidence': result})
                print('CASE ' + name + ' PASS', flush=True)
            except (HTTP.Failure, FixtureRefused, OSError, ValueError, KeyError, TypeError,
                    subprocess.SubprocessError) as error:
                record['cases'].append({'case': name, 'result': 'FAIL',
                    'elapsed_s': round(time.monotonic() - started, 3),
                    'error': fixture.redact(str(error))})
                raise
        record['exit_code'] = 0
    except (HTTP.Failure, FixtureRefused, OSError, ValueError, KeyError, TypeError,
            ImportError, subprocess.SubprocessError) as error:
        message = fixture.redact(str(error)) if fixture is not None else str(error)
        record['error'] = message
        print('agent_tenant_readiness: ' + message, file=sys.stderr, flush=True)
    finally:
        if daemon is not None:
            try:
                daemon.stop()
            except (HTTP.Failure, OSError, subprocess.SubprocessError) as error:
                record['exit_code'] = 1
                record.setdefault('error', str(error))
        if fixture is not None:
            try:
                fixture.cleanup()
            except (OSError, FixtureRefused, subprocess.SubprocessError) as error:
                record['exit_code'] = 1
                record.setdefault('error', fixture.redact(str(error)))
    passed = sum(case['result'] == 'PASS' for case in record['cases'])
    skipped = len(CASES) - len(record['cases'])
    require(record['exit_code'] != 0 or (passed == len(CASES) and skipped == 0),
            'focused acceptance cases were skipped')
    if evidence is not None:
        result_path = evidence / 'result.json'
        private_write(result_path, record)
        print('agent_tenant_readiness: evidence ' + str(result_path), flush=True)
    print('PAXEER_X_GATE tests=' + str(len(record['cases'])) + ' skipped=' + str(skipped), flush=True)
    print('agent_tenant_readiness: revision=' + str(record['revision']) + ' command=' + repr(COMMAND)
          + ' exit_code=' + str(record['exit_code']), flush=True)
    return record['exit_code']


if __name__ == '__main__':
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
