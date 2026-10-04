#!/usr/bin/env python3
"""Paxeer X program-registry contract qualification.

Each case runs the registry's own tests through the production types and
persistence paths, and fails when a required input, execution or test case is
absent, failed or ignored.
"""
import concurrent.futures
import http.client
import ssl
import signal
import time
import uuid
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import shutil
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.candidate.v1'
RESULT = re.compile(r'^test (\S+) \.\.\. (\S+)$', re.M)

CASES = {
    'guest-abi-discovery': {'tests': {'native_interfaces::guest_abi_discovery': 'real native and served ABI discovery contract'}},
    'durable-verification-idempotency': {
        'tests': {
            'verified::tests::durable_verification_recovers_artifact_before_publication':
                'a completed build is durably prepared before artifact persistence and recovers without a rebuild before or after artifact persistence',
            'verified::tests::durable_verification_replays_after_restart_and_refuses_changed_requests':
                'a completed scope replays its recorded response after restart; a changed request is 409 and leaves the record unchanged',
            'verified::tests::durable_verification_isolates_principals_and_refuses_foreign_records':
                'principals own distinct scopes; a record bound to another principal fails closed',
            'verified::tests::durable_verification_recovers_a_crashed_rebuild_as_a_new_attempt':
                'a rebuild without a live owner recovers as the next attempt and stays bound to its request',
            'verified::tests::durable_verification_recovers_a_persisted_publication_without_duplicate_events':
                'a persisted verification recovers its publication without a rebuild or a second outbox event',
            'verified::tests::durable_verification_retention_preserves_live_identities':
                'retention evicts only settled identities and refuses new ones when every retained identity is live',
            'verified::tests::durable_verification_fails_closed_on_corrupt_uncertain_and_unavailable_storage':
                'corrupt and future-version records fail closed and stay intact; interrupted writes are preserved; unavailable storage refuses',
            'verified::tests::durable_verification_concurrent_worker_process_has_one_build_owner':
                'a second worker process cannot take the journal while the build owner holds it and then replays its response',
        },
    },
}


def fail(message):
    print('registry-contract: FAIL: ' + message, flush=True)
    sys.exit(1)


def manifest(path):
    if not path:
        fail('--candidate-manifest is required')
    try:
        data = Path(path).read_bytes()
        document = json.loads(data)
    except (OSError, ValueError) as error:
        fail('candidate manifest unreadable: ' + str(error))
    if not isinstance(document, dict) or document.get('schema') != SCHEMA:
        fail('candidate manifest schema is not ' + SCHEMA)
    return hashlib.sha256(data).hexdigest()


def run(name, artifacts):
    case = CASES[name]
    environment = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
    command = [artifacts['component_tests']['path'], 'durable_verification', '--test-threads=1']
    result = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True)
    sys.stdout.write(result.stdout)
    sys.stdout.write(result.stderr)
    executions = RESULT.findall(result.stdout)
    outcomes = dict(executions)
    require(len(executions) == len(outcomes), "duplicate component case execution")
    expected = case['tests']
    missing = sorted(set(expected) - set(outcomes))
    unexpected = sorted(set(outcomes) - set(expected))
    failed = sorted(test for test in expected if outcomes.get(test, 'ok') != 'ok')
    for test, assertion in expected.items():
        print('registry-contract: %s %s: %s' % (outcomes.get(test, 'absent'), test, assertion))
    if missing:
        fail('cases did not execute: ' + ', '.join(missing))
    if unexpected:
        fail('unexpected cases executed: ' + ', '.join(unexpected))
    if failed:
        fail('cases did not pass: ' + ', '.join(failed))
    if result.returncode:
        fail('%s exited %d' % (' '.join(command), result.returncode))
    return len(expected)


