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


def rpc(url, method, params, headers=None):
    body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params})
    status, _, raw = exchange(url, url.path or '/', {'Content-Type': 'application/json', **(headers or {})}, body)
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
        env.update(getattr(self.h, "runtime_env", {}))
        if boundary:
            self.process = subprocess.Popen(['gdb', '--quiet', '--nx', '--interpreter=mi2', '--args'] + args,
                cwd=self.h.isolated, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT, text=True, start_new_session=True)
            self.reader = threading.Thread(target=self.read_debugger, daemon=True)
            self.reader.start()
            self.command('-gdb-set pagination off')
            if getattr(self.h, 'debugger_nonstop', False):
                self.command('-gdb-set non-stop on')
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


class KernelHarness:
    def __init__(self, manifest):
        import importlib.util
        from types import SimpleNamespace
        sys.dont_write_bytecode = True
        spec = importlib.util.spec_from_file_location('candidate_contract', ROOT / 'tools/paxeer-x/candidate.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.loader = module.load_private
        candidate = self.loader(manifest)
        require(candidate['schema'] == 'paxeer-x.candidate.v1', 'kernel gate requires candidate manifest')
        self.revision, source = source_identity()
        require(candidate['source']['revision'] == self.revision and not candidate['source']['dirty'],
                'candidate source revision mismatch')
        services = [row for row in candidate['services'] if row['id'] == 'search-web']
        require(len(services) == 1, 'one search-web service binding required')
        bindings = services[0]['bindings']
        self.m = self.reference(bindings['roles_ref'])
        self.accounts = self.reference(bindings['funded_accounts_ref'])
        require(self.m['schema'] == 'paxeer-x.kernel-web-recovery.v1'
                and self.accounts['schema'] == 'paxeer-x.kernel-web-accounts.v1',
                'kernel recovery consumer metadata absent')
        require(self.m['source_revision'] == self.revision and self.m['source_digest'] == source
                and self.accounts['source_revision'] == self.revision, 'kernel metadata source mismatch')
        self.isolated = private(self.m['isolated_root'], True).resolve()
        self.evidence = private(self.m['evidence_dir'], True).resolve()
        require(self.evidence.is_relative_to(self.isolated), 'kernel evidence isolation')
        self.gateway = endpoint(self.m['gateway']['endpoint'])
        self.authorization_file = private(self.m['caller_authorization_file'])
        self.binary = artifact(self.m['websearch'], self.revision, source)
        for role in ('gateway', 'kernel'):
            row = self.m[role]
            binary = artifact(row['artifact'], self.revision, source)
            proc = Path('/proc') / str(int(row['pid']))
            require((proc / 'exe').resolve() == binary.resolve()
                    and (proc / 'cwd').resolve().is_relative_to(self.isolated), 'real isolated runtime identity')
        gateway_owner = SimpleNamespace(pid=int(self.m['gateway']['pid']), h=SimpleNamespace(url=self.gateway))
        require(Candidate.listening(gateway_owner), 'gateway process does not own configured listener')
        self.processes = []
        self.configs = []
        self.count = 0
        self.journal = (self.evidence / 'kernel-assertions.jsonl').open('x')
        for ordinal, row in enumerate(self.m['relays']):
            config_path = private(row['config']['path'])
            require(digest(config_path) == row['config']['sha256'], 'relay configuration binding')
            config = self.loader(config_path)
            private(config['gateway']['authorization_file'])
            data = private(config['data_dir'], True).resolve()
            require(data.is_relative_to(self.isolated) and data != self.isolated
                    and not (data / 'kernel/kernel-relay.json').exists(), 'fresh isolated relay state required')
            url = endpoint(row['endpoint'])
            require(config['listen'] == '127.0.0.1:' + str(url.port), 'relay listener binding')
            require(endpoint(config['kernel']['endpoint']) == self.gateway
                    and endpoint(config['gateway']['endpoint']) == self.gateway, 'authenticated gateway route mismatch')
            env = {name: str(private(value)) for name, value in row['key_files'].items()}
            require(set(env) == {'X_WEBSEARCH_RECEIVER_KEY_FILE', 'X_WEBSEARCH_ATTESTOR_KEY_FILE'},
                    'separate receiver and attestor key references required')
            evidence = self.evidence / ('relay-' + str(ordinal))
            evidence.mkdir(mode=0o700)
            settings = SimpleNamespace(binary=self.binary, config_path=config_path, isolated=self.isolated,
                evidence=evidence, launches=0, url=url, runtime_env=env, debugger_nonstop=ordinal != 0, breakpoint=self.breakpoint)
            self.processes.append(Candidate(settings))
            self.configs.append(config)
        require(len({config['data_dir'] for config in self.configs}) == len(self.configs), 'relay data directories overlap')
        require(len(self.processes) >= 2 and shutil.which('gdb'), 'real peer quorum and GDB required')
        self.candidate = self.processes[0]
        self.primary = self.configs[0]
        self.state = Path(self.primary['data_dir']) / 'kernel/kernel-relay.json'
        self.network = self.primary['kernel_network_id']
        require(all(config['kernel_network_id'] == self.network for config in self.configs), 'relay network mismatch')
        self.program = bytes.fromhex(self.accounts['program_id'])
        self.request_id = int(self.accounts['request_id'])
        self.payload = bytes.fromhex(self.accounts['payload'])
        self.kind = int(self.accounts['kind'])
        self.fee = int(self.accounts['fee'])
        require(len(self.program) == 32 and self.payload and self.kind in (1, 2) and self.fee > 0,
                'paid request bindings absent')
        self.payouts = self.accounts['attestors']
        signers = [row['signer'] for row in self.payouts]
        require(signers == sorted(set(signers)) and len(signers) >= 2
                and self.accounts['threshold'] > len(signers) // 2, 'registered majority bindings invalid')
        self.balance_accounts = [self.accounts['program_account'], self.accounts['fee_account']]
        self.balance_accounts += [row['payout_account'] for row in self.payouts]
        require(len(set(self.balance_accounts)) == len(self.balance_accounts), 'fee accounts must be distinct')
        require(set(self.m['calls']) == {'request', 'read', 'wrong-fee', 'absent-read'},
                'reference program request/read/refusal inputs absent')
        self.calls = {name: bytes.fromhex(row) for name, row in self.m['calls'].items()}
        import struct
        for name, call in self.calls.items():
            require(len(call) >= 106 and call[:32] == self.program, 'real program call binding absent')
            fields = struct.unpack('>32sHHIHII7Q', call[:106])
            require(fields[1] == 4 and call[106:106 + fields[2]] == b'layerx_call', 'ABI4 reference call required')
            require(len(call) == 106 + sum(fields[2:6]), 'canonical program call lengths')
        request_input = self.call_input(self.calls['request'])
        expected = b'\1\1' + self.request_id.to_bytes(8, 'big') + bytes([self.kind])
        expected += bytes.fromhex(self.accounts['asset']) + bytes.fromhex(self.accounts['fee_account'])
        expected += self.fee.to_bytes(16, 'big') + self.payload
        require(request_input == expected, 'reference request does not pay bound fee')
        wrong_fee = self.call_input(self.calls['wrong-fee'])
        require(len(wrong_fee) == len(expected) and wrong_fee[:43] == expected[:43]
                and wrong_fee[43:75] != expected[43:75] and wrong_fee[75:] == expected[75:],
                'wrong-fee case must vary only the fee account')
        require(self.calls['absent-read'] == self.calls['read'], 'absent-read must exercise the same reference read')
        vector = ROOT / 'tests/fixtures/web/observation-activity.hex'
        require(digest(vector) == self.m['adapter_vector_sha256'], 'existing adapter vector binding missing')
        require(self.primary['kernel']['submitter_did'] != self.accounts['caller_did'], 'separate caller and relay sequence required')
        self.signing_key = private(self.m['caller_key_file'])
        self.caller = self.accounts['caller_did']
        wasm = self.m['reference_program']
        wasm_path = Path(wasm['path'])
        require(wasm_path.is_absolute() and wasm_path.is_file() and not wasm_path.is_symlink()
                and wasm['source_revision'] == self.revision and wasm['source_digest'] == source
                and wasm['build_exit'] == 0 and digest(wasm_path) == wasm['sha256'],
                'built reference program source binding absent')
        self.wasm = wasm_path.read_bytes()
        require(self.wasm.startswith(b'\0asm\1\0\0\0'), 'reference artifact is not wasm')
        self.deployment = bytes.fromhex(self.m['deployment_activity'])


    def headers(self):
        path = private(self.authorization_file)
        require(path.stat().st_nlink == 1 and path.stat().st_size <= 256, 'protected gateway credential bound')
        value = path.read_text().removesuffix('\n').removesuffix('\r')
        require(re.fullmatch(r'LayerX-Key [A-Za-z0-9_-]{1,64}:lxp_live_[0-9a-f]{64}', value),
                'configured gateway authorization format refused')
        return {'Authorization': value}

    def reference(self, value):
        require(isinstance(value, str) and value.startswith('private:/'), 'resolved private binding required')
        path, mark, fragment = value[len('private:'):].partition('#')
        record = self.loader(path)
        if mark:
            require(fragment and '/' not in fragment and fragment in record, 'binding fragment absent')
            record = record[fragment]
        require(isinstance(record, dict), 'binding metadata must be an object')
        return record

    def call_input(self, call):
        import struct
        fields = struct.unpack('>32sHHIHII7Q', call[:106])
        return call[106 + fields[2]:106 + fields[2] + fields[3]]

    def breakpoint(self, boundary):
        needles = {'attestation-held': 'let _ = self.exchange.collect(key, &self.set);', 'ready-to-sign': 'let Ok(signed) = self.submitter.sign(&observation, now_ms) else {',
                   'after-submit': 'let fresh = matches!(&answer, Some(RpcAnswer::Result(_)));'}
        source = ROOT / 'interop/crates/x-websearch/src/kernel.rs'
        lines = [i for i, line in enumerate(source.read_text().splitlines(), 1) if needles[boundary] in line]
        require(len(lines) == 1, 'exact production crash boundary absent')
        return str(source) + ':' + str(lines[0])

    def record(self, name):
        self.count += 1
        self.journal.write(json.dumps({'revision': self.revision, 'case': name, 'passed': True}) + '\n')
        self.journal.flush()
        os.fsync(self.journal.fileno())

    def entry(self):
        if not self.state.exists():
            return None
        journal = self.loader(self.state)
        require(journal['version'] == 'PAXEERX_KERNEL_RELAY_V1', 'production journal version')
        rows = [row for row in journal['entries'] if row['program_id'] == '0x' + self.program.hex()
                and row['request_id'] == self.request_id]
        require(len(rows) <= 1, 'duplicate durable request')
        if rows:
            require(rows[0]['sequence'] < journal['next_sequence'], 'cursor advanced past non-durable request')
            require(rows[0]['topic'] == b'PAXEERX_WEB_REQUEST_V1'.hex()
                    and rows[0]['payload'] == self.payload.hex(), 'durable request binding')
        return rows[0] if rows else None

    def wait_entry(self, condition):
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            row = self.entry()
            if row is not None and condition(row):
                return row
            require(self.candidate.process.poll() is None, 'relay exited during recovery')
            time.sleep(.05)
        raise Refusal('required production journal transition absent')

    def balances(self):
        values = []
        for account in self.balance_accounts:
            row = rpc(self.gateway, 'lx_getBalance', [account], self.headers())
            require(row['asset_id'] == self.accounts['asset'], 'fee account asset mismatch')
            values.append(int(row['balance']))
        return values

    def signed(self, activity_type, payload, key_file, did):
        import struct
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
        raw = private(key_file).read_bytes().strip()
        require(re.fullmatch(b'[0-9a-fA-F]{64}', raw), 'binary-compatible private signer key required')
        key = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(raw.decode()))
        public = key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
        require(did == 'did:layerx:' + public.hex(), 'signer/DID binding mismatch')
        blob = lambda value: len(value).to_bytes(4, 'big') + value
        sequence = int(rpc(self.gateway, 'lx_getSequence', [did, 'identity'], self.headers())['next_sequence'])
        now = time.time_ns() // 1000000
        context = (hashlib.sha256(b'LXP/v1/context-hash\0' + payload).digest()
                   if activity_type == 0x000B0001 else os.urandom(32))
        fields = (b'\1\0\3\2' + self.network.to_bytes(4, 'big') + b'\3' + activity_type.to_bytes(4, 'big')
                  + b'\4' + blob(did.encode()) + b'\5' + blob(public) + b'\6' + sequence.to_bytes(8, 'big')
                  + b'\7' + struct.pack('>QQ', now - 1000, now + 60000) + b'\10' + blob(context)
                  + b'\11' + int(self.m['activity_fee_limit']).to_bytes(16, 'big')
                  + b'\12' + blob(hashlib.sha256(b'LXP/v1/payload-hash\0' + payload).digest())
                  + b'\13' + blob(payload))
        unsigned = b'\0\3\20\1\13' + fields
        signature = key.sign(hashlib.sha256(b'LXP/v1/signature-preimage\0' + unsigned).digest())
        return b'\0\3\20\1\14' + fields + b'\14' + blob(signature)

    def receipt(self, activity, refused=False):
        sys.path.insert(0, str(ROOT / 'agent/sdk/python'))
        from layerx_sdk.verifier import _decode_protocol_receipt
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
        activity_id = hashlib.sha256(b'LXP/v1/activity-id\0' + activity).hexdigest()
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': 'lx_getReceipt', 'params': [activity_id]})
            status, _, raw = exchange(self.gateway, self.gateway.path or '/', {'Content-Type': 'application/json', **self.headers()}, body)
            require(status == 200, 'canonical receipt transport absent')
            row = json.loads(raw).get('result')
            if row and row.get('receipt'):
                require(row['activity_id'] == activity_id, 'foreign canonical receipt')
                canonical = bytes.fromhex(row['receipt'])
                facts, unsigned = _decode_protocol_receipt(canonical)
                key = bytes.fromhex(self.primary['gateway']['sequencer_public_key'])
                Ed25519PublicKey.from_public_bytes(key).verify(facts.sequencer_signature,
                    hashlib.sha256(b'LXP/v1/receipt\0' + unsigned).digest())
                require(facts.activity_id.hex() == activity_id and ((facts.result_code != 0) == refused),
                        'canonical committed outcome mismatch')
                return facts
            time.sleep(.05)
        raise Refusal('canonical committed receipt absent')

    def send(self, activity):
        body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': 'lx_sendActivity',
                           'params': [activity.hex(), 'executed']})
        status, _, raw = exchange(self.gateway, self.gateway.path or '/', {'Content-Type': 'application/json', **self.headers()}, body)
        require(status == 200 and json.loads(raw).get('id') == 1, 'real kernel submit transport refused')

    def program_call(self, name, refused=False):
        activity = self.signed(0x00090003, self.calls[name], self.signing_key, self.caller)
        self.send(activity)
        facts = self.receipt(activity, refused)
        require(facts.module_id == 9 and facts.program_outcome is not None, 'reference program did not execute')
        return activity

    def send_call_input(self, calldata):
        template = self.calls['request']
        entry_length = int.from_bytes(template[34:36], 'big')
        old_length = int.from_bytes(template[36:40], 'big')
        header = bytearray(template[:106])
        header[36:40] = len(calldata).to_bytes(4, 'big')
        payload = bytes(header) + template[106:106 + entry_length] + calldata
        payload += template[106 + entry_length + old_length:]
        activity = self.signed(0x00090003, payload, self.signing_key, self.caller)
        self.send(activity)
        self.receipt(activity)

    def adapter_vector(self):
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
        raw = bytes.fromhex((ROOT / 'tests/fixtures/web/observation-activity.hex').read_text().strip())
        require(raw[:5] == b'\0\2\20\1\14', 'legacy adapter envelope changed')
        cursor = 5
        fields = {}
        sizes = {1: 2, 2: 4, 3: 4, 6: 8, 7: 16, 9: 16}
        for tag in range(1, 13):
            require(cursor < len(raw) and raw[cursor] == tag, 'canonical vector field order')
            cursor += 1
            length = sizes.get(tag)
            if length is None:
                require(cursor + 4 <= len(raw), 'vector field bound')
                length = int.from_bytes(raw[cursor:cursor + 4], 'big')
                cursor += 4
            require(cursor + length <= len(raw), 'vector field truncated')
            fields[tag] = raw[cursor:cursor + length]
            cursor += length
        require(cursor == len(raw) and fields[3] == (0x000B0001).to_bytes(4, 'big'), 'vector activity identity')
        key = Ed25519PrivateKey.from_private_bytes(bytes([21]) + bytes(31))
        require(fields[5] == key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw), 'adapter vector authority')
        require(fields[8] == hashlib.sha256(b'LXP/v1/context-hash\0' + fields[11]).digest()
                and fields[10] == hashlib.sha256(b'LXP/v1/payload-hash\0' + fields[11]).digest(),
                'adapter vector payload/idempotency binding')
        unsigned = raw[:4] + b'\13' + raw[5:len(raw) - 69]
        signature = key.sign(hashlib.sha256(b'LXP/v1/signature-preimage\0' + unsigned).digest())
        require(signature == fields[12] and raw == unsigned[:4] + b'\14' + unsigned[5:]
                + b'\14\0\0\0\100' + signature, 'adapter exact signed bytes differ')
        self.record('adapter-encoding-vector')

    def reference_deployment(self):
        import struct
        raw = self.deployment
        require(raw[:5] == b'\0\3\20\1\14', 'reference deployment protocol mismatch')
        cursor = 5
        fields = {}
        sizes = {1: 2, 2: 4, 3: 4, 6: 8, 7: 16, 9: 16}
        for tag in range(1, 13):
            require(cursor < len(raw) and raw[cursor] == tag, 'deployment field order')
            cursor += 1
            length = sizes.get(tag)
            if length is None:
                require(cursor + 4 <= len(raw), 'deployment field bound')
                length = int.from_bytes(raw[cursor:cursor + 4], 'big')
                cursor += 4
            require(cursor + length <= len(raw), 'deployment field truncated')
            fields[tag] = raw[cursor:cursor + length]
            cursor += length
        expected = self.program + struct.pack('>HBB', 4, 0, 0) + bytes(32)
        expected += hashlib.sha256(self.wasm).digest() + len(self.wasm).to_bytes(4, 'big') + self.wasm
        require(cursor == len(raw) and fields[11] == expected
                and fields[2] == self.network.to_bytes(4, 'big')
                and fields[3] == (0x00090001).to_bytes(4, 'big'), 'deployed program is not the bound reference artifact')
        facts = self.receipt(raw)
        require(facts.module_id == 9, 'reference deployment receipt module')
        self.record('real-reference-deployment')

    def run(self):
        self.adapter_vector()
        self.reference_deployment()
        initial = self.balances()
        require(initial[0] >= self.fee, 'program fee account not funded')
        for name in ('wrong-fee', 'absent-read'):
            self.program_call(name, True)
            require(self.balances() == initial, 'refused reference program moved web fees')
            self.record(name)
        self.candidate.start()
        self.program_call('request')
        waiting = self.wait_entry(lambda row: row['stage'] == 'awaiting_quorum' and row['attestation'] is not None)
        require(len(waiting['attestation']['signatures']) < self.accounts['threshold'], 'before-quorum case absent')
        paid = self.balances()
        require(paid == [initial[0] - self.fee, initial[1] + self.fee] + initial[2:], 'atomic request fee mismatch')
        self.candidate.stop(crash=True)
        self.candidate.start('ready-to-sign')
        resumed = self.wait_entry(lambda row: row['attestation'] == waiting['attestation'])
        require(resumed['payload_hash'] == waiting['payload_hash'], 'restart changed attestation preimage')
        self.record('restart-before-quorum')
        for peer in self.processes[1:]:
            peer.start('attestation-held')
            require(peer.stopped.wait(45), 'peer did not durably attest before its send')
        require(self.candidate.stopped.wait(45), 'real quorum never reached')
        row = self.entry()
        require(row['stage'] == 'awaiting_quorum', 'negative cases require an unfulfilled paid request')
        attestation = row['attestation']
        signatures = [bytes.fromhex(item['signature']) for item in attestation['signatures']]
        signers = [item['signer'] for item in attestation['signatures']]
        registered = {item['signer'] for item in self.payouts}
        require(signers == sorted(set(signers)) and set(signers).issubset(registered)
                and len(signers) >= self.accounts['threshold'], 'registered quorum absent')
        response = bytes.fromhex(attestation['response'])
        observation = b'\2' + self.network.to_bytes(32, 'big') + self.program + self.request_id.to_bytes(8, 'big')
        observation += bytes([self.kind]) + bytes.fromhex(row['payload_hash'][2:])
        observation += bytes.fromhex(attestation['content_digest']) + int(attestation['full_length']).to_bytes(4, 'big')
        observation += len(response).to_bytes(4, 'big') + response
        offset = len(observation)
        observation += bytes([len(signatures)]) + b''.join(signatures)
        changed = bytearray(observation)
        changed[74] ^= 1
        wrong_program = bytearray(observation)
        wrong_program[33] ^= 1
        wrong_request = bytearray(observation)
        wrong_request[65] ^= 1
        wrong_origin = bytearray(observation)
        wrong_origin[0] = 1
        high_s = bytearray(signatures[0])
        order = 0xfffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141
        high_s[32:64] = (order - int.from_bytes(high_s[32:64], 'big')).to_bytes(32, 'big')
        high_s[64] = 55 - high_s[64]
        negative = {'payload-binding': bytes(changed), 'program-binding': bytes(wrong_program),
                    'request-binding': bytes(wrong_request), 'origin2-binding': bytes(wrong_origin),
                    'signer-order': observation[:offset] + bytes([len(signatures)]) + b''.join(reversed(signatures)),
                    'low-s': observation[:offset] + bytes([len(signatures)]) + bytes(high_s) + b''.join(signatures[1:]),
                    'minority': observation[:offset] + bytes([self.accounts['threshold'] - 1])
                                + b''.join(signatures[:self.accounts['threshold'] - 1])}
        for name, payload in negative.items():
            activity = self.signed(0x000B0001, payload, self.signing_key, self.caller)
            self.send(activity)
            facts = self.receipt(activity, True)
            require(facts.module_id == 11 and self.balances() == paid, 'refused observation changed fee state')
            self.record(name)
        self.candidate.command('-break-delete 1')
        location = self.breakpoint('after-submit')
        self.candidate.command('-break-insert ' + json.dumps(location))
        self.candidate.stopped.clear()
        self.candidate.command('-exec-continue')
        require(self.candidate.stopped.wait(45), 'after-submit crash boundary absent')
        submitted = self.entry()
        require(submitted['stage'] == 'submitting' and submitted['activity'], 'signed bytes not durable before send')
        activity = bytes.fromhex(submitted['activity'])
        self.receipt(activity)
        self.candidate.stop(crash=True)
        self.candidate.start()
        completed = self.wait_entry(lambda row: row['stage'] == 'completed')
        require(completed['activity'] == submitted['activity'] and completed['activity_id'] == submitted['activity_id'],
                'restart replaced signed activity identity')
        self.record('restart-after-submit')
        after = self.balances()
        expected = initial.copy()
        expected[0] -= self.fee
        quotient, remainder = divmod(self.fee, len(signers))
        for index, signer in enumerate(signers):
            matches = [i for i, row in enumerate(self.payouts) if row['signer'] == signer]
            require(len(matches) == 1, 'unregistered fee recipient')
            expected[2 + matches[0]] += quotient + (remainder if index == 0 else 0)
        require(after == expected, 'exactly-once fee split mismatch')
        read_input = b'\1\2' + self.request_id.to_bytes(8, 'big') + bytes.fromhex(attestation['content_digest'])
        read_input += int(attestation['full_length']).to_bytes(4, 'big') + response
        require(self.call_input(self.calls['read']) == read_input, 'web_read must compare identical committed data')
        self.program_call('read')
        first_receipt = self.receipt(activity)
        self.send(activity)
        require(self.receipt(activity) == first_receipt, 'exact-byte retry produced a second committed receipt')
        require(self.balances() == after, 'read or exact-byte retry charged web fee again')
        self.candidate.stop(crash=True)
        self.candidate.start()
        require(self.wait_entry(lambda row: row['stage'] == 'completed')['activity'] == completed['activity'],
                'completed observation changed across restart')
        self.program_call('read')
        require(self.balances() == after, 'second restart or read duplicated fee split')
        self.record('one-observation-one-fee-web-read')
        for peer in self.processes[1:]:
            peer.stop()
        original_id, original_payload = self.request_id, self.payload
        content = Path(self.primary['data_dir']) / 'content'
        held = content.with_name('content.kernel-recovery-held')
        require(content.is_dir() and not held.exists(), 'real content store fault boundary absent')
        content.rename(held)
        try:
            self.request_id = original_id + 1
            calldata = bytearray(self.call_input(self.calls['request']))
            calldata[2:10] = self.request_id.to_bytes(8, 'big')
            self.send_call_input(bytes(calldata))
            failed = self.wait_entry(lambda row: row['stage'] == 'awaiting_quorum' and row['attestation'] is None
                and row['last_error'] == 'attestation refused: content store')
            self.candidate.stop(crash=True)
            require(self.entry() == failed, 'retryable attestation dropped at crash')
        finally:
            held.rename(content)
        self.candidate.start()
        self.wait_entry(lambda row: row['stage'] == 'awaiting_quorum' and row['attestation'] is not None)
        self.record('retryable-attestation-restart')
        self.request_id = original_id + 2
        self.payload = b'\xff'
        calldata = bytearray(self.call_input(self.calls['request']))
        calldata[2:10] = self.request_id.to_bytes(8, 'big')
        calldata = calldata[:91] + self.payload
        self.send_call_input(bytes(calldata))
        refused = self.wait_entry(lambda row: row['stage'] == 'refused' and bool(row['reason']))
        self.candidate.stop(crash=True)
        self.candidate.start()
        require(self.wait_entry(lambda row: row['stage'] == 'refused') == refused,
                'terminal attestation refusal disappeared across restart')
        self.record('terminal-attestation-restart')
        self.request_id, self.payload = original_id, original_payload
        require(self.count == 16, 'required kernel cases absent')
        print('PAXEER_X_GATE tests=' + str(self.count) + ' skipped=0', flush=True)

    def close(self):
        for process in reversed(self.processes):
            process.stop()
        self.journal.close()


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=['paid-resource-delivery', 'evm-attestation-recovery', 'kernel-web-relay-recovery'])
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
        harness = (KernelHarness(args.candidate_manifest) if args.case == "kernel-web-relay-recovery"
                   else Harness(args.candidate_manifest))
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
            if isinstance(harness, KernelHarness):
                harness.close()
            else:
                harness.candidate.stop()
                harness.journal.close()


if __name__ == '__main__':
    raise SystemExit(main())
