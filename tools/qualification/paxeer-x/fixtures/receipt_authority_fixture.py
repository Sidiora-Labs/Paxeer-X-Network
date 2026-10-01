"""Disposable receipt authority bound to a RuntimeFixture replica.

Runs the real layerx-receipt-authority from PAXEER_X_HOSTED_BIN_DIR against
the owned chain-125 runtime: receipts come from the runtime LNI socket and
authority evidence only from the runtime's independent replica on loopback.
TLS comes from the caller's CaMaterial; bearer tokens are generated per
consuming service. Private material stays below work/receipt-authority.
"""

import hashlib
import json
import os
import secrets
import shutil
import ssl
import subprocess
import time
import urllib.error
import urllib.request
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import serialization

from hosted_delivery_fixture import binary, free_port, private_directory, private_file

WIRE_VERSION = '3'
NETWORK_NAME = 'paxeer-x-fixture'
CONSUMERS = ('gateway', 'registry', 'webhooks', 'agent', 'human-agent')


def der_pair(ca, directory, common_name, san_uris=(), eku_client=False, san_dns=('localhost',)):
    """Issues a leaf from ca and writes its DER certificate and PKCS#8 DER key (0600)."""
    cert_pem, key_pem = ca.issue(common_name, list(san_uris), eku_client, list(san_dns))
    cert = x509.load_pem_x509_certificate(Path(cert_pem).read_bytes())
    key = serialization.load_pem_private_key(Path(key_pem).read_bytes(), password=None)
    cert_der = directory / f'{common_name}.der'
    key_der = directory / f'{common_name}-key.der'
    for path, data in ((cert_der, cert.public_bytes(serialization.Encoding.DER)),
                       (key_der, key.private_bytes(serialization.Encoding.DER, serialization.PrivateFormat.PKCS8,
                                                   serialization.NoEncryption()))):
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, 'wb') as handle:
            handle.write(data)
    return {'cert_pem': Path(cert_pem), 'key_pem': Path(key_pem), 'cert_der': cert_der, 'key_der': key_der}


def https(method, url, ca, body=None, headers=None, client=None, timeout=10):
    """One HTTPS exchange trusting only the fixture CA; returns (status, content_type, bytes)."""
    context = ssl.create_default_context(cafile=str(ca.ca_pem))
    if client is not None:
        context.load_cert_chain(str(client['cert_pem']), str(client['key_pem']))
    request = urllib.request.Request(url, data=body, method=method, headers=headers or {})
    try:
        with urllib.request.urlopen(request, timeout=timeout, context=context) as response:
            return response.status, response.headers.get('Content-Type', ''), response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.headers.get('Content-Type', ''), error.read()


def wait_until(probe, process, what, deadline=60.0):
    """Polls probe until it returns a truthy value; refuses when the owned process exits."""
    end = time.monotonic() + deadline
    while time.monotonic() < end:
        if process.poll() is not None:
            raise RuntimeError(f'{what} exited with {process.returncode}; see its private log')
        try:
            value = probe()
            if value:
                return value
        except OSError:
            pass
        time.sleep(0.2)
    raise RuntimeError(f'{what} not ready within {deadline}s')


def node_environment(runtime):
    return dict(line.split('=', 1) for line in (runtime.directory / 'node/node.env').read_text().splitlines()
                if '=' in line)


