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
import socket
import threading
import time
import uuid
from urllib.parse import urlsplit
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
    'registry-readiness': {'tests': {}},
    'emulator-conformance': {'tests': {'conformance_verifies_actual_signed_observations': 'canonical production receipt verification and comparator refusals'}},
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
        log = self.spawn()
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

    def spawn(self, environment=None):
        self.group = self.cgroup_parent / ('registry-contract-' + uuid.uuid4().hex)
        self.group.mkdir()
        log = self.root / ('registry-' + uuid.uuid4().hex + '.log')
        output = log.open('xb')
        os.chmod(log, 0o600)
        self.logs.append(str(log))
        group = self.group
        def attach():
            (group / 'cgroup.procs').write_text(str(os.getpid()))
        self.process = subprocess.Popen([str(self.binary)],
                                        env=self.environment if environment is None else environment,
                                        stdin=subprocess.DEVNULL, stdout=output, stderr=output,
                                        preexec_fn=attach)
        output.close()
        return log

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


READINESS_CASES = (
    'transport-separate', 'mtls-required', 'credential-safe', 'builder-mutation',
    'dependency-loss-program-events', 'dependency-loss-webhooks-events', 'dependency-loss-node-authority',
    'restart-recovery', 'operation-record', 'wrong-trust-material', 'replica-identity-mismatch',
    'missing-material-distinct-credentials', 'missing-material-entrypoint', 'recovery-after-refusal',
)
READINESS_DEPENDENCIES = {
    'program-events': 'LAYERX_EVENTS_PROGRAM_UPSTREAM_URL',
    'webhooks-events': 'LAYERX_EVENTS_WEBHOOKS_UPSTREAM_URL',
    'node-authority': 'LAYERX_REGISTRY_NODE_ENDPOINT',
}
# Declared withdrawal and recovery bounds in seconds: the builder monitor
# withdraws within its 2 s freshness; event delivery within the 10 s admission
# freshness, the 5 s admission deadline and the 30 s delivery failure window;
# node authority on the next verdict, each plus one 10 s probe period.
READINESS_BOUNDS = {'builder': 3, 'program-events': 55, 'webhooks-events': 55, 'node-authority': 20}
READINESS_STARTUP = 120
READINESS_REFUSAL_WINDOW = 30
READINESS_SOURCES = {
    'platform/hosted/registry/src/main.rs',
    'platform/hosted/registry/src/routes.rs',
    'platform/hosted/registry/src/event_producer.rs',
    'platform/hosted/registry/fly.toml',
    'platform/hosted/registry/deployment.yaml',
    'platform/hosted/registry/tests/readiness.py',
    'docker/platform-registry/init.sh',
    'tools/qualification/paxeer-x/registry-contract.py',
}


class Relay:
    """Owned TCP transport between the registry and one real dependency."""

    def __init__(self, listen, upstream):
        host, port = listen.rsplit(':', 1)
        require(host == '127.0.0.1', 'loopback dependency relay')
        self.address = (host, int(port))
        upstream_host, upstream_port = upstream.rsplit(':', 1)
        self.upstream = (upstream_host, int(upstream_port))
        require(self.upstream != self.address, 'relay forwards to a distinct real dependency')
        self.server = None
        self.sockets = set()
        self.lock = threading.Lock()

    def open(self):
        require(self.server is None, 'relay opened once')
        self.server = socket.create_server(self.address)
        threading.Thread(target=self.accept, args=(self.server,), daemon=True).start()

    def accept(self, server):
        while True:
            try:
                client, _ = server.accept()
            except OSError:
                return
            try:
                upstream = socket.create_connection(self.upstream, timeout=10)
                upstream.settimeout(None)
            except OSError:
                client.close()
                continue
            with self.lock:
                if self.server is not server:
                    client.close()
                    upstream.close()
                    return
                self.sockets |= {client, upstream}
            for source, target in ((client, upstream), (upstream, client)):
                threading.Thread(target=self.pump, args=(source, target), daemon=True).start()

    @staticmethod
    def pump(source, target):
        try:
            while True:
                data = source.recv(65536)
                if not data:
                    break
                target.sendall(data)
        except OSError:
            pass
        finally:
            for item in (source, target):
                try:
                    item.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass

    def close(self):
        with self.lock:
            server, self.server = self.server, None
            sockets, self.sockets = self.sockets, set()
        if server is not None:
            server.close()
        for item in sockets:
            try:
                item.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            item.close()


