#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
import uuid

ROOT = Path(__file__).resolve().parents[3]
ROLES = {
    'human-components': ('components', 4020, 'layerx-human-components'),
    'human-identity': ('identity', 4020, 'layerx-human-identity-provider'),
    'human-security': ('security', 4020, 'layerx-human-security-provider'),
    'human-movement': ('movement', 4020, 'layerx-human-movement-provider'),
    'human-owner': ('agent', 4021, 'layerx-agentd'),
    'human': ('service', 4020, 'layerx-human-service'),
    'human-tls': ('service', 4020, 'layerx-human-service'),
}
PRIVATE = {role: ('/run/layerx/human/service-private' if role == 'service'
                  else '/run/human-private/' + role, uid)
           for role, uid, _ in ROLES.values()}
PRIVATE['kms'] = ('/run/human-private/kms', 4026)


def command(argv, timeout=30, check=True):
    result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True,
                            text=True, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError('command failed exit=' + str(result.returncode) + ': ' + argv[0])
    return result


def docker(*args, **kwargs):
    return command(['docker', *args], **kwargs)


class RoleDirectories(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.image = os.environ.get('PAXEER_X_RUNTIME_IMAGE', '')
        if not re.fullmatch(r'sha256:[0-9a-f]{64}', cls.image):
            raise RuntimeError('PAXEER_X_RUNTIME_IMAGE must identify the actual selected kernel image by sha256')
        cls.revision = command(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']).stdout.strip()
        if command(['git', '-C', str(ROOT), 'status', '--porcelain']).stdout.strip():
            raise RuntimeError('source must be committed before selected-image qualification')
        metadata = json.loads(docker('image', 'inspect', '--format',
                              '{"id":{{json .Id}},"revision":{{json (index .Config.Labels "org.opencontainers.image.revision")}}}',
                              cls.image).stdout)
        if metadata != {'id': cls.image, 'revision': cls.revision}:
            raise RuntimeError('selected image source revision mismatch')
        image_files = docker('run', '--rm', '--pull=never', '--network=none',
                             '--entrypoint', '/usr/bin/sha256sum', cls.image,
                             '/usr/local/bin/kernel-init', '/usr/local/bin/human-entrypoint').stdout.splitlines()
        for line, relative in zip(image_files, ('docker/kernel/init.sh', 'docker/human-service/entrypoint.sh')):
            if line.split()[0] != hashlib.sha256((ROOT / relative).read_bytes()).hexdigest():
                raise RuntimeError('selected image does not contain current ' + relative)
        if len(image_files) != 2:
            raise RuntimeError('selected image source identity incomplete')
        cls.scratch = Path(tempfile.mkdtemp(prefix='paxeer-x-role-directories-'))
        os.chmod(cls.scratch, 0o700)
        command(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                 '-keyout', str(cls.scratch / 'ca.key'), '-out', str(cls.scratch / 'ca.crt'),
                 '-days', '1', '-subj', '/CN=Disposable role-directory fixture'], timeout=30)
        cls.fixture = None
        cls.network = 'none'
        raw = os.environ.get('PAXEER_X_RUNTIME_FIXTURE_DIR')
        if raw:
            cls.fixture = Path(raw).resolve(strict=True)
            info = cls.fixture.stat()
            if info.st_uid != os.geteuid() or info.st_mode & 0o077:
                raise RuntimeError('fixture directory must be private and owned by the caller')
            provenance = json.loads((cls.fixture / 'provenance.json').read_text())
            producers = provenance.get('producers', [])
            if (provenance.get('purpose') != 'disposable-test-only' or
                    provenance.get('source_revision') != cls.revision or not producers or
                    any(not item.get('command') or item.get('exit_code') != 0 for item in producers)):
                raise RuntimeError('fixture requires selected-source production-producer provenance')
            for relative in ('data', 'secrets', 'trust/ca.crt', 'environment.json'):
                if not (cls.fixture / relative).exists():
                    raise RuntimeError('missing generated fixture input: ' + relative)
            cls.network = provenance.get('network', '')
            if not re.fullmatch(r'paxeer-x-fixture-[a-zA-Z0-9_.-]+', cls.network):
                raise RuntimeError('real fixture dependencies require an isolated task network')
            if docker('network', 'inspect', '--format', '{{.Internal}}', cls.network).stdout.strip() != 'true':
                raise RuntimeError('fixture dependency network must prohibit external egress')
            cls.environment = json.loads((cls.fixture / 'environment.json').read_text())
            if not isinstance(cls.environment, dict) or any(
                    not re.fullmatch(r'LAYERX_[A-Z0-9_]+', name) or
                    not isinstance(value, str) or '\n' in value or '\x00' in value
                    for name, value in cls.environment.items()):
                raise RuntimeError('invalid generated fixture environment')
        print('candidate revision=' + cls.revision + ' image=' + cls.image, flush=True)

    @classmethod
    def tearDownClass(cls):
        if hasattr(cls, 'scratch'):
            shutil.rmtree(cls.scratch)

    def setUp(self):
        self.containers = []
        self.volumes = []

    def tearDown(self):
        for container in reversed(self.containers):
            docker('rm', '-f', container)
        for volume in reversed(self.volumes):
            docker('volume', 'rm', volume)

    def require_fixture(self):
        self.assertIsNotNone(self.fixture,
            'Mandatory generated fixture unavailable: real genesis/publication, assembled Human policy, '
            'registry/authority/journal, independent TLS/event material and actual attestor/KMS dependencies. '
            'No placeholders or skipped startup assertions are accepted.')

    def volume(self, complete=False):
        name = 'paxeer-x-24-1-' + uuid.uuid4().hex
        docker('volume', 'create', name)
        self.volumes.append(name)
        if complete:
            self.require_fixture()
            docker('run', '--rm', '--pull=never', '--network=none', '--entrypoint', '/bin/cp',
                   '--mount', 'type=bind,src=' + str(self.fixture / 'data') + ',dst=/fixture,readonly',
                   '--mount', 'type=volume,src=' + name + ',dst=/data', self.image,
                   '-a', '/fixture/.', '/data/')
        return name

    def start(self, volume, complete=False, setup=None):
        name = 'paxeer-x-24-1-' + uuid.uuid4().hex
        self.containers.append(name)
        args = ['run', '--detach', '--pull=never', '--name', name, '--network=' + (self.network if complete else 'none'),
                '--cap-add', 'SYS_ADMIN', '--security-opt', 'seccomp=unconfined',
                '--security-opt', 'apparmor=unconfined',
                '--mount', 'type=volume,src=' + volume + ',dst=/data']
        environment = {'LAYERX_NODE_NETWORK_ID': '7654321',
                       'LAYERX_KERNEL_PAXEER_RPC_NAMES':
                       'api1.mainnet-beta.paxeer.network api2.mainnet-beta.paxeer.network'}
        ca = self.scratch / 'ca.crt'
        if complete:
            self.require_fixture()
            ca = self.fixture / 'trust/ca.crt'
            environment.update(self.environment)
            args += ['--mount', 'type=bind,src=' + str(self.fixture / 'secrets') + ',dst=/run/secrets,readonly']
        args += ['--mount', 'type=bind,src=' + str(ca) + ',dst=/etc/layerx/trust/ca.crt,readonly']
        for key, value in environment.items():
            args += ['--env', key + '=' + value]
        if setup:
            args += ['--entrypoint', '/bin/bash', self.image, '-ec', setup + '\nexec /usr/local/bin/kernel-init']
        else:
            args.append(self.image)
        docker(*args)
        return name

    def inspect(self, container, script, *args, check=True):
        return docker('exec', container, 'python3', '-c', script, *args, check=check)

    def await_directories(self, container):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            result = self.inspect(container, 'import os; assert os.path.isdir("/run/layerx/human/service-private")', check=False)
            if result.returncode == 0:
                return
            if docker('inspect', '--format', '{{.State.Running}}', container).stdout.strip() != 'true':
                break
            time.sleep(0.2)
        self.fail('actual kernel initialization did not produce runtime directories')

    def directory_metadata(self, container):
        script = '''import json,os,stat,sys
out={}
for path in sys.argv[1:]:
 s=os.lstat(path);assert stat.S_ISDIR(s.st_mode)
 out[path]=[s.st_uid,s.st_gid,stat.S_IMODE(s.st_mode)]
print(json.dumps(out))'''
        return json.loads(self.inspect(container, script, *[p for p, _ in PRIVATE.values()]).stdout)

    def live_roles(self, container):
        script = '''import json,os,pathlib,sys
roles=json.loads(sys.argv[1]);out={}
for name,(role,uid,binary) in roles.items():
 status=pathlib.Path('/run/layerx/init')/name
 fields=status.read_text().split() if status.exists() else []
 if len(fields)!=3 or fields[:2]!=[str(uid),'running']:continue
 root=int(fields[2]); processes={}
 for path in pathlib.Path('/proc').iterdir():
  if not path.name.isdecimal():continue
  try:
   fields=(path/'stat').read_text().rsplit(')',1)[1].split()
   processes[int(path.name)]=int(fields[1])
  except (OSError,ValueError):continue
 for pid,parent in processes.items():
  current=pid;seen=set()
  while current!=root and current not in seen and current in processes:
   seen.add(current);current=processes[current]
  if current!=root:continue
  proc=pathlib.Path('/proc')/str(pid)
  try:
   if os.readlink(proc/'exe').rsplit('/',1)[-1]!=binary:continue
   status=(proc/'status').read_text().splitlines()
   ids={row.split(':')[0]:row.split()[1:] for row in status if row.startswith(('Uid:','Gid:'))}
   if ids['Uid']!=[str(uid)]*4 or ids['Gid']!=['4020']*4:continue
   out.setdefault(name,[]).append({'pid':pid,'mount':os.readlink(proc/'ns/mnt')})
  except OSError:continue
print(json.dumps(out))'''
        return json.loads(self.inspect(container, script, json.dumps(ROLES)).stdout)

    def await_roles(self, container):
        deadline = time.monotonic() + 45
        observed = {}
        while time.monotonic() < deadline:
            observed = self.live_roles(container)
            if set(observed) == set(ROLES) and all(len(pids) == 1 for pids in observed.values()):
                namespaces = [observed[name][0]['mount'] for name in ROLES
                              if name not in ('human', 'human-tls')]
                self.assertEqual(len(set(namespaces)), len(namespaces), 'material namespaces must be distinct')
                return observed
            time.sleep(0.5)
        self.fail('actual configured role process roster incomplete: ' + ','.join(sorted(set(ROLES) - set(observed))))

    def test_01_root_created_private_directories(self):
        container = self.start(self.volume())
        self.await_directories(container)
        self.assertEqual(self.directory_metadata(container),
                         {path: [uid, 4020, 0o700] for path, uid in PRIVATE.values()})
        metadata = json.loads(self.inspect(container,
            'import json,os,stat;print(json.dumps([stat.S_IMODE(os.stat(p).st_mode) for p in '
            '["/run/human-private","/run/layerx","/run/layerx/human"]]))').stdout)
        self.assertTrue(all(mode & 0o002 == 0 for mode in metadata))
        self.assertTrue(metadata[1] & 0o1000, 'shared runtime parent requires sticky rename protection')

    def refused(self, setup):
        volume = self.volume()
        prefix = 'mkdir -p /data/guard; printf untouched > /data/guard/sentinel\n'
        container = self.start(volume, setup=prefix + setup)
        code = docker('wait', container, timeout=20).stdout.strip()
        self.assertNotEqual(code, '0')
        result = docker('logs', container)
        logs = result.stdout + result.stderr
        self.assertIn('private runtime', logs)
        sentinel = docker('run', '--rm', '--pull=never', '--network=none', '--entrypoint', '/bin/cat',
                          '--mount', 'type=volume,src=' + volume + ',dst=/data,readonly',
                          self.image, '/data/guard/sentinel').stdout
        self.assertEqual(sentinel, 'untouched')

    def test_02_refuse_symlink_child(self):
        self.refused('mkdir -p /run/human-private; mount -t tmpfs -o mode=0755 tmpfs /run/human-private; '
                     'ln -s /data/guard /run/human-private/components')

    def test_03_refuse_symlink_parent(self):
        self.refused('ln -s /data/guard /run/human-private')

    def test_04_refuse_unexpected_owner(self):
        self.refused('mkdir -p /run/human-private; mount -t tmpfs -o mode=0755 tmpfs /run/human-private; '
                     'mkdir -m0700 /run/human-private/components; chown 4021:4020 /run/human-private/components')

    def test_05_refuse_non_directory(self):
        self.refused('mkdir -p /run/human-private; mount -t tmpfs -o mode=0755 tmpfs /run/human-private; '
                     'touch /run/human-private/components')

    def test_06_refuse_permission_broadening(self):
        self.refused('mkdir -p /run/human-private; mount -t tmpfs -o mode=0755 tmpfs /run/human-private; '
                     'mkdir -m0755 /run/human-private/components; chown 4020:4020 /run/human-private/components')

    def test_07_real_roles_and_cross_uid_boundary(self):
        self.require_fixture()
        container = self.start(self.volume(complete=True), complete=True)
        observed = self.await_roles(container)
        probe = '''import os,sys
path=sys.argv[1]
for operation in (lambda:os.listdir(path),lambda:os.open(path+'/denied',os.O_CREAT|os.O_WRONLY,0o600),lambda:os.chdir(path)):
 try:operation()
 except PermissionError:continue
 raise SystemExit('cross-UID private access unexpectedly allowed')
'''
        for name, processes in observed.items():
            role, uid, _ = ROLES[name]
            path = PRIVATE[role][0]
            other_uid = 4021 if uid == 4020 else 4020
            docker('exec', container, 'nsenter', '--mount=/proc/' + str(processes[0]['pid']) + '/ns/mnt',
                   '--', 'setpriv', '--reuid=' + str(other_uid), '--regid=4020', '--clear-groups',
                   '--no-new-privs', 'python3', '-c', probe, path)
        self.assertEqual(self.directory_metadata(container),
                         {path: [uid, 4020, 0o700] for path, uid in PRIVATE.values()})

    def snapshot(self, volume):
        script = '''import hashlib,json,pathlib,stat
out={}
for root in ('/data/layerx/keys','/data/layerx/genesis','/data/human-state'):
 for p in pathlib.Path(root).rglob('*'):
  if p.is_symlink():raise SystemExit('durable fixture state contains symlink')
  s=p.stat()
  if stat.S_ISREG(s.st_mode):out[str(p)]=hashlib.sha256(p.read_bytes()).hexdigest()
print(json.dumps(out,sort_keys=True))'''
        result = docker('run', '--rm', '--pull=never', '--network=none', '--entrypoint', '/usr/bin/python3',
                        '--mount', 'type=volume,src=' + volume + ',dst=/data,readonly',
                        self.image, '-c', script)
        return json.loads(result.stdout)

    def test_08_restart_preserves_real_durable_state(self):
        self.require_fixture()
        volume = self.volume(complete=True)
        first = self.start(volume, complete=True)
        self.await_roles(first)
        docker('stop', '--time', '10', first)
        before = self.snapshot(volume)
        for child in ('components', 'identity', 'security', 'movement', 'agent'):
            self.assertTrue(any(path.startswith('/data/human-state/' + child + '/') for path in before),
                            'real role-created durable state missing: ' + child)
        second = self.start(volume, complete=True)
        self.await_roles(second)
        self.assertEqual(self.directory_metadata(second),
                         {path: [uid, 4020, 0o700] for path, uid in PRIVATE.values()})
        docker('stop', '--time', '10', second)
        self.assertEqual(before, self.snapshot(volume), 'restart changed durable fixture state or material')

    def test_09_compatible_directory_is_idempotent(self):
        setup = ('mkdir -p /run/human-private; mount -t tmpfs -o mode=0755 tmpfs /run/human-private; '
                 'mkdir -m0700 /run/human-private/components; chown 4020:4020 /run/human-private/components; '
                 'printf compatible > /run/human-private/components/sentinel; '
                 'chown 4020:4020 /run/human-private/components/sentinel')
        container = self.start(self.volume(), setup=setup)
        self.await_directories(container)
        self.assertEqual(self.directory_metadata(container),
                         {path: [uid, 4020, 0o700] for path, uid in PRIVATE.values()})
        self.assertEqual(docker('exec', container, 'cat',
                               '/run/human-private/components/sentinel').stdout, 'compatible')

    def test_10_refuse_service_parent_symlink(self):
        self.refused('mkdir -p /run/layerx; mount -t tmpfs -o mode=0755 tmpfs /run/layerx; '
                     'ln -s /data/guard /run/layerx/human')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=['role-directories'])
    parser.parse_args()
    os.umask(0o077)
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(RoleDirectories)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print('PAXEER_X_GATE tests=' + str(result.testsRun) + ' skipped=' + str(len(result.skipped)), flush=True)
    return 0 if result.wasSuccessful() and result.testsRun > 0 and not result.skipped else 1


if __name__ == '__main__':
    raise SystemExit(main())
