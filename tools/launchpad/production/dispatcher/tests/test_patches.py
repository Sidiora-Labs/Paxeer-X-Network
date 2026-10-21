import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
import tomllib

import sys
sys.path.insert(0, str(Path(__file__).parents[2]))
from dispatcher import patches as p

stage_raw = p.stage_patches
def stage(baseline, patches, destination, verify=None):
    return stage_raw(baseline, patches, destination, verify, {m['patch_digest']: m['owned'] for m in patches})

class PatchTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.base = self.root / 'base'; self.base.mkdir()
        (self.base / 'svc').mkdir()
        (self.base / 'svc/a').write_text('a')
        (self.base / 'svc/b').write_text('b')
    def candidate(self, name='candidate'):
        import shutil
        target = self.root / name
        shutil.copytree(self.base, target)
        return target
    def patch(self, candidate, name='patch'):
        return p.build_patch(self.base, candidate, ['svc/**'], self.root / name)
    def test_add_modify_delete_deterministic(self):
        a = self.candidate(); (a/'svc/a').write_text('new'); (a/'svc/b').unlink(); (a/'svc/c').write_text('c')
        manifest = self.patch(a)
        one = stage(self.base, [manifest], self.root/'stage1', lambda _: True)
        two = stage(self.base, [manifest], self.root/'stage2', lambda _: True)
        self.assertEqual(one['result_digest'], two['result_digest'])
        self.assertEqual((self.base/'svc/a').read_text(), 'a')
        self.assertFalse((self.root/'stage1/svc/b').exists())
    def test_order_independent(self):
        a=self.candidate('a'); b=self.candidate('b')
        (a/'svc/a').write_text('A'); (b/'svc/b').write_text('B')
        patches=[p.build_patch(self.base,a,['svc/a'],self.root/'pa'), p.build_patch(self.base,b,['svc/b'],self.root/'pb')]
        x=stage(self.base,patches,self.root/'x',lambda _:True)
        y=stage(self.base,list(reversed(patches)),self.root/'y',lambda _:True)
        self.assertEqual(x['result_digest'],y['result_digest'])
    def test_duplicate_deletions_rejected(self):
        a=self.candidate(); (a/'svc/a').unlink(); patch=self.patch(a)
        with self.assertRaises(p.PatchError): stage(self.base,[patch,patch],self.root/'s',lambda _:True)
        self.assertFalse((self.root/'s').exists())
    def test_verification_failure_cleanup(self):
        a=self.candidate(); (a/'svc/a').write_text('A'); patch=self.patch(a)
        with self.assertRaises(p.PatchError): stage(self.base,[patch],self.root/'s',lambda _:False)
        self.assertFalse((self.root/'s').exists()); self.assertEqual((self.base/'svc/a').read_text(),'a')
    def test_no_verifier_rejected(self):
        with self.assertRaises(p.PatchError): stage(self.base,[],self.root/'s')
    def test_verifier_mutation_rejected(self):
        a=self.candidate(); (a/'svc/a').write_text('A'); patch=self.patch(a)
        def mutation(stage): (stage/'svc/a').write_text('evil'); return True
        with self.assertRaises(p.PatchError): stage(self.base,[patch],self.root/'s',mutation)
        self.assertFalse((self.root/'s').exists())
    def test_command_verifier(self):
        import sys
        a=self.candidate(); (a/'svc/a').write_text('A'); patch=self.patch(a)
        result=stage(self.base,[patch],self.root/'s',[sys.executable,'-c','from pathlib import Path; assert Path("svc/a").read_text()=="A"'])
        self.assertTrue(result['qualified'])
    def test_symlink_rejected(self):
        a=self.candidate(); (a/'svc/link').symlink_to(self.base/'svc/a')
        with self.assertRaises(p.PatchError): self.patch(a)
    def test_directory_symlink_rejected(self):
        a=self.candidate(); (a/'svc/link').symlink_to(self.base, target_is_directory=True)
        with self.assertRaises(p.PatchError): self.patch(a)
    def test_hardlink_rejected(self):
        a=self.candidate(); os.link(a/'svc/a',a/'svc/hard')
        with self.assertRaises(p.PatchError): self.patch(a)
    def test_special_file_rejected(self):
        a=self.candidate(); os.mkfifo(a/'svc/fifo')
        with self.assertRaises(p.PatchError): self.patch(a)
    def test_outside_ownership(self):
        a=self.candidate(); (a/'outside').write_text('x')
        with self.assertRaises(p.PatchError): self.patch(a)
    def test_reserved_paths(self):
        for path in ('spec/x.kvx','shared/interfaces/v1/x.go','svc/package.json','svc/pnpm-lock.yaml','svc/go.mod','svc/test.gen.go','go.work','go.work.sum','client/pnpm-workspace.yaml','svc/cmd/server/main.go','svc/internal/app/app.go','svc/migrations/1.sql','svc/internal/db/sqlcdb/x.go','svc/sqlc.yaml','client/packages/production-contracts/a.ts'):
            with self.subTest(path=path): self.assertTrue(p._reserved(path))
    def test_reserved_change(self):
        a=self.candidate(); (a/'svc/package.json').write_text('{}')
        with self.assertRaises(p.PatchError): self.patch(a)
    def test_unsafe_ownership(self):
        a=self.candidate(); (a/'svc/a').write_text('A')
        for pattern in ('/svc/**','../svc/**','svc/../**','svc//**','svc\\**','**','*'):
            with self.subTest(pattern=pattern),self.assertRaises(p.PatchError): p.build_patch(self.base,a,[pattern],self.root/'o')
    def test_path_negatives(self):
        for path in ('/tmp/x','C:/tmp/x','../x','a/../x','a//b','a/./b','a\\b','a\x00b',''):
            with self.subTest(path=path), self.assertRaises(p.PatchError): p._path(path)
    def test_blob_tamper(self):
        a=self.candidate(); (a/'svc/a').write_text('A'); patch=self.patch(a)
        (self.root/'patch/blobs'/patch['entries'][0]['after']['sha256']).write_text('bad')
        with self.assertRaises(p.PatchError): stage(self.base,[patch],self.root/'s',lambda _:True)
    def test_manifest_tamper(self):
        a=self.candidate(); (a/'svc/a').write_text('A'); patch=self.patch(a)
        Path(patch['manifest_path']).write_text('{}')
        with self.assertRaises(p.PatchError): stage(self.base,[patch],self.root/'s',lambda _:True)
    def test_baseline_changed(self):
        a=self.candidate(); (a/'svc/a').write_text('A'); patch=self.patch(a); (self.base/'svc/b').write_text('B')
        with self.assertRaises(p.PatchError): stage(self.base,[patch],self.root/'s',lambda _:True)
    def test_overlapping_stage(self):
        a=self.candidate(); (a/'svc/a').write_text('A'); patch=self.patch(a)
        with self.assertRaises(p.PatchError): stage(self.base,[patch],self.base/'stage',lambda _:True)
    def test_existing_output(self):
        a=self.candidate(); (a/'svc/a').write_text('A')
        with self.assertRaises(p.PatchError): p.build_patch(self.base,a,['svc/**'],a)
    def test_missing_trusted_ownership(self):
        a=self.candidate(); (a/'svc/a').write_text('A'); patch=self.patch(a)
        with self.assertRaises(p.PatchError): stage_raw(self.base,[patch],self.root/'s',lambda _:True)
    def test_overlapping_ownership_even_distinct_files(self):
        a=self.candidate('a'); b=self.candidate('b'); (a/'svc/a').write_text('A'); (b/'svc/b').write_text('B')
        patches=[self.patch(a,'pa'),self.patch(b,'pb')]
        with self.assertRaises(p.PatchError): stage(self.base,patches,self.root/'s',lambda _:True)
    def test_symlink_output_ancestor(self):
        a=self.candidate(); (a/'svc/a').write_text('A'); (self.root/'link').symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(p.PatchError): p.build_patch(self.base,a,['svc/**'],self.root/'link/out')
    def test_output_inside_candidate(self):
        a=self.candidate(); (a/'svc/a').write_text('A')
        with self.assertRaises(p.PatchError): p.build_patch(self.base,a,['svc/**'],a/'out')
    def test_ignored_state_filtered_from_diff_and_stage(self):
        (self.base/'node_modules').symlink_to(self.root/'missing', target_is_directory=True)
        (self.base/'.next').mkdir(); (self.base/'.next/cache').write_text('generated')
        (self.base/'__pycache__').mkdir(); (self.base/'__pycache__/x.pyc').write_bytes(b'cache')
        (self.base/'.env').write_text('SECRET=private')
        # Materialize source independently; ignored state differs freely.
        a=self.root/'candidate'; (a/'svc').mkdir(parents=True)
        (a/'svc/a').write_text('A'); (a/'svc/b').write_text('b')
        (a/'node_modules').symlink_to(self.root/'elsewhere', target_is_directory=True)
        (a/'.env.local').write_text('SECRET=other')
        patch=self.patch(a)
        self.assertEqual([e['path'] for e in patch['entries']], ['svc/a'])
        stage(self.base,[patch],self.root/'s',lambda _:True)
        self.assertEqual(sorted(x.name for x in (self.root/'s').iterdir()),['svc'])
    def test_canonical_fifty_lane_ownership(self):
        spec_path = Path('/root/mk-falcon/KindleLaunch/spec/production-system/spec.kvx')
        data=tomllib.loads(spec_path.read_text())
        lanes=data['lane']
        self.assertEqual(len(lanes),50)
        for lane, declaration in sorted(lanes.items()):
            with self.subTest(lane=lane):
                patterns=declaration['write_paths']
                a=self.candidate('candidate-'+lane)
                path='spec/production-system/handoffs/'+lane+'/phase1/result.json'
                target=a/path; target.parent.mkdir(parents=True); target.write_text('{}')
                patch=p.build_patch(self.base,a,patterns,self.root/('patch-'+lane))
                self.assertEqual([entry['path'] for entry in patch['entries']],[path])
                result=stage_raw(self.base,[patch],self.root/('stage-'+lane),lambda _:True,{patch['patch_digest']:patterns})
                self.assertTrue(result['qualified'])
                self.assertTrue((self.root/('stage-'+lane)/path).exists())
    def test_handoff_exception_bounded(self):
        for path in ('spec/production-system/handoffs/L00/a','spec/production-system/handoffs/L51/a','spec/production-system/handoffs/L1/a','spec/production-system/handoffs/**','spec/production-system/spec.kvx'):
            with self.subTest(path=path): self.assertTrue(p._reserved(path))
    def test_empty_patch(self):
        with self.assertRaises(p.PatchError): self.patch(self.candidate())

if __name__ == '__main__': unittest.main()
