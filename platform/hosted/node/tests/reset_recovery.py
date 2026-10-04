#!/usr/bin/env python3
import concurrent.futures
import contextlib
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[4]
NODE = ROOT / 'platform/hosted/node'
BIN = Path(os.environ.get('LAYERX_TEST_NATIVE_BIN_DIR', ROOT / 'build/bin')).resolve()
NETWORK = 77
ASSET = 'b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898'
NATIVE_EXECUTABLES = ('layerxd', 'layerx-genesis-build', 'layerx-handover')
GENESIS_FILES = ('genesis/genesis.manifest', 'genesis/genesis-request.lxgb',
                 'genesis/genesis.registration', 'genesis/00000000000000000000.lxs',
                 'genesis/paxeer-registration-request.lxrr',
                 'genesis/paxeer-deployment-descriptor.lxgd')
sys.path.insert(0, str(ROOT / 'tests/daemon'))
sys.path.insert(0, str(ROOT / 'tests/support'))
sys.dont_write_bytecode = True


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()


def request(reset_id=None, operation='reset', network=NETWORK):
    value = {'version': 1, 'operation': 'reset',
             'reset_id': reset_id or secrets.token_hex(16), 'network': network}
    value['request_digest'] = hashlib.sha256(canonical(value)).hexdigest()
    value['operation'] = operation
    return value


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def native_artifacts(manifest, record=False):
    from custody_chain import git

    revision = git('rev-parse', 'HEAD').decode().strip()
    if manifest['source_revision'] != revision:
        raise ValueError('native and custody artifacts require the exact candidate revision')
    rows = {}
    for name in NATIVE_EXECUTABLES:
        path = BIN / name
        if any(item.is_symlink() for item in (path, *path.parents)):
            raise ValueError('native artifact path must not contain symlinks: ' + name)
        metadata = path.stat()
        if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.geteuid()
                or metadata.st_mode & 0o022 or not os.access(path, os.X_OK)):
            raise ValueError('unsafe prebuilt native artifact: ' + name)
        with path.open('rb') as stream:
            if stream.read(4) != b'\x7fELF':
                raise ValueError('native artifact must be an actual ELF: ' + name)
        rows[name] = {'path': str(path), 'sha256': digest(path), 'mtime_ns': metadata.st_mtime_ns}
    if record:
        return {'version': 1, 'source_revision': revision, 'source_binding': manifest['source_binding'],
                'source_paths': manifest['source_paths'], 'recorded_ns': time.time_ns(),
                'executables': rows}
    filename = os.environ.get('LAYERX_RESET_NATIVE_ARTIFACT_MANIFEST')
    if not filename:
        raise ValueError('explicit private native artifact manifest required')
    path = Path(filename)
    info = path.lstat()
    if (not path.is_absolute() or any(item.is_symlink() for item in (path, *path.parents))
            or not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
            or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or info.st_size > 65536):
        raise ValueError('native artifact manifest must be an owned private regular file')
    native = json.loads(path.read_bytes())
    recorded = native.get('recorded_ns')
    if (set(native) != {'version', 'source_revision', 'source_binding', 'source_paths',
                       'recorded_ns', 'executables'}
            or type(native['version']) is not int or native['version'] != 1
            or native['source_revision'] != revision or native['source_binding'] != manifest['source_binding']
            or native['source_paths'] != manifest['source_paths'] or type(recorded) is not int
            or not 0 < recorded <= time.time_ns() or native['executables'] != rows
            or any(row['mtime_ns'] > recorded for row in rows.values())):
        raise ValueError('native candidate build provenance or artifact digest mismatch')
    return manifest


def record_native_artifacts(filename):
    from custody_chain import artifact_manifest

    manifest = native_artifacts(artifact_manifest(os.environ.get('LAYERX_CUSTODY_ARTIFACT_MANIFEST')), record=True)
    path = Path(filename)
    if not path.is_absolute() or any(item.is_symlink() for item in path.parents):
        raise ValueError('native artifact manifest path must be absolute without symlinks')
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(canonical(manifest) + b'\n')
        stream.flush()
        os.fsync(stream.fileno())
    descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def write_private(path, contents):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(contents)
        stream.flush()
        os.fsync(stream.fileno())
    descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def wait_until(description, predicate, seconds=120):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(.02)
    raise AssertionError('deadline waiting for ' + description)


def load_module(name, path):
    specification = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(specification)
    sys.modules[name] = module
    specification.loader.exec_module(module)
    return module


def exchange(path, value, seconds=300, lost=False):
    raw = value if isinstance(value, bytes) else canonical(value) + b'\n'
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
        client.settimeout(seconds)
        client.connect(str(path))
        client.sendall(raw)
        if lost:
            return None
        response = bytearray()
        while not response.endswith(b'\n'):
            chunk = client.recv(4097 - len(response))
            if not chunk:
                raise AssertionError('supervisor closed without its bounded response')
            response.extend(chunk)
            if len(response) > 4096:
                raise AssertionError('oversized supervisor response')
        if response.count(b'\n') != 1:
            raise AssertionError('multiple supervisor responses')
        return bytes(response), json.loads(response)


def process_identity(pid):
    try:
        fields = Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
        return fields[19] if fields[0] != 'Z' else None
    except FileNotFoundError:
        return None


