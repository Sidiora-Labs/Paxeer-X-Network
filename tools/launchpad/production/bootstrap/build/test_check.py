"""Real local Go modules exercise build/list failures without substitute executables."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('check.py')


class BuildCheckTests(unittest.TestCase):
    def run_case(self, source, expected, reason=None, other_source=None):
        with tempfile.TemporaryDirectory(prefix='kindle-build-check-') as temporary:
            base = Path(temporary)
            module = base / 'sample'
            module.mkdir()
            (module / 'go.mod').write_text('module local.example/sample\n\ngo 1.25.0\n')
            if source is not None:
                (module / 'sample.go').write_text(source)
            if other_source is not None:
                (module / 'other.go').write_text(other_source)
            manifest = {'schemaVersion': 1, 'modules': [{
                'path': 'sample', 'module': 'local.example/sample', 'goVersion': '1.25.0',
                'listArgv': ['go', 'list', './...'], 'buildArgv': ['go', 'build', '-p', '1', './...'], 'entrypoints': []}]}
            path = base / 'manifest.json'
            path.write_text(json.dumps(manifest))
            completed = subprocess.run([sys.executable, str(SCRIPT), '--run', '--root', str(base),
                                        '--manifest', str(path), '--log-dir', str(base / 'logs')],
                                       capture_output=True, text=True, check=False)
            self.assertEqual(completed.returncode, expected, completed.stderr + completed.stdout)
            report = json.loads(completed.stdout)
            row = report['results'][0]
            if reason:
                self.assertIn(reason, row['reason'])
            return row

    def test_success_builds_actual_package(self):
        row = self.run_case('package sample\nfunc Value() int { return 125 }\n', 0)
        self.assertEqual([x['exitCode'] for x in row['commands']], [0, 0])

    def test_failed_list_is_not_skipped(self):
        row = self.run_case('package sample\n', 1, 'list failed', other_source='package conflicting\n')
        self.assertNotEqual(row['commands'][0]['exitCode'], 0)
        self.assertEqual(len(row['commands']), 1)

    def test_failed_build_propagates(self):
        row = self.run_case('package sample\nfunc Value() int { return "wrong type" }\n', 1, 'build failed')
        self.assertEqual(row['commands'][0]['exitCode'], 0)
        self.assertNotEqual(row['commands'][1]['exitCode'], 0)

    def test_empty_required_module_fails(self):
        self.run_case(None, 1, 'no enumerated packages')

    def test_unlisted_module_fails_before_commands(self):
        with tempfile.TemporaryDirectory(prefix='kindle-manifest-check-') as temporary:
            base = Path(temporary)
            for name in ['sample', 'forgotten']:
                (base / name).mkdir()
                (base / name / 'go.mod').write_text('module local.example/' + name + '\n\ngo 1.25.0\n')
            manifest = {'schemaVersion': 1, 'modules': [{'path': 'sample', 'module': 'local.example/sample',
                        'goVersion': '1.25.0', 'listArgv': ['go', 'list', './...'],
                        'buildArgv': ['go', 'build', '-p', '1', './...']}]}
            path = base / 'manifest.json'
            path.write_text(json.dumps(manifest))
            result = subprocess.run([sys.executable, str(SCRIPT), '--check', '--root', str(base),
                                     '--manifest', str(path)], capture_output=True, text=True, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('unlisted', json.loads(result.stdout)['error'])


if __name__ == '__main__':
    unittest.main()
