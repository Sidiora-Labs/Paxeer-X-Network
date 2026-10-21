"""Actual-process lifecycle checks; no simulated provider success."""
import importlib.util
import pathlib
import socket
import subprocess
import sys
import tempfile
import time
import unittest

spec = importlib.util.spec_from_file_location('l18_qualify', pathlib.Path(__file__).with_name('qualify.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class Lifecycle(unittest.TestCase):
    def setUp(self):
        module.PORT = 21802

    def child(self, code):
        child = subprocess.Popen([sys.executable, '-c', code], start_new_session=True)
        self.addCleanup(module.stop, child)
        return child

    def test_occupied_port_preserves_peer(self):
        with socket.socket() as peer:
            peer.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            peer.bind(('127.0.0.1', 21802))
            peer.listen()
            with self.assertRaisesRegex(RuntimeError, 'occupied'):
                module.refuse_occupied_port()
            with socket.create_connection(('127.0.0.1', 21802)):
                accepted, _ = peer.accept()
                accepted.close()

    def test_dead_child_never_accepts_peer_readiness(self):
        with socket.socket() as peer:
            peer.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            peer.bind(('127.0.0.1', 21802))
            peer.listen()
            child = self.child('pass')
            child.wait(timeout=5)
            with self.assertRaisesRegex(RuntimeError, 'exited'):
                module.wait_ready(child, attempts=1)

    def test_exhaustion_fails_and_cleanup_is_bounded(self):
        child = self.child('import time; time.sleep(30)')
        with self.assertRaisesRegex(RuntimeError, 'deadline exhausted'):
            module.wait_ready(child, attempts=2)
        started = time.monotonic()
        module.stop(child)
        self.assertIsNotNone(child.poll())
        self.assertLess(time.monotonic() - started, 12)

    def test_live_child_cannot_borrow_peer_listener(self):
        with socket.socket() as peer:
            peer.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            peer.bind(('127.0.0.1', 21802))
            peer.listen()
            child = self.child('import time; time.sleep(30)')
            with self.assertRaisesRegex(RuntimeError, 'not alive and listening'):
                module.assert_owned(child)

    def test_launcher_signal_unwinds_child_group_cleanup(self):
        with tempfile.TemporaryDirectory(prefix='l18-signal-') as private:
            pidfile = pathlib.Path(private) / 'child.pid'
            child_code = ("import os,socket,time,pathlib; "
                          "s=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);s.bind(('127.0.0.1',21802));s.listen(); "
                          f"pathlib.Path({str(pidfile)!r}).write_text(str(os.getpid())); "
                          "time.sleep(30)")
            launcher_code = ("import importlib.util,subprocess,sys; "
                             f"s=importlib.util.spec_from_file_location('q',{module.__file__!r}); "
                             "q=importlib.util.module_from_spec(s);s.loader.exec_module(q); "
                             "q.install_signal_handlers(); "
                             f"q.run_bounded([sys.executable,'-c',{child_code!r}],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)")
            launcher = self.child(launcher_code)
            deadline = time.monotonic() + 5
            while not pidfile.exists() and time.monotonic() < deadline:
                time.sleep(.02)
            self.assertTrue(pidfile.exists(), 'owned nested child failed to start')
            child_pid = int(pidfile.read_text())
            launcher.terminate()
            launcher.wait(timeout=12)
            self.assertEqual(launcher.returncode, 143)
            self.assertFalse(pathlib.Path(f'/proc/{child_pid}').exists(), 'nested child survived launcher SIGTERM')
            module.refuse_occupied_port()

    def test_existing_evidence_is_not_reused_or_truncated(self):
        with tempfile.TemporaryDirectory(prefix='l18-evidence-') as private:
            old = pathlib.Path(private) / 'repeat-go.jsonl'
            old.write_text('previous failed result\n')
            result = subprocess.run([sys.executable, str(pathlib.Path(module.__file__)),
                                     '--run', 'repeat', '--output-dir', private],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b'historical output will not be reused', result.stderr)
            self.assertEqual(old.read_text(), 'previous failed result\n')
            self.assertEqual(list(pathlib.Path(private).iterdir()), [old])


if __name__ == '__main__':
    unittest.main(verbosity=2)
