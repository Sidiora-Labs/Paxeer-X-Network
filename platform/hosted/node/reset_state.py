#!/usr/bin/env python3
import argparse
import contextlib
import copy
import ctypes
import fcntl
import hashlib
import json
import os
import re
import secrets
import signal
import socket
import stat
import struct
import sys
import threading
import time


MAX_RECORDS = 1024
MAX_BINDINGS = 8192
MAX_RECORD = 16384
MAX_STATE = MAX_RECORDS * MAX_RECORD + 4096
PHASES = {'admitted', 'stopping', 'bootstrapping', 'prepared', 'activating',
          'completed', 'ambiguous'}
NEXT = {'admitted': 'stopping', 'stopping': 'bootstrapping',
        'bootstrapping': 'prepared', 'prepared': 'activating',
        'activating': 'completed'}
IDENTITY = re.compile(r'[0-9a-f]{32}\Z')
DIGEST = re.compile(r'[0-9a-f]{64}\Z')
TEMPORARY = re.compile(r'\.state\.[0-9a-f]{32}\.tmp\Z')


class StoreError(Exception):
    def __init__(self, code):
        self.code = code
        super().__init__(code)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'),
                      allow_nan=False).encode('utf-8')


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise StoreError('invalid_json')
        result[key] = value
    return result


def decode(raw):
    try:
        return json.loads(raw, object_pairs_hook=unique_object,
                          parse_constant=lambda _: (_ for _ in ()).throw(
                              StoreError('invalid_json')))
    except (ValueError, UnicodeError, RecursionError):
        raise StoreError('invalid_json') from None


def request_digest(request):
    return hashlib.sha256(canonical({
        'version': 1, 'operation': 'reset', 'reset_id': request['reset_id'],
        'network': request['network']})).hexdigest()


def validate_request(request, network, operation):
    if not isinstance(request, dict) or set(request) != {
            'version', 'operation', 'reset_id', 'network', 'request_digest'}:
        raise StoreError('invalid_request')
    if type(request['version']) is not int or request['version'] != 1:
        raise StoreError('unsupported_version')
    if request['operation'] != operation:
        raise StoreError('invalid_operation')
    if not isinstance(request['reset_id'], str) or not IDENTITY.fullmatch(request['reset_id']):
        raise StoreError('invalid_reset_id')
    if type(request['network']) is not int or request['network'] != network:
        raise StoreError('wrong_network')
    if not isinstance(request['request_digest'], str) or not DIGEST.fullmatch(request['request_digest']):
        raise StoreError('invalid_digest')
    if request_digest(request) != request['request_digest']:
        raise StoreError('digest_mismatch')


def private_file(metadata):
    if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.geteuid()
            or stat.S_IMODE(metadata.st_mode) != 0o600 or metadata.st_nlink != 1):
        raise StoreError('unsafe_state_file')


def open_directory(path, create=False):
    absolute = os.path.abspath(path)
    descriptor = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
    try:
        parts = absolute.split('/')[1:]
        for index, part in enumerate(parts):
            if create and index == len(parts) - 1:
                try:
                    os.mkdir(part, 0o700, dir_fd=descriptor)
                    os.fsync(descriptor)
                except FileExistsError:
                    pass
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        metadata = os.fstat(descriptor)
        if metadata.st_uid != os.geteuid() or stat.S_IMODE(metadata.st_mode) != 0o700:
            raise StoreError('unsafe_state_directory')
        return descriptor
    except BaseException:
        os.close(descriptor)
        raise


