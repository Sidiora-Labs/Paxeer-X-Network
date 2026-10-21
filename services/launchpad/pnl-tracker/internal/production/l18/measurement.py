import argparse
import base64
import concurrent.futures
import decimal
import gzip
import hashlib
import http.client
import json
import math
import os
import pathlib
import re
import signal
import subprocess
import threading
import time
import urllib.parse
import uuid

DIGEST = re.compile(r'^[0-9a-f]{64}$')
ADDRESS = re.compile(r'^0x[0-9a-f]{40}$')
RAW = re.compile(r'^(0|[1-9][0-9]*)$')
SOURCE = pathlib.Path(__file__).resolve().parents[6]
USDL = '0x85fcd13735f4309833a503ee804ea32395851479'
TRANSFER = '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef'
READ_PATHS = {'portfolio_read': '/v3/me/portfolio', 'reward_read': '/v2/me/rewards'}
REQUIRED_ACTIONS = {'duplicate_payout_callback', 'restart_between_commit_publish', 'node_disagreement', 'missing_dependency', 'referral_views'}
TABLE_ORDERS = {'l18_funding':'program,version,day,asset','l18_awards':'identity','l18_entries':'sequence','l18_claims':'identity','l18_epochs':'pool,epoch','l18_epoch_claims':'pool,epoch,beneficiary','l18_payout_attempts':'award_id,attempt','l18_bindings':'campaign,beneficiary','l18_inbox':'identity','l18_outbox':'sequence','l18_analytics':'identity'}
TABLES = tuple(TABLE_ORDERS)

class InputFailure(Exception):
    pass


def require(value, message):
    if not value:
        raise InputFailure(message)


def encode(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), default=str).encode()


def sha(path):
    digest = hashlib.sha256()
    with pathlib.Path(path).open('rb') as source:
        for block in iter(lambda: source.read(1048576), b''):
            digest.update(block)
    return digest.hexdigest()


def private_file(path):
    path = pathlib.Path(path)
    require(path.is_absolute() and path.is_file() and not path.is_symlink(), 'absolute private input file required')
    require(path.stat().st_mode & 0o077 == 0, 'private input permissions required')
    return path


def amount(value):
    require(isinstance(value, str) and RAW.fullmatch(value), 'noncanonical integer amount')
    result = int(value)
    require(result < 1 << 256, 'uint256 amount overflow')
    return result


def endpoint(url):
    parsed = urllib.parse.urlsplit(url)
    require(parsed.scheme in ('http', 'https') and parsed.hostname and not parsed.username and not parsed.password and not parsed.fragment, 'explicit credential-free service URL required')
    require(parsed.scheme == 'https' or parsed.hostname in ('127.0.0.1', 'localhost', '::1'), 'TLS required outside loopback')
    return url



class HTTP:
    def __init__(self, config):
        self.config = config
        self.local = threading.local()

    def fetch(self, url, headers=None, body=None):
        parsed = urllib.parse.urlsplit(endpoint(url))
        if not hasattr(self.local, 'connections'):
            self.local.connections = {}
        key = (parsed.scheme, parsed.hostname, parsed.port)
        if key not in self.local.connections:
            implementation = http.client.HTTPSConnection if parsed.scheme == 'https' else http.client.HTTPConnection
            self.local.connections[key] = implementation(parsed.hostname, parsed.port, timeout=self.config['request_timeout_seconds'])
        connection = self.local.connections[key]
        try:
            connection.request('POST' if body is not None else 'GET', urllib.parse.urlunsplit(('', '', parsed.path or '/', parsed.query, '')), body=body, headers=headers or {})
            response = connection.getresponse()
            require(not 300 <= response.status < 400, 'redirected measurement request refused')
            raw = response.read(self.config['max_response_bytes'] + 1)
            require(len(raw) <= self.config['max_response_bytes'], 'response exceeds admitted byte bound')
            return response.status, raw
        except Exception:
            connection.close()
            self.local.connections.pop(key, None)
            raise

    def rpc(self, url, method, params):
        code, raw = self.fetch(url, {'Content-Type': 'application/json'}, encode({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}))
        require(code == 200, 'canonical RPC HTTP failure')
        data = json.loads(raw)
        require('error' not in data and 'result' in data, 'canonical RPC unavailable')
        return data['result']


