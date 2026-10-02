#!/usr/bin/env python3
import argparse
import base64
import concurrent.futures
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import signal
import shutil
import socket
import stat
import subprocess
import sys
import threading
import time
from urllib.parse import parse_qs, quote, urlsplit

ROOT = Path(__file__).resolve().parents[3]
ASSETS = ('SID', 'PAX', 'USDC', 'USDL')
SCHEMES = ('metered', 'exact')
BOUNDARIES = ('after-settlement', 'during-compute', 'after-persistence', 'before-response-ack')
DOMAIN = b'PAXEERX_WEB_CONTENT_V1'
MAX_BYTES = 16 * 1024 * 1024


class Refusal(Exception):
    pass


def require(ok, message):
    if not ok:
        raise Refusal(message)


def private(path, directory=False):
    path = Path(path)
    require(path.is_absolute() and not path.is_symlink() and path.name != '.env'
            and not path.name.startswith('.env.'), 'private absolute non-environment path required')
    info = path.stat()
    require(info.st_uid == os.geteuid() and info.st_mode & 0o077 == 0,
            'private material ownership or permissions')
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode),
            'private material type')
    return path


def load(path):
    path = private(path)
    require(path.stat().st_size <= MAX_BYTES, 'input exceeds bound')
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate JSON key')
            result[key] = value
        return result
    return json.loads(path.read_bytes(), object_pairs_hook=pairs)


def digest(path):
    value = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for part in iter(lambda: stream.read(1024 * 1024), b''):
            value.update(part)
    return value.hexdigest()


def run(args):
    return subprocess.run(args, cwd=ROOT, check=True, capture_output=True, timeout=60).stdout


def source_identity():
    revision = run(['git', 'rev-parse', 'HEAD']).decode().strip()
    files = run(['git', 'ls-files', '-z']).decode().split('\0')
    files = sorted(name for name in files if name and
                   (name.endswith(('.rs', '.c', '.h', '.cpp', '.proto')) or
                    Path(name).name in ('Cargo.toml', 'Cargo.lock', 'build.rs')))
    require(files, 'compiler source inventory absent')
    value = hashlib.sha256()
    for name in files:
        path = ROOT / name
        require(path.is_file() and not path.is_symlink(), 'compiler source missing or symlink')
        value.update(name.encode() + b'\0' + digest(path).encode() + b'\n')
    return revision, value.hexdigest()


def artifact(row, revision, source):
    path = Path(row['path'])
    require(path.is_absolute() and path.is_file() and not path.is_symlink()
            and os.access(path, os.X_OK), 'candidate executable absent')
    require(row['source_revision'] == revision and row['source_digest'] == source
            and row['build_exit'] == 0 and digest(path) == row['sha256'],
            'candidate build/source binding mismatch')
    return path


def endpoint(value):
    url = urlsplit(value)
    require(url.scheme == 'http' and url.hostname == '127.0.0.1' and url.port
            and not url.username and not url.password and not url.query and not url.fragment,
            'only isolated IPv4 loopback HTTP endpoints supported')
    return url


def exchange(url, target, headers=None, body=None):
    conn = http.client.HTTPConnection(url.hostname, url.port, timeout=25)
    try:
        conn.request('POST' if body is not None else 'GET', target, body=body, headers=headers or {})
        response = conn.getresponse()
        data = response.read(MAX_BYTES + 1)
        require(len(data) <= MAX_BYTES, 'response exceeds bound')
        pairs = response.getheaders()
        require(len([v for k, v in pairs if k.lower() == 'payment-response']) <= 1,
                'duplicate settlement response')
        return response.status, {k.lower(): v for k, v in pairs}, data
    finally:
        conn.close()


def rpc(url, method, params):
    body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params})
    status, _, raw = exchange(url, url.path or '/', {'Content-Type': 'application/json'}, body)
    require(status == 200, 'real gateway read failed')
    response = json.loads(raw)
    require(response.get('id') == 1 and 'result' in response and 'error' not in response,
            'real gateway evidence unavailable')
    return response['result']


