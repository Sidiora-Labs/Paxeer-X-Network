import base64
import http.client
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import ssl
import subprocess
import time

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
from cryptography.x509 import load_pem_x509_certificate

BINARY = 'layerx-kms'
PURPOSE = 'layerx-webhook-v1'
READY_SECONDS = 30
TOKEN_CHARS = set('abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.:')


def _binary():
    directory = os.environ.get('PAXEER_X_HOSTED_BIN_DIR')
    path = Path(directory) / BINARY if directory else None
    if path is None or not path.is_file() or not os.access(path, os.X_OK):
        raise RuntimeError('MISSING ' + BINARY)
    return path


def _private(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'wb') as handle:
        handle.write(data)
    return path


def _valid_token(value, maximum):
    return 0 < len(value) <= maximum and set(value) <= TOKEN_CHARS


class KmsFixture:
    def __init__(self, work, ca):
        self.work = Path(work) / 'kms'
        self.ca = ca
        self.process = None
        self.port = None
        self.endpoint = None
        self.materials = {}
        self.token_file = self.work / 'token'
        self.accounts = {}
        self._log = None

    def _prepare(self):
        if self.materials:
            return
        self.work.mkdir(mode=0o700, parents=False, exist_ok=False)
        (self.work / 'state').mkdir(mode=0o700)
        _private(self.token_file, secrets.token_hex(32).encode())
        _private(self.work / 'seal', secrets.token_hex(32).encode())
        server_pem, server_key_pem = self.ca.issue('localhost', [], False, san_dns=['localhost'])
        certificate = load_pem_x509_certificate(Path(server_pem).read_bytes())
        _private(self.work / 'server.der', certificate.public_bytes(serialization.Encoding.DER))
        key = serialization.load_pem_private_key(Path(server_key_pem).read_bytes(), password=None)
        _private(self.work / 'server-key.der', key.private_bytes(
            serialization.Encoding.DER, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
        client_pem, client_key_pem = self.ca.issue('layerx-kms-fixture-client', [], True)
        self.materials = {
            'token': self.token_file,
            'seal': self.work / 'seal',
            'state': self.work / 'state',
            'server_cert_der': self.work / 'server.der',
            'server_key_der': self.work / 'server-key.der',
            'client_ca_der': Path(self.ca.ca_der),
            'client_cert_pem': Path(client_pem),
            'client_key_pem': Path(client_key_pem),
            'log': self.work / 'kms.log',
        }

    def _context(self, client_identity=True):
        context = ssl.create_default_context(ssl.Purpose.SERVER_AUTH, cafile=str(self.ca.ca_pem))
        if client_identity:
            context.load_cert_chain(str(self.materials['client_cert_pem']), str(self.materials['client_key_pem']))
        return context

    def _request(self, method, path, body=None, token=None, idempotency=None, client_identity=True):
        connection = http.client.HTTPSConnection('localhost', self.port, timeout=10, context=self._context(client_identity))
        headers = {'Connection': 'close'}
        if token is not None:
            headers['Authorization'] = 'Bearer ' + token
        if idempotency is not None:
            headers['Idempotency-Key'] = idempotency
        payload = None
        if body is not None:
            payload = json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        try:
            connection.request(method, path, body=payload, headers=headers)
            response = connection.getresponse()
            return response.status, json.loads(response.read() or b'null')
        finally:
            connection.close()

    def _token(self):
        return self.token_file.read_text().strip()

    def start(self):
        if self.process is not None:
            raise RuntimeError('KMS fixture already running')
        binary = _binary()
        self._prepare()
        if self.port is None:
            with socket.socket() as probe:
                probe.bind(('127.0.0.1', 0))
                self.port = probe.getsockname()[1]
            self.endpoint = 'https://localhost:%d' % self.port
        environment = {
            'PATH': os.environ.get('PATH', '/usr/bin:/bin'),
            'LAYERX_KMS_LISTEN': '127.0.0.1:%d' % self.port,
            'LAYERX_KMS_STATE_DIR': str(self.materials['state']),
            'LAYERX_KMS_TOKEN_FILE': str(self.token_file),
            'LAYERX_KMS_SEAL_SECRET_FILE': str(self.materials['seal']),
            'LAYERX_KMS_TLS_CERT_DER': str(self.materials['server_cert_der']),
            'LAYERX_KMS_TLS_KEY_DER': str(self.materials['server_key_der']),
            'LAYERX_KMS_CLIENT_CA_DER': str(self.materials['client_ca_der']),
        }
        self._log = open(self.materials['log'], 'ab')
        os.chmod(self.materials['log'], 0o600)
        self.process = subprocess.Popen([str(binary)], env=environment, stdin=subprocess.DEVNULL,
                                        stdout=self._log, stderr=subprocess.STDOUT)
        deadline = time.monotonic() + READY_SECONDS
        while True:
            if self.process.poll() is not None:
                code = self.process.returncode
                self._close()
                raise RuntimeError('layerx-kms exited %d before readiness; log %s' % (code, self.materials['log']))
            try:
                if self._request('GET', '/readyz', client_identity=False) == (200, {'ready': True, 'ed25519_non_exportable': True}):
                    return self
            except (OSError, ssl.SSLError, http.client.HTTPException, ValueError):
                pass
            if time.monotonic() > deadline:
                self.stop()
                raise RuntimeError('layerx-kms not ready within %ds; log %s' % (READY_SECONDS, self.materials['log']))
            time.sleep(0.1)

    def _close(self):
        self.process = None
        if self._log is not None:
            self._log.close()
            self._log = None

    def stop(self, kill=False):
        if self.process is None:
            return
        if kill:
            self.process.kill()
        else:
            self.process.terminate()
        try:
            self.process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait()
        self._close()

    def restart(self, kill=False):
        self.stop(kill=kill)
        return self.start()

    def cleanup(self):
        self.stop()
        if self.work.exists():
            shutil.rmtree(self.work)
        self.materials = {}
        self.accounts = {}

    def __enter__(self):
        return self.start()

    def __exit__(self, *exc):
        self.cleanup()

    def bind_account(self, account_id, purpose=PURPOSE, expect_status=(200, 201)):
        if not _valid_token(account_id, 128) or not _valid_token(purpose, 64):
            raise ValueError('account identifier or purpose is not a KMS token')
        status, key = self._request('POST', '/v1/signing-keys', {'algorithm': 'ed25519', 'purpose': purpose},
                                    token=self._token(), idempotency=account_id)
        if status not in expect_status:
            raise RuntimeError('KMS bind refused %s: %s' % (status, key))
        if set(key) != {'key_id', 'handle', 'public_key'} or not key['key_id'].startswith('whk_'):
            raise RuntimeError('KMS bind returned an unexpected key shape')
        if len(base64.b64decode(key['public_key'], validate=True)) != 32:
            raise RuntimeError('KMS bind returned a malformed public key')
        previous = self.accounts.get(account_id)
        if previous is not None and previous['key_id'] != key['key_id']:
            raise RuntimeError('KMS rebound account to a different key')
        self.accounts[account_id] = dict(key, status=status)
        return dict(key, status=status)

    def sign(self, account_id, payload):
        key = self.accounts.get(account_id)
        if key is None:
            raise RuntimeError('account %s is not bound' % account_id)
        status, body = self._request('POST', '/v1/signatures', {
            'algorithm': 'ed25519', 'key_handle': key['handle'],
            'message': base64.b64encode(payload).decode()}, token=self._token())
        if status != 200 or not isinstance(body, dict) or set(body) != {'signature'}:
            raise RuntimeError('KMS signing refused %s: %s' % (status, body))
        signature = base64.b64decode(body['signature'], validate=True)
        try:
            Ed25519PublicKey.from_public_bytes(base64.b64decode(key['public_key'])).verify(signature, payload)
        except InvalidSignature:
            raise RuntimeError('KMS signature does not verify under the bound public key')
        return signature

    def raw_request(self, method, path, body=None, token=None, idempotency=None, client_identity=True):
        return self._request(method, path, body, token, idempotency, client_identity)