def load_config(path, mode):
    config = json.loads(private_file(path).read_text())
    require(config.get('version') == 1 and config.get('realm') == 'private_fixture', 'isolated qualification realm required')
    for field in ('release_digest', 'interface_digest', 'source_digest'):
        require(DIGEST.fullmatch(config.get(field, '')), 'current candidate digests required')
    runtime_path = private_file(config['runtime_document'])
    runtime = json.loads(runtime_path.read_text())
    require(runtime.get('version') == 3 and runtime.get('realm') == 'private_fixture', 'actual private pnl runtime document required')
    require(runtime.get('releaseDigest') == config['release_digest'] and runtime.get('interfaceDigest') == config['interface_digest'], 'runtime candidate/interface mismatch')
    domain = (SOURCE / 'services/launchpad/pnl-tracker/internal/production/l18/domain.go').read_text()
    manifest = re.search(r'const ManifestDigest = "([0-9a-f]{64})"', domain).group(1)
    require(runtime.get('rewardManifestDigest') == manifest, 'reward deployment manifest mismatch')
    require(config.get('rule_version') == 'l18-v1', 'current reward rule version required')
    manifest_rows = config['source_manifest']
    require(isinstance(manifest_rows, dict) and manifest_rows, 'actual source manifest required')
    owned = pathlib.Path(__file__).parent
    required_sources = {str(p.relative_to(SOURCE)) for p in owned.iterdir() if p.suffix in ('.go', '.py', '.sql', '.cjs', '.json')}
    require(required_sources <= set(manifest_rows), 'measurement source manifest omits lane source')
    for relative, expected in sorted(manifest_rows.items()):
        source = (SOURCE / relative).resolve()
        require(source.is_relative_to(SOURCE) and source.is_file() and DIGEST.fullmatch(expected), 'invalid source manifest member')
        require(sha(source) == expected, 'source changed since admission')
    require(hashlib.sha256(encode(manifest_rows)).hexdigest() == config['source_digest'], 'source manifest digest mismatch')
    require(re.fullmatch(r'[a-z][a-z0-9_]{0,62}', config['schema']), 'owned schema required')
    require(re.fullmatch(r'[A-Z][A-Z0-9_]*', config['database_dsn_env']) and os.environ.get(config['database_dsn_env']), 'explicit real PostgreSQL credential missing')
    require(type(config['duration_seconds']) is int and config['duration_seconds'] > 0, 'positive observation duration required')
    require(type(config['workers']) is int and 1 <= config['workers'] <= 2048, 'bounded worker count required')
    require(type(config['max_inflight']) is int and config['workers'] <= config['max_inflight'] <= 8192, 'bounded admission queue required')
    require(0 < config['request_timeout_seconds'] <= 30 and 1024 <= config['max_response_bytes'] <= 1048576, 'HTTP bounds required')
    require(config['reads'] and {r['kind'] for r in config['reads']} == set(READ_PATHS), 'actual portfolio and reward read clients required')
    for read in config['reads']:
        parsed = urllib.parse.urlsplit(endpoint(read['url']))
        require(parsed.path == READ_PATHS[read['kind']], 'read URL does not name actual service path')
        require(type(read['rps']) is int and read['rps'] > 0, 'positive integral offered rate required')
        private_file(read['authorization_file'])
    require(sum(r['rps'] for r in config['reads']) <= 10000, 'read allocation exceeds global dynamic target')
    require(set(config.get('actions', {})) <= REQUIRED_ACTIONS, 'unknown adversarial provider action')
    rpc_urls = config['rpc_urls']
    require(isinstance(rpc_urls, list) and len(rpc_urls) == 2 and rpc_urls[0] != rpc_urls[1], 'two explicit independent canonical RPC providers required')
    for url in rpc_urls:
        endpoint(url)
    if mode == 'serial-load':
        require(runtime.get('programs'), 'actual funded reward programs missing')
        require(config['duration_seconds'] == 86400, '24h mixed workload required')
        require(sum(r['rps'] for r in config['reads'] if r['kind'] == 'portfolio_read') == 2000, '2000 portfolio reads/s required')
        require(config.get('reward_mutations_per_second') == 100 and config.get('global_writes_per_second') == 2000, 'frozen 100 reward/2000 global writes target required')
        require(config.get('replay_span_seconds') == 259200, '72h canonical replay required')
        require(set(config.get('actions', {})) == REQUIRED_ACTIONS, 'all actual adversarial workload controls required')
        require(config.get('workload') and config.get('replay') and config.get('global_write_counter'), 'actual worker workload, replay and global write counter providers required')
    return config, runtime, sha(runtime_path)