class Store:
    def __init__(self, state_dir, data_dir, run_dir, network, bindings, allowed_gid=None,
                 initial_generation=1):
        self.state_dir = os.path.abspath(state_dir)
        self.data_dir = os.path.abspath(data_dir)
        self.run_dir = os.path.abspath(run_dir)
        if self.state_dir == '/':
            raise StoreError('unsafe_state_directory')
        for other in (self.data_dir, self.run_dir):
            common = os.path.commonpath((self.state_dir, other))
            if common in (self.state_dir, other):
                raise StoreError('state_directory_overlap')
        if type(network) is not int or not 0 <= network <= 0xffffffff:
            raise StoreError('wrong_network')
        if not isinstance(bindings, dict):
            raise StoreError('invalid_bindings')
        try:
            encoded = canonical(bindings)
        except (ValueError, TypeError, RecursionError):
            raise StoreError('invalid_bindings') from None
        if len(encoded) > MAX_BINDINGS:
            raise StoreError('bindings_too_large')
        self.network = network
        self.bindings = decode(encoded)
        self.allowed_gid = os.getegid() if allowed_gid is None else allowed_gid
        if type(self.allowed_gid) is not int or not 0 <= self.allowed_gid <= 0xffffffff:
            raise StoreError('invalid_allowed_gid')
        if type(initial_generation) is not int or not 1 <= initial_generation <= 0x7fffffffffffffff:
            raise StoreError('invalid_initial_generation')
        self.initial_generation = initial_generation

    def _authorize(self, peer):
        if (not isinstance(peer, (tuple, list)) or len(peer) != 2
                or any(type(value) is not int or not 0 <= value <= 0xffffffff for value in peer)
                or (peer[0] != os.geteuid() and peer[1] != self.allowed_gid)):
            raise StoreError('unauthorized_peer')

    def _record(self, record, reset_id):
        keys = {'reset_id', 'request_digest', 'network', 'peer_uid', 'peer_gid',
                'generation', 'genesis_timestamp_ms', 'bindings', 'phase', 'response'}
        if not isinstance(record, dict) or set(record) not in (keys, keys | {'genesis'}):
            raise StoreError('corrupt_state')
        if record['reset_id'] != reset_id or not IDENTITY.fullmatch(reset_id):
            raise StoreError('corrupt_state')
        request = {'version': 1, 'operation': 'reset', 'reset_id': reset_id,
                   'network': record['network'], 'request_digest': record['request_digest']}
        try:
            validate_request(request, self.network, 'reset')
        except StoreError:
            raise StoreError('corrupt_state') from None
        for key in ('peer_uid', 'peer_gid', 'generation', 'genesis_timestamp_ms'):
            if type(record[key]) is not int or record[key] < (1 if key in ('generation', 'genesis_timestamp_ms') else 0):
                raise StoreError('corrupt_state')
        if record['generation'] > 0x7fffffffffffffff or record['genesis_timestamp_ms'] > 0x7fffffffffffffff:
            raise StoreError('corrupt_state')
        if record['peer_uid'] > 0xffffffff or record['peer_gid'] > 0xffffffff:
            raise StoreError('corrupt_state')
        if not isinstance(record['phase'], str) or record['phase'] not in PHASES:
            raise StoreError('corrupt_state')
        if not isinstance(record['bindings'], dict) or len(canonical(record['bindings'])) > MAX_BINDINGS:
            raise StoreError('corrupt_state')
        if 'genesis' in record:
            self._genesis(record['genesis'])
        if record['phase'] in ('prepared', 'activating', 'completed') and 'genesis' not in record:
            raise StoreError('corrupt_state')
        expected = {'state': 'reset', 'reset_id': reset_id, 'generation': record['generation']}
        if record['response'] != (expected if record['phase'] == 'completed' else None):
            raise StoreError('corrupt_state')
        if len(canonical(record)) > MAX_RECORD:
            raise StoreError('corrupt_state')

    @staticmethod
    def _genesis(value):
        if not isinstance(value, dict) or not 1 <= len(value) <= 32:
            raise StoreError('invalid_genesis')
        for name, digest in value.items():
            if (not isinstance(name, str) or not name or len(name) > 256
                    or name.startswith('/') or any(part in ('', '.', '..') for part in name.split('/'))
                    or any(ord(character) < 32 or ord(character) == 127 for character in name)
                    or not isinstance(digest, str) or not DIGEST.fullmatch(digest)):
                raise StoreError('invalid_genesis')
        if len(canonical(value)) > 4096:
            raise StoreError('invalid_genesis')

    def _validate(self, state):
        if (not isinstance(state, dict) or set(state) != {
                'version', 'network', 'baseline_generation', 'generation', 'active', 'records'}
                or type(state['version']) is not int or state['version'] != 1
                or type(state['network']) is not int or not 0 <= state['network'] <= 0xffffffff
                or state['network'] != self.network
                or type(state['baseline_generation']) is not int
                or not 1 <= state['baseline_generation'] <= 0x7fffffffffffffff
                or type(state['generation']) is not int or not 1 <= state['generation'] <= 0x7fffffffffffffff
                or not isinstance(state['records'], dict) or len(state['records']) > MAX_RECORDS):
            raise StoreError('corrupt_state')
        if state['active'] is not None and (not isinstance(state['active'], str)
                                           or not IDENTITY.fullmatch(state['active'])):
            raise StoreError('corrupt_state')
        incomplete = []
        generations = set()
        for reset_id, record in state['records'].items():
            self._record(record, reset_id)
            if record['generation'] in generations:
                raise StoreError('corrupt_state')
            generations.add(record['generation'])
            if record['phase'] == 'completed':
                if record['generation'] > state['generation']:
                    raise StoreError('corrupt_state')
            else:
                incomplete.append(reset_id)
                if record['generation'] != state['generation'] + 1:
                    raise StoreError('corrupt_state')
        if incomplete != ([] if state['active'] is None else [state['active']]):
            raise StoreError('corrupt_state')
        baseline = state['baseline_generation']
        if (generations != set(range(baseline + 1, baseline + len(generations) + 1))
                or state['generation'] != baseline + len(generations) - len(incomplete)):
            raise StoreError('corrupt_state')

    def _load(self, descriptor):
        try:
            file = os.open('state.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                           dir_fd=descriptor)
        except FileNotFoundError:
            raise StoreError('state_uninitialized') from None
        try:
            metadata = os.fstat(file)
            private_file(metadata)
            if metadata.st_size > MAX_STATE:
                raise StoreError('state_too_large')
            with os.fdopen(file, 'rb', closefd=False) as stream:
                raw = stream.read(MAX_STATE + 1)
            if len(raw) > MAX_STATE:
                raise StoreError('state_too_large')
            state = decode(raw)
            self._validate(state)
            return state
        finally:
            os.close(file)

    def _save(self, descriptor, state):
        self._validate(state)
        raw = canonical(state) + b'\n'
        if len(raw) > MAX_STATE:
            raise StoreError('state_too_large')
        name = '.state.' + secrets.token_hex(16) + '.tmp'
        file = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                       0o600, dir_fd=descriptor)
        try:
            with os.fdopen(file, 'wb', closefd=False) as stream:
                stream.write(raw)
                stream.flush()
                os.fsync(file)
            os.replace(name, 'state.json', src_dir_fd=descriptor, dst_dir_fd=descriptor)
            os.fsync(descriptor)
        finally:
            os.close(file)
            try:
                os.unlink(name, dir_fd=descriptor)
            except FileNotFoundError:
                pass

    @contextlib.contextmanager
    def _locked(self, create=False, deadline=None):
        descriptor = lock = None
        try:
            descriptor = open_directory(self.state_dir, create)
            parent = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
            try:
                for part in os.path.dirname(self.data_dir).split('/')[1:]:
                    if not part:
                        continue
                    child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                    dir_fd=parent)
                    os.close(parent)
                    parent = child
                if os.fstat(parent).st_dev != os.fstat(descriptor).st_dev:
                    raise StoreError('state_volume_mismatch')
            finally:
                os.close(parent)
            for path in (self.data_dir, self.run_dir):
                ancestor = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
                try:
                    for part in path.split('/')[1:]:
                        try:
                            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                            dir_fd=ancestor)
                        except FileNotFoundError:
                            break
                        os.close(ancestor)
                        ancestor = child
                finally:
                    os.close(ancestor)
            flags = os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK
            new_lock = False
            if create:
                try:
                    lock = os.open('state.lock', flags | os.O_CREAT | os.O_EXCL,
                                   0o600, dir_fd=descriptor)
                    new_lock = True
                    os.fsync(lock)
                    os.fsync(descriptor)
                except FileExistsError:
                    lock = os.open('state.lock', flags, dir_fd=descriptor)
            else:
                lock = os.open('state.lock', flags, dir_fd=descriptor)
            private_file(os.fstat(lock))
            lock_deadline = time.monotonic() + 10
            if deadline is not None:
                lock_deadline = min(lock_deadline, deadline)
            while True:
                try:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    if time.monotonic() >= lock_deadline:
                        raise StoreError('state_busy') from None
                    time.sleep(0.01)
            yield (descriptor, new_lock) if create else descriptor
        except OSError:
            raise StoreError('state_unavailable') from None
        finally:
            if lock is not None:
                os.close(lock)
            if descriptor is not None:
                os.close(descriptor)

    def initialize(self):
        with self._locked(create=True) as (descriptor, new_lock):
            temporary = []
            with os.scandir(descriptor) as entries:
                for index, entry in enumerate(entries):
                    if index >= MAX_RECORDS + 2:
                        raise StoreError('state_directory_capacity')
                    if TEMPORARY.fullmatch(entry.name):
                        private_file(entry.stat(follow_symlinks=False))
                        temporary.append(entry.name)
            try:
                state = self._load(descriptor)
            except StoreError as error:
                if error.code != 'state_uninitialized' or not new_lock:
                    raise
                state = {'version': 1, 'network': self.network,
                         'baseline_generation': self.initial_generation,
                         'generation': self.initial_generation,
                         'active': None, 'records': {}}
                self._save(descriptor, state)
            if state['active']:
                record = state['records'][state['active']]
                self._bindings(record)
                if record['phase'] in ('bootstrapping', 'activating'):
                    record['phase'] = 'ambiguous'
                    self._save(descriptor, state)
            for name in temporary:
                os.unlink(name, dir_fd=descriptor)
            if temporary:
                os.fsync(descriptor)
            return copy.deepcopy(state['records'].get(state['active']))

    def _bindings(self, record):
        if record['phase'] != 'completed' and record['bindings'] != self.bindings:
            raise StoreError('bindings_mismatch')

    def _lookup(self, state, request, peer):
        record = state['records'].get(request['reset_id'])
        if record is not None:
            if (record['request_digest'] != request['request_digest']
                    or record['network'] != request['network']
                    or (record['peer_uid'], record['peer_gid']) != tuple(peer)):
                raise StoreError('identity_conflict')
        return record

    def admit(self, request, peer):
        self._authorize(peer)
        validate_request(request, self.network, 'reset')
        with self._locked() as descriptor:
            state = self._load(descriptor)
            record = self._lookup(state, request, peer)
            if record is not None:
                self._bindings(record)
                return copy.deepcopy(record)
            if state['active'] is not None:
                raise StoreError('reset_in_progress')
            if len(state['records']) >= MAX_RECORDS:
                raise StoreError('state_capacity')
            if state['generation'] == 0x7fffffffffffffff:
                raise StoreError('generation_exhausted')
            record = {'reset_id': request['reset_id'], 'request_digest': request['request_digest'],
                      'network': self.network, 'peer_uid': peer[0], 'peer_gid': peer[1],
                      'generation': state['generation'] + 1,
                      'genesis_timestamp_ms': time.time_ns() // 1000000,
                      'bindings': copy.deepcopy(self.bindings), 'phase': 'admitted', 'response': None}
            state['records'][request['reset_id']] = record
            state['active'] = request['reset_id']
            self._save(descriptor, state)
            return copy.deepcopy(record)

    def status(self, request, peer, deadline=None):
        self._authorize(peer)
        validate_request(request, self.network, 'status')
        with self._locked(deadline=deadline) as descriptor:
            record = self._lookup(self._load(descriptor), request, peer)
            if record is None:
                raise StoreError('unknown_reset')
            return copy.deepcopy(record)

    def active(self):
        with self._locked() as descriptor:
            state = self._load(descriptor)
            return copy.deepcopy(state['records'].get(state['active']))

    def transition(self, reset_id, expected_phase, new_phase, extra=None):
        if expected_phase not in NEXT or NEXT[expected_phase] != new_phase:
            raise StoreError('invalid_transition')
        if extra is not None:
            if new_phase != 'prepared' or not isinstance(extra, dict) or set(extra) != {'genesis'}:
                raise StoreError('invalid_transition_extra')
            self._genesis(extra['genesis'])
        if new_phase == 'prepared' and extra is None:
            raise StoreError('missing_genesis')
        with self._locked() as descriptor:
            state = self._load(descriptor)
            record = state['records'].get(reset_id)
            if record is None or record['phase'] != expected_phase or state['active'] != reset_id:
                raise StoreError('phase_conflict')
            self._bindings(record)
            record['phase'] = new_phase
            if extra:
                record.update(copy.deepcopy(extra))
            if new_phase == 'completed':
                record['response'] = {'state': 'reset', 'reset_id': reset_id,
                                      'generation': record['generation']}
                state['generation'] = record['generation']
                state['active'] = None
            self._save(descriptor, state)
            return copy.deepcopy(record)

    def generation(self):
        with self._locked() as descriptor:
            return self._load(descriptor)['generation']


