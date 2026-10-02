#!/usr/bin/env python3
import atexit
import base64
import hashlib
import ssl
import urllib.request
import http.server
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import stat
import struct
import subprocess
import sys
import threading
import time

COUNT = 0
ROOT = Path(__file__).resolve().parents[3]
ISSUER = 'https://identity.example.test/auth/v1'
AUDIENCE = 'authenticated'
TENANT = 'human-provider'
BINDING_TYPE = 'layerx-wallet-binding+jwt'
WALLET_DID = 'did:layerx:3f1c0a9e5b7d2468ace013579bdf2468ace013579bdf2468ace013579bdf2468'
OTHER_DID = 'did:layerx:9a8b7c6d5e4f30211203f4e5d6c7b8a99a8b7c6d5e4f30211203f4e5d6c7b8a9'
CASES = []

def require(value, message):
    if not value:
        raise ValueError(message)

def case(name, value):
    global COUNT
    require(value, 'acceptance unmet: ' + name)
    COUNT += 1
    CASES.append(name)

def executable(name):
    value = os.environ.get(name)
    require(value, 'prerequisite missing: ' + name)
    path = Path(value)
    require(path.is_absolute() and path.is_file() and os.access(path, os.X_OK), 'prerequisite not executable: ' + name)
    return path

def b64(data):
    return base64.urlsafe_b64encode(data).rstrip(b'=').decode()

class Key:
    def __init__(self, directory, name):
        self.path = directory / (name + '.pem')
        subprocess.run(['openssl', 'ecparam', '-name', 'prime256v1', '-genkey', '-noout', '-out', str(self.path)],
                       check=True, capture_output=True)
        os.chmod(self.path, 0o600)
        der = subprocess.run(['openssl', 'ec', '-in', str(self.path), '-pubout', '-outform', 'DER'],
                             check=True, capture_output=True).stdout
        self.point = der[-65:]
        require(self.point[0] == 4, 'unexpected public key encoding')

    def jwk(self, kid):
        return {'kty': 'EC', 'crv': 'P-256', 'kid': kid, 'use': 'sig', 'alg': 'ES256',
                'x': b64(self.point[1:33]), 'y': b64(self.point[33:])}

    def sign(self, header, claims):
        signing_input = b64(json.dumps(header, separators=(',', ':')).encode()) + '.' + \
            b64(json.dumps(claims, separators=(',', ':')).encode())
        der = subprocess.run(['openssl', 'dgst', '-sha256', '-sign', str(self.path)], input=signing_input.encode(),
                             check=True, capture_output=True).stdout
        return signing_input + '.' + b64(raw_signature(der))

    def assertion(self, subject, now):
        return self.sign({'alg': 'ES256', 'typ': 'JWT', 'kid': 'supabase-1'},
                         {'iss': ISSUER, 'sub': subject, 'aud': AUDIENCE, 'exp': now + 3600, 'iat': now,
                          'nbf': now - 5, 'role': 'authenticated'})

    def binding(self, subject, did, tenant):
        return self.sign({'alg': 'ES256', 'typ': BINDING_TYPE},
                         {'iss': ISSUER, 'sub': subject, 'did': did, 'tenant': tenant})

def raw_signature(der):
    require(der[0] == 0x30, 'signature encoding')
    offset = 2 if der[1] < 0x80 else 2 + (der[1] & 0x7f)
    parts = []
    for _ in range(2):
        require(der[offset] == 0x02, 'signature integer')
        length = der[offset + 1]
        value = der[offset + 2:offset + 2 + length].lstrip(b'\x00')
        require(len(value) <= 32, 'signature integer length')
        parts.append(value.rjust(32, b'\x00'))
        offset += 2 + length
    return b''.join(parts)

def serve_keys(keys):
    body = json.dumps({'keys': keys}).encode()
    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path != '/auth/v1/.well-known/jwks.json':
                self.send_error(404)
                return
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        def log_message(self, *args):
            pass
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server, 'http://127.0.0.1:%d/auth/v1/.well-known/jwks.json' % server.server_address[1]

def exchange(path, payload):
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(5)
        connection.connect(str(path))
        connection.sendall(struct.pack('>I', len(payload)) + payload)
        size = struct.unpack('>I', read_exact(connection, 4))[0]
        require(6 <= size <= 1048576, 'response length out of bound')
        return read_exact(connection, size)

