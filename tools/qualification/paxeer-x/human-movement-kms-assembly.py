#!/usr/bin/env python3
"""Movement is supplied by the kernel's authenticated Human KMS execution service.

--build compiles the Human KMS, the movement provider and the runtime clock
once and records them in a private build manifest. Without arguments the gate
checks its prerequisites and that manifest and never compiles. It then issues
the exact Human KMS identities through the CA catalog rows and the signer of
tools/bringup/ca.sh, prepares and projects the KMS material through the kernel
init functions and platform/hosted/human/material.py, and starts the real KMS
and movement roles through docker/human-service/entrypoint.sh and the runtime
clock under their kernel uids, mount namespaces and fixed loopback addresses
inside a private network namespace. Every refusal, loss, restart and recovery
case runs against those processes; all state and logs stay under a private
evidence directory.
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.human-movement-kms-assembly.v1'
MANIFEST = Path(os.environ.get('PAXEER_X_MOVEMENT_KMS_ASSEMBLY_MANIFEST',
                               ROOT / 'human/target/paxeer-x/human-movement-kms-assembly.json'))
SOURCES = ['human/Cargo.toml', 'human/Cargo.lock', 'human/crates', 'agent/crates',
           'platform/Cargo.toml', 'platform/Cargo.lock', 'platform/crates', 'platform/hosted/runtime-clock',
           'docker/kernel/init.sh', 'docker/human-service/entrypoint.sh', 'tools/bringup/ca.sh',
           'platform/hosted/human/material.py', 'tests/fixtures/custody/native-credit-receipt/profile',
           'tools/qualification/paxeer-x/human-movement-kms-assembly.py']
TOUCHES = ['docker/kernel/init.sh', 'docker/human-service/entrypoint.sh', 'tools/bringup/ca.sh',
           'tools/bringup/check-live.test.sh', 'platform/hosted/human/material.py',
           'platform/hosted/human/material.sh', 'human/crates/layerx-human-movement-provider/src/config.rs']
INIT = ROOT / 'docker/kernel/init.sh'
CA = ROOT / 'tools/bringup/ca.sh'
IDENTITIES = ('human-kms', 'human-kms-client', 'human-kms-executor')
PATH = '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'


def refuse(reason):
    raise RuntimeError(reason)


def require(condition, reason):
    if not condition:
        refuse(reason)


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            h.update(block)
    return h.hexdigest()


def git(*args):
    return subprocess.check_output(['git', '-C', str(ROOT), *args], text=True).strip()


def source():
    if git('status', '--porcelain', '--untracked-files=no', '--', *SOURCES):
        refuse('the qualified sources have uncommitted changes')
    return git('rev-parse', 'HEAD')


def artifacts(argv, wanted):
    result = subprocess.run(argv, cwd=ROOT, text=True, stdout=subprocess.PIPE, stdin=subprocess.DEVNULL)
    if result.returncode:
        refuse('build failed (%d): %s' % (result.returncode, ' '.join(argv)))
    found = {}
    for line in result.stdout.splitlines():
        try:
            item = json.loads(line)
        except ValueError:
            continue
        if item.get('reason') != 'compiler-artifact' or not item.get('executable'):
            continue
        key = (item['target']['name'], tuple(item['target']['kind']), bool(item['profile']['test']))
        if key in wanted:
            found[wanted[key]] = {'path': str(Path(item['executable']).resolve()),
                                  'sha256': digest(item['executable'])}
    if set(found) != set(wanted.values()):
        refuse('build did not produce: ' + ', '.join(sorted(set(wanted.values()) - set(found))))
    return found


def build():
    revision = source()
    built = {}
    built.update(artifacts(['cargo', 'build', '--locked', '--manifest-path', 'human/Cargo.toml',
                            '-p', 'layerx-human-kms', '--bin', 'layerx-human-kms',
                            '-p', 'layerx-human-movement-provider', '--bin', 'layerx-human-movement-provider',
                            '--message-format=json'], {
        ('layerx-human-kms', ('bin',), False): 'kms',
        ('layerx-human-movement-provider', ('bin',), False): 'provider',
    }))
    built.update(artifacts(['cargo', 'build', '--locked', '--manifest-path', 'platform/Cargo.toml',
                            '-p', 'layerx-runtime-clock', '--bin', 'layerx-runtime-clock',
                            '--message-format=json'], {
        ('layerx-runtime-clock', ('bin',), False): 'clock',
    }))
    if source() != revision:
        refuse('sources changed during the build')
    MANIFEST.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    temporary = MANIFEST.with_suffix('.next')
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump({'schema': SCHEMA, 'revision': revision, 'artifacts': built}, stream, indent=2)
        stream.write('\n')
    os.replace(temporary, MANIFEST)
    print('PAXEER_X_BUILD manifest=%s revision=%s' % (MANIFEST, revision))


def prerequisites():
    require(os.geteuid() == 0, 'the role uids, mount and network namespaces require root')
    for tool in ['openssl', 'socat', 'git', 'unshare', 'nsenter', 'setpriv', 'ip', 'mount', 'flock', 'cmp', 'install']:
        if not shutil.which(tool):
            refuse('prerequisite missing: ' + tool)
    if not (Path('/root/.foundry/bin/anvil').is_file() or shutil.which('anvil')):
        refuse('prerequisite missing: anvil')
    if not MANIFEST.is_file():
        refuse('no build manifest at %s; run this gate with --build first' % MANIFEST)
    info = MANIFEST.stat()
    if info.st_uid != os.geteuid() or info.st_mode & 0o077:
        refuse('the build manifest must be private and owned by the gate user')
    data = json.loads(MANIFEST.read_text())
    revision = source()
    if data.get('schema') != SCHEMA or data.get('revision') != revision:
        refuse('the prebuilt executables were not built from this revision; run --build')
    built = data['artifacts']
    require(set(built) == {'kms', 'provider', 'clock'}, 'the build manifest names other executables')
    for name, row in built.items():
        path = Path(row['path'])
        if not (path.is_absolute() and path.is_file() and not path.is_symlink() and os.access(path, os.X_OK)):
            refuse('prebuilt executable missing: ' + name)
        if digest(path) != row['sha256']:
            refuse('prebuilt executable changed since the build: ' + name)
    return revision, built


def shell_function(path, name):
    text = Path(path).read_text()
    match = re.search(r'^' + re.escape(name) + r'\(\) \{\n.*?^\}\n', text, re.M | re.S)
    require(match is not None, 'function %s absent from %s' % (name, path))
    return match.group(0)


def init_assignment(name):
    match = re.search(r'^' + re.escape(name) + r"=(.*)$", INIT.read_text(), re.M)
    require(match is not None, 'kernel init assignment %s absent' % name)
    return match.group(0)


def service_block(name):
    text = INIT.read_text()
    match = re.search(r'^human_root=\$human_state/\S+ service ' + re.escape(name) + r' (\d+) \\\n(.*?)/usr/local/bin/human-entrypoint (\S+)\n',
                      text, re.M | re.S)
    require(match is not None, 'kernel service %s absent' % name)
    return match


class Gate:
    def __init__(self, revision, built):
        self.revision, self.built = revision, built
        self.cases, self.processes, self.holder = [], {}, None
        raw = os.environ.get('PAXEER_X_EVIDENCE_DIR')
        if raw:
            self.evidence = Path(raw)
            self.evidence.mkdir(mode=0o700, parents=True, exist_ok=False)
        else:
            self.evidence = Path(tempfile.mkdtemp(prefix='human-movement-kms-assembly-'))
        self.evidence.chmod(0o700)
        self.logs = self.mkdir('logs', 0, 0, 0o700)
        spec = importlib.util.spec_from_file_location('human_material', ROOT / 'platform/hosted/human/material.py')
        self.material = importlib.util.module_from_spec(spec)
        sys.path.insert(0, str(ROOT / 'platform/hosted/human'))
        spec.loader.exec_module(self.material)
        print('PAXEER_X_EVIDENCE dir=%s' % self.evidence, flush=True)

    def case(self, name):
        self.cases.append(name)
        print('PAXEER_X_CASE %s ok' % name, flush=True)

    def mkdir(self, relative, uid, gid, mode, base=None):
        path = (base or self.evidence) / relative
        path.mkdir(mode=mode, parents=False)
        os.chown(path, uid, gid)
        path.chmod(mode)
        return path

    def run(self, argv, check=True, timeout=60, env=None, stdin=None):
        result = subprocess.run(argv, input=stdin, capture_output=True, timeout=timeout,
                                env=env if env is not None else {'PATH': PATH, 'HOME': '/root'})
        if check and result.returncode:
            refuse('command failed (%d): %s: %s' % (result.returncode, ' '.join(map(str, argv[:6])),
                                                   result.stderr.decode(errors='replace')[-2000:]))
        return result

    def bash(self, script, check=True, timeout=60):
        return self.run(['bash', '-euo', 'pipefail', '-c', script], check=check, timeout=timeout)

    def net(self, *argv):
        return ['nsenter', '--net=/proc/%d/ns/net' % self.holder.pid, *map(str, argv)]

    def spawn(self, name, argv):
        log = (self.logs / (name + '.log')).open('ab')
        process = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                                   env={'PATH': PATH, 'HOME': '/root'}, start_new_session=True)
        self.processes[name] = process
        return process

    def stop(self, name):
        process = self.processes.pop(name, None)
        if process is None:
            return
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)

    def close(self):
        for name in list(self.processes):
            self.stop(name)
        if self.holder is not None and self.holder.poll() is None:
            self.holder.kill()
            self.holder.wait(timeout=10)

    def wait_port(self, host, port, seconds=20):
        probe = 'import socket,sys\ns=socket.create_connection((sys.argv[1],int(sys.argv[2])),timeout=1)\ns.close()'
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            if self.run(self.net('python3', '-c', probe, host, port), check=False).returncode == 0:
                return
            time.sleep(0.2)
        refuse('nothing listens on %s:%s' % (host, port))

    # The inventory before any rollout: the candidate revision, every local
    # branch carrying changes to this task's files that the candidate lacks,
    # and the actual retained service/material state read from disk.
    def inventory(self, label):
        rows = []
        for path in sorted(self.state.rglob('*')) if self.state.exists() else []:
            info = path.lstat()
            rows.append({'path': str(path.relative_to(self.state)), 'uid': info.st_uid, 'gid': info.st_gid,
                         'mode': oct(info.st_mode & 0o7777),
                         'sha256': digest(path) if path.is_file() and not path.is_symlink() else None})
        return {'label': label, 'state': rows}

    def unmerged(self):
        branches = []
        for ref in git('for-each-ref', '--format=%(refname:short)', 'refs/heads').splitlines():
            changed = git('log', '--format=%H', 'HEAD..' + ref, '--', *TOUCHES)
            if changed:
                branches.append({'branch': ref, 'commits': changed.splitlines()})
        return branches

    def issue(self, ca, service, row, out):
        _, _, _, _, cn, eku, sans = row
        sans = '' if sans == '-' else sans.replace('<app>', self.app)
        out.mkdir(mode=0o700)
        script = '\n'.join([
            shell_function(CA, 'sign'),
            'cert_days=1', 'ca_dir=' + shlex.quote(str(ca)), 'work=' + shlex.quote(str(out)),
            'cd "$work"',
            'openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out key.pem 2>/dev/null',
            'openssl req -new -key key.pem -subj ' + shlex.quote('/O=Paxeer X Network/CN=' + cn) + ' -out csr.pem',
            'sign %s %s %s' % (shlex.quote(service), shlex.quote(eku), shlex.quote(sans)),
            'cp "$ca_dir/ca.pem" ca.pem',
            'openssl x509 -in cert.pem -outform DER -out cert.der',
            'openssl pkcs8 -topk8 -nocrypt -in key.pem -outform DER -out key.der',
            'openssl x509 -in ca.pem -outform DER -out ca.der',
            'rm -f csr.pem ext.cnf', 'chmod 0600 ./*'])
        self.bash(script)
        return out

    def usage_matches(self, ca, service, cert):
        script = '\n'.join([
            shell_function(CA, 'ca_services'), shell_function(CA, 'service_row'),
            shell_function(CA, 'certificate_usage_matches'),
            'certificate_usage_matches %s "$(cat %s)" %s %s' % (
                shlex.quote(service), shlex.quote(str(cert)), shlex.quote(str(ca)), shlex.quote(self.app))])
        return self.bash(script, check=False).returncode == 0

    def authority(self, name):
        ca = self.mkdir(name, 0, 0, 0o700, self.work)
        self.run(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(ca / 'ca.key')])
        self.run(['openssl', 'req', '-x509', '-new', '-key', str(ca / 'ca.key'), '-days', '1',
                  '-subj', '/O=Paxeer X Network/CN=' + name, '-addext', 'basicConstraints=critical,CA:TRUE',
                  '-addext', 'keyUsage=critical,keyCertSign,cRLSign', '-out', str(ca / 'ca.pem')])
        return ca

    def ca_cases(self):
        listed = self.run(['bash', str(CA), 'services']).stdout.decode().splitlines()
        rows = {line.split()[0]: line.split() for line in listed if line.split()}
        self.app = re.search(r'^app = "([^"]+)"', (ROOT / 'human/wallet/deploy/human.toml').read_text(), re.M).group(1)
        host = self.material.HUMAN_KMS_ENDPOINT.rsplit(':', 1)[0]
        want = {'human-kms': ('layerx-human-kms', 'serverAuth'),
                'human-kms-client': ('layerx-human-components', 'clientAuth'),
                'human-kms-executor': ('layerx-human-movement', 'clientAuth')}
        for service, (cn, eku) in want.items():
            row = rows.get(service)
            require(row is not None and len(row) == 7 and row[1] == 'human/wallet/deploy/human.toml'
                    and row[3] == 'volume' and row[4] == cn and row[5] == eku,
                    'CA catalog row %s is not the exact Human KMS identity' % service)
            require((row[6] == '-') == (eku == 'clientAuth'), 'CA catalog row %s SAN policy' % service)
        sans = rows['human-kms'][6].split(',')
        require('DNS:' + self.material.HUMAN_KMS_SERVER_NAME in sans and 'IP:' + host in sans,
                'the Human KMS server row does not name the movement endpoint and server name')
        require(len({rows[s][4] for s in IDENTITIES}) == 3, 'Human KMS identities share a common name')
        self.case('ca-catalog-exact-human-kms-rows')
        self.ca = self.authority('ca')
        foreign = self.authority('foreign-ca')
        issued = self.mkdir('issued', 0, 0, 0o700, self.work)
        for service in IDENTITIES:
            self.issue(self.ca, service, rows[service], issued / service)
        self.issue(self.ca, 'paxeer-boundary-loopback', rows['paxeer-boundary-loopback'], issued / 'paxeer-origin')
        self.issue(foreign, 'human-kms-executor', rows['human-kms-executor'], issued / 'foreign-executor')
        stripped = list(rows['human-kms'])
        stripped[6] = 'DNS:<app>.internal,DNS:localhost,IP:127.0.0.1'
        self.issue(self.ca, 'human-kms', stripped, issued / 'nameless-server')
        for service in IDENTITIES:
            directory = issued / service
            require(self.usage_matches(self.ca, service, directory / 'cert.pem'),
                    'issued %s does not satisfy its own exact row' % service)
            for other in IDENTITIES:
                if other != service:
                    require(not self.usage_matches(self.ca, other, directory / 'cert.pem'),
                            'issued %s was admitted as %s' % (service, other))
            for name in ('cert.der', 'key.der', 'ca.der', 'key.pem'):
                info = (directory / name).stat()
                require((info.st_uid, info.st_mode & 0o777) == (0, 0o600), 'issued %s/%s not private' % (service, name))
            require((directory.stat().st_mode & 0o777) == 0o700, 'issued %s directory not private' % service)
            require((directory / 'ca.der').read_bytes() == (issued / 'human-kms' / 'ca.der').read_bytes(),
                    'Human KMS identities chain to different trust roots')
        require(not self.usage_matches(self.ca, 'human-kms-executor', issued / 'foreign-executor/cert.pem'),
                'a foreign-CA executor was admitted')
        require(not self.usage_matches(self.ca, 'human-kms', issued / 'nameless-server/cert.pem'),
                'a server certificate without the KMS server name was admitted')
        self.case('ca-issuance-identity-by-identity-trust')
        # The kernel's TLS layout, as ca.sh leaves it on the volume.
        self.tls = self.mkdir('tls', 0, 0, 0o711)
        for service in IDENTITIES:
            shutil.copytree(issued / service, self.tls / service)
        shutil.copytree(issued / 'paxeer-origin', self.tls / 'paxeer-boundary-loopback')
        self.issued = issued

    def material_refusals(self):
        scratch = self.mkdir('refusals', 0, 0, 0o700, self.work)
        for label, mutate in [('same-service-and-executor', lambda t: shutil.copyfile(t / 'human-kms-client/cert.der', t / 'human-kms-executor/cert.der')),
                              ('foreign-executor-trust-root', lambda t: shutil.copyfile(self.issued / 'foreign-executor/ca.der', t / 'human-kms-executor/ca.der'))]:
            case = self.mkdir(label, 0, 0, 0o700, scratch)
            tls = case / 'tls'
            shutil.copytree(self.tls, tls)
            mutate(tls)
            state = self.mkdir('state', 0, 0, 0o700, case)
            out = self.mkdir('kms-prerequisite', 0, 0, 0o700, case) / 'material'
            try:
                self.material.kms_prerequisite(self.registry, tls, out, self.network, self.asset, state)
            except (ValueError, OSError):
                require(not out.exists(), label + ' published material while refusing')
            else:
                refuse(label + ' KMS material was admitted')
        self.case('kms-material-identity-and-trust-root-refusals')

    def kernel_layout(self):
        # The full-profile directories of kernel init, at their declared owners.
        self.state = self.mkdir('human-state', 0, 4020, 0o750)
        for name in ('components', 'identity', 'security', 'movement'):
            self.mkdir(name, 4020, 4020, 0o700, self.state)
        self.mkdir('movement/evidence', 4020, 4020, 0o700, self.state)
        self.mkdir('kms', 4026, 4020, 0o700, self.state)
        self.human_material = self.mkdir('human-material', 0, 4020, 0o751)
        self.sockets = self.mkdir('run-human', 4020, 4020, 0o750)
        self.genesis = self.mkdir('genesis', 0, 0, 0o700)
        (self.genesis / 'asset-id').write_text(self.asset)
        bin_dir = self.mkdir('bin', 0, 0, 0o755)
        shutil.copyfile(ROOT / 'docker/human-service/entrypoint.sh', bin_dir / 'human-entrypoint')
        (bin_dir / 'human-entrypoint').chmod(0o755)
        require(digest(bin_dir / 'human-entrypoint') == digest(ROOT / 'docker/human-service/entrypoint.sh'),
                'entrypoint copy differs')
        self.binaries = {'layerx-runtime-clock': self.built['clock']['path'],
                         'layerx-human-kms': self.built['kms']['path'],
                         'layerx-human-movement-provider': self.built['provider']['path'],
                         'human-entrypoint': str(bin_dir / 'human-entrypoint')}

    def init_globals(self, state=None, tls=None):
        state, tls = state or self.state, tls or self.tls
        return '\n'.join([
            'human_state=' + shlex.quote(str(state)), 'human_material=' + shlex.quote(str(self.human_material)),
            'tls=' + shlex.quote(str(tls)), 'genesis=' + shlex.quote(str(self.genesis)),
            'LAYERX_NODE_NETWORK_ID=%d' % self.network,
            'human_kms_source=' + shlex.quote(str(self.registry)),
            init_assignment('human_kms_out'), init_assignment('human_out'),
            init_assignment('human_kms_listen'), init_assignment('human_kms_server_name'),
            init_assignment('human_kms_provider')])

    def kms_prepare(self, state=None):
        script = '\n'.join([
            'mount --bind %s /usr/local/lib' % shlex.quote(str(self.libdir)),
            self.init_globals(state), shell_function(INIT, 'human_kms_prepare'), 'human_kms_prepare'])
        return self.run(['unshare', '--mount', '--propagation', 'private', '--', 'bash', '-euo', 'pipefail', '-c', script],
                        check=False, timeout=60)

    def namespace(self, material, state, uid, environment, command):
        """service() of kernel init: the role's material at /run/human-material,
        its state at /var/lib/layerx/human and the private runtime directories
        of the kernel initializer, then the command under the role uid."""
        binds = ' '.join(': > /usr/local/bin/%s && mount --bind %s /usr/local/bin/%s;' % (name, shlex.quote(path), name)
                         for name, path in self.binaries.items())
        script = '\n'.join([
            'mount -t tmpfs -o mode=0755 tmpfs /run',
            'mount -t tmpfs -o mode=0755 tmpfs /usr/local/bin',
            'mount -t tmpfs -o mode=0755 tmpfs /var/lib',
            'install -d -m 0755 /run/human-material /var/lib/layerx/human /run/layerx/human',
            binds,
            shell_function(INIT, 'private_runtime_directories'), 'private_runtime_directories',
            'mount --bind %s /run/layerx/human' % shlex.quote(str(self.sockets)),
            'mount --bind %s /run/human-material' % shlex.quote(str(material)),
            'mount --bind %s /var/lib/layerx/human' % shlex.quote(str(state)),
            'exec setpriv --reuid=%d --regid=4020 --clear-groups --no-new-privs --pdeathsig TERM env -i PATH=%s %s %s' % (
                uid, PATH, ' '.join(shlex.quote(k + '=' + v) for k, v in environment), command)])
        return self.net('unshare', '--mount', '--propagation', 'private', '--', 'bash', '-euo', 'pipefail', '-c', script)

    def init_environment(self, name, prefix):
        match = service_block(name)
        definitions = '\n'.join(['run=/run/layerx', init_assignment('human_paxeer_urls'),
                                 init_assignment('human_kms_listen'), init_assignment('human_kms_provider')])
        tokens = re.findall(prefix + r'[A-Z0-9_]+=(?:"[^"]*"|\S+)', match.group(2))
        output = self.bash(definitions + '\nprintf "%s\\0" ' + ' '.join(tokens)).stdout.decode()
        return [tuple(item.split('=', 1)) for item in output.split('\0') if item], match

    def start_kms(self):
        environment, match = self.init_environment('human-kms', 'LAYERX_HUMAN_KMS_')
        require(match.group(1) == '4026' and match.group(3) == 'kms', 'the kernel KMS service is not the kms role under uid 4026')
        values = dict(environment)
        require(values.get('LAYERX_HUMAN_KMS_LISTEN') == self.material.HUMAN_KMS_ENDPOINT
                and values.get('LAYERX_HUMAN_KMS_PROVIDER_REFERENCE') == self.material.HUMAN_KMS_PROVIDER
                and values.get('LAYERX_HUMAN_KMS_STATE_DIR') == '/var/lib/layerx/human'
                and values.get('LAYERX_HUMAN_KMS_EVM_CLIENT_CERT_DER') == '/run/human-private/kms/kms-executor.der'
                and values.get('LAYERX_HUMAN_KMS_CLIENT_CERT_DER') == '/run/human-private/kms/kms-client.der',
                'the kernel KMS launch does not serve the movement endpoint and identities')
        self.spawn('kms', self.namespace(self.human_material / 'human-kms', self.state / 'kms', 4026, environment,
                                         '/usr/local/bin/human-entrypoint kms'))
        deadline = time.monotonic() + 30
        while self.kms_call(self.request(0), check=False) != b'LXKP\x00\x01\x00\x00':
            require(self.processes['kms'].poll() is None, 'the real KMS role exited; log %s' % (self.logs / 'kms.log'))
            require(time.monotonic() < deadline, 'the real KMS role did not answer')
            time.sleep(0.25)

    def request(self, operation, reference=b''):
        provider = self.material.HUMAN_KMS_PROVIDER.encode()
        frame = b'LXKP\x00\x01' + bytes([operation]) + len(provider).to_bytes(4, 'big') + provider
        if operation:
            frame += self.binding + self.network.to_bytes(4, 'big') + b'\x01' + len(reference).to_bytes(4, 'big') + reference
        return frame

    def kms_call(self, frame, identity='human-kms-client', check=True):
        host, port = self.material.HUMAN_KMS_ENDPOINT.rsplit(':', 1)
        client = '''import socket,ssl,struct,sys
ctx=ssl.create_default_context(cafile=sys.argv[1]+'/ca.pem')
ctx.load_cert_chain(sys.argv[1]+'/cert.pem',sys.argv[1]+'/key.pem')
ctx.minimum_version=ssl.TLSVersion.TLSv1_3
payload=bytes.fromhex(sys.argv[5])
with socket.create_connection((sys.argv[2],int(sys.argv[3])),timeout=3) as tcp:
 with ctx.wrap_socket(tcp,server_hostname=sys.argv[4]) as tls:
  tls.sendall(struct.pack('>I',len(payload))+payload)
  def read(n):
   out=b''
   while len(out)<n:
    chunk=tls.recv(n-len(out))
    if not chunk: raise RuntimeError('closed')
    out+=chunk
   return out
  size=struct.unpack('>I',read(4))[0]
  if not 0<size<=2097152: raise RuntimeError('frame bounds')
  print(read(size).hex())
'''
        answer = self.run(self.net('python3', '-c', client, self.issued / identity, host, port,
                                   self.material.HUMAN_KMS_SERVER_NAME, frame.hex()), check=check)
        return bytes.fromhex(answer.stdout.decode().strip()) if answer.returncode == 0 else None

    def movement_material(self, label, executor):
        """The movement projection of human_project: the role directory and its
        env/ readable by uid 4020, each file root-owned, group 4020, 0440."""
        config = self.mkdir('movement-config-' + label, 0, 0, 0o700, self.work)
        values = self.material.movement_defaults(self.network, 125)
        values.update({'PAXEER_VAULT': self.material.CUSTODY_PRECOMPILE,
                       'PAXEER_CLAIMS_CONTRACT': self.material.CUSTODY_PRECOMPILE,
                       'PAXEER_EXIT_CONTRACT': self.material.CUSTODY_PRECOMPILE,
                       'PAXEER_CHECKPOINT_REGISTRY': '0x' + '0c' * 20,
                       'CUSTODY_PROFILE': '/run/human-private/movement/custody.profile',
                       'CUSTODY_PROFILE_SHA256': '0x' + hashlib.sha256(self.profile).hexdigest(),
                       'PAXEER_CHECKPOINT_AUTHORITY': self.checkpoint_authority,
                       'CUSTODY_REFERENCE': '0x' + '09' * 32, 'PAXEER_CONFIRMATIONS': 2,
                       'CHECKPOINT_INTERVAL_SECONDS': 10, 'PAXEER_BLOCK_SECONDS': 1,
                       'REMINDER_INTERVAL_SECONDS': 60})
        for key, value in values.items():
            self.material.write(config, 'LAYERX_HUMAN_MOVEMENT_PROVIDER_' + key, value)
        directory = self.human_material / ('human-movement' if label == 'assembly' else 'human-movement-' + label)
        if directory.exists():
            shutil.rmtree(directory)
        directory.mkdir(mode=0o500)
        os.chown(directory, 4020, 4020)
        env = directory / 'env'
        env.mkdir(mode=0o500)
        os.chown(env, 4020, 4020)
        for path in config.iterdir():
            shutil.copyfile(path, env / path.name)
            os.chown(env / path.name, 0, 4020)
            (env / path.name).chmod(0o440)
        sources = {'ca.der': self.tls / 'human-kms/ca.der', 'custody.profile': self.profile_path}
        if executor is not None:
            sources.update({'kms-executor.der': self.issued / executor / 'cert.der',
                            'kms-executor-key.der': self.issued / executor / 'key.der'})
        for name, path in sources.items():
            shutil.copyfile(path, directory / name)
            os.chown(directory / name, 0, 4020)
            (directory / name).chmod(0o440)
        return config, directory

    def start_movement(self, name, directory, state):
        environment, match = self.init_environment('human-movement', 'LAYERX_HUMAN_MOVEMENT_PROVIDER_')
        require(match.group(1) == '4020' and match.group(3) == 'movement', 'the kernel movement service is not the movement role')
        human_env = re.search(r"^human_env='(.*)'$", INIT.read_text(), re.M).group(1)
        command = '/bin/sh -ec %s sh env %s /usr/local/bin/human-entrypoint movement' % (
            shlex.quote(human_env), ' '.join(shlex.quote(k + '=' + v) for k, v in environment))
        socket_path = self.sockets / 'movement.sock'
        if socket_path.exists():
            socket_path.unlink()
        return self.spawn(name, self.namespace(directory, state, 4020, [], command))

    def ready(self):
        environment = [('LAYERX_HUMAN_MOVEMENT_PROVIDER_SOCKET', '/run/layerx/human/movement.sock'),
                       ('LAYERX_HUMAN_MOVEMENT_PROVIDER_DEADLINE_SECONDS', '10'),
                       ('LAYERX_HUMAN_MOVEMENT_PROVIDER_MAX_FRAME_BYTES', '1048576'),
                       ('LAYERX_HUMAN_MOVEMENT_PROVIDER_PROTOCOL_VERSION', '3')]
        argv = self.namespace(self.human_material / 'human-movement', self.state / 'movement', 4020, environment,
                              '/usr/local/bin/layerx-human-movement-provider probe')
        result = self.run(argv, check=False, timeout=60)
        require(result.returncode in (0, 1) and not result.stdout, 'the readiness probe answered %d' % result.returncode)
        return result.returncode == 0

    def ready_within(self, process, seconds):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            require(process.poll() is None, 'the movement role exited; logs %s' % self.logs)
            if (self.sockets / 'movement.sock').exists() and self.ready():
                return True
            time.sleep(0.5)
        return False

    def chain(self):
        anvil = '/root/.foundry/bin/anvil' if Path('/root/.foundry/bin/anvil').is_file() else shutil.which('anvil')
        self.spawn('anvil', self.net(anvil, '--port', '18545', '--chain-id', '125', '--silent'))
        self.wait_port('127.0.0.1', 18545)
        origin = self.tls / 'paxeer-boundary-loopback'
        urls = json.loads(re.search(r"^human_paxeer_urls='(.*)'$", INIT.read_text(), re.M).group(1))
        for index, url in enumerate(urls):
            host, port = re.fullmatch(r'https://([^:/]+):(\d+)', url).groups()
            for bind, family in ([('127.0.0.1', 'pf=ip4'), ('[::1]', 'pf=ip6')] if host == 'localhost' else [(host, 'pf=ip4')]):
                self.spawn('origin-%d-%s' % (index, family),
                           self.net('socat', 'OPENSSL-LISTEN:%s,bind=%s,%s,reuseaddr,fork,cert=%s,key=%s,verify=0' % (
                               port, bind, family, origin / 'cert.pem', origin / 'key.pem'), 'TCP:127.0.0.1:18545'))
            self.wait_port('127.0.0.1' if host == 'localhost' else host, int(port))

    def binding_check(self, state, tls=None):
        script = '\n'.join([self.init_globals(state, tls), shell_function(INIT, 'human_movement_kms_binding'),
                            'human_movement_kms_binding'])
        return self.bash(script, check=False).returncode == 0

    def execute(self):
        self.work = self.mkdir('work', 0, 0, 0o700)
        self.profile_path = ROOT / 'tests/fixtures/custody/native-credit-receipt/profile'
        self.profile = self.profile_path.read_bytes()
        require(len(self.profile) == 223 and self.profile[:5] == b'LXBC3' and int.from_bytes(self.profile[5:13], 'big') == 125,
                'the protocol-3 custody profile fixture is not a Paxeer custody profile')
        self.network = int.from_bytes(self.profile[201:205], 'big')
        self.asset = self.profile[97:129].hex()
        self.binding = os.urandom(32)
        key = self.work / 'checkpoint-authority.pem'
        self.run(['openssl', 'genpkey', '-algorithm', 'ed25519', '-out', str(key)])
        self.checkpoint_authority = '0x' + self.run(['openssl', 'pkey', '-in', str(key), '-pubout', '-outform', 'DER']).stdout[-32:].hex()
        self.registry = self.work / 'module-registry.json'
        self.registry.write_text(json.dumps({'schema_version': 2, 'assets': [{'asset': self.asset}],
                                             'modules': [{'module': 1, 'ordinals': [1, 2]}]}))
        self.libdir = self.mkdir('lib', 0, 0, 0o755)
        shutil.copytree(ROOT / 'platform/hosted/human', self.libdir / 'layerx-human')

        self.kernel_layout()
        inventory = {'revision': self.revision, 'unmerged_branches': self.unmerged(),
                     'before': self.inventory('before-rollout')}
        require(inventory['before']['state'] and all(row['sha256'] is None for row in inventory['before']['state']),
                'the isolated retained state is not the empty kernel layout')
        self.material.write_bytes(self.evidence / 'inventory.json', json.dumps(inventory, indent=2).encode())
        self.case('inventory-before-rollout-from-actual-state')

        self.ca_cases()
        self.material_refusals()

        endpoint = init_assignment('human_kms_listen').split('=', 1)[1]
        server_name = init_assignment('human_kms_server_name').split('=', 1)[1]
        provider = init_assignment('human_kms_provider').split('=', 1)[1]
        require((endpoint, server_name, provider) == (self.material.HUMAN_KMS_ENDPOINT, self.material.HUMAN_KMS_SERVER_NAME,
                                                      self.material.HUMAN_KMS_PROVIDER)
                and self.material.component_defaults(self.network, 125)['KMS_ENDPOINT'] == endpoint
                and self.material.component_defaults(self.network, 125)['KMS_SERVER_NAME'] == server_name,
                'kernel KMS listener, producer movement endpoint and server name differ')
        waits = service_block('human-movement').group(2).split('\n')[0]
        for needed in ('$tls/human-kms/cert.der', '$tls/human-kms-executor/cert.der', '$tls/human-kms-executor/key.der',
                       '$tls/human-kms-executor/ca.der', '$human_kms_out/kms-seal', '$human_kms_out/registry.json',
                       '$human_kms_out/kms-executor.der'):
            require(needed in waits, 'movement does not wait for ' + needed)
        self.case('movement-endpoint-server-name-and-waits-bound-to-kms')

        self.holder = subprocess.Popen(['unshare', '--net', '--', 'sleep', '3600'], stdin=subprocess.DEVNULL)
        deadline = time.monotonic() + 10
        while os.readlink('/proc/%d/ns/net' % self.holder.pid) == os.readlink('/proc/self/ns/net'):
            require(self.holder.poll() is None and time.monotonic() < deadline, 'the private network namespace did not start')
            time.sleep(0.05)
        self.run(self.net('ip', 'link', 'set', 'lo', 'up'))
        self.chain()

        # Movement before any KMS material: the role cannot start, and the
        # kernel's binding check keeps it waiting.
        human_out = self.state / 'material/human'
        human_out.mkdir(mode=0o700, parents=True)
        config, directory = self.movement_material('assembly', 'human-kms-executor')
        shutil.copytree(config, human_out / 'movement-config')
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
        self.case('movement-waits-without-kms-material')

        prepared = self.kms_prepare()
        require(prepared.returncode == 0, 'kernel human_kms_prepare failed: ' + prepared.stderr.decode(errors='replace')[-2000:])
        kms_out = self.state / 'kms-prerequisite/material'
        seal = digest(kms_out / 'kms-seal')
        projected = self.human_material / 'human-kms'
        info = projected.stat()
        require((info.st_uid, info.st_gid, info.st_mode & 0o777) == (4026, 4020, 0o500), 'KMS projection directory ownership')
        for path in projected.iterdir():
            info = path.stat()
            require((info.st_uid, info.st_gid, info.st_mode & 0o777) == (0, 4020, 0o440), 'KMS projected file ownership ' + path.name)
        require((projected / 'kms-executor.der').read_bytes() == (self.issued / 'human-kms-executor/cert.der').read_bytes()
                and (projected / 'kms-client.der').read_bytes() == (self.issued / 'human-kms-client/cert.der').read_bytes()
                and (projected / 'kms-server.der').read_bytes() == (self.issued / 'human-kms/cert.der').read_bytes(),
                'KMS projection does not pin the issued identities')
        require(self.binding_check(self.state), 'kernel movement binding refused the exact KMS service')
        for label, path, value in [('server-name', 'LAYERX_HUMAN_MOVEMENT_PROVIDER_KMS_SERVER_NAME', 'wrong-server'),
                                   ('endpoint', 'LAYERX_HUMAN_MOVEMENT_PROVIDER_KMS_ENDPOINT', '127.0.0.1:9451')]:
            target = human_out / 'movement-config' / path
            original = target.read_bytes()
            target.write_text(value)
            require(not self.binding_check(self.state), 'movement binding admitted a wrong KMS ' + label)
            target.write_bytes(original)
        swapped = self.work / 'swapped-tls'
        shutil.copytree(self.tls, swapped)
        shutil.copyfile(self.issued / 'human-kms-client/cert.der', swapped / 'human-kms-executor/cert.der')
        require(not self.binding_check(self.state, swapped), 'movement binding admitted the service identity as executor')
        self.case('kernel-kms-material-projection-and-movement-binding')

        movement_state = self.state / 'movement'
        script = '\n'.join([self.init_globals(), shell_function(INIT, 'human_movement_state'), 'human_movement_state'])
        self.bash(script)
        info = (movement_state / 'movement').stat()
        require((info.st_uid, info.st_gid, info.st_mode & 0o777) == (4020, 4020, 0o700), 'movement journal root ownership')

        movement = self.start_movement('movement', directory, movement_state)
        deadline = time.monotonic() + 30
        while not (self.sockets / 'movement.sock').exists():
            require(movement.poll() is None, 'movement exited before binding; log %s' % (self.logs / 'movement.log'))
            require(time.monotonic() < deadline, 'movement never bound its socket')
            time.sleep(0.2)
        require(not self.ready(), 'movement reported ready while its KMS service is absent')
        self.case('movement-unready-without-kms-service')

        self.start_kms()
        self.case('kernel-kms-entrypoint-runtime-clock-uid-4026-startup')
        require(self.ready_within(movement, 30), 'movement did not become ready with its authenticated KMS service')
        self.case('movement-ready-through-authenticated-kms-executor')

        created = self.kms_call(self.request(1))
        require(created is not None and len(created) == 109 and created[:12] == b'LXKP\x00\x01\x01\x00\x00\x00\x00\x20',
                'the service identity could not create a custody key')
        reference, public = created[12:44], created[44:76]
        require(self.kms_call(self.request(1), 'human-kms-executor') == b'LXKP\x00\x01\x01\x01',
                'the restricted executor created a service key')
        self.case('service-identity-authorized-and-executor-restricted')

        state_files = sorted(p.name for p in (self.state / 'kms').iterdir())
        self.stop('kms')
        require(not self.ready(), 'movement stayed ready after its KMS service stopped')
        require(movement.poll() is None, 'movement exited instead of reporting unready')
        self.case('movement-unready-on-kms-loss')

        prepared = self.kms_prepare()
        require(prepared.returncode == 0, 'retained KMS material was refused on restart')
        require(digest(kms_out / 'kms-seal') == seal, 'restart replaced the retained KMS seal')
        self.start_kms()
        described = self.kms_call(self.request(2, reference))
        require(described is not None and described[7] == 0 and described[12:44] == reference and described[44:76] == public,
                'restart did not recover the retained custody key')
        require(sorted(p.name for p in (self.state / 'kms').iterdir()) == state_files, 'restart generated replacement KMS state')
        require(self.ready_within(movement, 30), 'movement did not recover readiness after the KMS restart')
        self.case('kms-restart-recovers-retained-keys-and-movement-readiness')

        orphan = self.mkdir('orphan-state', 0, 4020, 0o750)
        shutil.copytree(self.state / 'kms', orphan / 'kms', symlinks=True)
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
        self.case('movement-restart-recovers-retained-state')

        self.stop('movement')
        _, wrong = self.movement_material('wrong-executor', 'human-kms-client')
        wrong_state = self.mkdir('wrong-movement', 4020, 4020, 0o700)
        self.mkdir('movement', 4020, 4020, 0o700, wrong_state)
        self.mkdir('evidence', 4020, 4020, 0o700, wrong_state)
        process = self.start_movement('movement-wrong', wrong, wrong_state)
        deadline = time.monotonic() + 30
        while not (self.sockets / 'movement.sock').exists():
            require(process.poll() is None, 'wrong-identity movement exited before binding')
            require(time.monotonic() < deadline, 'wrong-identity movement never bound its socket')
            time.sleep(0.2)
        require(not self.ready(), 'movement reported ready with the service identity as executor')
        self.case('movement-unready-with-non-executor-identity')
        self.stop('movement-wrong')

        after = self.inventory('after-rollout')
        self.material.write_bytes(self.evidence / 'inventory-after.json', json.dumps(after, indent=2).encode())


def main():
    if sys.argv[1:] == ['--build']:
        build()
        return 0
    if sys.argv[1:]:
        refuse('expected no arguments or --build')
    os.umask(0o077)
    revision, built = prerequisites()
    gate = Gate(revision, built)
    try:
        gate.execute()
    finally:
        gate.close()
    result = {'schema': SCHEMA, 'revision': revision, 'cases': gate.cases, 'tests': len(gate.cases), 'skipped': 0}
    gate.material.write_bytes(gate.evidence / 'result.json', json.dumps(result, indent=2).encode())
    print('PAXEER_X_GATE tests=%d skipped=0 evidence=%s' % (len(gate.cases), gate.evidence))
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, RuntimeError, AttributeError, subprocess.SubprocessError) as error:
        print('human movement KMS assembly refused:', error, file=sys.stderr)
        sys.exit(1)
