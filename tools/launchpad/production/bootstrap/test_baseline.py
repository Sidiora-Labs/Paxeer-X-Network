#!/usr/bin/env python3
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location('baseline', Path(__file__).with_name('baseline.py'))
baseline = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(baseline)

class BaselineControlFlow(unittest.TestCase):
    def test_missing_evidence_directory_fails_before_work(self):
        env = dict(os.environ)
        env.pop('QUALIFICATION_EVIDENCE_DIR', None)
        result = subprocess.run([sys.executable, baseline.__file__], env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 2)
        self.assertIn('QUALIFICATION_EVIDENCE_DIR is required', result.stderr)

    def test_real_pin_file_aliases_and_reject_multiple_versions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / '.tool-versions'
            path.write_text('golang 1.26.4\nnodejs 24.16.0\npnpm 9.15.0\n')
            self.assertEqual(baseline.read_pins(path), {'go': '1.26.4', 'node': '24.16.0', 'pnpm': '9.15.0'})
            path.write_text('pnpm 9.15.0 11.5.2\n')
            with self.assertRaises(ValueError):
                baseline.read_pins(path)

    def test_source_manifest_excludes_secrets_generated_and_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            evidence = root / 'evidence'
            evidence.mkdir()
            for name in ['source.go', '.env', '.env.local']:
                (root / name).write_text('content')
            for name in ['node_modules', 'dist', 'evidence']:
                (root / name).mkdir(exist_ok=True)
                (root / name / 'generated').write_text('content')
            (root / 'link').symlink_to(root / 'source.go')
            self.assertEqual([p.name for p in baseline.source_files(root, evidence)], ['source.go'])
            self.assertEqual(len(baseline.sha(root / 'source.go')), 64)

if __name__ == '__main__':
    unittest.main()