def readiness_artifacts(candidate_digest):
    path = os.environ.get('PAXEER_X_REGISTRY_ARTIFACT_MANIFEST')
    require(path, 'PAXEER_X_REGISTRY_ARTIFACT_MANIFEST is required')
    private = Path(path).resolve(strict=True)
    require(private.is_file() and private.stat().st_mode & 0o077 == 0, 'private readiness artifact manifest')
    document = strict_json(private.read_bytes())
    exact_fields(document, {'schema', 'candidate_manifest_sha256', 'source_revision', 'source_tree',
                            'source_files', 'artifacts', 'image', 'served'}, 'readiness artifact manifest')
    require(document['schema'] == 'paxeer-x.registry-readiness-artifacts.v1', 'registry readiness artifact schema')
    require(document['candidate_manifest_sha256'] == candidate_digest, 'readiness artifact candidate binding')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    tree = subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], cwd=ROOT, text=True).strip()
    require(document['source_revision'] == revision and document['source_tree'] == tree,
            'immutable readiness source binding')
    require(not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT),
            'clean published readiness source required')
    require(isinstance(document['source_files'], dict) and set(document['source_files']) == READINESS_SOURCES,
            'exact readiness source inventory')
    for relative, digest in document['source_files'].items():
        require(hashlib.sha256((ROOT / relative).read_bytes()).hexdigest() == digest,
                'readiness source binding: ' + relative)
    require(isinstance(document['artifacts'], dict) and set(document['artifacts']) == {'registry'},
            'served registry artifact required')
    for role, entry in document['artifacts'].items():
        exact_fields(entry, {'path', 'sha256'}, 'served artifact')
        binary = Path(entry['path']).resolve(strict=True)
        require(binary.is_file() and os.access(binary, os.X_OK), role + ' executable')
        require(hashlib.sha256(binary.read_bytes()).hexdigest() == entry['sha256'], role + ' digest')
    exact_fields(document['image'], {'reference', 'digest'}, 'registry image identity')
    require(isinstance(document['image']['reference'], str) and document['image']['reference']
            and re.fullmatch('sha256:[0-9a-f]{64}', document['image']['digest'] or '') is not None,
            'pinned registry image identity')
    served = document['served']
    for field in ('health_listen', 'wrong_trust_history', 'deployment_request_file', 'dependencies', 'gateway'):
        require(field in served, 'readiness fixture input: ' + field)
    require(isinstance(served['dependencies'], dict) and set(served['dependencies']) == set(READINESS_DEPENDENCIES),
            'every required readiness dependency relayed')
    exact_fields(served['gateway'], {'url', 'ca_pem', 'client_cert_pem', 'client_key_pem'}, 'gateway readiness reader')
    return document


