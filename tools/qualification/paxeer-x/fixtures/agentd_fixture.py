import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import ssl
import stat
import subprocess
import time

ROOT = Path(__file__).resolve().parents[4]
REQUIRED = {'layerx-agentd', 'layerxd', 'layerx-receipt-authority', 'layerx-program-registry'}
ALLOWED = REQUIRED | {'paxd', 'layerx-runtime-clock', 'layerx-human-service', 'layerxctl'}


class FixtureRefused(Exception):
    pass


def require(condition, reason):
    if not condition:
        raise FixtureRefused('agentd fixture refused: ' + reason)


def private_json(raw, name):
    require(bool(raw), name + ' is required')
    path = Path(raw)
    require(path.is_absolute() and path.resolve() == path, name + ' must be canonical')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
                and not info.st_mode & 0o077, name + ' must be owner-only')
        return json.load(stream)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def artifacts(document):
    rows = document.get('artifacts', {})
    require(REQUIRED <= set(rows) <= ALLOWED, 'required native service artifacts are missing')
    revision = subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip()
    for name, row in rows.items():
        path = Path(row['path'])
        require(path.is_absolute() and path.is_file() and os.access(path, os.X_OK),
                'missing executable ' + name)
        require(row.get('source_revision') == revision and row.get('sha256') == digest(path),
                'artifact source/digest mismatch ' + name)
        with path.open('rb') as stream:
            require(stream.read(4) == b'\x7fELF', 'artifact is not a native executable: ' + name)
    return rows