def read_exact(connection, size):
    data = b''
    while len(data) < size:
        chunk = connection.recv(size - len(data))
        require(chunk, 'peer closed early')
        data += chunk
    return data

def lxip(path, operation, fields):
    payload = b'LXIP\x01' + bytes([operation]) + struct.pack('>I', len(fields))
    for field in fields:
        payload += struct.pack('>I', len(field)) + field
    response = exchange(path, payload)
    require(response[:5] == b'LXIP\x01', 'response version')
    count = struct.unpack('>I', response[6:10])[0]
    rest, fields = response[10:], []
    for _ in range(count):
        length = struct.unpack('>I', rest[:4])[0]
        fields.append(rest[4:4 + length])
        rest = rest[4 + length:]
    require(not rest, 'trailing response bytes')
    return response[5], fields

def lxib(path, tenant, principal):
    response = exchange(path, b'LXIB\x01' + json.dumps({'operation': 'principal', 'tenant': tenant, 'principal': principal}).encode())
    require(response[:5] == b'LXIB\x01', 'binding response version')
    return json.loads(response[5:])

def login(path, token, binding=None):
    fields = [token.encode()] + ([binding.encode()] if binding is not None else [])
    status, values = lxip(path, 4, fields)
    return status, [value.decode() for value in values]

def stop_process(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)


def terminate(signum, frame):
    raise TimeoutError('qualification interrupted')


class Provider:
    starts = 0

    def __init__(self, clock, binary, state, env):
        Provider.starts += 1
        runtime = state / ('clock-%d' % Provider.starts)
        runtime.mkdir(mode=0o700)
        with open(state / 'provider.log', 'ab') as log:
            self.process = subprocess.Popen([str(clock), '--runtime-dir', str(runtime), '--', str(binary), 'serve'],
                                            env=env, stdout=subprocess.DEVNULL, stderr=log)
        atexit.register(stop_process, self.process)
        deadline = time.monotonic() + 10
        while True:
            try:
                if lxip(state / 'identity.sock', 0, []) == (0, []):
                    return
            except (OSError, ValueError):
                pass
            require(self.process.poll() is None, 'provider exited during startup')
            require(time.monotonic() < deadline, 'provider did not become ready')
            time.sleep(0.05)

    def stop(self):
        self.process.terminate()
        require(self.process.wait(timeout=10) == 0, 'provider did not stop cleanly')