def artifact_manifest(candidate_digest):
    path = os.environ.get('PAXEER_X_REGISTRY_ARTIFACT_MANIFEST')
    if not path:
        fail('PAXEER_X_REGISTRY_ARTIFACT_MANIFEST is required')
    document = json.loads(Path(path).read_bytes())
    require(document.get('schema') == 'paxeer-x.registry-artifacts.v1', 'registry artifact schema')
    require(document.get('candidate_manifest_sha256') == candidate_digest, 'artifact candidate binding')
    paths = ['platform/hosted/registry/src/' + name + '.rs'
             for name in ('main', 'routes', 'verified', 'event_producer', 'lib')]
    paths += ['tools/qualification/paxeer-x/registry-contract.py', 'tools/paxeer-x/gates/16.2.sh']
    require(set(document['source_files']) == set(paths), 'exact artifact source inventory')
    for relative, digest in document['source_files'].items():
        require(hashlib.sha256((ROOT / relative).read_bytes()).hexdigest() == digest,
                'artifact source binding: ' + relative)
    for role in ('registry', 'component_tests'):
        entry = document[role]
        binary = Path(entry['path']).resolve(strict=True)
        require(binary.is_file() and os.access(binary, os.X_OK), role + ' executable')
        require(hashlib.sha256(binary.read_bytes()).hexdigest() == entry['sha256'], role + ' digest')
    return document


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


