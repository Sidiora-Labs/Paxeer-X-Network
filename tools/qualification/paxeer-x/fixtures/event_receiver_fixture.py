"""Owned HTTPS event receiver for webhook delivery qualification.

The hosted webhooks service refuses destinations that resolve outside the
globally routable range. The receiver therefore runs inside a network and mount
namespace owned by the fixture: the chosen global address is bound to that
namespace's loopback device only, the receiver host name resolves through a
namespace-local hosts file, and the namespace has no other interface, so no
packet can leave it. Run the webhooks service, and every dependency it must
reach over loopback, through ``ns_command`` so they share that namespace. Host DNS, firewall and routes are never touched.
"""

import base64
import hashlib
import asyncio
import sqlite3
import http.server
import ipaddress
import json
import os
import pathlib
import shutil
import signal
import ssl
import subprocess
import sys
import threading
import time

sys.dont_write_bytecode = True

SIGNATURE_HEADER = 'layerx-webhook-signature'
DEFAULT_HOST = 'receiver.paxeer-x-fixture.test'
DEFAULT_PORT = 8443
ADDRESS_ENV = 'PAXEER_X_RECEIVER_ADDRESS'
READY_TIMEOUT = 20.0


def guard_accepts(address):
    value = ipaddress.ip_address(address)
    return value.version == 4 and value.is_global and not value.is_multicast


def host_global_address():
    output = subprocess.run(['ip', '-j', '-4', 'addr', 'show', 'scope', 'global'], check=True,
                            capture_output=True, text=True).stdout
    for link in json.loads(output or '[]'):
        for entry in link.get('addr_info', []):
            if guard_accepts(entry['local']):
                return entry['local']
    raise RuntimeError(f'MISSING globally routable host address; set {ADDRESS_ENV}')


def private_file(path, data):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, 'wb') as handle:
        handle.write(data)
    return path


