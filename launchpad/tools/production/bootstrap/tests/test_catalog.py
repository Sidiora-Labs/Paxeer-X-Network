"""Validator tests use constructed report records, never production qualification evidence."""
import importlib.util
from pathlib import Path
import tempfile
import json
import os
import subprocess
import unittest

spec = importlib.util.spec_from_file_location('catalog', Path(__file__).with_name('catalog.py'))
catalog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(catalog)


class ReportValidationTests(unittest.TestCase):
    def setUp(self):
        self.expected = [{'package': 'example/pkg', 'declarations': [{'name': 'TestMoney'}]}]
        self.events = [dict(Action='run', Package='example/pkg', Test='TestMoney'),
                       dict(Action='pass', Package='example/pkg', Test='TestMoney'),
                       dict(Action='pass', Package='example/pkg')]

    def test_complete_go_report(self):
        self.assertEqual(catalog.validate_go(self.events, self.expected)['expected_top_level_tests'], 1)

    def test_go_skip_rejects(self):
        with self.assertRaisesRegex(ValueError, 'skip'):
            catalog.validate_go(self.events + [dict(Action='skip', Package='example/pkg', Test='TestFixture')], self.expected)

    def test_go_failure_rejects(self):
        with self.assertRaisesRegex(ValueError, 'fail'):
            catalog.validate_go(self.events + [dict(Action='fail', Package='example/pkg')], self.expected)

    def test_go_missing_test_rejects(self):
        with self.assertRaisesRegex(ValueError, 'missing executed'):
            catalog.validate_go(self.events[1:], self.expected)

    def test_go_incomplete_package_rejects(self):
        with self.assertRaisesRegex(ValueError, 'package pass'):
            catalog.validate_go(self.events[:-1], self.expected)

    def test_go_empty_suite_rejects(self):
        with self.assertRaisesRegex(ValueError, 'no expected'):
            catalog.validate_go([], [])

    def test_go_incomplete_subtest_rejects(self):
        with self.assertRaisesRegex(ValueError, 'without final pass'):
            catalog.validate_go(self.events + [dict(Action='run', Package='example/pkg', Test='TestMoney/sub')], self.expected)

    def test_uncorroborated_package_skip_rejected(self):
        with self.assertRaisesRegex(ValueError, 'skip'):
            catalog.validate_go(self.events + [dict(Action='skip', Package='example/empty')], self.expected)

    def test_expected_package_no_test_files_still_rejected(self):
        records = [dict(Action='output', Package='example/pkg', Output='?\texample/pkg\t[no test files]\n'),
                   dict(Action='skip', Package='example/pkg')]
        with self.assertRaisesRegex(ValueError, 'skip'):
            catalog.validate_go(records, self.expected)

    def test_actual_go_mixed_packages_and_named_skip(self):
        # Exercise the installed Go runner, not invented JSON. No dependencies or network.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'go.mod').write_text('module example.test/catalogfixture\n\ngo 1.23.0\n')
            (root / 'tested').mkdir()
            (root / 'empty').mkdir()
            (root / 'tested/value.go').write_text('package tested\nfunc Sum(a, b int) int { return a+b }\n')
            (root / 'empty/value.go').write_text('package empty\nconst Value = 1\n')
            test_file = root / 'tested/value_test.go'
            test_file.write_text('package tested\nimport "testing"\nfunc TestSum(t *testing.T) { if Sum(2,3)!=5 { t.Fatal("sum") } }\n')
            expected = [{'package': 'example.test/catalogfixture/tested',
                         'declarations': [{'name': 'TestSum'}]}]
            env = dict(os.environ, GOWORK='off', GOMAXPROCS='2', GOTOOLCHAIN='local', GOPROXY='off')
            def execute():
                completed = subprocess.run(['go', 'test', '-p', '1', '-count=1', '-json', './...'],
                                           cwd=root, env=env, text=True, capture_output=True,
                                           timeout=60, check=False)
                self.assertEqual(completed.returncode, 0, completed.stderr)
                return [json.loads(line) for line in completed.stdout.splitlines() if line.strip()]
            result = catalog.validate_go(execute(), expected)
            self.assertEqual(result['executed_named_tests'], 1)
            self.assertEqual(result['no_test_packages'], ['example.test/catalogfixture/empty'])
            test_file.write_text('package tested\nimport "testing"\nfunc TestSum(t *testing.T) { t.Skip("real runner skip") }\n')
            with self.assertRaisesRegex(ValueError, 'skip'):
                catalog.validate_go(execute(), expected)

    def test_vitest_expected_file_and_totals(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            file = 'client/apps/web/src/value.test.ts'
            report = {'success': True, 'numTotalTests': 1, 'numPassedTests': 1,
                      'testResults': [{'name': str(root / file), 'status': 'passed',
                                       'assertionResults': [{'status': 'passed'}]}]}
            self.assertEqual(catalog.validate_vitest(report, root, [{'path': file}])['executed_assertions'], 1)
            report['numPendingTests'] = 1
            with self.assertRaisesRegex(ValueError, 'Pending'):
                catalog.validate_vitest(report, root, [{'path': file}])
            report['numPendingTests'] = 0
            report['numTotalTests'] = 2
            with self.assertRaisesRegex(ValueError, 'inconsistent'):
                catalog.validate_vitest(report, root, [{'path': file}])

    def test_vitest_missing_file_rejects(self):
        report = {'success': True, 'numTotalTests': 1, 'numPassedTests': 1, 'testResults': []}
        with self.assertRaisesRegex(ValueError, 'missing expected'):
            catalog.validate_vitest(report, Path('/tmp'), [{'path': 'client/apps/web/src/test.test.ts'}])

    def test_missing_modules_recorded_not_skipped(self):
        with tempfile.TemporaryDirectory() as directory:
            result = catalog.inventory(Path(directory))
            self.assertEqual(len(result['go_modules']), 13)
            self.assertEqual(sum(x.startswith('missing module') for x in result['issues']), 13)


if __name__ == '__main__':
    unittest.main()
