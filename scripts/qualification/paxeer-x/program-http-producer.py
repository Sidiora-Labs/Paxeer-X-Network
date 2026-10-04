#!/usr/bin/env python3
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import socket
import ssl
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
SCHEMA = 'paxeer-x.python-program-http-artifacts.v1'
TEST = 'native_interfaces::emit_python_program_http_fixture'
BUILD = ['cargo', 'build', '--locked', '--manifest-path', 'platform/Cargo.toml',
         '-p', 'layerx-platform-agent-boundary', '-p', 'layerx-platform-gateway',
         '-p', 'layerx-platform-identity', '-p', 'layerx-platform-registry',
         '-p', 'layerx-platform-authority', '--bins', '--tests', '--message-format=json']
VERIFY = ['timeout', '20m', 'scripts/paxeer-x/verify-sdk-program-python.sh']
ROLES = {'layerxd': 'layerxd', 'genesis-builder': 'layerx-genesis-build',
         'module-registry': 'layerx-module-registry', 'boundary': 'layerx-agent-boundary',
         'registry': 'layerx-program-registry', 'builder-isolation': 'bwrap',
         'builder-supervisor': 'layerx-cgroup-exec', 'gateway': 'layerx-gateway',
         'identity': 'layerx-identity', 'authority': 'layerx-receipt-authority', 'redis': 'redis-server'}
COMPILED_ROLES = {'boundary', 'registry', 'builder-supervisor', 'gateway', 'identity', 'authority'}
CACHED_ROLES = set(ROLES) - COMPILED_ROLES


def require(value, message):
    if not value:
        raise ValueError(message)


def unique(items):
    result = {}
    for key, value in items:
        require(key not in result, 'duplicate producer field')
        result[key] = value
    return result


def private(path, directory=False, create=False):
    path = Path(path).absolute()
    if create:
        path.mkdir(mode=0o700)
    info = path.lstat()
    require(path.resolve() == path and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == (0o700 if directory else 0o600)
            and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode) and info.st_nlink == 1),
            'canonical caller-owned private producer input required')
    return path


def load(path):
    path = private(path)
    private(path.parent, True)
    require(0 < path.stat().st_size <= 16_777_216, 'bounded producer record required')
    return json.loads(path.read_bytes(), object_pairs_hook=unique)


def write(path, value):
    data = value if isinstance(value, bytes) else (json.dumps(value, indent=2) + '\n').encode()
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    return str(path)


def artifact(path):
    path = Path(path).resolve(strict=True)
    info = path.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_size > 0, 'actual nonempty artifact required')
    with path.open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    return {'path': str(path), 'sha256': digest, 'bytes': info.st_size}


def identity():
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()
    require(not git('status', '--porcelain=v1', '--untracked-files=normal'), 'clean published candidate required')
    return {'revision': git('rev-parse', 'HEAD^{commit}'), 'tree': git('rev-parse', 'HEAD^{tree}')}


