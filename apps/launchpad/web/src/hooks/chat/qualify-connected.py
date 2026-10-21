import argparse
import asyncio
import base64
import collections
import contextlib
import datetime
import hashlib
import json
import ipaddress
import math
import os
from pathlib import Path
import re
import sqlite3
import ssl
import struct
import subprocess
import time
from urllib.parse import urlsplit, urlencode
import uuid

ROOT = Path(__file__).resolve().parents[6]
GLOBAL = {
    'peak': {'connections': 100000, 'authenticated_connections': 50000, 'dynamic_rps': 10000, 'write_rps': 2000, 'fanout_fps': 250000},
    'surge': {'connections': 200000, 'authenticated_connections': 100000, 'dynamic_rps': 20000, 'write_rps': 4000, 'fanout_fps': 1000000},
}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as source:
        for block in iter(lambda: source.read(1048576), b''):
            h.update(block)
    return h.hexdigest()


def position(value):
    require(isinstance(value, str) and re.fullmatch(r'[1-9][0-9]{0,18}', value) and int(value) <= 9223372036854775807, 'invalid durable position')
    return int(value)


def process_identity(node):
    proc = Path(node['proc_root']) / str(node['pid'])
    fields = (proc / 'stat').read_text().rsplit(')', 1)[1].split()
    executable = digest(proc / 'exe')
    receipt = json.loads(Path(node['build_receipt_path']).read_text())
    require(receipt['binary_sha256'] == executable, 'build receipt does not identify running executable')
    require(executable == node['binary_sha256'], 'running executable differs from admitted build')
    return {'node': node['id'], 'pid': node['pid'], 'start_ticks': fields[19], 'binary_sha256': executable}


def session_credentials(session):
    if 'credential_file' not in session:
        require(session.get('captured_credentials') is True, 'actual credential provider file required')
        return session
    path=Path(session['credential_file'])
    require(path.stat().st_size <= 16384,'oversized session credential provider record')
    require(path.stat().st_mode & 0o077 == 0,'current session credential provider file must be private')
    actual=json.loads(path.read_text())
    require(actual['wallet']==session['wallet'],'credential refresh changed allocated actor')
    require(0 < len(actual['cookie']) <= 4096 and re.fullmatch(r'[0-9a-f]{64}',actual['csrf']),'actual refreshed cookie/CSRF unavailable')
    require('\r' not in actual['cookie'] and '\n' not in actual['cookie'],'invalid refreshed cookie header')
    return {'wallet':session['wallet'],'source_ip':session['source_ip'],'cookie':actual['cookie'],'csrf':actual['csrf'],'captured_credentials':True}


def validate(config):
    require(all(Path(config[name]).is_file() for name in ('ca_file','client_cert_file','client_key_file','psql_executable')),'actual TLS and PostgreSQL executables/authority files required')
    origin=urlsplit(config['origin'])
    require(origin.scheme=='https' and origin.hostname and not origin.username and not origin.password and not origin.query and not origin.fragment and origin.path in ('','/'),'actual HTTPS browser origin required')
    ipaddress.ip_address(config['control_source_ip'])
    require(config['profile'] in GLOBAL, 'explicit peak or surge profile required')
    require(re.fullmatch(r'[a-z_][a-z0-9_]{0,62}', config['database_schema']), 'invalid social schema')
    require(config['duration_seconds'] > 0 and config['settle_seconds'] > 0, 'positive allocated duration and settlement deadline required')
    require(config['profile'] != 'surge' or config['duration_seconds'] >= 600, 'surge measurement must cover600 seconds')
    require(config['source_files'] and config['nodes'] and config['sessions'], 'source, topology and actual sessions required')
    required_sources={'client/apps/web/src/hooks/chat/qualify-connected.py','media/social/internal/fanout/conn.go','media/social/internal/fanout/hub.go','media/social/internal/fanout/durable.go','media/social/internal/fanout/notification.go','client/apps/web/src/core/clients/chat-ws.ts','client/apps/web/src/hooks/chat/use-chat.ts'}
    require(required_sources <= set(config['source_files']),'measurement source manifest omits a required actual code path')
    require(config['request_timeout_seconds'] > 0 and config['metrics_interval_seconds'] > 0 and config['privacy_observation_seconds'] > 0 and config['maximum_clock_skew_seconds'] > 0,'bounded request/measurement/ACL observation inputs required')
    for name, expected in config['source_files'].items():
        path = (ROOT / name).resolve()
        require(path.is_relative_to(ROOT) and path.is_file() and digest(path) == expected, 'source candidate digest mismatch')
    require(re.fullmatch(r'[0-9a-f]{64}', config['interface_sha256']), 'admitted interface digest required')
    for name, maximum in GLOBAL[config['profile']].items():
        allocation = config['allocation'][name]
        require(isinstance(allocation, int) and 0 < allocation <= maximum, 'allocation exceeds global ' + name)
    workload = config['workload']
    require(1001 <= workload['connections'] <= config['allocation']['authenticated_connections'], 'social socket allocation exceeded')
    require(workload['connections'] <= config['allocation']['connections'], 'connection allocation exceeded')
    require(0 < workload['message_rps'] <= config['allocation']['write_rps'], 'write allocation exceeded')
    require(0 < workload['http_rps'] <= config['allocation']['dynamic_rps'] - workload['message_rps'],'HTTP plus socket-write subset allocation exceeded')
    require(workload['message_rps'] * (workload['connections'] + 1) <= config['allocation']['fanout_fps'], 'hot-room fanout allocation exceeded')
    require(workload['message_rps'] * config['duration_seconds'] >= 1000,'actual workload must generate at least1000 sends')
    require(workload['connections'] <= len(config['sessions']), 'insufficient real session credentials; sessions cannot be synthesized')
    require(len({row['wallet'] for row in config['sessions']}) >= 3, 'three genuine wallet identities required')
    require(50 <= workload['content_bytes'] <= 2000,'actual message payload allocation must fit retained content bounds')
    require(workload['reconnect_every_seconds'] > 0 and workload['pause_reader_seconds'] > 0, 'reconnect and slow-reader intervals required')
    require(workload['maximum_pending_sends'] > 0 and workload['connect_concurrency'] > 0, 'bounded generator admission required')
    require(re.fullmatch(r'0x[0-9a-f]{40}', workload['pool']), 'actual admitted pool required')
    for session in config['sessions']:
        require(re.fullmatch(r'0x[0-9a-f]{40}',session['wallet']) and session['credential_file'],'actual allocated wallet and credential provider file required')
        ipaddress.ip_address(session['source_ip'])
        session_credentials(session)
    identities = [process_identity(node) for node in config['nodes']]
    for node in config['nodes']:
        receipt = json.loads(Path(node['build_receipt_path']).read_text())
        require(receipt['source_files'] == config['source_files'] and receipt['interface_sha256'] == config['interface_sha256'], 'running build receipt belongs to a different candidate or interface')
        require(node['rss_limit_bytes'] > 0, 'runtime memory allocation required')
    require(len({node['node'] for node in identities}) == len(identities), 'duplicate runtime topology node')
    require(config['limits_ms']['commit_p99'] > 0 and config['limits_ms']['fanout_p99'] > 0 and config['limits_ms']['replay_p99'] > 0, 'admitted latency limits required')
    return identities


