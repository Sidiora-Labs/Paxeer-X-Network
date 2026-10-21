import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from dispatcher import evidence

class Lease:
    def __init__(self, token): self.token = token; self.valid = True
    def validate(self, token):
        if not self.valid or token != self.token: raise ValueError('expired')
        return self.token

class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.base = Path(self.temp.name)
        self.root = self.base/'source'; self.root.mkdir()
        (self.root/'input.txt').write_text('real input')
        self.script = self.root/'check.py'
        self.script.write_text("from pathlib import Path\nassert Path('input.txt').read_text() == 'real input'\nprint('actual checker executed')\n")
        self.binding = {k:'a'*64 for k in ('baseline','candidate','interface','layout')}
        self.token = dict(task='1.1', lane='L01', owner='worker', attempt='attempt-1', fence=1, bindings=self.binding)
        self.secret = b'coordinator-only-secret-value-0001'
        self.store = Lease(self.token)
        self.check = dict(id='actual', command=[sys.executable, str(self.script)], timeout=5, inputs=['input.txt'])
    def tearDown(self): self.temp.cleanup()
    def run_check(self, checks=None):
        self.binding['candidate'] = evidence.source_digest(self.root)
        return evidence.run_checks(checks or [self.check], self.root, self.base/'receipts', self.binding, self.token, self.secret)
    def publish(self, result, required=None):
        return evidence.publish(result, self.secret, self.store, self.root, required or [self.check])
    def test_long_inline_python_native_positive_proof(self):
        code = "#" + "x" * 12000 + "\nimport unittest\nclass RealCheck(unittest.TestCase):\n def test_actual(self): self.assertEqual(2+2,4)\nunittest.main(argv=['native'], verbosity=2)\n"
        self.check.update(command=[sys.executable, '-c', code], proof='unittest')
        result=self.run_check()
        self.assertEqual(result['receipts'][0]['exit_code'],0)
        self.assertEqual(self.publish(result)['proofs']['actual']['positive_count'],1)
        self.assertIn(str(Path(sys.executable).resolve()),result['receipts'][0]['checker'])
    def test_checker_argument_stat_errors_fail_closed(self):
        import errno
        from unittest.mock import patch
        original=Path.lstat
        for number in (errno.EACCES,errno.EIO,errno.ELOOP,errno.EINVAL):
            def fake(path,*args,**kwargs):
                if str(path).endswith('/candidate-argument'):
                    raise OSError(number,'injected filesystem failure')
                return original(path,*args,**kwargs)
            with self.subTest(errno=number),patch.object(Path,'lstat',fake):
                with self.assertRaises(OSError) as caught:
                    evidence._checker([sys.executable,'candidate-argument'],self.root)
                self.assertEqual(caught.exception.errno,number)
    def test_checker_argument_links_and_reference_binding(self):
        external=self.base/'external';external.mkdir();script=external/'native.py';script.write_text('print(1)')
        paths=evidence._checker([sys.executable,str(script)],self.root)
        self.assertEqual(paths[str(script)],evidence._file(script))
        script.write_text('print(2)')
        self.assertNotEqual(paths[str(script)],evidence._checker([sys.executable,str(script)],self.root)[str(script)])
        for target in (script,self.base/'missing'):
            link=self.root/'leaf';link.symlink_to(target)
            with self.assertRaises(evidence.EvidenceError):evidence._checker([sys.executable,str(link)],self.root)
            link.unlink()
        link=self.root/'linked-parent';link.symlink_to(external,target_is_directory=True)
        with self.assertRaises(evidence.EvidenceError):evidence._checker([sys.executable,str(link/'native.py')],self.root)
    def test_actual_subprocess_publishes(self):
        result = self.run_check()
        self.assertIn('actual checker executed', result['receipts'][0]['stdout'])
        self.assertTrue(self.publish(result)['verified'])
        self.assertTrue((self.base/'receipts/evidence.json').is_file())
    def test_json_success_forgery_rejected(self):
        with self.assertRaises(evidence.EvidenceError): self.publish({'success':True})
    def test_tampered_receipt_rejected(self):
        result = self.run_check(); result['receipts'][0]['exit_code'] = 22
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_failed_actual_process_rejected(self):
        self.script.write_text('raise SystemExit(9)\n')
        result = self.run_check()
        self.assertEqual(result['receipts'][0]['exit_code'], 9)
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_timeout_actual_process_rejected(self):
        self.script.write_text('import time\ntime.sleep(30)\n'); self.check['timeout'] = .1
        result = self.run_check()
        self.assertTrue(result['receipts'][0]['timed_out'])
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_changed_source_rejected(self):
        result = self.run_check(); (self.root/'input.txt').write_text('changed')
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_changed_checker_rejected(self):
        result = self.run_check(); self.script.write_text('print("changed")')
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_expired_lease_rejected(self):
        result = self.run_check(); self.store.valid = False
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_fenced_attempt_rejected(self):
        result = self.run_check(); self.store.token = dict(self.token, fence=2)
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_missing_required_rejected(self):
        result = self.run_check()
        with self.assertRaises(evidence.EvidenceError): self.publish(result, [dict(self.check, id='absent')])
    def test_empty_checks_rejected(self):
        with self.assertRaises(evidence.EvidenceError):
            evidence.run_checks([], self.root, self.base/'receipts', self.binding, self.token, self.secret)
    def test_skipped_check_rejected(self):
        with self.assertRaises(evidence.EvidenceError): self.run_check([dict(self.check, skipped=True)])
    def test_duplicate_check_rejected(self):
        with self.assertRaises(evidence.EvidenceError): self.run_check([self.check, self.check])
    def test_missing_input_rejected(self):
        with self.assertRaises(evidence.EvidenceError): self.run_check([dict(self.check, inputs=['missing'])])
    def test_output_must_exist_and_match(self):
        output = self.base/'result.txt'
        self.script.write_text(f"from pathlib import Path\nPath({str(output)!r}).write_text('checked')\n")
        self.check['outputs'] = [str(output)]
        result = self.run_check(); self.assertTrue(self.publish(result)['verified'])
        (self.base/'result.txt').write_text('stale')
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_missing_output_rejected(self):
        with self.assertRaises(evidence.EvidenceError): self.run_check([dict(self.check, outputs=['missing'])])
    def test_wrong_secret_rejected(self):
        result = self.run_check()
        with self.assertRaises(evidence.EvidenceError): evidence.publish(result, b'b'*32, self.store, self.root, [self.check])
    def test_unsafe_paths_rejected(self):
        with self.assertRaises(evidence.EvidenceError): self.run_check([dict(self.check, inputs=['../outside'])])
        (self.root/'link').symlink_to(self.script)
        with self.assertRaises(evidence.EvidenceError): evidence.source_digest(self.root)
    def test_binding_mismatch_rejected(self):
        with self.assertRaises(evidence.EvidenceError):
            evidence.run_checks([self.check], self.root, self.base/'receipts', dict(self.binding, interface='b'*64), self.token, self.secret)
    def test_receipts_inside_source_rejected(self):
        with self.assertRaises(evidence.EvidenceError):
            evidence.run_checks([self.check], self.root, self.root/'receipts', self.binding, self.token, self.secret)

    def test_source_mutation_rejected(self):
        self.script.write_text("from pathlib import Path\nPath('input.txt').write_text('mutated')\n")
        with self.assertRaises(evidence.EvidenceError): self.run_check()
    def test_arbitrary_true_command_rejected_by_trusted_registry(self):
        result = self.run_check([dict(self.check, command=[sys.executable, '-c', 'pass'])])
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_id_only_registry_rejected(self):
        result = self.run_check()
        with self.assertRaises(evidence.EvidenceError): self.publish(result, ['actual'])

    def test_actual_unittest_positive_proof(self):
        self.script.write_text("import unittest\nclass Real(unittest.TestCase):\n def test_math(self): self.assertEqual(2+2,4)\nunittest.main()\n")
        self.check['proof'] = 'unittest'
        result = self.run_check()
        self.assertEqual(result['receipts'][0]['proof']['positive_count'], 1)
        self.assertTrue(self.publish(result)['verified'])
    def test_actual_unittest_empty_rejected(self):
        self.script.write_text("import unittest\nunittest.main()\n")
        self.check['proof'] = 'unittest'
        with self.assertRaises(evidence.EvidenceError): self.run_check()
    def test_actual_unittest_skip_rejected(self):
        self.script.write_text("import unittest\nclass Real(unittest.TestCase):\n @unittest.skip('unavailable')\n def test_missing(self): pass\nunittest.main()\n")
        self.check['proof'] = 'unittest'
        with self.assertRaises(evidence.EvidenceError): self.run_check()
    def test_actual_empty_tap_rejected(self):
        self.script.write_text("print('TAP version 13\\n1..0\\n# pass 0\\n# fail 0')\n")
        self.check['proof'] = 'node-tap'
        with self.assertRaises(evidence.EvidenceError): self.run_check()
    def test_go_json_named_pass_policy(self):
        receipt = dict(stdout='{"Action":"pass","Package":"p","Test":"TestActual"}\n{"Action":"pass","Package":"p"}\n', stderr='', exit_code=0, timed_out=False)
        self.assertEqual(evidence._proof(receipt, 'go-test-json')['positive_count'], 1)
        receipt['stdout'] += '{"Action":"skip","Test":"TestSkipped"}\n'
        with self.assertRaises(evidence.EvidenceError): evidence._proof(receipt, 'go-test-json')
    def test_tap_named_pass_and_skip_policy(self):
        receipt = dict(stdout='TAP version 13\nok 1 - actual assertion\n1..1\n# tests 1\n# suites 0\n# pass 1\n# fail 0\n# cancelled 0\n# skipped 0\n# todo 0\n', stderr='', exit_code=0, timed_out=False)
        self.assertEqual(evidence._proof(receipt, 'node-tap')['positive_count'], 1)
        receipt['stdout'] += '# skipped 1\n'
        with self.assertRaises(evidence.EvidenceError): evidence._proof(receipt, 'node-tap')
    def test_proof_policy_bound_to_trusted_registry(self):
        result = self.run_check()
        with self.assertRaises(evidence.EvidenceError): self.publish(result, [dict(self.check, proof='unittest')])

    def test_ignored_generated_outputs_and_secret_links_do_not_drift(self):
        before = evidence.source_digest(self.root)
        (self.root/'dist').mkdir(); (self.root/'dist/out.js').write_text('build')
        (self.root/'node_modules').symlink_to('/nonexistent', target_is_directory=True)
        (self.root/'.env').symlink_to('/nonexistent-secret')
        (self.root/'__pycache__').mkdir(); (self.root/'__pycache__/cache.pyc').write_bytes(b'cache')
        self.assertEqual(before, evidence.source_digest(self.root))
        with self.assertRaises(evidence.EvidenceError):
            evidence._paths(['dist/out.js'], self.root)
    def test_actual_python_import_cache_does_not_mutate_source(self):
        (self.root/'helper.py').write_text('VALUE = 4\n')
        self.script.write_text('import sys\nsys.dont_write_bytecode = False\nimport helper\nassert helper.VALUE == 4\n')
        self.assertTrue(self.publish(self.run_check())['verified'])
        self.assertTrue((self.root/'__pycache__').is_dir())
    def test_actual_executable_mode_drift_rejected(self):
        result = self.run_check()
        self.script.chmod(0o755)
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_special_source_modes_rejected(self):
        self.script.chmod(0o4755)
        with self.assertRaises(evidence.EvidenceError): evidence.source_digest(self.root)

    def test_non_executable_permission_drift_rejected(self):
        self.script.chmod(0o644)
        result = self.run_check()
        self.script.chmod(0o600)
        with self.assertRaises(evidence.EvidenceError): self.publish(result)
    def test_negative_skip_case_name_is_not_runtime_skip(self):
        self.script.write_text("import unittest\nclass Real(unittest.TestCase):\n def test_skipped_unittest_rejected(self): self.assertEqual(2+2,4)\nunittest.main(verbosity=2)\n")
        self.check['proof'] = 'unittest'
        result = self.run_check()
        self.assertEqual(result['receipts'][0]['proof']['positive_count'], 1)
        self.assertTrue(self.publish(result)['verified'])

if __name__ == '__main__': unittest.main()
