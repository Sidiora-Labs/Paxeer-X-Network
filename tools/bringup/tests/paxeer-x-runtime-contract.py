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
PRIVATE = {role: ('/run/human-private/' + role, uid)
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
            result = self.inspect(container, 'import os; assert os.path.isdir("/run/human-private/service")', check=False)
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
            'import json,os,stat;s=os.stat("/run/human-private");'
            'print(json.dumps([s.st_uid,s.st_gid,stat.S_IMODE(s.st_mode)]))').stdout)
        self.assertEqual(metadata, [0, 0, 0o755])

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

    def test_10_refuse_service_directory_symlink(self):
        self.refused('mkdir -p /run/human-private; mount -t tmpfs -o mode=0755 tmpfs /run/human-private; '
                     'ln -s /data/guard /run/human-private/service')


class DirectoryPrerequisite(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.geteuid() != 0:
            raise RuntimeError('directory prerequisite requires real root namespace privileges')
        for executable in ('unshare', 'mount', 'chroot', 'setpriv', 'bash', 'python3'):
            if shutil.which(executable) is None:
                raise RuntimeError('missing namespace prerequisite: ' + executable)
        cls.revision = command(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']).stdout.strip()
        if command(['git', '-C', str(ROOT), 'status', '--porcelain']).stdout.strip():
            raise RuntimeError('source must be committed before namespace qualification')
        source = (ROOT / 'docker/kernel/init.sh').read_text()
        start_marker = '\nprivate_runtime_directories() {\n'
        end_marker = '\n}\n\nprivate_runtime_directories\n'
        if source.count(start_marker) != 1 or source.count(end_marker) != 1:
            raise RuntimeError('production initializer extraction boundary changed')
        start = source.index(start_marker) + 1
        end = source.index(end_marker, start) + len('\n}\n')
        initializer = source[start:end]
        memory = re.findall(r'^memory\(\) \{\n.*?^\}\n', source, re.M | re.S)
        log = re.findall(r'^log\(\) \{[^\n]*\}\n', source, re.M)
        if len(memory) != 1 or len(log) != 1:
            raise RuntimeError('production mount initializer extraction boundary changed')
        cls.production = log[0] + memory[0] + initializer
        cls.namespace = os.readlink('/proc/self/ns/mnt')
        cls.pid_namespace = os.readlink('/proc/self/ns/pid')
        print('prerequisite revision=' + cls.revision + ' initializer_sha256=' +
              hashlib.sha256(cls.production.encode()).hexdigest(), flush=True)

    def setUp(self):
        self.scratch = Path(tempfile.mkdtemp(prefix='paxeer-x-runtime-prerequisite-'))
        self.durable = self.scratch / 'durable'
        self.durable.mkdir(mode=0o700)
        for role, (_, uid) in PRIVATE.items():
            directory = self.durable / role
            directory.mkdir(mode=0o700)
            path = directory / 'state'
            path.write_bytes(('separately seeded durable bytes: ' + role).encode())
            path.chmod(0o600)
            os.chown(path, uid, 4020)
            os.chown(directory, uid, 4020)
        self.before = self.snapshot()
        self.sequence = 0

    def tearDown(self):
        try:
            self.assertEqual(self.before, self.snapshot(), 'initializer modified seeded durable bytes or metadata')
        finally:
            shutil.rmtree(self.scratch)

    def snapshot(self):
        result = {}
        for path in sorted(self.durable.rglob('*')):
            info = path.lstat()
            self.assertFalse(path.is_symlink(), 'durable fixture acquired a symlink')
            result[str(path.relative_to(self.durable))] = [
                info.st_uid, info.st_gid, info.st_mode & 0o7777,
                hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None]
        return result

    def namespace_case(self, body):
        import shlex
        self.sequence += 1
        sandbox = self.scratch / ('root-' + str(self.sequence))
        sandbox.mkdir(mode=0o700)
        scaffold = '''set -euo pipefail
root=ROOT_PATH
durable=DURABLE_PATH
[ "$(readlink /proc/self/ns/mnt)" != ORIGINAL_MOUNT ]
[ "$(readlink /proc/self/ns/pid)" != ORIGINAL_PID ]
mount --make-rprivate /
mount -t tmpfs -o mode=0755,nosuid tmpfs "$root"
for directory in usr bin sbin lib lib64; do
    [ ! -d "/$directory" ] || {
        mkdir -p "$root/$directory"
        mount --bind "/$directory" "$root/$directory"
        mount -o remount,bind,ro "$root/$directory"
    }
done
mkdir -m0755 "$root/proc" "$root/dev" "$root/run" "$root/data" "$root/etc"
mkdir -m1777 "$root/tmp"
touch "$root/dev/null"
mount --bind /dev/null "$root/dev/null"
mount -t proc -o nosuid,nodev,noexec proc "$root/proc"
mount --bind "$durable" "$root/data"
exec chroot "$root" /bin/bash -se <<'PAXEER_X_NAMESPACE_CASE'
set -euo pipefail
umask 077
'''
        for name, value in (('ROOT_PATH', str(sandbox)), ('DURABLE_PATH', str(self.durable)),
                            ('ORIGINAL_MOUNT', self.namespace), ('ORIGINAL_PID', self.pid_namespace)):
            scaffold = scaffold.replace(name, shlex.quote(value))
        script = scaffold + self.production + '\n' + body + '\nPAXEER_X_NAMESPACE_CASE\n'
        argv = ['unshare', '--mount', '--pid', '--fork', '--kill-child=KILL', '--net',
                '--ipc', '--uts', '--propagation', 'private', '--mount-proc', '/bin/bash', '-se']
        try:
            result = subprocess.run(argv, input=script, capture_output=True, text=True, timeout=40,
                                    env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C'})
        except subprocess.TimeoutExpired:
            self.fail('disposable namespace exceeded40 seconds')
        self.assertEqual(os.readlink('/proc/self/ns/mnt'), self.namespace)
        self.assertEqual(os.readlink('/proc/self/ns/pid'), self.pid_namespace)
        self.assertEqual(self.before, self.snapshot())
        return result

    def initialized(self, following=''):
        result = self.namespace_case('memory /run/human-private 0755\nprivate_runtime_directories\n' + following)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def refused(self, setup):
        import shlex
        snapshot = "import os,json;from pathlib import Path;r=Path('/run/human-private');ps=[r,*r.rglob('*')];print(json.dumps({str(p):[p.lstat().st_uid,p.lstat().st_gid,p.lstat().st_mode,os.readlink(p) if p.is_symlink() else None] for p in ps},sort_keys=True))"
        capture = 'python3 -c ' + shlex.quote(snapshot)
        body = '''memory /run/human-private 0755
SETUP
CAPTURE > /tmp/before.json
set +e
private_runtime_directories
code=$?
set -e
[ "$code" -ne 0 ]
CAPTURE > /tmp/after.json
cmp /tmp/before.json /tmp/after.json
printf 'initializer refused exit=%s\\n' "$code"
'''.replace('SETUP', setup).replace('CAPTURE', capture)
        result = self.namespace_case(body)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('private runtime directory refused:', result.stderr)
        self.assertIn('initializer refused exit=', result.stdout)

    def test_01_root_initialization_and_declared_owners(self):
        self.initialized('''python3 - <<'PY_CHECK'
import os,stat
assert os.geteuid()==0
parent=os.lstat('/run/human-private')
assert (parent.st_uid,parent.st_gid,stat.S_IMODE(parent.st_mode))==(0,0,0o755)
expected={'components':4020,'identity':4020,'security':4020,'movement':4020,'agent':4021,'kms':4026,'service':4020}
assert set(os.listdir('/run/human-private'))==set(expected)
for role,uid in expected.items():
 info=os.lstat('/run/human-private/'+role)
 assert stat.S_ISDIR(info.st_mode)
 assert (info.st_uid,info.st_gid,stat.S_IMODE(info.st_mode))==(uid,4020,0o700)
PY_CHECK
''')

    def test_02_real_owner_uid_access(self):
        self.initialized('''python3 - <<'PY_CHECK'
import subprocess
roles={'components':4020,'identity':4020,'security':4020,'movement':4020,'agent':4021,'kms':4026,'service':4020}
probe="import os,sys; from pathlib import Path; assert os.geteuid()==int(sys.argv[2]); p=Path(sys.argv[1]); os.chdir(p); f=p/'owner'; f.write_bytes(b'owned'); assert f.read_bytes()==b'owned'; f.rename(p/'renamed'); (p/'renamed').unlink(); (p/'child').mkdir(); (p/'child').rmdir()"
for role,uid in roles.items():
 subprocess.run(['setpriv','--reuid='+str(uid),'--regid=4020','--clear-groups','--no-new-privs','python3','-c',probe,'/run/human-private/'+role,str(uid)],check=True)
PY_CHECK
''')

    def test_03_cross_uid_traversal_and_modification_refused(self):
        self.initialized('''python3 - <<'PY_CHECK'
import os,subprocess
from pathlib import Path
roles={'components':4020,'identity':4020,'security':4020,'movement':4020,'agent':4021,'kms':4026,'service':4020}
probe="""import os,sys
from pathlib import Path
p=Path(sys.argv[1]); uid=int(sys.argv[2]); assert os.geteuid()==uid
operations=[lambda:os.chdir(p),lambda:list(p.iterdir()),lambda:(p/'owned').read_bytes(),lambda:(p/'owned').write_bytes(b'bad'),lambda:(p/'new').write_bytes(b'bad'),lambda:(p/'owned').unlink(),lambda:(p/'owned').rename(p/'moved'),lambda:p.rename(p.parent/('moved-'+p.name)),lambda:(p/'child').mkdir(),lambda:os.chmod(p,0o777)]
for operation in operations:
 try:operation()
 except PermissionError:continue
 raise SystemExit('cross-UID private operation unexpectedly allowed')
"""
for role,owner in roles.items():
 p=Path('/run/human-private')/role
 (p/'owned').write_bytes(b'unchanged');os.chown(p/'owned',owner,4020)
 for uid in sorted(set(roles.values())-{owner}):
  subprocess.run(['setpriv','--reuid='+str(uid),'--regid=4020','--clear-groups','--no-new-privs','python3','-c',probe,str(p),str(uid)],check=True)
 assert (p/'owned').read_bytes()==b'unchanged'
PY_CHECK
''')

    def test_04_symlink_child_refused(self):
        self.refused('ln -s /data/components /run/human-private/components')

    def test_05_symlink_parent_refused(self):
        result = self.namespace_case('ln -s /data /run/human-private\nset +e\nmemory /run/human-private 0755\ncode=$?\nset -e\n[ "$code" -ne 0 ]\n')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('private runtime mount refused:', result.stderr)

    def test_06_wrong_child_uid_refused(self):
        self.refused('mkdir -m0700 /run/human-private/components; chown 4021:4020 /run/human-private/components')

    def test_07_wrong_child_gid_refused(self):
        self.refused('mkdir -m0700 /run/human-private/components; chown 4020:4021 /run/human-private/components')

    def test_08_wrong_child_mode_refused(self):
        self.refused('mkdir -m0755 /run/human-private/components; chown 4020:4020 /run/human-private/components')

    def test_09_non_directory_refused(self):
        self.refused('touch /run/human-private/components')

    def test_10_unexpected_parent_owner_refused(self):
        self.refused('chown 4021:4020 /run/human-private')

    def test_11_unexpected_parent_mode_refused(self):
        self.refused('chmod 0777 /run/human-private')

    def test_12_compatible_initialization_is_idempotent(self):
        self.initialized('''python3 - <<'PY_CHECK'
import json,os,stat
from pathlib import Path
root=Path('/run/human-private')
for path in root.iterdir():(path/'ephemeral').write_bytes(b'preserve-compatible')
snapshot={p.name:[p.stat().st_ino,p.stat().st_uid,p.stat().st_gid,stat.S_IMODE(p.stat().st_mode)] for p in root.iterdir()}
Path('/tmp/before.json').write_text(json.dumps(snapshot))
PY_CHECK
private_runtime_directories
python3 - <<'PY_CHECK'
import json,stat
from pathlib import Path
root=Path('/run/human-private')
after={p.name:[p.stat().st_ino,p.stat().st_uid,p.stat().st_gid,stat.S_IMODE(p.stat().st_mode)] for p in root.iterdir()}
assert after==json.loads(Path('/tmp/before.json').read_text())
assert all((p/'ephemeral').read_bytes()==b'preserve-compatible' for p in root.iterdir())
PY_CHECK
''')

    def test_13_ephemeral_recreation_preserves_seeded_durable_bytes(self):
        first = self.initialized('''python3 - <<'PY_CHECK'
from pathlib import Path
for path in Path('/run/human-private').iterdir():(path/'ephemeral').write_bytes(b'first namespace only')
PY_CHECK
''')
        self.assertEqual(first.returncode, 0)
        second = self.namespace_case('''[ ! -e /run/human-private ]
memory /run/human-private 0755
private_runtime_directories
python3 - <<'PY_CHECK'
import os,stat
from pathlib import Path
roles={'components':4020,'identity':4020,'security':4020,'movement':4020,'agent':4021,'kms':4026,'service':4020}
for role,uid in roles.items():
 p=Path('/run/human-private')/role;s=p.stat()
 assert not (p/'ephemeral').exists()
 assert (s.st_uid,s.st_gid,stat.S_IMODE(s.st_mode))==(uid,4020,0o700)
 assert (Path('/data')/role/'state').read_bytes()==('separately seeded durable bytes: '+role).encode()
PY_CHECK
''')
        self.assertEqual(second.returncode, 0, second.stdout + second.stderr)

    def test_14_unprivileged_initialization_refused(self):
        import shlex
        body = 'memory /run/human-private 0755\nset +e\nsetpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs /bin/bash -c ' + shlex.quote(self.production + '\nprivate_runtime_directories') + '\ncode=$?\nset -e\n[ "$code" -ne 0 ]\n'
        result = self.namespace_case(body)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('root initialization required', result.stderr)


class ExportRecovery(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.geteuid() != 0:
            raise RuntimeError('export recovery requires root and private Linux namespaces')
        for executable in ('unshare', 'mount', 'chroot', 'bash', 'python3', 'tar', 'sha256sum'):
            if shutil.which(executable) is None:
                raise RuntimeError('missing export recovery prerequisite: ' + executable)
        supplied = os.environ.get('LAYERX_TEST_RUNTIME_CLOCK_BIN', '')
        if not supplied:
            raise RuntimeError('LAYERX_TEST_RUNTIME_CLOCK_BIN must name the actual built runtime-clock service')
        cls.clock = Path(supplied).resolve(strict=True)
        with cls.clock.open('rb') as source:
            if source.read(4) != b'\x7fELF':
                raise RuntimeError('runtime-clock prerequisite is not an actual ELF service executable')
        if not os.access(cls.clock, os.X_OK):
            raise RuntimeError('runtime-clock prerequisite is not executable')
        cls.namespace = os.readlink('/proc/self/ns/mnt')
        cls.pid_namespace = os.readlink('/proc/self/ns/pid')
        print('export recovery clock_sha256=' + hashlib.sha256(cls.clock.read_bytes()).hexdigest(), flush=True)

    def namespace_case(self, scenario):
        import shlex
        with tempfile.TemporaryDirectory(prefix='paxeer-x-export-recovery-') as temporary:
            scratch = Path(temporary)
            os.chmod(scratch, 0o700)
            root = scratch / 'root'
            root.mkdir()
            fixture = scratch / 'fixture'
            fixture.mkdir(mode=0o700)
            shutil.copy2(self.clock, fixture / 'layerx-runtime-clock')
            (fixture / 'case.py').write_text(self.case_source)
            script = '''set -euo pipefail
root=ROOT_PATH
fixture=FIXTURE_PATH
[ "$(readlink /proc/self/ns/mnt)" != ORIGINAL_MOUNT ]
[ "$(readlink /proc/self/ns/pid)" != ORIGINAL_PID ]
mount --make-rprivate /
mount -t tmpfs -o mode=0755,nosuid tmpfs "$root"
for directory in usr bin sbin lib lib64; do
    [ ! -d "/$directory" ] || {
        mkdir -p "$root/$directory"
        mount --bind "/$directory" "$root/$directory"
        mount -o remount,bind,ro "$root/$directory"
    }
done
mount -t tmpfs -o mode=0755,nosuid tmpfs "$root/usr/local"
mkdir -p "$root/usr/local/bin" "$root/proc" "$root/dev" "$root/run" "$root/data" "$root/etc" "$root/var/lib/layerx/human" "$root/source" "$root/fixture"
mkdir -m1777 "$root/tmp"
cp "$fixture/layerx-runtime-clock" "$root/usr/local/bin/layerx-runtime-clock"
for device in null zero urandom full; do
    touch "$root/dev/$device"
    mount --bind "/dev/$device" "$root/dev/$device"
done
mount -t proc -o nosuid,nodev,noexec proc "$root/proc"
mount --bind "$fixture" "$root/fixture"
mount -o remount,bind,ro "$root/fixture"
mount --bind SOURCE_PATH "$root/source"
mount -o remount,bind,ro "$root/source"
exec chroot "$root" /usr/bin/python3 /fixture/case.py SCENARIO
'''
            for name, value in (('ROOT_PATH', str(root)), ('FIXTURE_PATH', str(fixture)),
                                ('SOURCE_PATH', str(ROOT)), ('ORIGINAL_MOUNT', self.namespace),
                                ('ORIGINAL_PID', self.pid_namespace), ('SCENARIO', scenario)):
                script = script.replace(name, shlex.quote(value))
            result = subprocess.run(
                ['unshare', '--mount', '--pid', '--fork', '--kill-child=KILL', '--net',
                 '--ipc', '--uts', '--propagation', 'private', '--mount-proc', '/bin/bash', '-se'],
                input=script, capture_output=True, text=True, timeout=90,
                env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C'})
            self.assertEqual(os.readlink('/proc/self/ns/mnt'), self.namespace)
            self.assertEqual(os.readlink('/proc/self/ns/pid'), self.pid_namespace)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('export-recovery passed ' + scenario, result.stdout)

    def test_01_success_stays_quiesced_and_resume_preserves_prior_stop(self):
        self.namespace_case('success')

    def test_02_empty_manifest_refuses_with_source_intact(self):
        self.namespace_case('empty')

    def test_03_failed_archive_stream_refuses_with_source_intact(self):
        self.namespace_case('stream')

    def test_04_archive_manifest_mismatch_refuses_with_source_intact(self):
        self.namespace_case('mismatch')

    def test_05_interruption_records_explicit_idempotent_recovery(self):
        self.namespace_case('interruption')

    def test_06_pid_reuse_and_other_identity_mismatches_refuse_all_resumption(self):
        self.namespace_case('identity')

    def test_07_exited_recorded_process_does_not_resume_another_process(self):
        self.namespace_case('exited')

    case_source = r'''
import errno
import hashlib
import json
import os
from pathlib import Path
import resource
import select
import signal
import stat
import subprocess
import sys
import time

os.umask(0o077)
scenario = sys.argv[1]
source = Path('/var/lib/layerx/human')
destination = Path('/tmp/export')
exporter = ['/bin/bash', '/source/tools/bringup/human-state-preserve.sh']
processes = []
export_process = None


def wait_for(predicate, description, seconds=15):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(0.002)
    raise AssertionError('deadline waiting for ' + description)


def process_state(pid):
    return Path('/proc/' + str(pid) + '/stat').read_text().rsplit(')', 1)[1].split()[0]


def stopped(pid):
    return process_state(pid) in ('T', 't')


def snapshot():
    return {str(path.relative_to(source)): [path.lstat().st_uid, path.lstat().st_gid,
            stat.S_IMODE(path.lstat().st_mode), path.lstat().st_ino,
            hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None]
            for path in sorted(source.rglob('*'))}


def invoke(operation, expected=0):
    result = subprocess.run(exporter + [operation, str(destination)], capture_output=True,
                            text=True, timeout=20)
    if expected == 0:
        assert result.returncode == 0, result.stdout + result.stderr
    else:
        assert result.returncode != 0, result.stdout + result.stderr
        assert 'pass export' not in result.stdout, result.stdout
    return result


def record():
    try:
        return json.loads((destination / 'quiescence.json').read_text())
    except (FileNotFoundError, json.JSONDecodeError):
        return None


def save_record(value):
    temporary = destination / 'record-replacement'
    with temporary.open('w') as stream:
        json.dump(value, stream)
        stream.flush()
        os.fsync(stream.fileno())
    temporary.replace(destination / 'quiescence.json')


def start_clock(index):
    directory = Path('/tmp/clock-' + str(index))
    directory.mkdir(mode=0o700)
    child = subprocess.Popen(['/usr/local/bin/layerx-runtime-clock', '--runtime-dir', str(directory),
                              '--', '/usr/bin/python3', '-c', 'import sys; sys.stdin.buffer.read()'],
                             stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    processes.append(child)
    wait_for(lambda: list(directory.glob('lxc-*/clock.sock')), 'actual runtime-clock socket')
    assert child.poll() is None, child.stderr.read().decode()
    assert os.readlink('/proc/' + str(child.pid) + '/exe') == '/usr/local/bin/layerx-runtime-clock'
    return child


def resume_and_check(active, prior):
    invoke('local-resume')
    wait_for(lambda: not stopped(active.pid), 'previously running service resumed')
    assert stopped(prior.pid), 'resume changed a service which was already stopped'
    invoke('local-resume')
    assert not stopped(active.pid), 'repeated resume stopped a service'
    assert stopped(prior.pid), 'repeated resume changed the prior stopped service'


try:
    if scenario != 'empty':
        (source / 'a-state').write_bytes(b'actual disposable durable state\n')
        (source / 'a-state').chmod(0o600)
    if scenario in ('stream', 'mismatch', 'interruption'):
        with (source / 'b-large-state').open('wb') as stream:
            for _ in range(64):
                stream.write(b'preserved-state!' * 65536)
        (source / 'z-state').write_bytes(b'before concurrent owner write\n')
    active = start_clock(1)
    prior = start_clock(2)
    os.kill(prior.pid, signal.SIGSTOP)
    wait_for(lambda: stopped(prior.pid), 'prior stopped service')
    baseline = snapshot()
    final_expected = baseline

    if scenario == 'stream':
        def file_limit():
            resource.setrlimit(resource.RLIMIT_FSIZE, (256 * 1024, 256 * 1024))
        result = subprocess.run(exporter + ['local-export', str(destination)], capture_output=True,
                                text=True, timeout=30, preexec_fn=file_limit)
        assert result.returncode != 0, result.stdout + result.stderr
        assert 'pass export' not in result.stdout
        assert 'local-resume' in result.stderr, result.stderr
        assert snapshot() == baseline, 'failed archive stream changed source state'
        assert record() is not None, 'failed archive stream lost recovery record'
        assert stopped(active.pid) and stopped(prior.pid)
        resume_and_check(active, prior)
    elif scenario in ('mismatch', 'interruption'):
        export_process = subprocess.Popen(exporter + ['local-export', str(destination)],
                                          stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                          text=True, start_new_session=True,
                                          env={**os.environ, 'TAR_OPTIONS': '--sort=name'})
        wait_for(record, 'durable quiescence record')
        archive = destination / 'state.tar'
        os.mkfifo(archive, 0o600)
        wait_for(lambda: (destination / 'manifest.sha256').exists() and
                 (destination / 'manifest.sha256').stat().st_size > 0 and stopped(active.pid),
                 'manifest and stopped service')
        if scenario == 'interruption':
            os.killpg(export_process.pid, signal.SIGTERM)
            stdout, stderr = export_process.communicate(timeout=20)
            assert export_process.returncode != 0, stdout + stderr
            assert 'pass export' not in stdout
            assert 'local-resume' in stderr, stderr
            assert snapshot() == baseline, 'interrupted export changed source state'
            assert record() is not None, 'interrupted export lost recovery record'
            assert stopped(active.pid) and stopped(prior.pid)
            resume_and_check(active, prior)
        else:
            descriptor = os.open(archive, os.O_RDONLY | os.O_NONBLOCK)
            try:
                def first_archive_bytes():
                    try:
                        return os.read(descriptor, 65536)
                    except BlockingIOError:
                        return None
                first = wait_for(first_archive_bytes, 'real tar stream entering FIFO')
                os.kill(export_process.pid, signal.SIGSTOP)
                wait_for(lambda: stopped(export_process.pid), 'export supervisor paused')
                replacement = b'after concurrent owner write\n'
                (source / 'z-state').write_bytes(replacement)
                expected = snapshot()
                final_expected = expected
                archive.unlink()
                with archive.open('wb', buffering=0) as output:
                    output.write(first)
                    deadline = time.monotonic() + 25
                    while True:
                        assert time.monotonic() < deadline, 'archive FIFO did not finish'
                        ready, _, _ = select.select([descriptor], [], [], 0.2)
                        if not ready:
                            continue
                        data = os.read(descriptor, 65536)
                        if not data:
                            break
                        output.write(data)
                    os.fsync(output.fileno())
                os.kill(export_process.pid, signal.SIGCONT)
                stdout, stderr = export_process.communicate(timeout=20)
                assert export_process.returncode != 0, stdout + stderr
                assert 'pass export' not in stdout
                assert 'local-resume' in stderr, stderr
                assert ('mismatch' in stderr.lower() or 'manifest' in stderr.lower()), stderr
                assert snapshot() == expected, 'archive mismatch changed the owner-written source'
                assert stopped(active.pid) and stopped(prior.pid)
                resume_and_check(active, prior)
            finally:
                os.close(descriptor)
    else:
        result = invoke('local-export', expected=1 if scenario == 'empty' else 0)
        assert snapshot() == baseline, 'export changed source bytes or metadata'
        value = record()
        assert value is not None, 'missing durable recovery record'
        assert stat.S_IMODE((destination / 'quiescence.json').stat().st_mode) == 0o600
        assert stat.S_IMODE(destination.stat().st_mode) == 0o700
        entries = value['processes']
        assert {entry['pid'] for entry in entries} == {active.pid, prior.pid}, entries
        required = {'pid', 'starttime', 'exe', 'device', 'inode', 'uid', 'boot_id',
                    'pid_namespace', 'prior_state', 'resume_required', 'state'}
        for entry in entries:
            assert required.issubset(entry), entry
            assert entry['exe'] == '/usr/local/bin/layerx-runtime-clock'
            assert entry['starttime'] and entry['inode'] and entry['boot_id'] and entry['pid_namespace']
            assert entry['resume_required'] == (entry['pid'] == active.pid)
            assert (entry['prior_state'] in ('T', 't')) == (entry['pid'] == prior.pid)
        assert stopped(active.pid) and stopped(prior.pid), 'export failed to preserve quiescence'
        if scenario == 'empty':
            assert 'local-resume' in result.stderr, result.stderr
            assert 'empty' in (result.stdout + result.stderr).lower()
            resume_and_check(active, prior)
        elif scenario == 'identity':
            for key in ('starttime', 'exe', 'device', 'inode', 'uid', 'boot_id', 'pid_namespace'):
                changed = json.loads(json.dumps(value))
                entry = next(item for item in changed['processes'] if item['pid'] == active.pid)
                original = entry[key]
                entry[key] = original + 1 if isinstance(original, int) else str(original) + '-wrong'
                save_record(changed)
                invoke('local-resume', expected=1)
                assert stopped(active.pid) and stopped(prior.pid), 'identity refusal partially resumed services'
                assert snapshot() == baseline, 'identity refusal changed source state'
                save_record(value)
            resume_and_check(active, prior)
        elif scenario == 'exited':
            os.kill(active.pid, signal.SIGKILL)
            active.wait(timeout=5)
            another = start_clock(3)
            os.kill(another.pid, signal.SIGSTOP)
            wait_for(lambda: stopped(another.pid), 'unrecorded stopped service')
            invoke('local-resume', expected=1)
            assert stopped(another.pid) and stopped(prior.pid), 'resume signalled an unrecorded service'
            assert snapshot() == baseline, 'exited identity refusal changed source state'
        else:
            assert value['state'] == 'exported', value
            assert (destination / 'manifest.sha256').stat().st_size > 0
            assert (destination / 'state.tar').stat().st_size > 0
            extracted = Path('/tmp/extracted')
            extracted.mkdir(mode=0o700)
            subprocess.run(['tar', '-C', str(extracted), '-xf', str(destination / 'state.tar')], check=True)
            subprocess.run(['sha256sum', '--quiet', '-c', str(destination / 'manifest.sha256')],
                           cwd=extracted, check=True)
            resume_and_check(active, prior)
    assert snapshot() == final_expected, 'export or recovery changed source state'
    print('export-recovery passed ' + scenario, flush=True)
finally:
    if export_process is not None and export_process.poll() is None:
        try:
            os.killpg(export_process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        export_process.wait(timeout=5)
    for process in processes:
        if process.poll() is None:
            try:
                os.kill(process.pid, signal.SIGCONT)
                process.terminate()
                process.wait(timeout=7)
            except (ProcessLookupError, subprocess.TimeoutExpired):
                process.kill()
                process.wait(timeout=5)
        if process.stdin is not None:
            process.stdin.close()
        if process.stderr is not None:
            process.stderr.close()
'''


def kms_service_prerequisite():
    import importlib.util
    import shlex
    os.umask(0o077)
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location('human_material', ROOT / 'platform/hosted/human/material.py')
    material = importlib.util.module_from_spec(specification)
    sys.path.insert(0, str(ROOT / 'platform/hosted/human'))
    specification.loader.exec_module(material)
    container = None
    evidence = None
    result = {'task': '24.12', 'cases': [], 'tests': 0, 'skipped': 0, 'exit_code': 1}

    def require(condition, message):
        if not condition:
            raise RuntimeError(message)

    def case(name):
        result['cases'].append(name)
        print('PASS ' + name, flush=True)

    try:
        raw = os.environ.get('PAXEER_X_KMS_PREREQUISITE_EVIDENCE')
        if not raw:
            raise FileNotFoundError('PAXEER_X_KMS_PREREQUISITE_EVIDENCE owned0700 directory required')
        evidence = Path(raw)
        material.protected_file(evidence, 0o700)
        revision = command(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']).stdout.strip()
        require(not command(['git', '-C', str(ROOT), 'status', '--porcelain']).stdout.strip(), 'published clean candidate required')
        raw = os.environ.get('PAXEER_X_FOUNDATION_MANIFEST')
        if not raw:
            raise FileNotFoundError('actual passed24.11 PAXEER_X_FOUNDATION_MANIFEST required')
        manifest_path = Path(raw)
        material.protected_file(manifest_path, 0o600)
        foundation = material.protected_json(manifest_path)
        proof = material.protected_json(manifest_path.parent / 'qualification.json')
        require(foundation.get('stage') == 'dependency-foundation' and foundation.get('purpose') == 'disposable-test-only'
                and proof.get('exit_code') == 0 and proof.get('tests', 0) > 0 and proof.get('skipped') == 0,
                'genuine qualified dependency foundation required')
        network = foundation['network']
        require(re.fullmatch(r'paxeer-x-fixture-[a-zA-Z0-9_.-]+', network) is not None, 'isolated foundation network name refused')
        require(docker('network', 'inspect', '--format', '{{.Internal}}', network).stdout.strip() == 'true', 'external dependency network refused')
        registry_path = manifest_path.parent / foundation['inputs']['module_registry']
        expected = foundation['generated_artifacts'][foundation['inputs']['module_registry']]
        registry_bytes = material.read_bytes(registry_path)
        require(hashlib.sha256(registry_bytes).hexdigest() == expected['sha256'], 'foundation canonical registry changed')
        image = os.environ.get('PAXEER_X_RUNTIME_IMAGE', '')
        if not re.fullmatch(r'sha256:[0-9a-f]{64}', image):
            raise FileNotFoundError('prebuilt source-bound PAXEER_X_RUNTIME_IMAGE required')
        metadata = json.loads(docker('image', 'inspect', image).stdout)[0]
        image_revision = (metadata['Config'].get('Labels') or {}).get('org.opencontainers.image.revision', '')
        require(metadata['Id'] == image and re.fullmatch('[0-9a-f]{40}', image_revision) is not None, 'image source identity refused')
        paths = ['human/Cargo.toml', 'human/Cargo.lock', 'human/crates', 'agent', 'programs', 'interop',
                 'platform/hosted/internal', 'platform/crates', 'platform/Cargo.toml', 'platform/Cargo.lock']
        def binding(source):
            return command(['git', '-C', str(ROOT), 'ls-tree', '-r', '--full-tree', '-z', source, '--', *paths]).stdout
        require(binding(image_revision) == binding(revision), 'prebuilt Human KMS/runtime clock source differs')
        work = evidence / ('kms-' + uuid.uuid4().hex)
        work.mkdir(mode=0o700)
        tls = work / 'tls'
        tls.mkdir(mode=0o700)
        command(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(tls / 'ca.key')])
        command(['openssl', 'req', '-x509', '-new', '-key', str(tls / 'ca.key'), '-days', '1', '-subj', '/CN=Disposable Human KMS CA',
                 '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign', '-out', str(tls / 'ca.crt')])
        command(['openssl', 'x509', '-in', str(tls / 'ca.crt'), '-outform', 'DER', '-out', str(tls / 'ca.der')])
        producer = (ROOT / 'platform/hosted/tests/beta-cluster.sh').read_text().split('issue_cert() {', 1)[1].split('\n}\n', 1)[0]
        for name, common, usage, san in [('human-kms', 'layerx-human-kms', 'serverAuth', 'DNS:layerx-human-kms,DNS:localhost,IP:127.0.0.1'),
                                       ('human-kms-client', 'layerx-human-components', 'clientAuth', ''),
                                       ('human-kms-executor', 'layerx-human-movement', 'clientAuth', ''),
                                       ('foreign', 'foreign-component', 'clientAuth', '')]:
            code = 'CA_DIR=' + shlex.quote(str(tls)) + '\nissue_cert() {' + producer + '\n}\nissue_cert ' + ' '.join(map(shlex.quote, (name, common, usage, san)))
            command(['bash', '-euo', 'pipefail', '-c', code])
            os.chmod(tls / name, 0o700)
            shutil.copyfile(tls / 'ca.der', tls / name / 'ca.der')
            os.chmod(tls / name / 'ca.der', 0o600)
        state = work / 'state'
        state.mkdir(mode=0o700)
        os.chown(state, 4026, 4020)
        projected = work / 'material'
        material.kms_prerequisite(registry_path, tls, projected, foundation['network_id'], foundation['asset_id'], state)
        private = work / 'private'
        private.mkdir(mode=0o700)
        os.chown(private, 4026, 4020)
        os.chown(projected, 4026, 4020)
        for path in projected.iterdir():
            os.chown(path, 4026, 4020)
        container = 'paxeer-x-kms-' + uuid.uuid4().hex
        arguments = ['run', '-d', '--pull=never', '--name', container, '--network', network, '--read-only', '--cap-drop', 'ALL',
                     '--security-opt', 'no-new-privileges', '--tmpfs', '/tmp:rw,nosuid,nodev,mode=1777', '--user', '4026:4020',
                     '--mount', 'type=bind,src=' + str(projected) + ',dst=/run/human-material,readonly',
                     '--mount', 'type=bind,src=' + str(private) + ',dst=/run/human-private/kms',
                     '--mount', 'type=bind,src=' + str(state) + ',dst=/var/lib/layerx/human',
                     '--mount', 'type=bind,src=' + str(tls) + ',dst=/fixture-tls,readonly',
                     '--mount', 'type=bind,src=' + str(ROOT / 'docker/human-service/entrypoint.sh') + ',dst=/usr/local/bin/human-entrypoint,readonly']
        values = {'LISTEN': '127.0.0.1:9450', 'PROVIDER_REFERENCE': 'layerx-human-kms', 'STATE_DIR': '/var/lib/layerx/human',
                  'DEADLINE_SECONDS': '3', 'REGISTRY_FILE': '/run/human-private/kms/registry.json',
                  'CLIENT_CA_DER': '/run/human-private/kms/ca.der', 'TLS_CERT_DER': '/run/human-private/kms/kms-server.der',
                  'TLS_KEY_DER': '/run/human-private/kms/kms-server-key.der', 'CLIENT_CERT_DER': '/run/human-private/kms/kms-client.der',
                  'EVM_CLIENT_CERT_DER': '/run/human-private/kms/kms-executor.der', 'SEAL_SECRET_FILE': '/run/human-private/kms/kms-seal'}
        for name, value in values.items():
            arguments += ['-e', 'LAYERX_HUMAN_KMS_' + name + '=' + value]
        arguments += ['--entrypoint', '/bin/sh', image, '/usr/local/bin/human-entrypoint', 'kms']
        docker(*arguments)
        client = '''import socket,ssl,struct,sys
ctx=ssl.create_default_context(cafile='/fixture-tls/ca.crt')
if sys.argv[1]!='none': ctx.load_cert_chain('/fixture-tls/'+sys.argv[1]+'/cert.pem','/fixture-tls/'+sys.argv[1]+'/key.pem')
if sys.argv[2]=='wrongca':
 ctx=ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
 ctx.load_cert_chain('/fixture-tls/human-kms-client/cert.pem','/fixture-tls/human-kms-client/key.pem')
ctx.minimum_version=ssl.TLSVersion.TLSv1_3
payload=bytes.fromhex(sys.argv[3])
with socket.create_connection(('127.0.0.1',9450),timeout=3) as tcp:
 with ctx.wrap_socket(tcp,server_hostname='wrong-server' if sys.argv[2]=='wrongname' else 'layerx-human-kms') as tls:
  tls.sendall(struct.pack('>I',len(payload))+payload)
  def read(n):
   out=b''
   while len(out)<n:
    chunk=tls.recv(n-len(out))
    if not chunk: raise RuntimeError('real KMS refused connection')
    out+=chunk
   return out
  size=struct.unpack('>I',read(4))[0]
  if not 0<size<=2097152: raise RuntimeError('real response frame bounds')
  print(read(size).hex())
'''
        provider = b'layerx-human-kms'
        binding_value = os.urandom(32)
        def request(operation, reference=b'', provider_reference=provider):
            frame = b'LXKP\x00\x01' + bytes([operation]) + len(provider_reference).to_bytes(4, 'big') + provider_reference
            if operation:
                frame += binding_value + int(foundation['network_id']).to_bytes(4, 'big') + b'\x01' + len(reference).to_bytes(4, 'big') + reference
            return frame
        def call(frame, role='human-kms-client', failure='', check=True):
            answer = docker('exec', '--user', '0:0', container, 'python3', '-c', client, role, failure, frame.hex(), check=check)
            return bytes.fromhex(answer.stdout.strip()) if answer.returncode == 0 else None
        deadline = time.monotonic() + 30
        while True:
            if call(request(0), check=False) == b'LXKP\x00\x01\x00\x00':
                break
            require(time.monotonic() < deadline, 'actual KMS process startup failed')
            time.sleep(0.25)
        case('real-entrypoint-runtime-clock-and-mtls-startup')
        created = call(request(1))
        require(len(created) == 109 and created[:12] == b'LXKP\x00\x01\x01\x00\x00\x00\x00\x20', 'actual authorized create response refused')
        reference, public = created[12:44], created[44:76]
        require(any(reference) and any(public), 'actual KMS key identity absent')
        described = call(request(2, reference))
        require(described[7] == 0 and described[12:76] == created[12:76], 'actual provider describe identity mismatch')
        case('authorized-provider-create-and-describe')
        for label, role, failure in [('wrong-server-name', 'human-kms-client', 'wrongname'), ('wrong-ca', 'human-kms-client', 'wrongca'),
                                     ('missing-client', 'none', ''), ('wrong-client-role', 'foreign', '')]:
            require(call(request(0), role, failure, check=False) is None, label + ' unexpectedly admitted')
            case(label + '-refused')
        refused = call(request(1), 'human-kms-executor')
        require(refused == b'LXKP\x00\x01\x01\x01', 'restricted executor admitted service key creation')
        case('restricted-executor-service-operation-refused')
        require(call(request(0, provider_reference=b'foreign-provider')) == b'LXKP\x00\x01\x00\x01', 'wrong provider policy admitted')
        case('wrong-provider-policy-refused')
        seal = hashlib.sha256((projected / 'kms-seal').read_bytes()).hexdigest()
        docker('restart', container)
        deadline = time.monotonic() + 30
        while True:
            restored = call(request(2, reference), check=False)
            if restored is not None:
                break
            require(time.monotonic() < deadline, 'retained KMS failed to resume')
            time.sleep(0.25)
        require(restored[7] == 0 and restored[12:76] == created[12:76] and hashlib.sha256((projected / 'kms-seal').read_bytes()).hexdigest() == seal,
                'restart replaced retained KMS seal or custody identity')
        case('retained-state-seal-and-provider-identity-restart')
        result.update(revision=revision, image=image, foundation_manifest=str(manifest_path), tests=len(result['cases']), exit_code=0)
        material.write_bytes(evidence / 'kms-service-prerequisite.json', json.dumps(result, sort_keys=True).encode())
        print('PAXEER_X_GATE tests=' + str(result['tests']) + ' skipped=0', flush=True)
        return 0
    except FileNotFoundError as error:
        code = 78
        result['observed'] = str(error)
    except Exception as error:
        code = 1
        result['observed'] = str(error)
    finally:
        if container is not None:
            docker('rm', '-f', container, check=False)
    result.update(exit_code=code, tests=len(result['cases']))
    if evidence is not None:
        material.write_bytes(evidence / 'kms-service-prerequisite.json', json.dumps(result, sort_keys=True).encode())
    print('Human KMS prerequisite refused: ' + result['observed'], file=sys.stderr, flush=True)
    return code


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=['role-directories', 'role-directory-prerequisite', 'export-recovery', 'fixture-foundation', 'kms-service-prerequisite'])
    arguments = parser.parse_args()
    os.umask(0o077)
    if arguments.case == 'kms-service-prerequisite':
        return kms_service_prerequisite()
    if arguments.case == 'fixture-foundation':
        import importlib.util
        sys.dont_write_bytecode = True
        spec = importlib.util.spec_from_file_location('fixture_foundation', Path(__file__).with_name('paxeer-x-fixture-foundation.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module.main()
    if arguments.case == 'role-directories':
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(RoleDirectories)
    elif arguments.case == 'role-directory-prerequisite':
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(DirectoryPrerequisite)
    else:
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(ExportRecovery)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print('PAXEER_X_GATE tests=' + str(result.testsRun) + ' skipped=' + str(len(result.skipped)), flush=True)
    return 0 if result.wasSuccessful() and result.testsRun > 0 and not result.skipped else 1


if __name__ == '__main__':
    raise SystemExit(main())
