#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import ssl
import stat
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tests/daemon'))
import paxeer_x_runtime_fixture as fixture

SCHEMA = 'paxeer-x.agent-envelope-artifacts.v1'
CONFIG_SCHEMA = 'paxeer-x.agent-envelope-qualification.v1'
ROUTE = '/v1/agent/rpc'
MAX_BODY = 1_048_576
LANGUAGES = ('rust', 'typescript', 'python', 'go', 'java', 'kotlin', 'swift', 'csharp')
LANGUAGE_CASES = ('read', 'program_read', 'approval_list')
RUST_PRE_RESTART = ('allowed_mutation', 'mutation_duplicate_same_result', 'wrong_scope', 'wrong_tenant',
                    'wrong_generation', 'wrong_session', 'wrong_token', 'revoked_session',
                    'missing_idempotency_key', 'changed_body_same_key', 'restart_unknown_pending')
RUST_POST_RESTART = ('restart_unknown_reconcile',)
DIRECT_CASES = ('read_account', 'program_read', 'approval_list', 'program_bearer_alone', 'gateway_key_alone_write',
                'forged_principal_header', 'malformed_body', 'truncated_body', 'oversized_body', 'unknown_operation',
                'unknown_field', 'bad_version', 'noncanonical_integer', 'faucet_retired', 'mtls_no_client_cert',
                'mtls_wrong_peer', 'mtls_wrong_ca', 'no_plaintext_fallback', 'native_rpc_unchanged',
                'programs_route_unchanged')
CLASSES = {'TransportFailure', 'Deadline', 'ProtocolIncompatibility', 'UnavailableCapability', 'CoreRejection',
           'VerificationFailure', 'PolicyRefusal', 'CapabilityRefusal', 'BudgetRefusal', 'RateLimit',
           'IdempotencyConflict', 'InternalFault'}
LEVELS = ('Unverified', 'SequencerSigned', 'BatchIncluded', 'StateProven', 'CheckpointFinalised', 'SettlementAnchored')
SOURCES = ('agent/crates', 'agent/Cargo.toml', 'agent/Cargo.lock', 'agent/schema/agent-api', 'agent/sdk/typescript/src',
           'agent/sdk/typescript/test', 'agent/sdk/typescript/package.json', 'agent/sdk/typescript/package-lock.json',
           'agent/sdk/typescript/tsconfig.json', 'agent/sdk/python', 'platform/hosted/gateway', 'platform/Cargo.toml',
           'platform/Cargo.lock', 'platform/sdk/go', 'platform/sdk/jvm', 'platform/sdk/swift', 'platform/sdk/dotnet',
           'tools/paxeer-x/route-catalogue.json', 'tools/qualification/paxeer-x/agent_operation_envelope.py')
CASE_LINE = re.compile(r'^PAXEER_X_AGENT_ENVELOPE_CASE ([a-z0-9_.\-]+) passed$', re.M)
COUNT_LINE = re.compile(r'^PAXEER_X_AGENT_ENVELOPE_CASES=(\d+)$', re.M)


def require(condition, reason):
    if not condition:
        raise RuntimeError('agent operation envelope refused: ' + reason)


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def capture(argv, cwd=ROOT):
    return subprocess.run([str(a) for a in argv], cwd=cwd, check=True, capture_output=True, text=True).stdout.strip()


def private_dir(raw, name):
    require(bool(raw), name + ' is required')
    path = Path(raw).absolute()
    require(not path.is_symlink(), name + ' must not be a symlink')
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = path.stat()
    require(info.st_uid == os.geteuid() and not info.st_mode & 0o077, name + ' must be private and owned by caller')
    require(path.resolve() != ROOT and ROOT not in path.resolve().parents, name + ' must be outside the repository')
    return path.resolve()


def load_private(path, name):
    require(bool(path), name + ' is required')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o077,
                name + ' must be a private regular file')
        return json.load(stream)


def write_private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def operations():
    text = (ROOT / 'agent/crates/layerx-agent-api/src/operation_generated.rs').read_text()
    body = text[text.index('pub const fn name(self)'):]
    body = body[:body.index('\n    }\n')]
    names = re.findall(r'=> "([a-z_.\-]+)"', body)
    require(len(names) == 50 and len(set(names)) == 50, 'generated catalogue must name exactly 50 operations')
    require('faucet.claim' in names, 'generated catalogue lost faucet.claim')
    return sorted(names)


def source_identity():
    revision = capture(['git', 'rev-parse', 'HEAD'])
    require(not capture(['git', 'status', '--porcelain', '--', *SOURCES]), 'candidate source tree is dirty')
    tracked = [n for n in capture(['git', 'ls-files', '-z', '--', *SOURCES]).split('\0') if n]
    return {'revision': revision, 'tree': capture(['git', 'rev-parse', 'HEAD^{tree}']),
            'sources': {name: digest(ROOT / name) for name in sorted(tracked)},
            'operations': operations()}


def cargo_executables(stdout, want):
    found = {}
    for line in stdout.splitlines():
        if not line.startswith('{'):
            continue
        row = json.loads(line)
        if row.get('reason') == 'compiler-artifact' and row.get('executable'):
            key = (row['target']['name'], 'test' if row['profile'].get('test') else 'bin')
            if key in want:
                found[want[key]] = row['executable']
    require(set(found) == set(want.values()), 'cargo did not report every required executable')
    return found


def record(path):
    path = Path(path).resolve(strict=True)
    require(path.is_file() and path.stat().st_size > 0, 'empty artifact ' + str(path))
    return {'path': str(path), 'sha256': digest(path), 'bytes': path.stat().st_size}


