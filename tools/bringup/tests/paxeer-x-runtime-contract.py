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


class RoleDirectoryEvidenceRead(unittest.TestCase):
    STEPS = (
        'layerx=/data/layerx\n',
        'keys=$layerx/keys\n',
        'run=/run/layerx\n',
        'authority_material=/run/authority-private/material\n',
        'memory "$run" 0755\n',
        'memory /run/authority-private 0700\n',
        'chown 4020:4020 "$run"\nchmod 2775 "$run"\n',
        'install -d -o 0 -g 4020 -m 2775 "$layerx" "$layerx/settlement"\n',
        'install -d -o 0 -g 4020 -m 0750 "$keys" "$keys/tokens"\n',
        'if [ "$kernel_profile" = full ]; then\n    install -d -o 0 -g 0 -m 0700 "$keys/human-authority"\nfi\n',
        'fresh "$keys/tokens/replica-token" 4020:4020 0440 openssl rand -hex 32\n',
        'for token in backend-admin gateway-component gateway-authority webhooks-component webhooks-authority; do\n'
        '\tfresh "$keys/tokens/$token" 4020:4020 0440 openssl rand -hex 32\ndone\n'
        'if [ "$kernel_profile" = full ]; then\n'
        '    fresh "$keys/human-authority/authority-token" 0:0 0600 openssl rand -hex 32\n'
        '    fresh "$keys/human-authority/explorer-evidence-read" 0:0 0600 printf %s "$(openssl rand -hex 32)"\nfi\n',
    )
    REGISTRY = 'registry_bearer LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION registry-authority\n'
    FUNCTIONS = ('log', 'memory', 'fresh', 'registry_bearer', 'human_evidence_read_material')

    @classmethod
    def setUpClass(cls):
        if os.geteuid() != 0:
            raise RuntimeError('evidence-read role directories require real root namespace privileges')
        for executable in ('unshare', 'mount', 'chroot', 'setpriv', 'bash', 'python3', 'openssl', 'ip'):
            if shutil.which(executable) is None:
                raise RuntimeError('missing namespace prerequisite: ' + executable)
        cls.revision = command(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']).stdout.strip()
        if command(['git', '-C', str(ROOT), 'status', '--porcelain']).stdout.strip():
            raise RuntimeError('source must be committed before namespace qualification')
        binary = Path(os.environ.get('PAXEER_X_AUTHORITY_BIN', ''))
        if not binary.is_absolute() or not binary.is_file() or binary.is_symlink() or not os.access(binary, os.X_OK):
            raise RuntimeError('PAXEER_X_AUTHORITY_BIN must name the built layerx-receipt-authority executable')
        source = (ROOT / 'docker/kernel/init.sh').read_text()
        production = ''
        for name in cls.FUNCTIONS:
            found = re.findall(r'^' + name + r'\(\) \{(?:[^\n]*\}\n|\n.*?^\}\n)', source, re.M | re.S)
            if len(found) != 1:
                raise RuntimeError('production function extraction boundary changed: ' + name)
            production += found[0]
        for step in cls.STEPS + (cls.REGISTRY,):
            if source.count('\n' + step) != 1:
                raise RuntimeError('production step extraction boundary changed: ' + step.splitlines()[0])
        material = re.findall(r'^human_authority_material\(\) \{\n.*?^\}\n', source, re.M | re.S)
        if len(material) != 1 or '\thuman_evidence_read_material || return 1\n' not in material[0]:
            raise RuntimeError('human-authority-material no longer installs the evidence-read token')
        start = '\nif [ "$kernel_profile" = full ]; then\nservice receipt-authority 4021 \\\n'
        if source.count(start) != 1:
            raise RuntimeError('full receipt-authority service extraction boundary changed')
        begin = source.index(start)
        service = source[begin:source.index('\nfi\n', begin)]
        for declared in ('$authority_material/human-agent.token $authority_material/evidence-read.token ',
                         'LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE="$authority_material/evidence-read.token" \\\n',
                         'LAYERX_AUTHORITY_TOKEN_FILES="$keys/tokens/gateway-authority:$run/registry-authority/token:'
                         '$keys/tokens/webhooks-authority" \\\n'):
            if declared not in service:
                raise RuntimeError('receipt-authority runtime inputs do not declare: ' + declared.strip())
        cls.production = production + 'kernel_profile=full\n' + ''.join(cls.STEPS[:6]) + \
            'chown 4021:4020 /run/authority-private\n' + ''.join(cls.STEPS[6:])
        cls.namespace = os.readlink('/proc/self/ns/mnt')
        cls.pid_namespace = os.readlink('/proc/self/ns/pid')
        cls.fixture = Path(tempfile.mkdtemp(prefix='paxeer-x-evidence-read-fixture-'))
        cls.fixture.chmod(0o755)
        shutil.copyfile(binary, cls.fixture / 'layerx-receipt-authority')
        (cls.fixture / 'layerx-receipt-authority').chmod(0o755)
        tls = cls.fixture / 'tls'
        tls.mkdir()
        tls.chmod(0o755)
        work = Path(tempfile.mkdtemp(prefix='paxeer-x-evidence-read-ca-'))
        try:
            for argv in (
                    ['openssl', 'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes',
                     '-keyout', str(work / 'ca.key'), '-out', str(tls / 'ca.pem'), '-days', '1',
                     '-subj', '/CN=paxeer-x-evidence-read-ca', '-addext', 'basicConstraints=critical,CA:TRUE',
                     '-addext', 'keyUsage=critical,keyCertSign'],
                    ['openssl', 'req', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes',
                     '-keyout', str(work / 'server.key'), '-out', str(work / 'server.csr'), '-subj', '/CN=localhost']):
                command(argv)
            (work / 'server.ext').write_text('subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\n'
                                             'extendedKeyUsage=serverAuth\nkeyUsage=critical,digitalSignature\n')
            command(['openssl', 'x509', '-req', '-in', str(work / 'server.csr'), '-CA', str(tls / 'ca.pem'),
                     '-CAkey', str(work / 'ca.key'), '-CAcreateserial', '-days', '1',
                     '-extfile', str(work / 'server.ext'), '-out', str(work / 'server.pem')])
            command(['openssl', 'x509', '-in', str(work / 'server.pem'), '-outform', 'DER', '-out', str(tls / 'cert.der')])
            command(['openssl', 'x509', '-in', str(tls / 'ca.pem'), '-outform', 'DER', '-out', str(tls / 'ca.der')])
            command(['openssl', 'pkcs8', '-topk8', '-nocrypt', '-in', str(work / 'server.key'), '-outform', 'DER',
                     '-out', str(tls / 'key.der')])
        finally:
            shutil.rmtree(work)
        for name in ('ca.pem', 'ca.der', 'cert.der'):
            (tls / name).chmod(0o444)
        os.chown(tls / 'key.der', 4021, 4020)
        (tls / 'key.der').chmod(0o400)
        (cls.fixture / 'probe.py').write_text(PROBE)
        (cls.fixture / 'probe.py').chmod(0o444)
        cls.identity = {
            'LAYERX_AUTHORITY_SEQUENCER_ID': os.urandom(32).hex(),
            'LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY': os.urandom(32).hex(),
            'LAYERX_AUTHORITY_REPLICA_ID': os.urandom(32).hex(),
        }
        print('evidence-read revision=' + cls.revision + ' production_sha256=' +
              hashlib.sha256(cls.production.encode()).hexdigest() + ' authority_sha256=' +
              hashlib.sha256(binary.read_bytes()).hexdigest(), flush=True)

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.fixture)

    def setUp(self):
        self.scratch = Path(tempfile.mkdtemp(prefix='paxeer-x-evidence-read-'))
        self.durable = self.scratch / 'durable'
        self.durable.mkdir()
        self.durable.chmod(0o755)
        self.registry = os.urandom(32).hex()
        self.sequence = 0

    def tearDown(self):
        shutil.rmtree(self.scratch)

    def namespace_case(self, body, registry=None, before=''):
        import shlex
        self.sequence += 1
        sandbox = self.scratch / ('root-' + str(self.sequence))
        sandbox.mkdir(mode=0o700)
        scaffold = '''set -euo pipefail
root=ROOT_PATH
[ "$(readlink /proc/self/ns/mnt)" != ORIGINAL_MOUNT ]
[ "$(readlink /proc/self/ns/pid)" != ORIGINAL_PID ]
ip link set lo up
mount --make-rprivate /
mount -t tmpfs -o mode=0755,nosuid tmpfs "$root"
for directory in usr bin sbin lib lib64 etc; do
    [ ! -d "/$directory" ] || {
        mkdir -p "$root/$directory"
        mount --bind "/$directory" "$root/$directory"
        mount -o remount,bind,ro "$root/$directory"
    }
done
mkdir -m0755 "$root/proc" "$root/dev" "$root/run" "$root/data" "$root/fixture"
mkdir -m1777 "$root/tmp"
for device in null urandom; do
    touch "$root/dev/$device"
    mount --bind "/dev/$device" "$root/dev/$device"
done
mount -t proc -o nosuid,nodev,noexec proc "$root/proc"
mount --bind DURABLE_PATH "$root/data"
mount --bind FIXTURE_PATH "$root/fixture"
mount -o remount,bind,ro "$root/fixture"
exec chroot "$root" /bin/bash -se <<'PAXEER_X_EVIDENCE_READ_CASE'
set -euo pipefail
umask 077
'''
        for name, value in (('ROOT_PATH', str(sandbox)), ('DURABLE_PATH', str(self.durable)),
                            ('FIXTURE_PATH', str(self.fixture)),
                            ('ORIGINAL_MOUNT', self.namespace), ('ORIGINAL_PID', self.pid_namespace)):
            scaffold = scaffold.replace(name, shlex.quote(value))
        scaffold += before
        authority = ['setpriv', '--reuid=4021', '--regid=4020', '--clear-groups', '--no-new-privs', 'env', '-i',
                     'PATH=/usr/bin:/bin', 'LAYERX_AUTHORITY_LISTEN=127.0.0.1:9445',
                     'LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID=7654321',
                     'LAYERX_AUTHORITY_NETWORK_ID=paxeer-x-role-directories',
                     'LAYERX_AUTHORITY_TLS_CERT_DER=/fixture/tls/cert.der',
                     'LAYERX_AUTHORITY_TLS_KEY_DER=/fixture/tls/key.der',
                     'LAYERX_AUTHORITY_CLIENT_CA_DER=/fixture/tls/ca.der',
                     'LAYERX_AUTHORITY_REPLICA_URL=http://127.0.0.1:9402',
                     'LAYERX_AUTHORITY_FIRST_BATCH=1', 'LAYERX_AUTHORITY_LAST_BATCH=18446744073709551615',
                     *(key + '=' + value for key, value in self.identity.items())]
        helpers = ('authority=(' + ' '.join(shlex.quote(item) for item in authority) +
                   ' "LAYERX_AUTHORITY_TOKEN_FILES=$keys/tokens/gateway-authority:$run/registry-authority/token:'
                   '$keys/tokens/webhooks-authority"'
                   ' "LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE=$authority_material/evidence-read.token"'
                   ' "LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE=$keys/tokens/replica-token"'
                   ' "LAYERX_AUTHORITY_LNI_SOCKET=$run/node/layerxd.lni.sock"'
                   ' /fixture/layerx-receipt-authority)\n'
                   '''refused_start() {
    local code
    set +e
    timeout 20 "${authority[@]}" 2>/tmp/refused.log
    code=$?
    set -e
    cat /tmp/refused.log >&2
    [ "$code" -eq 2 ] && grep -qF -- "$1" /tmp/refused.log
}
probe() {
    local pid code
    "${authority[@]}" 2>/tmp/authority.log &
    pid=$!
    set +e
    python3 /fixture/probe.py /fixture/tls/ca.pem 9445 "evidence=$authority_material/evidence-read.token" \\
        "gateway=$keys/tokens/gateway-authority" "registry=$run/registry-authority/token" >/tmp/probe.json
    code=$?
    set -e
    kill -TERM "$pid" 2>/dev/null || true
    for _ in $(seq 50); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
    kill -KILL "$pid" 2>/dev/null || true
    wait "$pid" || true
    cat /tmp/authority.log >&2
    [ "$code" -eq 0 ]
    printf 'PROBE %s\\n' "$(cat /tmp/probe.json)"
}
''')
        registry = self.registry if registry is None else registry
        script = (scaffold + self.production + 'export LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION=' +
                  shlex.quote(registry) + '\n' + self.REGISTRY + helpers + body + '\nPAXEER_X_EVIDENCE_READ_CASE\n')
        argv = ['unshare', '--mount', '--pid', '--fork', '--kill-child=KILL', '--net',
                '--ipc', '--uts', '--propagation', 'private', '--mount-proc', '/bin/bash', '-se']
        try:
            result = subprocess.run(argv, input=script, capture_output=True, text=True, timeout=180,
                                    env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C'})
        except subprocess.TimeoutExpired:
            self.fail('disposable evidence-read namespace exceeded 180 seconds')
        self.assertEqual(os.readlink('/proc/self/ns/mnt'), self.namespace)
        self.assertEqual(os.readlink('/proc/self/ns/pid'), self.pid_namespace)
        return result

    def initialized(self, following, registry=None):
        result = self.namespace_case('human_evidence_read_material\n' + following, registry)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def probed(self, result):
        lines = [line[6:] for line in result.stdout.splitlines() if line.startswith('PROBE ')]
        self.assertEqual(len(lines), 1, result.stdout + result.stderr)
        return json.loads(lines[0])

    def assert_routes(self, routes):
        self.assertEqual(routes['evidence_relay'][0], 503, routes)
        self.assertIn('replica_unavailable', routes['evidence_relay'][1])
        self.assertEqual(routes['evidence_relay_without_digest'][0], 400, routes)
        for forbidden in ('evidence_by_activity', 'evidence_wait_by_activity', 'evidence_internal_authority',
                          'anonymous_relay', 'altered_evidence_relay'):
            self.assertEqual(routes[forbidden][0], 401, (forbidden, routes))
            self.assertIn('identity_required', routes[forbidden][1])
        self.assertEqual(routes['gateway_relay'][0], 503, routes)
        self.assertNotEqual(routes['registry_by_activity'][0], 401, routes)

    def test_01_minted_installed_and_isolated(self):
        result = self.initialized('''python3 - <<'PY_CHECK'
import os,re,stat
source='/data/layerx/keys/human-authority/explorer-evidence-read'
installed='/run/authority-private/material/evidence-read.token'
info=os.lstat(source)
assert stat.S_ISREG(info.st_mode) and (info.st_uid,info.st_gid,stat.S_IMODE(info.st_mode),info.st_nlink)==(0,0,0o600,1)
token=open(source,'rb').read()
assert re.fullmatch(rb'[0-9a-f]{64}',token)
info=os.lstat(installed)
assert stat.S_ISREG(info.st_mode) and (info.st_uid,info.st_gid,stat.S_IMODE(info.st_mode),info.st_nlink)==(4021,4020,0o600,1)
assert open(installed,'rb').read()==token
for directory,expected in (('/run/authority-private',(4021,4020,0o700)),('/run/authority-private/material',(4021,4020,0o700)),('/data/layerx/keys/human-authority',(0,0,0o2700))):
 info=os.lstat(directory)
 assert stat.S_ISDIR(info.st_mode) and (info.st_uid,info.st_gid,stat.S_IMODE(info.st_mode))==expected,directory
for path in ('/','/run','/run/authority-private','/run/authority-private/material','/data','/data/layerx','/data/layerx/keys'):
 assert not os.lstat(path).st_mode&0o002,path
peers=['/data/layerx/keys/tokens/'+name for name in ('backend-admin','gateway-component','gateway-authority','webhooks-component','webhooks-authority','replica-token')]
peers+=['/data/layerx/keys/human-authority/authority-token','/run/layerx/registry-authority/token']
for peer in peers:
 assert open(peer,'rb').read().strip()!=token.strip(),peer
PY_CHECK
setpriv --reuid=4021 --regid=4020 --clear-groups --no-new-privs cmp -s /data/layerx/keys/human-authority/explorer-evidence-read /run/authority-private/material/evidence-read.token 2>/dev/null && exit 1
setpriv --reuid=4021 --regid=4020 --clear-groups --no-new-privs cat /run/authority-private/material/evidence-read.token >/dev/null
for uid in 4020 4026; do
    for action in "cat /run/authority-private/material/evidence-read.token" "ls /run/authority-private/material" \\
        "touch /run/authority-private/material/foreign" "cat /data/layerx/keys/human-authority/explorer-evidence-read" \\
        "rm -f /run/authority-private/material/evidence-read.token"; do
        if setpriv --reuid=$uid --regid=4020 --clear-groups --no-new-privs $action >/dev/null 2>&1; then
            printf 'cross-uid %s allowed: %s\\n' "$uid" "$action" >&2
            exit 1
        fi
    done
done
[ ! -e /run/authority-private/material/foreign ]
[ -s /run/authority-private/material/evidence-read.token ]
printf 'evidence-read isolated\\n'
''')
        self.assertIn('evidence-read isolated', result.stdout)

    def test_02_authority_accepts_only_receipt_authority_route(self):
        self.assert_routes(self.probed(self.initialized('probe\n')))

    def test_03_bearer_collisions_refuse_startup(self):
        token = self.initialized('cat "$authority_material/evidence-read.token"\n').stdout
        self.assertRegex(token, r'^[0-9a-f]{64}$')
        result = self.initialized('''printf %s "$LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION_SEEN" | cmp - "$run/registry-authority/token"
refused_start 'must differ from every general and replica bearer'
export LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION=DISTINCT
registry_bearer LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION registry-authority
tr -d '\\n' <"$keys/tokens/replica-token" >"$keys/human-authority/explorer-evidence-read"
rm -f "$authority_material/evidence-read.token"
human_evidence_read_material
tr -d '\\n' <"$keys/tokens/replica-token" | cmp - "$authority_material/evidence-read.token"
refused_start 'must differ from every general and replica bearer'
printf 'collisions refused\\n'
'''.replace('DISTINCT', os.urandom(32).hex()).replace('$LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION_SEEN', token),
            registry=token)
        self.assertIn('collisions refused', result.stdout)

    def test_04_unprotected_source_is_never_installed(self):
        result = self.initialized('''install -o 0 -g 0 -m 0600 "$keys/human-authority/explorer-evidence-read" /tmp/original
source=$keys/human-authority/explorer-evidence-read
for variant in symlink owner group-mode hardlink directory; do
    rm -rf "$source" "$authority_material/evidence-read.token" "$keys/human-authority/evidence-link"
    install -o 0 -g 0 -m 0600 /tmp/original "$source"
    case $variant in
        symlink) rm "$source"; ln -s "$keys/tokens/gateway-authority" "$source" ;;
        owner) chown 4021:4020 "$source" ;;
        group-mode) chmod 0640 "$source" ;;
        hardlink) ln "$source" "$keys/human-authority/evidence-link" ;;
        directory) rm "$source"; mkdir -m 0700 "$source" ;;
    esac
    set +e
    human_evidence_read_material 2>/tmp/refused.log
    code=$?
    set -e
    cat /tmp/refused.log >&2
    [ "$code" -eq 1 ]
    grep -qF 'explorer evidence-read token refused: owner, type or mode' /tmp/refused.log
    [ ! -e "$authority_material/evidence-read.token" ]
    printf 'refused %s\\n' "$variant"
done
[ "$(stat -c '%u:%g:%a' "$keys/tokens/gateway-authority")" = 4020:4020:440 ]
''')
        for variant in ('symlink', 'owner', 'group-mode', 'hardlink', 'directory'):
            self.assertIn('refused ' + variant + '\n', result.stdout)

    def test_05_unprotected_installed_token_refuses_startup(self):
        result = self.initialized('''token=$authority_material/evidence-read.token
chown 4020:4020 "$token"
refused_start 'LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE is unavailable or unprotected'
chown 4021:4020 "$token"
chmod 0640 "$token"
refused_start 'LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE is unavailable or unprotected'
chmod 0600 "$token"
mv "$token" "$token.moved"
ln -s "$token.moved" "$token"
refused_start 'LAYERX_AUTHORITY_EVIDENCE_READ_TOKEN_FILE is unavailable or unprotected'
printf 'installed token refusals\\n'
''')
        self.assertIn('installed token refusals', result.stdout)

    def test_06_restart_preserves_durable_token_and_recreates_runtime(self):
        first = self.initialized('''touch /run/authority-private/ephemeral
sha256sum "$keys/human-authority/explorer-evidence-read" "$authority_material/evidence-read.token" | sed 's/^/SHA /'
probe
''')
        self.assert_routes(self.probed(first))
        before = [line.split()[1] for line in first.stdout.splitlines() if line.startswith('SHA ')]
        self.assertEqual(len(before), 2)
        self.assertEqual(before[0], before[1])
        second = self.initialized('''[ ! -e /run/authority-private/ephemeral ]
sha256sum "$keys/human-authority/explorer-evidence-read" "$authority_material/evidence-read.token" | sed 's/^/SHA /'
[ "$(stat -c '%u:%g:%a' /run/authority-private/material "$authority_material/evidence-read.token")" = "$(printf '4021:4020:700\\n4021:4020:600')" ]
probe
''')
        after = [line.split()[1] for line in second.stdout.splitlines() if line.startswith('SHA ')]
        self.assertEqual(after, before)
        self.assert_routes(self.probed(second))

    def test_07_restart_refuses_redirected_runtime_directory(self):
        before = self.initialized('sha256sum "$keys/human-authority/explorer-evidence-read"\n').stdout
        result = self.namespace_case('printf unreachable\\n\n', before='ln -s /data /run/authority-private\n')
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn('unreachable', result.stdout)
        self.assertIn('private runtime mount refused: /run/authority-private', result.stderr)
        self.assertFalse((self.durable / 'material').exists())
        source = self.durable / 'layerx/keys/human-authority/explorer-evidence-read'
        self.assertEqual(before.split()[0], hashlib.sha256(source.read_bytes()).hexdigest())


PROBE = r'''import json, socket, ssl, sys, time
ca, port = sys.argv[1], int(sys.argv[2])
tokens = {}
for argument in sys.argv[3:]:
    name, path = argument.split('=', 1)
    tokens[name] = open(path).read().strip()
context = ssl.create_default_context(cafile=ca)


def get(path, token=None):
    request = 'GET ' + path + ' HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n'
    if token is not None:
        request += 'Authorization: Bearer ' + token + '\r\n'
    data = b''
    with socket.create_connection(('127.0.0.1', port), timeout=20) as raw:
        with context.wrap_socket(raw, server_hostname='localhost') as tls:
            tls.sendall((request + '\r\n').encode())
            while True:
                try:
                    chunk = tls.recv(65536)
                except ssl.SSLEOFError:
                    break
                if not chunk:
                    break
                data += chunk
    head, _, body = data.partition(b'\r\n\r\n')
    return [int(head.split(b' ')[1]), body.decode(errors='replace')]


deadline = time.monotonic() + 20
while True:
    try:
        if get('/livez')[0] == 200:
            break
    except OSError:
        pass
    if time.monotonic() > deadline:
        raise SystemExit('authority did not become live')
    time.sleep(0.1)
batch, digest, activity = '11' * 32, '22' * 32, '33' * 32
relay = '/v1/batches/' + batch + '/receipt-authority?receipt_digest=' + digest
evidence = tokens['evidence']
print(json.dumps({
    'evidence_relay': get(relay, evidence),
    'evidence_relay_without_digest': get('/v1/batches/' + batch + '/receipt-authority', evidence),
    'evidence_by_activity': get('/v1/authorized-batches/by-activity/' + activity, evidence),
    'evidence_wait_by_activity': get('/v1/authorized-batches/wait-by-activity/' + activity, evidence),
    'evidence_internal_authority': get('/internal/v1/activities/' + activity + '/authority', evidence),
    'anonymous_relay': get(relay),
    'altered_evidence_relay': get(relay, evidence[:-1] + ('0' if evidence[-1] != '0' else '1')),
    'gateway_relay': get(relay, tokens['gateway']),
    'registry_by_activity': get('/v1/authorized-batches/by-activity/' + activity, tokens['registry']),
}, sort_keys=True))
'''


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


def registry_material():
    import importlib.util
    import stat
    os.umask(0o077)
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location('registry_material', ROOT / 'platform/hosted/human/material.py')
    material = importlib.util.module_from_spec(specification)
    sys.path.insert(0, str(ROOT / 'platform/hosted/human'))
    specification.loader.exec_module(material)
    evidence = None
    containers = []
    volume = None
    result = {'task': '24.8', 'cases': [], 'tests': 0, 'skipped': 0, 'exit_code': 1}

    def require(condition, message):
        if not condition:
            raise RuntimeError(message)

    def case(name):
        result['cases'].append(name)
        print('PASS ' + name, flush=True)

    def execute(container, user, *args, check=True):
        return docker('exec', '--user', user, container, *args, check=check)

    def python(container, user, source, *args, check=True):
        return execute(container, user, 'python3', '-c', source, *args, check=check)

    try:
        raw = os.environ.get('PAXEER_X_REGISTRY_MATERIAL_EVIDENCE')
        if not raw:
            raise FileNotFoundError('PAXEER_X_REGISTRY_MATERIAL_EVIDENCE owned0700 directory required')
        evidence = Path(raw)
        material.protected_file(evidence, 0o700)
        revision = command(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']).stdout.strip()
        require(not command(['git', '-C', str(ROOT), 'status', '--porcelain']).stdout.strip(), 'published clean candidate required')
        raw = os.environ.get('PAXEER_X_FOUNDATION_MANIFEST')
        if not raw:
            raise FileNotFoundError('genuine qualified24.11 PAXEER_X_FOUNDATION_MANIFEST required')
        manifest_path = Path(raw)
        material.protected_file(manifest_path, 0o600)
        foundation = material.protected_json(manifest_path)
        proof = material.protected_json(manifest_path.parent / 'qualification.json')
        require(foundation.get('stage') == 'dependency-foundation' and foundation.get('purpose') == 'disposable-test-only'
                and proof.get('exit_code') == 0 and proof.get('tests', 0) > 0 and proof.get('skipped') == 0
                and foundation.get('producers') and all(p.get('exit_code') == 0 and p.get('command') for p in foundation['producers']),
                'actual qualified native producer provenance required')
        native_path = foundation['inputs']['native_genesis']
        require(native_path == 'node/genesis/genesis.manifest', 'native producer generation path mismatch')
        for name in (native_path, foundation['inputs']['metadata']):
            expected = foundation['generated_artifacts'][name]
            require(hashlib.sha256((manifest_path.parent / name).read_bytes()).hexdigest() == expected['sha256'],
                    'qualified producer document changed')
        require(os.geteuid() == 0, 'root-controlled local disposable Docker qualification required')
        context = json.loads(command(['docker', 'context', 'inspect']).stdout)
        host = context[0]['Endpoints']['docker']['Host']
        require(host.startswith('unix:///'), 'remote or unauthenticated Docker handoff refused')
        socket = Path(host.removeprefix('unix://'))
        info = socket.stat()
        require(stat.S_ISSOCK(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o002,
                'private authenticated Docker owner socket required')
        images = {}
        for role, environment, files in (
                ('node', 'PAXEER_X_NODE_IMAGE', {'/usr/local/lib/layerx-human/material.py': 'platform/hosted/human/material.py',
                                                '/opt/layerx/supervisor.sh': 'platform/hosted/node/supervisor.sh'}),
                ('registry', 'PAXEER_X_REGISTRY_IMAGE', {'/usr/local/lib/layerx-human/material.py': 'platform/hosted/human/material.py',
                                                        '/usr/local/bin/registry-fly-init': 'docker/platform-registry/init.sh'})):
            image = os.environ.get(environment, '')
            if not re.fullmatch(r'sha256:[0-9a-f]{64}', image):
                raise FileNotFoundError(environment + ' actual source-bound packaged image required')
            metadata = json.loads(docker('image', 'inspect', image).stdout)[0]
            require(metadata['Id'] == image and (metadata['Config'].get('Labels') or {}).get('org.opencontainers.image.revision') == revision,
                    role + ' packaged image source identity mismatch')
            lines = docker('run', '--rm', '--pull=never', '--network=none', '--entrypoint', '/usr/bin/sha256sum',
                           image, *files).stdout.splitlines()
            require(len(lines) == len(files), role + ' packaged production source inventory incomplete')
            for line, relative in zip(lines, files.values()):
                require(line.split()[0] == hashlib.sha256((ROOT / relative).read_bytes()).hexdigest(),
                        role + ' packaged production helper differs')
            images[role] = image
        volume = 'paxeer-x-registry-material-' + uuid.uuid4().hex
        docker('volume', 'create', volume)
        for role in ('node', 'registry'):
            name = volume + '-' + role
            containers.append(name)
            args = ['run', '--detach', '--pull=never', '--network=none', '--user', '0:0', '--name', name,
                    '--mount', 'type=volume,src=' + volume + ',dst=/case']
            if role == 'node':
                args += ['--mount', 'type=bind,src=' + str(manifest_path.parent) + ',dst=/foundation,readonly']
            docker(*args, '--entrypoint', '/bin/sh', images[role], '-ec', 'exec sleep 1800')
        node, registry = containers
        execute(node, '0:0', 'bash', '-euc', 'umask 077; cp -a /foundation/node /case/node; chown -R 4020:4020 /case/node; chmod 0700 /case/node; mkdir /case/producer; chown 4020:4020 /case/producer; chmod 0700 /case/producer; mkdir /case/consumer; chown 4030:4030 /case/consumer; chmod 0700 /case/consumer')
        helper = '/usr/local/lib/layerx-human/material.py'
        producer = ('python3', helper, '--hosted-registry-material-produce', '/case/node', '/case/producer/kernel-material')
        created = json.loads(execute(node, '4020:4020', *producer).stdout)
        selected = created['directory']
        generation = created['generation']
        case('actual-hosted-native-signed-genesis-producer')
        exported = execute(node, '4020:4020', 'python3', helper, '--export-registry-material', '/case/producer/kernel-material').stdout
        transfer = subprocess.run(['docker', 'exec', '-i', '--user', '0:0', registry, 'python3', '-c',
            "import os,sys; p='/case/consumer/export.json'; fd=os.open(p,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600); data=sys.stdin.buffer.read(1048577); assert 0<len(data)<=1048576; f=os.fdopen(fd,'wb'); f.write(data); f.flush(); os.fsync(f.fileno()); f.close(); os.chown(p,4030,4030)"],
            input=exported, text=True, capture_output=True, timeout=30)
        require(transfer.returncode == 0, 'authenticated private public-generation transfer failed')
        transfer_manifest = material.registry_manifest({name: __import__('base64').b64decode(value, validate=True)
            for name, value in json.loads(exported)['files'].items()})
        require(transfer_manifest['network_id'] == foundation['network_id']
                and transfer_manifest['sequencer_id'] == foundation['sequencer_id']
                and transfer_manifest['sequencer_public_key'] == foundation['sequencer_public_key'],
                'actual selected foundation producer identity differs')
        importer = ('python3', helper, '--import-registry-material', '/case/consumer/export.json', '/case/consumer/material',
                    str(foundation['network_id']), foundation['sequencer_id'], foundation['sequencer_public_key'])
        imported = json.loads(execute(registry, '4030:4030', *importer).stdout)
        require(imported['generation'] == generation, 'handoff selected another producer generation')
        consumer = imported['directory']
        validator = ('/usr/local/bin/registry-fly-init', '--validate-kernel-material', '/case/consumer/material')
        validated = json.loads(execute(registry, '4030:4030', *validator).stdout)
        require(validated['generation'] == generation, 'actual registry readiness validator selected another generation')
        python(registry, '0:0', "from pathlib import Path; import sys; a,b=map(Path,sys.argv[1:]); assert {p.name for p in a.iterdir()}=={'generation.json','replica-id','trust-history'}; assert all((a/p.name).read_bytes()==p.read_bytes() for p in b.iterdir())", selected, consumer)
        case('authenticated-private-transfer-exact-producer-bytes-and-registry-validation')
        wrong = list(importer)
        wrong[-3] = str(foundation['network_id'] + 1)
        require(execute(registry, '4030:4030', *wrong, check=False).returncode != 0, 'wrong authenticated producer network admitted')
        wrong = list(importer)
        wrong[-1] = '0' * 64
        require(execute(registry, '4030:4030', *wrong, check=False).returncode != 0, 'wrong authenticated public key admitted')
        case('wrong-authenticated-network-and-public-key-refused')
        for label, source in (
            ('independent-history', "p=d/'trust-history'; original=p.read_bytes(); p.write_bytes(original+b'foreign');"),
            ('mismatched-replica', "p=d/'replica-id'; original=p.read_bytes(); p.write_bytes(b'0'*64+b'\\n');"),
            ('partial-generation', "p=d/'generation.json'; original=p.read_bytes(); p.unlink();")):
            python(registry, '4030:4030', "from pathlib import Path; import sys; d=Path(sys.argv[1]); " + source + " (d.parent/'restore.bin').write_bytes(original)", consumer)
            require(execute(registry, '4030:4030', *validator, check=False).returncode != 0, label + ' admitted before readiness')
            name = 'generation.json' if label == 'partial-generation' else 'replica-id' if label == 'mismatched-replica' else 'trust-history'
            python(registry, '4030:4030', "from pathlib import Path; import os,sys; d=Path(sys.argv[1]); p=d/sys.argv[2]; p.write_bytes((d.parent/'restore.bin').read_bytes()); os.chmod(p,0o600); (d.parent/'restore.bin').unlink()", consumer, name)
            case(label + '-refused-before-registry-readiness')
        path = consumer + '/trust-history'
        execute(registry, '0:0', 'chmod', '0660', path)
        require(execute(registry, '4030:4030', *validator, check=False).returncode != 0, 'wrong mode admitted')
        execute(registry, '0:0', 'chmod', '0600', path)
        execute(registry, '0:0', 'chown', '4020:4020', path)
        require(execute(registry, '0:0', *validator, check=False).returncode != 0, 'mixed producer/consumer owner admitted')
        execute(registry, '0:0', 'chown', '4030:4030', path)
        case('wrong-mode-and-mixed-authority-owner-refused')
        replay = json.loads(execute(node, '4020:4020', *producer).stdout)
        require(replay == created, 'producer restart replaced retained selected generation')
        replay = json.loads(execute(registry, '4030:4030', *importer).stdout)
        require(replay == imported, 'same authenticated handoff replay changed retained generation')
        case('actual-producer-restart-and-handoff-replay-idempotent')
        python(registry, '4030:4030', "from pathlib import Path; p=Path('/case/consumer/material/.pending-abandoned'); p.mkdir(mode=0o700); (p/'replica-id').write_bytes(b'incomplete')")
        retained = json.loads(execute(registry, '4030:4030', *validator).stdout)
        require(retained['generation'] == generation, 'abandoned unpublished generation replaced prior complete selection')
        require(json.loads(execute(registry, '4030:4030', *importer).stdout) == imported, 'restart could not retain prior complete handoff')
        case('interrupted-unpublished-generation-retains-prior-complete-on-restart')
        execute(registry, '0:0', 'chown', '4021:4021', consumer)
        require(execute(registry, '0:0', *validator, check=False).returncode != 0, 'foreign owner generation admitted')
        execute(registry, '0:0', 'chown', '4030:4030', consumer)
        case('foreign-runtime-owner-generation-refused')
        retained = json.loads(execute(registry, '4030:4030', *validator).stdout)
        require(retained['generation'] == generation, 'final generation identity lost after refusals')
        source = (ROOT / 'platform/hosted/human/provision.sh').read_text()
        require('before' in source and '[ "$before" = "$after" ]' in source
                and 'exec layerx-node-0 -c layerxd --' in source and '--export-registry-material' in source,
                'actual authenticated Kubernetes producer handoff absent')
        supervisor = (ROOT / 'platform/hosted/node/supervisor.sh').read_text()
        publish = supervisor.split('publish_generation() {', 1)[1].split('\n}', 1)[0]
        require(publish.index('--hosted-registry-material-produce') < publish.index('replica-ready'), 'producer waits on replica or Human readiness')
        case('acyclic-production-order-and-identifiable-retained-generation')
        result.update(revision=revision, images=images, foundation_manifest=str(manifest_path), generation=generation,
                      tests=len(result['cases']), exit_code=0)
        material.write_bytes(evidence / 'registry-material.json', json.dumps(result, sort_keys=True).encode())
        print('PAXEER_X_GATE tests=' + str(result['tests']) + ' skipped=0', flush=True)
        return 0
    except FileNotFoundError as error:
        code = 78
        result['observed'] = str(error)
    except Exception as error:
        code = 1
        result['observed'] = str(error)
    finally:
        for container in reversed(containers):
            docker('rm', '-f', container, check=False)
        if volume is not None:
            docker('volume', 'rm', volume, check=False)
    result.update(exit_code=code, tests=len(result['cases']))
    if evidence is not None:
        material.write_bytes(evidence / 'registry-material.json', json.dumps(result, sort_keys=True).encode())
    print('Registry kernel material refused: ' + result['observed'], file=sys.stderr, flush=True)
    return code


def policy_graph():
    import importlib.util
    os.umask(0o077)
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location('policy_material', ROOT / 'platform/hosted/human/material.py')
    material = importlib.util.module_from_spec(specification)
    sys.path.insert(0, str(ROOT / 'platform/hosted/human'))
    specification.loader.exec_module(material)
    evidence = None
    container = None
    volume = None
    result = {'task': '24.2', 'cases': [], 'tests': 0, 'skipped': 0, 'exit_code': 1}

    def require(condition, message):
        if not condition:
            raise RuntimeError(message)

    def case(name):
        result['cases'].append(name)
        print('PASS ' + name, flush=True)

    def refused(action, fragment):
        try:
            action()
        except ValueError as error:
            require(fragment in str(error), 'refusal reason differs: ' + str(error))
            return
        raise RuntimeError('accepted: ' + fragment)

    def copy_tree(source, destination):
        destination.mkdir(mode=0o700)
        for path in sorted(source.rglob('*')):
            target = destination / path.relative_to(source)
            if path.is_symlink():
                raise RuntimeError('producer output symlink refused: ' + str(path))
            if path.is_dir():
                target.mkdir(mode=0o700)
            else:
                material.write_bytes(target, path.read_bytes())

    try:
        raw = os.environ.get('PAXEER_X_POLICY_GRAPH_EVIDENCE')
        if not raw:
            raise FileNotFoundError('PAXEER_X_POLICY_GRAPH_EVIDENCE owned0700 directory required')
        evidence = Path(raw)
        material.protected_file(evidence, 0o700)
        revision = command(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']).stdout.strip()
        require(not command(['git', '-C', str(ROOT), 'status', '--porcelain']).stdout.strip(), 'published clean candidate required')
        require(os.geteuid() == 0, 'root-controlled disposable qualification required')
        order = material.policy_graph_order()
        position = {name: index for index, name in enumerate(order)}
        chain = ['genesis', 'sequencer-identity', 'registry-material', 'module-registry', 'deployment-journal',
                 'assembled-policy', 'role-authority']
        require(all(position[a] < position[b] for a, b in zip(chain, chain[1:])), 'bootstrap order differs from genesis-to-authority')
        for name, (producer, requires) in material.POLICY_GRAPH.items():
            require(all(position[r] < position[name] for r in requires), 'graph order violates ' + name)
            if producer.startswith('owner-input:'):
                require(len(producer) > len('owner-input:'), 'unnamed owner input ' + name)
                continue
            sources = [ROOT / 'platform/hosted/human/provision.sh', ROOT / 'platform/hosted/tests/beta-cluster.sh']
            require(hasattr(material, producer) or __import__('provision').__dict__.get(producer) is not None
                    or any(re.search('^' + re.escape(producer) + r'\(\) *[({]', path.read_text(), re.M) for path in sources),
                    'declared producer absent from source: ' + producer)
        consumed = set(material.EVIDENCE_INPUTS.values()) | {'producer-records/' + n for n in material.PRODUCER_FILES}
        declared = {n for files in material.POLICY_GRAPH_FILES.values() for n in files}
        require(consumed == declared, 'consumed evidence without declared producer: ' + ','.join(sorted(consumed ^ declared)))
        case('every-consumed-document-has-declared-producer-or-owner-input')
        cyclic = dict(material.POLICY_GRAPH, genesis=(material.POLICY_GRAPH['genesis'][0], ('role-authority',)))
        refused(lambda: material.policy_graph_order(cyclic), 'policy graph cycle')
        dangling = dict(material.POLICY_GRAPH, genesis=(material.POLICY_GRAPH['genesis'][0], ('undeclared',)))
        refused(lambda: material.policy_graph_order(dangling), 'without declared producer')
        unproduced = dict(material.POLICY_GRAPH, genesis=('', ()))
        refused(lambda: material.policy_graph_order(unproduced), 'without declared producer')
        case('acyclic-order-and-cycle-detection')
        raw = os.environ.get('PAXEER_X_FOUNDATION_MANIFEST')
        if not raw:
            raise FileNotFoundError('genuine qualified24.11 PAXEER_X_FOUNDATION_MANIFEST required')
        manifest_path = Path(raw)
        material.protected_file(manifest_path, 0o600)
        foundation = material.protected_json(manifest_path)
        proof = material.protected_json(manifest_path.parent / 'qualification.json')
        require(foundation.get('stage') == 'dependency-foundation' and foundation.get('purpose') == 'disposable-test-only'
                and proof.get('exit_code') == 0 and proof.get('tests', 0) > 0 and proof.get('skipped') == 0
                and foundation.get('producers') and all(p.get('exit_code') == 0 and p.get('command') for p in foundation['producers']),
                'actual qualified foundation producer provenance required')
        network, chain_id = foundation['network_id'], foundation['chain_id']
        raw = os.environ.get('PAXEER_X_HUMAN_EVIDENCE_WORK')
        if not raw:
            raise FileNotFoundError('PAXEER_X_HUMAN_EVIDENCE_WORK produced by the actual owner/native/naming/journal producers on this foundation required')
        work = Path(raw).resolve(strict=True)
        provenance = json.loads((work / 'provenance.json').read_text())
        produced = {item.get('producer'): item for item in provenance.get('producers', [])}
        required = {producer for producer, _ in material.POLICY_GRAPH.values()
                    if not producer.startswith('owner-input:') and not hasattr(material, producer)
                    and __import__('provision').__dict__.get(producer) is None}
        require(provenance.get('purpose') == 'disposable-test-only' and provenance.get('source_revision') == revision
                and provenance.get('foundation_sha256') == hashlib.sha256(manifest_path.read_bytes()).hexdigest()
                and required <= set(produced)
                and all(produced[name].get('command') and produced[name].get('exit_code') == 0 for name in required),
                'owner/native/naming/journal evidence requires selected-source producer provenance on this foundation')
        image = os.environ.get('PAXEER_X_NODE_IMAGE', '')
        if not re.fullmatch(r'sha256:[0-9a-f]{64}', image):
            raise FileNotFoundError('PAXEER_X_NODE_IMAGE actual source-bound packaged image required')
        metadata = json.loads(docker('image', 'inspect', image).stdout)[0]
        require(metadata['Id'] == image and (metadata['Config'].get('Labels') or {}).get('org.opencontainers.image.revision') == revision,
                'node packaged image source identity mismatch')
        packaged = docker('run', '--rm', '--pull=never', '--network=none', '--entrypoint', '/usr/bin/sha256sum', image,
                          '/usr/local/lib/layerx-human/material.py').stdout.split()[0]
        require(packaged == hashlib.sha256((ROOT / 'platform/hosted/human/material.py').read_bytes()).hexdigest(),
                'packaged material producer differs')
        state = Path(tempfile.mkdtemp(prefix='policy-graph-', dir=evidence))
        volume = 'paxeer-x-policy-graph-' + uuid.uuid4().hex
        docker('volume', 'create', volume)
        container = volume + '-node'
        docker('run', '--detach', '--pull=never', '--network=none', '--user', '0:0', '--name', container,
               '--mount', 'type=volume,src=' + volume + ',dst=/case',
               '--mount', 'type=bind,src=' + str(manifest_path.parent) + ',dst=/foundation,readonly',
               '--entrypoint', '/bin/sh', image, '-ec', 'exec sleep 1800')
        docker('exec', '--user', '0:0', container, 'bash', '-euc',
               'umask 077; cp -a /foundation/node /case/node; chown -R 4020:4020 /case/node; chmod 0700 /case/node; '
               'mkdir /case/producer; chown 4020:4020 /case/producer; chmod 0700 /case/producer')
        helper = '/usr/local/lib/layerx-human/material.py'
        created = json.loads(docker('exec', '--user', '4020:4020', container, 'python3', helper,
                                    '--hosted-registry-material-produce', '/case/node', '/case/producer/kernel-material').stdout)
        exported = docker('exec', '--user', '4020:4020', container, 'python3', helper,
                          '--export-registry-material', '/case/producer/kernel-material').stdout
        material.write_bytes(state / 'export.json', exported.encode())
        kernel = state / 'registry-kernel'
        imported = material.import_registry_material(state / 'export.json', kernel, network,
                                                     foundation['sequencer_id'], foundation['sequencer_public_key'])
        require(imported['generation'] == created['generation'], 'authenticated handoff selected another kernel generation')
        case('kernel-identity-generation-before-registry-and-policy')
        source = state / 'work'
        source.mkdir(mode=0o700)
        copy_tree(work / 'human-evidence', source / 'human-evidence')
        (source / 'paxeer').mkdir(mode=0o700)
        material.write_bytes(source / 'paxeer/deployment.json', (work / 'paxeer/deployment.json').read_bytes())
        material.write_bytes(source / 'module-registry.json', (work / 'module-registry.json').read_bytes())
        if (work / 'human').is_dir():
            copy_tree(work / 'human', source / 'human')
        journal = source / 'human-evidence/journal'
        records = material.journal_records(journal)
        require(any(name.endswith('.admission') for name in records) and any(name.endswith('.deployment') for name in records),
                'nonempty admitted/deployed Programs journal required')

        def status(root, bundle, authority, registry=kernel, net=network):
            return material.policy_graph_status(root / 'human-evidence', root / 'human-evidence/journal',
                                                root / 'paxeer/deployment.json', root / 'module-registry.json',
                                                bundle, registry, authority, net, chain_id)

        bundle = source / 'bundle'
        authority = state / 'authority-graph'
        placement = state / 'human-authority'
        placement.mkdir(mode=0o700)
        before = status(source, bundle, authority)
        require(before['nodes']['assembled-policy']['state'] == 'waiting'
                and all(before['nodes'][n]['state'] == 'ready' for n in order if n not in ('assembled-policy', 'role-authority'))
                and before['roles']['receipt-authority'] == 'waiting', 'producer inputs not complete before assembly')
        refused(lambda: material.publish_authority_material(bundle, kernel, authority, network, chain_id), '')
        require(not authority.exists() or not (authority / 'current').is_symlink(), 'authority published before assembled policy')
        bundle.mkdir(mode=0o700)
        material.assemble_policy(source / 'human-evidence', source / 'paxeer/deployment.json', source / 'module-registry.json',
                                 bundle / 'policy.json', network, chain_id)
        published = material.publish_authority_material(bundle, kernel, authority, network, chain_id, placement)
        complete = status(source, bundle, authority)
        require(all(node['state'] == 'ready' for node in complete['nodes'].values())
                and all(state_ == 'ready' for state_ in complete['roles'].values()), 'complete graph not ready: ' + json.dumps(complete))
        selected = material.verify_authority_material(authority)
        policy = material.parse(material.read_bytes(bundle / 'policy.json'))
        require(material.parse(material.read_bytes(placement / 'principal-policy.json')) == policy['principal_policy']
                and material.parse(material.read_bytes(placement / 'authority.json')) == policy['authority']
                and material.read_bytes(placement / 'registry.json') == material.read_bytes(bundle / 'inputs/module-registry.json')
                and selected['registry_generation'] == imported['generation'] and selected['generation'] == published['generation'],
                'placed role authority differs from validated generation')
        case('complete-graph-publishes-validated-policy-and-role-authority')
        missing = [('paxeer-deployment', lambda r: (r / 'paxeer/deployment.json').unlink()),
                   ('module-registry', lambda r: (r / 'module-registry.json').unlink()),
                   ('deployment-journal', lambda r: shutil.rmtree(r / 'human-evidence/journal'))]
        for node in ('owner-evidence', 'native-evidence', 'naming-evidence', 'principal-policy'):
            for name in material.POLICY_GRAPH_FILES[node]:
                missing.append((node, lambda r, name=name: (r / 'human-evidence' / name).unlink()))
        for node, remove in missing + [('registry-material', None)]:
            root = Path(tempfile.mkdtemp(prefix='missing-', dir=state))
            os.rmdir(root)
            copy_tree(source, root)
            shutil.rmtree(root / 'bundle')
            registry = kernel if remove else root / 'absent-registry'
            if remove:
                remove(root)
            observed = status(root, root / 'bundle', root / 'authority', registry)
            require(observed['nodes'][node]['state'] == 'waiting', node + ' missing input not typed waiting')
            require(all(observed['nodes'][n]['state'] == 'waiting' for n in ('assembled-policy', 'role-authority')),
                    node + ' missing input admitted downstream policy')
            require(observed['roles']['receipt-authority'] == 'waiting' and not any(s == 'ready' for r, s in observed['roles'].items()
                    if r != 'human-security' or node in ('genesis', 'sequencer-identity', 'registry-material')),
                    node + ' missing input readied an affected role')
            (root / 'bundle').mkdir(mode=0o700)
            if remove:
                refused(lambda: material.assemble_policy(root / 'human-evidence', root / 'paxeer/deployment.json',
                                                         root / 'module-registry.json', root / 'bundle/policy.json',
                                                         network, chain_id), '')
                require(not any((root / 'bundle').iterdir()), node + ' partial bundle written from missing input')
            shutil.rmtree(root)
        case('each-missing-input-keeps-affected-roles-waiting')
        root = Path(tempfile.mkdtemp(prefix='inconsistent-', dir=state))
        os.rmdir(root)
        copy_tree(source, root)
        shutil.rmtree(root / 'bundle')
        (root / 'bundle').mkdir(mode=0o700)
        context = root / 'human-evidence/producer-records/native-context.json'
        value = material.parse(material.read_bytes(context))
        value['sequencer_public_key'] = hashlib.sha256(b'foreign sequencer').hexdigest()
        context.unlink()
        material.write_bytes(context, json.dumps(value, sort_keys=True).encode())
        observed = status(root, root / 'bundle', root / 'authority')
        require(observed['nodes']['native-evidence']['state'] == 'refused'
                and all(observed['roles'][r] == 'refused' for r in ('human-components', 'human-owner', 'receipt-authority')),
                'inconsistent native sequencer evidence admitted')
        observed = status(source, bundle, authority, net=network + 1)
        require(observed['nodes']['registry-material']['state'] == 'refused'
                and all(s == 'refused' for s in observed['roles'].values()), 'wrong network kernel generation admitted')
        refused(lambda: material.assemble_policy(root / 'human-evidence', root / 'paxeer/deployment.json', root / 'module-registry.json',
                                                 root / 'bundle/policy.json', network, chain_id), '')
        require(not any((root / 'bundle').iterdir()), 'inconsistent evidence wrote a policy bundle')
        refused(lambda: material.publish_authority_material(root / 'bundle', kernel, root / 'authority', network, chain_id), '')
        require(not (root / 'authority/current').is_symlink(), 'inconsistent input published authority')
        shutil.rmtree(root)
        case('inconsistent-inputs-refused-without-placeholder-or-bypass')
        generations = sorted((authority / 'generations').iterdir())
        replay = material.publish_authority_material(bundle, kernel, authority, network, chain_id, placement)
        require(replay == published and sorted((authority / 'generations').iterdir()) == generations
                and material.import_registry_material(state / 'export.json', kernel, network, foundation['sequencer_id'],
                                                      foundation['sequencer_public_key']) == imported,
                'restart regenerated identity or authority generation')
        material.assemble_policy(source / 'human-evidence', source / 'paxeer/deployment.json', source / 'module-registry.json',
                                 bundle / 'policy.json', network, chain_id)
        case('restart-resumes-existing-generation')
        pending = authority / '.pending-interrupted'
        pending.mkdir(mode=0o700)
        material.write_bytes(pending / 'registry.json', material.read_bytes(placement / 'registry.json'))
        require(material.publish_authority_material(bundle, kernel, authority, network, chain_id, placement) == published
                and not pending.exists(), 'consistent interrupted publication not resumed')
        pending.mkdir(mode=0o700)
        material.write_bytes(pending / 'authority.json', b'{"tenant":"foreign"}')
        refused(lambda: material.publish_authority_material(bundle, kernel, authority, network, chain_id), 'inconsistent partial authority publication')
        require(material.verify_authority_material(authority)['generation'] == published['generation'], 'refusal abandoned retained generation')
        shutil.rmtree(pending)
        target = Path(published['directory'])
        retained = material.read_bytes(target / 'authority.json')
        (target / 'authority.json').unlink()
        refused(lambda: material.verify_authority_material(authority), 'inconsistent partial authority publication')
        require(status(source, bundle, authority)['nodes']['role-authority']['state'] == 'refused', 'partial generation not typed refused')
        refused(lambda: material.publish_authority_material(bundle, kernel, authority, network, chain_id), 'inconsistent partial authority publication')
        material.write_bytes(target / 'authority.json', retained)
        moved = placement / 'authority.json'
        moved.unlink()
        material.write_bytes(moved, b'{"tenant":"placeholder"}')
        refused(lambda: material.publish_authority_material(bundle, kernel, authority, network, chain_id, placement), 'placed role authority differs')
        moved.unlink()
        material.write_bytes(moved, retained)
        lock = bundle.parent / ('.' + bundle.name + '-publish')
        lock.mkdir(mode=0o700)
        refused(lambda: material.assemble_policy(source / 'human-evidence', source / 'paxeer/deployment.json', source / 'module-registry.json',
                                                 bundle / 'policy.json', network, chain_id), 'bundle publication interrupted')
        lock.rmdir()
        require(material.publish_authority_material(bundle, kernel, authority, network, chain_id, placement) == published,
                'durable generation lost after interrupted publication')
        case('interrupted-and-inconsistent-partial-publication-detected')
        init = (ROOT / 'docker/kernel/init.sh').read_text()
        body = init.split('human_authority_publish() {', 1)[1].split('\n}\n', 1)[0]
        require('--policy-graph-status' in body and body.index('--policy-graph-status') < body.index('--publish-authority-material')
                and '$kernel_registry_material' in body and '"$keys/human-authority"' in body, 'kernel authority producer absent')
        provision = (ROOT / 'platform/hosted/human/provision.sh').read_text()
        flow = provision.split('human_evidence_provision() (', 1)[1].split('\n)\n', 1)[0]
        require(flow.index('human_native_owner_prepare') < flow.index('human_journal_deploy') < flow.index('human_policy_graph_inputs')
                and 'registry_kernel_material' in provision.split('human_native_owner_prepare() (', 1)[1].split('\n)\n', 1)[0],
                'provision order does not place registry and deployment evidence before policy consumers')
        case('production-init-and-provision-order')
        result.update(revision=revision, image=image, foundation_manifest=str(manifest_path), generation=published['generation'],
                      tests=len(result['cases']), exit_code=0)
        material.write_bytes(evidence / 'policy-graph.json', json.dumps(result, sort_keys=True).encode())
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
        if volume is not None:
            docker('volume', 'rm', volume, check=False)
    result.update(exit_code=code, tests=len(result['cases']))
    if evidence is not None:
        material.write_bytes(evidence / ('policy-graph-' + uuid.uuid4().hex + '.json'), json.dumps(result, sort_keys=True).encode())
    print('Human policy graph refused: ' + result['observed'], file=sys.stderr, flush=True)
    return code


class IdentityRotation(unittest.TestCase):
    marker = 'find "$node_data" -mindepth 1 -delete'

    @classmethod
    def setUpClass(cls):
        if os.geteuid() != 0:
            raise RuntimeError('identity rotation requires root and private Linux namespaces')
        for executable in ('unshare', 'mount', 'chroot', 'bash', 'python3', 'openssl', 'flock', 'git'):
            if shutil.which(executable) is None:
                raise RuntimeError('missing identity rotation prerequisite: ' + executable)
        if command(['git', '-C', str(ROOT), 'status', '--porcelain']).stdout.strip():
            raise RuntimeError('published clean candidate required')
        tool = 'tools/bringup/kernel-genesis.sh'
        cls.legacy = None
        for revision in command(['git', '-C', str(ROOT), 'log', '--format=%H', '-S', cls.marker, '--', tool]).stdout.split():
            if cls.marker not in command(['git', '-C', str(ROOT), 'show', revision + ':' + tool]).stdout:
                cls.legacy = revision + '^'
                break
        if cls.legacy is None:
            raise RuntimeError('the revision that retired the deleting rotation is not in history')
        cls.legacy_tool = command(['git', '-C', str(ROOT), 'show', cls.legacy + ':' + tool]).stdout
        cls.legacy_init = command(['git', '-C', str(ROOT), 'show', cls.legacy + ':docker/kernel/init.sh']).stdout
        if cls.marker not in cls.legacy_tool or 'identity_generation' in cls.legacy_init:
            raise RuntimeError('the legacy rotation and init are not the pre-reconciliation production paths')
        cls.namespace = os.readlink('/proc/self/ns/mnt')
        cls.pid_namespace = os.readlink('/proc/self/ns/pid')
        print('identity rotation legacy=' + command(['git', '-C', str(ROOT), 'rev-parse', cls.legacy]).stdout.strip(), flush=True)

    def namespace_case(self, scenario):
        import shlex
        with tempfile.TemporaryDirectory(prefix='paxeer-x-identity-rotation-') as temporary:
            scratch = Path(temporary)
            os.chmod(scratch, 0o700)
            root = scratch / 'root'
            root.mkdir()
            fixture = scratch / 'fixture'
            fixture.mkdir(mode=0o700)
            (fixture / 'case.py').write_text(self.case_source)
            (fixture / 'legacy-kernel-genesis.sh').write_text(self.legacy_tool)
            (fixture / 'legacy-init.sh').write_text(self.legacy_init)
            script = '''set -euo pipefail
root=ROOT_PATH
fixture=FIXTURE_PATH
source=SOURCE_PATH
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
mount -t tmpfs -o mode=0755,nosuid tmpfs "$root/usr/local/bin"
mount -t tmpfs -o mode=0755,nosuid tmpfs "$root/usr/local/lib"
mkdir -p "$root/usr/local/lib/python3.12" "$root/usr/local/lib/layerx-human" "$root/opt/layerx/paxeer" \
    "$root/proc" "$root/dev" "$root/run" "$root/data" "$root/etc/ssl" "$root/fixture" "$root/source"
mkdir -m1777 "$root/tmp"
bind() {
    [ -d "$1" ] || touch "$2"
    mount --bind "$1" "$2"
    mount -o remount,bind,ro "$2"
}
[ ! -d /usr/local/lib/python3.12 ] || bind /usr/local/lib/python3.12 "$root/usr/local/lib/python3.12"
bind "$source/platform/hosted/human" "$root/usr/local/lib/layerx-human"
bind "$source/docker/kernel/init.sh" "$root/usr/local/bin/kernel-init"
bind "$source/tools/bringup/kernel-genesis.sh" "$root/usr/local/bin/kernel-genesis.sh"
bind "$source/platform/hosted/node/checkpoint-authority.py" "$root/opt/layerx/checkpoint-authority.py"
bind "$source/platform/hosted/paxeer/evm.py" "$root/opt/layerx/paxeer/evm.py"
bind /etc/ssl "$root/etc/ssl"
bind "$fixture" "$root/fixture"
bind "$source" "$root/source"
for device in null zero urandom full; do
    touch "$root/dev/$device"
    mount --bind "/dev/$device" "$root/dev/$device"
done
ln -s /proc/self/fd "$root/dev/fd"
ln -s /proc/self/fd/0 "$root/dev/stdin"
ln -s /proc/self/fd/1 "$root/dev/stdout"
ln -s /proc/self/fd/2 "$root/dev/stderr"
mount -t proc -o nosuid,nodev,noexec proc "$root/proc"
mount -t tmpfs -o mode=0755,nosuid tmpfs "$root/data"
mount -t tmpfs -o mode=0755,nosuid tmpfs "$root/run"
exec chroot "$root" /usr/bin/python3 /fixture/case.py SCENARIO
'''
            for name, value in (('ROOT_PATH', str(root)), ('FIXTURE_PATH', str(fixture)),
                                ('SOURCE_PATH', str(ROOT)), ('ORIGINAL_MOUNT', self.namespace),
                                ('ORIGINAL_PID', self.pid_namespace), ('SCENARIO', scenario)):
                script = script.replace(name, shlex.quote(value))
            result = subprocess.run(
                ['unshare', '--mount', '--pid', '--fork', '--kill-child=KILL', '--net',
                 '--ipc', '--uts', '--propagation', 'private', '--mount-proc', '/bin/bash', '-se'],
                input=script, capture_output=True, text=True, timeout=300,
                env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C'})
            self.assertEqual(os.readlink('/proc/self/ns/mnt'), self.namespace)
            self.assertEqual(os.readlink('/proc/self/ns/pid'), self.pid_namespace)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('identity-rotation passed ' + scenario, result.stdout)

    def test_01_compatible_legacy_volume_adopts_and_stays_idempotent(self):
        self.namespace_case('compatible')

    def test_02_plan_inventories_bindings_and_dispositions(self):
        self.namespace_case('plan')

    def test_03_inferred_old_genesis_bindings_refuse_until_authorized_migration(self):
        self.namespace_case('inferred')

    def test_04_rotation_retains_generation_and_migration_rebinds(self):
        self.namespace_case('rotation')

    def test_05_interrupted_rotation_and_migration_resume_to_one_generation(self):
        self.namespace_case('interrupted')

    def test_06_inconsistent_bindings_refuse_rotation_and_migration(self):
        self.namespace_case('inconsistent')

    case_source = r'''
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys

os.umask(0o077)
scenario = sys.argv[1]
ENV = {'PATH': '/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C', 'LAYERX_NODE_NETWORK_ID': '7654321',
       'LAYERX_KERNEL_PROFILE': 'full', 'PYTHONDONTWRITEBYTECODE': '1'}
INIT = Path('/usr/local/bin/kernel-init').read_text()
TOOL = '/usr/local/bin/kernel-genesis.sh'
LEGACY_INIT = Path('/fixture/legacy-init.sh').read_text()
LEGACY_TOOL = '/fixture/legacy-kernel-genesis.sh'
MATERIAL = '/usr/local/lib/layerx-human/material.py'
layerx = Path('/data/layerx')
keys = layerx / 'keys'
genesis = layerx / 'genesis'
node = layerx / 'node'
registry = layerx / 'registry-material'
identity = layerx / 'identity'
rotation_journal = identity / 'rotation.json'
migration_journal = identity / 'migration.json'
human = Path('/data/human-state')
history = human / 'trust-history'
projection = human / 'genesis-binding'
material = human / 'material'
DURABLE = [human / name for name in ('components', 'identity', 'security', 'movement', 'agent', 'authority', 'kms')]
KEYS = [keys / 'sequencer.key', keys / 'checkpoint-authority' / 'key.pem',
        keys / 'checkpoint-submitter' / 'deposit-authority.pem', keys / 'publication' / 'recipient.key']


def require(condition, message):
    if not condition:
        raise SystemExit('identity-rotation ' + scenario + ': ' + message)


def run(argv, expect=0):
    result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, text=True, env=ENV, timeout=120)
    if expect is not None:
        require((result.returncode == 0) == (expect == 0),
                ' '.join(argv[:2]) + ' exit ' + str(result.returncode) + ': ' + result.stdout + result.stderr)
    return result


def extract(source, *names):
    text = ''
    for name in names:
        found = (re.search('^' + name + r'\(\) \{ [^\n]*\}\n', source, re.M)
                 or re.search('^' + name + r'\(\) \{\n.*?^\}\n', source, re.M | re.S))
        require(found is not None, 'production function absent: ' + name)
        text += found.group(0)
    return text


PRELUDE = """set -euo pipefail
umask 077
layerx=/data/layerx
node_data=$layerx/node
keys=$layerx/keys
genesis=$layerx/genesis
human_state=/data/human-state
trust_history_file=$human_state/trust-history
run=/run/layerx
kernel_registry_material=$layerx/registry-material
network_id=$LAYERX_NODE_NETWORK_ID
"""


def init(source, body, expect=0):
    names = ['log', 'missing', 'fresh', 'kernel_registry_identity', 'human_genesis_project']
    if 'identity_generation() {' in source:
        names.insert(0, 'identity_generation')
    return run(['bash', '-c', PRELUDE + extract(source, *names) + body], expect)


def tool(script, *arguments, expect=0):
    return run(['bash', script, *arguments], expect)


def gate(expect=0):
    return run(['/usr/local/bin/kernel-init', '--identity-generation', 'gate'], expect)


def bind(source=INIT, expect=0):
    return init(source, 'kernel_registry_identity\nhuman_genesis_project\n', expect)


def restart():
    if os.path.lexists('/run/layerx'):
        shutil.rmtree('/run/layerx')
    run(['install', '-d', '-o', '4020', '-g', '4020', '-m', '0750', '/run/layerx', '/run/layerx/node'])


def layout():
    run(['bash', '-c', """set -euo pipefail
install -d -o 0 -g 4020 -m 2775 /data/layerx /data/layerx/settlement
install -d -o 0 -g 4020 -m 0750 /data/layerx/genesis /data/layerx/keys /data/layerx/keys/tokens
chmod g-s /data/layerx/genesis
install -d -o 0 -g 0 -m 0700 /data/layerx/keys/checkpoint-authority /data/layerx/keys/publication /data/layerx/keys/human-authority
install -d -o 4021 -g 4020 -m 0750 /data/layerx/keys/checkpoint-submitter
install -d -o 4020 -g 4020 -m 2770 /data/layerx/guarantor-submitter
install -d -o 4020 -g 4020 -m 0750 /data/layerx/node
install -d -o 4021 -g 4020 -m 0700 /data/layerx/core /data/layerx/agent-boundary /data/layerx/mirror
install -d -o 0 -g 4020 -m 0750 /data/human-state
install -d -o 4020 -g 4020 -m 0700 /data/human-state/components /data/human-state/identity /data/human-state/security /data/human-state/movement
install -d -o 4021 -g 4020 -m 0700 /data/human-state/agent /data/human-state/authority
install -d -o 4026 -g 4020 -m 0700 /data/human-state/kms
"""])
    init(INIT, 'fresh "$keys/treasury.key" 4020:4020 0400 openssl rand -hex 32\n'
               'fresh "$keys/tokens/program-token" 4020:4020 0440 openssl rand -hex 32\n')
    restart()


def seed():
    for directory, owner in ((human / 'components', 4020), (human / 'identity', 4020), (human / 'security', 4020),
                             (human / 'movement', 4020), (human / 'agent', 4021), (human / 'authority', 4021),
                             (human / 'kms', 4026)):
        path = directory / 'state.db'
        path.write_bytes(os.urandom(4096))
        os.chown(path, owner, 4020)
    (human / 'components' / 'custody').mkdir(mode=0o700)
    (human / 'components' / 'custody' / 'balances.journal').write_bytes(os.urandom(2048))
    (node / 'ledger').mkdir(mode=0o750)
    (node / 'ledger' / '000001.log').write_bytes(os.urandom(8192))
    (layerx / 'settlement' / 'checkpoint-settlement.json').write_bytes(os.urandom(512))


GENESIS = extract(Path(TOOL).read_text(), 'hex_public', 'recipient_address', 'public_values', 'asset_id',
                  'genesis_metadata', 'genesis_ids')


def make_genesis():
    run(['bash', '-c', PRELUDE + GENESIS + """treasury_public=$(hex_public "$(tr -d ' \\r\\n' <"$keys/treasury.key")")
public_values
records=()
for record in PAX:6 SID:18 USDC:6 USDL:6; do
    records+=("$(asset_id "${record%%:*}"):$record")
done
genesis_metadata "${records[@]}"
genesis_ids "$(asset_id PAX)" "$replica_id"
"""])


def role_material():
    run(['bash', '-c', """set -euo pipefail
umask 077
work=/data/human-state/material.new
install -d -o 0 -g 0 -m 0700 "$work" "$work/human" "$work/human/identity"
install -o 0 -g 0 -m 0600 /data/human-state/genesis-binding/replica-id "$work/receipt-authority-replica-id"
python3 MATERIAL --genesis-binding /data/human-state/genesis-binding >"$work/genesis-binding"
openssl rand -hex 32 >"$work/human/identity/custody-secret"
python3 MATERIAL --seal-material "$work"
mv "$work" /data/human-state/material
""".replace('MATERIAL', MATERIAL)])


def populate(legacy):
    layout()
    seed()
    tool(LEGACY_TOOL if legacy else TOOL, 'keys')
    make_genesis()
    bind(LEGACY_INIT if legacy else INIT)
    role_material()


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def snap(root='/data', exclude=()):
    result = {}
    for directory, names, files in os.walk(root):
        names[:] = [name for name in names if os.path.join(directory, name) not in exclude]
        for name in sorted(names + files):
            path = os.path.join(directory, name)
            if path in exclude:
                continue
            info = os.lstat(path)
            content = (os.readlink(path) if stat.S_ISLNK(info.st_mode)
                       else digest(path) if stat.S_ISREG(info.st_mode) else None)
            result[path] = (info.st_ino, info.st_uid, info.st_gid, info.st_mode, content)
    return result


def contents(root):
    return {path[len(str(root)):]: value[1:] for path, value in snap(str(root)).items()}


def plan():
    result = tool(TOOL, 'plan')
    lines = result.stdout.splitlines()
    require(len(lines) == 2 and lines[1].startswith('plan_sha256='), 'plan output shape: ' + result.stdout)
    sha = lines[1][len('plan_sha256='):]
    require(hashlib.sha256(lines[0].encode()).hexdigest() == sha, 'plan_sha256 is not the digest of the plan')
    return json.loads(lines[0]), sha


def record():
    return json.loads((identity / 'current.json').read_bytes())


def registry_generation():
    return json.loads((Path(os.path.realpath(registry / 'current')) / 'generation.json').read_bytes())['generation']


def refused(result, *fragments):
    for fragment in fragments:
        require(fragment in result.stderr, 'refusal lacks ' + repr(fragment) + ': ' + result.stderr)


def coherent(expected):
    restart()
    bind()
    gate()
    require(record()['generation'] == registry_generation() == expected, 'one coherent identity generation')
    run(['python3', MATERIAL, '--verify-material', str(material)])
    require(run(['python3', MATERIAL, '--genesis-binding', str(projection)]).stdout == (material / 'genesis-binding').read_text(),
            'the role material is bound to the projected genesis')
    require((material / 'receipt-authority-replica-id').read_bytes() == (genesis / 'replica-id').read_bytes(),
            'the role material replica id is the genesis replica id')
    value, _ = plan()
    require(value['verdict'] == 'compatible', 'post-migration plan verdict ' + value['verdict'])
    again = snap()
    restart()
    bind()
    gate()
    require(snap() == again, 'a compatible restart is idempotent')


if scenario == 'compatible':
    populate(True)
    require(not os.path.lexists(identity), 'the legacy init records no identity generation')
    before = snap()
    gate()
    require(record()['generation'] == registry_generation(), 'the recorded generation is the registry generation')
    recorded = snap(str(identity))
    restart()
    bind()
    gate()
    require(snap(exclude={str(identity)}) == before, 'a compatible restart changed persisted state')
    require(snap(str(identity)) == recorded, 'the identity record is idempotent')
    refused(tool(TOOL, 'keys', expect=1), 'exists; pass rotate')
    value, _ = plan()
    require(value['verdict'] == 'compatible', 'legacy compatible verdict ' + value['verdict'])
    require(snap(exclude={str(identity)}) == before and snap(str(identity)) == recorded, 'plan is read-only')
elif scenario == 'plan':
    populate(False)
    before = snap()
    value, sha = plan()
    require(value['schema'] == 'layerx.kernel.identity-plan.v1' and value['verdict'] == 'compatible', 'plan verdict')
    require(value['recorded']['generation'] == value['genesis']['generation'] == registry_generation(), 'plan generations')
    for name, path in (('trust-history', history), ('registry-material', registry),
                       ('receipt-authority-replica', projection), ('role-material', material)):
        entry = value['bindings'][name]
        require(entry['path'] == str(path) and entry['state'] == 'bound' and entry['compatible'], 'plan binding ' + name)
    require(value['bindings']['trust-history']['claims']['history_sha256'] == digest(history), 'trust history inventory')
    require(value['bindings']['authority-graph']['state'] == 'absent', 'absent authority graph')
    require(set(value['durable']) == {str(path) for path in DURABLE}, 'durable inventory')
    require(value['durable'][str(human / 'components')]['entries'] == 4
            and value['durable'][str(human / 'components')]['bytes'] == 6144, 'durable summary')
    rotation = value['disposition']['rotation']
    for path in (*KEYS, genesis / 'metadata.lxgb', genesis / 'replica-id', registry):
        require(str(path) in rotation['retire'], 'rotation retires ' + str(path))
    require(str(node) + '/*' in rotation['retire'], 'rotation retires the node data')
    for path in (keys / 'treasury.key', history, projection, material, *DURABLE, layerx / 'settlement'):
        require(str(path) in rotation['retain'], 'rotation retains ' + str(path))
    require(value['disposition']['migration']['archive'] == [] and value['disposition']['migration']['rebind'] == [],
            'a compatible plan migrates nothing')
    require(plan()[1] == sha and snap() == before, 'plan is stable and read-only')
elif scenario == 'inferred':
    populate(True)
    old = {path: Path(path).read_bytes() for path in (history, projection / 'replica-id', material / 'receipt-authority-replica-id')}
    secret = (material / 'human' / 'identity' / 'custody-secret').read_bytes()
    old_generation = registry_generation()
    old_material = contents(material)
    durable = {path: snap(str(path)) for path in DURABLE}
    tool(LEGACY_TOOL, 'keys', 'rotate')
    require(os.listdir(node) == [], 'the legacy rotation deletes the node data')
    require(all(os.path.lexists(path) for path in (history, projection, material, registry)),
            'the legacy rotation leaves the old Human bindings in place')
    make_genesis()
    restart()
    refused(bind(LEGACY_INIT, expect=1), 'differs')
    refused(gate(expect=1), 'identity generation refused (inferred-mismatch)', 'inferred old-genesis binding: trust-history')
    before = snap()
    refused(bind(expect=1), 'inferred-mismatch')
    refused(init(INIT, 'human_genesis_project\nidentity_generation gate\n', expect=1), 'retained projection bytes differ')
    refused(tool(TOOL, 'keys', 'rotate', expect=1), 'rotation preflight refused (inferred-mismatch)')
    value, sha = plan()
    require(value['verdict'] == 'inferred-mismatch', 'inferred verdict ' + value['verdict'])
    require(value['disposition']['migration']['archive'] == [str(history), str(registry), str(projection)]
            and value['disposition']['migration']['rebind'] == [str(material)], 'inferred migration disposition')
    refused(tool(TOOL, 'migrate', '0' * 64, expect=1), 'is not the current plan')
    require(snap() == before, 'refusals change nothing')
    tool(TOOL, 'migrate', sha)
    archive = identity / 'generations' / old_generation
    files = archive / 'files'
    require((files / 'human-state' / 'trust-history').read_bytes() == old[history], 'old trust history retained byte for byte')
    require((files / 'human-state' / 'genesis-binding' / 'replica-id').read_bytes() == old[projection / 'replica-id'], 'old projection retained')
    require(contents(files / 'human-state' / 'material') == old_material, 'old role material retained')
    require(json.loads((Path(os.path.realpath(files / 'layerx' / 'registry-material' / 'current')) / 'generation.json')
                       .read_bytes())['generation'] == old_generation, 'old registry generation retained')
    require((archive / ('migration-' + value['genesis']['generation'] + '.json')).exists(), 'migration record')
    require(not os.path.lexists(migration_journal), 'migration journal closed')
    coherent(value['genesis']['generation'])
    require(history.read_bytes() != old[history], 'the new trust history is produced from the new genesis')
    require((material / 'human' / 'identity' / 'custody-secret').read_bytes() == secret, 'custody material preserved')
    require({path: snap(str(path)) for path in DURABLE} == durable, 'durable Human state untouched')
    require(old_generation in plan()[0]['retained_generations'], 'retained generation listed')
elif scenario == 'rotation':
    populate(False)
    old_generation = record()['generation']
    old_keys = {path: path.read_bytes() for path in KEYS}
    old_genesis = (genesis / 'metadata.lxgb').read_bytes()
    old_history = history.read_bytes()
    old_node = contents(node)
    durable = {path: snap(str(path)) for path in DURABLE}
    settlement = snap(str(layerx / 'settlement'))
    tool(TOOL, 'keys', 'rotate')
    archive = identity / 'generations' / old_generation
    files = archive / 'files'
    require(not os.path.lexists(rotation_journal), 'rotation journal closed')
    require(json.loads((archive / 'rotation.json').read_bytes())['phase'] == 'complete', 'rotation record complete')
    for path in KEYS:
        require((files / path.relative_to('/data')).read_bytes() == old_keys[path], 'retired key retained: ' + str(path))
        require(path.read_bytes() != old_keys[path], 'rotated key replaced: ' + str(path))
    require((files / 'layerx' / 'genesis' / 'metadata.lxgb').read_bytes() == old_genesis, 'retired genesis retained')
    require(os.listdir(node) == [] and contents(files / 'layerx' / 'node') == old_node, 'node data retained byte for byte')
    require(not os.path.lexists(registry) and os.path.lexists(files / 'layerx' / 'registry-material'), 'registry retired')
    require(history.read_bytes() == old_history and projection.is_dir() and material.is_dir(), 'Human bindings kept')
    refused(gate(expect=1), 'identity generation refused (genesis-incomplete)')
    make_genesis()
    restart()
    refused(gate(expect=1), 'identity generation refused (migration-required)', old_generation)
    refused(bind(expect=1), 'migration-required')
    value, sha = plan()
    require(value['verdict'] == 'migration-required', 'rotation verdict ' + value['verdict'])
    require(value['disposition']['migration']['archive'] == [str(history), str(projection)]
            and value['disposition']['migration']['rebind'] == [str(material)], 'rotation migration disposition')
    tool(TOOL, 'migrate', sha)
    require((files / 'human-state' / 'trust-history').read_bytes() == old_history, 'trust history retained')
    coherent(value['genesis']['generation'])
    refused(tool(TOOL, 'migrate', sha, expect=1), 'is not the current plan')
    refused(tool(TOOL, 'migrate', plan()[1], expect=1), 'no migration applies (compatible)')
    require({path: snap(str(path)) for path in DURABLE} == durable, 'durable Human state untouched')
    require(snap(str(layerx / 'settlement')) == settlement, 'settlement state untouched')
elif scenario == 'interrupted':
    populate(False)
    old_generation = record()['generation']
    old_sequencer = (keys / 'sequencer.key').read_bytes()
    durable = {path: snap(str(path)) for path in DURABLE}
    busy = node / 'zz-mount'
    busy.mkdir()
    run(['mount', '-t', 'tmpfs', '-o', 'mode=0700', 'tmpfs', str(busy)])
    tool(TOOL, 'keys', 'rotate', expect=1)
    require(json.loads(rotation_journal.read_bytes())['phase'] == 'retiring', 'interrupted rotation journal')
    require(not os.path.lexists(keys / 'sequencer.key'), 'the interruption fell inside the retirement')
    refused(gate(expect=1), 'identity generation refused (rotation-in-progress)')
    refused(bind(expect=1), 'rotation-in-progress')
    refused(tool(TOOL, 'keys', expect=1), 'identity rotation is pending')
    refused(tool(TOOL, 'genesis', expect=1), 'identity rotation is pending')
    require(plan()[0]['verdict'] == 'rotation-in-progress', 'plan names the pending rotation')
    run(['umount', str(busy)])
    tool(TOOL, 'keys', 'rotate')
    archive = identity / 'generations' / old_generation
    require(not os.path.lexists(rotation_journal), 'resumed rotation closed')
    require((archive / 'files' / 'layerx' / 'keys' / 'sequencer.key').read_bytes() == old_sequencer, 'one retained generation')
    require((archive / 'files' / 'layerx' / 'node' / 'zz-mount').is_dir() and os.listdir(node) == [], 'node data retired once')
    for entry in json.loads((archive / 'manifest.json').read_bytes())['entries']:
        require(os.path.lexists(entry['target']) and (Path(entry['source']).is_relative_to(keys) or not os.path.lexists(entry['source'])),
                'manifest entry ' + entry['source'])
    require(sorted(os.listdir(identity / 'generations')) == [old_generation], 'one retained generation directory')
    make_genesis()
    restart()
    value, sha = plan()
    require(value['verdict'] == 'migration-required', 'post-rotation verdict ' + value['verdict'])
    run(['mount', '--bind', str(projection), str(projection)])
    tool(TOOL, 'migrate', sha, expect=1)
    require(os.path.lexists(migration_journal) and not os.path.lexists(history), 'interrupted migration journal')
    refused(gate(expect=1), 'identity generation refused (migration-in-progress)')
    refused(tool(TOOL, 'keys', 'rotate', expect=1), 'identity migration is pending')
    refused(tool(TOOL, 'migrate', 'f' * 64, expect=1), 'pending migration was authorized by plan ' + sha)
    run(['umount', str(projection)])
    tool(TOOL, 'migrate', sha)
    coherent(value['genesis']['generation'])
    require({path: snap(str(path)) for path in DURABLE} == durable, 'durable Human state untouched')
elif scenario == 'inconsistent':
    populate(False)
    (projection / 'replica-id').write_bytes(b'0' * 64 + b'\n')
    before = snap()
    value, sha = plan()
    require(value['verdict'] == 'inconsistent', 'tampered verdict ' + value['verdict'])
    require(any('receipt-authority-replica replica_sha256' in line for line in value['refusals']), 'precise inconsistency')
    refused(gate(expect=1), 'identity generation refused (inconsistent)', 'receipt-authority-replica replica_sha256')
    refused(tool(TOOL, 'keys', 'rotate', expect=1), 'rotation preflight refused (inconsistent)')
    refused(tool(TOOL, 'migrate', sha, expect=1), 'no migration applies (inconsistent)')
    require(snap() == before, 'refusals change nothing')
else:
    raise SystemExit('unknown identity-rotation scenario ' + scenario)
print('identity-rotation passed ' + scenario, flush=True)
'''


def movement_kms():
    """Requirement 202 against the real Human KMS and movement roles: the
    identities are issued through the CA catalog rows and the signer of
    tools/bringup/ca.sh, the KMS material is validated and projected by the
    kernel's identity generation gate and human_kms_prepare, and the KMS and
    movement run from the prebuilt executables of the movement KMS assembly
    gate under their kernel uids, mount namespaces and loopback addresses."""
    import base64
    import importlib.util
    import shlex
    sys.dont_write_bytecode = True
    os.umask(0o077)
    specification = importlib.util.spec_from_file_location(
        'human_movement_kms_assembly', ROOT / 'tools/qualification/paxeer-x/human-movement-kms-assembly.py')
    assembly = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(assembly)
    require, refuse, digest = assembly.require, assembly.refuse, assembly.digest
    shell_function, init_assignment, service_block = assembly.shell_function, assembly.init_assignment, assembly.service_block
    INIT, CA = assembly.INIT, assembly.CA
    quote = shlex.quote
    chain_id = 125

    class MovementKms(assembly.Gate):
        def kms_prepare(self, state=None, tls=None):
            """human_kms_prepare of kernel init, behind the kernel identity
            generation gate it runs first."""
            script = '\n'.join([
                'mount --bind %s /usr/local/lib' % quote(str(self.libdir)),
                self.init_globals(state, tls), 'layerx=' + quote(str(self.layerx)),
                init_assignment('node_data'), init_assignment('keys'), init_assignment('trust_history_file'),
                shell_function(INIT, 'identity_generation'), shell_function(INIT, 'human_kms_prepare'),
                'human_kms_prepare'])
            return self.run(['unshare', '--mount', '--propagation', 'private', '--', 'bash', '-euo', 'pipefail', '-c', script],
                            check=False, timeout=60)

        def kernel_genesis(self):
            """The volume's kernel genesis the identity generation gate binds:
            the sequencer seed under keys, the genesis metadata, asset id and
            the authority replica id derived from the sequencer key."""
            self.layerx = self.mkdir('layerx', 0, 0, 0o700)
            keys = self.mkdir('keys', 0, 0, 0o700, self.layerx)
            seed = os.urandom(32).hex()
            self.material.write_bytes(keys / 'sequencer.key', seed.encode())
            self.sequencer_seed = bytes.fromhex(seed)
            der = self.run(['openssl', 'pkey', '-inform', 'DER', '-pubout', '-outform', 'DER'],
                           stdin=bytes.fromhex('302e020100300506032b657004220420') + self.sequencer_seed).stdout
            require(len(der) == 44, 'sequencer public key derivation')
            public = der[12:].hex()
            self.material.write_bytes(self.genesis / 'metadata.lxgb', b'LXGB isolated movement-kms genesis ' + os.urandom(32).hex().encode())
            self.material.write_bytes(self.genesis / 'replica-id',
                                      hashlib.sha256(('layerx-authority-replica:' + public).encode()).hexdigest().encode())

        def role_material(self):
            """$human_state/material as kernel init seals it: the receipt
            authority replica id, the genesis binding and the role inputs."""
            material = self.state / 'material'
            self.material.write_bytes(material / 'receipt-authority-replica-id', (self.genesis / 'replica-id').read_bytes())
            binding = self.material.genesis_binding(self.genesis)
            self.material.write_bytes(material / 'genesis-binding', (json.dumps(binding, sort_keys=True) + '\n').encode())
            self.material.seal_material(material)

        def presented(self, label, source):
            """A client directory presenting the source's certificate and key
            while trusting the Human KMS server's CA."""
            directory = self.issued / label
            directory.mkdir(mode=0o700)
            for name in ('cert.pem', 'key.pem', 'cert.der', 'key.der'):
                shutil.copyfile(self.issued / source / name, directory / name)
            for name in ('ca.pem', 'ca.der'):
                shutil.copyfile(self.issued / 'human-kms' / name, directory / name)
            return label

        def frame(self, version, operation, binding, reference=b'', payload=None, extra=b'', provider=None):
            provider = provider or self.material.HUMAN_KMS_PROVIDER.encode()
            out = (b'LXKP' + version.to_bytes(2, 'big') + bytes([operation]) + len(provider).to_bytes(4, 'big') + provider
                   + binding + self.network.to_bytes(4, 'big') + b'\x01' + len(reference).to_bytes(4, 'big') + reference + extra)
            if payload is not None:
                out += len(payload).to_bytes(4, 'big') + payload
            return out

        def answer(self, frame, identity, version, operation, status=0):
            response = self.kms_call(frame, identity)
            require(response is not None and response[:8] == b'LXKP' + version.to_bytes(2, 'big') + bytes([operation, status]),
                    'KMS operation %d.%d as %s answered %r, not status %d' % (
                        version, operation, identity, None if response is None else response[:8], status))
            self.responses.append(response)
            return response[8:]

        def authorize(self, key, nonce):
            now = int(time.time())
            plan = {'plan_id': list(os.urandom(32)), 'action_key': list(key), 'tenant': 'paxeer-x',
                    'principal': 'movement-kms', 'binding_digest': list(self.sign_binding), 'wallet': list(self.address),
                    'not_before': now - 300, 'not_after': now + 3600,
                    'transaction': {'chain_id': chain_id, 'nonce': nonce, 'max_priority_fee_per_gas': 10**9,
                                    'max_fee_per_gas': 100 * 10**9, 'gas_limit': 21000, 'to': list(b'\x0d' * 20),
                                    'value': list((nonce + 1).to_bytes(32, 'big')), 'calldata': []}}
            action = json.loads(self.answer(self.frame(3, 7, self.sign_binding, self.sign_reference, json.dumps(plan).encode()),
                                            'human-kms-client', 3, 7))
            require(action['raw_transaction'] == [] and action['transaction_hash'] is None, 'authorization signed by itself')
            return plan

        def executor_sign(self, key):
            action = json.loads(self.answer(self.frame(3, 8, self.sign_binding, self.sign_reference, json.dumps(list(key)).encode()),
                                            'human-kms-executor', 3, 8))
            raw = bytes(action['raw_transaction'])
            require(raw[:1] == b'\x02' and bytes(action['transaction_hash']) == bytes.fromhex(
                self.rpc('web3_sha3', ['0x' + raw.hex()])[2:]), 'executor signature is not a type-2 transaction with its hash')
            return raw, bytes(action['transaction_hash'])

        def rpc(self, method, params):
            client = '''import json,sys,urllib.request
request=urllib.request.Request('http://127.0.0.1:18545',data=json.dumps({'jsonrpc':'2.0','id':1,'method':sys.argv[1],'params':json.loads(sys.argv[2])}).encode(),headers={'content-type':'application/json'})
print(urllib.request.urlopen(request,timeout=10).read().decode())
'''
            reply = json.loads(self.run(self.net('python3', '-c', client, method, json.dumps(params))).stdout)
            require('error' not in reply, 'chain %s refused: %s' % (method, reply.get('error')))
            return reply['result']

        def broadcast(self, raw, transaction_hash, nonce):
            require(self.rpc('eth_sendRawTransaction', ['0x' + raw.hex()]) == '0x' + transaction_hash.hex(),
                    'the chain derived another hash for the executor-signed transaction')
            mined = self.rpc('eth_getTransactionByHash', ['0x' + transaction_hash.hex()])
            require(mined is not None and mined['from'].lower() == '0x' + self.address.hex()
                    and int(mined['nonce'], 16) == nonce and int(mined['chainId'], 16) == chain_id,
                    'the chain did not recover the KMS custody wallet as the signer')

        def refused_movement(self, label, directory):
            """The movement role with a wrong trust relationship either refuses
            to start or binds and never reports ready."""
            state = self.mkdir('movement-' + label, 4020, 4020, 0o700)
            self.mkdir('movement', 4020, 4020, 0o700, state)
            self.mkdir('evidence', 4020, 4020, 0o700, state)
            name = 'movement-' + label
            process = self.start_movement(name, directory, state)
            deadline = time.monotonic() + 30
            while not (self.sockets / 'movement.sock').exists():
                if process.poll() is not None:
                    require(process.returncode != 0, label + ' movement exited successfully')
                    self.processes.pop(name)
                    return
                require(time.monotonic() < deadline, label + ' movement neither bound nor exited')
                time.sleep(0.2)
            for _ in range(8):
                require(not self.ready(), 'movement reported ready with ' + label)
                time.sleep(1)
            self.stop(name)

        def execute(self):
            self.responses = []
            self.work = self.mkdir('work', 0, 0, 0o700)
            self.profile_path = ROOT / 'tests/fixtures/custody/native-credit-receipt/profile'
            self.profile = self.profile_path.read_bytes()
            require(len(self.profile) == 223 and self.profile[:5] == b'LXBC3' and int.from_bytes(self.profile[5:13], 'big') == chain_id,
                    'the protocol-3 custody profile fixture is not a Paxeer custody profile')
            self.network = int.from_bytes(self.profile[201:205], 'big')
            self.asset = self.profile[97:129].hex()
            self.binding = os.urandom(32)
            self.sign_binding = os.urandom(32)
            key = self.work / 'checkpoint-authority.pem'
            self.run(['openssl', 'genpkey', '-algorithm', 'ed25519', '-out', str(key)])
            self.checkpoint_authority = '0x' + self.run(['openssl', 'pkey', '-in', str(key), '-pubout', '-outform', 'DER']).stdout[-32:].hex()
            self.registry = self.work / 'module-registry.json'
            self.registry.write_text(json.dumps({'schema_version': 2, 'assets': [{'asset': self.asset}],
                                                 'modules': [{'module': 1, 'ordinals': [1, 2]}]}))
            self.libdir = self.mkdir('lib', 0, 0, 0o755)
            shutil.copytree(ROOT / 'platform/hosted/human', self.libdir / 'layerx-human')
            self.kernel_layout()
            self.kernel_genesis()
            inventory = {'revision': self.revision, 'unmerged_branches': self.unmerged(), 'before': self.inventory('before-rollout')}
            self.material.write_bytes(self.evidence / 'inventory.json', json.dumps(inventory, indent=2).encode())
            self.case('inventory-before-rollout-from-actual-state')

            # ac_1/ac_2: the exact identities through the catalog rows and the CA signer.
            self.ca_cases()
            listed = self.run(['bash', str(CA), 'services']).stdout.decode().splitlines()
            rows = {line.split()[0]: line.split() for line in listed if line.split()}
            usages = {'serverAuth': 'TLS Web Server Authentication', 'clientAuth': 'TLS Web Client Authentication'}
            require({service: (rows[service][4], usages[rows[service][5]]) for service in assembly.IDENTITIES}
                    == self.material.HUMAN_KMS_IDENTITIES, 'the KMS material identities differ from the CA catalog rows')
            self.case('kms-material-identities-are-the-ca-catalog-rows')
            attestor_ca = self.authority('attestor-ca')
            self.issue(attestor_ca, 'human-attestor-client', rows['human-attestor-client'], self.issued / 'xweb-attestor')
            self.issue(self.ca, 'human-attestor-client', rows['human-attestor-client'], self.issued / 'attestor-under-internal-ca')
            self.issue(self.ca, 'gateway-client', rows['gateway-client'], self.issued / 'wallet-gateway')
            self.issue(self.ca, 'agentd-client', rows['agentd-client'], self.issued / 'owner-agent')
            for label in ('xweb-attestor', 'wallet-gateway', 'owner-agent'):
                for service in ('human-kms-client', 'human-kms-executor'):
                    require(not self.usage_matches(self.ca, service, self.issued / label / 'cert.pem'),
                            '%s was admitted as the %s row' % (label, service))
            alias = self.work / 'internal-ca-copy'
            shutil.copytree(self.ca, alias)
            row_ca = '\n'.join([shell_function(CA, 'row_ca_dir'), 'attestor_services="human-attestor-client"',
                                'ca_dir=' + quote(str(self.ca)), 'LAYERX_ATTESTOR_CA_DIR="$1" row_ca_dir "$2"'])
            def row_ca_dir(attestor, service):
                result = self.run(['bash', '-euo', 'pipefail', '-c', row_ca, 'row-ca', str(attestor), service], check=False)
                return result.stdout.decode() if result.returncode == 0 else None
            require(row_ca_dir(attestor_ca, 'human-attestor-client') == str(attestor_ca)
                    and row_ca_dir(attestor_ca, 'human-kms-client') == str(self.ca),
                    'the catalog does not sign attestor and Human KMS clients under their own authorities')
            for same in (self.ca, alias, str(self.ca) + '/.'):
                require(row_ca_dir(same, 'human-attestor-client') is None,
                        'the attestors\' gateway CA was admitted as the internal CA at %s' % same)
            self.case('xweb-attestor-and-wallet-identities-never-kms-authority')

            # ac_2/ac_3: only validated material for the exact roles is admitted.
            self.material_refusals()
            scratch = self.mkdir('identity-refusals', 0, 0, 0o700, self.work)
            refusals = [('xweb-attestor-as-service-client', 'human-kms-client', 'xweb-attestor', ('cert.der',)),
                        ('wallet-gateway-as-service-client', 'human-kms-client', 'wallet-gateway', ('cert.der',)),
                        ('owner-agent-as-executor', 'human-kms-executor', 'owner-agent', ('cert.der',)),
                        ('foreign-ca-executor-leaf', 'human-kms-executor', 'foreign-executor', ('cert.der',)),
                        ('executor-as-server', 'human-kms', 'human-kms-executor', ('cert.der', 'key.der')),
                        ('server-without-kms-server-name', 'human-kms', 'nameless-server', ('cert.der', 'key.der')),
                        ('server-key-not-certified', 'human-kms', 'human-kms-client', ('key.der',))]
            for label, slot, source, names in refusals:
                case = self.mkdir(label, 0, 0, 0o700, scratch)
                tls = case / 'tls'
                shutil.copytree(self.tls, tls)
                for name in names:
                    shutil.copyfile(self.issued / source / name, tls / slot / name)
                state = self.mkdir('state', 0, 0, 0o700, case)
                out = self.mkdir('kms-prerequisite', 0, 0, 0o700, case) / 'material'
                try:
                    self.material.kms_prerequisite(self.registry, tls, out, self.network, self.asset, state)
                except (ValueError, OSError):
                    require(not out.exists() and not any(out.parent.iterdir()), label + ' left material while refusing')
                else:
                    refuse(label + ' KMS material was admitted')
            self.case('kms-material-refuses-wrong-role-wrong-ca-and-wrong-server')

            endpoint = init_assignment('human_kms_listen').split('=', 1)[1]
            server_name = init_assignment('human_kms_server_name').split('=', 1)[1]
            provider = init_assignment('human_kms_provider').split('=', 1)[1]
            require((endpoint, server_name, provider) == (self.material.HUMAN_KMS_ENDPOINT, self.material.HUMAN_KMS_SERVER_NAME,
                                                          self.material.HUMAN_KMS_PROVIDER)
                    and self.material.movement_defaults(self.network, chain_id)['KMS_ENDPOINT'] == endpoint
                    and self.material.movement_defaults(self.network, chain_id)['KMS_SERVER_NAME'] == server_name
                    and self.material.movement_defaults(self.network, chain_id)['KMS_PROVIDER_REFERENCE'] == provider
                    and self.material.component_defaults(self.network, chain_id)['KMS_ENDPOINT'] == endpoint,
                    'kernel KMS listener, movement endpoint, server name and provider differ')
            kms = service_block('human-kms').group(2)
            require(kms.split('\n')[0].count('$tls/human-kms') == 7 and '\thuman_kms_prepare - -- env' in kms,
                    'the KMS does not wait for every identity and its validated preparation')
            waits = service_block('human-movement').group(2).split('\n')[0]
            for needed in ('$tls/human-kms/cert.der', '$tls/human-kms-executor/cert.der', '$tls/human-kms-executor/key.der',
                           '$tls/human-kms-executor/ca.der', '$human_kms_out/kms-seal', '$human_kms_out/registry.json',
                           '$human_kms_out/kms-executor.der'):
                require(needed in waits, 'movement does not wait for ' + needed)
            self.case('kms-and-movement-start-only-after-identities-and-validated-material')

            self.holder = subprocess.Popen(['unshare', '--net', '--', 'sleep', '3600'], stdin=subprocess.DEVNULL)
            deadline = time.monotonic() + 10
            while os.readlink('/proc/%d/ns/net' % self.holder.pid) == os.readlink('/proc/self/ns/net'):
                require(self.holder.poll() is None and time.monotonic() < deadline, 'the private network namespace did not start')
                time.sleep(0.05)
            self.run(self.net('ip', 'link', 'set', 'lo', 'up'))
            self.chain()

            human_out = self.state / 'material/human'
            human_out.mkdir(mode=0o700, parents=True)
            config, directory = self.movement_material('assembly', 'human-kms-executor')
            shutil.copytree(config, human_out / 'movement-config')
            self.role_material()
            require(not self.binding_check(self.state), 'movement binding admitted without the KMS material')
            _, absent = self.movement_material('absent', None)
            process = self.start_movement('movement-absent', absent, self.state / 'movement')
            try:
                process.wait(timeout=30)
            except subprocess.TimeoutExpired:
                refuse('movement started without its KMS executor material')
            require(process.returncode != 0 and not (self.sockets / 'movement.sock').exists(),
                    'movement served without its KMS executor material')
            self.processes.pop('movement-absent')
            wrong = self.work / 'wrong-role-tls'
            shutil.copytree(self.tls, wrong)
            shutil.copyfile(self.issued / 'wallet-gateway/cert.der', wrong / 'human-kms-client/cert.der')
            refused = self.kms_prepare(tls=wrong)
            require(refused.returncode != 0 and not (self.state / 'kms-prerequisite/material').exists()
                    and not (self.human_material / 'human-kms').exists() and not any((self.state / 'kms').iterdir()),
                    'kernel KMS preparation admitted a wallet identity as its service client')
            self.case('kernel-kms-prepare-refuses-unvalidated-material-and-movement-waits')

            prepared = self.kms_prepare()
            require(prepared.returncode == 0, 'kernel human_kms_prepare failed: ' + prepared.stderr.decode(errors='replace')[-2000:])
            record = json.loads((self.layerx / 'identity/current.json').read_bytes())
            require(record['network_id'] == self.network and record['asset_id'] == self.asset, 'identity generation record')
            kms_out = self.state / 'kms-prerequisite/material'
            seal = digest(kms_out / 'kms-seal')
            projected = self.human_material / 'human-kms'
            info = projected.stat()
            require((info.st_uid, info.st_gid, info.st_mode & 0o777) == (4026, 4020, 0o500), 'KMS projection directory ownership')
            for path in projected.iterdir():
                info = path.stat()
                require((info.st_uid, info.st_gid, info.st_mode & 0o777) == (0, 4020, 0o440), 'KMS projected file ownership ' + path.name)
            require(sorted(p.name for p in projected.iterdir()) == sorted(['kms-server.der', 'kms-server-key.der', 'kms-client.der',
                                                                         'kms-executor.der', 'ca.der', 'kms-seal', 'registry.json'])
                    and (projected / 'kms-executor.der').read_bytes() == (self.issued / 'human-kms-executor/cert.der').read_bytes()
                    and (projected / 'kms-client.der').read_bytes() == (self.issued / 'human-kms-client/cert.der').read_bytes()
                    and (projected / 'kms-server.der').read_bytes() == (self.issued / 'human-kms/cert.der').read_bytes(),
                    'KMS projection does not pin exactly the issued identities')
            movement_files = sorted(p.name for p in directory.iterdir())
            require(movement_files == ['ca.der', 'custody.profile', 'env', 'kms-executor-key.der', 'kms-executor.der'],
                    'movement projection holds other material: %s' % movement_files)
            require(self.binding_check(self.state), 'kernel movement binding refused the exact KMS service')
            for label, path, value in [('server-name', 'LAYERX_HUMAN_MOVEMENT_PROVIDER_KMS_SERVER_NAME', 'wrong-server'),
                                       ('endpoint', 'LAYERX_HUMAN_MOVEMENT_PROVIDER_KMS_ENDPOINT', '127.0.0.1:9451'),
                                       ('signer-policy', 'LAYERX_HUMAN_MOVEMENT_PROVIDER_KMS_PROVIDER_REFERENCE', 'foreign-provider'),
                                       ('trust-root', 'LAYERX_HUMAN_MOVEMENT_PROVIDER_KMS_CA_DER', '/run/human-private/movement/foreign-ca.der')]:
                target = human_out / 'movement-config' / path
                original = target.read_bytes()
                target.write_text(value)
                require(not self.binding_check(self.state), 'movement binding admitted a wrong KMS ' + label)
                target.write_bytes(original)
            self.material.verify_material(self.state / 'material')
            swapped = self.work / 'swapped-tls'
            shutil.copytree(self.tls, swapped)
            shutil.copyfile(self.issued / 'human-kms-client/cert.der', swapped / 'human-kms-executor/cert.der')
            require(not self.binding_check(self.state, swapped), 'movement binding admitted the service identity as executor')
            self.case('kernel-kms-material-projection-and-movement-binding')

            movement_state = self.state / 'movement'
            self.bash('\n'.join([self.init_globals(), shell_function(INIT, 'human_movement_state'), 'human_movement_state']))
            info = (movement_state / 'movement').stat()
            require((info.st_uid, info.st_gid, info.st_mode & 0o777) == (4020, 4020, 0o700), 'movement journal root ownership')
            movement = self.start_movement('movement', directory, movement_state)
            deadline = time.monotonic() + 30
            while not (self.sockets / 'movement.sock').exists():
                require(movement.poll() is None, 'movement exited before binding; log %s' % (self.logs / 'movement.log'))
                require(time.monotonic() < deadline, 'movement never bound its socket')
                time.sleep(0.2)
            require(not self.ready(), 'movement reported ready while its KMS service is absent')
            self.start_kms()
            self.case('kernel-kms-entrypoint-runtime-clock-uid-4026-startup')
            require(self.ready_within(movement, 30), 'movement did not become ready with its authenticated KMS service')
            self.case('movement-readiness-follows-the-authenticated-kms')

            # ac_2/ac_4: signer policy at the real KMS.
            created = self.answer(self.frame(1, 1, self.sign_binding), 'human-kms-client', 1, 1)
            require(len(created) == 101 and created[:4] == b'\x00\x00\x00\x20', 'service key creation answer')
            self.sign_reference, public = created[4:36], created[36:68]
            self.address = self.answer(self.frame(3, 6, self.sign_binding, self.sign_reference), 'human-kms-client', 3, 6)
            require(len(self.address) == 20 and any(self.address), 'custody wallet address')
            require(self.answer(self.frame(3, 6, self.sign_binding, self.sign_reference), 'human-kms-executor', 3, 6) == self.address,
                    'the executor sees another custody wallet')
            first, second, stranger = os.urandom(32), os.urandom(32), os.urandom(32)
            self.authorize(first, 0)
            self.authorize(second, 1)
            self.answer(self.frame(3, 8, self.sign_binding, self.sign_reference, json.dumps(list(stranger)).encode()),
                        'human-kms-executor', 3, 8, status=2)
            plan = {'plan_id': list(os.urandom(32)), 'action_key': list(stranger), 'tenant': 'paxeer-x', 'principal': 'movement-kms',
                    'binding_digest': list(self.sign_binding), 'wallet': list(self.address), 'not_before': int(time.time()) - 300,
                    'not_after': int(time.time()) + 3600,
                    'transaction': {'chain_id': chain_id, 'nonce': 2, 'max_priority_fee_per_gas': 10**9, 'max_fee_per_gas': 100 * 10**9,
                                    'gas_limit': 21000, 'to': list(b'\x0d' * 20), 'value': list((9).to_bytes(32, 'big')), 'calldata': []}}
            self.answer(self.frame(3, 7, self.sign_binding, self.sign_reference, json.dumps(plan).encode()), 'human-kms-executor', 3, 7, status=1)
            self.answer(self.frame(1, 1, os.urandom(32)), 'human-kms-executor', 1, 1, status=1)
            self.answer(self.frame(1, 5, self.sign_binding, self.sign_reference,
                                   extra=os.urandom(32) + b'\x00\x00\x00\x01\x01\x00\x00\x00\x01\x01'), 'human-kms-executor', 1, 5, status=1)
            self.answer(self.frame(5, 14, self.sign_binding, self.sign_reference), 'human-kms-executor', 5, 14, status=1)
            self.answer(self.frame(3, 11, self.sign_binding, self.sign_reference, b'{}'), 'human-kms-executor', 3, 11, status=1)
            self.answer(self.frame(3, 8, self.sign_binding, self.sign_reference, json.dumps(list(first)).encode(),
                                   provider=b'foreign-provider'), 'human-kms-executor', 3, 8, status=1)
            self.case('executor-refused-authorization-service-signing-export-and-foreign-provider')
            self.rpc('anvil_setBalance', ['0x' + self.address.hex(), hex(10**18)])
            raw_first, hash_first = self.executor_sign(first)
            self.broadcast(raw_first, hash_first, 0)
            self.case('executor-signs-only-service-authorized-actions-recovered-on-chain')

            for label in ('xweb-attestor', 'wallet-gateway', 'owner-agent', 'attestor-under-internal-ca', 'foreign-executor'):
                presented = self.presented('presented-' + label, label)
                require(self.kms_call(self.request(0), presented, check=False) is None,
                        'the KMS admitted the %s identity' % label)
            self.case('kms-process-refuses-wallet-xweb-owner-and-foreign-ca-clients')

            self.stop('movement')
            for label, executor, key, value in [('service-identity-executor', 'human-kms-client', None, None),
                                                ('foreign-ca-executor', 'foreign-executor', None, None),
                                                ('xweb-attestor-executor', 'xweb-attestor', None, None),
                                                ('wrong-server-name', 'human-kms-executor', 'KMS_SERVER_NAME', 'wrong-server'),
                                                ('wrong-signer-policy', 'human-kms-executor', 'KMS_PROVIDER_REFERENCE', 'foreign-provider')]:
                _, variant = self.movement_material(label, executor)
                if key is not None:
                    target = variant / 'env' / ('LAYERX_HUMAN_MOVEMENT_PROVIDER_' + key)
                    target.chmod(0o640)
                    target.write_text(value)
                    target.chmod(0o440)
                self.refused_movement(label, variant)
            self.case('movement-refuses-wrong-ca-role-server-name-and-signer-policy')
            movement = self.start_movement('movement', directory, movement_state)
            require(self.ready_within(movement, 30), 'movement did not return to ready with its exact identity')

            state_files = sorted(p.name for p in (self.state / 'kms').iterdir())
            self.stop('kms')
            require(not self.ready(), 'movement stayed ready after its KMS service stopped')
            require(movement.poll() is None, 'movement exited instead of reporting unready')
            self.case('movement-unready-on-kms-loss')
            prepared = self.kms_prepare()
            require(prepared.returncode == 0, 'retained KMS material was refused on restart: ' + prepared.stderr.decode(errors='replace')[-2000:])
            require(digest(kms_out / 'kms-seal') == seal, 'restart replaced the retained KMS seal')
            self.start_kms()
            described = self.answer(self.frame(1, 2, self.sign_binding, self.sign_reference), 'human-kms-client', 1, 2)
            require(described[:68] == created[:68], 'restart did not recover the retained custody key')
            require(sorted(p.name for p in (self.state / 'kms').iterdir()) == state_files, 'restart generated replacement KMS state')
            require(self.ready_within(movement, 30), 'movement did not recover readiness after the KMS restart')
            raw_again, hash_again = self.executor_sign(first)
            require((raw_again, hash_again) == (raw_first, hash_first), 'restart re-signed an already signed action')
            raw_second, hash_second = self.executor_sign(second)
            self.broadcast(raw_second, hash_second, 1)
            self.answer(self.frame(3, 7, self.sign_binding, self.sign_reference, json.dumps(plan).encode()), 'human-kms-executor', 3, 7, status=1)
            self.case('restart-resumes-authorized-signing-with-retained-keys-and-readiness')

            orphan = self.mkdir('orphan-state', 0, 4020, 0o750)
            shutil.copytree(self.state / 'kms', orphan / 'kms', symlinks=True)
            shutil.copytree(self.state / 'material', orphan / 'material')
            os.chown(orphan / 'kms', 4026, 4020)
            refused = self.kms_prepare(orphan)
            require(refused.returncode != 0 and not (orphan / 'kms-prerequisite/material').exists(),
                    'retained KMS state without its material was given replacement keys')
            self.case('retained-kms-state-without-material-refused')

            pin = (movement_state / 'movement/custody-profile.pin').read_bytes()
            self.stop('movement')
            movement = self.start_movement('movement', directory, movement_state)
            require(self.ready_within(movement, 30), 'movement did not recover readiness after its own restart')
            require((movement_state / 'movement/custody-profile.pin').read_bytes() == pin, 'movement restart changed its retained pin')
            self.stop('movement')
            self.case('movement-restart-recovers-retained-state')

            secrets = [(self.issued / name / 'key.der').read_bytes() for name in assembly.IDENTITIES]
            secrets += [(kms_out / 'kms-seal').read_bytes(), self.sequencer_seed]
            for path in [self.state / 'kms', self.state / 'kms-prerequisite', projected]:
                info = path.stat()
                require(info.st_uid in (0, 4026) and not info.st_mode & 0o007 and not (info.st_gid == 4020 and info.st_mode & 0o010),
                        'KMS private material is reachable by the movement group at ' + path.name)
            exposed = b''.join(path.read_bytes() for path in sorted(self.logs.iterdir()) if path.is_file())
            exposed += b''.join(self.responses) + (self.evidence / 'inventory.json').read_bytes()
            for secret in secrets:
                for form in (secret, secret.hex().encode(), base64.b64encode(secret)):
                    require(form not in exposed, 'private key material appeared in a log or KMS answer')
            self.case('no-private-key-material-exposed-or-cross-projected')
            after = self.inventory('after-rollout')
            self.material.write_bytes(self.evidence / 'inventory-after.json', json.dumps(after, indent=2).encode())

    try:
        revision, built = assembly.prerequisites()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        print('movement-kms refused: ' + str(error), file=sys.stderr, flush=True)
        return 1
    gate = MovementKms(revision, built)
    result = {'task': '24.3', 'revision': revision, 'cases': gate.cases, 'skipped': 0}
    try:
        gate.execute()
        code = 0
    except (OSError, ValueError, KeyError, RuntimeError, AttributeError, subprocess.SubprocessError) as error:
        code = 1
        result['observed'] = str(error)
        print('movement-kms refused: %s; evidence %s' % (error, gate.evidence), file=sys.stderr, flush=True)
    finally:
        gate.close()
    result.update(tests=len(gate.cases), exit_code=code)
    gate.material.write_bytes(gate.evidence / 'movement-kms.json', json.dumps(result, indent=2).encode())
    if code == 0:
        print('PAXEER_X_GATE tests=%d skipped=0 evidence=%s' % (len(gate.cases), gate.evidence), flush=True)
    return code


def ca_roster():
    """Requirement 206: the CA identity roster of tools/bringup/ca.sh is the
    approved integrated set declared below, apart from the producer table.
    Every identity is issued from its own row through the signer, derivation
    and authority choice of ca.sh into isolated storage and its certificate is
    inspected against the pinned row; drift, malformed rows, corrupted roles
    and incompatible material are refused without exposing a private key."""
    import base64
    import shlex
    quote = shlex.quote
    tool = ROOT / 'tools/bringup/ca.sh'
    columns = ('toml', 'process group', 'custody', 'common name', 'extended key usage', 'SAN list', 'authority')
    organisation = 'Paxeer X Network'
    apps = {
        'platform/hosted/node/fly.toml': 'paxeer-x-core',
        'human/wallet/deploy/human.toml': 'paxeer-human-service',
        'human/wallet/deploy/redis.toml': 'paxeer-shared-endpoint-redis',
        'human/wallet/deploy/endpoint.toml': 'paxeer-shared-endpoint',
        'platform/hosted/identity/fly.toml': 'paxeer-identity',
        'platform/hosted/internal/fly.toml': 'paxeer-internal',
        'platform/hosted/internal/redis.toml': 'paxeer-internal-redis',
        'platform/hosted/registry/fly.toml': 'paxeer-registry',
        'platform/hosted/indexer/fly.toml': 'paxeer-indexer',
        'platform/hosted/interop/fly.toml': 'paxeer-interop',
        'platform/hosted/webhooks/fly.toml': 'paxeer-webhooks',
        'platform/hosted/dashboard/fly.toml': 'paxeer-dashboard',
        'platform/ramps/fly.toml': 'paxeer-ramp',
    }
    # The approved integrated identity set, the Human KMS roles included.
    expected = {
        'pending-core': ('platform/hosted/node/fly.toml', '-', 'volume', 'layerx-pending-core', 'serverAuth', 'DNS:layerx-pending-core,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'pending-core-admin': ('platform/hosted/node/fly.toml', '-', 'volume', 'layerx-pending-core-admin', 'serverAuth', 'DNS:layerx-pending-core-admin,DNS:<app>.internal', 'internal'),
        'receipt-authority': ('platform/hosted/node/fly.toml', '-', 'volume', 'layerx-receipt-authority', 'serverAuth', 'DNS:layerx-receipt-authority,DNS:authority,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'agent-boundary': ('platform/hosted/node/fly.toml', '-', 'volume', 'layerx-agent-boundary', 'serverAuth', 'DNS:layerx-agent-boundary,DNS:component,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'agentd': ('human/wallet/deploy/human.toml', '-', 'volume', 'layerx-agentd', 'serverAuth', 'DNS:layerx-agentd,DNS:machine.paxeer.network,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'agentd-client': ('human/wallet/deploy/human.toml', '-', 'volume', 'layerx-agentd-client', 'clientAuth', '-', 'internal'),
        'paxeer-boundary-loopback': ('human/wallet/deploy/human.toml', '-', 'volume', 'paxeer-boundary', 'serverAuth', 'DNS:paxeer-boundary,DNS:paxeer-boundary-loopback,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'paxeer-boundary-public': ('human/wallet/deploy/human.toml', '-', 'volume', 'paxeer-observer-boundary', 'serverAuth', 'DNS:paxeer-observer-boundary,DNS:paxeer-boundary-public,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'guarantor': ('human/wallet/deploy/human.toml', '-', 'volume', 'layerx-guarantor', 'serverAuth,clientAuth', 'DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'human': ('platform/hosted/node/fly.toml', '-', 'volume', 'layerx-human', 'serverAuth', 'DNS:layerx-human,DNS:<app>.internal,DNS:paxeer-human-service.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'human-event-client': ('human/wallet/deploy/human.toml', '-', 'volume', 'layerx-human-events', 'clientAuth', 'URI:urn:layerx:webhooks:role:producer', 'internal'),
        'human-attestor-client': ('human/wallet/deploy/human.toml', '-', 'volume', 'layerx-human-components', 'clientAuth', '-', 'attestor'),
        'human-kms': ('human/wallet/deploy/human.toml', '-', 'volume', 'layerx-human-kms', 'serverAuth', 'DNS:layerx-human-kms,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'human-kms-client': ('human/wallet/deploy/human.toml', '-', 'volume', 'layerx-human-components', 'clientAuth', '-', 'internal'),
        'human-kms-executor': ('human/wallet/deploy/human.toml', '-', 'volume', 'layerx-human-movement', 'clientAuth', '-', 'internal'),
        'relay-archive': ('human/wallet/deploy/human.toml', '-', 'volume', 'layerx-relay-archive', 'serverAuth', 'DNS:layerx-relay-archive,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'gateway-redis': ('human/wallet/deploy/redis.toml', '-', 'REDIS_TLS', 'layerx-gateway-redis', 'serverAuth', 'DNS:layerx-gateway-redis,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'gateway-client': ('human/wallet/deploy/endpoint.toml', '-', 'ENDPOINT_CLIENT', 'layerx-gateway', 'clientAuth', 'URI:urn:layerx:webhooks:role:producer', 'internal'),
        'identity': ('platform/hosted/identity/fly.toml', '-', 'volume', 'layerx-identity', 'serverAuth', 'DNS:layerx-identity,DNS:identity,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'internal-kms': ('platform/hosted/internal/fly.toml', 'kms', 'volume', 'kms', 'serverAuth', 'DNS:kms,DNS:kms.process.<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'internal-journeys': ('platform/hosted/internal/fly.toml', 'journeys', 'volume', 'journeys', 'serverAuth', 'DNS:journeys,DNS:journeys.process.<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'internal-payments': ('platform/hosted/internal/fly.toml', 'payments', 'volume', 'payments', 'serverAuth', 'DNS:payments,DNS:payments.process.<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'internal-approvals': ('platform/hosted/internal/fly.toml', 'approvals', 'volume', 'approvals', 'serverAuth', 'DNS:approvals,DNS:approvals.process.<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'internal-programs': ('platform/hosted/internal/fly.toml', 'programs', 'volume', 'programs', 'serverAuth', 'DNS:programs,DNS:programs.process.<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'internal-redis': ('platform/hosted/internal/redis.toml', '-', 'REDIS_TLS', 'redis', 'serverAuth', 'DNS:redis,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'registry': ('platform/hosted/registry/fly.toml', '-', 'volume', 'layerx-program-registry', 'serverAuth', 'DNS:layerx-program-registry,DNS:index.paxeer.network,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'registry-event-client': ('platform/hosted/registry/fly.toml', '-', 'volume', 'layerx-registry-events', 'clientAuth', 'URI:urn:layerx:webhooks:role:producer', 'internal'),
        'indexer': ('platform/hosted/indexer/fly.toml', '-', 'volume', 'layerx-indexer', 'serverAuth', 'DNS:layerx-indexer,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'interop-client': ('platform/hosted/interop/fly.toml', '-', 'INTEROP_CLIENT', 'layerx-interop-gateway', 'clientAuth', '-', 'internal'),
        'developer': ('platform/hosted/webhooks/fly.toml', 'ingress', 'WEBHOOKS_INGRESS_TLS', 'layerx-developer', 'serverAuth', 'DNS:layerx-webhooks,DNS:ingress.process.<app>.internal,DNS:public.process.<app>.internal,DNS:localhost,IP:127.0.0.1', 'internal'),
        'developer-client': ('platform/hosted/webhooks/fly.toml', '-', 'WEBHOOKS_CLIENT', 'layerx-developer', 'clientAuth', '-', 'internal'),
        'dashboard-client': ('platform/hosted/dashboard/fly.toml', '-', 'DASHBOARD_CLIENT', 'layerx-dashboard', 'clientAuth', '-', 'internal'),
        'ramp-client': ('platform/ramps/fly.toml', '-', 'RAMP_CLIENT', 'layerx-reference-ramp', 'clientAuth', 'DNS:<app>.internal', 'internal'),
    }
    expected_local = {
        'webhook-operator-client': ('-', '-', 'local', 'layerx-webhooks-operator', 'clientAuth', 'URI:urn:layerx:webhooks:role:operator', 'internal'),
    }
    kms_roles = ('human-kms', 'human-kms-client', 'human-kms-executor')
    usages = {'serverAuth': 'TLS Web Server Authentication', 'clientAuth': 'TLS Web Client Authentication'}
    identity_files = {'cert.pem', 'key.pem', 'ca.pem', 'cert.der', 'key.der', 'ca.der', 'identity.p12', 'password'}
    cases, outputs, fingerprints = [], [], {}

    def require(condition, message):
        if not condition:
            raise RuntimeError(message)

    def case(name):
        cases.append(name)
        print('PAXEER_X_CASE %s ok' % name, flush=True)

    raw = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    if raw:
        evidence = Path(raw)
        evidence.mkdir(mode=0o700, parents=True, exist_ok=False)
    else:
        evidence = Path(tempfile.mkdtemp(prefix='paxeer-x-ca-roster-evidence-'))
    evidence.chmod(0o700)
    scratch = Path(tempfile.mkdtemp(prefix='paxeer-x-ca-roster-'))
    scratch.chmod(0o700)
    print('PAXEER_X_EVIDENCE dir=%s' % evidence, flush=True)
    internal_ca, attestor_ca, foreign_ca = scratch / 'ca', scratch / 'attestor-ca', scratch / 'foreign-ca'

    def run(argv, environment=None, check=True):
        env = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'HOME': str(scratch), 'LC_ALL': 'C',
               'LAYERX_CA_DIR': str(internal_ca), 'LAYERX_ATTESTOR_CA_DIR': str(attestor_ca)}
        env.update(environment or {})
        result = subprocess.run([str(item) for item in argv], stdin=subprocess.DEVNULL, capture_output=True,
                                text=True, env=env, timeout=120)
        outputs.append(result.stdout + result.stderr)
        if check and result.returncode:
            raise RuntimeError('command failed exit=%d: %s: %s' % (result.returncode, ' '.join(map(str, argv[:4])),
                                                                     result.stderr.strip()[-600:]))
        return result

    def function(text, name):
        match = re.search(r'^' + re.escape(name) + r'\(\) \{\n.*?^\}\n', text, re.M | re.S)
        require(match is not None, 'function %s absent from ca.sh' % name)
        return match.group(0)

    def assignment(text, name):
        match = re.search(r'^' + re.escape(name) + r'=.*$', text, re.M)
        require(match is not None, 'assignment %s absent from ca.sh' % name)
        return match.group(0)

    def prelude(path):
        """check-live.sh sourced as ca.sh sources it, then the settings and
        the functions of the ca.sh at path."""
        text = Path(path).read_text()
        return '\n'.join(['set -euo pipefail', '. ' + quote(str(Path(path).with_name('check-live.sh')))]
                         + [assignment(text, name) for name in ('subject_org', 'cert_days', 'identity_files', 'attestor_services')]
                         + [function(text, name) for name in ('ca_services', 'local_services', 'service_row', 'row_ca_dir',
                                                              'certificate_usage_matches', 'sign', 'derive_cmd')])

    def bash(script, *arguments, path=tool, environment=None, check=True):
        return run(['bash', '-c', prelude(path) + '\n' + script, 'ca-roster', *arguments], environment, check)

    def attestors(path):
        value = bash('printf "%s" "$attestor_services"', path=path).stdout
        return set(value.split())

    def drift(lines, attestor_rows, pinned):
        """Every way the listed rows differ from the pinned set."""
        problems, rows = [], {}
        for line in lines:
            fields = line.split()
            if len(fields) != 7:
                problems.append('row with %d fields: %s' % (len(fields), line))
                continue
            if fields[0] in rows:
                problems.append('duplicate ' + fields[0])
                continue
            rows[fields[0]] = tuple(fields[1:]) + ('attestor' if fields[0] in attestor_rows else 'internal',)
        problems += ['missing ' + service for service in sorted(pinned.keys() - rows.keys())]
        problems += ['extra ' + service for service in sorted(rows.keys() - pinned.keys())]
        for service in sorted(pinned.keys() & rows.keys()):
            for label, want, got in zip(columns, pinned[service], rows[service]):
                if want != got:
                    problems.append('%s %s is %s, not %s' % (service, label, got, want))
        problems += ['attestor authority for unlisted ' + service for service in sorted(attestor_rows - rows.keys())]
        if len(lines) != len(pinned):
            problems.append('%d rows, not the %d pinned identities' % (len(lines), len(pinned)))
        return problems

    def fly_app(toml):
        return bash('fly_app "$1"', toml).stdout

    def sans_of(template, app):
        return [] if template == '-' else sorted(item.replace('<app>', app) for item in template.split(','))

    def text_extensions(cert):
        text = run(['openssl', 'x509', '-in', cert, '-noout', '-text']).stdout.splitlines()
        values = {}
        for index, line in enumerate(text):
            header = line.strip()
            for name in ('X509v3 Subject Alternative Name', 'X509v3 Extended Key Usage', 'X509v3 Basic Constraints',
                         'X509v3 Key Usage'):
                if header.startswith(name + ':'):
                    require(name not in values, '%s carries %s twice' % (cert, name))
                    values[name] = text[index + 1].strip()
        return values

    def verifies(cert, ca, *options):
        return run(['openssl', 'verify', '-no-CApath', '-no-CAstore', *options, '-CAfile', Path(ca) / 'ca.pem', cert],
                   check=False).returncode == 0

    def inspect(directory, row, app, authority):
        """Every reason the identity in directory is not exactly row: name,
        ownership is the caller's, role, EKU/SAN, trust and the files."""
        _, _, _, cn, eku, sans, _ = row
        cert = directory / 'cert.pem'
        problems = []
        subject = run(['openssl', 'x509', '-in', cert, '-noout', '-subject', '-nameopt', 'RFC2253']).stdout.strip()
        if sorted(subject.removeprefix('subject=').split(',')) != sorted(['CN=' + cn, 'O=' + organisation]):
            problems.append('subject ' + subject)
        values = text_extensions(cert)
        if sorted(values.get('X509v3 Extended Key Usage', '').split(', ')) != sorted(usages[item] for item in eku.split(',')):
            problems.append('extended key usage ' + values.get('X509v3 Extended Key Usage', 'absent'))
        listed = values.get('X509v3 Subject Alternative Name')
        got = [] if listed is None else sorted(item.replace('IP Address:', 'IP:') for item in listed.split(', '))
        if got != sans_of(sans, app):
            problems.append('SAN list %s' % ','.join(got))
        if values.get('X509v3 Basic Constraints') != 'CA:FALSE':
            problems.append('basic constraints ' + str(values.get('X509v3 Basic Constraints')))
        if values.get('X509v3 Key Usage') != 'Digital Signature, Key Encipherment':
            problems.append('key usage ' + str(values.get('X509v3 Key Usage')))
        purposes = {'serverAuth': 'sslserver', 'clientAuth': 'sslclient'}
        for item in eku.split(','):
            if not verifies(cert, authority, '-purpose', purposes[item]):
                problems.append('does not chain to the %s authority for %s' % (authority.name, item))
        for other in {internal_ca, attestor_ca, foreign_ca} - {authority}:
            if verifies(cert, other):
                problems.append('also chains to ' + other.name)
        if 'serverAuth' in eku:
            for item in sans_of(sans, app):
                option = ('-verify_hostname', item[4:]) if item.startswith('DNS:') else ('-verify_ip', item[3:])
                if not verifies(cert, authority, '-purpose', 'sslserver', *option):
                    problems.append('server name %s not verified' % item)
        if {path.name for path in directory.iterdir()} != identity_files:
            problems.append('files %s' % sorted(path.name for path in directory.iterdir()))
            return problems
        for path in directory.iterdir():
            info = path.lstat()
            if not path.is_file() or path.is_symlink() or info.st_mode & 0o777 != 0o600 or info.st_uid != os.geteuid():
                problems.append('%s is not a private regular file' % path.name)
        if directory.stat().st_mode & 0o777 != 0o700:
            problems.append('directory is not private')
        public = run(['openssl', 'x509', '-in', cert, '-noout', '-pubkey']).stdout
        if run(['openssl', 'pkey', '-in', directory / 'key.pem', '-pubout']).stdout != public:
            problems.append('key.pem is not the certified key')
        if run(['openssl', 'pkey', '-inform', 'DER', '-in', directory / 'key.der', '-pubout']).stdout != public:
            problems.append('key.der is not the certified key')
        der = subprocess.run(['openssl', 'x509', '-in', str(cert), '-outform', 'DER'], capture_output=True, check=True).stdout
        if (directory / 'cert.der').read_bytes() != der:
            problems.append('cert.der is not cert.pem')
        if (directory / 'ca.pem').read_bytes() != (authority / 'ca.pem').read_bytes():
            problems.append('ca.pem is not the %s authority' % authority.name)
        ca_der = subprocess.run(['openssl', 'x509', '-in', str(authority / 'ca.pem'), '-outform', 'DER'],
                                capture_output=True, check=True).stdout
        if (directory / 'ca.der').read_bytes() != ca_der:
            problems.append('ca.der is not the %s authority' % authority.name)
        bundled = run(['openssl', 'pkcs12', '-in', directory / 'identity.p12', '-passin', 'file:' + str(directory / 'password'),
                       '-nokeys', '-clcerts'], check=False)
        if bundled.returncode:
            problems.append('identity.p12 does not open with its password')
        elif 'BEGIN CERTIFICATE' not in bundled.stdout or fingerprint_of(bundled.stdout) != fingerprint_of(cert.read_text()):
            problems.append('identity.p12 does not hold the certificate')
        return problems

    def fingerprint_of(pem):
        result = subprocess.run(['openssl', 'x509', '-noout', '-fingerprint', '-sha256'], input=pem,
                                capture_output=True, text=True)
        return result.stdout.strip().split('=', 1)[-1] if result.returncode == 0 else None

    issue_script = '''umask 077
read -r _ toml _ _ cn eku sans <<<"$(service_row "$1")"
app="$(fly_app "$toml")"
ca_dir="$(row_ca_dir "$1")"
work="$2"
mkdir -m 0700 "$work"
(
	umask 077
	cd "$work"
	openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out key.pem 2>/dev/null
	openssl req -new -key key.pem -subj "/O=$subject_org/CN=$cn" -out csr.pem
)
sign "$1" "$eku" "$(app_sans "$sans" "$app")"
(
	umask 077
	cd "$work"
	cp "$ca_dir/ca.pem" ca.pem
	sh -c "$(derive_cmd "$cn")"
	rm -f csr.pem ext.cnf
)
printf '%s %s' "$app" "$ca_dir"
'''
    corrupt_script = '''umask 077
ca_dir="$3"
work="$2"
mkdir -m 0700 "$work"
(
	umask 077
	cd "$work"
	openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out key.pem 2>/dev/null
	openssl req -new -key key.pem -subj "/O=$subject_org/CN=$4" -out csr.pem
)
sign "$1" "$5" "$6"
(
	umask 077
	cd "$work"
	cp "$ca_dir/ca.pem" ca.pem
	sh -c "$(derive_cmd "$4")"
	rm -f csr.pem ext.cnf
)
'''

    def usage_matches(service, cert, authority, app):
        return bash('certificate_usage_matches "$1" "$(cat "$2")" "$3" "$4"', service, cert, authority, app,
                    check=False).returncode == 0

    def tree(label, mutate):
        """A copy of ca.sh and check-live.sh whose source text mutate edits."""
        directory = scratch / 'drift' / label / 'tools/bringup'
        directory.mkdir(mode=0o700, parents=True)
        shutil.copyfile(ROOT / 'tools/bringup/check-live.sh', directory / 'check-live.sh')
        original = tool.read_text()
        changed = mutate(original)
        require(changed != original, 'mutation %s left ca.sh unchanged' % label)
        (directory / 'ca.sh').write_text(changed)
        return directory / 'ca.sh'

    def row_edit(service, edit):
        def mutate(text):
            lines = text.split('\n')
            index = [i for i, line in enumerate(lines) if line.split(' ', 1)[0] == service and line.count(' ') == 6]
            require(len(index) == 1, 'the table holds no single row %s to mutate' % service)
            lines[index[0]:index[0] + 1] = edit(lines[index[0]].split(' '))
            return '\n'.join(lines)
        return mutate

    def column(service, number, value):
        return row_edit(service, lambda fields: [' '.join(fields[:number] + [value] + fields[number + 1:])])

    revision = command(['git', '-C', str(ROOT), 'rev-parse', 'HEAD']).stdout.strip()
    result = {'task': '24.7', 'revision': revision, 'cases': cases, 'skipped': 0}
    try:
        # ac_1/ac_3: the producer table is exactly the pinned set; the count is the pinned set's.
        require(set(kms_roles) <= expected.keys(), 'the pinned set lacks the Human KMS roles')
        run(['bash', tool, 'init'], {'LAYERX_CA_DIR': str(internal_ca)})
        run(['bash', tool, 'init'], {'LAYERX_CA_DIR': str(attestor_ca)})
        run(['bash', tool, 'init'], {'LAYERX_CA_DIR': str(foreign_ca)})
        roster = run(['bash', tool, 'services']).stdout
        lines = [line for line in roster.split('\n') if line]
        problems = drift(lines, attestors(tool), expected)
        require(not problems, 'the ca.sh roster differs from the pinned set: ' + '; '.join(problems))
        require(len(lines) == len(expected), 'the roster cardinality is not the pinned set\'s')
        local_lines = [line for line in run(['bash', tool, 'local-services']).stdout.split('\n') if line]
        problems = drift(local_lines, set(), expected_local)
        require(not problems, 'the ca.sh local roster differs from the pinned set: ' + '; '.join(problems))
        require(not (expected.keys() & expected_local.keys()), 'a local identity is also a Fly row')
        case('roster-is-exactly-the-pinned-identity-set')

        for toml, app in sorted(apps.items()):
            require(fly_app(toml) == app, '%s does not name the app %s' % (toml, app))
        require({row[0] for row in expected.values()} == apps.keys(), 'a pinned row names an app outside the pinned owners')
        for service, row in expected.items():
            authority = {'internal': internal_ca, 'attestor': attestor_ca}[row[6]]
            got = bash('row_ca_dir "$1"', service, check=False)
            require(got.returncode == 0 and got.stdout == str(authority),
                    '%s is signed under %s, not the %s authority' % (service, got.stdout or 'no authority', row[6]))
        for same in (internal_ca, str(internal_ca) + '/.'):
            require(bash('row_ca_dir "$1"', 'human-attestor-client', environment={'LAYERX_ATTESTOR_CA_DIR': str(same)},
                         check=False).returncode != 0, 'the internal CA was admitted as the attestors\' gateway CA')
        case('every-row-owned-by-its-pinned-app-and-authority')

        # ac_2/ac_4: drift and malformed rows are refused.
        valid_drift = {
            'removed-human-kms-executor': (row_edit('human-kms-executor', lambda fields: []), 'missing human-kms-executor'),
            'added-unapproved-identity': (row_edit('human-kms-client', lambda fields: [
                ' '.join(fields), 'human-kms-admin human/wallet/deploy/human.toml - volume layerx-human-kms-admin clientAuth -']),
                'extra human-kms-admin'),
            'kms-client-role-as-movement': (column('human-kms-client', 4, 'layerx-human-movement'), 'human-kms-client common name'),
            'kms-executor-as-server': (row_edit('human-kms-executor', lambda fields: [' '.join(
                fields[:5] + ['serverAuth', 'DNS:layerx-human-movement'])]), 'human-kms-executor extended key usage'),
            'kms-server-moved-app': (column('human-kms', 1, 'platform/hosted/node/fly.toml'), 'human-kms toml'),
            'kms-server-name-stripped': (column('human-kms', 6, 'DNS:<app>.internal,DNS:localhost,IP:127.0.0.1'), 'human-kms SAN list'),
            'producer-as-operator': (column('gateway-client', 6, 'URI:urn:layerx:webhooks:role:operator'), 'gateway-client SAN list'),
            'secret-custody-to-volume': (column('gateway-redis', 3, 'volume'), 'gateway-redis custody'),
            'attestor-client-under-internal-ca': (lambda text: text.replace('attestor_services="human-attestor-client"',
                                                                             'attestor_services=""'), 'human-attestor-client authority'),
            'kms-client-under-attestor-ca': (lambda text: text.replace('attestor_services="human-attestor-client"',
                                                                        'attestor_services="human-attestor-client human-kms-client"'),
                                             'human-kms-client authority'),
        }
        malformed = {
            'duplicate-human-kms': (row_edit('human-kms', lambda fields: [' '.join(fields)] * 2), 'duplicate human-kms'),
            'unknown-extended-key-usage': (column('human-kms-client', 5, 'codeSigning'), 'human-kms-client extended key usage'),
            'six-field-row': (row_edit('human-kms', lambda fields: [' '.join(fields[:6])]), 'row with 6 fields'),
            'lowercase-secret-prefix': (column('gateway-redis', 3, 'redis_tls'), 'gateway-redis custody'),
            'server-without-san-list': (column('human-kms', 6, '-'), 'human-kms SAN list'),
            'foreign-san-type': (column('human-kms-executor', 6, 'email:ops@paxeer.network'), 'human-kms-executor SAN list'),
            'repeated-san': (column('human-kms', 6, 'DNS:layerx-human-kms,DNS:layerx-human-kms,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1'),
                             'human-kms SAN list'),
        }
        for label, (mutate, reason) in {**valid_drift, **malformed}.items():
            copy = tree(label, mutate)
            table = [line for line in bash('ca_services', path=copy).stdout.split('\n') if line]
            problems = drift(table, attestors(copy), expected)
            require(any(problem.startswith(reason) for problem in problems),
                    '%s was not refused as %s: %s' % (label, reason, problems))
            listed = run(['bash', copy, 'services'], check=False)
            if label in malformed:
                require(listed.returncode == 1 and 'is malformed' in listed.stderr and not listed.stdout,
                        'ca.sh services listed the %s table' % label)
            else:
                require(listed.returncode == 0 and drift([line for line in listed.stdout.split('\n') if line], attestors(copy), expected),
                        'the %s roster passed as the pinned set' % label)
            case('roster-drift-refused-' + label)

        # ac_2: every pinned identity issued from its own row and inspected.
        issued = scratch / 'issued'
        issued.mkdir(mode=0o700)
        for service, row in expected.items():
            app_and_ca = bash(issue_script, service, issued / service).stdout.split(' ', 1)
            authority = {'internal': internal_ca, 'attestor': attestor_ca}[row[6]]
            require(app_and_ca == [apps[row[0]], str(authority)], '%s was issued for %s' % (service, app_and_ca))
            problems = inspect(issued / service, row, apps[row[0]], authority)
            require(not problems, 'issued %s is not its pinned identity: %s' % (service, '; '.join(problems)))
            require(usage_matches(service, issued / service / 'cert.pem', authority, apps[row[0]]),
                    'ca.sh does not accept issued %s for its own row' % service)
            fingerprints[service] = fingerprint_of((issued / service / 'cert.pem').read_text())
        require(len(fingerprints) == len(expected) and len(set(fingerprints.values())) == len(expected),
                'the issued identities are not one distinct certificate per pinned row')
        case('every-pinned-identity-issued-and-inspected-row-by-row')

        kernel = apps['human/wallet/deploy/human.toml']
        for service in kms_roles:
            for other in kms_roles:
                if other != service:
                    require(not usage_matches(other, issued / service / 'cert.pem', internal_ca, kernel),
                            'issued %s was admitted as %s' % (service, other))
                    require(inspect(issued / service, expected[other], kernel, internal_ca),
                            'issued %s passed inspection as %s' % (service, other))
        for service in ('human-attestor-client', 'gateway-client', 'agentd-client', 'human-event-client'):
            for other in ('human-kms-client', 'human-kms-executor'):
                require(not usage_matches(other, issued / service / 'cert.pem', internal_ca, kernel)
                        and inspect(issued / service, expected[other], kernel, internal_ca),
                        '%s was admitted as %s' % (service, other))
        case('human-kms-roles-never-stand-in-for-one-another')

        # ac_2: identities issued under corrupted roles are refused.
        corrupted = scratch / 'corrupted'
        corrupted.mkdir(mode=0o700)
        kms_sans = 'DNS:layerx-human-kms,DNS:%s.internal,DNS:localhost,IP:127.0.0.1' % kernel
        corruptions = [
            ('kms-client-with-movement-name', 'human-kms-client', internal_ca, 'layerx-human-movement', 'clientAuth', ''),
            ('kms-executor-with-server-usage', 'human-kms-executor', internal_ca, 'layerx-human-movement', 'serverAuth', kms_sans),
            ('kms-client-with-both-usages', 'human-kms-client', internal_ca, 'layerx-human-components', 'serverAuth,clientAuth', kms_sans),
            ('kms-server-without-server-name', 'human-kms', internal_ca, 'layerx-human-kms', 'serverAuth',
             'DNS:%s.internal,DNS:localhost,IP:127.0.0.1' % kernel),
            ('kms-client-under-attestor-ca', 'human-kms-client', attestor_ca, 'layerx-human-components', 'clientAuth', ''),
            ('kms-executor-from-foreign-ca', 'human-kms-executor', foreign_ca, 'layerx-human-movement', 'clientAuth', ''),
            ('producer-client-as-operator', 'gateway-client', internal_ca, 'layerx-gateway', 'clientAuth',
             'URI:urn:layerx:webhooks:role:operator'),
            ('kms-server-with-extra-name', 'human-kms', internal_ca, 'layerx-human-kms', 'serverAuth', kms_sans + ',DNS:paxeer-boundary'),
            ('attestor-client-under-internal-ca', 'human-attestor-client', internal_ca, 'layerx-human-components', 'clientAuth', ''),
        ]
        for label, service, authority, cn, eku, sans in corruptions:
            bash(corrupt_script, service, corrupted / label, authority, cn, eku, sans)
            row = expected[service]
            pinned_authority = {'internal': internal_ca, 'attestor': attestor_ca}[row[6]]
            app = apps[row[0]]
            require(inspect(corrupted / label, row, app, pinned_authority), '%s passed as the pinned %s' % (label, service))
            if service in kms_roles or service == 'gateway-client':
                require(not usage_matches(service, corrupted / label / 'cert.pem', pinned_authority, app),
                        'ca.sh accepted %s as %s' % (label, service))
            case('corrupted-role-refused-' + label)

        # ac_4: existing generations are kept; incompatible material is refused.
        before = {name: hashlib.sha256((internal_ca / name).read_bytes()).hexdigest() for name in ('ca.key', 'ca.pem', 'ca.der')}
        again = run(['bash', tool, 'init'], check=False)
        require(again.returncode == 1 and 'already holds a CA' in again.stderr and not again.stdout
                and before == {name: hashlib.sha256((internal_ca / name).read_bytes()).hexdigest() for name in before},
                'a second init touched the established CA')
        require(run(['bash', tool, 'services']).stdout == roster
                and run(['bash', tool, 'local-services']).stdout == '\n'.join(local_lines) + '\n',
                'the ca.sh roster is not stable')
        local = scratch / 'local'
        local.mkdir(mode=0o700)
        operator = local / 'webhook-operator-client'
        made = run(['bash', tool, 'issue-local', 'webhook-operator-client', '--output-dir', operator])
        require(re.fullmatch(r'issued webhook-operator-client custody=local fingerprint=([0-9A-F]{2}:){31}[0-9A-F]{2} expires_in=39\dd\n',
                             made.stdout) is not None, 'issue-local printed %r' % made.stdout)
        problems = inspect(operator, expected_local['webhook-operator-client'], '-', internal_ca)
        require(not problems, 'the local operator identity is not its pinned row: ' + '; '.join(problems))
        fingerprints['webhook-operator-client'] = fingerprint_of((operator / 'cert.pem').read_text())
        retained = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in operator.iterdir()}
        repeat = run(['bash', tool, 'issue-local', 'webhook-operator-client', '--output-dir', operator], check=False)
        require(repeat.returncode == 1 and 'already exists' in repeat.stderr and not repeat.stdout
                and retained == {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in operator.iterdir()},
                'a second issue-local replaced the retained operator identity')
        for refused, argv, status in (
                ('fly-row-as-local', ['issue-local', 'human-kms-client', '--output-dir', local / 'kms-client'], 2),
                ('inside-the-ca', ['issue-local', 'webhook-operator-client', '--output-dir', internal_ca / 'operator'], 1),
                ('relative-destination', ['issue-local', 'webhook-operator-client', '--output-dir', 'operator'], 1)):
            answer = run(['bash', tool, *argv], check=False)
            require(answer.returncode == status and not answer.stdout, 'issue-local %s was not refused' % refused)
        require(not (local / 'kms-client').exists() and not (internal_ca / 'operator').exists(),
                'a refused issue-local left material behind')
        for service, row in expected.items():
            authority = {'internal': internal_ca, 'attestor': attestor_ca}[row[6]]
            require(not inspect(issued / service, row, apps[row[0]], authority)
                    and fingerprint_of((issued / service / 'cert.pem').read_text()) == fingerprints[service],
                    'the retained %s generation changed' % service)
        case('existing-generations-idempotent-and-incompatible-material-refused')

        # ac_4: no private key value appears in any output.
        exposed = '\n'.join(outputs)
        require('PRIVATE KEY' not in exposed, 'a private key block appeared in an output')
        flat = exposed.replace('\n', '')
        keys = list(scratch.rglob('key.pem'))
        require(len(keys) == len(expected) + len(corruptions) + 1, 'not every issued key was examined')
        for key in keys:
            body = ''.join(line for line in key.read_text().splitlines() if not line.startswith('-----'))
            der = base64.b64decode(body)
            at = der.find(b'\x02\x01\x01\x04\x20')
            require(at >= 0, 'the key of %s is not an EC private key' % key.parent.name)
            scalar = der[at + 5:at + 37]
            require(body not in flat and scalar.hex() not in exposed.lower()
                    and base64.b64encode(scalar).decode() not in flat,
                    'private key material of %s appeared in an output' % key.parent.name)
        case('no-private-key-value-exposed')
        code = 0
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError) as error:
        code = 1
        result['observed'] = str(error)
        print('ca-roster refused: %s; evidence %s' % (error, evidence), file=sys.stderr, flush=True)
    finally:
        shutil.rmtree(scratch, ignore_errors=True)
    result.update(tests=len(cases), exit_code=code, roster=sorted(expected), local_roster=sorted(expected_local),
                  fingerprints=fingerprints)
    with open(evidence / 'ca-roster.json', 'x') as handle:
        json.dump(result, handle, indent=2)
    if code == 0:
        print('PAXEER_X_GATE tests=%d skipped=0 evidence=%s' % (len(cases), evidence), flush=True)
    return code


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=['role-directories', 'role-directory-prerequisite', 'export-recovery', 'fixture-foundation', 'kms-service-prerequisite', 'registry-material', 'policy-graph', 'identity-rotation', 'movement-kms', 'ca-roster'])
    arguments = parser.parse_args()
    os.umask(0o077)
    if arguments.case == 'ca-roster':
        return ca_roster()
    if arguments.case == 'movement-kms':
        return movement_kms()
    if arguments.case == 'policy-graph':
        return policy_graph()
    if arguments.case == 'registry-material':
        return registry_material()
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
        suite = unittest.TestSuite([unittest.defaultTestLoader.loadTestsFromTestCase(RoleDirectories),
                                    unittest.defaultTestLoader.loadTestsFromTestCase(RoleDirectoryEvidenceRead)])
    elif arguments.case == 'role-directory-prerequisite':
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(DirectoryPrerequisite)
    elif arguments.case == 'identity-rotation':
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(IdentityRotation)
    else:
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(ExportRecovery)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print('PAXEER_X_GATE tests=' + str(result.testsRun) + ' skipped=' + str(len(result.skipped)), flush=True)
    return 0 if result.wasSuccessful() and result.testsRun > 0 and not result.skipped else 1


if __name__ == '__main__':
    raise SystemExit(main())