def group_processes(group):
    result = {}
    for path in Path('/proc').iterdir():
        if not path.name.isdecimal():
            continue
        try:
            fields = (path / 'stat').read_text().rsplit(')', 1)[1].split()
            if int(fields[2]) == group and fields[0] != 'Z':
                result[int(path.name)] = fields[19]
        except (FileNotFoundError, ProcessLookupError):
            pass
    return result


def pause_phase(state, reset_id, phase, group, output):
    import ctypes
    import select
    import struct

    library = ctypes.CDLL(None, use_errno=True)
    descriptor = library.inotify_init1(os.O_CLOEXEC | os.O_NONBLOCK)
    if descriptor < 0:
        raise OSError(ctypes.get_errno(), 'actual reset state watch unavailable')
    try:
        if library.inotify_add_watch(descriptor, os.fsencode(state), 0x80) < 0:
            raise OSError(ctypes.get_errno(), 'actual reset state directory watch unavailable')
        write_private(Path(str(output) + '.ready'), b'ready\n')
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            if not select.select([descriptor], [], [], 1)[0]:
                continue
            events = os.read(descriptor, 65536)
            offset = 0
            while offset < len(events):
                _, mask, _, length = struct.unpack_from('iIII', events, offset)
                name = events[offset + 16:offset + 16 + length].split(b'\0', 1)[0]
                offset += 16 + length
                if mask & (0x4000 | 0x8000):
                    raise AssertionError('actual reset phase watch overflow or lost watch')
                if name != b'state.json' or not mask & 0x80:
                    continue
                record = json.loads((state / 'state.json').read_bytes())['records'].get(reset_id)
                if record is None or record['phase'] != phase:
                    continue
                os.killpg(group, signal.SIGSTOP)
                frozen = json.loads((state / 'state.json').read_bytes())['records'][reset_id]
                if frozen['phase'] != phase:
                    raise AssertionError('actual process advanced beyond requested crash boundary')
                write_private(output, canonical(frozen) + b'\n')
                return
        raise AssertionError('did not observe actual reset crash boundary ' + phase)
    finally:
        os.close(descriptor)