def compatible(value, roles, runtime=False):
    require(set(value) == {'schema', 'source', 'artifacts', 'build_logs', 'registry_configuration',
                          'registry_cgroup_parent', 'custody_fixture'}, 'exact cached production artifact contract required')
    require(value['schema'] == SCHEMA and set(value['artifacts']) == roles, 'complete genuine process inventory required')
    require(value['build_logs'], 'actual cached artifact build provenance required')
    for log in value['build_logs']:
        require(artifact(log['path']) == log, 'cached build provenance changed')
    for role in roles:
        name = ROLES[role]
        record = value['artifacts'][role]
        expected = {'artifact', 'source_files', 'package_provenance', 'environment'} if role in {'redis', 'builder-isolation'} else {'artifact', 'source_files'}
        require(set(record) == expected, 'exact artifact/source compatibility record required')
        require(artifact(record['artifact']['path']) == record['artifact'], 'cached executable changed')
        require(Path(record['artifact']['path']).name == name and os.access(record['artifact']['path'], os.X_OK),
                'actual production executable required')
        require(isinstance(record['source_files'], dict), 'cached source inventory required')
        if role in {'redis', 'builder-isolation'}:
            require(not record['source_files'] and artifact(record['package_provenance']['path']) == record['package_provenance'],
                    'actual external package provenance required')
            require(set(record['environment']) <= {'LD_LIBRARY_PATH'}, 'closed external loader environment required')
            for search in record['environment'].values():
                require(isinstance(search, str) and search and len(search) < 4096, 'bounded actual package loader path required')
                for component in search.split(':'):
                    info = Path(component).stat()
                    require(Path(component).is_absolute() and Path(component).resolve() == Path(component)
                            and stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o022,
                            'owner-controlled actual package library directory required')
        else:
            require(record['source_files'], 'cached source inventory required')
        for relative, digest in record['source_files'].items():
            path = (ROOT / relative).resolve(strict=True)
            require(ROOT in path.parents and not Path(relative).is_absolute()
                    and hashlib.sha256(path.read_bytes()).hexdigest() == digest, 'cached production source differs from candidate')
    if runtime:
        require(value['custody_fixture'] and value['registry_configuration'] and value['registry_cgroup_parent'],
                'genuine funding, registry configuration and delegated cgroup are required for behavior')
        private(value['custody_fixture'], True)
        load(value['registry_configuration'])
        require(Path(value['registry_cgroup_parent']).is_dir(), 'actual delegated registry cgroup required')
    else:
        for key in ('custody_fixture', 'registry_configuration', 'registry_cgroup_parent'):
            require(value[key] is None or isinstance(value[key], str) and Path(value[key]).is_absolute(),
                    'missing runtime authority must remain explicit, never manufactured')
    native = Path(value['artifacts']['layerxd']['artifact']['path']).parent
    for role in ('genesis-builder', 'module-registry'):
        require(Path(value['artifacts'][role]['artifact']['path']).parent == native, 'genuine native binary directory mismatch')


def build(cache_path, output):
    value = load(cache_path)
    compatible(value, CACHED_ROLES)
    source = identity()
    directory = private(output, True, True)
    require(not any(directory.iterdir()), 'fresh build directory required')
    log = directory / 'build.log'
    environment = dict(os.environ, CARGO_BUILD_JOBS='4', CARGO_TARGET_DIR='/root/lx-target/platform')
    with open('/root/lx-cargo/platform-tooling.lock', 'a') as lock, log.open('xb') as stream:
        fcntl.flock(lock, fcntl.LOCK_EX)
        process = subprocess.run(BUILD, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                 stdout=stream, stderr=subprocess.STDOUT, timeout=1200)
    require(process.returncode == 0, 'declared producer build failed; inspect private build.log')
    executable = []
    compiled = {role: [] for role in COMPILED_ROLES}
    finished = False
    for line in log.read_text().splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        if event.get('reason') == 'build-finished':
            finished = event.get('success') is True
        if event.get('reason') != 'compiler-artifact' or not event.get('executable'):
            continue
        target = event['target']
        if target.get('name') == 'real_node' and target.get('kind') == ['test'] and event['profile']['test']:
            executable.append(event['executable'])
        for role in COMPILED_ROLES:
            if target.get('name') == ROLES[role] and target.get('kind') == ['bin'] and not event['profile']['test']:
                compiled[role].append(event['executable'])
    require(finished and len(executable) == 1 and all(len(paths) == 1 for paths in compiled.values())
            and identity() == source, 'actual source-bound producer and every production executable required')
    tracked = subprocess.check_output(['git', 'ls-files', '-z', '--', 'platform', 'agent', 'programs', 'include', 'src', 'cmd'], cwd=ROOT)
    sources = {}
    for encoded in tracked.split(b'\0'):
        if not encoded:
            continue
        relative = encoded.decode()
        if Path(relative).name.startswith('.env'):
            continue
        path = ROOT / relative
        if path.is_file() and not path.is_symlink():
            sources[relative] = hashlib.sha256(path.read_bytes()).hexdigest()
    value['source'] = source
    value['artifacts']['real-node-tests'] = {'artifact': artifact(executable[0]), 'source_files': {
        'platform/hosted/agent-boundary/tests/real_node/native_interfaces.rs':
        artifact(ROOT / 'platform/hosted/agent-boundary/tests/real_node/native_interfaces.rs')['sha256']}}
    for role, paths in compiled.items():
        value['artifacts'][role] = {'artifact': artifact(paths[0]), 'source_files': sources}
    if value['registry_configuration'] is not None:
        configuration = load(value['registry_configuration'])
        for role, key in (('builder-isolation', 'LAYERX_REGISTRY_BUILDER_ISOLATION_RUNTIME'),
                          ('builder-supervisor', 'LAYERX_REGISTRY_BUILDER_JOB_SUPERVISOR')):
            record = value['artifacts'][role]['artifact']
            if role == 'builder-isolation':
                require(configuration[key] == record['path'] and configuration[key + '_DIGEST'] == record['sha256'],
                        'genuine cached registry isolation owner differs')
            configuration[key] = record['path']
            configuration[key + '_DIGEST'] = record['sha256']
        value['registry_configuration'] = write(directory / 'registry-configuration.json', configuration)
    value['build_logs'].append(artifact(log))
    write(directory / 'manifest.json', value)
    print(directory / 'manifest.json')


