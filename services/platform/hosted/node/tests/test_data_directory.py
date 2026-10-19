import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'data_directory.py'


class DataDirectoryTests(unittest.TestCase):
    def run_directory(self, operation, path):
        return subprocess.run([sys.executable, str(SCRIPT), operation, str(path)],
                              capture_output=True, text=True, check=False)

    def test_force_refuses_directory_and_parent_symlinks(self):
        with tempfile.TemporaryDirectory(prefix='node-data-directory-') as directory:
            root = Path(directory)
            target = root / 'target'
            target.mkdir()
            sentinel = target / 'retained'
            sentinel.write_text('retained')
            link = root / 'link'
            link.symlink_to(target, target_is_directory=True)
            for path in (link, link / 'child'):
                for operation in ('prepare', 'clear'):
                    result = self.run_directory(operation, path)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(sentinel.read_text(), 'retained')

    def test_force_removes_owned_contents_without_following_child_symlink(self):
        with tempfile.TemporaryDirectory(prefix='node-data-directory-') as directory:
            root = Path(directory)
            target = root / 'target'
            target.mkdir()
            sentinel = target / 'retained'
            sentinel.write_text('retained')
            data = root / 'data'
            self.assertEqual(self.run_directory('prepare', data).returncode, 0)
            (data / 'link').symlink_to(target, target_is_directory=True)
            (data / 'nested').mkdir()
            (data / 'nested' / 'file').write_text('discard')
            self.assertEqual(self.run_directory('clear', data).returncode, 0)
            self.assertEqual(list(data.iterdir()), [])
            self.assertEqual(sentinel.read_text(), 'retained')
            self.assertEqual(data.stat().st_mode & 0o777, 0o700)

    def test_stage_retains_live_data_and_is_idempotent(self):
        with tempfile.TemporaryDirectory(prefix='node-generation-stage-') as directory:
            root = Path(directory)
            data = root / 'data'
            state = root / 'state'
            data.mkdir(mode=0o700)
            state.mkdir(mode=0o700)
            sentinel = data / 'retained-economic-generation'
            sentinel.write_text('retained')
            identity = 'a' * 32
            command = [sys.executable, str(SCRIPT), 'prepare-stage',
                       str(data), str(state), identity]
            first = subprocess.run(command, capture_output=True, text=True, check=False)
            self.assertEqual(first.returncode, 0, first.stderr)
            stage = state / 'generations' / identity
            self.assertEqual(first.stdout.strip(), str(stage))
            for path in (stage.parent, stage, stage / 'data', stage / 'run'):
                self.assertEqual(path.stat().st_mode & 0o777, 0o700)
            (stage / 'run').chmod(0o750)
            second = subprocess.run(command, capture_output=True, text=True, check=False)
            self.assertEqual(second.returncode, 0, second.stderr)
            self.assertEqual(first.stdout, second.stdout)
            self.assertEqual(sentinel.read_text(), 'retained')
            self.assertEqual(list((stage / 'data').iterdir()), [])

    def test_stage_refuses_aliases_overlap_and_invalid_identity(self):
        with tempfile.TemporaryDirectory(prefix='node-generation-refusal-') as directory:
            root = Path(directory)
            data = root / 'data'
            state = root / 'state'
            data.mkdir(mode=0o700)
            state.mkdir(mode=0o700)
            sentinel = data / 'retained-economic-generation'
            sentinel.write_text('retained')
            alias = root / 'alias'
            alias.symlink_to(state, target_is_directory=True)
            for state_path, identity in ((alias, 'b' * 32), (data, 'b' * 32),
                                         (state, '../escape'), (state, 'B' * 32)):
                result = subprocess.run([sys.executable, str(SCRIPT), 'prepare-stage',
                                         str(data), str(state_path), identity],
                                        capture_output=True, text=True, check=False)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(sentinel.read_text(), 'retained')
            generations = state / 'generations'
            generations.symlink_to(data, target_is_directory=True)
            result = subprocess.run([sys.executable, str(SCRIPT), 'prepare-stage',
                                     str(data), str(state), 'b' * 32],
                                    capture_output=True, text=True, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(sentinel.read_text(), 'retained')

    def test_activation_without_canonical_producer_refuses_before_effect(self):
        with tempfile.TemporaryDirectory(prefix='node-generation-activation-refusal-') as directory:
            root = Path(directory)
            data = root / 'data'
            staged = root / 'staged'
            data.mkdir(mode=0o700)
            staged.mkdir(mode=0o700)
            sentinel = data / 'retained-economic-generation'
            sentinel.write_text('retained')
            for operation in ('seal-generation', 'activate-generation'):
                result = subprocess.run([sys.executable, str(SCRIPT), operation,
                                         str(data), str(staged), 'c' * 32, '1', '{}'],
                                        capture_output=True, text=True, check=False)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(sentinel.read_text(), 'retained')
                self.assertEqual(list(staged.iterdir()), [])

    def test_rollback_removes_only_unsealed_staging(self):
        with tempfile.TemporaryDirectory(prefix='node-generation-rollback-') as directory:
            root = Path(directory)
            data = root / 'data'
            state = root / 'state'
            data.mkdir(mode=0o700)
            state.mkdir(mode=0o700)
            sentinel = data / 'retained-economic-generation'
            sentinel.write_text('retained')
            identity = 'd' * 32
            prepare = subprocess.run([sys.executable, str(SCRIPT), 'prepare-stage',
                                      str(data), str(state), identity],
                                     capture_output=True, text=True, check=False)
            self.assertEqual(prepare.returncode, 0, prepare.stderr)
            stage = state / 'generations' / identity
            (stage / 'data' / 'incomplete-producer-output').write_text('incomplete')
            rollback = subprocess.run([sys.executable, str(SCRIPT), 'rollback-stage',
                                       str(data), str(state), identity],
                                      capture_output=True, text=True, check=False)
            self.assertEqual(rollback.returncode, 0, rollback.stderr)
            self.assertEqual(rollback.stdout.strip(), 'rolled_back')
            self.assertFalse(stage.exists())
            self.assertEqual(sentinel.read_text(), 'retained')


if __name__ == '__main__':
    unittest.main()
