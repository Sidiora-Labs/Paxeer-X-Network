#!/usr/bin/env python3
import argparse
import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

PRODUCER = Path(__file__).resolve().parents[1] / 'registry-artifacts.py'
spec = importlib.util.spec_from_file_location('registry_artifacts', PRODUCER)
artifacts = importlib.util.module_from_spec(spec)
spec.loader.exec_module(artifacts)
MANIFEST = None


def sandbox(script):
    return ['/usr/bin/bwrap', '--unshare-user', '--unshare-all', '--die-with-parent',
            '--new-session', '--disable-userns', '--cap-drop', 'ALL', '--clearenv',
            '--ro-bind', '/builder', '/', '--dir', '/build', '--bind', '/quota/slot-0', '/build',
            '--tmpfs', '/tmp', '--proc', '/proc', '--dev', '/dev', '--chdir', '/build',
            '--', '/opt/node/bin/node', '-e', script]


class RegistryArtifacts(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.manifest = artifacts.validate(MANIFEST)
        cls.evidence = Path(MANIFEST).parent / 'qualification'
        cls.evidence.mkdir(mode=0o700)
        cls.fixture = cls.evidence / 'fixture'
        cls.sentinel = cls.evidence / 'unrelated-sentinel'
        cls.sentinel.write_bytes(b'owned by another lifecycle\n')
        cls.addClassCleanup(cls.cleanup_fixture)
        artifacts.prepare(MANIFEST, cls.fixture)

    @classmethod
    def cleanup_fixture(cls):
        if (cls.fixture / 'lifecycle.json').exists():
            result = artifacts.cleanup(cls.fixture)
            if result['phase'] != 'cleaned' or not all(result['cleanup'].values()):
                raise AssertionError('actual resource cleanup incomplete')
        if cls.sentinel.read_bytes() != b'owned by another lifecycle\n':
            raise AssertionError('unrelated sentinel changed')

    def negative_manifest(self, mutate, phrase):
        value = copy.deepcopy(self.manifest)
        mutate(value)
        path = self.evidence / (self.id().split('.')[-1] + '.json')
        artifacts.write_json(path, value)
        with self.assertRaisesRegex((RuntimeError, OSError), phrase):
            artifacts.validate(path)

    def test_01_real_bound_artifacts_and_quota(self):
        self.assertEqual(self.manifest['registry_readiness'], 'unclaimed')
        current = artifacts.inspect(self.fixture)
        self.assertEqual(current['phase'], 'prepared')
        self.assertLessEqual(current['mount']['bytes'], artifacts.QUOTA_BYTES)
        self.assertLessEqual(current['mount']['inodes'], artifacts.QUOTA_INODES)

    def test_02_wrong_source_refused(self):
        self.negative_manifest(lambda value: value['source'].update(revision='0' * 40), 'source identity mismatch')

    def test_03_missing_executable_refused(self):
        self.negative_manifest(lambda value: value['artifacts']['layerx-cgroup-exec'].update(path=str(self.evidence / 'absent')),
                               'No such file')

    def test_04_changed_binary_digest_refused(self):
        self.negative_manifest(lambda value: value['artifacts']['layerx-cgroup-exec'].update(sha256='0' * 64),
                               'executable digest mismatch')

    def test_05_wrong_isolation_digest_refused(self):
        self.negative_manifest(lambda value: value.update(isolation_sha256='0' * 64), 'isolation image evidence mismatch')

    def test_06_changed_rootfs_digest_refused(self):
        self.negative_manifest(lambda value: value.update(builder_digest='0' * 64), 'builder rootfs digest mismatch')

    def test_07_dirty_source_refused(self):
        directory = self.evidence / 'source-negative'
        directory.mkdir()
        def git(*args):
            subprocess.run(['git', '-C', str(directory), *args], check=True, capture_output=True)
        git('init', '--initial-branch=main')
        git('config', 'user.name', 'Registry artifact qualification')
        git('config', 'user.email', 'registry-test@example.invalid')
        path = directory / 'source.txt'
        path.write_text('committed public fixture\n')
        git('add', 'source.txt')
        git('commit', '-m', 'Public source fixture')
        path.write_text('changed public fixture\n')
        with self.assertRaisesRegex(RuntimeError, 'source checkout is dirty'):
            artifacts.source(directory)

    def test_08_production_privilege_drop_boundary(self):
        artifacts.startup_boundary(self.manifest, self.evidence / 'startup-boundary.json')

    def test_09_mandatory_supervisor_flags(self):
        result = artifacts.run_job(self.fixture, ['/bin/true'], mandatory=False)
        self.assertNotEqual(result['exit_code'], 0)
        self.assertIn('mandatory cgroup supervisor contract is absent', result['stderr'])

    def test_10_real_isolation_and_attachment(self):
        script = '''const fs=require('fs'),os=require('os');
if(process.getuid()!==4030)throw Error('wrong uid');
if(Object.keys(os.networkInterfaces()).some(x=>x!=='lo'))throw Error('network leaked');
let denied=false;try{fs.writeFileSync('/outside-slot','bad')}catch(e){denied=['EROFS','EACCES'].includes(e.code)}
if(!denied)throw Error('rootfs writable');
fs.writeFileSync('/build/real-isolation','owned');
console.log('isolated-uid4030');setTimeout(()=>{},300);'''
        result = artifacts.run_job(self.fixture, sandbox(script), cpu=2000000)
        self.assertEqual(result['exit_code'], 0, result['stderr'])
        self.assertIn('isolated-uid4030', result['stdout'])
        self.assertTrue(any(s.get('cgroup.procs', '').strip() for s in result['observations']), 'no actual attached child observed')
        self.assertTrue(any(s.get('memory.max', '').strip() == '67108864' for s in result['observations']))
        self.assertTrue(any(s.get('pids.max', '').strip() == '16' for s in result['observations']))
        (self.fixture / 'quota/slot-0/real-isolation').unlink()

    def test_11_memory_limit(self):
        script = "let blocks=[];console.log('allocating');setInterval(()=>blocks.push(Buffer.alloc(16*1024*1024,1)),30);"
        result = artifacts.run_job(self.fixture, sandbox(script), memory=134217728, cpu=4000000, wall=10000)
        self.assertNotEqual(result['exit_code'], 0)
        self.assertIn('allocating', result['stdout'])
        self.assertLess(result['elapsed'], 8)
        self.assertTrue(any('oom_kill 1' in s.get('memory.events', '') or 'oom 1' in s.get('memory.events', '')
                            for s in result['observations']), 'no kernel memory refusal observed')

    def test_12_pid_limit(self):
        script = '''const cp=require('child_process');let refused=false;
for(let i=0;i<40;i++){let p=cp.spawn('/bin/sleep',['20']);p.on('error',e=>{if(e.code==='EAGAIN'){refused=true;console.log('pid-refused')}})}
setTimeout(()=>process.exit(refused?0:9),500);'''
        result = artifacts.run_job(self.fixture, sandbox(script), pids=16, cpu=4000000, wall=5000)
        self.assertEqual(result['exit_code'], 0, result['stderr'])
        self.assertIn('pid-refused', result['stdout'])
        self.assertTrue(any('max 0' not in s.get('pids.events', 'max 0') for s in result['observations']),
                        'no kernel PID refusal observed')

    def test_13_cpu_limit(self):
        result = artifacts.run_job(self.fixture, sandbox("console.log('burning');while(true){}"),
                                   cpu=200000, wall=10000)
        self.assertNotEqual(result['exit_code'], 0)
        self.assertIn('burning', result['stdout'])
        self.assertLess(result['elapsed'], 8)
        self.assertTrue(any(int(s.get('cpu.stat', 'usage_usec 0').split()[1]) >= 200000
                            for s in result['observations']), 'no actual CPU ceiling observed')

    def test_14_wall_limit_and_descendant_cleanup(self):
        result = artifacts.run_job(self.fixture, sandbox("require('child_process').spawn('/bin/sleep',['60']);console.log('sleeping');setTimeout(()=>{},60000)"),
                                   cpu=4000000, wall=700)
        self.assertNotEqual(result['exit_code'], 0)
        self.assertIn('sleeping', result['stdout'])
        self.assertLess(result['elapsed'], 10)
        self.assertTrue(all(c['phase'] == 'cleaned' for c in artifacts.inspect(self.fixture)['containers']))

    def test_15_byte_quota(self):
        script = '''const fs=require('fs');let file='/build/byte-quota',fd=fs.openSync(file,'w'),n=0;
try{let b=Buffer.alloc(1024*1024,1);while(true)n+=fs.writeSync(fd,b)}catch(e){if(e.code!=='ENOSPC')throw e;
if(n<=0||n>5368709120)throw Error('byte bound');console.log('byte-refused '+n)}finally{fs.closeSync(fd);fs.unlinkSync(file)}'''
        result = artifacts.run_job(self.fixture, sandbox(script), memory=134217728, cpu=60000000,
                                   wall=120000, io_limit=artifacts.QUOTA_BYTES * 2)
        self.assertEqual(result['exit_code'], 0, result['stderr'])
        self.assertIn('byte-refused ', result['stdout'])

    def test_16_inode_quota(self):
        script = '''const fs=require('fs');let n=0;fs.mkdirSync('/build/inodes');
try{for(;;n++)fs.closeSync(fs.openSync('/build/inodes/'+n,'wx'))}catch(e){if(e.code!=='ENOSPC')throw e;
if(n<=0||n>65536)throw Error('inode bound');console.log('inode-refused '+n)}finally{for(let x of fs.readdirSync('/build/inodes'))fs.unlinkSync('/build/inodes/'+x);fs.rmdirSync('/build/inodes')}'''
        result = artifacts.run_job(self.fixture, sandbox(script), memory=134217728, cpu=60000000,
                                   wall=120000, io_limit=artifacts.QUOTA_BYTES * 2)
        self.assertEqual(result['exit_code'], 0, result['stderr'])
        self.assertIn('inode-refused ', result['stdout'])

    def test_17_interrupted_workload_and_fresh_inspection(self):
        result = artifacts.run_job(self.fixture, sandbox("setTimeout(()=>{},60000)"), wall=5000, interrupt=True)
        self.assertTrue(result['interrupted'])
        self.assertNotEqual(result['exit_code'], 0)
        resumed = subprocess.run([sys.executable, str(PRODUCER), 'inspect', '--directory', str(self.fixture)],
                                 check=True, capture_output=True, text=True, timeout=30)
        self.assertEqual(json.loads(resumed.stdout)['phase'], 'prepared')
        result = artifacts.run_job(self.fixture, ['/bin/true'])
        self.assertEqual(result['exit_code'], 0, result['stderr'])

    def test_18_unusable_quota_boundary_refused(self):
        wrong = self.evidence / 'unmounted'
        (wrong / 'quota/slot-0').mkdir(parents=True)
        with self.assertRaisesRegex(RuntimeError, 'not a distinct filesystem'):
            artifacts.quota_state(wrong)

    def test_19_missing_image_refused(self):
        self.negative_manifest(lambda value: value.update(runtime_image='sha256:' + '0' * 64), 'command failed')

    def test_20_unavailable_cgroup_refused(self):
        result = artifacts.run_job(self.fixture, ['/bin/true'], unavailable=True)
        self.assertNotEqual(result['exit_code'], 0)
        self.assertIn('could not create job cgroup', result['stderr'])

    def test_21_io_limit(self):
        script = "const fs=require('fs');let f=fs.openSync('/build/io-limit','w'),b=Buffer.alloc(131072,1);console.log('writing');setInterval(()=>{fs.writeSync(f,b);fs.fsyncSync(f)},10);"
        result = artifacts.run_job(self.fixture, sandbox(script), cpu=10000000, wall=15000, io_limit=1048576)
        self.assertNotEqual(result['exit_code'], 0)
        self.assertIn('writing', result['stdout'])
        self.assertLess(result['elapsed'], 13)
        written = [sum(int(word.split('=')[1]) for word in sample.get('io.stat', '').split()
                       if word.startswith('wbytes=')) for sample in result['observations']]
        self.assertTrue(any(value > 1048576 for value in written), 'no actual I/O ceiling observed')
        (self.fixture / 'quota/slot-0/io-limit').unlink()

    def test_99_interrupted_owner_reconciles_and_cleans(self):
        code = ("import importlib.util,sys; "
                "s=importlib.util.spec_from_file_location('producer',sys.argv[1]); "
                "m=importlib.util.module_from_spec(s);s.loader.exec_module(m); "
                "m.run_job(sys.argv[2],['/bin/sleep','60'],wall=60000,cpu=10000000)")
        with (self.evidence / 'interrupted-owner.log').open('w') as log:
            process = subprocess.Popen([sys.executable, '-c', code, str(PRODUCER), str(self.fixture)],
                                       stdout=log, stderr=subprocess.STDOUT)
            try:
                deadline = time.monotonic() + 20
                while True:
                    state = json.loads((self.fixture / 'lifecycle.json').read_text())
                    if state['phase'] == 'running' and state['containers'][-1]['phase'] == 'executing':
                        break
                    self.assertIsNone(process.poll(), 'owner process failed before interruption')
                    self.assertLess(time.monotonic(), deadline, 'owned workload did not start')
                    time.sleep(0.02)
                process.kill()
                process.wait(timeout=10)
                result = subprocess.run([sys.executable, str(PRODUCER), 'cleanup', '--directory', str(self.fixture)],
                                        capture_output=True, text=True, timeout=30)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(json.loads(result.stdout)['phase'], 'cleaned')
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=10)


if __name__ == '__main__':
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', required=True)
    arguments = parser.parse_args()
    MANIFEST = str(Path(arguments.manifest).resolve(strict=True))
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(RegistryArtifacts)
    result = unittest.TextTestRunner(verbosity=2, failfast=True).run(suite)
    print(f'PAXEER_X_GATE tests={result.testsRun} skipped={len(result.skipped)}', flush=True)
    sys.exit(0 if result.wasSuccessful() and result.testsRun == 22 and not result.skipped else 1)
