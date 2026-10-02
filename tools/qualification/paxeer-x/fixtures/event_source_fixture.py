"""Disposable chain-backed payment event source for the event delivery contract.

Brings up, from real compiled binaries only, the closure behind a canonical
payment event on the owned chain-125 RuntimeFixture:

  layerx-identity            principal + session with the treasury signer allowed
  layerx-receipt-authority   receipts from the runtime LNI, evidence only from the runtime replica
  layerx-agent-boundary      component receipts (/internal/v1/receipts/{activity})
  redis-server (TLS)         gateway durable store and producer outbox
  layerx-gateway             API key, /v1/settle verification, payment producer, /internal/v1/principal
  layerx-event-source        LAYERX_EVENTS_KIND=payments bound to the gateway upstream

produce_event('payment') submits one real signed send through the runtime's
real client, settles its receipt through the gateway (which verifies it with
the authority and component and enqueues the payment observation), and waits
until the event source has journaled the record and serves it on
/internal/v1/events/{id}. No upstream answer is synthesized here.
"""

import base64
import hashlib
import hmac
import json
import os
import secrets
import shutil
import subprocess
import time
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import pkcs12

from hosted_delivery_fixture import TlsRedis, binary, free_port, private_directory, private_file
from identity_fixture import IdentityFixture
from receipt_authority_fixture import (NETWORK_NAME, WIRE_VERSION, ReceiptAuthorityFixture, der_pair, https,
                                       node_environment, wait_until)

PRODUCER_ROLE = 'urn:layerx:webhooks:role:producer'
TENANT = 'paxeer-x-fixture'
PRINCIPAL = 'paxeer-x-fixture-payer'
MERKLE_LEAF = b'LXP/v1/merkle-leaf\0'
X402_IDEMPOTENCY = b'LayerX/middleware/x402/idempotency\0'
PREPARATION = (('register', 0), ('open', 1), ('open-bob', 0), ('mint', 2), ('burn', 3), ('grant-issue', 4),
               ('grant-revoke', 5))
FIRST_SEND = 6


def event_id(kind, resource, sequence):
    """platform/hosted/internal/src/producer.rs event_id."""
    data = b''
    for part in (kind.encode(), resource.encode(), sequence.to_bytes(8, 'big')):
        data += len(part).to_bytes(8, 'big') + part
    return hashlib.sha256(data).hexdigest()


def receipt_rows(raw):
    return [dict(item.split('=', 1) for item in line.split()[1:])
            for line in raw.decode().splitlines() if line.startswith('receipt ')]


class Service:
    """One owned real binary with a fixed port, environment and private log."""

    def __init__(self, name, directory, environment, ready):
        self.name, self.directory, self.environment, self.ready = name, directory, environment, ready
        self.binary = binary(name)
        self.log_path = directory / f'{name}.log'
        self.process = None

    def start(self, deadline=90.0):
        if self.process is not None and self.process.poll() is None:
            return
        log = open(self.log_path, 'ab')
        try:
            self.process = subprocess.Popen([str(self.binary)], env={'PATH': '/usr/bin:/bin', **self.environment()},
                                            cwd=self.directory, stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                            start_new_session=True)
        finally:
            log.close()
        wait_until(self.ready, self.process, self.name, deadline)

    def stop(self, kill=False):
        if self.process is not None and self.process.poll() is None:
            self.process.kill() if kill else self.process.terminate()
            self.process.wait(timeout=20)
        self.process = None