def build(manifest):
    manifest = Path(manifest).absolute()
    out = private_dir(str(manifest.parent), 'manifest directory')
    require(not manifest.exists(), 'refusing to replace a retained artifact manifest')
    identity = source_identity()
    bundle = out / ('agent-envelope-' + identity['revision'])
    bundle.mkdir(mode=0o700)
    log = (bundle / 'build.log').open('w')
    commands = []

    def step(argv, cwd=ROOT, env=None):
        commands.append({'cwd': str(Path(cwd).relative_to(ROOT)) if Path(cwd) != ROOT else '.', 'argv': [str(a) for a in argv]})
        result = subprocess.run([str(a) for a in argv], cwd=cwd, env=env, capture_output=True, text=True, timeout=1500)
        log.write('$ ' + ' '.join(str(a) for a in argv) + '\n' + result.stderr + '\n')
        log.flush()
        require(result.returncode == 0, 'build step failed: ' + ' '.join(str(a) for a in argv[:4]))
        return result.stdout

    artifacts = {}
    agent = step(['cargo', 'build', '--locked', '--release', '--manifest-path', 'agent/Cargo.toml', '-p', 'layerx-agentd',
                  '--bins', '--message-format=json'])
    artifacts.update(cargo_executables(agent, {('layerx-agentd', 'bin'): 'agentd'}))
    probe = step(['cargo', 'test', '--locked', '--release', '--manifest-path', 'agent/Cargo.toml', '-p', 'layerx-sdk',
                  '--test', 'agent_operation_envelope', '--no-run', '--message-format=json'])
    artifacts.update(cargo_executables(probe, {('agent_operation_envelope', 'test'): 'rust_probe'}))
    gateway = step(['cargo', 'build', '--locked', '--release', '--manifest-path', 'platform/hosted/gateway/Cargo.toml',
                    '--bin', 'layerx-gateway', '--message-format=json'])
    artifacts.update(cargo_executables(gateway, {('layerx-gateway', 'bin'): 'gateway'}))
    ts = ROOT / 'agent/sdk/typescript'
    step(['npm', 'ci', '--offline', '--ignore-scripts'], cwd=ts)
    step(['npm', 'run', 'build'], cwd=ts)
    artifacts['typescript_probe'] = str(ts / 'dist/test/agent-operation-envelope.test.js')
    go_probe = bundle / 'go-agent-envelope.test'
    step(['go', 'test', '-c', '-o', go_probe, '.'], cwd=ROOT / 'platform/sdk/go')
    artifacts['go_probe'] = str(go_probe)
    jvm = ROOT / 'platform/sdk/jvm'
    classpath_file = bundle / 'jvm-classpath.txt'
    step(['mvn', '-q', '-o', '-B', 'test-compile', 'dependency:build-classpath',
          '-Dmdep.outputFile=' + str(classpath_file), '-Dmdep.includeScope=test'], cwd=jvm)
    artifacts['java_probe'] = str(jvm / 'target/test-classes/com/sidiora/layerx/sdk/AgentOperationEnvelopeProbe.class')
    artifacts['kotlin_probe'] = str(jvm / 'target/test-classes/com/sidiora/layerx/sdk/AgentOperationEnvelopeProbeKt.class')
    artifacts['jvm_main_classes'] = str(jvm / 'target/classes/com/sidiora/layerx/sdk/HttpProductionTransport.class')
    artifacts['jvm_classpath'] = str(classpath_file)
    swift = ROOT / 'platform/sdk/swift'
    step(['swift', 'build', '--build-tests', '-c', 'debug'], cwd=swift)
    swift_bin = Path(step(['swift', 'build', '--show-bin-path', '-c', 'debug'], cwd=swift).strip().splitlines()[-1])
    tests = sorted(swift_bin.glob('*PackageTests.xctest'))
    require(len(tests) == 1, 'swift test bundle not unique')
    artifacts['swift_probe'] = str(tests[0] / 'Contents/MacOS' / tests[0].stem) if (tests[0] / 'Contents').exists() else str(tests[0])
    dotnet = ROOT / 'platform/sdk/dotnet/tests/LayerX.Sdk.Tests'
    step(['dotnet', 'build', '-c', 'Release', '--no-restore', 'LayerX.Sdk.Tests.csproj'], cwd=dotnet)
    dlls = sorted(dotnet.glob('bin/Release/*/LayerX.Sdk.Tests.dll'))
    require(len(dlls) == 1, 'dotnet test assembly not unique')
    artifacts['csharp_probe'] = str(dlls[0])
    for name in ('agent/sdk/python/layerx_sdk/agent_http.py', 'agent/sdk/python/tests/test_agent_operation_envelope.py',
                 'tools/qualification/paxeer-x/agent_operation_envelope.py'):
        step([sys.executable, '-m', 'py_compile', name])
    artifacts['python_probe'] = str(ROOT / 'agent/sdk/python/tests/test_agent_operation_envelope.py')
    log.close()
    toolchains = {}
    for name, argv in (('rustc', ['rustc', '-vV']), ('cargo', ['cargo', '--version']), ('node', ['node', '--version']),
                       ('go', ['go', 'version']), ('java', ['java', '-version']), ('mvn', ['mvn', '-v']),
                       ('swift', ['swift', '--version']), ('dotnet', ['dotnet', '--version']),
                       ('python', [sys.executable, '--version'])):
        result = subprocess.run(argv, cwd=ROOT, capture_output=True, text=True)
        require(result.returncode == 0, 'toolchain identity ' + name)
        toolchains[name] = (result.stdout + result.stderr).strip()
    require(source_identity() == identity, 'source changed during build')
    value = {'schema': SCHEMA, 'source_revision': identity['revision'], 'source_tree': identity['tree'],
             'sources': identity['sources'], 'operations': identity['operations'], 'build_exit': 0,
             'commands': commands, 'toolchains': toolchains, 'build_log': str(bundle / 'build.log'),
             'artifacts': {name: record(path) for name, path in artifacts.items()}}
    write_private(manifest, value)
    print('PAXEER_X_AGENT_ENVELOPE_BUILD revision=' + identity['revision'] + ' artifacts=' + str(len(value['artifacts'])), flush=True)
    return 0