class NodeProcesses:
    def __init__(self, work, profile, settlement, sequencer_seed, treasury_seed):
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
        from lxgb_metadata import metadata

        self.work = work
        self.data = work / 'data'
        self.run = work / 'run'
        self.state = work / 'supervisor-state'
        self.socket = self.run / 'supervisor.sock'
        self.processes = {}
        self.logs = {}
        self.sequence = 0
        self.environment = {key: value for key, value in os.environ.items()
                            if not key.startswith('LAYERX_')}
        self.environment['PATH'] = str(BIN) + os.pathsep + self.environment.get('PATH', '')
        self.environment['PYTHONDONTWRITEBYTECODE'] = '1'
        self.sequencer_key = work / 'sequencer.key'
        self.treasury_key = work / 'treasury.key'
        write_private(self.sequencer_key, sequencer_seed)
        write_private(self.treasury_key, treasury_seed)
        public = Ed25519PrivateKey.from_private_bytes(treasury_seed).public_key().public_bytes(
            Encoding.Raw, PublicFormat.Raw)
        self.metadata = work / 'metadata.lxgb'
        write_private(self.metadata, metadata(bytes.fromhex(ASSET), public, os.urandom(32)))
        self.reservations = [socket.socket(), socket.socket()]
        for reservation in self.reservations:
            reservation.bind(('127.0.0.1', 0))
        self.program_port, self.replica_port = [item.getsockname()[1] for item in self.reservations]
        self.arguments = [
            '--network-id', str(NETWORK), '--asset', ASSET,
            '--sequencer-key', str(self.sequencer_key), '--treasury-key', str(self.treasury_key),
            '--genesis-metadata', str(self.metadata),
            '--settlement-document', str(ROOT / 'contracts/config/checkpoint-settlement.json'),
            '--settlement-env', str(settlement), '--custody-profile', str(profile),
            '--lni-uid', str(pwd.getpwnam('nobody').pw_uid), '--lni-gid', str(os.getegid()),
            '--program-port', str(self.program_port), '--replica-port', str(self.replica_port),
            '--migrations', str(ROOT / 'migrations/0007_history_index.sql'),
            '--genesis-build', str(BIN / 'layerx-genesis-build'),
        ]

    def launch(self, role, explicit_state=False):
        assert role not in self.processes, 'duplicate owned supervisor'
        self.sequence += 1
        path = self.work / f'{role}-{self.sequence}.log'
        command = ['bash', str(NODE / 'supervisor.sh'), '--role', role,
                   '--data-dir', str(self.data), '--run-dir', str(self.run),
                   '--layerxd', str(BIN / 'layerxd')]
        if explicit_state:
            command += ['--state-dir', str(self.state)]
        if role == 'sequencer':
            command += ['--', *self.arguments]
        with path.open('wb') as log:
            process = subprocess.Popen(command, cwd=ROOT, env=self.environment,
                                       stdout=log, stderr=log, start_new_session=True)
        self.processes[role] = process
        self.logs[role] = path
        return process

    def alive(self):
        for role, process in self.processes.items():
            if process.poll() is not None:
                raise AssertionError(f'{role} exited {process.returncode}; log={self.logs[role]}')

    def start(self, explicit_state=False):
        for reservation in self.reservations:
            reservation.close()
        self.reservations.clear()
        self.launch('replica', explicit_state)
        self.launch('sequencer', explicit_state)

        def ready():
            self.alive()
            if not self.socket.is_socket():
                return False
            try:
                _, status = exchange(self.socket, b'status\n', seconds=2)
                return status.get('state') == 'running'
            except (ConnectionError, FileNotFoundError, socket.timeout):
                return False

        wait_until('actual supervisors and private reset socket', ready, 180)
        self.native_pids()
        assert stat.S_IMODE(self.socket.stat().st_mode) == 0o660
        assert self.socket.stat().st_gid == os.getegid()
        assert self.state.is_dir() and self.state != self.data and self.state != self.run

    def native_pids(self):
        found = {}
        for role, process in self.processes.items():
            mode = b'--serve' if role == 'sequencer' else b'--authority-replica'
            matches = []
            for pid, started in group_processes(process.pid).items():
                try:
                    executable = Path(f'/proc/{pid}/exe').resolve(strict=True)
                    arguments = Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
                except FileNotFoundError:
                    continue
                if executable == (BIN / 'layerxd').resolve() and mode in arguments:
                    assert os.fsencode(self.data / f'{role}.conf') in arguments
                    matches.append((pid, started))
            assert len(matches) == 1, f'expected one actual native {role}, got {matches}'
            found[role] = matches[0]
        assert set(found) == {'sequencer', 'replica'}
        return found

    def stop_role(self, role, kill=False):
        process = self.processes.pop(role, None)
        if process is None:
            return
        try:
            if not kill:
                os.killpg(process.pid, signal.SIGCONT)
            if process.poll() is None:
                if kill:
                    os.killpg(process.pid, signal.SIGKILL)
                else:
                    process.send_signal(signal.SIGTERM)
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
        finally:
            with contextlib.suppress(ProcessLookupError):
                os.killpg(process.pid, signal.SIGKILL)
        wait_until('owned process group cleanup', lambda: not group_processes(process.pid), 10)

    def stop(self, kill=False):
        for role in ('sequencer', 'replica'):
            self.stop_role(role, kill)
        for reservation in self.reservations:
            reservation.close()
        self.reservations.clear()

    def state_value(self):
        return json.loads((self.state / 'state.json').read_bytes())

    def pause_scheduler(self):
        supervisor = self.processes['sequencer']
        supervisor.send_signal(signal.SIGSTOP)

        def no_inflight_state_command():
            for pid in group_processes(supervisor.pid):
                try:
                    arguments = Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
                except FileNotFoundError:
                    continue
                helper = os.fsencode(NODE / 'reset_state.py')
                if helper in arguments and b'serve' not in arguments:
                    return False
            return True

        wait_until('in-flight durable state command before filesystem failure', no_inflight_state_command, 15)

    def record(self, reset_id):
        return self.state_value()['records'].get(reset_id)

    def wait_record(self, reset_id, phase='completed'):
        def selected():
            self.alive()
            record = self.record(reset_id)
            if record is not None and record['phase'] == phase:
                return record
            return None
        return wait_until('durable reset phase ' + phase, selected, 180)

    def genesis(self):
        directory = self.data / 'genesis'
        assert directory.is_dir(), 'canonical bootstrap did not produce genesis'
        result = {}
        for entry in sorted(directory.rglob('*')):
            if entry.is_file():
                assert not entry.is_symlink()
                result[str(entry.relative_to(self.data))] = digest(entry)
        for name in ('genesis.manifest', '00000000000000000000.lxs',
                     'genesis.registration', 'genesis-request.lxgb',
                     'paxeer-registration-request.lxrr', 'paxeer-deployment-descriptor.lxgd'):
            assert 'genesis/' + name in result, 'canonical artifact absent: ' + name
        return result

    def assert_completed(self, reset_id, response):
        record = self.record(reset_id)
        assert record['phase'] == 'completed'
        assert record['response'] == response
        assert response == {'state': 'reset', 'reset_id': reset_id, 'generation': record['generation']}
        assert self.state_value()['active'] is None
        assert self.state_value()['generation'] == record['generation']
        assert record['peer_uid'] == os.geteuid() and record['peer_gid'] == os.getegid()
        genesis = self.genesis()
        assert record['genesis'] == {name: genesis[name] for name in GENESIS_FILES}, \
            'durable original canonical genesis identity differs'
        raw = (self.data / 'genesis/genesis-request.lxgb').read_bytes()
        assert raw[:5] == b'LXGB\x02'
        assert int.from_bytes(raw[7:11], 'big') == NETWORK
        assert int.from_bytes(raw[11:19], 'big') == record['genesis_timestamp_ms']
        return record

    def failed_restart(self, expected):
        process = self.launch('sequencer', explicit_state=True)
        try:
            code = process.wait(timeout=20)
            assert code != 0, 'unsafe state startup succeeded'
            diagnostic = self.logs['sequencer'].read_text()
            assert expected in diagnostic, self.logs['sequencer']
            assert 'started layerxd' not in diagnostic, 'native process started before durable recovery refusal'
            for pid in group_processes(process.pid):
                with contextlib.suppress(FileNotFoundError):
                    assert Path(f'/proc/{pid}/exe').resolve() != (BIN / 'layerxd').resolve()
        finally:
            self.stop_role('sequencer')

    def freeze_request(self, value, phase):
        output = self.work / ('pause-' + value['reset_id'] + '-' + phase + '.json')
        log = output.with_suffix('.log')
        group = self.processes['sequencer'].pid
        with log.open('wb') as stream:
            watcher = subprocess.Popen([
                sys.executable, str(Path(__file__).resolve()), '--pause-phase', str(self.state),
                value['reset_id'], phase, str(group), str(output)],
                cwd=ROOT, env=self.environment, stdin=subprocess.DEVNULL,
                stdout=stream, stderr=stream, start_new_session=True)
        try:
            def ready():
                assert watcher.poll() is None, 'actual phase watcher exited before readiness; log=' + str(log)
                return Path(str(output) + '.ready').exists()
            wait_until('actual phase watcher readiness', ready, 10)
            exchange(self.socket, value, lost=True)
            assert watcher.wait(timeout=190) == 0, 'actual phase watcher failed; log=' + str(log)
            frozen = json.loads(output.read_bytes())
            assert frozen == self.record(value['reset_id']) and frozen['phase'] == phase
            return frozen
        finally:
            if watcher.poll() is None:
                watcher.kill()
                watcher.wait(timeout=5)