def readiness_contract(artifacts, candidate_digest):
    served = artifacts['served']
    server = ServedRegistry({'registry': artifacts['artifacts']['registry'], 'served': served})
    environment = server.environment
    require(environment.get('LAYERX_REGISTRY_HEALTH_LISTEN') == served['health_listen'],
            'served readiness listener configured')
    health_host, health_port = served['health_listen'].rsplit(':', 1)
    health_port = int(health_port)
    require(health_host == '127.0.0.1' and health_port != server.port, 'separate loopback readiness listener')
    request_token = Path(environment['LAYERX_REGISTRY_REQUEST_TOKEN_FILE']).read_text().strip()
    require(request_token and request_token != server.token, 'distinct request and publication credentials')
    rootfs = Path(environment['LAYERX_REGISTRY_BUILDER_ENVIRONMENT_ROOT']).resolve(strict=True)
    require(rootfs.is_relative_to(server.root), 'fixture-owned pinned builder rootfs')
    trust = Path(environment['LAYERX_REGISTRY_SEQUENCER_TRUST_HISTORY']).resolve(strict=True)
    wrong = Path(served['wrong_trust_history']).resolve(strict=True)
    require(wrong.is_relative_to(server.root) and wrong.read_bytes() != trust.read_bytes(),
            'distinct real wrong sequencer trust material')
    replica = environment['LAYERX_REGISTRY_RECEIPT_AUTHORITY_REPLICA_ID']
    require(re.fullmatch('[0-9a-f]{64}', replica) is not None, 'configured replica identity')
    deployment = Path(served['deployment_request_file']).resolve(strict=True).read_bytes()
    relays = {}
    for name, variable in READINESS_DEPENDENCIES.items():
        entry = served['dependencies'][name]
        exact_fields(entry, {'relay_listen', 'upstream'}, 'dependency relay')
        relays[name] = Relay(entry['relay_listen'], entry['upstream'])
        target = urlsplit(environment[variable])
        require(target.port == relays[name].address[1]
                and socket.gethostbyname(target.hostname) == '127.0.0.1',
                'dependency routed through the qualification relay: ' + name)
    secrets = [server.token.encode(), request_token.encode()] + [key.encode() for key in server.keys]
    markers = []

    def mark(name, detail):
        print('REGISTRY_READINESS_CASE %s %s' % (name, detail), flush=True)
        markers.append(name)

    def verdict(method='GET', path='/healthz', headers=None):
        connection = http.client.HTTPConnection(health_host, health_port, timeout=12)
        try:
            connection.request(method, path, headers=dict(headers or {}, Connection='close'))
            response = connection.getresponse()
            body = response.read(65537)
            require(len(body) <= 65536, 'bounded readiness answer')
            return response.status, body
        finally:
            connection.close()

    def authenticated():
        return server.request('GET', '/healthz', b'', authenticated=False)

    def code(answer):
        document = strict_json(answer[1])
        require(isinstance(document.get('error'), dict) and isinstance(document['error'].get('code'), str),
                'typed refusal')
        return document['error']['code']

    def await_verdict(status, bound, label):
        started = time.monotonic()
        deadline = started + bound
        while True:
            require(server.process is not None and server.process.poll() is None,
                    'registry exited while awaiting ' + label)
            try:
                answer = verdict()
                if answer[0] == status:
                    return answer, time.monotonic() - started
            except (OSError, http.client.HTTPException):
                pass
            require(time.monotonic() < deadline, '%s not observed within %ss' % (label, bound))
            time.sleep(0.1)

    def refused_or_waiting(name, overrides):
        changed = dict(environment)
        for key, value in overrides.items():
            if value is None:
                changed.pop(key, None)
            else:
                changed[key] = value
        server.spawn(changed)
        deadline = time.monotonic() + READINESS_REFUSAL_WINDOW
        outcome = 'waiting'
        while time.monotonic() < deadline:
            if server.process.poll() is not None:
                require(server.process.returncode != 0, name + ' exited zero')
                outcome = 'refused exit=%d' % server.process.returncode
                break
            for probe in (verdict, authenticated):
                try:
                    require(probe()[0] != 200, name + ' admitted serving readiness')
                except (OSError, ssl.SSLError, http.client.HTTPException):
                    pass
            time.sleep(0.2)
        server.stop()
        mark(name, outcome)

    for relay in relays.values():
        relay.open()
    try:
        server.start()
        ready, elapsed = await_verdict(200, READINESS_STARTUP, 'startup readiness')
        require(ready == authenticated(), 'readiness port answers the exact mTLS /healthz verdict')
        require(strict_json(ready[1]) == {'status': 'ready', 'service': 'program-registry'}, 'ready verdict body')
        with socket.create_connection((server.host, server.port), timeout=5):
            pass
        plain = http.client.HTTPConnection(server.host, server.port, timeout=5)
        try:
            plain.request('GET', '/healthz', headers={'Connection': 'close'})
            answered = plain.getresponse().status
        except (OSError, http.client.HTTPException):
            answered = None
        finally:
            plain.close()
        require(answered is None, 'mTLS listener answered plaintext HTTP')
        mark('transport-separate', 'tcp=open readiness=200 equal=mtls elapsed=%.2f' % elapsed)
        anonymous = ssl.create_default_context(cafile=server.config['client_ca_pem'])
        connection = http.client.HTTPSConnection(server.host, server.port, context=anonymous, timeout=10)
        try:
            connection.request('GET', '/healthz', headers={'Connection': 'close'})
            admitted = connection.getresponse().status
        except (OSError, ssl.SSLError, http.client.HTTPException):
            admitted = None
        finally:
            connection.close()
        require(admitted is None, 'mTLS listener admitted a client without a certificate')
        mark('mtls-required', 'certificateless=refused')
        for method, path in (('GET', '/v1/programs/registry'), ('GET', '/metrics'), ('POST', '/__registry/sources'),
                             ('POST', '/__registry/deployments'), ('POST', '/healthz'), ('GET', '/')):
            status, body = verdict(method, path, {'Authorization': 'Bearer ' + request_token})
            require(status in (404, 405), 'readiness listener served ' + method + ' ' + path)
            require(not any(secret in body for secret in secrets), 'readiness answer exposes a credential')
        require(verdict(headers={'Authorization': 'Bearer forged'}) == verdict(), 'readiness ignores credentials')
        require(not any(secret in verdict()[1] for secret in secrets), 'ready verdict exposes a credential')
        mark('credential-safe', 'other-routes=404/405 secrets=absent')
        changed = next(path for path in sorted(rootfs.rglob('*'))
                       if path.is_file() and not path.is_symlink() and path.stat().st_size)
        original = changed.read_bytes()
        try:
            changed.write_bytes(bytes([original[0] ^ 1]) + original[1:])
            withdrawn, elapsed = await_verdict(503, READINESS_BOUNDS['builder'], 'builder mutation withdrawal')
            require(code(withdrawn) == 'builder_unavailable' and authenticated()[0] == 503,
                    'typed builder withdrawal on both listeners')
            saved = server.token
            server.token = request_token
            try:
                refused = server.post('readiness-' + uuid.uuid4().hex)
            finally:
                server.token = saved
            require(refused[0] == 503 and code(refused) == 'builder_unavailable', 'typed request refusal while mutated')
        finally:
            changed.write_bytes(original)
        _, recovered = await_verdict(200, READINESS_BOUNDS['builder'], 'builder reverification')
        require(authenticated()[0] == 200, 'mTLS verdict resumed after reverification')
        mark('builder-mutation', 'withdrawn=%.2fs recovered=%.2fs refusal=builder_unavailable' % (elapsed, recovered))
        for name, relay in relays.items():
            relay.close()
            try:
                withdrawn, elapsed = await_verdict(503, READINESS_BOUNDS[name], name + ' loss withdrawal')
                typed = code(withdrawn)
                mtls = authenticated()
                require(mtls[0] == 503 and code(mtls) == typed, 'typed mTLS refusal during ' + name + ' loss')
            finally:
                relay.open()
            _, recovered = await_verdict(200, READINESS_BOUNDS[name], name + ' recovery')
            mark('dependency-loss-' + name, 'withdrawn=%.2fs code=%s recovered=%.2fs' % (elapsed, typed, recovered))
        server.stop()
        try:
            verdict()
            raise RuntimeError('readiness answered while the registry was stopped')
        except (OSError, http.client.HTTPException):
            pass
        server.start()
        restarted, elapsed = await_verdict(200, READINESS_STARTUP, 'restart readiness')
        require(restarted == authenticated(), 'restart resumes the exact verified verdict')
        mark('restart-recovery', 'stopped=unanswered ready=%.2fs' % elapsed)
        saved = server.token
        server.token = request_token
        try:
            deploy = server.request('POST', '/__registry/deployments', deployment, key='readiness-' + uuid.uuid4().hex)
            discovery = server.request('GET', '/v1/programs/registry/' + server.program, b'')
        finally:
            server.token = saved
        require(deploy[0] == 200, 'real signed deploy admitted')
        require(discovery[0] == 200 and strict_json(discovery[1]).get('verification')
                == 'registry-receipt-and-current-head-verified', 'receipt-verified discovery read')
        gateway = served['gateway']
        target = urlsplit(gateway['url'])
        require(target.scheme == 'https' and target.hostname and not target.username, 'gateway readiness origin')
        context = ssl.create_default_context(cafile=gateway['ca_pem'])
        context.load_cert_chain(gateway['client_cert_pem'], gateway['client_key_pem'])
        connection = http.client.HTTPSConnection(target.hostname, target.port or 443, context=context, timeout=20)
        try:
            connection.request('GET', target.path.rstrip('/') + '/readyz', headers={'Connection': 'close'})
            response = connection.getresponse()
            gateway_answer = (response.status, response.read(65537))
        finally:
            connection.close()
        components = strict_json(gateway_answer[1]).get('components') or {}
        require(gateway_answer[0] == 200 and components.get('program_registry') == 'ready',
                'gateway program_registry readiness')
        record = {
            'schema': 'paxeer-x.registry-readiness-operation.v1',
            'candidate_manifest_sha256': candidate_digest,
            'source_revision': artifacts['source_revision'],
            'image': artifacts['image'],
            'registry_sha256': artifacts['artifacts']['registry']['sha256'],
            'builder_environment_digest': environment['LAYERX_REGISTRY_BUILDER_IMAGE_DIGEST'],
            'deploy': {'request_sha256': hashlib.sha256(deployment).hexdigest(), 'status': deploy[0],
                       'response_sha256': hashlib.sha256(deploy[1]).hexdigest()},
            'discovery': {'program_id': server.program, 'status': discovery[0],
                          'verification': 'registry-receipt-and-current-head-verified',
                          'response_sha256': hashlib.sha256(discovery[1]).hexdigest()},
            'gateway': {'url': gateway['url'], 'status': gateway_answer[0], 'program_registry': 'ready'},
            'readiness': {'status': restarted[0], 'response_sha256': hashlib.sha256(restarted[1]).hexdigest()},
        }
        evidence = server.root / ('registry-readiness-operation-' + uuid.uuid4().hex + '.json')
        with evidence.open('x') as output:
            os.chmod(evidence, 0o600)
            json.dump(record, output, sort_keys=True)
        mark('operation-record', str(evidence))
        server.stop()
        flipped = '%064x' % (int(replica, 16) ^ 1)
        refused_or_waiting('wrong-trust-material', {'LAYERX_REGISTRY_SEQUENCER_TRUST_HISTORY': str(wrong)})
        refused_or_waiting('replica-identity-mismatch', {'LAYERX_REGISTRY_RECEIPT_AUTHORITY_REPLICA_ID': flipped})
        refused_or_waiting('missing-material-distinct-credentials', {
            'LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE': environment['LAYERX_REGISTRY_REQUEST_TOKEN_FILE']})
        refused_or_waiting('missing-material-entrypoint', {'LAYERX_REGISTRY_BUILDER_ENTRYPOINT': None})
        server.start()
        resumed, elapsed = await_verdict(200, READINESS_STARTUP, 'recovery after refusal')
        require(resumed == authenticated(), 'verified material resumes readiness')
        mark('recovery-after-refusal', 'ready=%.2fs' % elapsed)
    finally:
        server.stop()
        for relay in relays.values():
            relay.close()
        for path in server.logs:
            print('registry-contract: private served log ' + path)
    require(tuple(markers) == READINESS_CASES, 'readiness cases absent, duplicated or reordered')
    for role, entry in artifacts['artifacts'].items():
        require(hashlib.sha256(Path(entry['path']).read_bytes()).hexdigest() == entry['sha256'],
                'readiness artifact changed during execution: ' + role)
    return len(markers)