def load_manifest():
    built = load_private(os.environ.get('PAXEER_X_AGENT_ENVELOPE_ARTIFACT_MANIFEST'), 'PAXEER_X_AGENT_ENVELOPE_ARTIFACT_MANIFEST')
    require(built.get('schema') == SCHEMA and built.get('build_exit') == 0, 'artifact manifest schema or build exit')
    identity = source_identity()
    require(built['source_revision'] == identity['revision'] and built['source_tree'] == identity['tree']
            and built['sources'] == identity['sources'] and built['operations'] == identity['operations'],
            'artifact manifest does not bind the candidate revision')
    for name in ('agentd', 'rust_probe', 'gateway', 'typescript_probe', 'go_probe', 'java_probe', 'kotlin_probe',
                 'jvm_main_classes', 'jvm_classpath', 'swift_probe', 'csharp_probe', 'python_probe'):
        row = built['artifacts'].get(name)
        require(row is not None, 'missing candidate artifact ' + name)
        target = Path(row['path'])
        require(target.is_absolute() and target.is_file() and not target.is_symlink() and digest(target) == row['sha256'],
                'candidate artifact digest ' + name)
    for name in ('agentd', 'rust_probe', 'gateway', 'go_probe'):
        require(os.access(built['artifacts'][name]['path'], os.X_OK), 'candidate artifact not executable ' + name)
    return built


def load_config():
    config = load_private(os.environ.get('PAXEER_X_AGENT_ENVELOPE_CONFIG'), 'PAXEER_X_AGENT_ENVELOPE_CONFIG')
    require(config.get('schema') == CONFIG_SCHEMA, 'qualification configuration schema')
    for key in ('agentd_env', 'gateway_env', 'requests', 'credential_file', 'gateway_api_key_file',
                'program_bearer_file', 'gateway_peer_identity', 'program_route', 'native_rpc_body'):
        require(key in config, 'qualification configuration lacks ' + key)
    for key in ('read.account', 'program.interface', 'approval.list', 'mutation'):
        require(key in config['requests'], 'provisioned request lacks ' + key)
    for key in ('credential_file', 'gateway_api_key_file', 'program_bearer_file'):
        load_private(config[key], key) if key == 'credential_file' else require(
            Path(config[key]).is_file() and not Path(config[key]).stat().st_mode & 0o077, 'provisioned authority ' + key)
    credential = load_private(config['credential_file'], 'credential_file')
    require(set(credential) == {'tenant', 'session_id', 'token_id', 'generation'}, 'provisioned credential coordinates')
    require(re.fullmatch('(0|[1-9][0-9]{0,19})', credential['generation']) and int(credential['generation']) < 2**64,
            'provisioned generation is not canonical')
    for key in ('agentd_env', 'gateway_env'):
        require(isinstance(config[key], dict) and all(isinstance(k, str) and isinstance(v, str) for k, v in config[key].items()),
                key + ' must be a string map')
    require('LAYERX_AGENTD_RPC_LISTEN' not in config['agentd_env'], 'harness owns the agent RPC listener configuration')
    return config, credential


def tls_material(directory, peer):
    tls = directory / 'tls'
    tls.mkdir(mode=0o700)
    log = (directory / 'tls-producer.log').open('wb')

    def openssl(*argv):
        subprocess.run(['openssl', *[str(a) for a in argv]], check=True, stdout=log, stderr=log)

    for ca in ('ca', 'wrong-ca'):
        openssl('req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes', '-days', '1',
                '-subj', '/CN=paxeer-x-' + ca, '-addext', 'basicConstraints=critical,CA:TRUE',
                '-addext', 'keyUsage=critical,keyCertSign', '-keyout', tls / (ca + '.key'), '-out', tls / (ca + '.pem'))
    for name, ca, san, usage in (('agentd', 'ca', 'localhost', 'serverAuth'), ('gateway', 'ca', 'localhost', 'serverAuth'),
                                 ('gateway-client', 'ca', peer, 'clientAuth'), ('wrong-peer', 'ca', 'wrong-' + peer, 'clientAuth'),
                                 ('untrusted-client', 'wrong-ca', peer, 'clientAuth'), ('wrong-server', 'wrong-ca', 'localhost', 'serverAuth')):
        openssl('req', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes', '-subj', '/CN=' + san,
                '-keyout', tls / (name + '.key'), '-out', tls / (name + '.csr'))
        ext = tls / (name + '.ext')
        ext.write_text('subjectAltName=DNS:' + san + '\nextendedKeyUsage=' + usage + '\nbasicConstraints=CA:FALSE\n')
        openssl('x509', '-req', '-in', tls / (name + '.csr'), '-CA', tls / (ca + '.pem'), '-CAkey', tls / (ca + '.key'),
                '-CAcreateserial', '-days', '1', '-extfile', ext, '-out', tls / (name + '.pem'))
    for name in ('ca', 'wrong-ca', 'gateway', 'agentd'):
        openssl('x509', '-in', tls / (name + '.pem'), '-outform', 'DER', '-out', tls / (name + '.der'))
    openssl('pkcs8', '-topk8', '-nocrypt', '-in', tls / 'gateway.key', '-outform', 'DER', '-out', tls / 'gateway-key.der')
    password = tls / 'client-identity.password'
    password.write_text(os.urandom(16).hex())
    password.chmod(0o600)
    for name in ('gateway-client', 'wrong-peer'):
        openssl('pkcs12', '-export', '-in', tls / (name + '.pem'), '-inkey', tls / (name + '.key'), '-certfile', tls / 'ca.pem',
                '-passout', 'file:' + str(password), '-out', tls / (name + '.p12'))
    log.close()
    return tls