class EventSourceFixture:
    """Payment event source whose upstream is a real gateway on the owned runtime.

    webhooks_ingress: dict(url=<https private ingress origin>, token_file=<payment source producer bearer>)
    naming the 17.1 private ingress the gateway producer notifies; the ingress may start later
    (the producer outbox retries), but the gateway refuses partial producer configuration.
    """

    def __init__(self, work, ca, runtime, webhooks_ingress, identity=None):
        if not isinstance(webhooks_ingress, dict) or not webhooks_ingress.get('url', '').startswith('https://') \
                or not Path(webhooks_ingress.get('token_file', '')).is_file():
            raise RuntimeError('MISSING webhooks ingress url/token_file for the gateway payment producer')
        for name in ('layerx-gateway', 'layerx-event-source', 'layerx-agent-boundary'):
            binary(name)
        self.ca, self.runtime, self.ingress = ca, runtime, webhooks_ingress
        self.root = Path(work)
        self.work = self.root / 'event-source'
        if self.work.exists():
            raise RuntimeError('event source fixture directory already exists')
        private_directory(self.work)
        self.owned_identity = identity is None
        self.identity = identity or IdentityFixture(self.root, ca)
        self.authority = ReceiptAuthorityFixture(self.root, ca, runtime)
        self.redis = TlsRedis(self.work, ca)
        self.redis.username = 'layerx-gateway'
        private_file(self.redis.materials['username'], self.redis.username)
        self.state_path = self.work / 'fixture-state.json'
        self._materials()
        self.boundary = Service('layerx-agent-boundary', self.dirs['boundary'], self._boundary_env,
                                lambda: self._ready(self.boundary_endpoint))
        self.gateway = Service('layerx-gateway', self.dirs['gateway'], self._gateway_env, self._gateway_listening)
        self.source = Service('layerx-event-source', self.dirs['source'], self._source_env,
                              lambda: self._source_alive())
        self.endpoint = f'https://localhost:{self.ports["source"]}'
        self.registry = RegistryFixture(self)

    def _materials(self):
        d = {name: private_directory(self.work / name) for name in ('boundary', 'gateway', 'source', 'tokens')}
        self.dirs = d
        self.ports = {name: free_port() for name in ('boundary', 'gateway', 'source', 'registry')}
        self.boundary_endpoint = f'https://localhost:{self.ports["boundary"]}'
        self.gateway_endpoint = f'https://localhost:{self.ports["gateway"]}'
        token = lambda name: private_file(d['tokens'] / f'{name}.token', secrets.token_hex(32))
        self.boundary_tokens = {name: token('boundary-' + name) for name in ('gateway', 'registry', 'webhook')}
        self.registry_token = token('gateway-registry')
        self.enrollment_key = token('source-enrollment')
        self.producer_token_file = token('source-producer')
        self.source_token_file = token('source-reader')
        self.principal_token_file = d['source'] / 'principal.credential'
        private_file(d['gateway'] / 'key-provisioning', secrets.token_hex(32))
        self.tls = {name: der_pair(self.ca, d[name], f'layerx-{name}-server') for name in ('boundary', 'gateway', 'source')}
        self.producer_client = der_pair(self.ca, d['gateway'], 'layerx-gateway', [PRODUCER_ROLE], True, [])
        self.reader_client = der_pair(self.ca, d['source'], 'paxeer-x-fixture-source-reader', [], True, [])
        password = secrets.token_hex(24)
        self.p12_password_file = private_file(d['gateway'] / 'client-identity.password', password)
        key = serialization.load_pem_private_key(self.producer_client['key_pem'].read_bytes(), password=None)
        cert = x509.load_pem_x509_certificate(self.producer_client['cert_pem'].read_bytes())
        bundle = pkcs12.serialize_key_and_certificates(b'layerx-gateway', key, cert, None,
                                                       serialization.BestAvailableEncryption(password.encode()))
        self.p12 = d['gateway'] / 'client-identity.p12'
        descriptor = os.open(self.p12, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, 'wb') as handle:
            handle.write(bundle)
        private_file(d['source'] / 'producers.json', json.dumps(
            [{'token_file': str(self.producer_token_file), 'allow_principal_digest': True}]))
        self.materials = {
            'principal_token_file': self.principal_token_file, 'source_token_file': self.source_token_file,
            'producer_token_file': self.producer_token_file, 'producer_client_pkcs12': self.p12,
            'producer_client_password_file': self.p12_password_file, 'reader_client_cert': self.reader_client['cert_pem'],
            'reader_client_key': self.reader_client['key_pem'], 'ca_der': self.ca.ca_der,
            'credentials_map': d['source'] / 'credentials.json', 'journal': d['source'] / 'state',
            'state': self.state_path}

    def _ready(self, endpoint):
        status, _, _ = https('GET', endpoint + '/readyz', self.ca, timeout=3)
        return status == 200

    def _gateway_listening(self):
        status, _, _ = https('GET', self.gateway_endpoint + '/livez', self.ca, timeout=3)
        return status == 200

    def _source_alive(self):
        status, _, _ = https('GET', self.endpoint + '/livez', self.ca, timeout=3)
        return status == 200

    def _sequencer(self):
        public = self.runtime.manifest['sequencer_public_key']
        return public, hashlib.sha256(('layerx-sequencer:' + public).encode()).hexdigest()

    def _boundary_env(self):
        node = node_environment(self.runtime)
        return {
            'LAYERX_AGENT_BOUNDARY_LISTEN': f'127.0.0.1:{self.ports["boundary"]}',
            'LAYERX_AGENT_BOUNDARY_TLS_CERT_DER': str(self.tls['boundary']['cert_der']),
            'LAYERX_AGENT_BOUNDARY_TLS_KEY_DER': str(self.tls['boundary']['key_der']),
            'LAYERX_AGENT_BOUNDARY_CLIENT_CA_DER': str(self.ca.ca_der),
            'LAYERX_AGENT_BOUNDARY_GATEWAY_TOKEN_FILE': str(self.boundary_tokens['gateway']),
            'LAYERX_AGENT_BOUNDARY_REGISTRY_TOKEN_FILE': str(self.boundary_tokens['registry']),
            'LAYERX_AGENT_BOUNDARY_WEBHOOK_TOKEN_FILE': str(self.boundary_tokens['webhook']),
            'LAYERX_AGENT_BOUNDARY_LNI_SOCKET': self.runtime.manifest['node_socket'],
            'LAYERX_AGENT_BOUNDARY_NODE_URL': f'http://127.0.0.1:{self.runtime.ports[7]}',
            'LAYERX_AGENT_BOUNDARY_NODE_BEARER_TOKEN_FILE': node['LAYERX_NODE_PROGRAM_BEARER_TOKEN_FILE'],
            'LAYERX_AGENT_BOUNDARY_STATE_DIR': str(private_directory(self.dirs['boundary'] / 'state')),
            'LAYERX_AGENT_BOUNDARY_PROTOCOL_NETWORK_ID': str(self.runtime.manifest['network_id']),
            'LAYERX_AGENT_BOUNDARY_NETWORK_ID': NETWORK_NAME,
            'LAYERX_AGENT_BOUNDARY_RECEIPT_WAIT_MS': '15000',
        }

    def _module_registry(self):
        path = self.dirs['gateway'] / 'module-registry.json'
        if path.exists():
            return path
        node = node_environment(self.runtime)
        result = subprocess.run([self.runtime.binary('layerx-module-registry'), 'read-node', '--socket',
                                 self.runtime.manifest['node_socket'], '--network-id', str(self.runtime.manifest['network_id']),
                                 '--protocol-version', WIRE_VERSION, '--actor', node['LAYERX_NODE_TREASURY_DID']],
                                capture_output=True, timeout=60)
        if result.returncode != 0:
            (self.dirs['gateway'] / 'module-registry.log').write_bytes(result.stdout + result.stderr)
            raise RuntimeError(f'layerx-module-registry read-node exited {result.returncode}')
        private_file(path, result.stdout.decode())
        return path

    def _gateway_env(self):
        node = node_environment(self.runtime)
        public, sequencer = self._sequencer()
        g = self.dirs['gateway']
        files = {'SEQUENCER_PUBLIC_KEY_FILE': public, 'SEQUENCER_ID_FILE': sequencer,
                 'SEQUENCER_FIRST_BATCH_FILE': node['LAYERX_NODE_FIRST_BATCH'],
                 'SEQUENCER_LAST_BATCH_FILE': node['LAYERX_NODE_LAST_BATCH']}
        env = {f'LAYERX_GATEWAY_{name}': str(private_file(g / name.lower(), value)) for name, value in files.items()}
        env.update({
            'LAYERX_GATEWAY_LISTEN': f'127.0.0.1:{self.ports["gateway"]}',
            'LAYERX_GATEWAY_TLS_CERT_DER': str(self.tls['gateway']['cert_der']),
            'LAYERX_GATEWAY_TLS_KEY_DER': str(self.tls['gateway']['key_der']),
            'LAYERX_GATEWAY_OUTBOUND_CA_DER': str(self.ca.ca_der),
            'LAYERX_GATEWAY_NETWORK_ID': NETWORK_NAME,
            'LAYERX_GATEWAY_LXP_WIRE_VERSION': WIRE_VERSION,
            'LAYERX_GATEWAY_PROTOCOL_NETWORK_ID': str(self.runtime.manifest['network_id']),
            'LAYERX_GATEWAY_REDIS_URL': self.redis.endpoint,
            'LAYERX_GATEWAY_REDIS_USERNAME_FILE': str(self.redis.materials['username']),
            'LAYERX_GATEWAY_REDIS_PASSWORD_FILE': str(self.redis.materials['password']),
            'LAYERX_GATEWAY_COMPONENT_URL': self.boundary_endpoint,
            'LAYERX_GATEWAY_COMPONENT_TOKEN_FILE': str(self.boundary_tokens['gateway']),
            'LAYERX_GATEWAY_CLIENT_IDENTITY_PKCS12': str(self.p12),
            'LAYERX_GATEWAY_CLIENT_IDENTITY_PASSWORD_FILE': str(self.p12_password_file),
            'LAYERX_GATEWAY_KEY_PROVISIONING_KEY_FILE': str(g / 'key-provisioning'),
            'LAYERX_GATEWAY_MODULE_REGISTRY_FILE': str(self._module_registry()),
            'LAYERX_GATEWAY_AUTHORITY_URL': self.authority.endpoint,
            'LAYERX_GATEWAY_AUTHORITY_TOKEN_FILE': str(self.authority.token_files['gateway']),
            'LAYERX_GATEWAY_IDENTITY_URL': self.identity.endpoint,
            'LAYERX_GATEWAY_IDENTITY_TOKEN_FILE': str(self.identity.service_tokens['layerx-gateway']),
            'LAYERX_GATEWAY_PROGRAM_REGISTRY_URL': f'https://localhost:{self.ports["registry"]}',
            'LAYERX_GATEWAY_PROGRAM_REGISTRY_TOKEN_FILE': str(self.registry_token),
        })
        for kind, url, token in (('PAYMENT', self.endpoint, self.producer_token_file),
                                 ('WEBHOOKS', self.ingress['url'], self.ingress['token_file'])):
            env.update({
                f'LAYERX_EVENTS_{kind}_UPSTREAM_URL': url,
                f'LAYERX_EVENTS_{kind}_UPSTREAM_CA_DER': str(self.ca.ca_der),
                f'LAYERX_EVENTS_{kind}_UPSTREAM_TOKEN_FILE': str(token),
                f'LAYERX_EVENTS_{kind}_UPSTREAM_CLIENT_IDENTITY_PKCS12': str(self.p12),
                f'LAYERX_EVENTS_{kind}_UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE': str(self.p12_password_file),
            })
            if kind == 'WEBHOOKS' and 'client_identity' in self.ingress:
                env[f'LAYERX_EVENTS_{kind}_UPSTREAM_CLIENT_IDENTITY_PKCS12'] = str(self.ingress['client_identity'])
                env[f'LAYERX_EVENTS_{kind}_UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE'] = str(self.ingress['client_password'])
        return env

    def _source_env(self):
        s = self.dirs['source']
        return {
            'LAYERX_EVENTS_LISTEN': f'127.0.0.1:{self.ports["source"]}',
            'LAYERX_EVENTS_KIND': 'payments',
            'LAYERX_EVENTS_TLS_CERT_DER': str(self.tls['source']['cert_der']),
            'LAYERX_EVENTS_TLS_KEY_DER': str(self.tls['source']['key_der']),
            'LAYERX_EVENTS_CLIENT_CA_DER': str(self.ca.ca_der),
            'LAYERX_EVENTS_CREDENTIALS_FILE': str(s / 'credentials.json'),
            'LAYERX_EVENTS_ENROLLMENT_KEY_FILE': str(self.enrollment_key),
            'LAYERX_EVENTS_STATE_DIR': str(private_directory(s / 'state')),
            'LAYERX_EVENTS_UPSTREAM_URL': self.gateway_endpoint,
            'LAYERX_EVENTS_UPSTREAM_CA_DER': str(self.ca.ca_der),
            'LAYERX_EVENTS_TOKEN_FILE': str(self.source_token_file),
            'LAYERX_EVENTS_PRODUCERS_FILE': str(s / 'producers.json'),
        }

    def _state(self):
        return json.loads(self.state_path.read_text()) if self.state_path.exists() else {}

    def _save(self, state):
        staging = self.state_path.with_suffix('.staging')
        private_file(staging, json.dumps(state, sort_keys=True))
        os.replace(staging, self.state_path)

    def _signer(self):
        seed = (self.runtime.directory / 'keys/treasury.seed').read_bytes()
        return Ed25519PrivateKey.from_private_bytes(seed).public_key().public_bytes(
            serialization.Encoding.Raw, serialization.PublicFormat.Raw).hex()

    def _provision(self):
        """identity principal (treasury signer allowed) -> session -> gateway API key -> source credential map."""
        if self.principal_token_file.exists():
            return
        status, body = self.identity._request('POST', '/v1/principals', 'provisioning', {
            'tenant': TENANT, 'sub': PRINCIPAL, 'allowed_signer_public_keys': [self._signer()]})
        if status != 200 or body.get('sub') != PRINCIPAL:
            raise RuntimeError(f'identity principal refused: {status} {body.get("error")}')
        status, body = self.identity._request('POST', '/v1/sessions', 'provisioning', {'tenant': TENANT, 'sub': PRINCIPAL})
        session = body.get('token', '')
        if status != 200 or not session.startswith('ses_'):
            raise RuntimeError(f'identity session refused: {status} {body.get("error")}')
        private_file(self.dirs['tokens'] / 'developer-session', session)
        request = json.dumps({'signer_public_key': self._signer(), 'scopes': ['receipt:read'],
                              'quota_requests': 1000, 'quota_window_seconds': 3600}).encode()
        status, kind, reply = https('POST', self.gateway_endpoint + '/v1/keys', self.ca, request,
                                    {'Authorization': f'Bearer {session}', 'Content-Type': 'application/json',
                                     'Idempotency-Key': hashlib.sha256(b'paxeer-x-fixture-key' + PRINCIPAL.encode()).hexdigest()})
        key = json.loads(reply).get('key', {}) if kind.startswith('application/json') else {}
        if status not in (200, 201) or key.get('authorization_scheme') != 'LayerX-Key' or not key.get('secret'):
            raise RuntimeError(f'gateway key issuance refused: {status}')
        private_file(self.principal_token_file, key['secret'])
        staging = self.dirs['source'] / 'credentials.staging'
        credential = self.principal_token_file.read_bytes().rstrip(b'\r\n')
        key = self.enrollment_key.read_bytes().rstrip(b'\r\n')
        message = f'layerx-enrollment-v1\npayments\n1\n{PRINCIPAL}\n{hashlib.sha256(credential).hexdigest()}\n'.encode()
        private_file(staging, json.dumps({'version': 1, 'generation': 1,
            'principals': [{'principal': PRINCIPAL, 'credential_file': str(self.principal_token_file)}],
            'mac': hmac.new(key, message, hashlib.sha256).hexdigest()}))
        os.replace(staging, self.dirs['source'] / 'credentials.json')

    def _source_ready(self):
        status, kind, body = https('GET', self.endpoint + '/readyz', self.ca, timeout=3)
        return status == 200 and kind.startswith('application/json') and json.loads(body).get('ready') is True

    def start(self):
        if self.owned_identity:
            self.identity.start()
        self.authority.start()
        self.redis.start()
        self.boundary.start()
        self.registry.start()
        self.gateway.start()
        self.source.start()
        self._provision()
        wait_until(self._source_ready, self.source.process, 'layerx-event-source principal binding', 30.0)
        self._prepare_runtime()
        return self

    def _prepare_runtime(self):
        state = self._state()
        if state.get('prepared'):
            return
        for operation, sequence in PREPARATION:
            rows = receipt_rows(self.runtime.invoke(operation, sequence, 'event-source-' + operation).stdout)
            if len(rows) != 1 or rows[0]['result'] != '0':
                raise RuntimeError(f'runtime preparation {operation} produced no successful receipt')
        state.update(prepared=True, next_send=FIRST_SEND, events=[])
        self._save(state)

    def stop(self, kill=False):
        self.source.stop(kill)
        self.gateway.stop(kill)
        self.registry.stop()
        self.boundary.stop(kill)
        self.redis.stop()
        self.authority.stop(kill)
        if self.owned_identity:
            self.identity.stop()

    def restart(self, kill=False):
        self.stop(kill)
        self.start()
        return self

    def read_event(self, identifier):
        """GET /internal/v1/events/{id} as the webhooks source client; returns (status, json)."""
        status, kind, body = https('GET', f'{self.endpoint}/internal/v1/events/{identifier}', self.ca,
                                   headers={'Authorization': f'Bearer {self.source_token_file.read_text().strip()}'},
                                   client=self.reader_client)
        return status, json.loads(body) if kind.startswith('application/json') and body else {}

    def produce_event(self, kind):
        if kind != 'payment':
            raise ValueError(f'MISSING {kind} upstream: journey/approval need layerx-human-service, '
                             'program needs the registry producer; this fixture serves payment only')
        state = self._state()
        sequence = state['next_send']
        rows = receipt_rows(self.runtime.invoke('send-one', sequence, f'event-source-send-{sequence}').stdout)
        if len(rows) != 1 or rows[0]['result'] != '0' or len(rows[0]['id']) != 64:
            raise RuntimeError('real signed send produced no successful receipt')
        activity, raw = rows[0]['id'], bytes.fromhex(rows[0]['raw'])
        state['next_send'] = sequence + 1
        self._save(state)
        wait_until(lambda: self.authority.authority(activity, 'gateway')[0] == 200, self.authority.process,
                   'receipt authority evidence for the signed send', 90.0)
        request_digest = hashlib.sha256(b'paxeer-x-fixture-settle' + bytes.fromhex(activity)).digest()
        document = json.dumps({
            'principal': PRINCIPAL,
            'payload': {'x402Version': 2, 'accepted': {}, 'payload': {
                'receipt': base64.b64encode(raw).decode(),
                'receiptDigest': hashlib.sha256(MERKLE_LEAF + raw).hexdigest(),
                'verificationLevel': 'sequencer-signed'}},
            'requirements': {},
            'idempotencyKey': hashlib.sha256(X402_IDEMPOTENCY + PRINCIPAL.encode() + request_digest).hexdigest(),
            'requestDigest': request_digest.hex()}).encode()
        headers = {'Authorization': f'LayerX-Key {self.principal_token_file.read_text().strip()}',
                   'Content-Type': 'application/json'}

        def settled():
            status, content, body = https('POST', self.gateway_endpoint + '/v1/settle', self.ca, document, headers)
            result = json.loads(body) if content.startswith('application/json') and body else {}
            if status == 200 and result.get('state') == 'refused':
                raise RuntimeError(f'gateway settlement refused: {result.get("reason")}')
            return status == 200 and result.get('state') == 'settled' and result.get('activity_id') == activity
        wait_until(settled, self.gateway.process, 'gateway settlement of the signed send', 90.0)
        identifier = event_id('payment', activity, 1)

        def journaled():
            status, record = self.read_event(identifier)
            return status == 200 and record.get('id') == identifier and record.get('activity_id') == activity \
                and record.get('principal') == PRINCIPAL
        wait_until(journaled, self.source.process, 'event source journal of the gateway payment observation', 90.0)
        state['events'].append({'id': identifier, 'activity_id': activity, 'sequence': sequence})
        self._save(state)
        return identifier

    def cleanup(self):
        self.stop()
        self.redis.cleanup()
        self.authority.cleanup()
        if self.owned_identity:
            self.identity.cleanup()
        shutil.rmtree(self.work, ignore_errors=True)

    def __enter__(self):
        return self.start()

    def __exit__(self, *exc):
        self.cleanup()


