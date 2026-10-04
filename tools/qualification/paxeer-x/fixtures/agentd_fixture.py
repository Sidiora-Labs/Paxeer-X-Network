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
ALLOWED = REQUIRED | {'paxd', 'layerx-runtime-clock', 'layerx-human-service', 'layerxctl',
                      'layerx-gateway', 'layerx-mcp'}


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

    def provision_tenant_readiness(self):
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

        profile = self.document.get('tenant_readiness')
        require(isinstance(profile, dict)
                and set(profile) in ({'schema', 'tenants', 'operator_probe'},
                                     {'schema', 'tenants', 'operator_probe', 'mcp_binding_seeds'})
                and profile['schema'] == 'paxeer-x.agentd-tenant-recovery.v1',
                'genuine two-tenant recovery profile is required')
        rows = profile['tenants']
        require(isinstance(rows, list) and len(rows) == 2, 'exactly two provisioned tenants required')
        tenants = []
        for row in rows:
            require(isinstance(row, dict) and set(row) == {
                'read_request', 'prepare_request', 'signer_seed', 'signer_public_key',
                'managed_agent_id'}, 'tenant recovery profile fields')
            read = self.envelope(row['read_request'])
            prepare = self.envelope(row['prepare_request'])
            credential = read['credential']
            require(read.get('operation') == 'read.account'
                    and prepare.get('operation') == 'prepare'
                    and prepare['credential'] == credential
                    and set(credential) == {'tenant', 'session_id', 'token_id', 'generation'},
                    'real same-owner read and prepare requests required')
            require(all(isinstance(value, str) for value in credential.values())
                    and 0 < len(credential['tenant'].encode()) <= 255
                    and '\0' not in credential['tenant']
                    and all(re.fullmatch('[0-9a-f]{64}', credential[name])
                            for name in ('session_id', 'token_id'))
                    and re.fullmatch('[1-9][0-9]{0,19}', credential['generation'])
                    and int(credential['generation']) < 2**64,
                    'provisioned credential is not canonical')
            request = prepare['request']
            require(request.get('variant') == 'native_effect_v1'
                    and request.get('activity') == {'version': '1', 'module': '1', 'ordinal': '5'}
                    and isinstance(request.get('purpose'), dict)
                    and request['purpose'].get('purpose', {}).get('tenant') == credential['tenant']
                    and request['purpose']['purpose'].get('session_id') == credential['session_id']
                    and request['purpose']['purpose'].get('generation') == credential['generation'],
                    'real consented native Send preparation bound to the retained session required')
            name = row['signer_seed']
            require(name in self.document['seeds'] and name in self.values,
                    'registered signer key must be a copied protected seed')
            path = Path(self.values[name])
            info = path.lstat()
            require(path.is_file() and not path.is_symlink() and info.st_uid == os.geteuid()
                    and not info.st_mode & 0o077 and info.st_nlink == 1 and info.st_size == 32,
                    'registered signer seed must be an owned private raw Ed25519 seed')
            signer = Ed25519PrivateKey.from_private_bytes(path.read_bytes())
            public = signer.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
            require(public == row['signer_public_key'], 'provisioned signer public identity mismatch')
            require(isinstance(row['managed_agent_id'], str) and row['managed_agent_id'],
                    'actual managed owner identifier is required')
            tenants.append({'credential': credential, 'read': read, 'prepare': prepare,
                            'signer': signer, 'public_key': public,
                            'managed_agent_id': row['managed_agent_id']})
        require(tenants[0]['credential']['tenant'] != tenants[1]['credential']['tenant'],
                'recovery case must use different actual tenants')
        probe = profile['operator_probe']
        require(isinstance(probe, dict) and set(probe) == {
            'url', 'rpc_url', 'ca_seed', 'client_cert_seed', 'client_key_seed',
            'gateway_key_seed'}, 'real operator probe route profile required')
        require('layerx-gateway' in self.artifacts and any(
            row.get('artifact') == 'layerx-gateway' and row.get('oneshot') is not True
            for row in self.document['services']), 'real candidate gateway process required for operator journey')
        resolved = {key: value.format_map(self.values) for key, value in probe.items()
                    if key in ('url', 'rpc_url')}
        from urllib.parse import urlsplit
        for name, raw in resolved.items():
            url = urlsplit(raw)
            require(url.scheme == 'https' and url.hostname in ('localhost', '127.0.0.1')
                    and url.username is None and url.password is None and not url.query
                    and not url.fragment and (name != 'rpc_url' or url.path == '/v1/agent/rpc'),
                    'operator probe must use the actual disposable HTTPS route')
        for key in ('ca_seed', 'client_cert_seed', 'client_key_seed', 'gateway_key_seed'):
            name = probe[key]
            require(name in self.document['seeds'] and name in self.values,
                    'operator TLS and authority material must be provisioned seeds')
            path = Path(self.values[name])
            require(path.is_file() and self.directory in path.parents,
                    'operator authority material must be copied disposable files')
            resolved[key] = str(path)
        bearer = self.directory / 'operator-program-bearer'
        fd = os.open(bearer, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as stream:
            stream.write(self.values['program_bearer'] + '\n')
        resolved['bearer_seed'] = str(bearer)
        return tenants, resolved

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