class Qualification:
    def __init__(self, directory, built, config, credential, runtime):
        self.d, self.built, self.config, self.credential, self.runtime = directory, built, config, credential, runtime
        self.tls = tls_material(directory, config['gateway_peer_identity'])
        ports = fixture.reserve_ports(2)
        self.agent_port, self.gateway_port = (s.getsockname()[1] for s in ports)
        for sock in ports:
            sock.close()
        self.agentd = self.gateway = None
        self.results = []
        self.request_id = 1000

    def substitute(self, env, extra=None):
        values = {'tls_dir': str(self.tls), 'state_dir': str(self.d / 'agent-state'), 'runtime_dir': str(self.runtime.directory),
                  'agent_rpc_port': str(self.agent_port), 'gateway_port': str(self.gateway_port),
                  'node_rpc_url': self.runtime.rpc_url, 'replica_url': 'http://127.0.0.1:' + str(self.runtime.ports[8]),
                  'node_socket': self.runtime.manifest['node_socket']}
        values.update(extra or {})
        return {key: value.format(**values) for key, value in env.items()}

    def start_agentd(self, overrides=None, expect_up=True):
        if self.agentd is not None and self.agentd.poll() is None:
            self.agentd.terminate()
            self.agentd.wait(timeout=15)
        (self.d / 'agent-state').mkdir(mode=0o700, exist_ok=True)
        env = dict(self.runtime.env)
        env.update(self.substitute(self.config['agentd_env']))
        env.update({'LAYERX_AGENTD_RPC_LISTEN': '127.0.0.1:' + str(self.agent_port),
                    'LAYERX_AGENTD_RPC_TLS_CERT': str(self.tls / 'agentd.pem'),
                    'LAYERX_AGENTD_RPC_TLS_KEY': str(self.tls / 'agentd.key'),
                    'LAYERX_AGENTD_RPC_TLS_CLIENT_CA': str(self.tls / 'ca.pem'),
                    'LAYERX_AGENTD_RPC_PEER': self.config['gateway_peer_identity']})
        env.update(overrides or {})
        with (self.d / 'agentd.log').open('ab') as log:
            self.agentd = subprocess.Popen([self.built['artifacts']['agentd']['path']], env=env, stdout=log, stderr=log)
        if expect_up:
            self.wait_port(self.agent_port, self.agentd, 'agentd')
        return self.agentd

    def start_gateway(self, client_identity='gateway-client', outbound_ca='ca'):
        if self.gateway is not None and self.gateway.poll() is None:
            self.gateway.terminate()
            self.gateway.wait(timeout=15)
        env = dict(self.runtime.env)
        env.update(self.substitute(self.config['gateway_env']))
        env.update({'LAYERX_GATEWAY_LISTEN': '127.0.0.1:' + str(self.gateway_port),
                    'LAYERX_GATEWAY_TLS_CERT_DER': str(self.tls / 'gateway.der'),
                    'LAYERX_GATEWAY_TLS_KEY_DER': str(self.tls / 'gateway-key.der'),
                    'LAYERX_GATEWAY_OUTBOUND_CA_DER': str(self.tls / (outbound_ca + '.der'))})
        if client_identity is None:
            env.pop('LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12', None)
            env.pop('LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE', None)
        else:
            env['LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12'] = str(self.tls / (client_identity + '.p12'))
            env['LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE'] = str(self.tls / 'client-identity.password')
        with (self.d / 'gateway.log').open('ab') as log:
            self.gateway = subprocess.Popen([self.built['artifacts']['gateway']['path']], env=env, stdout=log, stderr=log)
        self.wait_port(self.gateway_port, self.gateway, 'gateway')

    def wait_port(self, port, process, name):
        def up():
            require(process.poll() is None, name + ' exited during boot')
            with socket.create_connection(('127.0.0.1', port), timeout=.3):
                return True
        self.runtime.wait(up)

    def stop(self):
        for process in (self.gateway, self.agentd):
            if process is not None and process.poll() is None:
                process.terminate()
                process.wait(timeout=15)

    def passed(self, case, evidence):
        require(case not in [row['case'] for row in self.results], 'case counted twice: ' + case)
        self.results.append({'case': case, 'result': 'passed', 'evidence': str(evidence)})
        print('PAXEER_X_PROGRESS cases=' + str(len(self.results)) + ' last=' + case, flush=True)

    def context(self, verify=True, client=None, server_ca='ca'):
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        ctx.minimum_version = ssl.TLSVersion.TLSv1_2
        ctx.load_verify_locations(str(self.tls / (server_ca + '.pem')))
        if client:
            ctx.load_cert_chain(str(self.tls / (client + '.pem')), str(self.tls / (client + '.key')))
        return ctx

    def raw(self, port, payload, ctx, case):
        with socket.create_connection(('127.0.0.1', port), timeout=20) as sock:
            with ctx.wrap_socket(sock, server_hostname='localhost') as tls:
                tls.sendall(payload)
                chunks = []
                while True:
                    chunk = tls.recv(65536)
                    if not chunk:
                        break
                    chunks.append(chunk)
        data = b''.join(chunks)
        (self.d / 'responses' / (case + '.http')).write_bytes(data)
        head, _, body = data.partition(b'\r\n\r\n')
        status = int(head.split(b' ', 2)[1])
        return status, body

    def http(self, body, path=ROUTE, headers=None, method='POST', case='case', port=None, ctx=None):
        headers = dict(headers or {})
        headers.setdefault('Host', 'localhost')
        headers.setdefault('Content-Type', 'application/json')
        headers.setdefault('Connection', 'close')
        if 'Content-Length' not in headers and body is not None:
            headers['Content-Length'] = str(len(body))
        head = method + ' ' + path + ' HTTP/1.1\r\n' + ''.join(k + ': ' + v + '\r\n' for k, v in headers.items()) + '\r\n'
        return self.raw(port or self.gateway_port, head.encode() + (body or b''), ctx or self.context(), case)

    def api_key(self):
        return {'LayerX-Key': Path(self.config['gateway_api_key_file']).read_text().strip()}

    def envelope(self, operation, request, credential=True, idempotency=None, **extra):
        self.request_id += 1
        value = {'version': 1, 'request_id': str(self.request_id), 'operation': operation, 'request': request,
                 'credential': dict(self.credential) if credential else None}
        if idempotency is not None:
            value['idempotency_key'] = idempotency
        value.update(extra)
        return value

    def encode(self, value):
        return json.dumps(value, separators=(',', ':')).encode()

    def success(self, status, body, request_id, case):
        require(status == 200, case + ': expected 200, got ' + str(status))
        value = json.loads(body)
        require(set(value) == {'request_id', 'value', 'verification_status'}, case + ': success envelope fields')
        require(value['request_id'] == str(request_id), case + ': request_id not preserved')
        vs = value['verification_status']
        if vs.get('state') == 'achieved':
            require(set(vs) == {'state', 'level'} and vs['level'] in LEVELS, case + ': verification status')
        else:
            require(set(vs) == {'state', 'requested', 'achieved', 'reason'} and vs['state'] == 'unverified'
                    and vs['requested'] in LEVELS and vs['achieved'] in LEVELS
                    and LEVELS.index(vs['achieved']) < LEVELS.index(vs['requested']), case + ': unverified status')
        return value

    def refusal(self, status, body, http_status, klass, reason, case, request_id=None):
        require(status == http_status, case + ': expected HTTP ' + str(http_status) + ', got ' + str(status))
        value = json.loads(body)
        require(set(value) == {'class', 'protocol_result_code', 'retriability', 'request_id', 'reason'}, case + ': error envelope fields')
        require(value['class'] in CLASSES and value['retriability'] in ('Terminal', 'Retriable'), case + ': error lattice')
        require(value['class'] == klass, case + ': class ' + value['class'] + ' != ' + klass)
        require(reason is None or value['reason'] == reason, case + ': reason ' + value['reason'])
        require(request_id is None or value['request_id'] == str(request_id), case + ': error request_id')
        require(value['protocol_result_code'] is None or isinstance(value['protocol_result_code'], int), case + ': result code')
        return value

    def call(self, case, operation, request, **kwargs):
        value = self.envelope(operation, request, **kwargs)
        status, body = self.http(self.encode(value), headers=self.api_key(), case=case)
        return value, status, body

    def direct_cases(self):
        req = self.config['requests']
        for case, operation in (('read_account', 'read.account'), ('program_read', 'program.interface'),
                                ('approval_list', 'approval.list')):
            value, status, body = self.call(case, operation, req[operation])
            self.success(status, body, value['request_id'], case)
            self.passed(case, self.d / 'responses' / (case + '.http'))
        mutation = req['mutation']
        key = os.urandom(32).hex()
        value = self.envelope(mutation['operation'], mutation['request'], credential=False, idempotency=key)
        status, body = self.http(self.encode(value), headers={'Authorization': 'Bearer ' + Path(self.config['program_bearer_file']).read_text().strip()},
                                 case='program_bearer_alone')
        require(status in (400, 401, 403), 'program_bearer_alone: bearer authorized a catalogue write')
        require(json.loads(body)['class'] in ('ProtocolIncompatibility', 'PolicyRefusal'), 'program_bearer_alone: class')
        self.passed('program_bearer_alone', self.d / 'responses/program_bearer_alone.http')
        status, body = self.http(self.encode(value), headers=self.api_key(), case='gateway_key_alone_write')
        require(status in (400, 401, 403), 'gateway_key_alone_write: gateway key authorized a catalogue write')
        require(json.loads(body)['class'] in ('ProtocolIncompatibility', 'PolicyRefusal'), 'gateway_key_alone_write: class')
        self.passed('gateway_key_alone_write', self.d / 'responses/gateway_key_alone_write.http')
        value = self.envelope('read.account', req['read.account'])
        forged = dict(self.api_key(), **{'LayerX-Tenant': 'forged', 'LayerX-Agent': 'forged', 'X-Forwarded-For': '203.0.113.1'})
        status, body = self.http(self.encode(value), headers=forged, case='forged_principal_header')
        self.success(status, body, value['request_id'], 'forged_principal_header')
        self.direct_daemon_header_principal(value)
        self.passed('forged_principal_header', self.d / 'responses/forged_principal_header.http')
        bad = [('malformed_body', b'{"version":1,"version":1}', 400, 'ProtocolIncompatibility', 'envelope.malformed'),
               ('truncated_body', self.encode(self.envelope('read.account', req['read.account']))[:-7], 400,
                'ProtocolIncompatibility', 'envelope.malformed')]
        for case, body, http_status, klass, reason in bad:
            status, response = self.http(body, headers=self.api_key(), case=case)
            self.refusal(status, response, http_status, klass, reason, case, request_id=0)
            self.passed(case, self.d / 'responses' / (case + '.http'))
        status, response = self.http(b'\xff\xfe' + b' ' * 16, headers=self.api_key(), case='malformed_body_utf8')
        self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.malformed', 'malformed_body_utf8', request_id=0)
        status, response = self.http(b'{"version":1}', headers=dict(self.api_key(), **{'Content-Length': '200'}), case='truncated_framing')
        require(status in (400, 408) or not response, 'truncated_framing accepted a short body')
        over = self.envelope('read.account', req['read.account'])
        over['request'] = dict(req['read.account'])
        body = self.encode(over)
        body = body[:-1] + b',"padding":"' + b'a' * (MAX_BODY - len(body) + 16) + b'"}'
        require(len(body) > MAX_BODY, 'oversized case is not over the bound')
        status, response = self.http(body, headers=self.api_key(), case='oversized_body')
        self.refusal(status, response, 413, 'ProtocolIncompatibility', 'envelope.oversized', 'oversized_body')
        self.passed('oversized_body', self.d / 'responses/oversized_body.http')
        value = self.envelope('read.unknown_operation', req['read.account'])
        status, response = self.http(self.encode(value), headers=self.api_key(), case='unknown_operation')
        self.refusal(status, response, 404, 'ProtocolIncompatibility', 'envelope.unknown_operation', 'unknown_operation', value['request_id'])
        self.passed('unknown_operation', self.d / 'responses/unknown_operation.http')
        value = self.envelope('read.account', req['read.account'], principal='forged')
        status, response = self.http(self.encode(value), headers=self.api_key(), case='unknown_field')
        self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.unknown_field', 'unknown_field')
        value = self.envelope('read.account', req['read.account'])
        value['credential']['agent'] = 'forged'
        status, response = self.http(self.encode(value), headers=self.api_key(), case='unknown_credential_field')
        self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.unknown_field', 'unknown_credential_field')
        self.passed('unknown_field', self.d / 'responses/unknown_field.http')
        for case, version in (('bad_version', 2), ('bad_version_string', '1')):
            value = self.envelope('read.account', req['read.account'])
            value['version'] = version
            status, response = self.http(self.encode(value), headers=self.api_key(), case=case)
            self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.version', case)
        self.passed('bad_version', self.d / 'responses/bad_version.http')
        for case, generation in (('noncanonical_leading_zero', '0' + self.credential['generation']),
                                 ('noncanonical_overflow', str(2**64)), ('noncanonical_sign', '+' + self.credential['generation'])):
            value = self.envelope('read.account', req['read.account'])
            value['credential']['generation'] = generation
            status, response = self.http(self.encode(value), headers=self.api_key(), case=case)
            self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.noncanonical_integer', case)
        value = self.envelope('read.account', req['read.account'])
        value['credential']['generation'] = int(self.credential['generation'])
        status, response = self.http(self.encode(value), headers=self.api_key(), case='noncanonical_number')
        require(status == 400 and json.loads(response)['class'] == 'ProtocolIncompatibility', 'noncanonical_number accepted')
        value = self.envelope('read.account', req['read.account'])
        value['request_id'] = '0' + value['request_id']
        status, response = self.http(self.encode(value), headers=self.api_key(), case='noncanonical_request_id')
        self.refusal(status, response, 400, 'ProtocolIncompatibility', 'envelope.noncanonical_integer', 'noncanonical_request_id')
        self.passed('noncanonical_integer', self.d / 'responses/noncanonical_overflow.http')
        value, status, response = self.call('faucet_retired', 'faucet.claim', req.get('faucet.claim', {}),
                                            idempotency=os.urandom(32).hex())
        error = self.refusal(status, response, 503, 'UnavailableCapability', 'unavailable_capability.faucet.claim',
                             'faucet_retired', value['request_id'])
        require(error['retriability'] == 'Terminal' and error['protocol_result_code'] is None, 'faucet_retired: retriability')
        self.passed('faucet_retired', self.d / 'responses/faucet_retired.http')
        status, response = self.http(self.config['native_rpc_body'].encode(), path='/rpc', headers=self.api_key(), case='native_rpc_unchanged')
        value = json.loads(response)
        require(status == 200 and value.get('jsonrpc') == '2.0' and 'request_id' not in value, 'native_rpc_unchanged: /rpc changed contract')
        status, response = self.http(None, path='/rpc/schema', method='GET', headers={'Content-Type': 'application/json'},
                                     case='native_rpc_schema_unchanged')
        require(status == 200 and 'verification_status' not in json.loads(response), 'native /rpc/schema changed contract')
        self.passed('native_rpc_unchanged', self.d / 'responses/native_rpc_unchanged.http')
        route = self.config['program_route']
        status, response = self.http(None if route['method'] == 'GET' else route['body'].encode(), path=route['path'],
                                     method=route['method'], headers=self.api_key(), case='programs_route_unchanged')
        require(status == route['status'] and 'verification_status' not in json.loads(response), 'programs route changed contract')
        status, response = self.http(self.encode(self.envelope('read.account', req['read.account'])), path=ROUTE + '/',
                                     headers=self.api_key(), case='route_prefix_refused')
        require(status in (404, 405), 'agent route admitted a prefix match')
        status, response = self.http(None, path=ROUTE, method='GET', headers=self.api_key(), case='route_method_refused')
        require(status == 405, 'agent route admitted GET')
        self.passed('programs_route_unchanged', self.d / 'responses/programs_route_unchanged.http')

    def direct_daemon_header_principal(self, value):
        status, response = self.http(self.encode(value), path='/rpc', headers={'LayerX-Tenant': 'forged'},
                                     case='daemon_header_principal', port=self.agent_port,
                                     ctx=self.context(client='gateway-client'))
        self.refusal(status, response, 403, 'PolicyRefusal', 'envelope.header_principal', 'daemon_header_principal')

    def mtls_cases(self):
        body = self.encode(self.envelope('read.account', self.config['requests']['read.account']))
        def refused(case, ctx, port=None):
            try:
                status, _ = self.http(body, path='/rpc', case=case, port=port or self.agent_port, ctx=ctx)
            except (ssl.SSLError, ConnectionError, OSError):
                return
            raise RuntimeError('agent operation envelope refused: ' + case + ' answered HTTP ' + str(status))
        refused('mtls_no_client_cert', self.context())
        refused('mtls_untrusted_client', self.context(client='untrusted-client'))
        refused('mtls_wrong_peer_direct', self.context(client='wrong-peer'))
        self.passed('mtls_no_client_cert', self.d / 'agentd.log')
        self.start_gateway(client_identity='wrong-peer')
        status, response = self.http(body, headers=self.api_key(), case='mtls_wrong_peer')
        require(status in (502, 503) and b'verification_status' not in response, 'gateway with wrong peer identity reached the daemon')
        self.passed('mtls_wrong_peer', self.d / 'responses/mtls_wrong_peer.http')
        self.start_gateway(outbound_ca='wrong-ca')
        status, response = self.http(body, headers=self.api_key(), case='mtls_wrong_ca')
        require(status in (502, 503) and b'verification_status' not in response, 'gateway trusted a wrong daemon CA')
        self.start_gateway(client_identity=None)
        status, response = self.http(body, headers=self.api_key(), case='mtls_absent_identity')
        require(status in (502, 503) and b'verification_status' not in response, 'gateway without client identity reached the daemon')
        self.start_gateway()
        try:
            self.http(body, case='client_wrong_server_ca', ctx=self.context(server_ca='wrong-ca'))
            raise RuntimeError('agent operation envelope refused: gateway certificate verified under a wrong CA')
        except ssl.SSLCertVerificationError:
            pass
        self.passed('mtls_wrong_ca', self.d / 'responses/mtls_wrong_ca.http')
        with socket.create_connection(('127.0.0.1', self.agent_port), timeout=10) as sock:
            sock.sendall(b'POST /rpc HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: '
                         + str(len(body)).encode() + b'\r\nConnection: close\r\n\r\n' + body)
            sock.settimeout(10)
            try:
                data = sock.recv(65536)
            except (ConnectionError, socket.timeout):
                data = b''
        require(not data.startswith(b'HTTP/'), 'agent RPC listener answered plaintext HTTP')
        broken = self.start_agentd({'LAYERX_AGENTD_RPC_TLS_CLIENT_CA': str(self.d / 'absent-ca.pem')}, expect_up=False)
        require(broken.wait(timeout=60) != 0, 'agentd booted the RPC listener without its trusted client CA')
        self.start_agentd()
        self.start_gateway()
        self.passed('no_plaintext_fallback', self.d / 'agentd.log')

    def probe(self, language, cases, phase, state):
        case_file = self.d / 'probes' / (language + '-' + phase + '.json')
        fixture.write_json(case_file, {
            'endpoint': 'https://localhost:' + str(self.gateway_port) + ROUTE, 'server_name': 'localhost',
            'ca_pem': str(self.tls / 'ca.pem'), 'ca_der': str(self.tls / 'ca.der'),
            'gateway_api_key_file': self.config['gateway_api_key_file'],
            'program_bearer_file': self.config['program_bearer_file'],
            'credential_file': self.config['credential_file'], 'requests': self.config['requests'],
            'operations': self.built['operations'], 'cases': list(cases), 'phase': phase,
            'state_file': str(state), 'response_dir': str(self.d / 'probes' / (language + '-' + phase))})
        (self.d / 'probes' / (language + '-' + phase)).mkdir(mode=0o700)
        art = self.built['artifacts']
        env = dict(self.runtime.env, PAXEER_X_AGENT_ENVELOPE_CASE=str(case_file))
        jvm_classpath = ':'.join([str(Path(art['jvm_main_classes']['path']).parents[4]),
                                  str(Path(art['java_probe']['path']).parents[4]),
                                  Path(art['jvm_classpath']['path']).read_text().strip()])
        argv = {
            'rust': [art['rust_probe']['path'], '--exact', 'agent_operation_envelope_process_cases', '--nocapture', '--test-threads=1'],
            'typescript': ['node', art['typescript_probe']['path']],
            'python': [sys.executable, art['python_probe']['path']],
            'go': [art['go_probe']['path'], '-test.run', '^TestAgentOperationEnvelopeProcessCases$', '-test.v', '-test.count=1'],
            'java': ['java', '-cp', jvm_classpath, 'com.sidiora.layerx.sdk.AgentOperationEnvelopeProbe'],
            'kotlin': ['java', '-cp', jvm_classpath, 'com.sidiora.layerx.sdk.AgentOperationEnvelopeProbeKt'],
            'swift': ['swift', 'test', '--skip-build', '--package-path', str(ROOT / 'platform/sdk/swift'),
                      '--filter', 'AgentOperationEnvelopeTests'],
            'csharp': ['dotnet', 'test', art['csharp_probe']['path'], '--no-build', '--filter',
                       'FullyQualifiedName~AgentOperationEnvelopeTests', '--logger', 'console;verbosity=detailed'],
        }[language]
        result = subprocess.run([str(a) for a in argv], env=env, cwd=ROOT, capture_output=True, text=True, timeout=300)
        log = self.d / 'probes' / (language + '-' + phase + '.log')
        log.write_text(result.stdout + result.stderr)
        require(result.returncode == 0, language + ' probe failed in ' + phase + '; inspect private probe log')
        reported = CASE_LINE.findall(result.stdout)
        counts = COUNT_LINE.findall(result.stdout)
        require(len(counts) == 1 and int(counts[0]) == len(reported) == len(set(reported)), language + ' probe case accounting')
        require(set(reported) == set(cases), language + ' probe reported ' + str(sorted(set(cases) ^ set(reported))))
        for case in cases:
            self.passed('sdk_' + language + '_' + case if phase == 'read' else case, log)

    def run(self):
        (self.d / 'responses').mkdir(mode=0o700)
        (self.d / 'probes').mkdir(mode=0o700)
        self.start_agentd()
        self.start_gateway()
        self.direct_cases()
        self.mtls_cases()
        for language in LANGUAGES:
            self.probe(language, LANGUAGE_CASES, 'read', self.d / 'probes' / (language + '.state'))
        state = self.d / 'probes/rust-mutation.state'
        operation_cases = tuple('operation.' + name for name in self.built['operations'])
        self.probe('rust', RUST_PRE_RESTART + operation_cases, 'pre-restart', state)
        old = self.agentd.pid
        self.agentd.kill()
        self.agentd.wait(timeout=15)
        self.start_agentd()
        require(self.agentd.pid != old, 'agentd restart reused the process')
        self.probe('rust', RUST_POST_RESTART, 'post-restart', state)