class Attestors:
    def __init__(self, binary, directory, jwks_url, assertion):
        self.children = []
        atexit.register(self.stop)
        directory.mkdir(mode=0o700)
        def run(*args):
            return subprocess.run(['openssl', *args], cwd=directory, check=True,
                                  capture_output=True, timeout=15).stdout
        def authority(name):
            run('req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:prime256v1',
                '-nodes', '-keyout', name + '.key', '-out', name + '.pem', '-days', '1',
                '-subj', '/CN=' + name, '-addext', 'basicConstraints=critical,CA:TRUE')
        def leaf(ca, name, serial, server):
            run('req', '-new', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:prime256v1',
                '-nodes', '-keyout', name + '.key', '-out', name + '.csr', '-subj', '/CN=' + name)
            extension = 'basicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature\n'
            extension += ('extendedKeyUsage=serverAuth,clientAuth\nsubjectAltName=IP:127.0.0.1\n'
                          if server else 'extendedKeyUsage=clientAuth\n')
            (directory / (name + '.ext')).write_text(extension)
            run('x509', '-req', '-in', name + '.csr', '-CA', ca + '.pem', '-CAkey', ca + '.key',
                '-set_serial', str(serial), '-days', '1', '-sha256', '-extfile', name + '.ext',
                '-out', name + '.pem')
            os.chmod(directory / (name + '.key'), 0o600)
        for name in ['node-ca', 'gateway-ca', 'operator-ca']:
            authority(name)
            os.chmod(directory / (name + '.key'), 0o600)
        leaf('gateway-ca', 'gateway', 1, False)
        ids = ['node-%d' % number for number in range(1, 6)]
        pins = []
        for number, node in enumerate(ids, 2):
            leaf('node-ca', node, number, True)
            run('x509', '-in', node + '.pem', '-noout', '-pubkey', '-out', node + '.pub')
            spki = run('pkey', '-pubin', '-in', node + '.pub', '-outform', 'DER')
            pins.append(hashlib.sha256(spki).hexdigest())
        (directory / 'api-ca.pem').write_bytes((directory / 'node-ca.pem').read_bytes()
                                              + (directory / 'gateway-ca.pem').read_bytes())
        for name in ['node-ca', 'gateway']:
            run('x509', '-in', name + '.pem', '-outform', 'DER', '-out', name + '.der')
        run('pkcs8', '-topk8', '-nocrypt', '-in', 'gateway.key', '-outform', 'DER', '-out', 'gateway.pk8')
        os.chmod(directory / 'gateway.pk8', 0o600)
        reservations = []
        addresses = []
        try:
            for _ in range(10):
                reservation = socket.socket()
                reservation.bind(('127.0.0.1', 0))
                reservations.append(reservation)
                addresses.append('127.0.0.1:%d' % reservation.getsockname()[1])
            for index, node in enumerate(ids):
                key = directory / (node + '.node-key')
                with os.fdopen(os.open(key, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as handle:
                    handle.write(os.urandom(32).hex())
                others = [i for i in range(5) if i != index]
                env = {
                    'ATTESTOR_NODE_ID': node, 'ATTESTOR_REGION': 'local',
                    'ATTESTOR_LISTEN_ADDR': addresses[index],
                    'ATTESTOR_PEER_LISTEN_ADDR': addresses[index + 5],
                    'ATTESTOR_PEERS': ','.join(ids[i] + '=' + addresses[i + 5] for i in others),
                    'ATTESTOR_PEER_PINS': ','.join(ids[i] + '=' + pins[i] for i in others),
                    'ATTESTOR_NODE_KEY_FILE': str(key),
                    'ATTESTOR_DATA_DIR': str(directory / (node + '.data')),
                    'ATTESTOR_CHAIN_ID': '125', 'ATTESTOR_JWKS_URL': jwks_url,
                    'ATTESTOR_JWT_ISSUER': ISSUER, 'ATTESTOR_JWT_AUDIENCE': AUDIENCE,
                    'ATTESTOR_TLS_CERT_FILE': str(directory / (node + '.pem')),
                    'ATTESTOR_TLS_KEY_FILE': str(directory / (node + '.key')),
                    'ATTESTOR_TLS_CA_FILE': str(directory / 'api-ca.pem'),
                    'ATTESTOR_OPERATOR_CA_FILE': str(directory / 'operator-ca.pem'),
                }
                reservations[index].close()
                reservations[index + 5].close()
                with open(directory / (node + '.log'), 'wb') as log:
                    self.children.append(subprocess.Popen([str(binary)], env=env, stdin=subprocess.DEVNULL,
                                                          stdout=log, stderr=subprocess.STDOUT))
        finally:
            for reservation in reservations:
                reservation.close()
        context = ssl.create_default_context(cafile=str(directory / 'node-ca.pem'))
        context.load_cert_chain(str(directory / 'gateway.pem'), str(directory / 'gateway.key'))
        deadline = time.monotonic() + 30
        pending = set(range(5))
        while pending:
            require(all(child.poll() is None for child in self.children), 'attestor exited; inspect retained node logs')
            require(time.monotonic() < deadline, 'actual local attestor quorum did not become ready')
            for index in list(pending):
                try:
                    with urllib.request.urlopen('https://' + addresses[index] + '/health', context=context, timeout=1) as response:
                        report = json.load(response)
                    if report.get('ready') is True and report.get('reachable_peers') == 4:
                        pending.remove(index)
                except (OSError, ValueError):
                    pass
            if pending:
                time.sleep(0.05)
        self.fixture = directory / 'admission.json'
        with os.fdopen(os.open(self.fixture, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as handle:
            json.dump({'nodes': list(zip(ids, addresses[:5])),
                       'root_certificate': str(directory / 'node-ca.der'),
                       'client_certificate': str(directory / 'gateway.der'),
                       'client_private_key': str(directory / 'gateway.pk8'),
                       'jwks_url': jwks_url, 'assertion': assertion, 'subject': 'supabase-user-a'}, handle)

    def stop(self):
        for child in self.children:
            if child.poll() is None:
                child.terminate()
        for child in self.children:
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)

def run():
    os.umask(0o077)
    attestor = executable('PAXEER_X_ATTESTOR_BIN')
    provider = executable('PAXEER_X_IDENTITY_PROVIDER_BIN')
    principal_store = executable('PAXEER_X_PRINCIPAL_STORE_TEST_BIN')
    clock = executable('LAYERX_RUNTIME_CLOCK_BIN')
    require(shutil.which('openssl'), 'prerequisite missing: openssl')
    evidence_root = os.environ.get('PAXEER_X_EVIDENCE_DIR')
    require(evidence_root and Path(evidence_root).is_absolute(), 'prerequisite missing: PAXEER_X_EVIDENCE_DIR')
    evidence_root = Path(evidence_root)
    evidence_root.mkdir(mode=0o700, parents=True, exist_ok=True)
    info = evidence_root.stat()
    require(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) & 0o077 == 0, 'evidence directory must be private')
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    state = evidence_root / ('iab-%d' % time.time_ns())
    state.mkdir(mode=0o700)
    keys = state / 'keys'
    keys.mkdir(mode=0o700)
    supabase, producer, forger = Key(keys, 'issuer'), Key(keys, 'producer'), Key(keys, 'forger')
    jwks, jwks_url = serve_keys([supabase.jwk('supabase-1')])
    atexit.register(jwks.shutdown)
    policy = state / 'policy.json'
    with os.fdopen(os.open(policy, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as handle:
        json.dump({'root': [0x43] * 32, 'threshold': 1, 'delay_seconds': 86400}, handle)
    uid = str(os.geteuid())
    env = {k: v for k, v in os.environ.items() if not k.startswith(('LAYERX_', 'ATTESTOR_'))}
    env.update({
        'LAYERX_HUMAN_IDENTITY_PROVIDER_STATE_ROOT': str(state / 'identity'),
        'LAYERX_HUMAN_IDENTITY_PROVIDER_SOCKET': str(state / 'identity.sock'),
        'LAYERX_HUMAN_IDENTITY_PROVIDER_RECOVERY_POLICY_FILE': str(policy),
        'LAYERX_HUMAN_IDENTITY_PROVIDER_ALLOWED_UID': uid,
        'LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_JWKS_URL': jwks_url,
        'LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_ISSUER': ISSUER,
        'LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_AUDIENCE': AUDIENCE,
        'LAYERX_HUMAN_IDENTITY_PROVIDER_ASSERTION_BINDING_PRODUCER_KEY': producer.point.hex(),
        'LAYERX_HUMAN_IDENTITY_PROVIDER_BINDING_SOCKET': str(state / 'binding.sock'),
        'LAYERX_HUMAN_IDENTITY_PROVIDER_BINDING_TENANT': TENANT,
        'LAYERX_HUMAN_IDENTITY_PROVIDER_BINDING_ALLOWED_UIDS': uid,
    })
    sock, reader = state / 'identity.sock', state / 'binding.sock'
    now = int(time.time())
    alice = supabase.assertion('supabase-user-a', now)
    bob = supabase.assertion('supabase-user-b', now)
    running = Provider(clock, provider, state, env)
    try:
        status, pending = login(sock, alice)
        case('first login without binding is explicit pending state', status == 0 and len(pending) == 1 and pending[0].startswith('act_'))
        principal = pending[0]
        case('pending principal has no LXIB binding', lxib(reader, TENANT, principal) == {'status': 'refused'})
        case('unverified assertion refused', login(sock, forger.assertion('supabase-user-a', now), producer.binding('supabase-user-a', WALLET_DID, TENANT))[0] == 2)
        case('caller-selected DID text refused', login(sock, alice, WALLET_DID)[0] == 1)
        case('binding from unauthenticated producer refused', login(sock, alice, forger.binding('supabase-user-a', WALLET_DID, TENANT))[0] == 3)
        case('binding for another subject refused', login(sock, alice, producer.binding('supabase-user-b', WALLET_DID, TENANT))[0] == 3)
        case('binding for wrong tenant refused', login(sock, alice, producer.binding('supabase-user-a', WALLET_DID, 'other-tenant'))[0] == 3)
        case('refusals leave the account pending', login(sock, alice) == (0, [principal]))
        binding = producer.binding('supabase-user-a', WALLET_DID, TENANT)
        case('authenticated binding consumed on first bound login', login(sock, alice, binding) == (0, [principal, WALLET_DID]))
        case('repeat login resolves same principal and DID', login(sock, alice) == (0, [principal, WALLET_DID]) and login(sock, alice, binding) == (0, [principal, WALLET_DID]))
        case('changed recorded DID refused', login(sock, alice, producer.binding('supabase-user-a', OTHER_DID, TENANT))[0] == 3)
        case('DID owned by another subject refused', login(sock, bob, producer.binding('supabase-user-b', WALLET_DID, TENANT))[0] == 3)
        status, bob_fields = login(sock, bob, producer.binding('supabase-user-b', OTHER_DID, TENANT))
        case('distinct subject resolves distinct principal', status == 0 and bob_fields[1:] == [OTHER_DID] and bob_fields[0] != principal)
        bound = lxib(reader, TENANT, principal)
        case('LXIB resolves principal, DID and configured tenant', bound == {'status': 'bound', 'tenant': TENANT, 'principal': principal, 'did': WALLET_DID})
        case('LXIB refuses a wrong tenant', lxib(reader, 'other-tenant', principal) == {'status': 'refused'})
        status, passkey = lxip(sock, 1, [b'carol@example.com', b'Person', b'carol', struct.pack('>Q', 1)])
        case('passkey provisioning coexists without merging', status == 0 and len(passkey) == 5 and passkey[0].decode() not in (principal, bob_fields[0]))
        carol = passkey[0].decode()
        carol_binding = lxib(reader, TENANT, carol)
        case('passkey binding contract unchanged', carol_binding.get('status') == 'bound' and carol_binding.get('did') not in (WALLET_DID, OTHER_DID))
        running.stop()
        running = Provider(clock, provider, state, env)
        case('restart preserves principal and DID', login(sock, alice) == (0, [principal, WALLET_DID]) and login(sock, bob) == (0, bob_fields))
        case('restart preserves LXIB tenant binding', lxib(reader, TENANT, principal) == bound and lxib(reader, TENANT, carol) == carol_binding)
        case('restart keeps changed DID refused', login(sock, alice, producer.binding('supabase-user-a', OTHER_DID, TENANT))[0] == 3)
    finally:
        running.process.terminate()
        running.process.wait(timeout=10)
    attestors = Attestors(attestor, state / 'attestors', jwks_url, alice)
    test_log = state / 'principal_store.log'
    test_env = {k: v for k, v in os.environ.items() if not k.startswith(('LAYERX_', 'ATTESTOR_'))}
    test_env['TMPDIR'] = str(state)
    test_env['PAXEER_X_IDENTITY_ATTESTOR_FIXTURE'] = str(attestors.fixture)
    (state / 'test-clock').mkdir(mode=0o700)
    with open(test_log, 'wb') as handle:
        result = subprocess.run([str(clock), '--runtime-dir', str(state / 'test-clock'), '--', str(principal_store), '--test-threads=1'],
                                env=test_env, stdout=handle, stderr=subprocess.STDOUT, cwd=state, timeout=600)
    output = test_log.read_text(errors='replace')
    summary = re.search(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored', output)
    require(result.returncode == 0 and summary and summary.group(2) == '0' and summary.group(3) == '0',
            'principal_store integration failed (exit %d), log %s' % (result.returncode, test_log))
    passed = set(re.findall(r'^test (\S+) \.\.\. ok$', output, re.M))
    required_tests = {
        'actual_provider_binding_preserves_dynamic_store_isolation_and_restart',
        'actual_provider_refuses_static_conflicts_and_durable_binding_replacement',
        'assertion_subject_resolves_one_durable_wallet_principal_and_tenant_across_restart',
        'assertion_capability_reaches_existing_attestor_admission',
    }
    require(required_tests <= passed, 'principal_store integration corpus incomplete')
    for name in sorted(required_tests):
        case('principal_store::' + name, True)
    attestors.stop()
    jwks.shutdown()
    record = state / 'evidence.json'
    with os.fdopen(os.open(record, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as handle:
        json.dump({'schema': 'paxeer-x.identity-assertion-binding.v1', 'revision': revision,
                   'provider': str(provider), 'principal_store': str(principal_store), 'clock': str(clock),
                   'attestor': str(attestor),
                   'cases': CASES, 'tests': COUNT, 'skipped': 0}, handle, indent=2)
    print('revision ' + revision)
    print('command timeout 15m python3 tools/qualification/paxeer-x/identity-assertion-binding.py')
    print('exit 0')
    print('evidence ' + str(record))
    print('PAXEER_X_GATE tests=%d skipped=0' % COUNT)

if __name__ == '__main__':
    signal.signal(signal.SIGTERM, terminate)
    try:
        run()
    except Exception as error:
        print('identity assertion binding refused: %s: %s' % (type(error).__name__, error), file=sys.stderr)
        print('exit 1')
        print('PAXEER_X_GATE tests=%d skipped=0' % COUNT)
        sys.exit(1)