class EventReceiverFixture:
    def __init__(self, work, ca, host=DEFAULT_HOST, address=None, port=DEFAULT_PORT):
        self.work = pathlib.Path(work)
        self.ca = ca
        self.host = host
        self.port = port
        self.address = address or os.environ.get(ADDRESS_ENV) or host_global_address()
        if not guard_accepts(self.address):
            raise RuntimeError(f'receiver address {self.address} is outside the destination guard range')
        self.root = self.work / 'event-receiver'
        self.state = self.root / 'received'
        self.endpoint = f'https://{host}:{port}/events'
        self.ca_der = pathlib.Path(ca.ca_der)
        self.materials = {}
        self.holder = None
        self.receiver = None

    def _materials(self):
        if self.materials:
            return
        self.root.mkdir(mode=0o700, exist_ok=True)
        self.state.mkdir(mode=0o700, exist_ok=True)
        certificate, key = self.ca.issue(self.host, [], False, san_dns=[self.host])
        hosts = private_file(self.root / 'hosts', f'127.0.0.1 localhost\n{self.address} {self.host}\n'.encode())
        resolver = private_file(self.root / 'resolv.conf', b'options attempts:1 timeout:1\n')
        os.chmod(hosts, 0o644)
        os.chmod(resolver, 0o644)
        self.materials = {'server_cert': pathlib.Path(certificate), 'server_key': pathlib.Path(key),
                          'ca_der': self.ca_der, 'ca_pem': pathlib.Path(self.ca.ca_pem),
                          'hosts': hosts, 'resolv_conf': resolver}

    def _start_namespace(self):
        if self.holder is not None and self.holder.poll() is None:
            return
        script = ('set -e; ip link set lo up; ip addr add "$1/32" dev lo; '
                  'mount --bind "$2" /etc/hosts; mount --bind "$3" /etc/resolv.conf; '
                  'touch "$4"; exec sleep infinity')
        marker = self.root / 'namespace.ready'
        marker.unlink(missing_ok=True)
        log = open(self.root / 'namespace.log', 'ab')
        self.holder = subprocess.Popen(
            ['unshare', '--net', '--mount', '--propagation', 'private', '--', 'sh', '-c', script, 'ns',
             self.address, str(self.materials['hosts']), str(self.materials['resolv_conf']), str(marker)],
            stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
        log.close()
        self._wait(lambda: marker.exists(), self.holder, 'receiver namespace')

    def ns_command(self, argv):
        if self.holder is None or self.holder.poll() is not None:
            raise RuntimeError('receiver namespace is not running')
        return ['nsenter', '--target', str(self.holder.pid), '--net', '--mount', '--', *map(str, argv)]

    def _wait(self, ready, process, name):
        deadline = time.monotonic() + READY_TIMEOUT
        while time.monotonic() < deadline:
            if ready():
                return
            if process.poll() is not None:
                raise RuntimeError(f'{name} exited with {process.returncode}; see {self.root}')
            time.sleep(0.05)
        raise RuntimeError(f'{name} did not become ready within {READY_TIMEOUT}s')

    def start(self):
        self._materials()
        self._start_namespace()
        if self.receiver is not None and self.receiver.poll() is None:
            return self
        marker = self.root / 'receiver.ready'
        marker.unlink(missing_ok=True)
        log = open(self.root / 'event-receiver.log', 'ab')
        self.receiver = subprocess.Popen(
            self.ns_command([sys.executable, pathlib.Path(__file__).resolve(), 'serve', self.root, self.address,
                             self.port, self.materials['server_cert'], self.materials['server_key']]),
            stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
        log.close()
        self._wait(lambda: marker.exists(), self.receiver, 'event receiver')
        return self

    @staticmethod
    def _terminate(process):
        if process is None or process.poll() is not None:
            return
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()

    def stop(self):
        self._terminate(self.receiver)
        self.receiver = None

    def restart(self):
        self.stop()
        return self.start()

    def cleanup(self):
        self.stop()
        self._terminate(self.holder)
        self.holder = None
        shutil.rmtree(self.root, ignore_errors=True)
        self.materials = {}

    def received(self):
        records = []
        for path in sorted(self.state.glob('*.json')):
            record = json.loads(path.read_text())
            record['body'] = base64.b64decode(record['body'])
            records.append(record)
        return records

    def __enter__(self):
        return self.start()

    def __exit__(self, *_):
        self.cleanup()


class DurableDeliveryStore:
    def __init__(self, path):
        self.path = path
        with sqlite3.connect(path) as connection:
            connection.execute('PRAGMA journal_mode=WAL')
            connection.execute('PRAGMA synchronous=FULL')
            connection.execute('CREATE TABLE IF NOT EXISTS deliveries (id TEXT PRIMARY KEY, digest TEXT NOT NULL, lease INTEGER NOT NULL, completed INTEGER NOT NULL)')
        path.chmod(0o600)

    async def claim(self, value):
        with sqlite3.connect(self.path, timeout=3) as connection:
            connection.execute('BEGIN IMMEDIATE')
            row = connection.execute('SELECT digest, lease, completed FROM deliveries WHERE id=?', (value.delivery_id,)).fetchone()
            if row is not None:
                if row[0] != value.payload_digest: return 'conflict'
                if row[2]: return 'completed'
                if row[1] > int(time.time() * 1000): return 'processing'
            connection.execute('INSERT OR REPLACE INTO deliveries VALUES (?, ?, ?, 0)',
                (value.delivery_id, value.payload_digest, value.lease_until_ms))
            return 'claimed'

    async def complete(self, identifier, digest):
        with sqlite3.connect(self.path, timeout=3) as connection:
            changed = connection.execute('UPDATE deliveries SET completed=1, lease=0 WHERE id=? AND digest=?', (identifier, digest))
            if changed.rowcount != 1: raise ValueError('receiver claim changed')

    async def release(self, identifier, digest):
        with sqlite3.connect(self.path, timeout=3) as connection:
            connection.execute('DELETE FROM deliveries WHERE id=? AND digest=? AND completed=0', (identifier, digest))


def serve(root, address, port, certificate, key):
    root = pathlib.Path(root)
    state = root / 'received'
    sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[4] / 'platform/integrations/fastapi'))
    from layerx_fastapi.webhooks import VerifiedWebhookConsumer, WebhookRequestHeaders
    from layerx_fastapi.protocol import MiddlewareError
    keys = {name: base64.b64decode(value, validate=True) for name, value in json.loads((root / 'public-keys.json').read_text()).items()}
    consumer = VerifiedWebhookConsumer(keys, DurableDeliveryStore(root / 'deliveries.sqlite'))
    lock = threading.Lock()
    counter = [len(list(state.glob('*.json')))]

    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = 'HTTP/1.1'

        def log_message(self, *_):
            pass

        def _reply(self, status, body):
            self.send_response(status)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            self.send_header('Connection', 'close')
            self.end_headers()
            self.wfile.write(body)
            self.close_connection = True

        def _record(self):
            length = int(self.headers.get('Content-Length', '0'))
            if length <= 0 or length > 1048576:
                raise ValueError('receiver body bound')
            body = self.rfile.read(length)
            headers = {name.lower(): value for name, value in self.headers.items()}
            identifier = headers['layerx-webhook-id']
            if len(identifier) != 64 or any(char not in '0123456789abcdef' for char in identifier):
                raise ValueError('receiver event identity refused')
            effects = root / 'effects'
            effects.mkdir(mode=0o700, exist_ok=True)
            async def apply(event, event_id):
                effect = effects / event_id
                if effect.exists():
                    if effect.read_bytes() != body: raise ValueError('receiver conflicting effect')
                    return
                fd = os.open(effect, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                with os.fdopen(fd, 'wb') as stream:
                    stream.write(body)
                    stream.flush()
                    os.fsync(stream.fileno())
            result = asyncio.run(consumer.consume(body, WebhookRequestHeaders(
                identifier, headers['layerx-webhook-timestamp'], headers['layerx-webhook-key-id'],
                headers[SIGNATURE_HEADER]), apply))
            if result == 'processing':
                raise ValueError('receiver delivery already processing')
            duplicate = result == 'duplicate'
            with lock:
                counter[0] += 1
                sequence = counter[0]
                record = {'sequence': sequence, 'received_at': time.time(), 'method': self.command,
                          'path': self.path, 'headers': headers, 'signature': headers.get(SIGNATURE_HEADER),
                          'body': base64.b64encode(body).decode(), 'duplicate': duplicate, 'verified': True}
                target = state / f'{sequence:08d}.json'
                temporary = target.with_suffix('.tmp')
                private_file(temporary, json.dumps(record, sort_keys=True).encode())
                os.replace(temporary, target)
            return sequence

        def do_POST(self):
            if self.path != '/events':
                self._reply(404, b'{"error":"path"}')
                return
            try:
                sequence = self._record()
            except (OSError, ValueError, KeyError, MiddlewareError, sqlite3.Error):
                self._reply(401, b'{"error":"verification_refused"}')
                return
            self._reply(200, json.dumps({'received': sequence}).encode())

        def do_GET(self):
            self._reply(405, b'{"error":"method"}')

    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(certificate, key)
    server = http.server.ThreadingHTTPServer((address, int(port)), Handler)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    signal.signal(signal.SIGTERM, lambda *_: threading.Thread(target=server.shutdown).start())
    (root / 'receiver.ready').touch()
    server.serve_forever()
    server.server_close()


if __name__ == '__main__' and len(sys.argv) == 7 and sys.argv[1] == 'serve':
    serve(*sys.argv[2:])