def worker(directory):
    built = load_manifest()
    config, credential = load_config()
    bundle = fixture.artifacts(os.environ.get('PAXEER_X_RUNTIME_ARTIFACTS'))
    client = fixture.client_artifact(os.environ.get('PAXEER_X_RUNTIME_CLIENT_MANIFEST'))
    for name in ('net', 'pid', 'mnt'):
        require(os.readlink('/proc/self/ns/' + name) != os.environ['PAXEER_X_PARENT_' + name.upper()], 'namespace isolation')
    subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True)
    runtime = fixture.RuntimeFixture(directory, bundle, client)
    qualification = None
    try:
        runtime.generate()
        runtime.start()
        runtime.readiness()
        qualification = Qualification(runtime.directory, built, config, credential, runtime)
        qualification.run()
        expected = (len(DIRECT_CASES) + len(LANGUAGES) * len(LANGUAGE_CASES) + len(RUST_PRE_RESTART)
                    + len(built['operations']) + len(RUST_POST_RESTART))
        require(len(qualification.results) == expected, 'case count ' + str(len(qualification.results)) + ' != ' + str(expected))
        write_private(runtime.directory / 'case-results.json', qualification.results)
        print(f'PAXEER_X_GATE tests={len(qualification.results)} skipped=0', flush=True)
    finally:
        if qualification is not None:
            qualification.stop()
        runtime.cleanup()