CONFORMANCE_CASES = {f'abi{abi}-{kind}' for abi in range(1, 5)
                     for kind in ('execute', 'refuse', 'replay', 'restart')}
CONFORMANCE_CASES |= {'malformed-envelope', 'invalid-signature', 'unknown-abi', 'forbidden-downgrade'}


def exact_fields(value, fields, label):
    require(isinstance(value, dict) and set(value) == set(fields), label + ' closed fields')


def strict_json(raw):
    def unique(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, 'duplicate JSON field: ' + key)
            value[key] = item
        return value
    return json.loads(raw, object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(ValueError('nonfinite JSON value')))


def conformance_corpus(path):
    data = Path(path).read_bytes()
    require(0 < len(data) <= 4 * 1024 * 1024, 'nonempty bounded conformance corpus')
    value = strict_json(data)
    exact_fields(value, {'schema', 'network_id', 'protocol_version', 'case_count', 'initial_state',
                         'normalization', 'cases'}, 'conformance corpus')
    require(value['schema'] == 'layerx.emulator-conformance.corpus.v1', 'conformance corpus version')
    require(type(value['network_id']) is int and 0 < value['network_id'] <= 0xffffffff
            and value['protocol_version'] == 3, 'explicit native network/protocol')
    require(isinstance(value['initial_state'], dict) and value['initial_state'], 'declared known initial state')
    exact_fields(value['normalization'], {'state', 'error', 'receipt'}, 'normalization')
    allowed = {'state': {'network_mode', 'batch_cadence'}, 'error': {'trace_id'}, 'receipt': {'sequencer_signature'}}
    for kind, entries in value['normalization'].items():
        require(isinstance(entries, dict) and set(entries) <= allowed[kind]
                and all(isinstance(reason, str) and reason.strip() for reason in entries.values()),
                'only explicitly justified environment metadata normalization')
    require(type(value['case_count']) is int and value['case_count'] == len(CONFORMANCE_CASES)
            and isinstance(value['cases'], list) and len(value['cases']) == value['case_count'],
            'exact required case count')
    require(set(value['normalization']['receipt']) == {'sequencer_signature'},
            'explicit justification for independently verified environment signatures')
    stages = {
        'decode': {'LXP_ERR_TRUNCATED', 'LXP_ERR_TRAILING_BYTES', 'LXP_ERR_NON_CANONICAL', 'LXP_ERR_MALFORMED_ENVELOPE', 'malformed_activity', 'non_canonical_activity', 'invalid_argument'},
        'signature': {'LXP_ERR_BAD_SIGNATURE', 'bad_signature'},
        'protocol': {'LXP_ERR_VERSION_UNSUPPORTED', 'LXP_ERR_WRONG_NETWORK', 'version_unsupported', 'wrong_network'},
    }
    names = set()
    for row in value['cases']:
        exact_fields(row, {'id', 'kind', 'guest_abi', 'activity', 'idempotency_key', 'expected', 'replay_of'}, 'case')
        require(isinstance(row['id'], str) and row['id'] in CONFORMANCE_CASES and row['id'] not in names, 'unique required case identity')
        names.add(row['id'])
        require(row['kind'] in ('execute', 'refuse', 'replay', 'restart', 'rejection'), 'declared case kind')
        require(type(row['guest_abi']) is int and row['guest_abi'] in (0, 1, 2, 3, 4), 'declared ABI')
        if row['id'].startswith('abi'):
            abi, kind = row['id'].split('-')
            require(row['guest_abi'] == int(abi[3:]) and row['kind'] == kind, 'ABI/kind coverage binding')
        else:
            require(row['guest_abi'] == 0 and row['kind'] in ('rejection', 'refuse'), 'declared negative case')
        require(isinstance(row['activity'], str) and re.fullmatch('(?:[0-9a-f]{2})+', row['activity']) is not None
                and len(row['activity']) <= 2 * 1024 * 1024, 'actual bounded canonical activity bytes')
        require(isinstance(row['idempotency_key'], str) and re.fullmatch('[A-Za-z0-9_-]{16,128}', row['idempotency_key']) is not None,
                'declared bounded idempotency key')
        exact_fields(row['expected'], {'status', 'result_code', 'error_code', 'stage', 'observation'}, 'expected outcome')
        expected = row['expected']
        if row['kind'] == 'rejection':
            require(expected['status'] in (400, 409, 422) and expected['result_code'] is None
                    and isinstance(expected['error_code'], str) and expected['error_code']
                    and expected['stage'] in stages and expected['error_code'] in stages[expected['stage']]
                    and expected['observation'] is None, 'explicit protocol refusal, never authentication/transport')
        else:
            require(expected['status'] == 200 and type(expected['result_code']) is int
                    and expected['error_code'] is None and expected['stage'] == 'receipt'
                    and isinstance(expected['observation'], dict) and expected['observation'],
                    'declared actual signed receipt observation')
            require((expected['result_code'] < 0) == (row['kind'] == 'refuse'), 'positive/negative execution taxonomy')
        if row['kind'] in ('replay', 'restart'):
            require(isinstance(row['replay_of'], str) and row['replay_of'] in names and row['replay_of'] != row['id'], 'prior successful original request')
            original = next(item for item in value['cases'] if item['id'] == row['replay_of'])
            require(original['kind'] == 'execute' and original['guest_abi'] == row['guest_abi']
                    and original['activity'] == row['activity'] and original['idempotency_key'] == row['idempotency_key'],
                    'resume exact original canonical activity and request identity')
        else:
            require(row['replay_of'] is None, 'non-replay has no original reference')
    require(names == CONFORMANCE_CASES, 'all supported ABIs and declared negatives required')
    exact_fields(value['initial_state'], {'root', 'sequence'}, 'known initial state')
    require(isinstance(value['initial_state']['root'], str) and re.fullmatch('[0-9a-f]{64}', value['initial_state']['root']) is not None
            and type(value['initial_state']['sequence']) is int and value['initial_state']['sequence'] > 0, 'genuine known head')
    return value


