"""Disposable hosted delivery fixture for the event delivery contract.

Owns the internal CA material every fixture issues from, a TLS Redis with the
layerx-webhooks ACL user, and binary resolution from PAXEER_X_HOSTED_BIN_DIR.
Every process is a real compiled service; a missing binary or tool refuses.
Private material stays below the caller's 0700 work directory.
"""

import os
import shutil
import socket
import subprocess
import time
from pathlib import Path


class Missing(Exception):
    """A required binary, tool or fixture input is absent."""


def binary(name):
    directory = os.environ.get('PAXEER_X_HOSTED_BIN_DIR')
    if not directory:
        raise Missing('MISSING PAXEER_X_HOSTED_BIN_DIR')
    path = Path(directory) / name
    if not path.is_file() or not os.access(path, os.X_OK):
        raise Missing(f'MISSING {name}')
    return path


def tool(name):
    path = shutil.which(name)
    if path is None:
        raise Missing(f'MISSING {name}')
    return path


def private_directory(path):
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(path, 0o700)
    return path


def private_file(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, 'w') as handle:
        handle.write(value)
    return path


def free_port():
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', 0))
        return probe.getsockname()[1]


def wait_port(port, process, deadline=20.0):
    end = time.monotonic() + deadline
    while time.monotonic() < end:
        if process.poll() is not None:
            raise RuntimeError(f'process exited with {process.returncode} before listening on {port}')
        try:
            with socket.create_connection(('127.0.0.1', port), timeout=0.5):
                return
        except OSError:
            time.sleep(0.1)
    raise RuntimeError(f'nothing listened on {port} within {deadline}s')


def openssl(*arguments):
    subprocess.run([tool('openssl'), *arguments], check=True, capture_output=True, timeout=60)


class CaMaterial:
    """An isolated internal CA issuing EC P-256 leaves with exact URI SANs."""

    def __init__(self, work):
        self.directory = private_directory(Path(work) / 'ca')
        self.ca_pem = self.directory / 'ca.pem'
        self.ca_der = self.directory / 'ca.der'
        self._key = self.directory / 'ca.key'
        self._serial = 0
        openssl('genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(self._key))
        openssl('req', '-x509', '-new', '-key', str(self._key), '-days', '2', '-subj',
                '/O=Paxeer X Network/CN=fixture internal CA', '-addext', 'basicConstraints=critical,CA:TRUE',
                '-addext', 'keyUsage=critical,keyCertSign,cRLSign', '-out', str(self.ca_pem))
        openssl('x509', '-in', str(self.ca_pem), '-outform', 'DER', '-out', str(self.ca_der))

    def issue(self, common_name, san_uris, eku_client, san_dns=()):
        self._serial += 1
        leaf = private_directory(self.directory / f'leaf-{self._serial}')
        key, csr, cert, extensions = (leaf / 'key.pem', leaf / 'leaf.csr', leaf / 'cert.pem', leaf / 'leaf.ext')
        names = [f'URI:{uri}' for uri in san_uris] + [f'DNS:{name}' for name in san_dns]
        lines = ['basicConstraints=critical,CA:FALSE', 'keyUsage=critical,digitalSignature',
                 f'extendedKeyUsage={"clientAuth" if eku_client else "serverAuth"}']
        if names:
            lines.append('subjectAltName=' + ','.join(names))
        private_file(extensions, '\n'.join(lines) + '\n')
        openssl('genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(key))
        openssl('req', '-new', '-key', str(key), '-subj', f'/O=Paxeer X Network/CN={common_name}', '-out', str(csr))
        openssl('x509', '-req', '-in', str(csr), '-CA', str(self.ca_pem), '-CAkey', str(self._key),
                '-set_serial', str(1000 + self._serial), '-days', '1', '-extfile', str(extensions),
                '-out', str(cert))
        for path in (key, cert):
            os.chmod(path, 0o600)
        return cert, key


class TlsRedis:
    """redis-server over TLS only, with the layerx-webhooks ACL user and AOF persistence."""

    def __init__(self, work, ca):
        self.work = private_directory(Path(work) / 'redis')
        self.ca = ca
        self.port = free_port()
        self.username = 'layerx-webhooks'
        self.password = os.urandom(24).hex()
        self.cert, self.key = ca.issue('internal-redis', [], False, ['localhost'])
        self.materials = {
            'username': private_file(self.work / 'username', self.username),
            'password': private_file(self.work / 'password', self.password),
        }
        self.endpoint = f'rediss://localhost:{self.port}'
        self.process = None

    def start(self):
        config = '\n'.join([
            'port 0', f'tls-port {self.port}', 'bind 127.0.0.1', f'dir {self.work}',
            f'tls-cert-file {self.cert}', f'tls-key-file {self.key}', f'tls-ca-cert-file {self.ca.ca_pem}',
            'tls-auth-clients no', 'appendonly yes', 'appendfsync always', 'save ""',
            'user default off',
            f'user {self.username} on >{self.password} ~* &* +@all',
        ]) + '\n'
        private_file(self.work / 'redis.conf', config)
        log = open(self.work / 'redis.log', 'ab')
        self.process = subprocess.Popen([tool('redis-server'), str(self.work / 'redis.conf')],
                                        stdout=log, stderr=log, cwd=self.work)
        wait_port(self.port, self.process)

    def stop(self):
        if self.process is not None and self.process.poll() is None:
            self.process.terminate()
            self.process.wait(timeout=20)
        self.process = None

    def restart(self):
        self.stop()
        self.start()

    def cleanup(self):
        self.stop()
        shutil.rmtree(self.work, ignore_errors=True)

    def __enter__(self):
        self.start()
        return self

    def __exit__(self, *exc):
        self.cleanup()