def canonical(body, route):
    data = json.loads(body)
    if route == 'search':
        require(data['results'], 'real index returned no qualifying search results')
        ordered = [{key: row[key] for key in ('url', 'title', 'snippet')} for row in data['results']]
        text = json.dumps(ordered, ensure_ascii=False, separators=(',', ':')).encode()
        payload, kind = data['query'].encode(), 2
    else:
        text, payload, kind = data['text'].encode(), data['url'].encode(), 1
        require(data['length'] == len(text), 'fetch byte length')
    media = data['media_type'].encode()
    raw = DOMAIN + bytes([kind]) + len(payload).to_bytes(4, 'big') + payload
    raw += len(media).to_bytes(4, 'big') + media + len(text).to_bytes(8, 'big') + text
    from Crypto.Hash import keccak
    value = keccak.new(digest_bits=256, data=raw).hexdigest()
    require(value == data['digest'], 'canonical content digest mismatch')
    return value, raw


class Candidate:
    def __init__(self, harness):
        self.h = harness
        self.process = None
        self.pid = None
        self.log = None
        self.stopped = threading.Event()
        self.condition = threading.Condition()
        self.replies = {}
        self.serial = 0
        self.frame = ''
        self.reader = None
        self.location = None

    def command(self, command):
        self.serial += 1
        token = str(self.serial)
        self.process.stdin.write(token + command + '\n')
        self.process.stdin.flush()
        with self.condition:
            require(self.condition.wait_for(lambda: token in self.replies, timeout=10),
                    'debugger command timeout')
            result = self.replies.pop(token)
        require('^error' not in result, 'source breakpoint/debugger command refused')
        return result

    def read_debugger(self):
        for line in self.process.stdout:
            self.log.write(line)
            self.log.flush()
            with self.condition:
                match = re.match(r'(\d+)\^', line)
                if match:
                    self.replies[match[1]] = line
                    self.condition.notify_all()
                match = re.search(r'=thread-group-started,.*pid="(\d+)"', line)
                if match:
                    self.pid = int(match[1])
                if line.startswith('*stopped,reason="breakpoint-hit"'):
                    self.frame = line
                    self.stopped.set()

    def start(self, boundary=None):
        self.stopped.clear()
        args = [str(self.h.binary), '--config', str(self.h.config_path)]
        self.log = (self.h.evidence / ('process-' + str(self.h.launches) + '.log')).open('w')
        self.h.launches += 1
        env = {'PATH': '/usr/local/bin:/usr/bin:/bin', 'RUST_BACKTRACE': '0'}
        if boundary:
            self.process = subprocess.Popen(['gdb', '--quiet', '--nx', '--interpreter=mi2', '--args'] + args,
                cwd=self.h.isolated, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT, text=True, start_new_session=True)
            self.reader = threading.Thread(target=self.read_debugger, daemon=True)
            self.reader.start()
            self.command('-gdb-set pagination off')
            self.command('-gdb-set breakpoint pending off')
            location = self.h.breakpoint(boundary)
            self.location = location
            reply = self.command('-break-insert ' + json.dumps(location))
            require('addr="<PENDING>"' not in reply, 'unresolved crash boundary')
            self.command('-exec-run')
        else:
            self.process = subprocess.Popen(args, cwd=self.h.isolated, env=env,
                stdout=self.log, stderr=subprocess.STDOUT, start_new_session=True)
            self.pid = self.process.pid
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            require(self.process.poll() is None, 'candidate exited before listening')
            if self.pid and self.listening():
                require(Path('/proc/' + str(self.pid) + '/exe').resolve() == self.h.binary.resolve(),
                        'served executable identity mismatch')
                return
            time.sleep(.05)
        raise Refusal('candidate did not listen')

    def listening(self):
        fds = Path('/proc/' + str(self.pid) + '/fd')
        try:
            sockets = {os.readlink(p) for p in fds.iterdir()}
            for line in Path('/proc/net/tcp').read_text().splitlines()[1:]:
                fields = line.split()
                if fields[1] == '0100007F:' + format(self.h.url.port, '04X') and fields[3] == '0A':
                    return 'socket:[' + fields[9] + ']' in sockets
        except (FileNotFoundError, ProcessLookupError):
            return False
        return False

    def stop(self, crash=False):
        if self.pid and Path('/proc/' + str(self.pid)).exists():
            require(Path('/proc/' + str(self.pid) + '/exe').resolve() == self.h.binary.resolve(),
                    'refuse signalling a foreign process')
            os.kill(self.pid, signal.SIGKILL if crash else signal.SIGTERM)
        if self.process:
            if self.reader and self.process.poll() is None:
                self.process.terminate()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
        if self.reader:
            self.reader.join(timeout=5)
            require(not self.reader.is_alive(), 'debugger reader did not stop')
            self.reader = None
        self.pid = None
        self.process = None
        if self.log:
            self.log.close()