def error_response(code):
    return {'error': {'code': code, 'retry': 'never', 'retry_after_seconds': 0}}


def read_request(connection):
    deadline = time.monotonic() + 10
    raw = bytearray()
    while b'\n' not in raw:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise StoreError('request_timeout')
        connection.settimeout(remaining)
        chunk = connection.recv(4097 - len(raw))
        if not chunk:
            raise StoreError('incomplete_request')
        raw.extend(chunk)
        if len(raw) > 4096:
            raise StoreError('request_too_large')
    if raw.count(b'\n') != 1 or not raw.endswith(b'\n'):
        raise StoreError('invalid_request')
    return bytes(raw[:-1]).removesuffix(b'\r')


def handle_connection(connection, store, wait_seconds=300):
    legacy = False
    try:
        _, uid, gid = struct.unpack('iII', connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
        peer = (uid, gid)
        store._authorize(peer)
        raw = read_request(connection)
        legacy = raw in (b'reset', b'status')
        legacy_reset = raw == b'reset'
        if raw == b'status':
            response = {'state': 'running', 'generation': store.generation()}
        else:
            if raw == b'reset':
                request = {'version': 1, 'operation': 'reset',
                           'reset_id': secrets.token_hex(16), 'network': store.network}
                request['request_digest'] = request_digest(request)
            else:
                request = decode(raw)
            if not isinstance(request, dict):
                raise StoreError('invalid_request')
            operation = request.get('operation')
            if operation == 'status':
                record = store.status(request, peer)
            elif operation == 'reset':
                record = store.admit(request, peer)
                deadline = time.monotonic() + min(max(wait_seconds, 0), 300)
                status_request = dict(request, operation='status')
                while record['phase'] not in ('completed', 'ambiguous'):
                    if time.monotonic() >= deadline:
                        raise StoreError('reset_timeout')
                    time.sleep(min(0.1, max(0, deadline - time.monotonic())))
                    record = store.status(status_request, peer, deadline=deadline)
            else:
                raise StoreError('invalid_operation')
            if record['phase'] == 'ambiguous':
                raise StoreError('reset_ambiguous')
            response = record['response'] or {'state': record['phase'],
                        'reset_id': record['reset_id'], 'generation': record['generation']}
            if legacy_reset:
                response = {'state': response['state'], 'reset_id': response['reset_id']}
    except StoreError as error:
        response = error_response(error.code)
    except (OSError, ValueError, TypeError, RecursionError):
        response = error_response('request_unavailable')
    try:
        connection.settimeout(10)
        encoded = (json.dumps(response, separators=(',', ':'), allow_nan=False).encode()
                   if legacy else canonical(response))
        connection.sendall(encoded + b'\n')
    except OSError:
        pass
    finally:
        connection.close()


def serve(store, socket_path):
    path = os.path.abspath(socket_path)
    if os.path.dirname(path) != store.run_dir:
        raise StoreError('unsafe_socket_path')
    metadata = os.lstat(store.run_dir)
    if (not stat.S_ISDIR(metadata.st_mode) or metadata.st_uid != os.geteuid()
            or metadata.st_mode & 0o002):
        raise StoreError('unsafe_run_directory')
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as listener:
        listener.bind(path)
        os.chown(path, os.geteuid(), store.allowed_gid)
        os.chmod(path, 0o660)
        listener.listen(32)
        slots = threading.BoundedSemaphore(32)
        while True:
            connection, _ = listener.accept()
            if not slots.acquire(blocking=False):
                connection.close()
                continue
            def worker(client):
                try:
                    handle_connection(client, store)
                finally:
                    slots.release()
            threading.Thread(target=worker, args=(connection,), daemon=True).start()


def parent_death_signal():
    parent = os.getppid()
    if parent == 1:
        raise StoreError('supervisor_unavailable')
    libc = ctypes.CDLL(None, use_errno=True)
    libc.prctl.argtypes = [ctypes.c_int, ctypes.c_ulong, ctypes.c_ulong,
                          ctypes.c_ulong, ctypes.c_ulong]
    libc.prctl.restype = ctypes.c_int
    if libc.prctl(1, signal.SIGTERM, 0, 0, 0) != 0 or os.getppid() != parent:
        raise StoreError('supervisor_unavailable')


def replica_generation(state_dir, generation):
    descriptor = open_directory(state_dir)
    lock = None
    try:
        lock = os.open('state.lock', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                       dir_fd=descriptor)
        private_file(os.fstat(lock))
        fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
        file = os.open('state.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                       dir_fd=descriptor)
        try:
            private_file(os.fstat(file))
            if os.fstat(file).st_size > MAX_STATE:
                raise StoreError('state_too_large')
            with os.fdopen(file, 'rb', closefd=False) as stream:
                raw = stream.read(MAX_STATE + 1)
        finally:
            os.close(file)
        if len(raw) > MAX_STATE:
            raise StoreError('state_too_large')
        state = decode(raw)
        if not isinstance(state, dict):
            raise StoreError('corrupt_state')
        reader = Store.__new__(Store)
        reader.network = state.get('network')
        reader._validate(state)
        if state['active'] is None:
            allowed = state['generation'] == generation
        else:
            record = state['records'][state['active']]
            allowed = record['phase'] == 'activating' and record['generation'] == generation
        if not allowed:
            raise StoreError('generation_not_admitted')
    finally:
        if lock is not None:
            os.close(lock)
        os.close(descriptor)


def main():
    if len(sys.argv) > 1 and sys.argv[1] in ('exec-daemon', 'replica-generation'):
        try:
            if sys.argv[1] == 'exec-daemon':
                if len(sys.argv) < 4 or sys.argv[2] != '--':
                    raise StoreError('invalid_command')
                parent_death_signal()
                os.execvp(sys.argv[3], sys.argv[3:])
            parser = argparse.ArgumentParser()
            parser.add_argument('action')
            parser.add_argument('--state-dir', required=True)
            parser.add_argument('--generation', required=True, type=int)
            arguments = parser.parse_args()
            replica_generation(arguments.state_dir, arguments.generation)
            return 0
        except StoreError as error:
            print(canonical(error_response(error.code)).decode())
        except (OSError, ValueError, TypeError, RecursionError):
            print(canonical(error_response('state_unavailable')).decode())
        return 1
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('initialize', 'active', 'phase', 'complete', 'generation', 'serve'))
    for name in ('state-dir', 'data-dir', 'run-dir', 'bindings'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--network', type=int, required=True)
    parser.add_argument('--allowed-gid', type=int, default=os.getegid())
    parser.add_argument('--initial-generation', type=int, default=1)
    parser.add_argument('--reset-id')
    parser.add_argument('--expected-phase')
    parser.add_argument('--new-phase')
    parser.add_argument('--extra', '--extra-json', dest='extra')
    parser.add_argument('--socket')
    arguments = parser.parse_args()
    try:
        if arguments.action == 'serve':
            parent_death_signal()
        descriptor = os.open(arguments.bindings, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            metadata = os.fstat(descriptor)
            if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.geteuid()
                    or metadata.st_mode & 0o022 or metadata.st_size > MAX_BINDINGS):
                raise StoreError('unsafe_bindings_file')
            with os.fdopen(descriptor, 'rb', closefd=False) as stream:
                bindings = decode(stream.read(MAX_BINDINGS + 1))
        finally:
            os.close(descriptor)
        store = Store(arguments.state_dir, arguments.data_dir, arguments.run_dir,
                      arguments.network, bindings, arguments.allowed_gid,
                      arguments.initial_generation)
        if arguments.action == 'initialize':
            result = store.initialize()
        elif arguments.action == 'active':
            result = store.active()
        elif arguments.action == 'generation':
            result = store.generation()
        elif arguments.action in ('phase', 'complete'):
            result = store.transition(arguments.reset_id, arguments.expected_phase,
                                      'completed' if arguments.action == 'complete' else arguments.new_phase,
                                      decode(arguments.extra) if arguments.extra else None)
        else:
            if not arguments.socket:
                raise StoreError('missing_socket')
            serve(store, arguments.socket)
            return 0
        print(canonical(result).decode())
        return 0
    except StoreError as error:
        print(canonical(error_response(error.code)).decode())
    except (OSError, ValueError, TypeError, RecursionError):
        print(canonical(error_response('state_unavailable')).decode())
    return 1


if __name__ == '__main__':
    sys.exit(main())