def verify_transfer(http, urls, tx_hash, asset, source, beneficiary, raw, identity=None):
    require(ADDRESS.fullmatch(asset) and ADDRESS.fullmatch(source) and ADDRESS.fullmatch(beneficiary), 'canonical transfer actors required')
    require(re.fullmatch(r'0x[0-9a-f]{64}', tx_hash), 'canonical transaction hash required')
    receipts = []
    for url in urls:
        require(http.rpc(url, 'eth_chainId', []) == '0x7d', 'wrong settlement chain')
        receipt = http.rpc(url, 'eth_getTransactionReceipt', [tx_hash])
        require(receipt is not None and receipt.get('status') == '0x1' and receipt.get('transactionHash') == tx_hash, 'successful real settlement receipt required')
        block = http.rpc(url, 'eth_getBlockByNumber', [receipt['blockNumber'], False])
        require(block is not None and block['hash'] == receipt['blockHash'], 'receipt/canonical checkpoint mismatch')
        receipts.append(receipt)
    require(receipts[0] == receipts[1], 'independent nodes disagree on settlement receipt')
    matches = []
    for log in receipts[0]['logs']:
        if log['address'].lower() == asset and log.get('topics') == [TRANSFER, '0x' + '0' * 24 + source[2:], '0x' + '0' * 24 + beneficiary[2:]]:
            require(not log.get('removed', False) and len(log['data']) == 66, 'invalid real transfer event')
            matches.append(log)
    require(len(matches) == 1 and int(matches[0]['data'], 16) == amount(raw), 'receipt transfer does not equal liability')
    if identity is not None:
        require(identity == '125:' + tx_hash + ':' + str(int(matches[0]['logIndex'], 16)), 'settled receipt identity mismatch')
    return receipts[0]


def snapshot(config, runtime, output, label, http):
    try:
        import psycopg
        from psycopg.rows import dict_row
    except ImportError:
        raise InputFailure('real psycopg PostgreSQL driver missing')
    schema = config['schema']
    connection = psycopg.connect(os.environ[config['database_dsn_env']], row_factory=dict_row)
    state = {'counts': {}, 'digests': {}, 'violations': [], 'observed_wall_ns': time.time_ns()}
    programs = {}
    for admitted in runtime.get('programs', []):
        program = {key.lower(): value for key, value in admitted.items()}
        programs[(program['id'], program['version'])] = program
    try:
        return database_snapshot(connection, config, runtime, output, label, http, state, programs)
    finally:
        connection.close()