class Ledger:
    def __init__(self, path):
        self.db = sqlite3.connect(path)
        os.chmod(path, 0o600)
        self.db.executescript('PRAGMA journal_mode=WAL; CREATE TABLE attempts(client_id TEXT PRIMARY KEY,sender TEXT,started_ns INTEGER,kind TEXT NOT NULL,ack_id TEXT,conversation TEXT,sequence TEXT,revision TEXT); CREATE TABLE samples(kind TEXT,latency_ms REAL); CREATE INDEX sample_order ON samples(kind,latency_ms); CREATE TABLE received(peer INTEGER,id TEXT,revision INTEGER,sequence INTEGER,PRIMARY KEY(peer,id));')
        self.recent = collections.OrderedDict()
        self.events = 0

    def attempt(self, key, sender, started, bound, kind="pool"):
        self.db.execute('INSERT INTO attempts(client_id,sender,started_ns,kind) VALUES(?,?,?,?)', (key, sender, started, kind))
        self.recent[key] = started
        while len(self.recent) > bound:
            self.recent.popitem(last=False)

    def start(self, key):
        if key in self.recent:
            return self.recent[key]
        row = self.db.execute('SELECT started_ns FROM attempts WHERE client_id=?', (key,)).fetchone()
        return row[0] if row else None

    def sample(self, kind, started, observed):
        require(observed >= started, 'negative monotonic latency')
        self.db.execute('INSERT INTO samples VALUES(?,?)', (kind, (observed - started) / 1000000))
        self.events += 1
        if self.events % 1000 == 0:
            self.db.commit()

    def percentiles(self, kind):
        count = self.db.execute('SELECT count(*) FROM samples WHERE kind=?', (kind,)).fetchone()[0]
        require(count > 0, 'missing actual ' + kind + ' latency samples')
        result = {'count': count}
        for p in (50, 95, 99):
            index = max(0, math.ceil(count * p / 100) - 1)
            result['p' + str(p)] = self.db.execute('SELECT latency_ms FROM samples WHERE kind=? ORDER BY latency_ms LIMIT 1 OFFSET ?', (kind, index)).fetchone()[0]
        return result