class ServedRegistry:
    def __init__(self, artifacts):
        self.binary = Path(artifacts['registry']['path']).resolve(strict=True)
        self.config = artifacts['served']
        self.root = Path(self.config['fixture_root']).resolve(strict=True)
        require((self.root / '.registry-qualification-fixture').is_file(), 'dedicated fixture marker')
        require(self.root != Path('/') and self.root != ROOT, 'isolated fixture root')
        self.environment = dict(os.environ)
        environment_file = Path(self.config['environment_file'])
        require(environment_file.stat().st_mode & 0o077 == 0, 'private fixture environment file')
        supplied = json.loads(environment_file.read_bytes())
        require(all(isinstance(k, str) and isinstance(v, str) for k, v in supplied.items()), 'environment values')
        self.environment.update(supplied)
        for name in ('LAYERX_REGISTRY_STATE', 'LAYERX_REGISTRY_JOURNAL',
                     'LAYERX_REGISTRY_VERIFIED', 'LAYERX_REGISTRY_BUILD_ROOT',
                     'LAYERX_REGISTRY_SOURCE_MIRROR'):
            require(name in supplied, 'explicit fixture path: ' + name)
            require(Path(supplied[name]).resolve().is_relative_to(self.root), 'fixture path containment: ' + name)
        self.journal = Path(supplied['LAYERX_REGISTRY_JOURNAL']) / 'verification-requests'
        self.verified = Path(supplied['LAYERX_REGISTRY_VERIFIED'])
        self.mirror = Path(supplied['LAYERX_REGISTRY_SOURCE_MIRROR'])
        self.host, port = supplied['LAYERX_REGISTRY_LISTEN'].rsplit(':', 1)
        require(self.host == '127.0.0.1', 'loopback qualification listener')
        self.port = int(port)
        self.context = ssl.create_default_context(cafile=self.config['client_ca_pem'])
        self.context.load_cert_chain(self.config['client_cert_pem'], self.config['client_key_pem'])
        self.token = Path(supplied['LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE']).read_text().strip()
        self.keys = [Path(path).read_text().strip() for path in self.config['principal_key_files']]
        require(len(self.keys) == 2 and self.keys[0] != self.keys[1], 'two independently provisioned principals')
        self.program = self.config['program_id']
        require(re.fullmatch('[0-9a-f]{64}', self.program) is not None, 'program identity')
        self.body = Path(self.config['source_request_file']).read_bytes()
        source = json.loads(self.body)
        require(re.fullmatch('[0-9a-f]{64}', source['source_digest']) is not None, 'source digest')
        self.archive = self.mirror / (source['source_digest'] + '.archive')
        require(self.archive.is_file(), 'real mirrored source archive')
        self.cgroup_parent = Path(self.config['cgroup_parent']).resolve(strict=True)
        require(str(self.cgroup_parent).startswith('/sys/fs/'), 'delegated cgroup parent')
        require((self.cgroup_parent / 'cgroup.procs').read_text().strip() == '', 'empty dedicated cgroup parent')
        self.process = None
        self.group = None
        self.logs = []

    def start(self):
        self.group = self.cgroup_parent / ('registry-contract-' + uuid.uuid4().hex)
        self.group.mkdir()
        log = self.root / ('registry-' + uuid.uuid4().hex + '.log')
        output = log.open('xb')
        os.chmod(log, 0o600)
        self.logs.append(str(log))
        group = self.group
        def attach():
            (group / 'cgroup.procs').write_text(str(os.getpid()))
        self.process = subprocess.Popen([str(self.binary)], env=self.environment,
                                        stdin=subprocess.DEVNULL, stdout=output, stderr=output,
                                        preexec_fn=attach)
        output.close()
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            require(self.process.poll() is None, 'served registry exited; log: ' + str(log))
            try:
                status, _ = self.request('GET', '/healthz', b'', authenticated=False)
                if status == 200:
                    return
            except (OSError, ssl.SSLError, http.client.HTTPException):
                pass
            time.sleep(0.1)
        raise RuntimeError('served registry readiness deadline; log: ' + str(log))

    def stop(self):
        if self.group is not None and self.group.exists():
            (self.group / 'cgroup.kill').write_text('1')
        if self.process is not None:
            self.process.wait(timeout=10)
            self.process = None
        if self.group is not None:
            for _ in range(100):
                directories = sorted((p for p in self.group.rglob('*') if p.is_dir()),
                                     key=lambda p: len(p.parts), reverse=True)
                for directory in directories:
                    try:
                        directory.rmdir()
                    except OSError:
                        pass
                try:
                    self.group.rmdir()
                    self.group = None
                    break
                except OSError:
                    time.sleep(0.01)
            require(self.group is None, 'registry cgroup did not quiesce')

    def request(self, method, path, body, key=None, principal=0, authenticated=True):
        connection = http.client.HTTPSConnection(self.host, self.port, context=self.context, timeout=180)
        headers = {'Content-Type': 'application/json', 'Connection': 'close'}
        if authenticated:
            headers['Authorization'] = 'Bearer ' + self.token
            headers['LayerX-Key'] = self.keys[principal]
        if key:
            headers['Idempotency-Key'] = key
        try:
            connection.request(method, path, body=body, headers=headers)
            response = connection.getresponse()
            return response.status, response.read()
        finally:
            connection.close()

    def post(self, key, body=None, principal=0, authenticated=True):
        return self.request('POST', '/v1/programs/registry/' + self.program + '/source',
                            self.body if body is None else body, key, principal, authenticated)

    def crash_at_phase(self, key, phase):
        before = set(self.records())
        artifact_times = {p.name: p.stat().st_mtime_ns for p in self.verified.glob('*.verified')}
        connection = http.client.HTTPSConnection(self.host, self.port, context=self.context, timeout=180)
        connection.putrequest('POST', '/v1/programs/registry/' + self.program + '/source')
        connection.putheader('Authorization', 'Bearer ' + self.token)
        connection.putheader('LayerX-Key', self.keys[0])
        connection.putheader('Idempotency-Key', key)
        connection.putheader('Content-Type', 'application/json')
        connection.putheader('Content-Length', str(len(self.body)))
        connection.putheader('Connection', 'close')
        connection.endheaders(self.body)
        try:
            deadline = time.monotonic() + 180
            while time.monotonic() < deadline:
                current = self.records()
                for name in set(current) - before:
                    record = current[name][1]
                    if record['state']['phase'] != phase:
                        if record['state']['phase'] == 'completed':
                            raise RuntimeError('required crash phase was not observed: ' + phase)
                        continue
                    if phase == 'building':
                        building = any('/builds/' in str(path) and path.read_text().strip()
                                       for path in self.group.rglob('cgroup.procs'))
                        if not building:
                            continue
                    if phase == 'artifact':
                        persisted = any(p.stat().st_mtime_ns != artifact_times.get(p.name)
                                        for p in self.verified.glob('*.verified'))
                        if not persisted:
                            continue
                    (self.group / 'cgroup.freeze').write_text('1')
                    frozen_deadline = time.monotonic() + 5
                    while 'frozen 1' not in (self.group / 'cgroup.events').read_text():
                        require(time.monotonic() < frozen_deadline, 'crash boundary freeze deadline')
                        time.sleep(0.001)
                    frozen = self.records()[name][1]
                    if frozen['state']['phase'] != phase:
                        (self.group / 'cgroup.freeze').write_text('0')
                        continue
                    self.stop()
                    return name, frozen
                time.sleep(0.0005)
            raise RuntimeError('required production crash boundary absent: ' + phase)
        finally:
            connection.close()

    def records(self):
        return {p.name: (p.read_bytes(), json.loads(p.read_bytes())) for p in self.journal.glob('*.request')}

    def snapshot(self):
        return {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in self.verified.glob('*.verified')}