class RegistryFixture:
    """The native-interface fixture's real registry/cgroup contract on this runtime."""

    def __init__(self, source):
        import tempfile
        self.source = source
        self.process = None
        self.cgroup = None
        self.root = Path(tempfile.mkdtemp(prefix='webhook-registry-'))
        self.root.chmod(0o700)
        os.chown(self.root, 4030, 4030)
        self.program = None

    def _owned(self, name, data):
        path = self.root / name
        if isinstance(data, str):
            data = data.encode()
        with path.open('wb') as stream:
            stream.write(data)
        path.chmod(0o600)
        os.chown(path, 4030, 4030)
        return path

    def start(self):
        if self.process is not None and self.process.poll() is None:
            return
        source = self.source
        config_path = os.environ.get('PAXEER_X_REGISTRY_CONFIGURATION')
        parent_path = os.environ.get('PAXEER_X_REGISTRY_CGROUP_PARENT')
        if not config_path or not parent_path:
            raise RuntimeError('MISSING genuine registry configuration/delegated cgroup inputs')
        info = Path(config_path).lstat()
        if Path(config_path).is_symlink() or info.st_uid != os.geteuid() or info.st_mode & 0o077:
            raise RuntimeError('registry builder configuration must be protected')
        configured = json.loads(Path(config_path).read_bytes())
        builder = ('IMAGE_DIGEST', 'ENVIRONMENT_ROOT', 'ENTRYPOINT', 'ISOLATION_RUNTIME',
                   'ISOLATION_RUNTIME_DIGEST', 'JOB_SUPERVISOR', 'JOB_SUPERVISOR_DIGEST')
        environment = {f'LAYERX_REGISTRY_BUILDER_{key}': configured[f'LAYERX_REGISTRY_BUILDER_{key}']
                       for key in builder}
        for key in ('ISOLATION_RUNTIME', 'JOB_SUPERVISOR'):
            path = Path(environment[f'LAYERX_REGISTRY_BUILDER_{key}'])
            with path.open('rb') as stream:
                digest = hashlib.file_digest(stream, 'sha256').hexdigest()
            if digest != environment[f'LAYERX_REGISTRY_BUILDER_{key}_DIGEST'] or not os.access(path, os.X_OK):
                raise RuntimeError('registry builder executable differs from its pinned input')
        parent = Path(parent_path)
        if not parent.is_absolute() or parent.resolve(strict=True) != parent or parent.stat().st_uid != 0:
            raise RuntimeError('registry cgroup parent must be canonical and root-owned')
        if not {'cpu', 'memory', 'pids', 'io'} <= set((parent / 'cgroup.subtree_control').read_text().split()):
            raise RuntimeError('registry cgroup controllers are not delegated')
        self.cgroup = parent / ('webhook-ingress-' + secrets.token_hex(12))
        self.cgroup.mkdir()
        if hasattr(self, '_launch_environment'):
            self.program.start()
            self._spawn(self._launch_environment)
            return
        state = self.root / 'state'
        state.mkdir(mode=0o700, exist_ok=True)
        os.chown(state, 4030, 4030)
        mount = self.root / 'host-cgroup'
        mount.mkdir(mode=0o755, exist_ok=True)
        tls = der_pair(source.ca, self.root, 'registry-server')
        for path in (tls['cert_der'], tls['key_der']):
            os.chown(path, 4030, 4030)
        ca = self._owned('ca.der', source.ca.ca_der.read_bytes())
        request = self._owned('request.token', source.registry_token.read_bytes())
        publication = self._owned('publication.token', secrets.token_hex(32))
        identity = self._owned('client.p12', source.p12.read_bytes())
        password = self._owned('password', source.p12_password_file.read_bytes())
        node = node_environment(source.runtime)
        public, sequencer = source._sequencer()
        history = b'LayerX/sequencer-trust-history/v1\0' + bytes((0, 1, 0, 0))
        history += int(WIRE_VERSION).to_bytes(2, 'big') + int(source.runtime.manifest['network_id']).to_bytes(4, 'big')
        history += (0).to_bytes(8, 'big') + bytes.fromhex(sequencer) + bytes.fromhex(public)
        history += int(node['LAYERX_NODE_FIRST_BATCH']).to_bytes(8, 'big') + int(node['LAYERX_NODE_LAST_BATCH']).to_bytes(8, 'big') + bytes(9)
        trust = self._owned('trust-history', history)
        environment.update({
            'PATH': '/usr/bin:/bin',
            'LAYERX_REGISTRY_STATE': str(state),
            'LAYERX_REGISTRY_LISTEN': f'127.0.0.1:{source.ports["registry"]}',
            'LAYERX_REGISTRY_HOST_CGROUP_MOUNT': str(mount),
            'LAYERX_REGISTRY_REQUEST_TOKEN_FILE': str(request),
            'LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE': str(publication),
            'LAYERX_REGISTRY_TLS_CERT_DER': str(tls['cert_der']),
            'LAYERX_REGISTRY_TLS_KEY_DER': str(tls['key_der']),
            'LAYERX_REGISTRY_CLIENT_CA_DER': str(ca),
            'LAYERX_REGISTRY_NODE_ENDPOINT': source.boundary_endpoint,
            'LAYERX_REGISTRY_NODE_AUTHORIZATION': source.boundary_tokens['registry'].read_text().strip(),
            'LAYERX_REGISTRY_OUTBOUND_CA_DER': str(ca),
            'LAYERX_REGISTRY_CLIENT_IDENTITY_PKCS12': str(identity),
            'LAYERX_REGISTRY_CLIENT_IDENTITY_PASSWORD_FILE': str(password),
            'LAYERX_REGISTRY_RECEIPT_AUTHORITY_ENDPOINT': f'http://127.0.0.1:{source.runtime.ports[8]}',
            'LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION': (source.runtime.directory / 'node/secrets/replica-token').read_text().strip(),
            'LAYERX_REGISTRY_RECEIPT_AUTHORITY_REPLICA_ID': node['LAYERX_NODE_REPLICA_ID'],
            'LAYERX_REGISTRY_SEQUENCER_TRUST_HISTORY': str(trust),
        })
        # The actual empty program source is live while enrollment is pending;
        # it cannot fabricate a program observation or advertise readiness.
        program_dir = private_directory(source.work / 'program-source')
        program_port = free_port()
        program_key = private_file(program_dir / 'enrollment.key', secrets.token_hex(32))
        consumer = private_file(program_dir / 'consumer.token', secrets.token_hex(32))
        mac = hmac.new(program_key.read_bytes(), b'layerx-enrollment-v1\nprograms\n1\n', hashlib.sha256).hexdigest()
        private_file(program_dir / 'enrollment.json', json.dumps({'version': 1, 'generation': 1, 'principals': [], 'mac': mac}))
        program_tls = der_pair(source.ca, program_dir, 'program-source')
        program_environment = {
            'LAYERX_EVENTS_LISTEN': f'127.0.0.1:{program_port}', 'LAYERX_EVENTS_KIND': 'programs',
            'LAYERX_EVENTS_TLS_CERT_DER': str(program_tls['cert_der']),
            'LAYERX_EVENTS_TLS_KEY_DER': str(program_tls['key_der']), 'LAYERX_EVENTS_CLIENT_CA_DER': str(source.ca.ca_der),
            'LAYERX_EVENTS_ENROLLMENT_KEY_FILE': str(program_key),
            'LAYERX_EVENTS_CREDENTIALS_FILE': str(program_dir / 'enrollment.json'),
            'LAYERX_EVENTS_STATE_DIR': str(private_directory(program_dir / 'state')),
            'LAYERX_EVENTS_TOKEN_FILE': str(consumer),
            'LAYERX_EVENTS_UPSTREAM_URL': f'https://localhost:{source.ports["registry"]}',
            'LAYERX_EVENTS_UPSTREAM_CA_DER': str(source.ca.ca_der),
        }
        self.program = Service('layerx-event-source', program_dir, lambda: program_environment,
            lambda: https('GET', f'https://localhost:{program_port}/livez', source.ca)[0] == 200)
        self.program.start()
        for kind, endpoint, token in (('PROGRAM', f'https://localhost:{program_port}', consumer),
                                     ('WEBHOOKS', source.ingress['url'], Path(source.ingress['token_file']))):
            environment.update({
                f'LAYERX_EVENTS_{kind}_UPSTREAM_URL': endpoint,
                f'LAYERX_EVENTS_{kind}_UPSTREAM_CA_DER': str(ca),
                f'LAYERX_EVENTS_{kind}_UPSTREAM_TOKEN_FILE': str(self._owned(kind.lower() + '.token', token.read_bytes())),
                f'LAYERX_EVENTS_{kind}_UPSTREAM_CLIENT_IDENTITY_PKCS12': str(identity),
                f'LAYERX_EVENTS_{kind}_UPSTREAM_CLIENT_IDENTITY_PASSWORD_FILE': str(password),
            })
        self._owned('runtime-environment.json', json.dumps(environment, sort_keys=True))
        self._launch_environment = environment
        self._spawn(environment)

    def _spawn(self, environment):
        source = self.source
        mount = self.root / 'host-cgroup'
        script = '''set -eu
printf '%s' "$$" > "$1/cgroup.procs"
shift
exec /usr/bin/unshare --mount --cgroup /bin/sh -c '
set -eu
mount --make-rprivate /
mount --bind /sys/fs/cgroup "$1"
mount -t cgroup2 none /sys/fs/cgroup
shift
exec "$@"
' registry "$@"
'''
        log = open(self.root / 'registry.log', 'ab')
        try:
            self.process = subprocess.Popen(['/bin/sh', '-c', script, 'registry', str(self.cgroup), str(mount),
                str(binary('layerx-program-registry'))], env=environment, stdin=subprocess.DEVNULL,
                stdout=log, stderr=log, start_new_session=True)
        finally:
            log.close()
        from hosted_delivery_fixture import wait_port
        wait_port(source.ports['registry'], self.process, 60)

    def ready(self):
        source = self.source
        status, content, body = https('GET', f'https://localhost:{source.ports["registry"]}/healthz', source.ca,
            headers={'Authorization': 'Bearer ' + source.registry_token.read_text().strip()}, client=source.reader_client)
        value = json.loads(body) if content.startswith('application/json') else {}
        return status == 200 and value.get('status') == 'ready' and value.get('service') == 'program-registry'

    def stop(self):
        if self.process is not None and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
        self.process = None
        if self.cgroup is not None:
            (self.cgroup / 'cgroup.kill').write_text('1')
            for name in ('workers', 'main'):
                path = self.cgroup / name
                if path.exists():
                    path.rmdir()
            self.cgroup.rmdir()
            self.cgroup = None
        if self.program is not None:
            self.program.stop()