def port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]


class Runtime:
    def __init__(self, directory, artifacts):
        self.directory, self.artifacts = directory, artifacts
        self.children, self.streams = [], []

    def start(self, role, environment, arguments=(), uid=None, working_directory=None, binary=None):
        stream = (self.directory / (role + '.log')).open('xb')
        self.streams.append(stream)
        binary = binary or self.artifacts[role]['artifact']['path']
        environment = dict(self.artifacts[role].get('environment', {}), **environment)
        child = subprocess.Popen([binary, *arguments], cwd=working_directory or self.directory, env=environment,
                                 stdin=subprocess.DEVNULL, stdout=stream, stderr=subprocess.STDOUT,
                                 start_new_session=True, user=uid, group=uid,
                                 extra_groups=[] if uid is not None else None)
        self.children.append(child)
        return child

    def wait_file(self, path, child, seconds=120):
        deadline = time.monotonic() + seconds
        while not path.exists():
            require(child.poll() is None, 'real producer exited; inspect private process log')
            require(time.monotonic() < deadline, 'real producer readiness deadline')
            time.sleep(0.05)

    def wait_port(self, number, child):
        deadline = time.monotonic() + 30
        while True:
            require(child.poll() is None, 'production service exited; inspect private process log')
            try:
                with socket.create_connection(('127.0.0.1', number), timeout=0.2):
                    return
            except OSError:
                require(time.monotonic() < deadline, 'production service readiness deadline')
                time.sleep(0.05)

    def close(self):
        for child in reversed(self.children):
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait(timeout=5)
        for stream in self.streams:
            stream.close()