class ActualResetRecovery(unittest.TestCase):
    completed_cases = []

    @classmethod
    def setUpClass(cls):
        from custody_chain import artifact_manifest

        assert os.geteuid() == 0, 'real peer credential checks require root'
        cls.artifacts = native_artifacts(artifact_manifest(os.environ.get('LAYERX_CUSTODY_ARTIFACT_MANIFEST')))
        for name in ('socat', 'openssl', 'jq'):
            assert shutil.which(name), 'required real-process tool missing: ' + name

    def case(self, name):
        self.completed_cases.append(name)
        print('actual-reset-case: ' + name, flush=True)

    def make_node(self, *arguments):
        return NodeProcesses(*arguments)

    def test_actual_reset_identity_and_recovery(self):
        from custody_chain import boundaries, owned_chain

        withdraw = load_module('reset_withdraw_custody', ROOT / 'tests/daemon/withdraw-custody.py')
        work = Path(tempfile.mkdtemp(prefix='lx-reset-', dir='/tmp'))
        work.chmod(0o755)
        print('actual reset private evidence: ' + str(work), flush=True)
        node = None
        executables = self.artifacts['executables']
        previous = {key: os.environ.get(key) for key in ('PAXD', 'LAYERX_CUSTODY_PROOF_BIN')}
        os.environ['PAXD'] = executables['paxd']['path']
        os.environ['LAYERX_CUSTODY_PROOF_BIN'] = executables['layerx-custody-proof']['path']
        try:
            sequencer_seed, treasury_seed = os.urandom(32), os.urandom(32)
            genesis = withdraw.custody_genesis(work, NETWORK, sequencer_seed)
            with owned_chain(work, Path(self.artifacts['contract_directory']), genesis) as chain:
                with boundaries(work, chain, Path(executables['layerx-paxeer-boundary']['path'])) as boundary:
                    origins, ca, identity = boundary
                    profile = work / 'custody.profile'
                    comet = json.loads(chain.identity_path.read_text())['comet_url']
                    with (work / 'custody-profile.log').open('wb') as log:
                        subprocess.run([
                            sys.executable, str(ROOT / 'tests/bridge/custody_credit.py'), 'profile',
                            '--rpc', origins[0], '--rpc', origins[1], '--ca-bundle', str(ca),
                            '--disposable-identity', str(identity), '--comet-rpc', comet,
                            '--chain-id', '125', '--network-id', str(NETWORK),
                            '--vault', '0x0000000000000000000000000000000000001013',
                            '--runtime-sha256', '0x' + withdraw.module_identity().hex(),
                            '--asset', '0x' + ASSET, '--trusted-height', '1',
                            '--trusting-period-seconds', '1209600', '--output', str(profile),
                        ], cwd=ROOT, check=True, stdout=log, stderr=log, timeout=90)
                    self.assertEqual(profile.stat().st_size, 223)
                    withdraw.write_settlement(work, chain, withdraw.ANCHOR_ADDRESS, withdraw.ANCHOR_ADDRESS)
                    node = self.make_node(work, profile, work / 'settlement.env', sequencer_seed, treasury_seed)
                    node.start()
                    self.exercise(node)
                    state_bytes = (node.state / 'state.json').read_bytes()
                    for seed in (sequencer_seed, treasury_seed):
                        self.assertTrue(seed not in state_bytes and seed.hex().encode() not in state_bytes,
                                        'durable reset state persisted a generated private seed')
        finally:
            if node is not None:
                node.stop()
            for key, value in previous.items():
                if value is None:
                    os.environ.pop(key, None)
                else:
                    os.environ[key] = value
            for key in ('sequencer.key', 'treasury.key', 'deposit-root-authority.key'):
                (work / key).unlink(missing_ok=True)
            if node is not None:
                for directory in (node.data / 'secrets', node.data / 'work'):
                    if directory.is_dir():
                        shutil.rmtree(directory)
            work.chmod(0o700)

    def exercise(self, node):
        initial = node.native_pids()
        initial_genesis = node.genesis()
        before = node.state_value()['generation']
        marker = node.data / 'pre-reset-marker'
        write_private(marker, b'old generation must be discarded\n')
        first = request()
        old_manifest = (node.data / 'genesis/genesis.manifest').open('rb')
        try:
            original_inode = os.fstat(old_manifest.fileno()).st_ino
            raw, response = exchange(node.socket, first)
            record = node.assert_completed(first['reset_id'], response)
            self.assertEqual(record['generation'], before + 1)
            self.assertFalse(marker.exists())
            self.assertNotEqual((node.data / 'genesis/genesis.manifest').stat().st_ino, original_inode)
            self.assertNotEqual(node.genesis(), initial_genesis)
            self.assertNotEqual(node.native_pids(), initial)
        finally:
            old_manifest.close()
        self.case('first reset uses canonical bootstrap and both actual native daemons')

        marker = node.data / 'post-reset-marker'
        write_private(marker, os.urandom(128))
        original_marker = marker.read_bytes()
        original_genesis = node.genesis()
        current = node.native_pids()
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            replies = list(pool.map(lambda _: exchange(node.socket, first), range(4)))
        self.assertEqual([item[0] for item in replies], [raw] * 4)
        self.assertEqual(exchange(node.socket, request(first['reset_id'], 'status'))[0], raw)
        self.assertEqual(node.native_pids(), current)
        self.assertEqual(node.genesis(), original_genesis)
        self.assertEqual(marker.read_bytes(), original_marker)
        self.case('concurrent completed replay and read-only status preserve original response and data')

        durable_before_duplicate = (node.state / 'state.json').read_bytes()
        command = ['bash', str(NODE / 'supervisor.sh'), '--role', 'sequencer',
                   '--data-dir', str(node.data), '--run-dir', str(node.run),
                   '--layerxd', str(BIN / 'layerxd'), '--', *node.arguments]
        with (node.work / 'duplicate-supervisor.log').open('wb') as log:
            duplicate = subprocess.run(command, cwd=ROOT, env=node.environment,
                                       stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                       start_new_session=True, timeout=15, check=False)
        self.assertNotEqual(duplicate.returncode, 0)
        self.assertIn('supervisor_already_owned', (node.work / 'duplicate-supervisor.log').read_text())
        self.assertEqual((node.state / 'state.json').read_bytes(), durable_before_duplicate)
        self.assertEqual(node.native_pids(), current)
        self.assertEqual(node.genesis(), original_genesis)
        self.assertEqual(marker.read_bytes(), original_marker)
        self.case('second actual sequencer supervisor refuses durable lifetime ownership')

        state_before = (node.state / 'state.json').read_bytes()
        invalid = [
            (dict(first, version=2), 'unsupported_version'),
            (dict(first, reset_id='A' * 32), 'invalid_reset_id'),
            (dict(first, reset_id='a' * 31), 'invalid_reset_id'),
            (request(first['reset_id'], network=NETWORK + 1), 'wrong_network'),
            (dict(first, request_digest='0' * 64), 'digest_mismatch'),
            (dict(first, extra=1), 'invalid_request'),
            (dict(first, operation='erase'), 'invalid_operation'),
            (b'{"version":1,"version":1}\n', 'invalid_json'),
            (b'x' * 4096 + b'\n', 'request_too_large'),
        ]
        for value, expected in invalid:
            with self.subTest(refusal=expected):
                self.assertEqual(exchange(node.socket, value)[1]['error']['code'], expected)
        self.assertEqual(exchange(node.socket, request(operation='status'))[1]['error']['code'], 'unknown_reset')
        self.assertEqual((node.state / 'state.json').read_bytes(), state_before)
        self.assertEqual(node.native_pids(), current)
        self.assertEqual(node.genesis(), original_genesis)
        self.assertEqual(marker.read_bytes(), original_marker)
        self.peer_refusals(node, first)
        self.assertEqual((node.state / 'state.json').read_bytes(), state_before)
        self.case('malformed network digest and conflicting peer reuse refuse without mutation')

        concurrent_request = request()
        before = node.state_value()['generation']
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            replies = list(pool.map(lambda _: exchange(node.socket, concurrent_request), range(4)))
        self.assertEqual([item[0] for item in replies], [replies[0][0]] * 4)
        node.assert_completed(concurrent_request['reset_id'], replies[0][1])
        self.assertEqual(node.state_value()['generation'], before + 1)
        self.case('concurrent first admission commits exactly one generation')

        lost = request()
        exchange(node.socket, lost, lost=True)
        completed = node.wait_record(lost['reset_id'])
        expected = canonical(completed['response']) + b'\n'
        self.assertEqual(exchange(node.socket, lost)[0], expected)
        node.assert_completed(lost['reset_id'], completed['response'])
        write_private(marker, original_marker)
        genesis_before_restart = node.genesis()
        native_before_kill = node.native_pids()
        sequencer = node.processes['sequencer']
        sequencer.kill()
        sequencer.wait(timeout=5)
        pid, started = native_before_kill['sequencer']
        wait_until('native sequencer parent-death SIGKILL', lambda: process_identity(pid) != started, 10)
        node.stop(kill=True)
        shutil.rmtree(node.run)
        original_env = node.data / 'node.env'
        retained_env = node.data / 'node.env.retained'
        original_env.rename(retained_env)
        try:
            node.failed_restart('completed reset generation is missing')
            self.assertEqual(node.genesis(), genesis_before_restart)
            self.assertEqual(marker.read_bytes(), original_marker)
            self.assertEqual(node.record(lost['reset_id'])['response'], completed['response'])
        finally:
            retained_env.rename(original_env)
        self.case('completed missing generation refuses bootstrap without changing original genesis')
        node.start(explicit_state=True)
        self.assertEqual(exchange(node.socket, lost)[0], expected)
        self.assertEqual(exchange(node.socket, request(lost['reset_id'], 'status'))[0], expected)
        self.assertEqual(node.genesis(), genesis_before_restart)
        self.assertEqual(marker.read_bytes(), original_marker)
        self.assertEqual(node.state_value()['generation'], completed['generation'])
        self.case('lost response completed SIGKILL and recreated RUN_DIR retain original outcome')

        generation_before_restart = node.state_value()['generation']
        node.stop()
        self.assertTrue((node.run / 'generation').exists())
        node.start(explicit_state=True)
        self.assertEqual(exchange(node.socket, lost)[0], expected)
        self.assertEqual(node.state_value()['generation'], generation_before_restart)
        self.assertEqual(node.genesis(), genesis_before_restart)
        self.assertEqual(marker.read_bytes(), original_marker)
        node.native_pids()
        self.case('retained RUN_DIR restart reaffirms actual replica readiness without advancing generation')

        admitted = request()
        sequencer = node.processes['sequencer']
        sequencer.send_signal(signal.SIGSTOP)
        exchange(node.socket, admitted, lost=True)
        admission = node.wait_record(admitted['reset_id'], 'admitted')
        self.assertEqual(exchange(node.socket, request(admitted['reset_id'], 'status'))[1]['state'], 'admitted')
        self.assertEqual(exchange(node.socket, request())[1]['error']['code'], 'reset_in_progress')
        node.stop(kill=True)
        metadata = node.metadata.read_bytes()
        write_private(node.metadata, metadata[:-1] + bytes([metadata[-1] ^ 1]))
        node.failed_restart('bindings_mismatch')
        self.assertEqual(node.genesis(), genesis_before_restart)
        self.assertEqual(marker.read_bytes(), original_marker)
        write_private(node.metadata, metadata)
        node.start(explicit_state=True)
        recovered = node.wait_record(admitted['reset_id'])
        for key in ('genesis_timestamp_ms', 'bindings', 'generation', 'request_digest', 'peer_uid', 'peer_gid'):
            self.assertEqual(recovered[key], admission[key])
        node.assert_completed(admitted['reset_id'], recovered['response'])
        self.assertFalse(marker.exists())
        self.case('admitted SIGKILL recovery freezes time and canonical inputs and refuses changed inputs')

        write_private(marker, original_marker)
        genesis_before_failure = node.genesis()
        pids_before_failure = node.native_pids()
        state_file = node.state / 'state.json'
        saved = state_file.read_bytes()
        node.pause_scheduler()
        node.state.chmod(0o500)
        try:
            refused = exchange(node.socket, request())[1]
            self.assertEqual(refused['error']['code'], 'unsafe_state_directory')
            self.assertEqual(state_file.read_bytes(), saved)
            self.assertEqual(node.genesis(), genesis_before_failure)
            self.assertEqual(marker.read_bytes(), original_marker)
            self.assertEqual(node.native_pids(), pids_before_failure)
        finally:
            node.state.chmod(0o700)
            node.processes['sequencer'].send_signal(signal.SIGCONT)
        self.case('unwritable durable directory mode refuses before destructive admission')

        node.stop()
        write_private(state_file, b'{"version":1,"records":')
        node.failed_restart('invalid_json')
        self.assertEqual(node.genesis(), genesis_before_failure)
        self.assertEqual(marker.read_bytes(), original_marker)
        write_private(state_file, saved)
        node.start(explicit_state=True)
        self.assertEqual(exchange(node.socket, admitted)[1], recovered['response'])
        self.case('corrupt durable state refuses native startup without deleting retained generation')

        previous_generation = node.state_value()['generation']
        previous_genesis = node.genesis()
        previous_pids = node.native_pids()
        _, legacy = exchange(node.socket, b'reset\n')
        self.assertEqual(set(legacy), {'state', 'reset_id'})
        self.assertEqual(legacy['state'], 'reset')
        legacy_record = node.record(legacy['reset_id'])
        node.assert_completed(legacy['reset_id'], legacy_record['response'])
        self.assertEqual(legacy_record['generation'], previous_generation + 1)
        self.assertEqual(exchange(node.socket, b'status\n')[1],
                         {'state': 'running', 'generation': legacy_record['generation']})
        self.assertNotEqual(node.genesis(), previous_genesis)
        self.assertNotEqual(node.native_pids(), previous_pids)
        self.assertFalse(marker.exists())
        self.case('unchanged legacy reset and status protocol uses the actual reset lifecycle')

        ambiguous = request()
        exchange(node.socket, ambiguous, lost=True)
        group = node.processes['sequencer'].pid
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            active = node.record(ambiguous['reset_id'])
            if active is not None and active['phase'] == 'bootstrapping':
                os.killpg(group, signal.SIGSTOP)
                break
            node.alive()
            time.sleep(.001)
        else:
            self.fail('did not observe actual durable bootstrapping phase')
        frozen = node.record(ambiguous['reset_id'])
        self.assertEqual(frozen['phase'], 'bootstrapping')
        os.killpg(group, signal.SIGKILL)
        node.processes['sequencer'].wait(timeout=5)
        node.stop(kill=True)
        partial = {str(path.relative_to(node.data)): digest(path)
                   for path in (node.data / 'genesis').glob('*') if path.is_file()}
        write_private(marker, original_marker)
        node.failed_restart('ambiguous')
        refused = node.record(ambiguous['reset_id'])
        self.assertEqual(refused['phase'], 'ambiguous')
        self.assertIsNone(refused['response'])
        self.assertEqual(refused['genesis_timestamp_ms'], frozen['genesis_timestamp_ms'])
        self.assertEqual(refused['bindings'], frozen['bindings'])
        self.assertEqual(marker.read_bytes(), original_marker)
        self.assertEqual({str(path.relative_to(node.data)): digest(path)
                          for path in (node.data / 'genesis').glob('*') if path.is_file()}, partial)
        self.case('actual bootstrap crash remains explicit ambiguous and never reruns destruction')

    def peer_refusals(self, node, original):
        nobody = pwd.getpwnam('nobody')
        program = '''import json, socket, sys
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(10)
try:
    s.connect(sys.argv[1])
    s.sendall(sys.argv[2].encode() + b'\\n')
    data = b''
    while not data.endswith(b'\\n') and len(data) <= 4096:
        chunk = s.recv(4097 - len(data))
        if not chunk:
            raise RuntimeError('no refusal response')
        data += chunk
    print(json.loads(data)['error']['code'])
except PermissionError:
    print('socket_permission_denied')
finally:
    s.close()
'''
        for gid, expected in ((os.getegid(), 'identity_conflict'),
                              (nobody.pw_gid, 'socket_permission_denied')):
            result = subprocess.run(['/usr/bin/python3', '-c', program, str(node.socket), canonical(original).decode()],
                                    user=nobody.pw_uid, group=gid, extra_groups=[],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=True)
            self.assertEqual(result.stdout.decode().strip(), expected)