def conformance_artifacts(candidate_digest):
    path = os.environ.get('PAXEER_X_EMULATOR_CONFORMANCE_ARTIFACTS')
    require(path, 'missing genuine equivalent-state emulator/hosted fixture: PAXEER_X_EMULATOR_CONFORMANCE_ARTIFACTS')
    private = Path(path).resolve(strict=True)
    require(private.is_file() and private.stat().st_mode & 0o077 == 0, 'private conformance artifact manifest')
    value = strict_json(private.read_bytes())
    exact_fields(value, {'schema', 'candidate_manifest_sha256', 'source_revision', 'source_files', 'artifacts',
                         'fixture_root', 'environments', 'sequencer_public_keys', 'sequencer_authorizations', 'initialization', 'corpus_file'}, 'genuine conformance artifact manifest')
    require(value['schema'] == 'paxeer-x.emulator-conformance-artifacts.v1'
            and value['candidate_manifest_sha256'] == candidate_digest, 'conformance candidate binding')
    require(value['source_revision'] == subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
            and not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT), 'clean immutable conformance candidate')
    paths = {'platform/emulator/tests/conformance.sh', 'platform/emulator/tests/gateway.rs',
             'platform/emulator/tests/authority.rs', 'tools/qualification/paxeer-x/registry-contract.py',
             'platform/emulator/src/main.rs', 'platform/emulator/core/emulator_core.c',
             'platform/hosted/agent-boundary/src/main.rs'}
    require(isinstance(value['source_files'], dict) and set(value['source_files']) == paths, 'exact conformance source inventory')
    for name, digest in value['source_files'].items():
        require(hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == digest, 'conformance source binding: ' + name)
    require(isinstance(value['artifacts'], dict) and set(value['artifacts']) == {'emulator', 'hosted', 'gateway-tests', 'authority-tests'}, 'real prebuilt conformance artifacts')
    for name, row in value['artifacts'].items():
        exact_fields(row, {'path', 'sha256'}, 'actual prebuilt artifact')
        binary = Path(row['path']).resolve(strict=True)
        require(binary.is_file() and os.access(binary, os.X_OK)
                and hashlib.sha256(binary.read_bytes()).hexdigest() == row['sha256'], 'artifact identity: ' + name)
    require(isinstance(value['environments'], dict) and set(value['environments']) == {'emulator', 'hosted'}
            and isinstance(value['sequencer_public_keys'], dict) and set(value['sequencer_public_keys']) == {'emulator', 'hosted'}, 'two actual environment and authority bindings')
    for key in value['sequencer_public_keys'].values():
        require(isinstance(key, str) and re.fullmatch('[0-9a-f]{64}', key) is not None and key != '0' * 64, 'actual independently provisioned sequencer public key')
    exact_fields(value['sequencer_authorizations'], {'emulator', 'hosted'}, 'independent sequencer authorization inventory')
    for name, authorization in value['sequencer_authorizations'].items():
        exact_fields(authorization, {'sequencer_id', 'public_key', 'first_batch', 'last_batch'}, 'independently pinned sequencer authorization')
        require(isinstance(authorization['sequencer_id'], str)
                and re.fullmatch('[0-9a-f]{64}', authorization['sequencer_id']) is not None
                and authorization['sequencer_id'] != '0' * 64
                and authorization['public_key'] == value['sequencer_public_keys'][name]
                and type(authorization['first_batch']) is int and type(authorization['last_batch']) is int
                and 0 < authorization['first_batch'] <= authorization['last_batch'] <= 0xffffffffffffffff,
                'actual independent sequencer identity, key and bounded range')
    require(isinstance(value['initialization'], list), 'actual deterministic initialization activities')
    root = Path(value['fixture_root']).resolve(strict=True)
    require(root != ROOT and not root.is_relative_to(ROOT) and root.stat().st_mode & 0o077 == 0
            and (root / '.emulator-conformance-fixture').is_file(), 'dedicated genuine fixture root')
    return value