class Transport:
    def __init__(self, config, raw):
        self.config = config
        self.raw = raw
        self.context = ssl.create_default_context(cafile=config['ca_file'])
        self.context.load_cert_chain(config['client_cert_file'], config['client_key_file'])
        self.counts = collections.Counter()
        self.total = collections.Counter()
        self.http_lock=asyncio.Lock()
        self.next_http=time.monotonic()

    def account(self, kind):
        second = int(time.monotonic())
        self.counts[(second, kind)] += 1
        self.total[kind] += 1
        budget = self.config['allocation'][{'http':'dynamic_rps','frames':'fanout_fps','writes':'write_rps'}[kind]]
        require(self.counts[(second, kind)] <= budget, 'observed lane ' + kind + ' budget exceeded')
        require(self.counts[(second,'http')] + self.counts[(second,'writes')] <= self.config['allocation']['dynamic_rps'], 'dynamic request/write subset budget exceeded')
        for entry in list(self.counts):
            if entry[0] < second - 2:
                del self.counts[entry]

    def record(self, value):
        self.raw.write(json.dumps({'observed_ns': time.monotonic_ns(), **value}, separators=(',', ':')) + '\n')

    async def stream(self, endpoint, session=None):
        target = urlsplit(endpoint)
        require(target.scheme in ('https', 'wss') and target.hostname and not target.username and not target.password and not target.fragment, 'TLS endpoint without URL credentials required')
        reader, writer = await asyncio.wait_for(asyncio.open_connection(target.hostname, target.port or 443, ssl=self.context, server_hostname=target.hostname, local_addr=(session['source_ip'] if session else self.config['control_source_ip'],0)), self.config['request_timeout_seconds'])
        return target, reader, writer

    async def response(self, reader):
        header = await reader.readuntil(b'\r\n\r\n')
        require(len(header) <= 16384, 'oversized HTTP headers')
        lines = header.decode('ascii').split('\r\n')
        status = int(lines[0].split()[1])
        fields = {}
        for line in lines[1:]:
            if line:
                name, value = line.split(':', 1)
                fields[name.lower()] = value.strip()
        return status, fields

    async def request(self, endpoint, session=None, method='GET', body=None):
        return await asyncio.wait_for(self._request(endpoint, session, method, body), self.config['request_timeout_seconds'])

    async def _request(self, endpoint, session=None, method='GET', body=None):
        if session is not None: session=session_credentials(session)
        async with self.http_lock:
            wait=max(0,self.next_http-time.monotonic())
            self.next_http=max(self.next_http,time.monotonic())+1/self.config['workload']['http_rps']
            await asyncio.sleep(wait)
            self.account('http')
        target, reader, writer = await self.stream(endpoint,session)
        payload = b'' if body is None else json.dumps(body, separators=(',', ':')).encode()
        headers = {'Host': target.netloc, 'Connection': 'close', 'Accept': 'application/json', 'Content-Length': str(len(payload)), 'Origin': self.config['origin']}
        if session:
            headers['Cookie'] = session['cookie']
            if method != 'GET':
                headers['X-CSRF-Token'] = session['csrf']
        if body is not None:
            headers['Content-Type'] = 'application/json'
        path = target.path or '/'
        if target.query:
            path += '?' + target.query
        request = f'{method} {path} HTTP/1.1\r\n' + ''.join(f'{key}: {value}\r\n' for key, value in headers.items()) + '\r\n'
        try:
            writer.write(request.encode() + payload)
            await writer.drain()
            status, fields = await asyncio.wait_for(self.response(reader), self.config['request_timeout_seconds'])
            body = bytearray()
            if fields.get('transfer-encoding', '').lower() == 'chunked':
                while True:
                    length = int((await reader.readline()).split(b';')[0], 16)
                    require(length <= 1048576 - len(body), 'oversized response')
                    if length == 0:
                        break
                    body.extend(await reader.readexactly(length))
                    require(await reader.readexactly(2) == b'\r\n', 'invalid HTTP chunk')
            elif 'content-length' in fields:
                length = int(fields['content-length'])
                require(0 <= length <= 1048576, 'oversized response')
                body.extend(await reader.readexactly(length))
            else:
                while chunk := await reader.read(65536):
                    body.extend(chunk)
                    require(len(body) <= 1048576, 'oversized response')
            if target.path in {urlsplit(self.config[name]).path for name in ('history_url','conversations_url','read_url')}:
                require('no-store' in fields.get('cache-control','').lower(),'private chat response is cacheable')
            self.record({'kind': 'http', 'status': status, 'bytes': len(body), 'cache_control':fields.get('cache-control','')})
            require('json' in fields.get('content-type', ''), 'JSON response required')
            return status, json.loads(body)
        finally:
            writer.close()
            with contextlib.suppress(Exception):
                await writer.wait_closed()

    async def connect(self, session, topic):
        session=session_credentials(session)
        topics = [topic]
        binding = {'schemaMajor': 2, 'topics': topics, 'filterDigest': hashlib.sha256(json.dumps(sorted(topics), separators=(',', ':')).encode()).hexdigest(), 'origin': self.config['origin'], 'endpoint': '/v2/stream'}
        status, envelope = await self.request(self.config['ticket_url'], session, 'POST', binding)
        require(status == 200, 'actual ticket issuance rejected')
        ticket = envelope['data']
        expiry = datetime.datetime.fromisoformat(ticket['expiresAt'].replace('Z', '+00:00')).timestamp()
        require(re.fullmatch(r'[0-9a-f]{64}', ticket['ticket']) and ticket['subprotocol'] == 'sidiora.v2' and time.time() < expiry <= time.time() + 60, 'invalid actual upgrade ticket')
        endpoint = self.config['socket_url'] + ('&' if '?' in self.config['socket_url'] else '?') + urlencode({'topics': topic})
        target, reader, writer = await self.stream(endpoint,session)
        key = base64.b64encode(os.urandom(16)).decode()
        path = target.path + '?' + target.query
        request = f'GET {path} HTTP/1.1\r\nHost: {target.netloc}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: sidiora.v2, ticket.{ticket["ticket"]}\r\nOrigin: {self.config["origin"]}\r\nCookie: {session["cookie"]}\r\n\r\n'
        try:
            writer.write(request.encode()); await writer.drain()
            status, headers = await self.response(reader)
            expected = base64.b64encode(hashlib.sha1((key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
            require(status == 101 and headers.get('sec-websocket-accept') == expected and headers.get('sec-websocket-protocol') == 'sidiora.v2', 'actual websocket upgrade rejected')
            socket = Socket(self, reader, writer)
            accepted = await asyncio.wait_for(socket.receive(), self.config['request_timeout_seconds'])
            require(accepted.get('type') == 'session_accepted' and accepted.get('protocol_version') == 2, 'session acceptance required before connected')
            return socket
        except BaseException:
            writer.close()
            raise


class Socket:
    def __init__(self, transport, reader, writer):
        self.transport, self.reader, self.writer = transport, reader, writer
        self.lock = asyncio.Lock()
        self.close_code = None

    async def send(self, value=None, opcode=1, payload=None):
        body = json.dumps(value, separators=(',', ':')).encode() if payload is None else payload
        require(len(body) <= 16384, 'inbound frame exceeds16KiB')
        mask = os.urandom(4)
        head = bytes([128 | opcode, 128 | len(body)]) if len(body) < 126 else bytes([128 | opcode, 128 | 126]) + struct.pack('!H', len(body))
        wire = head + mask + bytes(value ^ mask[index % 4] for index, value in enumerate(body))
        async with self.lock:
            self.writer.write(wire)
            await self.writer.drain()

    async def receive(self):
        fragments = bytearray()
        message_opcode = None
        while True:
            first, second = await self.reader.readexactly(2)
            require(first & 112 == 0 and second & 128 == 0, 'unsupported websocket extensions or masked server frame')
            final, opcode, length = bool(first & 128), first & 15, second & 127
            if length == 126:
                length = struct.unpack('!H', await self.reader.readexactly(2))[0]
            elif length == 127:
                length = struct.unpack('!Q', await self.reader.readexactly(8))[0]
            require(length <= 1048576 - len(fragments), 'server frame exceeds capture memory bound')
            payload = await self.reader.readexactly(length)
            if opcode == 8:
                self.close_code = struct.unpack('!H', payload[:2])[0] if len(payload) >= 2 else 1005
                self.transport.record({'kind': 'socket_close', 'code': self.close_code})
                raise ConnectionError('socket closed')
            if opcode == 9:
                await self.send(opcode=10, payload=payload)
                continue
            if opcode == 10:
                continue
            require(opcode in (0, 1) and (opcode != 0 or message_opcode == 1), 'text message required')
            if opcode == 1:
                require(message_opcode is None, 'interleaved fragmented message')
                message_opcode = 1
            fragments.extend(payload)
            if final:
                self.transport.account('frames')
                value = json.loads(fragments)
                require(isinstance(value, dict), 'invalid websocket envelope')
                return value

    async def close(self):
        self.writer.close()
        with contextlib.suppress(Exception):
            await self.writer.wait_closed()


async def history(transport, session, conversation, cursor, ledger, peer):
    start = time.monotonic_ns()
    last = 0
    count = 0
    resets = 0
    while True:
        query = urlencode({'conversation_id': conversation, 'cursor': cursor, 'limit': 100})
        status, page = await transport.request(transport.config['history_url'] + '?' + query, session)
        if status == 410:
            require(page.get('error') in ('history_snapshot_required', 'replay_window_expired') and page.get('next_cursor') and resets < 2, 'invalid replay restart')
            resets += 1
            cursor = page['next_cursor']; last = 0
            ledger.db.execute('DELETE FROM received WHERE peer=?', (peer,))
            continue
        require(status == 200 and isinstance(page['messages'], list) and len(page['messages']) <= 100, 'actual bounded history failed')
        for row in page['messages']:
            sequence, revision = position(row['sequence']), position(row['revision'])
            require(sequence > last and row['conversation_id'] == conversation, 'unordered or foreign replay')
            require(not row['deleted'] or row['content'] == '', 'tombstone released content')
            existing = ledger.db.execute('SELECT revision,sequence FROM received WHERE peer=? AND id=?', (peer, row['id'])).fetchone()
            require(not existing or existing[1] == sequence, 'replay identity changed sequence')
            ledger.db.execute('INSERT INTO received VALUES(?,?,?,?) ON CONFLICT(peer,id) DO UPDATE SET revision=max(revision,excluded.revision)', (peer, row['id'], revision, sequence))
            last = sequence; count += 1
        next_cursor = page['next_cursor']
        require(next_cursor and (len(page['messages']) < 100 or next_cursor != cursor), 'history cursor did not advance')
        cursor = next_cursor
        if len(page['messages']) < 100:
            break
    completed=time.monotonic_ns()
    ledger.sample('replay', start, completed)
    transport.record({'started_ns':start,'completed_ns':completed,'kind': 'replay', 'peer': peer, 'rows': count, 'last_sequence': last, 'resets': resets})
    return cursor


def reconcile(config, ledger, output):
    ledger.db.commit()
    cursor = ledger.db.execute('SELECT client_id,sender,ack_id,conversation,sequence FROM attempts ORDER BY client_id')
    totals = {'attempted': 0, 'acknowledged': 0, 'lost_acknowledged': 0, 'duplicate_creation': 0, 'persisted_unacknowledged': 0}
    schema = '"' + config['database_schema'] + '"'
    while batch := cursor.fetchmany(500):
        probe = [{'client': row[0], 'sender': row[1], 'ack': row[2], 'conversation': row[3], 'sequence': row[4]} for row in batch]
        encoded = json.dumps(probe, separators=(',', ':')).replace("'", "''")
        sql = f'''BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; SELECT coalesce(json_agg(result),'[]'::json) FROM (SELECT a.client,a.ack,a.conversation,a.sequence,m.id,m.conversation_id,m.sequence::text AS persisted_sequence,(SELECT count(*) FROM {schema}.chat_outbox o WHERE o.message_id=m.id AND o.revision=1) AS creation_intents FROM jsonb_to_recordset('{encoded}'::jsonb) AS a(client text,sender text,ack text,conversation text,sequence text) LEFT JOIN {schema}.chat_messages m ON m.sender=a.sender AND m.client_id=a.client) result; ROLLBACK;'''
        environment = dict(os.environ, PGDATABASE=config['database_dsn'])
        completed = subprocess.run([config['psql_executable'], '-X', '-q', '-t', '-A', '-v', 'ON_ERROR_STOP=1'], input=sql, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, timeout=15)
        require(completed.returncode == 0, 'actual PostgreSQL reconciliation failed; credentials and schema authority must be admitted')
        rows = json.loads(completed.stdout.strip())
        require(len(rows) == len(batch), 'reconciliation did not return every send attempt')
        for row in rows:
            totals['attempted'] += 1
            if row['ack']:
                totals['acknowledged'] += 1
                if row['id'] != row['ack'] or row['conversation_id'] != row['conversation'] or row['persisted_sequence'] != row['sequence']:
                    totals['lost_acknowledged'] += 1
            elif row['id']:
                totals['persisted_unacknowledged'] += 1
            if row['id'] and row['creation_intents'] != 1:
                totals['duplicate_creation'] += 1
            output.write(json.dumps({'client_id': row['client'], 'ack_id': row['ack'], 'persisted_id': row['id'], 'creation_intents': row['creation_intents']}) + '\n')
    require(totals['acknowledged'] > 0 and totals['lost_acknowledged'] == 0 and totals['duplicate_creation'] == 0, 'acknowledged database reconciliation failed')
    return totals


async def workload(config, ledger, transport, identities):
    count = config['workload']['connections']
    pool = config['workload']['pool']
    def content(key):
        prefix='capacity '+key
        return prefix+'.'*(config['workload']['content_bytes']-len(prefix))
    topic = 'chat.pool.' + pool
    sessions = config['sessions'][:count]
    sockets = [None] * count
    slot_locks=[asyncio.Lock() for _ in range(count)]
    cursors = [''] * count
    readers = []
    stopped = asyncio.Event()
    pending = set()
    failures = []
    slow_evicted = False
    semaphore = asyncio.Semaphore(config['workload']['connect_concurrency'])

    async def connect(index):
        async with semaphore, slot_locks[index]:
            if sockets[index] is not None: await sockets[index].close()
            start = time.monotonic_ns()
            socket = await transport.connect(sessions[index], topic)
            await socket.send({'type': 'join_pool', 'pool_address': pool})
            while True:
                frame = await asyncio.wait_for(socket.receive(), config['request_timeout_seconds'])
                if frame.get('type') == 'joined_pool':
                    require(frame.get('pool_address') == pool, 'wrong actual joined pool')
                    break
                require(frame.get('type') == 'pool_message' and frame.get('pool_address') == pool, 'actual room join rejected')
            sockets[index] = socket
            completed=time.monotonic_ns()
            ledger.sample('handshake', start, completed)
            transport.record({'kind':'handshake','peer':index,'started_ns':start,'completed_ns':completed})
            cursors[index] = await history(transport, sessions[index], 'pool:' + pool, cursors[index], ledger, index)

    async def consume(index, pause=True):
        nonlocal slow_evicted
        if pause and index == count - 1:
            await asyncio.sleep(config['workload']['pause_reader_seconds'])
        try:
            while not stopped.is_set():
                frame = await sockets[index].receive()
                now = time.monotonic_ns()
                kind = frame.get('type')
                if kind == 'message_persisted':
                    key = frame['client_message_id']
                    start = ledger.start(key)
                    require(start is not None, 'acknowledgement outside generated attempt set')
                    position(frame['sequence']); position(frame['revision'])
                    prior = ledger.db.execute('SELECT ack_id FROM attempts WHERE client_id=?', (key,)).fetchone()[0]
                    require(prior is None or prior == frame['id'], 'retry acknowledgement changed persisted ID')
                    ledger.db.execute('UPDATE attempts SET ack_id=?,conversation=?,sequence=?,revision=? WHERE client_id=?', (frame['id'], frame['conversation_id'], frame['sequence'], frame['revision'], key))
                    pending.discard(key)
                    ledger.sample('commit', start, now)
                    transport.record({'kind': 'ack', 'started_ns':start,'received_ns':now, 'client_id': key, 'id': frame['id'], 'sequence': frame['sequence'], 'peer': index, 'latency_ms': (now-start)/1000000})
                elif kind == 'pool_message':
                    require(frame['conversation_id'] == 'pool:' + pool and frame.get('pool_address') == pool, 'foreign room payload received')
                    sequence, revision = position(frame['sequence']), position(frame['revision'])
                    require(not frame['deleted'] or frame['content'] == '', 'deleted payload retained text')
                    previous = ledger.db.execute('SELECT revision,sequence FROM received WHERE peer=? AND id=?', (index, frame['id'])).fetchone()
                    require(not previous or previous[1] == sequence, 'live identity changed sequence')
                    ledger.db.execute('INSERT INTO received VALUES(?,?,?,?) ON CONFLICT(peer,id) DO UPDATE SET revision=max(revision,excluded.revision)', (index, frame['id'], revision, sequence))
                    start = ledger.start(frame.get('client_message_id'))
                    if start is not None:
                        ledger.sample('fanout', start, now)
                    transport.record({'kind': 'fanout', 'started_ns':start,'received_ns':now, 'peer': index, 'id': frame['id'], 'sequence': sequence, 'revision': revision, 'duplicate': bool(previous)})
                elif kind in ('error', 'message_uncertain'):
                    transport.record({'kind': kind, 'peer': index, 'client_id': frame.get('client_message_id')})
                    failures.append(kind)
        except (ConnectionError, asyncio.IncompleteReadError):
            if not stopped.is_set():
                if index == count - 1:
                    require(sockets[index].close_code == 4008,'stalled reader was not closed with replay-required4008')
                    slow_evicted = True
                transport.record({'kind': 'reader_interrupted', 'peer': index, 'code': sockets[index].close_code})
                await connect(index)
                if not stopped.is_set():
                    await consume(index, pause=False)

    async def metrics():
        previous={}
        while not stopped.is_set():
            for node, identity in zip(config['nodes'], identities):
                require(process_identity(node) == identity, 'runtime process/build identity changed during measurement')
                status, actual = await transport.request(node['queue_metrics_url'])
                require(status == 200 and actual['queue_frames_peak'] <= 256 and actual['queue_bytes_peak'] <= 1048576, 'actual per-socket queue bound violated')
                fields=('connected','evicted','queue_frames_peak','queue_bytes_peak','queue_frames_current','queue_bytes_current','enqueued_frames','enqueued_bytes','written_frames','written_bytes')
                require(all(isinstance(actual[name],int) and actual[name]>=0 for name in fields),'invalid actual queue counters')
                observed=datetime.datetime.fromisoformat(actual['observed_at'].replace('Z','+00:00')).timestamp()
                require(abs(time.time()-observed)<=config['maximum_clock_skew_seconds'],'stale actual queue telemetry')
                prior=previous.get(node['id'])
                cumulative=('evicted','queue_frames_peak','queue_bytes_peak','enqueued_frames','enqueued_bytes','written_frames','written_bytes')
                require(prior is None or all(actual[name]>=prior[name] for name in cumulative),'runtime queue counters regressed')
                previous[node['id']]=actual
                status_text = (Path(node['proc_root']) / str(node['pid']) / 'status').read_text()
                rss = re.search(r'^VmRSS:\s+(\d+) kB$', status_text, re.MULTILINE)
                require(rss is not None, 'actual process RSS missing')
                rss_bytes = int(rss.group(1)) * 1024
                require(rss_bytes <= node['rss_limit_bytes'], 'actual process memory allocation exceeded')
                transport.record({'kind': 'queue_measurement', 'node': node['id'], 'rss_bytes':rss_bytes, **actual})
            await asyncio.sleep(config['metrics_interval_seconds'])

    async def reconnect():
        while not stopped.is_set():
            await asyncio.sleep(config['workload']['reconnect_every_seconds'])
            if stopped.is_set():
                return
            index = 0
            readers[index].cancel()
            await asyncio.gather(readers[index], return_exceptions=True)
            await connect(index)
            readers[index] = asyncio.create_task(consume(index, pause=False))
            for key in list(pending):
                actor = ledger.db.execute('SELECT sender FROM attempts WHERE client_id=?', (key,)).fetchone()[0]
                if actor == sessions[index]['wallet']:
                    transport.account('writes')
                    await sockets[index].send({'type':'pool_message','pool_address':pool,'content':content(key),'client_message_id':key})
                    transport.record({'kind':'same_id_retry','client_id':key,'peer':index})

    try:
        await asyncio.gather(*(connect(index) for index in range(count)))
        readers.extend(asyncio.create_task(consume(index)) for index in range(count))
        background = [asyncio.create_task(metrics()), asyncio.create_task(reconnect())]
        started = time.monotonic()
        rate = config['workload']['message_rps']
        next_send = started
        index = 0
        while time.monotonic() - started < config['duration_seconds']:
            for task in background + readers:
                if task.done():
                    task.result()
                    raise RuntimeError('workload worker stopped unexpectedly')
            require(len(pending) < config['workload']['maximum_pending_sends'], 'generator pending-memory budget exhausted')
            key = str(uuid.uuid4())
            sender = index % max(1, count - 1)
            require(sockets[sender] is not None, 'missing actual sending socket')
            now = time.monotonic_ns()
            transport.account('writes')
            ledger.attempt(key, sessions[sender]['wallet'], now, config['workload']['maximum_pending_sends'])
            pending.add(key)
            async with slot_locks[sender]:
                await sockets[sender].send({'type': 'pool_message', 'pool_address': pool, 'content': content(key), 'client_message_id': key})
            transport.record({'kind': 'send', 'started_ns':now,'client_id': key, 'peer': sender})
            index += 1
            next_send += 1 / rate
            await asyncio.sleep(max(0, next_send - time.monotonic()))
        sent = index
        generator_seconds=time.monotonic()-started
        require(sent >= math.floor(config['duration_seconds']*rate),'generator did not drive allocated write workload')
        background[1].cancel()
        await asyncio.gather(background[1],return_exceptions=True)
        await asyncio.sleep(config['settle_seconds'])
        for task in readers + background[:1]:
            if task.done(): task.result()
        require(not failures and not pending, 'send errors or unacknowledged sends remain at settlement deadline')
        require(slow_evicted,'actual paused socket did not exercise queue-budget eviction')
        for index in (0,count-1):
            cursors[index] = await history(transport, sessions[index], 'pool:' + pool, cursors[index], ledger, index)
        expected = ledger.db.execute("SELECT count(DISTINCT ack_id) FROM attempts WHERE ack_id IS NOT NULL AND kind='pool'").fetchone()[0]
        for index in range(count):
            actual = ledger.db.execute("SELECT count(*) FROM received r JOIN attempts a ON a.ack_id=r.id WHERE r.peer=? AND a.kind='pool'", (index,)).fetchone()[0]
            require(actual == expected, 'healthy/reconnected recipient lost acknowledged IDs')
        return {'duration_seconds': time.monotonic()-started, 'sent': sent, 'generator_seconds':generator_seconds,'observed_write_rps':sent/generator_seconds, 'distinct_acknowledged': expected, 'observed_http_requests': transport.total['http'], 'observed_server_frames': transport.total['frames']}
    finally:
        stopped.set()
        for task in readers + locals().get('background', []):
            task.cancel()
        await asyncio.gather(*(readers + locals().get('background', [])), return_exceptions=True)
        await asyncio.gather(*(socket.close() for socket in sockets if socket), return_exceptions=True)


async def privacy_probe(config, ledger, transport):
    sessions = []
    for session in config['sessions']:
        if session['wallet'] not in {row['wallet'] for row in sessions}:
            sessions.append(session)
        if len(sessions) == 3: break
    sockets = []
    try:
        for session in sessions:
            socket = await transport.connect(session, 'private.chat.' + session['wallet'])
            await socket.send({'type':'subscribe_dms'})
            frame = await asyncio.wait_for(socket.receive(), config['request_timeout_seconds'])
            require(frame.get('type') == 'subscribed_dms', 'actual private subscription rejected')
            sockets.append(socket)
        key = str(uuid.uuid4())
        start = time.monotonic_ns()
        ledger.attempt(key, sessions[0]['wallet'], start, config['workload']['maximum_pending_sends'], kind='dm')
        transport.account('writes')
        await sockets[0].send({'type':'dm','to':sessions[1]['wallet'],'content':'private capacity '+key,'client_message_id':key,'sender':sessions[2]['wallet']})
        ack = None
        while ack is None:
            frame = await asyncio.wait_for(sockets[0].receive(),config['request_timeout_seconds'])
            if frame.get('type') == 'message_persisted': ack = frame
            else: require(frame.get('type') == 'dm','actual private send rejected')
        require(ack['client_message_id'] == key,'private acknowledgement changed client identity')
        ledger.db.execute('UPDATE attempts SET ack_id=?,conversation=?,sequence=?,revision=? WHERE client_id=?',(ack['id'],ack['conversation_id'],ack['sequence'],ack['revision'],key))
        ledger.sample('commit',start,time.monotonic_ns())
        received = await asyncio.wait_for(sockets[1].receive(),config['request_timeout_seconds'])
        require(received.get('type') == 'dm' and received['id'] == ack['id'] and received['sender'] == sessions[0]['wallet'],'frame sender changed immutable actual principal')
        query = urlencode({'conversation_id':ack['conversation_id'],'limit':100})
        status, authorized = await transport.request(config['history_url']+'?'+query,sessions[0])
        require(status == 200 and any(row['id']==ack['id'] for row in authorized['messages']),'authorized private history missing acknowledgement')
        for conversation, cursor, expected in ((ack['conversation_id'],'',404),(str(uuid.uuid4()),'',404),(ack['conversation_id'],authorized['next_cursor'],400)):
            query = urlencode({'conversation_id':conversation,'cursor':cursor,'limit':100})
            status, response = await transport.request(config['history_url']+'?'+query,sessions[2])
            require(status == expected and 'messages' not in response,'third-wallet REST/cursor private payload leak')
        await sockets[2].send({'type':'subscribe_dms','wallet':sessions[0]['wallet']})
        frame = await asyncio.wait_for(sockets[2].receive(),config['request_timeout_seconds'])
        require(frame.get('type') == 'subscribed_dms','forged frame identity replaced scoped subscription')
        try:
            frame = await asyncio.wait_for(sockets[2].receive(),config['privacy_observation_seconds'])
            require(frame.get('type') not in ('dm','pool_message'),'unauthorized socket received private payload')
        except asyncio.TimeoutError:
            pass
        second_device=await transport.connect(sessions[1],'private.chat.'+sessions[1]['wallet'])
        sockets.append(second_device)
        await second_device.send({'type':'subscribe_dms'})
        require((await asyncio.wait_for(second_device.receive(),config['request_timeout_seconds'])).get('type')=='subscribed_dms','second real device private subscription failed')
        transport.account('writes')
        status,result=await transport.request(config['read_url'],sessions[1],'POST',{'conversation_id':ack['conversation_id'],'through_sequence':ack['sequence']})
        require(status==200 and result.get('success') is True,'actual read marker failed')
        for device in (sockets[1],second_device):
            marker=await asyncio.wait_for(device.receive(),config['request_timeout_seconds'])
            require(marker.get('type')=='read_marker' and marker['conversation_id']==ack['conversation_id'] and int(marker['through_sequence'])>=position(ack['sequence']),'read marker did not reach both authenticated devices')
        status,result=await transport.request(config['conversations_url'],sessions[1])
        require(status==200,'actual unread conversation reconciliation failed')
        own=next(row for row in result['conversations'] if row['id']==ack['conversation_id'])
        require(own['unread_count']==0,'read marker did not reconcile unread to zero')
        transport.record({'kind':'cross_device_read_marker','message_id':ack['id'],'devices':2,'unread_count':own['unread_count']})
        transport.record({'kind':'three_wallet_acl','message_id':ack['id'],'rest_denials':3,'socket_private_payloads':0})
        return {'conversation_id':ack['conversation_id'],'message_id':ack['id'],'sender_wallet':sessions[0]['wallet'],'recipient_wallet':sessions[1]['wallet']}
    finally:
        await asyncio.gather(*(socket.close() for socket in sockets),return_exceptions=True)


async def browser_probe(config, output, privacy, ledger, transport):
    from playwright.async_api import async_playwright
    browser_config = config['browser']
    executable = Path(browser_config['executable'])
    require(digest(executable) == browser_config['executable_sha256'],'actual browser executable digest mismatch')
    results = {}
    callback_errors=[]
    browser_pending=set()
    require(browser_config['wallet']==privacy['sender_wallet'],'actual browser wallet must match admitted sender profile')
    require(not any(argument in ('--ignore-certificate-errors','--disable-web-security') for argument in browser_config['args']),'browser TLS/security checks cannot be disabled')
    def observe_socket(socket):
        if urlsplit(socket.url).path != urlsplit(config['socket_url']).path: return
        def sent(payload):
            try:
                frame=json.loads(payload)
                if frame.get('type') in ('dm','pool_message'):
                    key=frame['client_message_id'];transport.account('writes')
                    ledger.attempt(key,browser_config['wallet'],time.monotonic_ns(),config['workload']['maximum_pending_sends'],kind='dm')
                    browser_pending.add(key);transport.record({'kind':'browser_send','client_id':key})
            except Exception: callback_errors.append('browser send capture failed')
        def received(payload):
            try:
                transport.account('frames');frame=json.loads(payload)
                if frame.get('type')=='message_persisted' and frame.get('client_message_id') in browser_pending:
                    key=frame['client_message_id'];position(frame['sequence']);position(frame['revision'])
                    ledger.db.execute('UPDATE attempts SET ack_id=?,conversation=?,sequence=?,revision=? WHERE client_id=?',(frame['id'],frame['conversation_id'],frame['sequence'],frame['revision'],key))
                    ledger.sample('commit',ledger.start(key),time.monotonic_ns());browser_pending.remove(key)
                    transport.record({'kind':'browser_ack','client_id':key,'id':frame['id'],'sequence':frame['sequence']})
            except Exception: callback_errors.append('browser frame capture failed')
        socket.on('framesent',sent);socket.on('framereceived',received)
    async with async_playwright() as playwright:
        context = await playwright.chromium.launch_persistent_context(browser_config['profile_directory'],executable_path=str(executable),headless=browser_config['headless'],args=browser_config['args'])
        try:
            page = await context.new_page()
            page.on('websocket',observe_socket)
            await page.goto(browser_config['conversation_url_template'].format(conversation_id=privacy['conversation_id']),wait_until='domcontentloaded')
            composer = page.get_by_role('textbox',name='Message',exact=True)
            await composer.wait_for(state='visible',timeout=browser_config['timeout_ms'])
            await page.wait_for_function("() => {const input=document.getElementById('dm-composer');return input && !input.disabled}",timeout=browser_config['timeout_ms'])
            live = page.locator('[role="status"][aria-live="polite"]')
            require(await live.count() > 0,'delivery status lacks a polite live region')
            await composer.focus()
            draft='browser capacity '+str(uuid.uuid4())
            await composer.fill(draft)
            await context.set_offline(True)
            await page.wait_for_function("() => document.getElementById('dm-composer').disabled",timeout=browser_config['timeout_ms'])
            require(await composer.input_value() != '', 'offline draft was lost')
            await page.screenshot(path=str(output/'browser-offline.png'),full_page=True)
            await context.set_offline(False)
            await page.wait_for_function("() => !document.getElementById('dm-composer').disabled",timeout=browser_config['timeout_ms'])
            await page.evaluate("""draft => {window.__chatObservedStates=[];new MutationObserver(()=>{for(const article of document.querySelectorAll('article')){if(!article.textContent.includes(draft))continue;for(const element of article.querySelectorAll('span')){const value=element.textContent;if(value==='Sending…'||value==='Saved')window.__chatObservedStates.push(value)}}}).observe(document.body,{childList:true,subtree:true,characterData:true})}""",draft)
            await page.get_by_role('button',name='Send',exact=True).click()
            await page.wait_for_function("() => window.__chatObservedStates.includes('Saved')",timeout=browser_config['timeout_ms'])
            require(not callback_errors and not browser_pending,'actual browser acknowledgement capture failed')
            states = await page.evaluate('window.__chatObservedStates')
            require('Sending…' in states,'browser never displayed actual pending send before persisted acknowledgement')
            await page.screenshot(path=str(output/'browser-persisted.png'),full_page=True)
            results['send_states']=list(dict.fromkeys(states))
            await page.goto(browser_config['pool_url'],wait_until='domcontentloaded')
            log = page.get_by_role('log',name='Pool chat')
            await log.wait_for(state='visible',timeout=browser_config['timeout_ms'])
            await page.get_by_role('button',name='Earlier messages',exact=True).wait_for(state='visible',timeout=browser_config['timeout_ms'])
            rendered = await log.get_by_role('article').count()
            require(0 < rendered <= 200,'history virtualization exceeds200 rendered messages')
            earlier=page.get_by_role('button',name='Earlier messages',exact=True)
            await earlier.focus();await page.keyboard.press('Enter')
            require(await log.get_by_role('article').count() <= 200,'keyboard history pagination exceeded render bound')
            await page.screenshot(path=str(output/'browser-history.png'),full_page=True)
            results.update({'rendered_messages':rendered,'keyboard_history_navigation':True,'offline_send_disabled':True,'polite_delivery_region':True})
        finally:
            await context.close()
    return results


async def run(config, output):
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    report = {'task': '4.36', 'scope': 'lane_measurement_only', 'final_shared_acceptance_deferred_to': '9.1', 'started_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'passed': False}
    try:
        identities = validate(config)
        report.update({'source_files': config['source_files'], 'interface_sha256': config['interface_sha256'], 'runtime_identities': identities, 'allocation': config['allocation'], 'profile': config['profile']})
        ledger = Ledger(output / 'observations.sqlite')
        with (output / 'transport.ndjson').open('x') as raw:
            transport = Transport(config, raw)
            report['privacy'] = await privacy_probe(config,ledger,transport)
            report['workload'] = await workload(config, ledger, transport, identities)
            report['browser'] = await browser_probe(config,output,report['privacy'],ledger,transport)
        with (output / 'reconciliation.ndjson').open('x') as raw:
            report['reconciliation'] = reconcile(config, ledger, raw)
        report['latency_definitions']={'commit':'generator send to durable persisted acknowledgement, including transport, policy and transaction','fanout':'generator send to actual recipient application-frame observation','replay':'complete ordered retained history pagination per reconnect','handshake':'ticket issuance, accepted websocket session and authorized room join'}
        report['latencies_ms'] = {kind: ledger.percentiles(kind) for kind in ('commit', 'fanout', 'replay', 'handshake')}
        for kind in ('commit', 'fanout', 'replay'):
            require(report['latencies_ms'][kind]['p99'] <= config['limits_ms'][kind + '_p99'], 'measured ' + kind + ' p99 exceeds admitted limit')
        ledger.db.commit(); ledger.db.close()
        report['passed'] = True
    except BaseException as error:
        report['error_type'] = type(error).__name__
        report['error'] = str(error) if isinstance(error, RuntimeError) else 'actual input/provider/workload failure'
        raise
    finally:
        report['finished_at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        if 'ledger' in locals():
            with contextlib.suppress(Exception): ledger.db.commit();ledger.db.close()
        report['artifacts'] = {path.name: digest(path) for path in output.iterdir() if path.is_file()}
        (output / 'manifest.json').write_text(json.dumps(report, indent=2) + '\n')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    require(args.config.stat().st_mode & 0o077 == 0, 'session credential configuration must be private')
    config = json.loads(args.config.read_text())
    asyncio.run(run(config, args.output))


if __name__ == '__main__':
    main()