class StagedResetRecovery(ActualResetRecovery):
    def make_node(self, *arguments):
        node = NodeProcesses(*arguments)
        node.environment['LAYERX_NODE_RESET_STAGED_GENERATIONS'] = '1'
        return node

    def assert_staged_completed(self, node, value, frozen):
        raw, response = exchange(node.socket, value)
        expected = {'state': 'reset', 'reset_id': value['reset_id'], 'generation': frozen['generation']}
        self.assertEqual(response, expected)
        self.assertEqual(exchange(node.socket, request(value['reset_id'], 'status'))[0], raw)
        node.native_pids()
        marker_path = node.data / '.reset-generation.json'
        info = marker_path.lstat()
        self.assertTrue(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                        and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o600)
        genesis = node.genesis()
        expected_marker = {'version': 1, 'reset_id': value['reset_id'],
                           'generation': frozen['generation'], 'data_dir': str(node.data),
                           'genesis': {name: genesis[name] for name in GENESIS_FILES}}
        self.assertEqual(json.loads(marker_path.read_bytes()), expected_marker)
        decision = node.record(value['reset_id'])
        self.assertEqual(decision['phase'], 'activating', 'activation must not require a completion append')
        self.assertEqual(decision['activation_response'], expected)
        self.assertEqual(decision['genesis'], expected_marker['genesis'])
        for key in ('reset_id', 'generation', 'genesis_timestamp_ms', 'bindings',
                    'request_digest', 'peer_uid', 'peer_gid'):
            self.assertEqual(decision[key], frozen[key])
        self.assertEqual(decision['request_digest'], value['request_digest'])
        self.assertEqual(decision['peer_uid'], os.geteuid())
        self.assertEqual(decision['peer_gid'], os.getegid())
        canonical_request = (node.data / 'genesis/genesis-request.lxgb').read_bytes()
        self.assertEqual(canonical_request[:5], b'LXGB\x02')
        self.assertEqual(int.from_bytes(canonical_request[7:11], 'big'), NETWORK)
        self.assertEqual(int.from_bytes(canonical_request[11:19], 'big'), frozen['genesis_timestamp_ms'])
        return raw

    def exercise(self, node):
        retained_state = (node.state / 'state.json').read_bytes()
        retained_genesis = node.genesis()
        retained_pids = node.native_pids()
        node.pause_scheduler()
        node.state.chmod(0o500)
        try:
            refusal = exchange(node.socket, request())[1]
            self.assertEqual(refusal['error']['code'], 'unsafe_state_directory')
            self.assertEqual((node.state / 'state.json').read_bytes(), retained_state)
            self.assertEqual(node.genesis(), retained_genesis)
            self.assertEqual(node.native_pids(), retained_pids)
        finally:
            node.state.chmod(0o700)
            node.processes['sequencer'].send_signal(signal.SIGCONT)
        for phase in ('admitted', 'bootstrapping', 'prepared', 'activating'):
            with self.subTest(crash_boundary=phase):
                original_genesis = node.genesis()
                original_pids = node.native_pids()
                previous_generation = json.loads((node.data / '.reset-generation.json').read_bytes())['generation'] \
                    if (node.data / '.reset-generation.json').exists() else node.state_value()['generation']
                sentinel = node.data / 'retained-prior-generation'
                retained = os.urandom(128)
                write_private(sentinel, retained)
                value = request()
                frozen = node.freeze_request(value, phase)
                self.assertEqual(frozen['generation'], previous_generation + 1)
                marker_path = node.data / '.reset-generation.json'
                activated = phase == 'activating' and marker_path.exists() \
                    and json.loads(marker_path.read_bytes())['reset_id'] == value['reset_id']
                if activated:
                    marker = json.loads(marker_path.read_bytes())
                    self.assertEqual(marker['genesis'], frozen['genesis'])
                    self.assertEqual(marker['generation'], frozen['generation'])
                    self.assertEqual(marker['data_dir'], str(node.data))
                    self.assertEqual({name: node.genesis()[name] for name in GENESIS_FILES}, frozen['genesis'])
                else:
                    self.assertEqual(node.genesis(), original_genesis,
                                     'previous canonical generation must survive until the activation decision')
                    self.assertEqual(sentinel.read_bytes(), retained)
                if phase == 'admitted':
                    self.assertEqual(node.native_pids(), original_pids)
                stage = node.state / 'generations' / value['reset_id'] / 'data'
                if phase == 'prepared':
                    staged_marker = json.loads((stage / '.reset-generation.json').read_bytes())
                    self.assertEqual(staged_marker['reset_id'], value['reset_id'])
                    self.assertEqual(staged_marker['generation'], frozen['generation'])
                    self.assertEqual(staged_marker['data_dir'], str(node.data))
                    self.assertEqual(staged_marker['genesis'], frozen['genesis'])
                    for name, expected in frozen['genesis'].items():
                        self.assertEqual(digest(stage / name), expected)
                node.stop(kill=True)
                shutil.rmtree(node.run)
                node.start(explicit_state=True)
                original_response = self.assert_staged_completed(node, value, frozen)
                self.assertFalse(sentinel.exists())
                self.assertNotEqual(node.genesis(), original_genesis)
                self.assertNotEqual(node.native_pids(), original_pids)
                self.assertEqual({name: digest(stage / name) for name in GENESIS_FILES},
                                 {name: original_genesis[name] for name in GENESIS_FILES})
                self.assertEqual((stage / 'retained-prior-generation').read_bytes(), retained)
                current_genesis, current_pids = node.genesis(), node.native_pids()
                with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
                    replies = list(pool.map(lambda _: exchange(node.socket, value)[0], range(4)))
                self.assertEqual(replies, [original_response] * 4)
                self.assertEqual(node.genesis(), current_genesis)
                self.assertEqual(node.native_pids(), current_pids)
                self.case('staged actual ' + phase + ' crash retains prior generation and resumes frozen reset')

        value = request()
        original_genesis = node.genesis()
        exchange(node.socket, value, lost=True)

        def completed():
            node.alive()
            response = exchange(node.socket, request(value['reset_id'], 'status'), seconds=2)[1]
            return response if response.get('state') == 'reset' else None

        response = wait_until('staged actual readiness after lost reset response', completed, 180)
        frozen = node.record(value['reset_id'])
        original_response = self.assert_staged_completed(node, value, frozen)
        self.assertEqual(response, json.loads(original_response))
        self.assertNotEqual(node.genesis(), original_genesis)
        activated_genesis = node.genesis()
        durable_decision = (node.state / 'state.json').read_bytes()
        node.stop(kill=True)
        shutil.rmtree(node.run)
        node.start(explicit_state=True)
        self.assertEqual(self.assert_staged_completed(node, value, frozen), original_response)
        self.assertEqual(node.genesis(), activated_genesis)
        self.assertEqual((node.state / 'state.json').read_bytes(), durable_decision)
        node.stop()
        node.start(explicit_state=True)
        self.assertEqual(self.assert_staged_completed(node, value, frozen), original_response)
        self.assertEqual(node.genesis(), activated_genesis)
        self.assertEqual((node.state / 'state.json').read_bytes(), durable_decision)
        self.case('staged completion response loss and persistent or ephemeral restart require no outcome append')