class ConformanceProcess:
    def __init__(self, name, artifacts, directory):
        self.name = name
        self.config = artifacts['environments'][name]
        self.binary = artifacts['artifacts'][name]['path']
        self.directory = directory
        self.url = urlsplit(self.config['url'])
        require(self.url.scheme in ('http', 'https') and self.url.hostname == '127.0.0.1'
                and self.url.port and self.url.path in ('', '/') and not self.url.query
                and not self.url.username and not self.url.password, 'private loopback test environment')
        environment_file = Path(self.config['environment_file']).resolve(strict=True)
        require(not environment_file.name.startswith('.env') and environment_file.stat().st_mode & 0o077 == 0,
                'private disposable fixture environment JSON')
        configured = strict_json(environment_file.read_bytes())
        require(isinstance(configured, dict) and all(isinstance(k, str) and isinstance(v, str)
                for k, v in configured.items()), 'actual fixture environment values')
        self.environment = {key: item for key, item in os.environ.items() if not key.startswith('LAYERX_')}
        self.environment.update(configured)
        fixture = Path(artifacts['fixture_root']).resolve(strict=True)
        if name == 'hosted':
            for key in ('LAYERX_AGENT_BOUNDARY_STATE_DIR', 'LAYERX_AGENT_BOUNDARY_LNI_SOCKET'):
                require(key in configured and Path(configured[key]).resolve().is_relative_to(fixture), 'dedicated actual hosted state/socket')
        self.arguments = self.config['arguments']
        require(isinstance(self.arguments, list) and all(isinstance(item, str) for item in self.arguments), 'actual production process arguments')
        require(name != 'emulator' or self.arguments[:2] == ['emulator', 'up'], 'real CLI emulator owner')
        self.token = Path(self.config['token_file']).read_text().strip() if self.config['token_file'] else None
        self.context = ssl.create_default_context(cafile=self.config['ca_file']) if self.url.scheme == 'https' else None
        if self.context is not None and self.config.get('client_cert_file') is not None:
            self.context.load_cert_chain(self.config['client_cert_file'], self.config['client_key_file'])
        self.process = None
        self.log = None

    def start(self):
        self.log = (self.directory / (self.name + '-' + uuid.uuid4().hex + '.log')).open('xb')
        os.chmod(self.log.name, 0o600)
        self.process = subprocess.Popen([self.binary, *self.arguments], cwd=self.directory,
                                        env=self.environment, stdin=subprocess.DEVNULL,
                                        stdout=self.log, stderr=self.log, start_new_session=True)
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            require(self.process.poll() is None, 'actual ' + self.name + ' startup refusal; private log ' + self.log.name)
            try:
                status, body = self.request('GET', '/healthz' if self.name == 'emulator' else '/readyz', b'')
                if status == 200:
                    document = strict_json(body)
                    result = document.get('result', document)
                    require(result.get('status') == 'ready' if self.name == 'emulator' else result.get('ready') is True, 'real readiness verdict')
                    return
            except (OSError, http.client.HTTPException):
                pass
            time.sleep(0.1)
        raise RuntimeError(self.name + ' readiness deadline')

    def stop(self):
        if self.process is not None:
            if self.process.poll() is None:
                os.killpg(self.process.pid, signal.SIGTERM)
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(self.process.pid, signal.SIGKILL)
                    self.process.wait(timeout=5)
            self.process = None
        if self.log is not None:
            self.log.close()
            self.log = None

    def request(self, method, path, body, content_type='application/json', key=None):
        require(path.startswith('/') and not path.startswith('//'), 'local production route')
        connection = (http.client.HTTPSConnection(self.url.hostname, self.url.port, context=self.context, timeout=20)
                      if self.context else http.client.HTTPConnection(self.url.hostname, self.url.port, timeout=20))
        headers = {'Content-Type': content_type, 'Connection': 'close'}
        if self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        if key:
            headers['Idempotency-Key'] = key
        try:
            connection.request(method, path, body=body, headers=headers)
            response = connection.getresponse()
            payload = response.read(4 * 1024 * 1024 + 1)
            require(len(payload) <= 4 * 1024 * 1024, 'bounded actual response')
            require(response.status not in (401, 403, 429, 500, 502, 503, 504), 'transport/authentication/unavailable is never protocol parity')
            return response.status, payload
        finally:
            connection.close()

    def state(self, case):
        if self.name == 'emulator':
            status, body = self.request('GET', '/v1/protocol/account-state/head', b'')
            require(status == 200, 'actual retained emulator maintenance head must exist')
            document = strict_json(body)['result']
        else:
            address = urlsplit(self.config['state_url'])
            require(address.scheme == 'http' and address.hostname == '127.0.0.1' and address.port
                    and address.path == '/v1/protocol/account-state/head' and not address.query
                    and not address.username and not address.password, 'actual private native state-head producer')
            connection = http.client.HTTPConnection(address.hostname, address.port, timeout=20)
            try:
                connection.request('GET', address.path, headers={'Connection': 'close'})
                response = connection.getresponse()
                raw = response.read(4 * 1024 * 1024 + 1)
                require(response.status == 200 and len(raw) <= 4 * 1024 * 1024, 'real native state-head collection')
                document = strict_json(raw)
            finally:
                connection.close()
        require(document['current'] is True, 'fresh native head observation')
        root, sequence, receipt = document['state_root'], document['observed_sequence'], document['receipt_hex']
        require(re.fullmatch('[0-9a-f]{64}', root) is not None and type(sequence) is int and sequence > 0
                and re.fullmatch('(?:[0-9a-f]{2})+', receipt) is not None, 'canonical signed head observation')
        evidence = document['batch_evidence']
        require(isinstance(evidence, dict)
                and all(isinstance(evidence.get(field), str)
                        and re.fullmatch('(?:[0-9a-f]{2})+', evidence[field]) is not None
                        for field in ('header_hex', 'header_signature', 'receipt_proof_hex')),
                'actual retained signed head header and receipt inclusion proof')
        return {'root': root, 'sequence': sequence, 'head': document}

    def restart(self, previous):
        snapshot = None
        if self.name == 'emulator':
            status, snapshot = self.request('GET', '/__emulator/snapshot', b'')
            require(status == 200 and snapshot, 'real authenticated emulator recovery snapshot')
            path = self.directory / ('recovery-' + uuid.uuid4().hex + '.snapshot')
            path.write_bytes(snapshot)
            os.chmod(path, 0o600)
        before = self.state(previous)
        self.stop()
        self.start()
        if snapshot is not None:
            status, body = self.request('PUT', '/__emulator/snapshot', snapshot, 'application/octet-stream')
            require(status == 200 and strict_json(body)['result']['imported'] is True, 'actual snapshot restore')
        require(self.state(previous) == before, 'real process restart preserves exact signed state')