class Harness:
    def __init__(self, manifest):
        self.m = load(manifest)
        require(self.m['schema'] == 'paxeer-x-paid-resource-delivery-v1', 'unsupported manifest schema')
        self.revision, source = source_identity()
        require(self.m['source_revision'] == self.revision and self.m['source_digest'] == source,
                'manifest source identity mismatch')
        self.binary = artifact(self.m['artifacts']['websearch'], self.revision, source)
        self.isolated = private(self.m['isolated_root'], True).resolve()
        self.evidence = private(self.m['evidence_dir'], True).resolve()
        require(self.evidence.is_relative_to(self.isolated), 'evidence must remain in isolated root')
        self.config_path = private(self.m['config']['path'])
        require(digest(self.config_path) == self.m['config']['sha256'], 'configuration binding mismatch')
        self.config = load(self.config_path)
        self.data = Path(self.config['data_dir']).resolve()
        require(self.data.is_relative_to(self.isolated) and self.data != self.isolated,
                'candidate data directory escapes isolation')
        private(self.data, True)
        require(not (self.data / 'payments').exists(), 'payment state must be fresh before the gate')
        self.url = endpoint(self.m['endpoint'])
        require(self.config['listen'] == '127.0.0.1:' + str(self.url.port), 'listen/config mismatch')
        self.gateway = endpoint(self.config['gateway']['endpoint'])
        self.runtime = self.m['gateway_process']
        gateway_binary = artifact(self.runtime['artifact'], self.revision, source)
        pid = int(self.runtime['pid'])
        require(Path('/proc/' + str(pid) + '/exe').resolve() == gateway_binary.resolve()
                and Path('/proc/' + str(pid) + '/cwd').resolve().is_relative_to(self.isolated),
                'real isolated gateway process binding absent')
        require(self.runtime['endpoint'] == self.config['gateway']['endpoint'], 'gateway endpoint mismatch')
        require(self.m['sequence_probes'] and all(isinstance(row['params'], list)
                for row in self.m['sequence_probes']), 'real charge observation probes missing')
        self.cases = self.m['cases']
        expected = {f'{route}-{asset}-{scheme}' for route in ('search', 'fetch')
                    for asset in ASSETS for scheme in SCHEMES}
        expected |= set(BOUNDARIES) | {'store-recovery-search', 'store-recovery-fetch',
                                     'foreign-receipt', 'pending-payment'}
        require(set(self.cases) == expected, 'required case materials missing or unsupported')
        require(shutil.which('gdb'), 'GDB with source breakpoint support is required')
        require(set(self.m['boundaries']) == set(BOUNDARIES), 'all crash boundary inputs are required')
        for boundary in BOUNDARIES:
            self.breakpoint(boundary)
        self.headers = {}
        for name, row in self.cases.items():
            require(row['route'] in ('search', 'fetch') and row['asset'] in ASSETS
                    and row['scheme'] in SCHEMES, 'unsupported request case')
            path = private(row['payment_signature_file'])
            header = path.read_text().strip()
            require(header and '\r' not in header and '\n' not in header, 'invalid private payment header')
            self.headers[name] = header
            require(row['target'].startswith('/' + row['route'] + '?'), 'route/target mismatch')
            require(row['amount'] == self.config['assets'][row['asset']]['price'], 'base-unit price mismatch')
            require(row['asset_id'] == self.config['assets'][row['asset']]['asset_id'], 'asset identity mismatch')
            require(re.fullmatch(r'[1-9][0-9]*', row['amount']) and int(row['amount']) < 2**128,
                    'noncanonical base-unit amount')
        require(len(set(self.headers.values())) == len(self.headers), 'cases require independent real payments')
        require(self.cases['during-compute']['route'] == 'search', 'compute crash requires real search execution')
        require(self.cases['foreign-receipt']['scheme'] == 'exact', 'foreign receipt requires exact evidence')
        from Crypto.Hash import keccak
        require(keccak.new(digest_bits=256).digest_size == 32, 'Keccak unavailable')
        self.launches = 0
        self.count = 0
        self.candidate = Candidate(self)
        self.journal = (self.evidence / 'assertions.jsonl').open('x')

    def record(self, name):
        self.count += 1
        self.journal.write(json.dumps({'revision': self.revision, 'case': name, 'passed': True}) + '\n')
        self.journal.flush()
        os.fsync(self.journal.fileno())
        print('PAXEER_X_PROGRESS cases=' + str(self.count), flush=True)

    def sequences(self):
        values = []
        for probe in self.m['sequence_probes']:
            result = rpc(self.gateway, 'lx_getSequence', probe['params'])
            value = result['next_sequence']
            require(isinstance(value, str) and re.fullmatch(r'0|[1-9][0-9]*', value),
                    'noncanonical real sequence observation')
            values.append(int(value))
        return values

    def snapshot(self):
        payments = self.data / 'payments'
        return {str(p.relative_to(payments)): digest(p) for p in payments.rglob('*') if p.is_file()}

    def request(self, name, target=None, header=None):
        case = self.cases[name]
        return exchange(self.url, target or case['target'], {
            'PAYMENT-SIGNATURE': header or self.headers[name], 'LAYERX-PAYER-DID': case['payer_did']})

    def records(self):
        return {p.name: load(p) for p in (self.data / 'payments/requests').iterdir()
                if re.fullmatch('[0-9a-f]{64}', p.name)}

    def association(self, receipt):
        matches = [(key, row) for key, row in self.records().items() if row['receipt_digest'] == receipt]
        require(len(matches) == 1, 'receipt must bind exactly one payment')
        key, payment = matches[0]
        require((self.data / 'payments/receipts' / receipt).read_text() == key,
                'receipt claim owner mismatch')
        delivery = load(self.data / 'payments/deliveries' / key)
        require(delivery['receipt_digest'] == receipt and delivery['version'] == 1,
                'durable delivery receipt mismatch')
        for field in ('principal', 'request_digest', 'resource'):
            require(delivery[field] == payment[field], 'canonical identity association mismatch')
        return payment, delivery

    def success(self, name, response):
        status, headers, body = response
        require(status == 200 and 'payment-response' in headers, 'paid content unavailable')
        settlement = json.loads(base64.b64decode(headers['payment-response'], validate=True))
        case = self.cases[name]
        layerx = settlement['extensions']['layerx']
        receipt = layerx['receiptDigest']
        require(settlement['success'] is True and settlement['amount'] == case['amount']
                and layerx['verificationLevel'] == 'sequencer-signed'
                and settlement['transaction'] == 'lxp:' + receipt, 'settlement evidence mismatch')
        payment, delivery = self.association(receipt)
        parameter = 'q' if case['route'] == 'search' else 'url'
        query = parse_qs(urlsplit(case['target']).query, strict_parsing=True)[parameter]
        require(len(query) == 1, 'canonical request parameter count')
        resource = 'GET /' + case['route'] + '?' + parameter + '=' + quote(query[0], safe='-._~')
        require(payment['resource'] == resource, 'receipt bound to different canonical request')
        web = settlement['extensions']['x-websearch']
        require(web['resource'] == resource and web['receiptDigest'] == receipt
                and web['delivery'] == 'ready' and web['responseDigest'] == hashlib.sha256(body).hexdigest(),
                'PAYMENT-RESPONSE result association mismatch')
        require(payment['offer']['scheme'] == case['scheme'] and payment['offer']['asset'] == case['asset_id']
                and payment['offer']['amount'] == case['amount'], 'scheme or base units changed')
        canonical_receipt = base64.b64decode(layerx['receipt'], validate=True)
        require(canonical_receipt.hex() == payment['receipt'], 'response receipt differs from verified persistence')
        evidence = rpc(self.gateway, 'lx_getReceipt', [payment['activity_id']])
        require(evidence['receipt'] == payment['receipt'] and evidence['activity_id'] == payment['activity_id']
                and evidence.get('state', 'completed') == 'completed', 'real executed receipt unavailable')
        require(delivery['state'] == 'ready' and bytes(delivery['response']['body']) == body
                and delivery['response']['body_digest'] == hashlib.sha256(body).hexdigest(),
                'persisted result differs from delivered result')
        expected_digest = 'sha-256=:' + base64.b64encode(hashlib.sha256(body).digest()).decode() + ':'
        require(headers.get('content-digest') == expected_digest, 'HTTP content digest mismatch')
        content_digest, raw = canonical(body, case['route'])
        require(web['contentDigest'] == content_digest
                and delivery['response']['canonical_digest'] == content_digest
                and bytes(delivery['response']['canonical_content']) == raw,
                'receipt extension differs from persisted canonical content')
        require((self.data / 'content' / content_digest).read_bytes() == raw, 'canonical content persistence mismatch')
        code, _, fetched = exchange(self.url, '/content/' + content_digest)
        require(code == 200 and fetched == raw, 'served canonical content mismatch')
        return body

    def no_content(self, response, allowed, allow_settled=False):
        code, headers, body = response
        require(code in allowed, 'refusal HTTP status mismatch')
        value = json.loads(body)
        require('error' in value and not any(key in value for key in ('results', 'text', 'digest')),
                'refused request released content')
        if 'payment-response' in headers:
            value = json.loads(base64.b64decode(headers['payment-response'], validate=True))
            require(allow_settled or value.get('success') is not True, 'refusal advertised successful delivery')

    def breakpoint(self, boundary):
        row = self.m['boundaries'][boundary]
        source = ROOT / row['file']
        allowed = {'after-settlement': ('payment.rs', 'self.deliver('),
                   'during-compute': ('search.rs', 'let hits = searcher.search(&query,'),
                   'after-persistence': ('payment.rs', 'delivery.response.as_ref().ok_or(io::ErrorKind::InvalidData)?.response()'),
                   'before-response-ack': ('server.rs', '.write_all(&bytes)')}
        filename, needle = allowed[boundary]
        require(row['file'] == 'interop/crates/x-websearch/src/' + filename,
                'boundary source file mismatch')
        lines = source.read_text().splitlines()
        line = int(row['line'])
        require(0 < line <= len(lines) and needle in lines[line - 1]
                and digest(source) == row['sha256'], 'boundary source anchor mismatch')
        if boundary == 'after-persistence':
            require('self.store.save_json(DELIVERIES, key, &delivery)?;' in lines[line - 2],
                    'after-persistence breakpoint is not after durable result write')
        return str(source) + ':' + str(line)

    def run(self):
        self.candidate.start()
        valid = 'search-SID-metered'
        before, state = self.sequences(), self.snapshot()
        status, headers, _ = exchange(self.url, self.cases[valid]['target'],
            {'LAYERX-PAYER-DID': self.cases[valid]['payer_did']})
        require(status == 402, 'unpaid request did not challenge')
        required = json.loads(base64.b64decode(headers['payment-required'], validate=True))
        offers = required['accepts']
        expected = {(scheme, row['asset_id'], row['price'])
                    for row in self.config['assets'].values() for scheme in SCHEMES}
        require(len(offers) == 8 and {(row['scheme'], row['asset'], row['amount']) for row in offers} == expected,
                'exact/metered or base-unit offers changed')
        require(self.sequences() == before and self.snapshot() == state, 'unpaid challenge changed payment state')
        self.record('unpaid-eight-offers')
        invalid = ['/search', '/search?q=', '/search?q=a&q=b', '/search?q=%GG',
                   '/search?q=%FF', '/search?q=a&', '/search?q=a&&', '/search?q=a&%71=b',
                   '/search?q=a&other=%GG', '/search?q=a&unexpected=b', '/search?q=' + '+'.join('term' + str(n) for n in range(33)), '/search?q=' + 'a' * 513, '/search?q=%21%21',
                   '/fetch', '/fetch?url=', '/fetch?url=https%3A%2F%2Fexample.invalid%2F&',
                   '/fetch?url=a&%75rl=b', '/fetch?url=a&other=%GG', '/fetch?url=a&url=b', '/fetch?url=%GG',
                   '/fetch?url=file%3A%2F%2Fetc%2Fpasswd', '/fetch?url=not-a-url']
        for target in invalid:
            before, state = self.sequences(), self.snapshot()
            self.no_content(self.request(valid, target), {400, 403, 422})
            require(self.sequences() == before and self.snapshot() == state,
                    'invalid syntax charged or consumed receipt')
            self.record('syntax-' + str(self.count))
        for route in ('search', 'fetch'):
            for asset in ASSETS:
                for scheme in SCHEMES:
                    name = f'{route}-{asset}-{scheme}'
                    first = self.request(name)
                    body = self.success(name, first)
                    after = self.sequences()
                    require(self.success(name, self.request(name)) == body and self.sequences() == after,
                            'same request retry changed content or charged again')
                    changed = '/search?q=foreign-resource' if route == 'fetch' else '/fetch?url=https%3A%2F%2Fexample.invalid%2F'
                    self.no_content(self.request(name, changed), {400, 402, 403, 409, 503})
                    require(self.sequences() == after, 'changed resource caused another charge')
                    self.candidate.stop()
                    self.candidate.start()
                    self.no_content(self.request(name, changed), {400, 402, 403, 409, 503})
                    require(self.sequences() == after, 'changed resource after restart charged')
                    require(self.success(name, self.request(name)) == body and self.sequences() == after,
                            'restart retry changed content or charged again')
                    self.record(name)
        for name in ('foreign-receipt', 'pending-payment'):
            case = self.cases[name]
            if name == 'pending-payment':
                state = rpc(self.gateway, 'lx_getActivityStatus', [case['activity_id']])
                require(state['state'] == 'pending' and state.get('receipt') is None,
                        'pending case lacks genuine pending gateway activity')
            else:
                payload = json.loads(base64.b64decode(self.headers[name], validate=True))
                receipt = base64.b64decode(payload['payload']['receipt'], validate=True).hex()
                state = rpc(self.gateway, 'lx_getReceipt', [case['activity_id']])
                require(state['receipt'] == receipt, 'foreign receipt is not genuine gateway evidence')
            before = self.sequences()
            self.no_content(self.request(name), {400, 402, 403, 409, 503})
            require(self.sequences() == before, 'unverified or foreign receipt caused charge')
            self.record(name)
        for route in ('search', 'fetch'):
            name = 'store-recovery-' + route
            directory = self.data / 'content'
            held = self.data / 'content.qualification-held'
            require(not held.exists(), 'fault backup path occupied')
            before_records = set(self.records())
            directory.rename(held)
            try:
                response = self.request(name)
                self.no_content(response, {500, 503}, allow_settled=True)
                new = set(self.records()) - before_records
                require(len(new) == 1, 'handler failure payment association absent')
                payment = self.records()[new.pop()]
                _, delivery = self.association(payment['receipt_digest'])
                settlement = json.loads(base64.b64decode(response[1]['payment-response'], validate=True))
                binding = settlement['extensions']['x-websearch']
                require(binding['delivery'] == 'failed' and binding['receiptDigest'] == payment['receipt_digest']
                        and binding['contentDigest'] is None
                        and binding['responseDigest'] == hashlib.sha256(response[2]).hexdigest(),
                        'failed delivery extension lost paid association')
                require(delivery['state'] == 'failed'
                        and bytes(delivery['response']['body']) == response[2],
                        'handler failure lost recoverable entitlement')
                after = self.sequences()
            finally:
                held.rename(directory)
            self.candidate.stop()
            self.candidate.start()
            self.success(name, self.request(name))
            require(self.sequences() == after, 'failed handler retry charged twice')
            self.record(name)
        self.candidate.stop()
        for boundary in BOUNDARIES:
            before_records = set(self.records())
            self.candidate.start(boundary)
            with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
                request = executor.submit(self.request, boundary)
                require(self.candidate.stopped.wait(30), 'required production crash boundary was not executed')
                source, line = self.candidate.location.rsplit(':', 1)
                frame = self.candidate.frame
                require('fullname=' + json.dumps(source) in frame and 'line=' + json.dumps(line) in frame
                        and 'thread-id=' in frame and 'bkptno=' in frame,
                        'debugger stopped at a different source line or thread')
                new = set(self.records()) - before_records
                require(len(new) == 1, 'crash boundary lacks exactly one paid association')
                payment = self.records()[new.pop()]
                require(payment['receipt'] and payment['receipt_digest'], 'crash occurred before verified settlement')
                if boundary != 'after-settlement':
                    _, delivery = self.association(payment['receipt_digest'])
                    expected = 'computing' if boundary == 'during-compute' else 'ready'
                    require(delivery['state'] == expected, 'crash observed at wrong durable state')
                after = self.sequences()
                self.candidate.stop(crash=True)
                try:
                    response = request.result(timeout=30)
                    require(response[0] != 200, 'crash boundary already acknowledged success')
                except (OSError, http.client.HTTPException):
                    pass
            if boundary == 'after-settlement':
                directory = self.data / 'index'
                held = self.data / 'index.qualification-held'
                require(directory.is_dir() and not held.exists(), 'isolated index fault path unavailable')
                directory.rename(held)
                try:
                    with directory.open('x') as block:
                        block.write('qualification-owned unavailable index\n')
                    with (self.evidence / 'index-unavailable.log').open('w') as log:
                        failed = subprocess.run([str(self.binary), '--config', str(self.config_path)],
                            cwd=self.isolated, env={'PATH': '/usr/local/bin:/usr/bin:/bin'},
                            stdout=log, stderr=subprocess.STDOUT, timeout=30)
                    require(failed.returncode == 2, 'unavailable index did not refuse startup with exit 2')
                    with socket.socket() as probe:
                        probe.settimeout(.5)
                        require(probe.connect_ex(('127.0.0.1', self.url.port)) != 0,
                                'unavailable index candidate still serves requests')
                    _, unavailable = self.association(payment['receipt_digest'])
                    require(unavailable['state'] == 'pending' and self.sequences() == after,
                            'unavailable index lost or recharged entitlement')
                finally:
                    if directory.exists() or directory.is_symlink():
                        require(directory.is_file() and not directory.is_symlink()
                                and directory.read_bytes() == b'qualification-owned unavailable index\n',
                                'index fault ownership changed')
                        directory.unlink()
                    held.rename(directory)
                self.record('index-unavailable-recovery')
            self.candidate.start()
            _, recovered = self.association(payment['receipt_digest'])
            require(recovered['state'] in ('pending', 'ready'), 'restart did not expose recoverable delivery')
            self.success(boundary, self.request(boundary))
            require(self.sequences() == after, 'crash recovery made a second charge')
            self.record(boundary)
            self.candidate.stop()
        print('PAXEER_X_GATE tests=' + str(self.count) + ' skipped=0', flush=True)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=['paid-resource-delivery', 'evm-attestation-recovery'])
    parser.add_argument('--candidate-manifest', required=True)
    args = parser.parse_args()
    if args.case == 'evm-attestation-recovery':
        import importlib.util
        module_path = Path(__file__).resolve().parent / 'fixtures' / 'xweb_attestation.py'
        spec = importlib.util.spec_from_file_location('xweb_attestation', module_path)
        if spec is None or spec.loader is None:
            raise RuntimeError('attestation verifier module unavailable')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module.main()
    harness = None
    try:
        require(bool(args.candidate_manifest), 'candidate manifest absent')
        harness = Harness(args.candidate_manifest)
        harness.run()
        return 0
    except (Refusal, OSError, ValueError, KeyError, TypeError, ImportError,
            subprocess.SubprocessError, http.client.HTTPException) as error:
        reason = str(error) if isinstance(error, Refusal) else type(error).__name__
        if harness is not None:
            with (harness.evidence / 'failure.json').open('x') as failure:
                json.dump({'revision': harness.revision, 'exit_code': 1,
                           'exception': type(error).__name__, 'assertion': reason}, failure)
                failure.flush()
                os.fsync(failure.fileno())
        print('PAXEER_X_GATE_REFUSED: ' + reason + '; inspect private evidence',
              file=sys.stderr, flush=True)
        return 1
    finally:
        if harness is not None:
            harness.candidate.stop()
            harness.journal.close()


if __name__ == '__main__':
    raise SystemExit(main())