def main():
    os.umask(0o077)
    if len(sys.argv) == 3 and sys.argv[1] == '--record-native':
        record_native_artifacts(sys.argv[2])
        return 0
    if len(sys.argv) == 7 and sys.argv[1] == '--pause-phase':
        pause_phase(Path(sys.argv[2]), sys.argv[3], sys.argv[4], int(sys.argv[5]), Path(sys.argv[6]))
        return 0
    staged = len(sys.argv) == 2 and sys.argv[1] == '--staged'
    if len(sys.argv) != 1 and not staged:
        raise ValueError('usage: reset_recovery.py [--staged | --record-native EXACT_MANIFEST_PATH]')
    helper_tests = load_module('reset_state_tests', NODE / 'tests/test_reset_state.py')
    helper_suite = unittest.defaultTestLoader.loadTestsFromModule(helper_tests)
    actual_suite = unittest.defaultTestLoader.loadTestsFromTestCase(ActualResetRecovery)
    if staged:
        actual_suite.addTests(unittest.defaultTestLoader.loadTestsFromTestCase(StagedResetRecovery))
    helper_count = helper_suite.countTestCases()
    actual_count = actual_suite.countTestCases()
    if helper_count == 0 or actual_count != (2 if staged else 1):
        raise AssertionError('missing durable-store or actual-process tests')
    result = unittest.TextTestRunner(verbosity=2).run(unittest.TestSuite((helper_suite, actual_suite)))
    counts = {'tests_run': result.testsRun, 'durable_store_tests_planned': helper_count,
              'actual_process_tests_planned': actual_count,
              'actual_cases_completed': len(ActualResetRecovery.completed_cases),
              'failures': len(result.failures), 'errors': len(result.errors), 'skipped': len(result.skipped)}
    print(json.dumps(counts, sort_keys=True), flush=True)
    print('PAXEER_X_GATE tests=%d skipped=%d actual_cases=%d' %
          (result.testsRun, len(result.skipped), len(ActualResetRecovery.completed_cases)), flush=True)
    return 0 if (result.wasSuccessful() and not result.skipped
                 and len(ActualResetRecovery.completed_cases) == (18 if staged else 13)
                 and result.testsRun == helper_count + actual_count) else 1


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, lambda signum, frame: sys.exit(128 + signum))
    sys.exit(main())
