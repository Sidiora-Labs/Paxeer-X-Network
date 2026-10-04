#!/usr/bin/env python3
import base64
import os
from pathlib import Path
import secrets
import socket
import stat
import sys
import tempfile
import threading
import unittest

NODE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(NODE))
import generation_transport as transport
from reset_state import Store, StoreError, canonical, decode


class GenerationTransportTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.data = self.root / 'data'
        self.state = self.root / 'state'
        self.run = self.root / 'run'
        for directory in (self.data, self.state, self.run):
            directory.mkdir(mode=0o700)
        Store(str(self.state), str(self.data), str(self.run), 1, {}).initialize()
        self.broker = transport.Broker(str(self.data), str(self.state), str(self.run), os.geteuid(), os.getegid())
        self.capabilities = self.state / 'generation-authorizations'
        self.capabilities.mkdir(mode=0o700)
        self.capability = secrets.token_bytes(32)
        for slot in (1, 2):
            descriptor = os.open(self.capabilities / ('slot-' + str(slot) + '.cap'), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            try:
                os.write(descriptor, self.capability if slot == 1 else secrets.token_bytes(32))
            finally:
                os.close(descriptor)

    def tearDown(self):
        self.temporary.cleanup()

    def request(self, **updates):
        result = {'version': 1, 'operation': 'identity', 'slot': 1,
                  'capability': base64.b64encode(self.capability).decode(),
                  'expected_generation': None}
        result.update(updates)
        return result

    def refusal(self, request, expected):
        with self.assertRaises(StoreError) as error:
            self.broker.request(request, (os.geteuid(), os.getegid()))
        self.assertEqual(error.exception.code, expected)

    def test_same_uid_cannot_read_other_slot(self):
        self.refusal(self.request(slot=2), 'unauthorized_slot')

    def test_closed_requests_cannot_name_paths(self):
        self.refusal(self.request(path='../../secrets'), 'invalid_request')

    def test_capability_requires_exact_standard_encoding(self):
        self.refusal(self.request(capability=base64.b64encode(self.capability).decode() + '\n'), 'unauthorized_slot')
        self.refusal(self.request(slot=True), 'unauthorized_slot')

    def test_actual_peer_credentials_refuse_socket_request(self):
        self.broker.allowed_uid = (os.geteuid() + 1) % 0xffffffff
        client, server = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
        worker = threading.Thread(target=self.broker.handle, args=(server,))
        worker.start()
        try:
            client.settimeout(5)
            client.sendall(canonical(self.request()) + b'\n')
            raw, ancillary, flags, _ = client.recvmsg(8192, socket.CMSG_SPACE(16 * 4))
            self.assertEqual(decode(raw), {'error': {'code': 'generation_unavailable'}})
            self.assertEqual(ancillary, [])
            self.assertEqual(flags & socket.MSG_CTRUNC, 0)
        finally:
            client.close()
            worker.join(timeout=5)
        self.assertFalse(worker.is_alive())

    def test_absent_canonical_generation_cannot_return_fds(self):
        with self.assertRaises(FileNotFoundError):
            self.broker.request(self.request(), (os.geteuid(), os.getegid()))

    def test_unsafe_capability_permissions_refuse(self):
        os.chmod(self.capabilities / 'slot-1.cap', 0o640)
        self.refusal(self.request(), 'unsafe_state_file')

    def test_nofollow_file_and_directory_opens(self):
        real = self.data / 'real'
        real.write_bytes(b'bounded actual filesystem bytes')
        os.chmod(real, 0o600)
        (self.data / 'link').symlink_to(real)
        (self.data / 'linked-directory').symlink_to(self.state, target_is_directory=True)
        directory = transport.open_directory(str(self.data))
        try:
            with self.assertRaises(OSError):
                transport.artifact(directory, 'link')
            with self.assertRaises(OSError):
                transport.artifact(directory, 'linked-directory/state.json')
            with self.assertRaises(StoreError):
                transport.artifact(directory, '../state/state.json')
            descriptor = transport.artifact(directory, 'real', 64)
            try:
                self.assertEqual(transport.contents(descriptor, 64), real.read_bytes())
                with self.assertRaises(OSError):
                    os.write(descriptor, b'forbidden')
            finally:
                os.close(descriptor)
        finally:
            os.close(directory)

    def test_data_mode_and_socket_path_preserve_protected_boundary(self):
        os.chmod(self.data, 0o750)
        self.refusal(self.request(), 'unsafe_state_directory')
        os.chmod(self.data, 0o700)
        with self.assertRaises(StoreError) as error:
            self.broker.serve(str(self.root / 'unleased.sock'))
        self.assertEqual(error.exception.code, 'unsafe_socket_path')
        self.assertEqual(stat.S_IMODE(os.stat(self.data).st_mode), 0o700)


if __name__ == '__main__':
    unittest.main()