def conformance_invalid_corpora(corpus, directory):
    valid = json.dumps(corpus, separators=(',', ':')).encode()
    mutations = {'empty': b'', 'comment-only': b'# no activity cases\n', 'malformed-json': b'{'}
    for name in ('wrong-count', 'missing-abi', 'duplicate-case', 'unknown-abi', 'unjustified-normalization'):
        changed = strict_json(valid)
        if name == 'wrong-count':
            changed['case_count'] += 1
        elif name == 'missing-abi':
            changed['cases'].pop()
        elif name == 'duplicate-case':
            changed['cases'][-1] = changed['cases'][0]
        elif name == 'unknown-abi':
            changed['cases'][0]['guest_abi'] = 5
        else:
            changed['normalization']['state']['root'] = 'forbidden removal of committed state'
        mutations[name] = json.dumps(changed, separators=(',', ':')).encode()
    for name, raw in mutations.items():
        path = directory / ('invalid-' + name + '.json')
        path.write_bytes(raw)
        os.chmod(path, 0o600)
        try:
            conformance_corpus(path)
        except (RuntimeError, ValueError):
            print('EMULATOR_CONFORMANCE_INPUT_CASE ' + name, flush=True)
        else:
            raise RuntimeError('invalid corpus admitted: ' + name)
    return len(mutations)