class AgentdFixture:
    def __init__(self, directory):
        self.directory = Path(directory).resolve()
        require(not self.directory.exists(), 'fixture directory already exists')
        self.directory.mkdir(mode=0o700, parents=True)
        self.processes = []
        self.values = {'fixture': str(self.directory)}
        self.document = private_json(os.environ.get('PAXEER_X_AGENTD_UPSTREAM_ARTIFACTS'),
                                     'PAXEER_X_AGENTD_UPSTREAM_ARTIFACTS')
        require(self.document.get('schema') == 'paxeer-x.agentd-http-fixture.v1', 'fixture schema')
        self.artifacts = artifacts(self.document)
        self.config = None

    def substitute(self, values):
        require(isinstance(values, dict) and all(isinstance(v, str) for v in values.values()),
                'environment must be a string mapping')
        return {key: value.format_map(self.values) for key, value in values.items()}

    def seed(self):
        seeds = self.document.get('seeds')
        require(isinstance(seeds, dict) and seeds, 'provisioned durable state is required')
        for name, raw in seeds.items():
            require(re.fullmatch('[a-z][a-z0-9_]*', name) and name not in self.values, 'invalid seed name')
            source = Path(raw)
            require(source.is_absolute() and source.resolve() == source and source.exists(),
                    'missing canonical seed ' + name)
            paths = [source] + (list(source.rglob('*')) if source.is_dir() else [])
            for path in paths:
                info = path.lstat()
                require(not path.is_symlink() and info.st_uid == os.geteuid() and not info.st_mode & 0o077
                        and (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)),
                        'unsafe provisioned seed ' + name)
            destination = self.directory / name
            if source.is_dir():
                shutil.copytree(source, destination)
            else:
                shutil.copyfile(source, destination)
            for path in [destination] + (list(destination.rglob('*')) if destination.is_dir() else []):
                path.chmod(0o700 if path.is_dir() else 0o600)
            self.values[name] = str(destination)
        for name in ('program_bearer', 'node_bearer', 'authority_bearer'):
            self.values[name] = secrets.token_hex(32)
        reserved = []
        try:
            for name in self.document.get('ports', []):
                require(re.fullmatch('[a-z][a-z0-9_]*', name) and name not in self.values, 'invalid port name')
                probe = socket.socket()
                reserved.append(probe)
                probe.bind(('127.0.0.1', 0))
                self.values[name] = str(probe.getsockname()[1])
        finally:
            for probe in reserved:
                probe.close()

    def tls_material(self):
        tls = self.directory / 'tls'
        tls.mkdir(mode=0o700)
        with (self.directory / 'tls.log').open('wb') as log:
            def openssl(*args):
                subprocess.run(['openssl', *map(str, args)], check=True, stdout=log, stderr=log,
                               timeout=30)
            for ca in ('ca', 'untrusted-ca'):
                openssl('req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256',
                        '-nodes', '-days', '1', '-subj', '/CN=' + ca,
                        '-addext', 'basicConstraints=critical,CA:TRUE',
                        '-addext', 'keyUsage=critical,keyCertSign',
                        '-keyout', tls / (ca + '.key'), '-out', tls / (ca + '.pem'))
            for name, ca, identity, usage in (
                ('server', 'ca', 'localhost', 'serverAuth'),
                ('client', 'ca', 'agent-http-peer.invalid', 'clientAuth'),
                ('wrong-peer', 'ca', 'other-peer.invalid', 'clientAuth'),
                ('untrusted', 'untrusted-ca', 'agent-http-peer.invalid', 'clientAuth'),
            ):
                openssl('req', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes',
                        '-subj', '/CN=' + identity, '-keyout', tls / (name + '.key'),
                        '-out', tls / (name + '.csr'))
                extension = tls / (name + '.ext')
                extension.write_text('subjectAltName=DNS:' + identity + '\nextendedKeyUsage='
                                     + usage + '\nbasicConstraints=CA:FALSE\n')
                openssl('x509', '-req', '-in', tls / (name + '.csr'), '-CA', tls / (ca + '.pem'),
                        '-CAkey', tls / (ca + '.key'), '-CAcreateserial', '-days', '1',
                        '-extfile', extension, '-out', tls / (name + '.pem'))
        for path in tls.iterdir():
            path.chmod(0o600)
        self.tls = tls

    def start(self):
        for name in ('net', 'pid', 'mnt'):
            require(os.environ.get('PAXEER_X_HTTP_PARENT_' + name.upper())
                    and os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_HTTP_PARENT_' + name.upper()],
                    'isolated fixture namespace required')
        self.seed()
        self.tls_material()
        services = self.document.get('services')
        require(isinstance(services, list) and services, 'native service launch configuration required')
        required_services = {'layerxd', 'layerx-receipt-authority', 'layerx-program-registry'}
        require(required_services <= {row.get('artifact') for row in services}, 'authority services missing')
        for index, row in enumerate(services):
            name = row.get('artifact')
            require(name in self.artifacts and name != 'layerx-agentd', 'unknown service artifact')
            arguments = row.get('arguments', [])
            require(isinstance(arguments, list) and all(isinstance(value, str) for value in arguments),
                    'service arguments are malformed')
            command = [self.artifacts[name]['path']] + [value.format_map(self.values) for value in arguments]
            environment = {'PATH': os.environ.get('PATH', '/usr/bin:/bin')}
            environment.update(self.substitute(row.get('environment', {})))
            log = (self.directory / f'service-{index}.log').open('wb')
            process = subprocess.Popen(command, cwd=self.directory, env=environment,
                                       stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT)
            self.processes.append((process, log))
            if row.get('oneshot') is True:
                require(process.wait(timeout=120) == 0, 'native initializer failed: ' + name)
                continue
            endpoint = row.get('ready_socket')
            require(isinstance(endpoint, str), 'service readiness socket required: ' + name)
            endpoint = endpoint.format_map(self.values)
            deadline = time.monotonic() + 120
            while True:
                require(process.poll() is None, 'native service stopped: ' + name)
                try:
                    if endpoint.startswith('/'):
                        with socket.socket(socket.AF_UNIX) as probe:
                            probe.settimeout(1)
                            probe.connect(endpoint)
                    else:
                        host, port = endpoint.rsplit(':', 1)
                        require(host.startswith('127.'), 'service readiness must be loopback')
                        socket.create_connection((host, int(port)), timeout=1).close()
                    break
                except OSError:
                    require(time.monotonic() < deadline, 'native service readiness deadline: ' + name)
                    time.sleep(0.1)
        config = self.substitute(self.document.get('agentd_environment', {}))
        require(config.get('LAYERX_AGENT_MODE') == 'full', 'full daemon mode required')
        for name in ('LAYERX_AGENT_HUMAN_STORE', 'LAYERX_AGENT_HUMAN_SESSION_KEY_ROOT',
                     'LAYERX_AGENT_DEPLOYMENT_JOURNAL', 'LAYERX_AGENTD_RPC_IDEMPOTENCY_ROOT'):
            path = Path(config.get(name, ''))
            require(path.is_absolute() and path.resolve() == path and self.directory in path.parents,
                    'daemon durable path must be canonical and disposable: ' + name)
        for name in ('LAYERX_AGENT_HUMAN_SOCKET', 'LAYERX_AGENT_HUMAN_NODE_LNI'):
            path = Path(config.get(name, ''))
            require(path.is_absolute() and path.resolve() == path and self.directory in path.parents,
                    'daemon socket must be canonical and disposable: ' + name)
        policies = config.get('LAYERX_POLICY_SOURCES', '')
        require(bool(policies), 'configured policies are required for the Programs owner')
        for declaration in policies.split(','):
            tenant, separator, raw_path = declaration.partition(':')
            path = Path(raw_path.strip())
            require(bool(tenant.strip()) and separator and path.is_absolute()
                    and path.resolve() == path and self.directory in path.parents,
                    'policy source must be canonical and disposable')
        require(config.get('LAYERX_AGENT_PROGRAM_BEARER_TOKEN') == self.values['program_bearer'],
                'daemon must use the generated program bearer')
        self.rpc_port = int(self.values['agent_rpc_port'])
        config.update({'LAYERX_AGENTD_RPC_LISTEN': '127.0.0.1:' + str(self.rpc_port),
                       'LAYERX_AGENTD_RPC_TLS_CERT': str(self.tls / 'server.pem'),
                       'LAYERX_AGENTD_RPC_TLS_KEY': str(self.tls / 'server.key'),
                       'LAYERX_AGENTD_RPC_TLS_CLIENT_CA': str(self.tls / 'ca.pem'),
                       'LAYERX_AGENTD_RPC_PEER': 'agent-http-peer.invalid'})
        self.config = config
        return config

    def client(self, identity='client'):
        context = ssl.create_default_context(cafile=str(self.tls / 'ca.pem'))
        if identity:
            context.load_cert_chain(str(self.tls / (identity + '.pem')), str(self.tls / (identity + '.key')))
        raw = socket.create_connection(('127.0.0.1', self.rpc_port), timeout=5)
        return context.wrap_socket(raw, server_hostname='localhost')

    def envelope(self, case):
        raw = self.document.get('requests', {}).get(case)
        require(isinstance(raw, dict), 'provisioned production request required: ' + case)
        value = json.loads(json.dumps(raw))
        require(value.get('version') == 1 and isinstance(value.get('request'), dict)
                and isinstance(value.get('credential'), dict), 'canonical RPC envelope required')
        return value

    def secret_values(self):
        values = {value for key, value in self.values.items() if key.endswith('_bearer')}
        def visit(value):
            if isinstance(value, dict):
                for child in value.values():
                    visit(child)
            elif isinstance(value, list):
                for child in value:
                    visit(child)
            elif isinstance(value, str) and len(value) >= 16:
                values.add(value)
        for envelope in self.document.get('requests', {}).values():
            visit(envelope.get('credential', {}))
        for key, value in (self.config or {}).items():
            if any(word in key for word in ('BEARER', 'TOKEN', 'PASSWORD')):
                values.add(value)
        return values

    def redact(self, message):
        for value in sorted(self.secret_values(), key=len, reverse=True):
            if value:
                message = message.replace(value, '[redacted]')
        return message

    def cleanup(self):
        for process, log in reversed(self.processes):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=10)
            log.close()
