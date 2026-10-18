import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import snapshot


class SnapshotTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'source'
        self.root.mkdir()
        self.put('owned/file.go', b'package actual\n')
        self.put('peer/file.go', b'package peer\n')

    def put(self, name, value):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(value)
        return path

    def test_real_hash_and_reproducibility(self):
        baseline = snapshot.inventory(self.root)
        self.assertEqual(baseline, snapshot.inventory(self.root))
        self.assertEqual(baseline['files']['owned/file.go']['sha256'],
                         hashlib.sha256(b'package actual\n').hexdigest())
        snapshot.validate_manifest(baseline)

    def test_secrets_and_generated_are_excluded(self):
        before = snapshot.inventory(self.root)
        for path in ('.env', '.env.production', 'config/tls.key', 'secrets/account.json',
                     '.npmrc', 'config/id_ed25519', 'config/credentials.json',
                     'node_modules/pkg/index.js', '.next/chunk.js', 'runtime/db', 'dist/app.js'):
            self.put(path, b'private generated fixture')
        self.assertEqual(before, snapshot.inventory(self.root))

    def test_add_modify_delete_and_permissions(self):
        before = snapshot.inventory(self.root)
        self.put('owned/file.go', b'package updated\n')
        self.put('owned/new.go', b'package new\n')
        changes = snapshot.owned_diff(before, snapshot.inventory(self.root), ['owned/**'])
        self.assertEqual([c['path'] for c in changes['changes']], ['owned/file.go', 'owned/new.go'])
        baseline = snapshot.inventory(self.root)
        (self.root / 'owned/new.go').unlink()
        (self.root / 'owned/file.go').chmod(0o755)
        changes = snapshot.owned_diff(baseline, snapshot.inventory(self.root), ['owned/**'])
        self.assertTrue(changes['changes'][0]['after']['executable'])
        self.assertIsNone(changes['changes'][1]['after'])

    def test_reject_outside_owned_including_prefix_collision(self):
        baseline = snapshot.inventory(self.root)
        self.put('owned-other/file.go', b'package collision\n')
        with self.assertRaisesRegex(ValueError, 'outside owned'):
            snapshot.owned_diff(baseline, snapshot.inventory(self.root), ['owned/**'])
        for pattern in ('../peer/**', '/peer/**', 'owned/*', 'owned/../peer/**'):
            with self.assertRaises(ValueError):
                snapshot.owned_diff(baseline, baseline, [pattern])

    def test_tamper_and_symlink_rejected(self):
        baseline = snapshot.inventory(self.root)
        changed = copy.deepcopy(baseline)
        changed['files']['owned/file.go']['sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'digest mismatch'):
            snapshot.owned_diff(changed, baseline, ['owned/**'])
        external = Path(self.temp.name) / 'private'
        external.write_bytes(b'never hash external contents')
        (self.root / 'owned/link').symlink_to(external)
        with self.assertRaisesRegex(ValueError, 'symlink rejected'):
            snapshot.inventory(self.root)

    def run_cli(self, *args):
        return subprocess.run([sys.executable, str(Path(snapshot.__file__)), *args],
                              capture_output=True, text=True)

    def test_cli_immutable_baseline_and_drift(self):
        output = Path(self.temp.name) / 'baseline.json'
        args = ('create', '--root', str(self.root), '--output', str(output))
        result = self.run_cli(*args)
        self.assertEqual(result.returncode, 0, result.stderr)
        original = output.read_bytes()
        self.assertEqual(self.run_cli(*args).returncode, 1)
        self.assertEqual(output.read_bytes(), original)
        args = ('verify', '--root', str(self.root), '--baseline', str(output))
        self.assertEqual(self.run_cli(*args).returncode, 0)
        self.put('owned/file.go', b'source drift')
        result = self.run_cli(*args)
        self.assertEqual(result.returncode, 1)
        self.assertIn('source drift', result.stderr)
        diff = Path(self.temp.name) / 'diff.json'
        result = self.run_cli('diff', '--root', str(self.root), '--baseline', str(output),
                              '--owned', 'owned/**', '--output', str(diff))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(diff.read_text())['changes'][0]['path'], 'owned/file.go')
        self.assertEqual(output.read_bytes(), original)

    def test_reject_output_inside_source(self):
        result = self.run_cli('create', '--root', str(self.root), '--output', str(self.root / 'manifest.json'))
        self.assertEqual(result.returncode, 1)
        self.assertFalse((self.root / 'manifest.json').exists())


if __name__ == '__main__':
    unittest.main()