def post(endpoint, path, token, value, context, expected):
    request = urllib.request.Request(endpoint + path, data=json.dumps(value).encode(), method='POST',
                                    headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
    try:
        with urllib.request.urlopen(request, context=context, timeout=15) as response:
            require(response.status == expected, 'actual authority admission status differs')
            return json.loads(response.read(1_048_576), object_pairs_hook=unique)
    except urllib.error.HTTPError as error:
        raise ValueError('actual authority admission refused: HTTP ' + str(error.code)) from None


def token_file(directory, name):
    return write(directory / name, secrets.token_hex(32).encode())


def produce(manifest_path, output, corpus_path, run_verify):
    value = load(manifest_path)
    require(value['source'] == identity() and set(value['artifacts']) == set(ROLES) | {'real-node-tests'},
            'complete published producer artifact manifest required')
    cached = {**value, 'artifacts': {role: value['artifacts'][role] for role in ROLES}}
    compatible(cached, set(ROLES), runtime=True)
    for record in value['artifacts'].values():
        require(artifact(record['artifact']['path']) == record['artifact'], 'producer artifact changed')
    source = value['source']
    corpus = load(corpus_path)
    require(corpus['source_revision'] == source['revision'], 'actual native corpus and HTTP candidate differ')
    directory = private(output, True, True)
    native_directory = private(directory / 'native', True, True)
    runtime = Runtime(directory, value['artifacts'])
    owner = None
    try:
        native_env = dict(os.environ, PAXEER_X_PROGRAM_HTTP_PRODUCER_DIR=str(native_directory),
                          LAYERX_MULTI_ASSET_CUSTODY_FIXTURE=value['custody_fixture'],
                          LAYERX_TEST_NATIVE_BIN_DIR=str(Path(value['artifacts']['layerxd']['artifact']['path']).parent),
                          PAXEER_X_NATIVE_INTERFACE_BOUNDARY=value['artifacts']['boundary']['artifact']['path'],
                          PAXEER_X_PROGRAM_HTTP_MODULE_REGISTRY_BINARY=value['artifacts']['module-registry']['artifact']['path'],
                          PAXEER_X_REGISTRY_BINARY=value['artifacts']['registry']['artifact']['path'],
                          PAXEER_X_REGISTRY_CONFIGURATION=value['registry_configuration'],
                          PAXEER_X_REGISTRY_CGROUP_PARENT=value['registry_cgroup_parent'])
        native = runtime.start('real-node-tests', native_env, ['--exact', TEST, '--nocapture', '--test-threads=1'])
        runtime.wait_file(native_directory / 'native-owner.json', native)
        owner = load(native_directory / 'native-owner.json')
        require(owner['version'] == 1 and owner['protocol_version'] == 3 and owner['producer_pid'] == native.pid,
                'actual native owner process binding required')
        tls = owner['tls']
        for name in tls:
            private(tls[name])
        context = ssl.create_default_context(cafile=tls['boundary_ca_pem'])
        context.load_cert_chain(tls['client_certificate_pem'], tls['client_key_pem'])
        tokens = private(directory / 'identity-tokens', True, True)
        for name in ('gateway', 'registry', 'registrar', 'webhooks', 'dashboard', 'faucet', 'testnet', 'ramp', 'provisioning'):
            token_file(tokens, name)
        identity_state = private(directory / 'identity-state', True, True)
        identity_port = port()
        identity_env = {'LAYERX_IDENTITY_LISTEN': '127.0.0.1:' + str(identity_port),
                        'LAYERX_IDENTITY_TLS_CERT_DER': tls['boundary_cert_der'],
                        'LAYERX_IDENTITY_TLS_KEY_DER': tls['boundary_key_der'],
                        'LAYERX_IDENTITY_STATE_DIR': str(identity_state),
                        'LAYERX_IDENTITY_SERVICE_TOKENS_DIR': str(tokens),
                        'LAYERX_IDENTITY_STORE_KEY_FILE': token_file(directory, 'identity-store-key'),
                        'LAYERX_IDENTITY_SESSION_TTL_SECONDS': '3600'}
        identity_child = runtime.start('identity', identity_env)
        runtime.wait_port(identity_port, identity_child)
        identity_endpoint = 'https://localhost:' + str(identity_port)
        provisioning = (tokens / 'provisioning').read_text()
        post(identity_endpoint, '/v1/principals', provisioning,
             {'tenant': 'python-sdk-fixture', 'sub': owner['actor_did'],
              'allowed_signer_public_keys': [owner['actor_public_key']], 'account': owner['actor_account'], 'audiences': []}, context, 200)
        session = post(identity_endpoint, '/v1/sessions', provisioning, {'sub': owner['actor_did']}, context, 200)
        authority_port = port()
        authority_token = token_file(directory, 'authority-token')
        authority_env = {'LAYERX_AUTHORITY_LISTEN': '127.0.0.1:' + str(authority_port),
                         'LAYERX_AUTHORITY_TLS_CERT_DER': tls['boundary_cert_der'],
                         'LAYERX_AUTHORITY_TLS_KEY_DER': tls['boundary_key_der'],
                         'LAYERX_AUTHORITY_CLIENT_CA_DER': tls['boundary_client_ca_der'],
                         'LAYERX_AUTHORITY_TOKEN_FILES': authority_token,
                         'LAYERX_AUTHORITY_REPLICA_URL': owner['replica_endpoint'],
                         'LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE': owner['replica_authorization_file'],
                         'LAYERX_AUTHORITY_REPLICA_ID': owner['replica_id'],
                         'LAYERX_AUTHORITY_LNI_SOCKET': owner['node_socket'],
                         'LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID': str(owner['network_id']),
                         'LAYERX_AUTHORITY_NETWORK_ID': str(owner['network_id']), 'LAYERX_AUTHORITY_WIRE_VERSION': '3',
                         'LAYERX_AUTHORITY_SEQUENCER_ID': owner['sequencer_id'],
                         'LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY': owner['sequencer_public_key'],
                         'LAYERX_AUTHORITY_FIRST_BATCH': str(owner['first_batch']),
                         'LAYERX_AUTHORITY_LAST_BATCH': str(owner['last_batch'])}
        authority_directory = Path(owner['node_root']) / 'python-http-authority'
        authority_directory.mkdir(mode=0o700)
        os.chown(authority_directory, 65534, 65534)
        for key in ('LAYERX_AUTHORITY_TLS_CERT_DER', 'LAYERX_AUTHORITY_TLS_KEY_DER',
                    'LAYERX_AUTHORITY_CLIENT_CA_DER', 'LAYERX_AUTHORITY_TOKEN_FILES',
                    'LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE'):
            target = authority_directory / key.lower()
            write(target, Path(authority_env[key]).read_bytes())
            os.chown(target, 65534, 65534)
            authority_env[key] = str(target)
        authority_binary = Path(owner['node_root']) / 'layerx-receipt-authority'
        shutil.copyfile(value['artifacts']['authority']['artifact']['path'], authority_binary)
        authority_binary.chmod(0o755)
        require(artifact(authority_binary)['sha256'] == value['artifacts']['authority']['artifact']['sha256'],
                'accessible native-peer authority executable differs')
        authority_child = runtime.start('authority', authority_env, uid=65534,
                                        working_directory=authority_directory, binary=str(authority_binary))
        runtime.wait_port(authority_port, authority_child)
        redis_state = private(directory / 'redis', True, True)
        redis_port = port()
        redis_password = secrets.token_hex(32)
        acl = write(redis_state / 'users.acl', ('user default off\nuser qualification on >' + redis_password + ' +@all ~* &*\n').encode())
        redis_config = write(redis_state / 'redis.conf', (
            f'bind 127.0.0.1\nport 0\ntls-port {redis_port}\ntls-cert-file {tls["boundary_cert_pem"]}\n'
            f'tls-key-file {tls["boundary_key_pem"]}\ntls-ca-cert-file {tls["boundary_ca_pem"]}\n'
            f'tls-auth-clients no\naclfile {acl}\nappendonly yes\nappendfsync always\ndir {redis_state}\nprotected-mode yes\n').encode())
        redis_child = runtime.start('redis', {}, [redis_config])
        runtime.wait_port(redis_port, redis_child)
        gateway_port = port()
        gateway_env = {'LAYERX_GATEWAY_LISTEN': '127.0.0.1:' + str(gateway_port),
                       'LAYERX_GATEWAY_TLS_CERT_DER': tls['boundary_cert_der'],
                       'LAYERX_GATEWAY_TLS_KEY_DER': tls['boundary_key_der'],
                       'LAYERX_GATEWAY_OUTBOUND_CA_DER': tls['boundary_ca_der'],
                       'LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12': tls['client_pkcs12'],
                       'LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE': tls['client_password_file'],
                       'LAYERX_GATEWAY_COMPONENT_URL': owner['boundary_endpoint'],
                       'LAYERX_GATEWAY_COMPONENT_TOKEN_FILE': owner['boundary_authorization_file'],
                       'LAYERX_GATEWAY_PUBLIC_CORE_URL': owner['boundary_endpoint'],
                       'LAYERX_GATEWAY_AUTHORITY_URL': 'https://localhost:' + str(authority_port),
                       'LAYERX_GATEWAY_AUTHORITY_TOKEN_FILE': authority_token,
                       'LAYERX_GATEWAY_IDENTITY_URL': identity_endpoint,
                       'LAYERX_GATEWAY_IDENTITY_TOKEN_FILE': str(tokens / 'gateway'),
                       'LAYERX_GATEWAY_PROGRAM_REGISTRY_URL': owner['registry_endpoint'],
                       'LAYERX_GATEWAY_PROGRAM_REGISTRY_TOKEN_FILE': owner['registry_authorization_file'],
                       'LAYERX_GATEWAY_SEQUENCER_PUBLIC_KEY_FILE': write(directory / 'sequencer-key', owner['sequencer_public_key'].encode()),
                       'LAYERX_GATEWAY_SEQUENCER_ID_FILE': write(directory / 'sequencer-id', owner['sequencer_id'].encode()),
                       'LAYERX_GATEWAY_SEQUENCER_FIRST_BATCH_FILE': write(directory / 'first-batch', str(owner['first_batch']).encode()),
                       'LAYERX_GATEWAY_SEQUENCER_LAST_BATCH_FILE': write(directory / 'last-batch', str(owner['last_batch']).encode()),
                       'LAYERX_GATEWAY_KEY_PROVISIONING_KEY_FILE': token_file(directory, 'gateway-provisioning-key'),
                       'LAYERX_GATEWAY_NETWORK_ID': str(owner['network_id']),
                       'LAYERX_GATEWAY_PROTOCOL_NETWORK_ID': str(owner['network_id']),
                       'LAYERX_GATEWAY_LXP_WIRE_VERSION': '3',
                       'LAYERX_GATEWAY_MODULE_REGISTRY_FILE': owner['module_registry_file'],
                       'LAYERX_GATEWAY_REDIS_URL': 'rediss://localhost:' + str(redis_port),
                       'LAYERX_GATEWAY_REDIS_USERNAME_FILE': write(directory / 'redis-username', b'qualification'),
                       'LAYERX_GATEWAY_REDIS_PASSWORD_FILE': write(directory / 'redis-password', redis_password.encode())}
        gateway_child = runtime.start('gateway', gateway_env)
        runtime.wait_port(gateway_port, gateway_child)
        endpoint = 'https://localhost:' + str(gateway_port)
        issued = post(endpoint, '/v1/keys', session['token'],
                      {'signer_public_key': owner['actor_public_key'], 'scopes': ['program:call'],
                       'quota_requests': 1000, 'quota_window_seconds': 60}, context, 201)
        require(isinstance(issued.get('key'), dict) and issued['key'].get('authorization_scheme') == 'LayerX-Key'
                and issued['key'].get('signer_public_key') == owner['actor_public_key']
                and isinstance(issued['key'].get('id'), str) and isinstance(issued['key'].get('secret'), str),
                'genuine LayerX key issuance required')
        key_id, key_secret = issued['key']['id'], issued['key']['secret']
        key_file = write(directory / 'gateway-key', key_secret.encode())
        write(Path(owner['control']['calls_sign_request_file']), b'fresh-signed-calls')
        runtime.wait_file(Path(owner['control']['calls_signed_file']), native, 30)
        owner = load(native_directory / 'native-owner.json')
        def row(record):
            require(set(record) == {'guest_abi_version', 'program_id', 'payload_file', 'signed_activity_file', 'fee_limit', 'expected_result_code'},
                    'exact genuine signed native call export required')
            private(record['payload_file']); private(record['signed_activity_file'])
            return {'guest_abi': record['guest_abi_version'], 'payload_file': record['payload_file'],
                    'signed_activity_file': record['signed_activity_file'], 'fee_limit': record['fee_limit'],
                    'expected_result_code': record['expected_result_code']}
        fixture = {'version': 1, 'isolated': True, 'approved_program_calls': True,
                   'endpoint': endpoint, 'network_id': owner['network_id'],
                   'sequencer_public_key': owner['sequencer_public_key'], 'gateway_key_id': key_id,
                   'gateway_key_file': key_file, 'gateway_pid': gateway_child.pid,
                   'gateway_source_manifest': write(directory / 'gateway-source.json', {
                       'binary_sha256': value['artifacts']['gateway']['artifact']['sha256'],
                       'sources': {relative: artifact(ROOT / relative)['sha256'] for relative in
                           ('platform/hosted/gateway/src/main.rs', 'platform/hosted/gateway/src/native_call.rs')}}),
                   'programs': [row(record) for record in owner['programs']],
                   'refused_call': row(owner['refused_call']), 'unknown_call': row(owner['unknown_call'])}
        fixture_path = write(directory / 'http-fixture.json', fixture)
        control_path = write(directory / 'consumer-control.json', dict(
            {key: owner['control'][key] for key in
             ('refused_activity_id', 'node_disconnect_request_file', 'node_disconnected_file')}, node_pid=owner['node_pid']))
        environment = dict(os.environ, LAYERX_PYTHON_PROGRAM_HTTP_FIXTURE=fixture_path,
                           PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS=str(Path(corpus_path).absolute()),
                           PAXEER_X_MAINLINE=source['revision'], PAXEER_X_PROGRAM_HTTP_CONTROL=control_path,
                           SSL_CERT_FILE=tls['boundary_ca_pem'], PYTHONDONTWRITEBYTECODE='1')
        write(directory / 'ready.json', {'source': source, 'gateway_pid': gateway_child.pid,
              'native_pid': native.pid, 'fixture': fixture_path, 'control': control_path,
              'corpus': str(Path(corpus_path).absolute()), 'ssl_cert_file': tls['boundary_ca_pem']})
        if not run_verify:
            print(directory / 'ready.json', flush=True)
            deadline = time.monotonic() + 1200
            while not Path(owner['control']['stop_file']).exists():
                require(time.monotonic() < deadline and native.poll() is None, 'producer serving deadline or native owner exit')
                time.sleep(0.1)
            return 0
        log = directory / 'verify.log'
        with log.open('xb') as stream:
            process = subprocess.run(VERIFY, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                     stdout=stream, stderr=subprocess.STDOUT, timeout=1200)
        write(directory / 'result.json', {'revision': source['revision'], 'command': VERIFY,
              'exit_code': process.returncode, 'log': str(log), 'fixture': fixture_path,
              'native_corpus': str(Path(corpus_path).absolute())})
        return process.returncode
    finally:
        if owner is not None and not Path(owner['control']['stop_file']).exists():
            write(Path(owner['control']['stop_file']), b'stop')
            if runtime.children:
                try:
                    runtime.children[0].wait(timeout=15)
                except subprocess.TimeoutExpired:
                    pass
        runtime.close()


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    parser.add_argument('--cache-manifest')
    parser.add_argument('--manifest')
    parser.add_argument('--output', required=True)
    parser.add_argument('--corpus', default=os.environ.get('PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS'))
    parser.add_argument('--verify', action='store_true')
    args = parser.parse_args()
    try:
        if args.build:
            require(args.cache_manifest and not args.verify, 'explicit cache owner manifest required before declared build')
            build(args.cache_manifest, args.output)
            return 0
        require(args.manifest and args.corpus, 'source-bound producer artifacts and genuine native corpus required')
        with open('/root/lx-cargo/platform-tooling.lock', 'a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            return produce(args.manifest, args.output, args.corpus, args.verify)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print('program HTTP producer refused: ' + str(error), file=sys.stderr)
        return 78


if __name__ == '__main__':
    sys.exit(main())