def served_cases(artifacts):
    server = ServedRegistry(artifacts)
    count = 0
    prefix = uuid.uuid4().hex
    try:
        server.start()
        before_reservation = server.records()
        server.stop()
        server.start()
        require(server.records() == before_reservation, "crash before reservation preserves journal")
        before = server.records()
        first = server.post(prefix + '-replay')
        require(first[0] == 200, 'real source build must complete successfully')
        records = server.records()
        added = set(records) - set(before)
        require(len(added) == 1, 'one durable reservation for completed served request')
        record_name = next(iter(added))
        raw, record = records[record_name]
        require(record['state']['phase'] == 'completed' and record['attempt'] == 1,
                'served terminal response durably bound before acknowledgment')
        require(record['state']['response']['status'] == first[0]
                and record['state']['response']['body'].encode() == first[1], 'recorded exact served response')
        archived = server.archive.with_suffix('.qualification-held')
        require(not archived.exists(), 'archive hold path unused')
        server.archive.rename(archived)
        try:
            require(server.post(prefix + '-replay') == first, 'later HTTP worker replay without source rebuild')
            server.stop()
            server.start()
            require(server.post(prefix + '-replay') == first, 'registry restart replay without source rebuild')
            require(server.records()[record_name][0] == raw, 'replay must not alter request identity or attempts')
        finally:
            archived.rename(server.archive)
        print('registry-contract: served ok durable response across workers and registry restart without build')
        count += 1
        changed = json.loads(server.body)
        changed['source_uri'] += '/changed'
        snapshot = server.snapshot()
        require(server.post(prefix + '-replay', json.dumps(changed).encode())[0] == 409, 'changed request 409')
        require(server.snapshot() == snapshot and server.records()[record_name][0] == raw,
                'conflict must preserve verified state and recorded response')
        require(server.post(prefix + '-unauthorized', authenticated=False)[0] in (401, 403), 'unauthenticated source refusal')
        require(set(server.records()) == set(records), 'unauthenticated request cannot reserve identity')
        print('registry-contract: served ok conflict integrity and authentication refusal')
        count += 1
        before = server.records()
        other = server.post(prefix + '-replay', principal=1)
        require(other[0] == 200, 'second authenticated principal must not collide')
        after = server.records()
        new_names = set(after) - set(before)
        require(len(new_names) == 1, 'distinct principal reservation')
        other_record = after[next(iter(new_names))][1]
        require(other_record['principal'] != record['principal'], 'distinct authenticated principal binding')
        require(server.post(prefix + '-replay', principal=1) == other, 'second principal own recorded response')
        print('registry-contract: served ok principal isolation and independent publication')
        count += 1
        before = server.records()
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            replies = list(pool.map(lambda _: server.post(prefix + '-concurrent'), range(2)))
        require(replies[0][0] == 200 and replies[0] == replies[1], 'identical concurrent served responses')
        after = server.records()
        new_names = set(after) - set(before)
        require(len(new_names) == 1, 'concurrent requests share one reservation')
        require(after[next(iter(new_names))][1]['attempt'] == 1, 'one concurrent build owner')
        print('registry-contract: served ok concurrent single build ownership')
        count += 1
        for phase in ('building', 'artifact', 'completed'):
            crash_key = prefix + '-crash-' + phase
            recovered_name, interrupted = server.crash_at_phase(crash_key, phase)
            if phase != 'building':
                archived = server.archive.with_suffix('.qualification-held')
                require(not archived.exists(), 'archive recovery hold unused')
                server.archive.rename(archived)
            try:
                server.start()
                recovered = server.post(crash_key)
                require(recovered[0] == 200, 'crash recovery completion: ' + phase)
                restored = server.records()[recovered_name][1]
                require(restored['state']['phase'] == 'completed', 'durable recovered completion: ' + phase)
                expected_attempt = interrupted['attempt'] + (1 if phase == 'building' else 0)
                require(restored['attempt'] == expected_attempt, 'bounded build recovery ownership: ' + phase)
                if phase != 'building':
                    require(restored['state']['response'] == interrupted['state']['response'],
                            'exact artifact or pre-acknowledgment response recovery: ' + phase)
            finally:
                if phase != 'building':
                    archived.rename(server.archive)
            print('registry-contract: served ok process crash recovery at ' + phase)
            count += 1
        target = server.journal / record_name
        target.write_bytes(b'{"version":')
        try:
            require(server.post(prefix + '-replay')[0] == 503, 'corrupt request must fail closed')
            require(target.read_bytes() == b'{"version":', 'corruption evidence must be preserved')
            require(server.snapshot() == snapshot, 'corrupt identity cannot change verified sources')
        finally:
            target.write_bytes(raw)
        server.stop()
        blocked = server.journal.with_name('verification-requests-held')
        require(not blocked.exists(), 'journal hold path unused')
        server.journal.rename(blocked)
        server.journal.write_bytes(b'unavailable')
        try:
            try:
                server.start()
            except RuntimeError:
                require(server.process is not None and server.process.poll() is not None
                        and server.process.returncode != 0, 'unavailable storage requires nonzero startup refusal')
            else:
                raise RuntimeError('unavailable journal unexpectedly admitted listener')
        finally:
            server.stop()
            server.journal.unlink()
            blocked.rename(server.journal)
        print('registry-contract: served ok corrupt and unavailable storage fail closed')
        count += 1
        return count
    finally:
        server.stop()
        for path in server.logs:
            print('registry-contract: private served log ' + path)



