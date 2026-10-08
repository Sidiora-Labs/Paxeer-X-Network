import argparse
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
if 'ai_evidence' in sys.modules:
    evidence = sys.modules['ai_evidence']
else:
    spec = importlib.util.spec_from_file_location('ai_evidence', Path(__file__).parents[1] / 'ai_evidence.py')
    evidence = importlib.util.module_from_spec(spec)
    sys.modules['ai_evidence'] = evidence
    spec.loader.exec_module(evidence)

AUTHORITY = None


class EvidenceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.authority = AUTHORITY if AUTHORITY is not None else evidence.load_authority()

    def setUp(self):
        self.admission, self.approval = copy.deepcopy(self.authority)
        self.temporary = tempfile.TemporaryDirectory(
            prefix='paxai-negative-', dir=self.admission['evidence']['directory'])
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)

    def write(self, name, raw, mode=0o600):
        path = self.directory / name
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, mode)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(raw)
        return path

    def refuse_structure(self):
        with self.assertRaises(evidence.Invalid):
            evidence.validate_structure(self.admission, self.approval)

    def test_actual_approved_authority(self):
        contracts, overlay = evidence.validate_structure(*self.authority)
        self.assertEqual(len(contracts), 39)
        self.assertEqual(set(overlay), set(evidence.PATHS))
        self.assertEqual(self.authority[0]['overlay']['status'], 'READY')
        self.assertFalse(self.authority[0]['access']['full_sandbox_pass'])
        self.assertEqual(self.authority[0]['access']['infrastructure_probe_qualification'], 'UNRUN')

    def test_selector_rejection(self):
        for selector in ('', 'workspace', 'unknown', ' workspace-admission',
                         'workspace-admission ', 'WORKSPACE-ADMISSION', None, {}, 1):
            with self.subTest(selector=selector), self.assertRaises(evidence.Invalid):
                evidence.validate_selector(selector)

    def test_cli_argument_rejection(self):
        for args in ([], ['--selector', ''], ['--selector', 'unknown'],
                     ['--selector', 'workspace-admission', '--extra'],
                     ['--select', 'workspace-admission']):
            parser = evidence.Parser(add_help=False, allow_abbrev=False)
            parser.add_argument('--selector', required=True, choices=evidence.SELECTORS)
            with self.assertRaises(evidence.Invalid):
                parser.parse_args(args)

    def test_missing_files(self):
        with self.assertRaises(evidence.Invalid):
            evidence.load_authority(str(self.directory / 'absent'), str(self.directory / 'approval'))

    def test_duplicate_and_nonobject_json(self):
        for raw in (b'{"schema":1,"schema":2}', b'{"x":{"y":1,"y":2}}',
                    b'{} trailing', b'[]', b'null', b'', b'{"x":NaN}', b'\xff'):
            with self.subTest(raw=raw), self.assertRaises(evidence.Invalid):
                evidence.parse(raw)

    def test_missing_unknown_fields(self):
        for which in ('admission', 'approval'):
            for change in ('missing', 'unknown'):
                self.admission, self.approval = copy.deepcopy(self.authority)
                record = getattr(self, which)
                if change == 'missing':
                    del record['schema']
                else:
                    record['unexpected'] = True
                self.refuse_structure()
        for section in ('candidate', 'contract', 'toolchain', 'dependencies', 'ownership',
                        'access', 'evidence', 'model', 'authorization', 'overlay'):
            self.admission, self.approval = copy.deepcopy(self.authority)
            self.admission[section]['unexpected'] = True
            self.refuse_structure()

    def test_strict_types(self):
        cases = (('issued_at_unix', None, True), ('ownership', 'fence', True),
                 ('ownership', 'exclusive', 1), ('access', 'network', 0),
                 ('access', 'full_sandbox_pass', 0), ('access', 'project_doc_max_bytes', False),
                 ('contract', 'version', True), ('model', 'runtime_probe_exit', False))
        for section, key, value in cases:
            self.admission, self.approval = copy.deepcopy(self.authority)
            if key is None:
                self.admission[section] = value
            else:
                self.admission[section][key] = value
            self.refuse_structure()

    def test_identity_and_stale_base(self):
        cases = (('task_id', None, 'ai.P-OTHER'), ('decision_id', None, 'other'),
                 ('selector', None, 'deployment-admission'),
                 ('candidate', 'base_revision', '0' * 40),
                 ('candidate', 'branch', 'main'), ('contract', 'version', 2))
        for section, key, value in cases:
            self.admission, self.approval = copy.deepcopy(self.authority)
            if key is None:
                self.admission[section] = value
            else:
                self.admission[section][key] = value
            self.refuse_structure()

    def test_contract_hash_and_coverage(self):
        self.admission['contract']['files'][0]['sha256'] = '0' * 64
        self.refuse_structure()
        self.admission, self.approval = copy.deepcopy(self.authority)
        self.admission['contract']['files'].pop()
        self.refuse_structure()
        self.admission, self.approval = copy.deepcopy(self.authority)
        self.admission['contract']['files'][0]['path'] = 'spec.kvx'
        self.refuse_structure()
        self.admission, self.approval = copy.deepcopy(self.authority)
        self.admission['contract']['files'][0] = self.admission['contract']['files'][1]
        self.refuse_structure()

    def test_changed_bytes_and_pin(self):
        raw = evidence.read_bytes(os.environ['PAXAI_ADMISSION_RECORD'])
        path = self.write('copied-admission.json', raw)
        pinned = hashlib.sha256(raw).hexdigest()
        self.assertEqual(evidence.pinned_file(str(path), pinned), raw)
        with path.open('ab') as stream:
            stream.write(b' ')
        with self.assertRaises(evidence.Invalid):
            evidence.pinned_file(str(path), pinned)
        self.approval['admission_sha256'] = '0' * 64
        with self.assertRaises(evidence.Invalid):
            evidence.validate_workspace(self.admission, self.approval,
                Path(os.environ['PAXAI_ADMISSION_RECORD']),
                Path(os.environ['PAXAI_APPROVAL_RECORD']), raw)

    def test_ownership_and_fence(self):
        for name, value in (('manager', 'other'), ('writer', 'native-controller'),
                            ('codify_attempt', '0' * 64), ('fence', 3), ('exclusive', False)):
            self.admission, self.approval = copy.deepcopy(self.authority)
            self.admission['ownership'][name] = value
            self.refuse_structure()

    def test_sandbox_and_parent_limits(self):
        for name, value in (('runtime_scope_result', 'FAIL'), ('full_sandbox_pass', True),
                            ('infrastructure_probe_exit', 0),
                            ('infrastructure_probe_qualification', 'PASS'),
                            ('limits', []), ('allowed_tools', ['all']), ('network', True),
                            ('ignore_user_config', False)):
            self.admission, self.approval = copy.deepcopy(self.authority)
            self.admission['access'][name] = value
            self.refuse_structure()

    def test_evidence_and_log_identity(self):
        self.admission['evidence']['runtime_log_sha256'] = '0' * 64
        self.refuse_structure()
        self.admission, self.approval = copy.deepcopy(self.authority)
        self.admission['access']['policy_sha256'] = '0' * 64
        self.refuse_structure()
        self.admission, self.approval = copy.deepcopy(self.authority)
        self.admission['access']['runtime_result_sha256'] = '0' * 64
        self.refuse_structure()
        self.admission, self.approval = copy.deepcopy(self.authority)
        self.admission['evidence']['runtime_log_path'] = '/outside/runtime.log'
        self.refuse_structure()

    def test_overlay_pending_partial_wrong_paths(self):
        for value in ('PENDING_COMPLETE_AUTHOR_OUTPUT', 'PASS', '', True):
            self.admission, self.approval = copy.deepcopy(self.authority)
            self.admission['overlay']['status'] = value
            self.refuse_structure()
        self.admission, self.approval = copy.deepcopy(self.authority)
        self.admission['overlay']['files'].pop()
        self.refuse_structure()
        for value in ('tools/paxeer-x/other.py', '../ai_evidence.py', '/ai_evidence.py'):
            self.admission, self.approval = copy.deepcopy(self.authority)
            self.admission['overlay']['files'][0]['path'] = value
            self.refuse_structure()
        self.admission, self.approval = copy.deepcopy(self.authority)
        self.admission['overlay']['files'][0]['sha256'] = '0' * 64
        self.refuse_structure()

    def test_symlink_public_hardlink_files(self):
        actual = self.write('real.json', b'{}')
        symlink = self.directory / 'link.json'
        symlink.symlink_to(actual)
        with self.assertRaises(evidence.Invalid):
            evidence.read_bytes(str(symlink))
        public = self.write('public.json', b'{}', 0o644)
        os.chmod(public, 0o644)
        with self.assertRaises(evidence.Invalid):
            evidence.read_bytes(str(public))
        hard = self.directory / 'hard.json'
        os.link(actual, hard)
        for path in (actual, hard):
            with self.assertRaises(evidence.Invalid):
                evidence.read_bytes(str(path))
        link_directory = self.directory / 'link-directory'
        link_directory.symlink_to(self.directory, target_is_directory=True)
        with self.assertRaises(evidence.Invalid):
            evidence.read_bytes(str(link_directory / 'public.json'))

    def test_nonregular_and_ownership_files(self):
        with self.assertRaises(evidence.Invalid):
            evidence.read_bytes(str(self.directory))
        fifo = self.directory / 'fifo'
        os.mkfifo(fifo, 0o600)
        with self.assertRaises(evidence.Invalid):
            evidence.read_bytes(str(fifo))
        if os.geteuid() == 0:
            path = self.write('foreign-owner', b'{}')
            os.chown(path, 65534, 65534)
            try:
                with self.assertRaises(evidence.Invalid):
                    evidence.read_bytes(str(path))
            finally:
                os.chown(path, 0, 0)
        else:
            with self.assertRaises(evidence.Invalid):
                evidence.read_bytes('/dev/null')

    def test_secret_escape_and_public_directory(self):
        for value in ('relative.json', str(self.directory) + '/../file',
                      str(self.directory / '.env'), str(self.directory / '.env.local'),
                      str(self.directory / '.ssh' / 'id_rsa')):
            with self.assertRaises(evidence.Invalid):
                evidence.safe_path(value)
        with self.assertRaises(evidence.Invalid):
            evidence.inside('/outside/file', self.directory)
        os.chmod(self.directory, 0o755)
        with self.assertRaises(evidence.Invalid):
            evidence.private_directory(str(self.directory))
        os.chmod(self.directory, 0o700)

    def test_real_contract_overlay_runtime_file_tampering(self):
        sources = [(Path(self.admission['candidate']['checkout']) / row['path'], row['sha256'], False)
                   for row in self.admission['overlay']['files']]
        sources.extend((Path(self.admission['access'][path]), self.admission['access'][sha], True)
                       for path, sha in (('policy_path', 'policy_sha256'),
                                         ('runtime_result_path', 'runtime_result_sha256')))
        common = evidence.git(self.admission['candidate']['checkout'], 'rev-parse',
                              '--path-format=absolute', '--git-common-dir')
        row = self.admission['contract']['files'][0]
        sources.append((Path(common).parent / row['path'], row['sha256'], False))
        for index, (source, expected, private) in enumerate(sources):
            raw = evidence.read_bytes(str(source), private=private)
            copied = self.write('tampered-%d' % index, raw + b'\n')
            with self.assertRaises(evidence.Invalid):
                evidence.pinned_file(str(copied), expected)

    def test_future_selector_missing_or_empty_evidence(self):
        for selector in evidence.SELECTORS[1:]:
            self.admission['other_selector_records'] = {}
            with self.assertRaises(evidence.Invalid):
                evidence.validate_future(selector, self.admission)
            self.admission['other_selector_records'] = {selector: {}}
            with self.assertRaises(evidence.Invalid):
                evidence.validate_future(selector, self.admission)
            path = self.write(selector + '.json', b'{}')
            self.admission['other_selector_records'] = {selector: {
                'path': str(path), 'sha256': hashlib.sha256(b'{}').hexdigest()}}
            with self.assertRaises(evidence.Invalid):
                evidence.validate_future(selector, self.admission)

    def test_bounds_and_hash_types(self):
        for value in (True, -1, 2**63, '1', 1.5):
            with self.assertRaises(evidence.Invalid):
                evidence.integer(value, 'test')
        for value in ('', '0' * 63, 'A' * 64, 1, None):
            with self.assertRaises(evidence.Invalid):
                evidence.digest(value)
        oversized = self.write('oversized', b'x' * (evidence.MAX_BYTES + 1))
        with self.assertRaises(evidence.Invalid):
            evidence.read_bytes(str(oversized))

    def test_future_fact_refusal(self):
        for selector in evidence.SELECTORS[1:4]:
            name = evidence.FUTURE_FIELDS[selector][0]
            with self.assertRaises(evidence.Invalid):
                evidence.validate_facts(selector, {name: {}}, {})
        for value in (True, 'PASS', 0):
            with self.assertRaises(evidence.Invalid):
                evidence.fact_value(value, 'positive')
        for value in (['same', 'same'], [], ['one']):
            with self.assertRaises(evidence.Invalid):
                evidence.fact_value(value, 'operators')

    def test_safe_refusal_labels(self):
        self.assertEqual(evidence.refusal_reason(evidence.Invalid('file SHA256 mismatch')),
                         'file-sha256-pin')
        self.assertEqual(evidence.refusal_reason(evidence.Invalid('contract: coverage')),
                         'contract-coverage')
        self.assertEqual(evidence.refusal_reason(evidence.Invalid('private-record-value\n')),
                         'invalid-evidence')
        self.assertEqual(evidence.refusal_reason(OSError('private-host-path')),
                         'filesystem-error')
        self.assertEqual(evidence.refusal_reason(ValueError('private-record-value')),
                         'value-error')
        self.assertEqual(evidence.refusal_reason(evidence.Invalid({'private': 'value'})),
                         'invalid-evidence')

    def test_runtime_producer_control_binding(self):
        # Component fixture only; the workspace gate still consumes pinned real bytes.
        runtime = {'runtime_scope_result': 'PASS', 'full_sandbox_pass': False,
                   'infrastructure_probe_exit': 1,
                   'infrastructure_probe_qualification': 'UNRUN_UNCHANGED'}
        evidence.validate_runtime(runtime)
        for name in runtime:
            missing = dict(runtime)
            del missing[name]
            with self.subTest(missing=name), self.assertRaises(evidence.Invalid):
                evidence.validate_runtime(missing)
        for name, value in (('runtime_scope_result', 'FAIL'),
                            ('full_sandbox_pass', True), ('full_sandbox_pass', 0),
                            ('infrastructure_probe_exit', 0),
                            ('infrastructure_probe_exit', True),
                            ('infrastructure_probe_qualification', 'UNRUN'),
                            ('infrastructure_probe_qualification', 'PASS'),
                            ('infrastructure_probe_qualification', 'unknown')):
            changed = dict(runtime)
            changed[name] = value
            with self.subTest(field=name), self.assertRaises(evidence.Invalid):
                evidence.validate_runtime(changed)
        for value in ({}, [], None):
            with self.assertRaises(evidence.Invalid):
                evidence.validate_runtime(value)

    def test_safe_testcase_failure_identities(self):
        method = 'test_safe_testcase_failure_identities'
        case = EvidenceTests(method)
        result = evidence.SafeTestResult(EvidenceTests, (method,))
        failure = (AssertionError, AssertionError('private-assertion-value'), None)
        error = (OSError, OSError('private-host-path'), None)
        result.addFailure(case, failure)
        result.addError(case, error)
        # Parameters and fixture descriptions must never be rendered.
        subtest = unittest.TestCase()
        result.addSubTest(case, subtest, failure)
        result.addError(unittest.TestCase(), error)
        result.addSkip(case, 'private-skip-reason')
        result.addUnexpectedSuccess(case)
        identity = 'EvidenceTests.' + method
        self.assertIn((identity, 'failure', 'test-exception'), result.diagnostics)
        self.assertIn((identity, 'error', 'filesystem-error'), result.diagnostics)
        self.assertIn((identity, 'subtest-failure', 'test-exception'), result.diagnostics)
        self.assertIn(('fixture-or-loader', 'error', 'filesystem-error'), result.diagnostics)
        self.assertIn((identity, 'skip', 'skip'), result.diagnostics)
        self.assertIn((identity, 'unexpected-success', 'unexpected-success'), result.diagnostics)
        self.assertEqual(result.failures[0][1], 'test-exception')
        self.assertEqual(result.errors[0][1], 'filesystem-error')
        self.assertEqual(result.skipped[0][1], 'skip')
        self.assertFalse(result.wasSuccessful())


if __name__ == '__main__':
    unittest.main()
