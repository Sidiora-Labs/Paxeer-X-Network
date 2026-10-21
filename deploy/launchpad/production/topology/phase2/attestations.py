from __future__ import annotations
import base64
import hashlib
import os
from pathlib import Path
import subprocess
import stat
import tempfile
from . import v1


def regular_bytes(path, limit=2 * 1024 * 1024):
    p = Path(path)
    v1.need(not p.is_symlink() and p.is_file(), 'bounded regular artifact required')
    descriptor = os.open(p, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as stream:
        v1.need(stat.S_ISREG(os.fstat(stream.fileno()).st_mode), 'bounded regular artifact required')
        data = stream.read(limit + 1)
    v1.need(len(data) <= limit, 'artifact exceeds byte limit')
    return data


class SignatureVerifier:
    def __init__(self, executable, executable_sha256, trust):
        self.executable = str(Path(executable).absolute())
        v1.sha(executable_sha256, 'openssl executable digest')
        v1.need(os.access(self.executable, os.X_OK), 'openssl executable unavailable')
        v1.need(hashlib.sha256(regular_bytes(self.executable, 128 * 1024 * 1024)).hexdigest() == executable_sha256,
                'openssl executable digest mismatch')
        v1.need(type(trust) is dict and 1 <= len(trust) <= 32, 'trusted key inventory required')
        self.trust = trust
        self.executable_sha256 = executable_sha256

    def verify(self, payload, signature, purpose='qualification'):
        v1.need(hashlib.sha256(regular_bytes(self.executable, 128 * 1024 * 1024)).hexdigest() == self.executable_sha256,
                'openssl executable changed')
        v1.keys(signature, ['schemaVersion', 'keyId', 'algorithm', 'signature'], 'signature')
        v1.need(type(signature['schemaVersion']) is int and signature['schemaVersion'] == 2,
                'signature schema version')
        v1.need(signature['algorithm'] == 'Ed25519', 'unsupported signature algorithm')
        key = self.trust.get(signature['keyId'])
        v1.need(key is not None, 'untrusted signature identity')
        v1.keys(key, ['path', 'sha256', 'purposes'], 'trusted public key')
        v1.need(type(key['purposes']) is list and 1 <= len(key['purposes']) <= 2
                and set(key['purposes']) <= {'qualification', 'activation'} and purpose in key['purposes'],
                'signature identity lacks required authority')
        v1.sha(key['sha256'], 'public key digest')
        public = regular_bytes(key['path'], 16384)
        v1.need(hashlib.sha256(public).hexdigest() == key['sha256'], 'trusted public key changed')
        v1.need(type(signature['signature']) is str and len(signature['signature']) <= 128,
                'bounded signature required')
        try:
            raw = base64.b64decode(signature['signature'], validate=True)
        except (ValueError, TypeError) as exc:
            raise v1.Invalid('invalid signature encoding') from exc
        v1.need(len(raw) == 64, 'Ed25519 signature length')
        with tempfile.TemporaryDirectory(prefix='kindle-topology-signature-') as directory:
            directory = Path(directory)
            (directory / 'key.pem').write_bytes(public)
            (directory / 'payload.json').write_bytes(v1.canonical(payload))
            (directory / 'signature.bin').write_bytes(raw)
            result = subprocess.run([self.executable, 'pkeyutl', '-verify', '-pubin',
                                     '-inkey', str(directory / 'key.pem'), '-rawin',
                                     '-in', str(directory / 'payload.json'),
                                     '-sigfile', str(directory / 'signature.bin')],
                                    stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                                    stderr=subprocess.DEVNULL, timeout=10, check=False)
        v1.need(result.returncode == 0, 'evidence signature verification failed')
        return {'keyId': signature['keyId'], 'payloadDigest': v1.digest(payload),
                'publicKeyDigest': key['sha256']}
