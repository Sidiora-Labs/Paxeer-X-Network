#!/usr/bin/env python3
"""Disposable layerx-identity service fixture over its real TLS listener and durable store."""

import json
import os
import secrets
import shutil
import signal
import socket
import ssl
import subprocess
import time
from pathlib import Path

SERVICES = (
    'gateway', 'registry', 'webhooks', 'dashboard', 'faucet',
    'testnet', 'ramp', 'provisioning', 'registrar',
)
READY_SECONDS = 30
LISTEN_PREFIX = 'layerx-identity listening on '


def _binary():
    directory = os.environ.get('PAXEER_X_HOSTED_BIN_DIR', '')
    path = Path(directory) / 'layerx-identity'
    if not directory or not path.is_file() or not os.access(path, os.X_OK):
        raise RuntimeError('MISSING layerx-identity')
    return path


def _private_write(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'wb') as handle:
        handle.write(data)
    return path


def _openssl(*args):
    result = subprocess.run(['openssl', *map(str, args)], capture_output=True, check=False)
    if result.returncode != 0:
        raise RuntimeError(f'openssl {args[0]} failed with exit {result.returncode}')


class IdentityFixture:
    def __init__(self, work, ca):
        self.binary = _binary()
        self.ca = ca
        self.root = Path(work) / 'identity'
        self.root.mkdir(mode=0o700)
        self.state_dir = self.root / 'state'
        self.state_dir.mkdir(mode=0o700)
        self.log_path = Path(work) / 'identity.log'
        self.process = None
        self.materials = {}
        self.service_tokens = {}
        self._tokens = {}
        self._materials()
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', 0))
            self.port = probe.getsockname()[1]
        self.endpoint = f'https://localhost:{self.port}'
        self._context = ssl.create_default_context(cafile=str(ca.ca_pem))

    def _materials(self):
        cert_pem, key_pem = self.ca.issue('layerx-identity', [], False, san_dns=['localhost'])
        cert_der = self.root / 'server.crt.der'
        key_der = self.root / 'server.key.der'
        for path in (cert_der, key_der):
            _private_write(path, b'')
        _openssl('x509', '-in', cert_pem, '-outform', 'DER', '-out', cert_der)
        _openssl('pkcs8', '-topk8', '-nocrypt', '-in', key_pem, '-outform', 'DER', '-out', key_der)
        tokens = self.root / 'tokens'
        tokens.mkdir(mode=0o700)
        for service in SERVICES:
            value = secrets.token_hex(32)
            self._tokens[service] = value
            self.service_tokens[f'layerx-{service}'] = _private_write(
                tokens / service, f'{value}\n'.encode())
        self.materials = {
            'tls_cert_pem': Path(cert_pem),
            'tls_key_pem': Path(key_pem),
            'tls_cert_der': cert_der,
            'tls_key_der': key_der,
            'ca_pem': Path(self.ca.ca_pem),
            'ca_der': Path(self.ca.ca_der),
            'service_tokens_dir': tokens,
            'store_key': _private_write(self.root / 'store.key',
                                        f'{secrets.token_hex(32)}\n'.encode()),
            'state_dir': self.state_dir,
            'log': self.log_path,
        }

    def _environment(self):
        return {
            'PATH': '/usr/bin:/bin',
            'LAYERX_IDENTITY_LISTEN': f'127.0.0.1:{self.port}',
            'LAYERX_IDENTITY_TLS_CERT_DER': str(self.materials['tls_cert_der']),
            'LAYERX_IDENTITY_TLS_KEY_DER': str(self.materials['tls_key_der']),
            'LAYERX_IDENTITY_STATE_DIR': str(self.state_dir),
            'LAYERX_IDENTITY_SERVICE_TOKENS_DIR': str(self.materials['service_tokens_dir']),
            'LAYERX_IDENTITY_STORE_KEY_FILE': str(self.materials['store_key']),
            'LAYERX_IDENTITY_SESSION_TTL_SECONDS': '3600',
        }

    def start(self):
        if self.process is not None:
            raise RuntimeError('layerx-identity already running')
        offset = self.log_path.stat().st_size if self.log_path.exists() else 0
        log = os.open(self.log_path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
        try:
            self.process = subprocess.Popen(
                [str(self.binary)], env=self._environment(), stdin=subprocess.DEVNULL,
                stdout=log, stderr=log, start_new_session=True)
        finally:
            os.close(log)
        deadline = time.monotonic() + READY_SECONDS
        expected = f'{LISTEN_PREFIX}127.0.0.1:{self.port} with TLS'
        while True:
            code = self.process.poll()
            if code is not None:
                self.process = None
                raise RuntimeError(f'layerx-identity exited {code} before listening; log {self.log_path}')
            with open(self.log_path, 'rb') as handle:
                handle.seek(offset)
                lines = handle.read().decode(errors='replace').splitlines()
            if expected in lines:
                break
            if time.monotonic() > deadline:
                self.stop()
                raise RuntimeError(f'layerx-identity did not listen; log {self.log_path}')
            time.sleep(0.05)
        while True:
            try:
                status, body = self._request('GET', '/readyz')
            except OSError:
                status, body = 0, {}
            if status == 200 and body == {'status': 'ready', 'service': 'identity'}:
                return self
            if time.monotonic() > deadline or self.process.poll() is not None:
                self.stop()
                raise RuntimeError(f'layerx-identity not ready (status {status}); log {self.log_path}')
            time.sleep(0.1)

    def stop(self):
        process, self.process = self.process, None
        if process is None:
            return
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)

    def restart(self):
        previous = self.process.pid if self.process else None
        self.stop()
        self.start()
        if self.process.pid == previous:
            raise RuntimeError('layerx-identity restart reused the previous PID')
        return self

    def cleanup(self):
        self.stop()
        shutil.rmtree(self.root, ignore_errors=True)
        self.log_path.unlink(missing_ok=True)

    def __enter__(self):
        return self.start()

    def __exit__(self, *_):
        self.cleanup()

    @property
    def pid(self):
        return self.process.pid if self.process else None

    def _request(self, method, path, service=None, body=None):
        payload = b'' if body is None else json.dumps(body, separators=(',', ':')).encode()
        head = [f'{method} {path} HTTP/1.1', 'Host: localhost']
        if service is not None:
            head.append(f'Authorization: Bearer {self._tokens[service]}')
        if payload:
            head.append('Content-Type: application/json')
        head += [f'Content-Length: {len(payload)}', 'Connection: close', '', '']
        with socket.create_connection(('127.0.0.1', self.port), timeout=10) as raw:
            with self._context.wrap_socket(raw, server_hostname='localhost') as stream:
                stream.sendall('\r\n'.join(head).encode() + payload)
                chunks = []
                while chunk := stream.recv(65536):
                    chunks.append(chunk)
        reply = b''.join(chunks)
        header, separator, content = reply.partition(b'\r\n\r\n')
        if not separator:
            raise RuntimeError(f'identity {method} {path}: malformed reply')
        status = int(header.split(b'\r\n', 1)[0].split()[1])
        return status, json.loads(content) if content else {}

    def provision_principal(self, subject, tenant):
        status, body = self._request('POST', '/v1/principals', 'provisioning', {
            'tenant': tenant, 'sub': subject, 'allowed_signer_public_keys': []})
        if status != 200 or body.get('sub') != subject or body.get('tenant') != tenant:
            raise RuntimeError(f'identity principal refused: {status} {body.get("error")}')
        status, body = self._request('POST', '/v1/sessions', 'provisioning',
                                     {'tenant': tenant, 'sub': subject})
        token = body.get('token', '')
        if status != 200 or body.get('sub') != subject or not token.startswith('ses_'):
            raise RuntimeError(f'identity session refused: {status} {body.get("error")}')
        return token

    def introspect(self, token, service='webhooks'):
        status, body = self._request('POST', '/v1/sessions/introspect', service, {'token': token})
        if status != 200 or not isinstance(body.get('active'), bool):
            raise RuntimeError(f'identity introspection refused: {status} {body.get("error")}')
        return body