def database_snapshot(connection, config, runtime, output, label, http, state, programs):
    schema = config['schema']
    with connection:
        with connection.cursor() as cursor:
            cursor.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
            cursor.execute('SET LOCAL statement_timeout = 30000')
            cursor.execute('SET LOCAL search_path TO "' + schema + '"')
            for table in TABLES:
                digest = hashlib.sha256()
                count = 0
                with gzip.open(output / (label + '-' + table + '.jsonl.gz'), 'xb') as capture:
                    with connection.cursor(name=label + '_' + table) as rows:
                        rows.execute('SELECT to_jsonb(t)::text AS row FROM ' + table + ' t ORDER BY ' + TABLE_ORDERS[table])
                        for row in rows:
                            value = json.loads(row['row'], parse_int=str, parse_float=str)
                            if table == 'l18_outbox':
                                value.pop('published_at', None)
                            encoded = encode(value) + b'\n'
                            capture.write(encoded)
                            digest.update(encoded)
                            count += 1
                state['counts'][table] = count
                state['digests'][table] = digest.hexdigest()
            cursor.execute('SELECT program,version,day::text,asset,funded::text,reserved::text,daily_cap::text,beneficiary_cap::text,treasury,proof FROM l18_funding')
            funding = cursor.fetchall()
            for fund in funding:
                funded, reserved, daily, cap = [amount(fund[v]) for v in ('funded', 'reserved', 'daily_cap', 'beneficiary_cap')]
                require(0 <= reserved <= min(funded, daily), 'reserved budget exceeds funded/daily cap')
                cursor.execute('SELECT COALESCE(sum(payable),0)::text AS total FROM l18_awards WHERE program=%s AND version=%s AND day=%s AND asset=%s', (fund['program'], fund['version'], fund['day'], fund['asset']))
                require(int(cursor.fetchone()['total']) == reserved, 'reserved budget differs from actual awards')
                cursor.execute('SELECT beneficiary,sum(payable)::text AS total FROM l18_awards WHERE program=%s AND version=%s AND day=%s AND asset=%s GROUP BY beneficiary', (fund['program'], fund['version'], fund['day'], fund['asset']))
                require(all(int(row['total']) <= cap for row in cursor.fetchall()), 'beneficiary cap exceeded')
                p = programs.get((fund['program'], fund['version']))
                require(p is not None and p.get('custody') and p.get('treasury') == fund['treasury'], 'actual funded payout authority missing')
                evidence = verify_transfer(http, config['rpc_urls'], fund['proof'], fund['asset'], fund['treasury'], p['custody'], fund['funded'])
                with gzip.open(output / (label+'-funding-receipts.jsonl.gz'), 'ab') as receipts:
                    receipts.write(encode({'program':fund['program'],'version':fund['version'],'proof':fund['proof'],'receipt':evidence})+b'\n')
            award_rows = connection.cursor(name=label + '_award_invariants')
            award_rows.execute("SELECT a.identity,a.asset,a.beneficiary,a.program,a.version,a.payable::text,a.eligible::text,a.status,a.provenance,a.hold_reason,a.appeal_status,COALESCE(sum(e.raw) FILTER (WHERE e.debit_account='treasury_reserved:'||f.treasury AND e.credit_account='user_payable:'||a.beneficiary),0)::text AS reserved,COALESCE(sum(e.raw) FILTER (WHERE e.debit_account='user_payable:'||a.beneficiary AND e.credit_account='settled:'||a.beneficiary),0)::text AS settled FROM l18_awards a LEFT JOIN l18_funding f ON f.program=a.program AND f.version=a.version AND f.day=a.day AND f.asset=a.asset LEFT JOIN l18_entries e ON e.identity=a.identity GROUP BY a.identity")
            for award in award_rows:
                payable, eligible = amount(award['payable']), amount(award['eligible'])
                require(payable <= eligible and int(award['reserved']) == payable, 'award liability is not balanced by treasury debit')
                require(int(award['settled']) == (payable if award['status'] == 'paid' else 0), 'settled entry is not equal to paid liability')
                if award['status'] == 'pending_review':
                    require(award['hold_reason'] and award['appeal_status'] == 'open', 'fraud hold lost reason or appeal status')
                if award['status'] == 'paid':
                    cursor.execute("SELECT tx_hash,receipt_identity,raw::text FROM l18_payout_attempts WHERE award_id=%s AND state='included_success'", (award['identity'],))
                    settled = cursor.fetchall()
                    require(len(settled) == 1 and settled[0]['raw'] == award['payable'], 'award has duplicate/missing paid attempt')
                    program = programs.get((award['program'], award['version']))
                    require(program is not None, 'settlement rule authority missing')
                    evidence = verify_transfer(http, config['rpc_urls'], settled[0]['tx_hash'], award['asset'], program['custody'], award['beneficiary'], award['payable'], settled[0]['receipt_identity'])
                    with gzip.open(output / (label + '-receipts.jsonl.gz'), 'ab') as receipts:
                        receipts.write(encode({'award': award['identity'], 'receipt': evidence}) + b'\n')
            award_rows.close()
            cursor.execute('SELECT count(*) AS invalid FROM l18_entries e JOIN l18_awards a ON a.identity=e.identity WHERE e.asset<>a.asset OR e.rule_version<>a.version OR e.raw<=0 OR e.service_identity=\'\'')
            require(cursor.fetchone()['invalid'] == 0, 'entry asset/rule/service identity mismatch')
            cursor.execute("SELECT count(*) AS invalid FROM l18_entries e JOIN l18_awards a ON a.identity=e.identity LEFT JOIN l18_funding f ON f.program=a.program AND f.version=a.version AND f.day=a.day AND f.asset=a.asset WHERE NOT ((e.debit_account='treasury_reserved:'||f.treasury AND e.credit_account='user_payable:'||a.beneficiary AND e.provenance=a.provenance) OR (e.debit_account='user_payable:'||a.beneficiary AND e.credit_account='settled:'||a.beneficiary AND EXISTS(SELECT 1 FROM l18_payout_attempts p WHERE p.award_id=a.identity AND p.state='included_success' AND p.receipt_identity=e.provenance))) OR f.treasury IS NULL")
            require(cursor.fetchone()['invalid'] == 0, 'entry accounts/provenance are not the actual funding or settled transfer')
            cursor.execute("SELECT count(*) AS invalid FROM l18_epochs e WHERE e.claimed<>(SELECT COALESCE(sum(c.raw),0) FROM l18_epoch_claims c WHERE c.pool=e.pool AND c.epoch=e.epoch) OR e.claimed>e.funded")
            require(cursor.fetchone()['invalid'] == 0, 'epoch claimed amount differs from distinct receipt claims')
            cursor.execute("SELECT count(*) AS invalid FROM (SELECT campaign,actor,day,count(*) AS total FROM l18_analytics GROUP BY campaign,actor,day HAVING count(*)>1000) bounded")
            require(cursor.fetchone()['invalid'] == 0, 'valid referral analytics exceeded daily actor cap')
            cursor.execute("SELECT count(*) AS paid_awards FROM l18_awards WHERE status='paid'")
            state['paid_awards'] = cursor.fetchone()['paid_awards']
            cursor.execute("SELECT COALESCE(sum(payable) FILTER(WHERE status<>'paid'),0)::text AS outstanding_raw,COALESCE(sum(payable) FILTER(WHERE status='paid'),0)::text AS settled_raw FROM l18_awards")
            state['liabilities'] = cursor.fetchone()
    (output / (label + '-snapshot.json')).write_bytes(encode(state) + b'\n')
    return state


