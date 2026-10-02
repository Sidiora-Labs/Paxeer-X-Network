#!/usr/bin/env python3
import hashlib
import importlib.util
import json
import multiprocessing
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest


HELPER = Path(__file__).resolve().parents[1] / 'reset_state.py'
SPEC = importlib.util.spec_from_file_location('reset_state', HELPER)
reset_state = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(reset_state)


def request_for(reset_id='a' * 32, network=4242, operation='reset'):
    request = {'version': 1, 'operation': operation,
               'reset_id': reset_id, 'network': network}
    request['request_digest'] = reset_state.request_digest(request)
    return request


def admit_process(paths, request, queue):
    try:
        store = reset_state.Store(*paths, 4242, {'producer': 'canonical'})
        queue.put(('ok', store.admit(request, (os.geteuid(), os.getegid()))))
    except reset_state.StoreError as error:
        queue.put(('error', error.code))


class ResetStateTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='lx-reset-state-')
        self.root = Path(self.temporary.name)
        self.data = self.root / 'data'
        self.run = self.root / 'run'
        self.state = self.root / 'state'
        self.data.mkdir(mode=0o700)
        self.run.mkdir(mode=0o700)
        self.bindings = {'producer': 'canonical'}
        self.store = self.new_store()
        self.store.initialize()
        self.peer = (os.geteuid(), os.getegid())
        artifact = self.data / 'genesis.manifest'
        artifact.write_bytes(b'unit state-transition artifact\n')
        self.genesis = {'genesis.manifest': hashlib.sha256(artifact.read_bytes()).hexdigest()}
        self.processes = []

    def tearDown(self):
        for process in self.processes:
            if process.poll() is None:
                process.terminate()
            process.wait(timeout=10)
        if self.state.exists() and not self.state.is_symlink():
            self.state.chmod(0o700)
            for path in self.state.iterdir():
                if not path.is_symlink():
                    path.chmod(0o600)
        self.temporary.cleanup()

    def new_store(self, **changes):
        options = {'state_dir': self.state, 'data_dir': self.data, 'run_dir': self.run,
                   'network': 4242, 'bindings': self.bindings}
        options.update(changes)
        return reset_state.Store(**options)

    def assert_code(self, code, function, *args):
        with self.assertRaises(reset_state.StoreError) as caught:
            function(*args)
        self.assertEqual(caught.exception.code, code)

    def finish(self, record):
        identity = record['reset_id']
        for phase, next_phase in reset_state.NEXT.items():
            extra = {'genesis': self.genesis} if next_phase == 'prepared' else None
            record = self.store.transition(identity, phase, next_phase, extra)
        return record

    def status(self, identity='a' * 32):
        return request_for(identity, operation='status')

    def test_original_outcome_survives_new_store_and_removed_run_directory(self):
        first = self.store.admit(request_for(), self.peer)
        self.assertEqual(first['generation'], 2)
        self.assertEqual(self.store.generation(), 1)
        self.assertEqual(first, self.new_store().admit(request_for(), self.peer))
        completed = self.finish(first)
        durable = (self.state / 'state.json').read_bytes()
        shutil.rmtree(self.run)
        self.run.mkdir(mode=0o700)
        other = self.new_store(bindings={'producer': 'changed after completion'})
        other.initialize()
        self.assertEqual(other.admit(request_for(), self.peer), completed)
        self.assertEqual(other.status(self.status(), self.peer), completed)
        self.assertEqual(completed['response'], {'state': 'reset', 'reset_id': 'a' * 32, 'generation': 2})
        self.assertEqual(other.generation(), 2)
        self.assertIsNone(other.active())
        self.assertEqual((self.state / 'state.json').read_bytes(), durable)

    def test_existing_generation_becomes_a_persistent_baseline_only_once(self):
        directory = self.root / 'upgraded-state'
        store = self.new_store(state_dir=directory, initial_generation=17)
        store.initialize()
        self.assertEqual(store.generation(), 17)
        self.assertEqual(json.loads((directory / 'state.json').read_bytes())['records'], {})
        record = store.admit(request_for(), self.peer)
        self.assertEqual(record['generation'], 18)
        reopened = self.new_store(state_dir=directory, initial_generation=1)
        reopened.initialize()
        self.assertEqual(reopened.generation(), 17)
        self.assertEqual(reopened.active(), record)

    def test_real_processes_serialize_duplicate_admission(self):
        context = multiprocessing.get_context('fork')
        queue = context.Queue()
        paths = (str(self.state), str(self.data), str(self.run))
        processes = [context.Process(target=admit_process, args=(paths, request_for(), queue))
                     for _ in range(8)]
        try:
            for process in processes:
                process.start()
            results = [queue.get(timeout=20) for _ in processes]
            for process in processes:
                process.join(timeout=20)
                self.assertEqual(process.exitcode, 0)
            self.assertTrue(all(result[0] == 'ok' for result in results), results)
            self.assertTrue(all(result[1] == results[0][1] for result in results))
            self.assertEqual(len(json.loads((self.state / 'state.json').read_bytes())['records']), 1)
        finally:
            for process in processes:
                if process.is_alive():
                    process.kill()
                    process.join()
            queue.close()

    def test_refusals_do_not_mutate_state(self):
        request = request_for()
        self.store.admit(request, self.peer)
        before = (self.state / 'state.json').read_bytes()
        malformed = [dict(request, reset_id='A' * 32), dict(request, reset_id='a' * 31),
                     dict(request, reset_id='../' + 'a' * 29), dict(request, version=True),
                     dict(request, version=2), dict(request, network=True),
                     dict(request, network=4243), dict(request, request_digest='0' * 64),
                     dict(request, request_digest='F' * 64), dict(request, operation='status'),
                     dict(request, extra='not allowed'), {}, [], None]
        for invalid in malformed:
            with self.subTest(invalid=invalid):
                with self.assertRaises(reset_state.StoreError):
                    self.store.admit(invalid, self.peer)
                self.assertEqual((self.state / 'state.json').read_bytes(), before)
        self.assert_code('identity_conflict', self.store.admit, request,
                         (self.peer[0], self.peer[1] + 1))
        self.assert_code('reset_in_progress', self.store.admit, request_for('b' * 32), self.peer)
        self.assert_code('unknown_reset', self.store.status, self.status('b' * 32), self.peer)
        self.assertEqual((self.state / 'state.json').read_bytes(), before)

    def test_unauthorized_peer_is_rejected_before_state_directory_access(self):
        absent = self.root / 'absent'
        store = self.new_store(state_dir=absent)
        peer = (os.geteuid() + 1, os.getegid() + 1)
        self.assert_code('unauthorized_peer', store.admit, request_for(), peer)
        self.assert_code('unauthorized_peer', store.status, self.status(), peer)
        self.assertFalse(absent.exists())

    def test_group_authorized_peer_is_bound_to_original_uid_and_gid(self):
        peer = (os.geteuid() + 1, os.getegid())
        record = self.store.admit(request_for(), peer)
        self.assertEqual(record['peer_uid'], peer[0])
        self.assert_code('identity_conflict', self.store.status, self.status(), self.peer)

    def test_binding_change_refuses_incomplete_mutation(self):
        self.store.admit(request_for(), self.peer)
        before = (self.state / 'state.json').read_bytes()
        changed = self.new_store(bindings={'producer': 'different'})
        self.assert_code('bindings_mismatch', changed.initialize)
        self.assert_code('bindings_mismatch', changed.admit, request_for(), self.peer)
        self.assert_code('bindings_mismatch', changed.transition, 'a' * 32, 'admitted', 'stopping')
        self.assertEqual((self.state / 'state.json').read_bytes(), before)

    def test_initialize_is_the_only_recovery_entry_and_preserves_frozen_identity(self):
        for phase in ('admitted', 'stopping', 'bootstrapping', 'prepared', 'activating'):
            with self.subTest(phase=phase), tempfile.TemporaryDirectory(dir=self.root) as directory:
                store = self.new_store(state_dir=Path(directory) / 'state')
                store.initialize()
                record = store.admit(request_for(), self.peer)
                while record['phase'] != phase:
                    current = record['phase']
                    following = reset_state.NEXT[current]
                    record = store.transition(record['reset_id'], current, following,
                                              {'genesis': self.genesis} if following == 'prepared' else None)
                reopened = self.new_store(state_dir=Path(directory) / 'state')
                self.assertEqual(reopened.active(), record)
                recovered = reopened.initialize()
                self.assertEqual(recovered['genesis_timestamp_ms'], record['genesis_timestamp_ms'])
                self.assertEqual(recovered['bindings'], record['bindings'])
                self.assertEqual(recovered['generation'], record['generation'])
                self.assertEqual(recovered['phase'], 'ambiguous' if phase in ('bootstrapping', 'activating') else phase)
                if recovered['phase'] == 'ambiguous':
                    self.assert_code('reset_in_progress', reopened.admit, request_for('b' * 32), self.peer)
                    self.assert_code('invalid_transition', reopened.transition,
                                     record['reset_id'], 'ambiguous', 'bootstrapping')

    def test_transition_order_and_extra_fields_are_not_mutable(self):
        self.store.admit(request_for(), self.peer)
        self.assert_code('invalid_transition', self.store.transition, 'a' * 32, 'admitted', 'completed')
        self.assert_code('phase_conflict', self.store.transition, 'a' * 32, 'stopping', 'bootstrapping')
        self.assert_code('invalid_transition_extra', self.store.transition, 'a' * 32, 'admitted', 'stopping', {'generation': 9})
        self.store.transition('a' * 32, 'admitted', 'stopping')
        self.store.transition('a' * 32, 'stopping', 'bootstrapping')
        self.assert_code('missing_genesis', self.store.transition, 'a' * 32, 'bootstrapping', 'prepared')
        for genesis in ({}, {'../escape': 'a' * 64}, {'/absolute': 'a' * 64}, {'safe': 'G' * 64}, {'safe': 'a' * 63}):
            self.assert_code('invalid_genesis', self.store.transition, 'a' * 32, 'bootstrapping', 'prepared', {'genesis': genesis})

    def test_state_location_modes_links_and_owner_are_refused(self):
        for location in (self.data / 'state', self.run / 'state', self.root):
            with self.assertRaises(reset_state.StoreError):
                self.new_store(state_dir=location)
        self.state.chmod(0o500)
        self.assert_code('unsafe_state_directory', self.store.generation)
        self.state.chmod(0o700)
        state_file = self.state / 'state.json'
        state_file.chmod(0o400)
        self.assert_code('unsafe_state_file', self.store.generation)
        state_file.chmod(0o600)
        hardlink = self.root / 'hardlink'
        os.link(state_file, hardlink)
        self.assert_code('unsafe_state_file', self.store.generation)
        hardlink.unlink()
        original = state_file.read_bytes()
        state_file.unlink()
        state_file.symlink_to(self.root / 'elsewhere')
        self.assert_code('state_unavailable', self.store.generation)
        state_file.unlink()
        state_file.write_bytes(original)
        state_file.chmod(0o600)
        alias = self.root / 'alias'
        alias.symlink_to(self.state, target_is_directory=True)
        self.assert_code('state_unavailable', self.new_store(state_dir=alias).generation)
        if os.geteuid() == 0:
            os.chown(state_file, 1, -1)
            try:
                self.assert_code('unsafe_state_file', self.store.generation)
            finally:
                os.chown(state_file, 0, -1)

    def test_corrupt_records_and_missing_journal_never_initialize_over(self):
        self.store.admit(request_for(), self.peer)
        path = self.state / 'state.json'
        original = path.read_bytes()
        corrupted = [b'{', b'{"version":1,"version":1}', b'null', b'[]']
        parsed = json.loads(original)
        parsed['records']['a' * 32]['genesis_timestamp_ms'] = 0
        corrupted.append(reset_state.canonical(parsed))
        parsed = json.loads(original)
        parsed['records']['a' * 32]['response'] = {'state': 'reset'}
        corrupted.append(reset_state.canonical(parsed))
        parsed = json.loads(original)
        parsed['active'] = None
        corrupted.append(reset_state.canonical(parsed))
        for content in corrupted:
            path.write_bytes(content)
            with self.assertRaises(reset_state.StoreError):
                self.store.initialize()
            self.assertEqual(path.read_bytes(), content)
        path.write_bytes(original)
        path.unlink()
        self.assert_code('state_uninitialized', self.store.initialize)
        self.assertFalse(path.exists())

    def test_replica_generation_refuses_incomplete_and_ambiguous_phases(self):
        reset_state.replica_generation(self.state, 1)
        record = self.store.admit(request_for(), self.peer)
        while record['phase'] != 'activating':
            self.assert_code('generation_not_admitted', reset_state.replica_generation, self.state, record['generation'])
            self.assert_code('generation_not_admitted', reset_state.replica_generation, self.state, 1)
            phase = record['phase']
            following = reset_state.NEXT[phase]
            record = self.store.transition(record['reset_id'], phase, following,
                                          {'genesis': self.genesis} if following == 'prepared' else None)
        reset_state.replica_generation(self.state, record['generation'])
        self.store.initialize()
        self.assert_code('generation_not_admitted', reset_state.replica_generation, self.state, record['generation'])

    def start_server(self):
        bindings = self.run / 'bindings.json'
        bindings.write_bytes(reset_state.canonical(self.bindings))
        bindings.chmod(0o600)
        path = self.run / 'supervisor.sock'
        command = [sys.executable, str(HELPER), 'serve', '--state-dir', str(self.state),
                   '--data-dir', str(self.data), '--run-dir', str(self.run), '--network', '4242',
                   '--bindings', str(bindings), '--socket', str(path), '--allowed-gid', str(os.getegid())]
        process = subprocess.Popen(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.processes.append(process)
        deadline = time.monotonic() + 10
        while not path.exists():
            self.assertIsNone(process.poll())
            self.assertLess(time.monotonic(), deadline)
            time.sleep(0.01)
        self.assertEqual(path.stat().st_mode & 0o777, 0o660)
        self.assertEqual(path.stat().st_gid, os.getegid())
        return path

    def exchange_raw(self, path, raw):
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
            connection.settimeout(10)
            connection.connect(str(path))
            connection.sendall(raw)
            response = bytearray()
            while not response.endswith(b'\n'):
                chunk = connection.recv(4096)
                self.assertTrue(chunk)
                response.extend(chunk)
            return bytes(response)

    def exchange(self, path, raw):
        return json.loads(self.exchange_raw(path, raw))

    def test_actual_socket_peer_credentials_legacy_status_and_replayed_result(self):
        completed = self.finish(self.store.admit(request_for(), self.peer))
        path = self.start_server()
        self.assertEqual(self.exchange_raw(path, b'status\n'), b'{"state":"running","generation":2}\n')
        self.assertEqual(self.exchange(path, b'status\n'), {'state': 'running', 'generation': 2})
        for request in (request_for(), self.status()):
            self.assertEqual(self.exchange(path, reset_state.canonical(request) + b'\n'), completed['response'])
        self.assertEqual(self.exchange(path, b'{"version":1,"version":1}\n')['error']['code'], 'invalid_json')
        self.assertEqual(self.exchange(path, b'x' * 4097 + b'\n')['error']['code'], 'request_too_large')

    def test_legacy_reset_waits_on_real_durable_operation(self):
        path = self.start_server()
        responses = []
        thread = threading.Thread(target=lambda: responses.append(self.exchange(path, b'reset\n')))
        thread.start()
        deadline = time.monotonic() + 5
        while self.store.active() is None:
            self.assertLess(time.monotonic(), deadline)
            time.sleep(0.01)
        record = self.store.active()
        completed = self.finish(record)
        thread.join(timeout=10)
        self.assertFalse(thread.is_alive())
        self.assertEqual(responses, [{'state': 'reset', 'reset_id': completed['reset_id']}])

    def test_capacity_never_prunes_completed_original_outcomes(self):
        first = None
        for index in range(reset_state.MAX_RECORDS):
            request = request_for(format(index, '032x'))
            completed = self.finish(self.store.admit(request, self.peer))
            if first is None:
                first = completed
        before = (self.state / 'state.json').read_bytes()
        self.assert_code('state_capacity', self.store.admit,
                         request_for(format(reset_state.MAX_RECORDS, '032x')), self.peer)
        self.assertEqual(self.store.admit(request_for('0' * 32), self.peer), first)
        self.assertEqual((self.state / 'state.json').read_bytes(), before)
        self.assertEqual(self.store.generation(), reset_state.MAX_RECORDS + 1)

    def test_oversized_journal_is_refused_without_reading_or_replacing_it(self):
        path = self.state / 'state.json'
        with path.open('r+b') as stream:
            stream.truncate(reset_state.MAX_STATE + 1)
        self.assert_code('state_too_large', self.store.initialize)
        self.assertEqual(path.stat().st_size, reset_state.MAX_STATE + 1)

    def test_recovery_cleans_only_owned_atomic_write_debris(self):
        pending = self.state / ('.state.' + 'a' * 32 + '.tmp')
        pending.write_bytes(b'interrupted durable write')
        pending.chmod(0o600)
        original = (self.state / 'state.json').read_bytes()
        self.store.initialize()
        self.assertFalse(pending.exists())
        self.assertEqual((self.state / 'state.json').read_bytes(), original)
        pending.symlink_to(self.root / 'must-not-touch')
        self.assert_code('unsafe_state_file', self.store.initialize)
        self.assertTrue(pending.is_symlink())

    def test_real_unprivileged_filesystem_eacces_is_typed_and_has_no_journal(self):
        blocked = self.root / 'blocked'
        blocked.mkdir(mode=0o700)
        self.root.chmod(0o711)
        blocked.chmod(0)
        code = '''import errno, importlib.util, json, os, sys
spec = importlib.util.spec_from_file_location('reset_state', sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
if os.geteuid() == 0:
    os.setgroups([])
    os.setgid(65534)
    os.setuid(65534)
try:
    descriptor = os.open(sys.argv[2], os.O_RDONLY | os.O_DIRECTORY)
except OSError as error:
    assert error.errno == errno.EACCES
else:
    os.close(descriptor)
    raise AssertionError('unprivileged open unexpectedly succeeded')
store = module.Store(sys.argv[2] + '/state', sys.argv[3], sys.argv[4], 4242, {})
try:
    store.initialize()
except module.StoreError as error:
    assert error.code == 'state_unavailable'
else:
    raise AssertionError('state persistence unexpectedly succeeded')
print('EACCES state_unavailable')
'''
        try:
            result = subprocess.run([sys.executable, '-c', code, str(HELPER), str(blocked),
                                     str(self.data), str(self.run)], capture_output=True,
                                    timeout=10, check=False)
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertEqual(result.stdout, b'EACCES state_unavailable\n')
        finally:
            blocked.chmod(0o700)
        self.assertEqual(list(blocked.iterdir()), [])

    def test_request_and_binding_bounds(self):
        with self.assertRaises(reset_state.StoreError) as caught:
            self.new_store(bindings={'large': 'x' * reset_state.MAX_BINDINGS})
        self.assertEqual(caught.exception.code, 'bindings_too_large')
        for value in (b'{"a":NaN}', b'{"a":Infinity}', b'\xff'):
            self.assert_code('invalid_json', reset_state.decode, value)
        first, second = socket.socketpair()
        try:
            first.sendall(b'status\nreset\n')
            self.assert_code('invalid_request', reset_state.read_request, second)
        finally:
            first.close()
            second.close()


if __name__ == '__main__':
    unittest.main()