def conformance_contract(artifacts, corpus):
    root = Path(artifacts['fixture_root'])
    directory = root / ('conformance-' + uuid.uuid4().hex)
    directory.mkdir(mode=0o700)
    input_count = conformance_invalid_corpora(corpus, directory)
    servers = {name: ConformanceProcess(name, artifacts, directory) for name in ('emulator', 'hosted')}
    observations = []
    previous = {}
    try:
        for server in servers.values():
            server.start()
        for initialization in artifacts['initialization']:
            exact_fields(initialization, {'activity', 'idempotency_key'}, 'genuine deterministic initialization')
            require(re.fullmatch('(?:[0-9a-f]{2})+', initialization['activity']) is not None, 'actual signed initialization activity')
            for server in servers.values():
                status, body = server.request('POST', '/v1/activities', bytes.fromhex(initialization['activity']),
                                              'application/octet-stream', initialization['idempotency_key'])
                require(status == 200 and strict_json(body)['result']['state'] == 'completed', 'real deterministic fixture initialization')
        for name, server in servers.items():
            state = server.state('initial')
            require({key: state[key] for key in ('root', 'sequence')} == corpus['initial_state'], 'equivalent declared initial state: ' + name)
        prior_case = 'initial'
        for row in corpus['cases']:
            observation = {'id': row['id'], 'expected': row['expected'], 'guest_abi': row['guest_abi'], 'kind': row['kind'], 'activity': row['activity']}
            for name, server in servers.items():
                if row['kind'] == 'restart':
                    server.restart(prior_case)
                before = server.state(prior_case)
                request = bytes.fromhex(row['activity'])
                status, raw = server.request('POST', '/v1/activities', request, 'application/octet-stream', key=row['idempotency_key'])
                body = strict_json(raw)
                require(isinstance(body, dict) and status == row['expected']['status'], 'actual expected operation/refusal status')
                after = server.state(row['id'])
                state = dict(after)
                if row['kind'] == 'rejection':
                    require(before == after, 'preexecution refusal preserves actual state')
                    error = body.get('error')
                    require(isinstance(error, dict) and error.get('code') == row['expected']['error_code'], 'exact protocol error taxonomy')
                    error = dict(error)
                    for field in corpus['normalization']['error']:
                        error.pop(field, None)
                    outcome = {'status': status, 'error': error, 'stage': row['expected']['stage'], 'state': state, 'before': before}
                else:
                    result = body.get('result')
                    require(isinstance(result, dict) and isinstance(result.get('receipt'), str), 'actual canonical receipt execution')
                    receipt = result['receipt']
                    require(re.fullmatch('(?:[0-9a-f]{2})+', receipt), 'lowerhex actual receipt')
                    outcome = {'status': status, 'receipt': receipt, 'state': state, 'before': before}
                outcome['receipt_before'] = (previous[(name, row['replay_of'])]['receipt_before']
                                             if row['kind'] in ('replay', 'restart') else before)
                if row['kind'] in ('replay', 'restart'):
                    original = previous[(name, row['replay_of'])]
                    require({key: item for key, item in outcome.items() if key != 'before'}
                            == {key: item for key, item in original.items() if key != 'before'}, 'exact original canonical replay after real restart')
                previous[(name, row['id'])] = outcome
                observation[name] = outcome
            observations.append(observation)
            prior_case = row['id']
        capture = {'schema': 'layerx.emulator-conformance.observations.v1', 'network_id': corpus['network_id'],
                   'protocol_version': corpus['protocol_version'], 'initial_state': corpus['initial_state'], 'pins': artifacts['sequencer_public_keys'],
                   'authorizations': artifacts['sequencer_authorizations'], 'cases': observations}
        path = directory / 'observations.json'
        path.write_text(json.dumps(capture, separators=(',', ':')))
        os.chmod(path, 0o600)
        environment = dict(os.environ, PAXEER_X_CONFORMANCE_OBSERVATIONS=str(path))
        binary = artifacts['artifacts']['authority-tests']['path']
        command = [binary, '--exact', 'conformance_verifies_actual_signed_observations', '--nocapture', '--test-threads=1']
        result = subprocess.run(command, cwd=ROOT, env=environment, capture_output=True, text=True, timeout=120)
        sys.stdout.write(result.stdout)
        sys.stdout.write(result.stderr)
        require(re.findall(r'^test (\S+) \.\.\.', result.stdout, re.M)
                == ['conformance_verifies_actual_signed_observations'], 'exact production comparator execution')
        require(result.returncode == 0 and re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', result.stdout, re.M) == [('1', '0', '0')],
                'actual canonical verifier/comparator must execute once without skips')
        markers = re.findall(r'^EMULATOR_CONFORMANCE_CASE ([A-Za-z0-9_-]+)$', result.stdout, re.M)
        require(len(markers) == len(set(markers)) and set(markers) == CONFORMANCE_CASES,
                'all required parity cases actually verified')
        require(re.findall(r'^EMULATOR_CONFORMANCE_COMPARATOR ([a-z-]+)$', result.stdout, re.M)
                == ['different-state'], 'actual comparator failure cases required')
        require(re.findall(r'^EMULATOR_CONFORMANCE_HEAD ([a-z]+)$', result.stdout, re.M)
                == ['emulator', 'hosted'], 'real canonical head proof and independent authorization refusals')
        gateway = subprocess.run([artifacts['artifacts']['gateway-tests']['path'], '--exact',
                                  'conformance_gateway_collects_real_receipts_and_rejects_transport_lookalikes',
                                  '--nocapture', '--test-threads=1'], cwd=ROOT, env=environment,
                                 capture_output=True, text=True, timeout=120)
        sys.stdout.write(gateway.stdout)
        sys.stdout.write(gateway.stderr)
        require(re.findall(r'^test (\S+) \.\.\.', gateway.stdout, re.M)
                == ['conformance_gateway_collects_real_receipts_and_rejects_transport_lookalikes'], 'exact actual gateway case execution')
        require(gateway.returncode == 0 and re.findall(r'^test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;', gateway.stdout, re.M) == [('1', '0', '0')],
                'real gateway observation and refusal case must execute once')
        for name, entry in artifacts['artifacts'].items():
            require(hashlib.sha256(Path(entry['path']).read_bytes()).hexdigest() == entry['sha256'], 'artifact changed during conformance: ' + name)
        return len(markers) + 2 + input_count
    finally:
        for server in servers.values():
            server.stop()


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--case', required=True, choices=sorted(CASES))
    parser.add_argument('--candidate-manifest', required=True)
    parser.add_argument('--corpus')
    parser.add_argument('--emulator-url')
    parser.add_argument('--hosted-url')
    arguments = parser.parse_args()
    digest = manifest(arguments.candidate_manifest)
    print('registry-contract: candidate manifest sha256 ' + digest)
    if arguments.case == 'emulator-conformance':
        artifacts = conformance_artifacts(digest)
        for name, supplied in [('emulator', arguments.emulator_url), ('hosted', arguments.hosted_url)]:
            require(supplied is None or supplied == artifacts['environments'][name]['url'], 'URL bound to genuine fixture process')
        corpus = conformance_corpus(arguments.corpus or artifacts['corpus_file'])
        count = conformance_contract(artifacts, corpus)
    elif arguments.case == 'guest-abi-discovery':
        count = discovery_contract(discovery_artifacts(digest))
    elif arguments.case == 'registry-readiness':
        count = readiness_contract(readiness_artifacts(digest), digest)
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
