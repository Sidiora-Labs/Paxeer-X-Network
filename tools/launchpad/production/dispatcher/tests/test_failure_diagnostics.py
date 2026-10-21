"""Actual subprocess failures retain unsigned private receipts, never acceptance."""
import json
from pathlib import Path
import stat
import sys
import tempfile
import unittest
sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from dispatcher import evidence

class FailureDiagnosticsTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        self.root = self.base/'source'; self.root.mkdir()
        (self.root/'input.txt').write_text('bound input')
        self.out = self.base/'receipts'
        self.binding = {k:'a'*64 for k in ('baseline','candidate','interface','layout')}
        self.binding['candidate'] = evidence.source_digest(self.root)
        self.token = dict(bindings=self.binding, attempt='one', fence=1)
        self.secret = b'private-test-coordinator-key-000001'
    def run_case(self, code, **overrides):
        check = dict(id='../../evidence.json', command=[sys.executable,'-c',code],
                     timeout=3, inputs=['input.txt'], proof='unittest')
        check.update(overrides)
        with self.assertRaises(evidence.EvidenceError):
            evidence.run_checks([check], self.root, self.out, self.binding, self.token, self.secret)
        paths = list(self.out.glob('unqualified-*/*.json'))
        self.assertTrue(paths)
        path = max(paths, key=lambda p:p.stat().st_mtime_ns)
        body = json.loads(path.read_text())
        self.assertEqual(body['kind'],'unqualified-command-diagnostic')
        self.assertIs(body['acceptance_eligible'],False)
        self.assertNotIn('signature',body)
        self.assertEqual(body['bindings'],self.binding)
        self.assertEqual(body['source_before'],self.binding['candidate'])
        self.assertEqual(body['receipt']['command'],check['command'])
        self.assertEqual(body['receipt']['inputs'], {str(self.root/'input.txt'):evidence._file(self.root/'input.txt')})
        self.assertTrue(body['receipt']['checker'])
        self.assertEqual(stat.S_IMODE(path.stat().st_mode),0o600)
        self.assertEqual(stat.S_IMODE(path.parent.stat().st_mode),0o700)
        with self.assertRaisesRegex(evidence.EvidenceError,'unauthenticated'):
            evidence.publish(body,self.secret,None,self.root,[check])
        return body['receipt']
    def test_native_failure_exact_raw_output_no_signed_evidence(self):
        receipt=self.run_case("import sys;print('actual stdout');print('actual stderr',file=sys.stderr);sys.exit(7)")
        self.assertEqual(receipt['stdout'],'actual stdout\n')
        self.assertEqual(receipt['stderr'],'actual stderr\n')
        self.assertEqual(receipt['exit_code'],7)
        self.assertFalse(receipt['timed_out'])
        self.assertGreaterEqual(receipt['duration'],0)
        self.assertFalse((self.out/'evidence.json').exists())
        self.assertFalse((self.base/'evidence.json').exists())
    def test_actual_unittest_failure_preserved(self):
        receipt=self.run_case("import unittest\nclass Real(unittest.TestCase):\n def test_actual(self): self.assertEqual(2+2,5)\nunittest.main(verbosity=2)\n")
        self.assertEqual(receipt['exit_code'],1)
        self.assertIn('FAILED (failures=1)',receipt['stderr'])
        self.assertIn('AssertionError: 4 != 5',receipt['stderr'])
    def test_parse_failure_preserved(self):
        self.assertEqual(self.run_case("print('not native test proof')")['exit_code'],0)
    def test_output_boundary_failure_preserved(self):
        self.assertEqual(self.run_case("print('real executed')",outputs=['input.txt'])['stdout'],'real executed\n')
    def test_timeout_preserved(self):
        receipt=self.run_case("import time;print('before timeout',flush=True);time.sleep(5)",timeout=.1)
        self.assertTrue(receipt['timed_out'])
        self.assertEqual(receipt['stdout'],'before timeout\n')
    def test_prior_signed_success_not_overwritten_and_invocations_fresh(self):
        good = dict(id='native',command=[sys.executable,'-c',"print('actual success')"],timeout=3)
        success=evidence.run_checks([good],self.root,self.out,self.binding,self.token,self.secret)
        self.assertIn('signature',success)
        prior=(self.out/'evidence.json').read_bytes()
        self.run_case("print('attempt one')")
        self.run_case("print('attempt two')")
        self.assertEqual((self.out/'evidence.json').read_bytes(),prior)
        self.assertEqual(len(list(self.out.glob('unqualified-*'))),3)
    def test_symlink_output_rejected_without_external_write(self):
        external=self.base/'external';external.mkdir();self.out.symlink_to(external,target_is_directory=True)
        with self.assertRaisesRegex(evidence.EvidenceError,'directory symlink'):
            evidence.run_checks([dict(id='x',command=[sys.executable,'-c',"print('x')"],timeout=1)],self.root,self.out,self.binding,self.token,self.secret)
        self.assertEqual(list(external.iterdir()),[])
    def test_actual_invocation_and_output_directory_swaps_cannot_escape(self):
        for target in ('invocation','output'):
            with self.subTest(target=target):
                self.out=self.base/('receipts-'+target)
                external=self.base/('external-'+target);external.mkdir()
                selected="next(out.glob('unqualified-*'))" if target=='invocation' else 'out'
                code=f"from pathlib import Path;out=Path({str(self.out)!r});p={selected};p.rename(p.with_name(p.name+'-moved'));p.symlink_to({str(external)!r},target_is_directory=True);print('actual swap')"
                check=dict(id='swap',command=[sys.executable,'-c',code],timeout=3)
                with self.assertRaisesRegex(evidence.EvidenceError,'directory replaced'):
                    evidence.run_checks([check],self.root,self.out,self.binding,self.token,self.secret)
                self.assertEqual(list(external.iterdir()),[])
                retained=list(self.base.glob('receipts-*/*/0000.json'))
                self.assertTrue(retained)
                self.assertTrue(any(json.loads(p.read_text())['receipt']['stdout']=='actual swap\n' for p in retained))
    def test_signed_destination_links_rejected_without_external_changes(self):
        import os
        for kind in ('symlink','hardlink'):
            with self.subTest(kind=kind):
                self.out=self.base/('signed-'+kind);self.out.mkdir()
                external=self.base/('prior-'+kind);external.write_bytes(b'unchanged')
                dest=self.out/'evidence.json'
                if kind=='symlink':dest.symlink_to(external)
                else:os.link(external,dest)
                check=dict(id='good',command=[sys.executable,'-c',"print('actual pass')"],timeout=3)
                with self.assertRaisesRegex(evidence.EvidenceError,'unsafe existing'):
                    evidence.run_checks([check],self.root,self.out,self.binding,self.token,self.secret)
                self.assertEqual(external.read_bytes(),b'unchanged')
                self.assertEqual(len(list(self.out.glob('unqualified-*/*.json'))),1)
    def test_source_mutation_failure_raw_preserved(self):
        self.run_case("from pathlib import Path;Path('new.txt').write_text('actual mutation');print('native source mutation')",proof=None)
        self.assertFalse((self.out/'evidence.json').exists())

if __name__ == '__main__': unittest.main()