class ReceiptAuthorityFixture:
    """layerx-receipt-authority dialing only the RuntimeFixture replica and LNI socket."""

    def __init__(self, work, ca, runtime, human=None, max_bytes=1048576):
        self.binary = binary('layerx-receipt-authority')
        self.ca, self.runtime = ca, runtime
        self.work = Path(work) / 'receipt-authority'
        if self.work.exists():
            raise RuntimeError('receipt authority fixture directory already exists')
        private_directory(self.work)
        tokens = private_directory(self.work / 'tokens')
        self.token_files = {name: private_file(tokens / f'{name}.token', secrets.token_hex(32))
                            for name in CONSUMERS}
        self.tls = der_pair(ca, self.work, 'layerx-receipt-authority')
        self.port = free_port()
        self.endpoint = f'https://localhost:{self.port}'
        self.log_path = self.work / 'receipt-authority.log'
        self.materials = {'tls_cert_der': self.tls['cert_der'], 'tls_key_der': self.tls['key_der'],
                          'client_ca_der': ca.ca_der, **{f'token_{k}': v for k, v in self.token_files.items()},
                          'log': self.log_path}
        self.ca_der_path = Path(ca.ca_der)
        self.max_bytes = max_bytes
        self.human = None
        if human is not None:
            # human = {tenant, principal, policy (dict), module_registry_file, horizon,
            #          binding: {socket, uid, gid}, peer_uid (agentd uid, default os.getuid())}
            state = private_directory(self.work / 'human-state')
            policy = private_file(self.work / 'principal-policy.json', json.dumps(human['policy']))
            token = private_file(tokens / 'human-agent-direct.token', secrets.token_hex(32))
            self.human = {**human, 'state_root': state, 'policy_file': policy, 'token_file': token}
            self.materials.update(human_policy=policy, human_state_root=state, token_human_agent_direct=token)
            self.peers = f"uid={human.get('peer_uid', os.getuid())};tenant={human['tenant']};principal={human['principal']}"
        self.process = None

    def human_environment(self):
        if self.human is None:
            return {}
        human = self.human
        return {
            'LAYERX_AUTHORITY_HUMAN_AGENT_TOKEN_FILE': str(human['token_file']),
            'LAYERX_AUTHORITY_PRINCIPAL_POLICY_FILE': str(human['policy_file']),
            'LAYERX_AUTHORITY_CORE_CLOCK_HORIZON': str(human['horizon']),
            'LAYERX_AUTHORITY_STATE_ROOT': str(human['state_root']),
            'LAYERX_AUTHORITY_HUMAN_AGENT_TENANT': human['tenant'],
            'LAYERX_AUTHORITY_HUMAN_AGENT_PRINCIPAL': human['principal'],
            'LAYERX_AUTHORITY_MODULE_REGISTRY_FILE': str(human['module_registry_file']),
            'LAYERX_AUTHORITY_IDENTITY_BINDING_SOCKET': str(human['binding']['socket']),
            'LAYERX_AUTHORITY_IDENTITY_BINDING_UID': str(human['binding']['uid']),
            'LAYERX_AUTHORITY_IDENTITY_BINDING_GID': str(human['binding']['gid']),
        }

    def consumer_environment(self):
        # Exact layerx-agentd (9be40a190) inputs: https endpoint, bearer VALUE (not a file),
        # CA DER file trusted alone with hostname verification (SAN localhost).
        env = {
            'LAYERX_AGENT_AUTHORITY_ENDPOINT': self.endpoint,
            'LAYERX_AGENT_AUTHORITY_BEARER_TOKEN': self.token_files['agent'].read_text().strip(),
            'LAYERX_AGENT_AUTHORITY_CA_DER': str(self.ca_der_path),
            'LAYERX_AGENT_AUTHORITY_REPLICA_ID': node_environment(self.runtime)['LAYERX_NODE_REPLICA_ID'],
        }
        if self.human is not None:
            env.update({
                'LAYERX_AGENT_HUMAN_AUTHORITY_ENDPOINT': self.endpoint,
                'LAYERX_AGENT_HUMAN_AUTHORITY_BEARER': self.human['token_file'].read_text().strip(),
                'LAYERX_AGENT_HUMAN_AUTHORITY_MAX_BYTES': str(self.max_bytes),
                'LAYERX_AGENT_HUMAN_AUTHORITY_CA_DER': str(self.ca_der_path),
                'LAYERX_AGENT_HUMAN_PEERS': self.peers,
            })
        return env

    def environment(self):
        runtime, node = self.runtime, node_environment(self.runtime)
        public = runtime.manifest['sequencer_public_key']
        return {
            'PATH': '/usr/bin:/bin',
            'LAYERX_AUTHORITY_LISTEN': f'127.0.0.1:{self.port}',
            'LAYERX_AUTHORITY_TLS_CERT_DER': str(self.tls['cert_der']),
            'LAYERX_AUTHORITY_TLS_KEY_DER': str(self.tls['key_der']),
            'LAYERX_AUTHORITY_CLIENT_CA_DER': str(self.ca.ca_der),
            'LAYERX_AUTHORITY_TOKEN_FILES': ':'.join(str(path) for path in self.token_files.values()),
            'LAYERX_AUTHORITY_REPLICA_URL': f'http://127.0.0.1:{runtime.ports[8]}',
            'LAYERX_AUTHORITY_REPLICA_BEARER_TOKEN_FILE': str(runtime.directory / 'node/secrets/replica-token'),
            'LAYERX_AUTHORITY_REPLICA_ID': node['LAYERX_NODE_REPLICA_ID'],
            'LAYERX_AUTHORITY_LNI_SOCKET': runtime.manifest['node_socket'],
            'LAYERX_AUTHORITY_PROTOCOL_NETWORK_ID': str(runtime.manifest['network_id']),
            'LAYERX_AUTHORITY_NETWORK_ID': NETWORK_NAME,
            'LAYERX_AUTHORITY_WIRE_VERSION': WIRE_VERSION,
            'LAYERX_AUTHORITY_SEQUENCER_ID': hashlib.sha256(('layerx-sequencer:' + public).encode()).hexdigest(),
            'LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY': public,
            'LAYERX_AUTHORITY_FIRST_BATCH': node['LAYERX_NODE_FIRST_BATCH'],
            'LAYERX_AUTHORITY_LAST_BATCH': node['LAYERX_NODE_LAST_BATCH'],
            **self.human_environment(),
        }

    def ready(self):
        status, kind, body = https('GET', self.endpoint + '/readyz', self.ca, timeout=3)
        return status == 200 and kind.startswith('application/json') and json.loads(body).get('ready') is True

    def start(self):
        if self.process is not None and self.process.poll() is None:
            return self
        log = open(self.log_path, 'ab')
        try:
            self.process = subprocess.Popen([str(self.binary)], env=self.environment(), cwd=self.work,
                                            stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
        finally:
            log.close()
        wait_until(self.ready, self.process, 'layerx-receipt-authority', 90.0)
        return self

    def stop(self, kill=False):
        if self.process is not None and self.process.poll() is None:
            self.process.kill() if kill else self.process.terminate()
            self.process.wait(timeout=20)
        self.process = None

    def restart(self, kill=False):
        previous = self.process.pid if self.process else None
        self.stop(kill)
        self.start()
        if self.process.pid == previous:
            raise RuntimeError('receipt authority restart reused its process')
        return self

    def authority(self, activity_id, consumer='webhooks'):
        """GET /internal/v1/activities/{id}/authority with one consumer bearer; returns (status, json)."""
        token = self.token_files[consumer].read_text().strip()
        status, kind, body = https('GET', f'{self.endpoint}/internal/v1/activities/{activity_id}/authority', self.ca,
                                   headers={'Authorization': f'Bearer {token}'})
        return status, json.loads(body) if kind.startswith('application/json') and body else {}

    def cleanup(self):
        self.stop()
        shutil.rmtree(self.work, ignore_errors=True)

    def __enter__(self):
        return self.start()

    def __exit__(self, *exc):
        self.cleanup()