DISCOVERY_SUFFIXES = (
    'deploy', 'upgrade', 'interface', 'discovery', 'account-state', 'restart',
    'bad-record', 'bad-interface', 'stale-head', 'foreign-signer',
    'bad-membership', 'unknown-abi', 'downgrade',
)
DISCOVERY_CASES = {f'abi{abi}-{suffix}' for abi in range(1, 5)
                   for suffix in DISCOVERY_SUFFIXES}
DISCOVERY_CASES |= {'source-unpublished', 'source-mismatch', 'source-verified',
                    'remote-attestation'}


def discovery_artifacts(candidate_digest):
    path = os.environ.get('PAXEER_X_REGISTRY_ARTIFACT_MANIFEST')
    require(path, 'PAXEER_X_REGISTRY_ARTIFACT_MANIFEST is required')
    document = json.loads(Path(path).read_bytes())
    require(document.get('schema') == 'paxeer-x.registry-discovery-artifacts.v1',
            'distinct registry discovery artifact contract required')
    require(document.get('candidate_manifest_sha256') == candidate_digest,
            'discovery artifact candidate binding')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    tree = subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], cwd=ROOT, text=True).strip()
    require(document.get('source_revision') == revision and document.get('source_tree') == tree,
            'immutable discovery source binding')
    require(not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT),
            'clean published discovery source required')
    paths = {
        'programs/crates/layerx-programs-registry/src/protocol_evidence.rs',
        'platform/hosted/registry/src/routes.rs',
        'platform/hosted/registry/src/node_state.rs',
        'platform/hosted/registry/src/head_attestation.rs',
        'platform/hosted/registry/src/program_state.rs',
        'platform/emulator/core/emulator_core.c',
        'platform/emulator/src/main.rs',
        'platform/hosted/registry/tests/native_deployment.rs',
        'tools/qualification/paxeer-x/registry-contract.py',
        'cmd/layerxd/lxp_daemon_protocol.c',
        'platform/hosted/agent-boundary/src/main.rs',
        'platform/hosted/agent-boundary/tests/real_node/native_interfaces.rs',
    }
    require(set(document['source_files']) == paths, 'exact discovery source inventory')
    for relative, expected in document['source_files'].items():
        require(hashlib.sha256((ROOT / relative).read_bytes()).hexdigest() == expected,
                'discovery source digest: ' + relative)
    roles = {'registry', 'emulator', 'registry-tests', 'real-node-tests', 'boundary', 'layerxd',
             'genesis-builder', 'cli', 'sign-credit', 'test-credit', 'anvil', 'forge'}
    require(set(document['artifacts']) == roles, 'genuine discovery process artifacts required')
    for role, entry in document['artifacts'].items():
        binary = Path(entry['path']).resolve(strict=True)
        require(binary.is_file() and os.access(binary, os.X_OK), role + ' executable')
        require(hashlib.sha256(binary.read_bytes()).hexdigest() == entry['sha256'], role + ' digest')
    return document