def main():
    os.umask(0o077)
    if len(sys.argv) == 4 and sys.argv[1] == '--build' and sys.argv[2] == '--manifest':
        return build(sys.argv[3])
    if len(sys.argv) == 3 and sys.argv[1] == '--worker':
        worker(sys.argv[2])
        return 0
    require(len(sys.argv) == 1, 'usage: agent_operation_envelope.py [--build --manifest PATH]')
    require(os.geteuid() == 0, 'disposable runtime requires root for distinct peer credentials')
    load_manifest()
    load_config()
    for name in ('openssl', 'unshare', 'ip', 'node', 'java', 'swift', 'dotnet'):
        require(shutil.which(name), 'required tool absent: ' + name)
    evidence = private_dir(os.environ.get('PAXEER_X_EVIDENCE_DIR'), 'PAXEER_X_EVIDENCE_DIR')
    revision = capture(['git', 'rev-parse', 'HEAD'])
    directory = Path(tempfile.mkdtemp(prefix='px-agent-envelope-', dir='/var/tmp'))
    directory.rmdir()
    command = 'timeout 30m python3 tools/qualification/paxeer-x/agent_operation_envelope.py'
    started = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
    env = dict(os.environ)
    for name in ('net', 'pid', 'mnt'):
        env['PAXEER_X_PARENT_' + name.upper()] = os.readlink('/proc/self/ns/' + name)
    log_path = evidence / 'worker.log'
    with log_path.open('wb') as log:
        result = subprocess.run(['unshare', '--mount', '--net', '--pid', '--fork', '--kill-child=KILL', '--mount-proc',
                                 '--propagation', 'private', sys.executable, str(Path(__file__).resolve()), '--worker', str(directory)],
                                env=env, stdout=log, stderr=log, timeout=1680)
    text = log_path.read_text()
    markers = re.findall(r'^PAXEER_X_GATE tests=(\d+) skipped=0$', text, re.M)
    progress = re.findall(r'^PAXEER_X_PROGRESS cases=(\d+)', text, re.M)
    count = int(markers[0]) if len(markers) == 1 else int(progress[-1]) if progress else 0
    write_private(evidence / 'result.json', {
        'candidate_revision': revision, 'command': command, 'started_utc': started,
        'finished_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'exit_code': result.returncode,
        'cases_passed': count, 'gate_marker': len(markers) == 1, 'worker_log': str(log_path),
        'runtime_directory': str(directory), 'case_results': str(directory / 'case-results.json')})
    print(f'PAXEER_X_GATE tests={count} skipped=0' if len(markers) == 1 else
          f'PAXEER_X_GATE tests={count} skipped=0 incomplete=1', flush=True)
    require(result.returncode == 0 and len(markers) == 1 and count > 0, 'worker failed; inspect private worker.log')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