class Samples:
    def __init__(self, output):
        self.output = output
        self.lock = threading.Lock()
        self.files = {}
        self.counts = {}
        self.histograms = {}
        self.failures = 0
        self.read_complete = 0
        self.completed_buckets = {}
        self.active_files = {}
        self.seen_files = set()

    def record(self, sample, raw=None):
        with self.lock:
            bucket = max(0, sample['scheduled_offset_ns'] // 60000000000)
            key = (sample['kind'], bucket)
            previous = self.active_files.get(sample['kind'])
            if previous is not None and previous != key:
                self.files.pop(previous).close()
            if key not in self.files:
                mode = 'ab' if key in self.seen_files else 'xb'
                self.files[key] = gzip.open(self.output / ('requests-' + sample['kind'] + '-' + str(bucket) + '.jsonl.gz'), mode)
                self.seen_files.add(key)
                self.active_files[sample['kind']] = key
            if raw is not None and (sample['sequence'] % 1000 == 0 or not sample['valid']):
                sample['response_base64'] = base64.b64encode(raw).decode()
            self.files[key].write(encode(sample) + b'\n')
            self.counts[key] = self.counts.get(key, 0) + int(sample['valid'])
            complete_key = (sample['kind'], sample.get('completed_offset_ns', sample['scheduled_offset_ns']) // 60000000000)
            self.completed_buckets[complete_key] = self.completed_buckets.get(complete_key, 0) + int(sample['valid'])
            self.failures += int(not sample['valid'])
            self.read_complete += 1
            milliseconds = (sample['latency_ns'] + 999999) // 1000000
            histogram = self.histograms.setdefault(sample['kind'], {})
            histogram[milliseconds] = histogram.get(milliseconds, 0) + 1

    def close(self):
        for file in self.files.values():
            file.close()

    def quantile(self, kind, percentile):
        histogram = self.histograms.get(kind, {})
        threshold = math.ceil(sum(histogram.values()) * percentile)
        cumulative = 0
        for milliseconds, count in sorted(histogram.items()):
            cumulative += count
            if cumulative >= threshold:
                return milliseconds
        return None


def measure_reads(config, output, stop):
    samples = Samples(output)
    http = HTTP(config)
    slots = threading.BoundedSemaphore(config['max_inflight'])
    start = time.monotonic_ns()
    duration = config['duration_seconds'] * 1000000000
    errors = []
    error_lock = threading.Lock()

    def request(read, sequence, planned):
        sample = {'kind': read['kind'], 'sequence': sequence, 'request_id': str(uuid.uuid4()), 'scheduled_offset_ns': planned - start, 'scheduled_monotonic_ns': planned, 'started_monotonic_ns': time.monotonic_ns(), 'started_wall_ns': time.time_ns(), 'status': None, 'valid': False}
        raw = None
        try:
            headers = json.loads(private_file(read['authorization_file']).read_text())
            require(isinstance(headers, dict) and headers and all(isinstance(k, str) and isinstance(v, str) and '\n' not in v and '\r' not in v for k, v in headers.items()), 'current private authenticated headers required')
            require(not {'host', 'content-length'} & {k.lower() for k in headers}, 'credential headers cannot alter request destination')
            headers['X-Request-ID'] = sample['request_id']
            code, raw = http.fetch(read['url'], headers)
            sample['status'] = code
            sample['response_sha256'] = hashlib.sha256(raw).hexdigest()
            response = json.loads(raw)
            require(code == 200 and isinstance(response, dict) and 'data' in response and response.get('quality') == 'complete', 'read did not return a complete actual snapshot')
            watermark = response.get('watermark')
            require(isinstance(watermark, dict) and watermark.get('chainId') == '125' and watermark.get('integrity') == 'healthy', 'read canonical watermark missing/unhealthy')
            sample['watermark'] = watermark
            sample['valid'] = True
        except Exception as error:
            sample['failure_class'] = type(error).__name__
        finally:
            sample['finished_monotonic_ns'] = time.monotonic_ns()
            sample['completed_offset_ns'] = sample['finished_monotonic_ns'] - start
            sample['latency_ns'] = sample['finished_monotonic_ns'] - planned
            try:
                samples.record(sample, raw)
            except Exception as error:
                with error_lock:
                    errors.append(type(error).__name__)
                stop.set()
            slots.release()

    nexts = [0] * len(config['reads'])
    with concurrent.futures.ThreadPoolExecutor(max_workers=config['workers']) as executor:
        while not stop.is_set():
            chosen = min(range(len(nexts)), key=lambda i: nexts[i] * 1000000000 // config['reads'][i]['rps'])
            read = config['reads'][chosen]
            planned = start + nexts[chosen] * 1000000000 // read['rps']
            if planned - start >= duration:
                break
            wait = (planned - time.monotonic_ns()) / 1000000000
            if wait > 0 and stop.wait(wait):
                break
            sequence = nexts[chosen]
            nexts[chosen] += 1
            if not slots.acquire(blocking=False):
                samples.record({'kind': read['kind'], 'sequence': sequence, 'scheduled_offset_ns': planned-start, 'scheduled_monotonic_ns': planned, 'finished_monotonic_ns': time.monotonic_ns(), 'latency_ns': time.monotonic_ns()-planned, 'status': None, 'valid': False, 'failure_class': 'ClientAdmissionExhausted'})
                continue
            executor.submit(request, read, sequence, planned)
    samples.close()
    require(not errors and not stop.is_set(), 'measurement capture interrupted or failed')
    results = {'duration_seconds': config['duration_seconds'], 'completed': samples.read_complete, 'failures': samples.failures, 'buckets': [{'kind': kind, 'minute': bucket, 'successful': count} for (kind, bucket), count in sorted(samples.counts.items())], 'completed_buckets': [{'kind': kind, 'minute': bucket, 'successful': count} for (kind, bucket), count in sorted(samples.completed_buckets.items())], 'latency_ms': {kind: {'p95': samples.quantile(kind, .95), 'p99': samples.quantile(kind, .99)} for kind in READ_PATHS}}
    (output / 'read-results.json').write_bytes(encode(results) + b'\n')
    for kind, latency in results['latency_ms'].items():
        require(latency['p95'] is not None and latency['p95'] <= 150 and latency['p99'] <= 400, 'read latency exceeds fixed SLO')
    require(samples.failures == 0, 'failed HTTP requests cannot count as admitted throughput')
    expected = sum(read['rps'] for read in config['reads']) * config['duration_seconds']
    require(samples.read_complete == expected, 'offered workload incomplete')
    (output / 'read-results.json').write_bytes(encode(results) + b'\n')
    return results


def control(config, output, name, declaration, wait):
    argv = declaration['argv']
    require(isinstance(argv, list) and argv and all(isinstance(v, str) and '\n' not in v for v in argv), 'actual provider argv required')
    executable = pathlib.Path(argv[0])
    require(executable.is_absolute() and executable.is_file() and os.access(executable, os.X_OK), 'explicit provider executable missing')
    require(DIGEST.fullmatch(declaration.get('sha256', '')) and sha(executable) == declaration['sha256'], 'provider executable identity mismatch')
    require(declaration.get('realm') == 'private_fixture' and declaration.get('release_digest') == config['release_digest'], 'provider workload authority/candidate mismatch')
    inputs = declaration.get('input_manifest')
    require(isinstance(inputs, dict) and inputs, 'actual workload/provider input identity manifest required')
    for file, expected in inputs.items():
        source = pathlib.Path(file)
        require(source.is_absolute() and source.is_file() and DIGEST.fullmatch(expected) and sha(source)==expected, 'workload/replay provider source input changed')
    for argument in argv[1:]:
        path = pathlib.Path(argument)
        if path.is_absolute() and path.is_file():
            require(argument in inputs, 'provider file argument lacks source identity binding')
    for variable in declaration.get('required_env', []):
        require(re.fullmatch(r'[A-Z][A-Z0-9_]*', variable) and os.environ.get(variable), 'actual provider credential missing')
    stdout = (output / (name + '-stdout.log')).open('xb')
    stderr = (output / (name + '-stderr.log')).open('xb')
    child = subprocess.Popen(argv, cwd=SOURCE, stdout=stdout, stderr=stderr, start_new_session=True)
    stdout.close()
    stderr.close()
    if wait:
        try:
            code = child.wait(timeout=declaration['timeout_seconds'])
        except subprocess.TimeoutExpired:
            terminate(child)
            raise InputFailure('provider action exceeded admitted deadline')
        require(code == 0, 'actual provider action failed')
        return {'name': name, 'exit_code': code, 'executable_sha256': declaration['sha256']}
    return child


def terminate(child):
    if child.poll() is not None:
        return child.returncode
    try:
        os.killpg(child.pid, signal.SIGTERM)
        return child.wait(timeout=15)
    except ProcessLookupError:
        return child.wait(timeout=15)
    except subprocess.TimeoutExpired:
        os.killpg(child.pid, signal.SIGKILL)
        child.wait(timeout=15)
        raise InputFailure('workload provider failed bounded graceful stop')


def counter(config, http):
    declaration = config['global_write_counter']
    require(declaration.get('semantics') == 'committed_application_writes', 'counter must explicitly measure committed application writes')
    semantics_source = declaration['source_path']
    require(semantics_source in config['source_manifest'] and sha(SOURCE/semantics_source)==config['source_manifest'][semantics_source], 'application counter source is not in admitted source manifest')
    headers = json.loads(private_file(declaration['authorization_file']).read_text()) if declaration.get('authorization_file') else {}
    require(isinstance(headers, dict) and not {'host','content-length'} & {key.lower() for key in headers}, 'invalid metric authentication headers')
    code, raw = http.fetch(endpoint(declaration['url']),headers)
    require(code == 200, 'actual global write counter unavailable')
    key = declaration['sample']
    require(re.fullmatch(r'[A-Za-z_:][A-Za-z0-9_:]*(?:\{[^\n]+\})?', key), 'explicit application global write counter required')
    matched = [line.split() for line in raw.decode().splitlines() if line and not line.startswith('#') and line.split()[0] == key]
    require(len(matched) == 1 and len(matched[0]) in (2, 3), 'ambiguous/missing global application write counter')
    value = decimal.Decimal(matched[0][1])
    require(value.is_finite() and value >= 0 and value == value.to_integral_value(), 'global write counter is not an integral application count')
    return {'value': int(value), 'observed_wall_ns': time.time_ns(), 'raw_base64': base64.b64encode(raw).decode(), 'response_sha256': hashlib.sha256(raw).hexdigest()}


def verify_replay_input(config, http):
    declaration = config['replay']
    require(declaration['source_archive'] in declaration['argv'], 'replay provider is not bound to canonical archive argument')
    path = private_file(declaration['source_archive'])
    require(DIGEST.fullmatch(declaration.get('archive_sha256', '')) and sha(path) == declaration['archive_sha256'], 'immutable replay archive identity mismatch')
    opened = gzip.open if path.suffix == '.gz' else open
    first = last = None
    count = 0
    previous = None
    with opened(path, 'rb') as source:
        for line in source:
            require(len(line) <= 1048576, 'canonical replay event exceeds byte bound')
            event = json.loads(line)
            position = event['position']
            require(position['chainId'] == '125', 'replay event has wrong chain')
            seconds = amount(event['blockTimestampSeconds'])
            order = tuple(amount(position[key]) for key in ('height', 'transactionIndex', 'logIndex'))
            require(previous is None or previous < order, 'replay archive is not ordered by distinct canonical identity')
            previous = order
            observation = (seconds, position)
            if first is None:
                first = observation
            last = observation
            count += 1
    require(first is not None and last[0] - first[0] >= config['replay_span_seconds'], 'actual archive does not cover required 72h replay')
    for seconds, position in (first, last):
        for url in config['rpc_urls']:
            block = http.rpc(url, 'eth_getBlockByNumber', [hex(amount(position['height'])), False])
            require(block and block['hash'] == position['blockHash'] and int(block['timestamp'], 16) == seconds, 'replay archive boundary is not a real canonical checkpoint')
    return {'archive_sha256': declaration['archive_sha256'], 'events': count, 'first_timestamp_seconds': str(first[0]), 'last_timestamp_seconds': str(last[0]), 'covered_seconds': last[0]-first[0], 'from_height': first[1]['height'], 'to_height': last[1]['height']}

def replay_job(config, output, label, archive, complete):
    import psycopg
    from psycopg.rows import dict_row
    declaration = config['replay']
    require(re.fullmatch(r'[a-zA-Z0-9_-]{1,128}', declaration['job_id']), 'actual admitted replay job identity required')
    with psycopg.connect(os.environ[config['database_dsn_env']], row_factory=dict_row) as connection:
        with connection.cursor() as cursor:
            cursor.execute('SET TRANSACTION READ ONLY')
            cursor.execute('SELECT job_id,chain_id,source_stream,target,generation,projection_revision,mode,authorization_id,from_height,to_height,progress_height,status,request_digest,request FROM indexer.indexer_replay_jobs WHERE job_id=%s', (declaration['job_id'],))
            jobs = cursor.fetchall()
            if not complete:
                require(not jobs, 'replay job already has historical evidence')
                return
            require(len(jobs)==1, 'actual replay job never admitted')
            job = jobs[0]
            require(job['chain_id']==125 and job['mode']=='replay' and job['status']=='completed' and job['progress_height']==job['to_height'], 'durable canonical replay is not complete')
            for key in ('source_stream','target','generation','projection_revision','authorization_id'):
                require(job[key]==declaration[key], 'replay authority/target generation mismatch')
            require(str(job['from_height'])==archive['from_height'] and str(job['to_height'])==archive['to_height'], 'durable replay does not cover actual archive range')
            cursor.execute('SELECT from_height,to_height,raw_count,intent_count,identity_digest,checkpoint_digest FROM indexer.indexer_replay_batches WHERE job_id=%s ORDER BY from_height', (declaration['job_id'],))
            batches = cursor.fetchall()
            require(batches and sum(batch['raw_count'] for batch in batches)>0 and sum(batch['intent_count'] for batch in batches)>0, 'no actual canonical replay batches emitted')
            expected = job['from_height']
            for batch in batches:
                require(batch['from_height']==expected and batch['to_height']>=expected, 'durable replay batches have a gap/overlap')
                expected = batch['to_height']+1
            require(expected==job['to_height']+1, 'replay batches do not complete source range')
            evidence = {'job':job,'batches':batches,'observed_wall_ns':time.time_ns()}
            (output/(label+'-replay-job.json')).write_bytes(encode(evidence)+b'\n')
            return evidence

def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', type=pathlib.Path, required=True)
    parser.add_argument('--output-dir', type=pathlib.Path, required=True)
    parser.add_argument('--mode', choices=('observe', 'serial-load'), default='observe')
    args = parser.parse_args()
    output = args.output_dir.resolve()
    output.mkdir(parents=True, exist_ok=False, mode=0o700)
    stop = threading.Event()
    signal.signal(signal.SIGTERM, lambda signum, frame: stop.set())
    signal.signal(signal.SIGINT, lambda signum, frame: stop.set())
    result = {'schema_version': 1, 'mode': args.mode, 'global_qualification_owner': 'serial9.1', 'capacity_qualified': False, 'lane_measurement_passed': False, 'started_wall_ns': time.time_ns()}
    child = None
    try:
        config, runtime, runtime_digest = load_config(args.config, args.mode)
        result.update({field: config[field] for field in ('release_digest', 'interface_digest', 'source_digest', 'rule_version')})
        result['runtime_document_sha256'] = runtime_digest
        http = HTTP(config)
        before = snapshot(config, runtime, output, 'before-load', http)
        if args.mode == 'serial-load':
            require(before['counts']['l18_funding']>0, 'actual funded qualification treasury missing')
        global_before = counter(config, http) if config.get('global_write_counter') else None
        if config.get('workload'):
            child = control(config, output, 'workload', config['workload'], False)
        result['reads'] = measure_reads(config, output, stop)
        if child is not None:
            require(child.poll() is None, 'workload provider exited before complete mixed observation')
            result['workload_exit_code'] = terminate(child)
            child = None
            require(result['workload_exit_code'] in (0, -signal.SIGTERM), 'workload provider failed on stop')
        global_after = counter(config, http) if global_before else None
        after = snapshot(config, runtime, output, 'after-load', http)
        result['reward_awards_created'] = after['counts']['l18_awards'] - before['counts']['l18_awards']
        result['reward_entries_appended'] = after['counts']['l18_entries'] - before['counts']['l18_entries']
        result['payout_attempts_total'] = after['counts']['l18_payout_attempts']
        result['paid_awards_total'] = after['paid_awards']
        result['actions'] = []
        for name, declaration in sorted(config.get('actions', {}).items()):
            state_before = snapshot(config, runtime, output, 'before-' + name, http)
            result['actions'].append(control(config, output, name, declaration, True))
            state_after = snapshot(config, runtime, output, 'after-' + name, http)
            if name in ('duplicate_payout_callback', 'node_disagreement', 'missing_dependency', 'referral_views'):
                for table in ('l18_funding', 'l18_awards', 'l18_entries', 'l18_claims', 'l18_epochs', 'l18_epoch_claims'):
                    require(state_before['digests'][table] == state_after['digests'][table], 'adversarial control changed monetary state: ' + name)
            if name == 'referral_views':
                require(declaration.get('event_count') == 10000, '10000 actual referral views required')
                valid_views = state_after['counts']['l18_analytics'] - state_before['counts']['l18_analytics']
                require(1 <= valid_views <= 1000, 'referral view workload did not produce bounded actual valid events')
        if config.get('replay'):
            result['replay_input'] = verify_replay_input(config, http)
            replay_job(config, output, 'before', result['replay_input'], False)
            replay_before = snapshot(config, runtime, output, 'before-replay', http)
            result['replay'] = control(config, output, 'replay', config['replay'], True)
            result['durable_replay'] = replay_job(config, output, 'after', result['replay_input'], True)
            replay_after = snapshot(config, runtime, output, 'after-replay', http)
            require(replay_before['digests'] == replay_after['digests'], 'canonical replay changed existing durable effects')
        if args.mode == 'serial-load':
            require(result['reward_awards_created'] >= 100 * 86400, 'actual award mutations did not meet 100/s target')
            global_writes = global_after['value'] - global_before['value']
            require(global_writes >= 2000 * 86400, 'actual global application writes did not meet 2000/s target')
            require(result['reward_awards_created'] <= global_writes, 'reward mutations are not a subset of actual global application writes')
            require(after['paid_awards']>before['paid_awards'], 'mixed workload produced no real settled reward transfer')
            result['global_write_observations'] = {'before': global_before, 'after': global_after}
            result['capacity_targets_observed'] = True
        result['lane_measurement_passed'] = True
        return 0
    except Exception as error:
        result['failure_class'] = type(error).__name__
        result['failure'] = str(error) if isinstance(error, InputFailure) else 'actual provider/capture failure; inspect bounded private artifacts'
        return 2
    finally:
        if child is not None:
            try:
                terminate(child)
            except Exception:
                result['workload_cleanup_failed'] = True
        result['finished_wall_ns'] = time.time_ns()
        result['artifact_manifest'] = {path.name: sha(path) for path in output.iterdir() if path.is_file() and path.name != 'measurement.json'}
        (output / 'measurement.json').write_bytes(encode(result) + b'\n')


if __name__ == '__main__':
    raise SystemExit(main())