def discovery_publication(artifacts):
    server = ServedRegistry({'registry': artifacts['artifacts']['registry'],
                             'served': artifacts['served']})
    expected_environment = json.loads(Path(server.config['environment_file']).read_bytes())
    request_token = Path(expected_environment['LAYERX_REGISTRY_REQUEST_TOKEN_FILE']).read_text().strip()
    require(request_token and server.token and request_token != server.token,
            'source publication has a distinct actual operator authority')
    route = '/v1/programs/registry/' + server.program
    prefix = 'discovery-' + uuid.uuid4().hex
    mismatch = Path(server.config['mismatch_source_request_file']).resolve(strict=True).read_bytes()
    mismatch_document = json.loads(mismatch)
    require(set(mismatch_document) == {'source_uri', 'source_digest'}
            and mismatch_document['source_digest'] != json.loads(server.body)['source_digest'],
            'genuine distinct reproducible mismatch archive input')
    mismatch_archive = server.mirror / (mismatch_document['source_digest'] + '.archive')
    require(mismatch_archive.is_file(), 'actual mirrored mismatch source archive')
    def read_source():
        status, body = server.request('GET', route, b'')
        require(status == 200, 'receipt-verified source discovery read required')
        document = json.loads(body)
        require(document['verification'] == 'registry-receipt-and-current-head-verified',
                'source never substitutes for deployment receipt verification')
        return document['versions'][-1]['source']
    try:
        server.start()
        require(read_source()['status'] == 'unpublished', 'genuine initial unpublished source state')
        print('REGISTRY_DISCOVERY_CASE source-unpublished', flush=True)
        status, body = server.post(prefix + '-operator-refused', authenticated=False)
        require(status in (401, 403), 'unauthenticated source publication refusal')
        saved_token = server.token
        server.token = request_token
        try:
            require(server.post(prefix + '-request-authority-refused')[0] in (401, 403),
                    'request authority cannot publish source')
        finally:
            server.token = saved_token
        status, body = server.post(prefix + '-mismatch', mismatch)
        require(status == 409 and json.loads(body)['error']['code'] == 'source_mismatch',
                'actual builder artifact mismatch refusal')
        require(read_source()['status'] == 'mismatch', 'explicit mismatch discovery state')
        print('REGISTRY_DISCOVERY_CASE source-mismatch', flush=True)
        status, body = server.post(prefix + '-verified')
        require(status == 200, 'actual pinned reproducible builder success')
        verified = read_source()
        require(verified['status'] == 'verified'
                and verified['source_digest'] == json.loads(server.body)['source_digest']
                and re.fullmatch('[0-9a-f]{64}', verified['environment_digest']) is not None,
                'exact source and pinned environment publication provenance')
        server.stop()
        server.start()
        require(read_source() == verified, 'source provenance survives actual registry restart')
        print('REGISTRY_DISCOVERY_CASE source-verified', flush=True)
    finally:
        server.stop()
    return 3


