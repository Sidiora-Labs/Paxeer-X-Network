import hashlib
import io
import os
from pathlib import Path
import signal
import sys
import tarfile
import tempfile
import time
import unittest
from ..workspace import WorkspaceError, create_archive, create_workspace, namespace, run_process

class WorkspaceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.source = self.root / 'source'
        self.source.mkdir()
        (self.source / 'a.txt').write_text('source')
    def tearDown(self):
        self.temp.cleanup()
    def test_deterministic_archive_and_exclusion(self):
        (self.source / '.env.production').write_text('secret')
        (self.source / 'node_modules').mkdir()
        (self.source / 'node_modules' / 'x').write_text('ignored')
        first = create_archive(self.source, self.root / 'one.tar')
        os.utime(self.source / 'a.txt', (17, 17))
        second = create_archive(self.source, self.root / 'two.tar')
        self.assertEqual(first, second)
        self.assertEqual(create_workspace(self.root/'one.tar', self.root/'work'), first)
        self.assertEqual(sorted(x.name for x in (self.root/'work').iterdir()), ['a.txt'])
        self.assertEqual((self.root/'one.tar').stat().st_mode & 0o777, 0o444)
        with self.assertRaises(WorkspaceError): create_archive(self.source, self.root/'one.tar')
    def test_shared_policy_ignores_links_and_caches_before_stat(self):
        (self.source/'node_modules').symlink_to(self.root/'missing-dependencies', target_is_directory=True)
        (self.source/'.env').symlink_to('/outside-secret-do-not-read')
        (self.source/'.next').mkdir()
        (self.source/'.next'/'cache').symlink_to('/outside-generated-do-not-read')
        (self.source/'build').mkdir()
        (self.source/'build'/'source.txt').write_text('build is source under shared policy')
        first=create_archive(self.source,self.root/'one.tar')
        create_workspace(self.root/'one.tar',self.root/'work')
        self.assertEqual(sorted(p.name for p in (self.root/'work').iterdir()),['a.txt','build'])
        self.assertTrue((self.root/'work'/'build'/'source.txt').exists())
        (self.source/'a.txt').write_text('changed source')
        second=create_archive(self.source,self.root/'two.tar')
        self.assertNotEqual(first,second)
    def test_source_links_and_special_rejected(self):
        for kind in ('symlink', 'hardlink', 'fifo'):
            with self.subTest(kind=kind):
                target = self.source/'bad'
                if kind == 'symlink': target.symlink_to('/etc/passwd')
                elif kind == 'hardlink': os.link(self.source/'a.txt', target)
                else: os.mkfifo(target)
                with self.assertRaises(WorkspaceError): create_archive(self.source, self.root/'bad.tar')
                target.unlink()
    def test_archive_traversal_and_links_rejected(self):
        for name, kind in (('../escape', tarfile.REGTYPE), ('/absolute', tarfile.REGTYPE), ('dir/../../escape', tarfile.REGTYPE), ('safe', tarfile.SYMTYPE), ('safe', tarfile.LNKTYPE), ('safe', tarfile.FIFOTYPE)):
            with self.subTest(name=name, kind=kind):
                archive=self.root/'bad.tar'
                with tarfile.open(archive, 'w') as tf:
                    info=tarfile.TarInfo(name); info.type=kind; info.linkname='/etc/passwd'
                    tf.addfile(info, io.BytesIO())
                with self.assertRaises(WorkspaceError): create_workspace(archive, self.root/'work')
                self.assertFalse((self.root/'work').exists())
    def test_destination_symlink_and_internal_archive_rejected(self):
        (self.root/'alias').symlink_to(self.source, target_is_directory=True)
        with self.assertRaises(WorkspaceError): create_archive(self.source, self.root/'alias'/'bad.tar')
        with self.assertRaises(WorkspaceError): create_archive(self.source, self.source/'bad.tar')
    def test_extraction_limits(self):
        archive=self.root/'one.tar'
        create_archive(self.source, archive)
        for limits in ({'max_archive_bytes': 1}, {'max_member_bytes': 1}, {'max_expanded_bytes': 1}):
            with self.subTest(limits=limits):
                with self.assertRaises(WorkspaceError): create_workspace(archive,self.root/'work',**limits)
                self.assertFalse((self.root/'work').exists())
        (self.source/'second').write_text('second')
        create_archive(self.source,self.root/'two.tar')
        with self.assertRaises(WorkspaceError): create_workspace(self.root/'two.tar',self.root/'work',max_members=1)
    def test_namespace_all_inputs_bind(self):
        binding={k:hashlib.sha256(k.encode()).hexdigest() for k in ('baseline','candidate','interface','layout')}
        original=namespace(binding,'1.1','L01','attempt')
        for key in binding:
            changed=dict(binding); changed[key]='0'*64
            self.assertNotEqual(original, namespace(changed,'1.1','L01','attempt'))
        self.assertNotEqual(original,namespace(binding,'1.1','L01','next'))
        with self.assertRaises(WorkspaceError): namespace(binding,'../escape','L01','attempt')
    def test_real_process_output_bound_and_exit(self):
        result=run_process([sys.executable,'-c','import sys; print("x"*2000000); sys.stderr.write("error"); sys.exit(7)'],self.source,5)
        self.assertEqual(result['exit_code'],7)
        self.assertEqual(len(result['stdout']),1024*1024)
        self.assertEqual(result['stderr'],'error')
        self.assertFalse(result['timed_out'])
        self.assertTrue(result['cleanup'])
    def test_timeout_kills_owned_child_only(self):
        survivor=__import__('subprocess').Popen([sys.executable,'-c','import time; time.sleep(30)'])
        try:
            script='import subprocess,sys,time; p=subprocess.Popen([sys.executable,"-c","import time; time.sleep(30)"]); print(p.pid,flush=True); time.sleep(30)'
            result=run_process([sys.executable,'-c',script],self.source,0.3)
            self.assertTrue(result['timed_out']); self.assertTrue(result['cleanup'])
            child=int(result['stdout'].strip())
            for _ in range(50):
                path=Path(f'/proc/{child}/stat')
                if not path.exists() or path.read_text().split()[2]=='Z': break
                time.sleep(.02)
            else: self.fail('Owned child still running')
            self.assertIsNone(survivor.poll())
            self.assertLess(result['duration'],5)
        finally:
            survivor.kill(); survivor.wait()
    def test_sigterm_coordinator_cleans_child_and_grandchild(self):
        from ..qualification_timeout import qualify_timeout
        result = qualify_timeout(Path(__file__).resolve().parents[5])
        self.assertTrue(result['passed'])
        self.assertTrue(result['owned_descendants_stopped'])
        self.assertTrue(result['unrelated_process_survived'])
    def test_signal_handlers_restored(self):
        handlers = {s: signal.getsignal(s) for s in (signal.SIGTERM, signal.SIGINT)}
        run_process([sys.executable,'-c','pass'],self.source,5)
        self.assertEqual(handlers,{s: signal.getsignal(s) for s in handlers})
    def test_exited_leader_does_not_leave_child(self):
        script='import subprocess,sys; p=subprocess.Popen([sys.executable,"-c","import time; time.sleep(30)"]); print(p.pid,flush=True)'
        result=run_process([sys.executable,'-c',script],self.source,5)
        self.assertTrue(result['cleanup']); self.assertFalse(result['timed_out'])
        child=int(result['stdout'].strip())
        path=Path(f'/proc/{child}/stat')
        from ..qualification_timeout import _running
        self.assertFalse(_running(child))

if __name__ == '__main__': unittest.main()