def discovery_contract(artifacts):
    environment_file = Path(artifacts['environment_file']).resolve(strict=True)
    require(environment_file.stat().st_mode & 0o077 == 0,
            'private genuine discovery fixture environment')
    configured = json.loads(environment_file.read_bytes())
    require(isinstance(configured, dict) and all(isinstance(k, str) and isinstance(v, str)
                                               for k, v in configured.items()),
            'discovery environment values')
    require('PAXEER_X_REGISTRY_CONFIGURATION' in configured
            and 'PAXEER_X_REGISTRY_CGROUP_PARENT' in configured,
            'real pinned builder and delegated fixture cgroup required')
    environment = dict(os.environ, **configured)
    binaries = artifacts['artifacts']
    environment['PAXEER_X_REGISTRY_BINARY'] = binaries['registry']['path']
    environment['PAXEER_X_REGISTRY_EMULATOR_BINARY'] = binaries['emulator']['path']
    environment['PAXEER_X_NATIVE_INTERFACE_BOUNDARY'] = binaries['boundary']['path']
    environment['PYTHONDONTWRITEBYTECODE'] = '1'
    native = Path(binaries['layerxd']['path']).resolve(strict=True).parent
    require(Path(binaries['genesis-builder']['path']).resolve(strict=True) == native / 'layerx-genesis-build',
            'actual native fixture builder binding')
    require(Path(binaries['layerxd']['path']).resolve(strict=True).name == 'layerxd',
            'actual native fixture daemon binding')
    environment['LAYERX_TEST_NATIVE_BIN_DIR'] = str(native)
    require(Path(binaries['cli']['path']).resolve(strict=True)
            == Path(binaries['emulator']['path']).resolve(strict=True),
            'emulator is the actual layerx CLI library owner')
    for role in ('sign-credit', 'test-credit'):
        require((ROOT / 'build/tests/bridge' / role).resolve(strict=True)
                == Path(binaries[role]['path']).resolve(strict=True),
                'actual custody fixture artifact binding: ' + role)
    tool_directories = [str(Path(binaries[role]['path']).resolve(strict=True).parent)
                        for role in ('anvil', 'forge')]
    environment['PATH'] = os.pathsep.join(tool_directories + [environment.get('PATH', '/usr/bin:/bin')])
    for role in ('anvil', 'forge'):
        resolved = shutil.which(role, path=environment['PATH'])
        require(resolved is not None and Path(resolved).resolve(strict=True)
                == Path(binaries[role]['path']).resolve(strict=True),
                'actual funding tool invocation binding: ' + role)
    root = Path(artifacts['fixtures_root']).resolve(strict=True)
    require(root != ROOT and root != Path('/')
            and (root / '.registry-qualification-fixture').is_file(),
            'dedicated genuine discovery fixture root')
    output = root / ('guest-abi-discovery-' + uuid.uuid4().hex)
    output.mkdir(mode=0o700)
    environment['PAXEER_X_NATIVE_INTERFACE_EVIDENCE'] = str(output)
    command = [binaries['real-node-tests']['path'], '--exact',
               'native_interfaces::guest_abi_discovery', '--nocapture', '--test-threads=1']
    result = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True)
    sys.stdout.write(result.stdout)
    sys.stdout.write(result.stderr)
    executions = re.findall(r'^test (\S+) \.\.\.', result.stdout, re.M)
    summaries = re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;',
                           result.stdout, re.M)
    require(executions == ['native_interfaces::guest_abi_discovery']
            and summaries == [('1', '0', '0')],
            'the genuine focused discovery test must execute once and pass')
    markers = re.findall(r'^REGISTRY_DISCOVERY_CASE ([A-Za-z0-9_-]+)$', result.stdout, re.M)
    require(len(markers) == len(set(markers)) and set(markers) == DISCOVERY_CASES - {'source-unpublished', 'source-mismatch', 'source-verified'},
            'discovery cases absent, duplicated, unexpected or skipped')
    require(result.returncode == 0, 'genuine discovery process exited ' + str(result.returncode))
    capture_command = [binaries['registry-tests']['path'], '--exact',
                       'guest_abi_discovery_captured_native_proofs_preserve_authority',
                       '--nocapture', '--test-threads=1']
    captured = subprocess.run(capture_command, cwd=ROOT, env=environment,
                              stdin=subprocess.DEVNULL, capture_output=True, text=True)
    sys.stdout.write(captured.stdout)
    sys.stdout.write(captured.stderr)
    require(captured.returncode == 0
            and re.findall(r'^test (\S+) \.\.\.', captured.stdout, re.M)
                == ['guest_abi_discovery_captured_native_proofs_preserve_authority']
            and re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;',
                           captured.stdout, re.M) == [('1', '0', '0')],
            'actual native capture proof contract must execute once and pass')
    publication_count = discovery_publication(artifacts)
    for role, entry in binaries.items():
        require(hashlib.sha256(Path(entry['path']).read_bytes()).hexdigest() == entry['sha256'],
                'discovery artifact changed during execution: ' + role)
    print('registry-contract: private discovery evidence ' + str(output), flush=True)
    return len(markers) + publication_count + 1

def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--case', required=True, choices=sorted(CASES))
    parser.add_argument('--candidate-manifest', required=True)
    arguments = parser.parse_args()
    digest = manifest(arguments.candidate_manifest)
    print('registry-contract: candidate manifest sha256 ' + digest)
    if arguments.case == 'guest-abi-discovery':
        count = discovery_contract(discovery_artifacts(digest))
    else:
        artifacts = artifact_manifest(digest)
        count = run(arguments.case, artifacts)
        count += served_cases(artifacts)
    print('registry-contract: case %s cases=%d passed=%d' % (arguments.case, count, count))


def interrupted(signum, frame):
    raise RuntimeError('qualification interrupted by signal ' + str(signum))


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        main()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.TimeoutExpired) as error:
        fail(str(error))
