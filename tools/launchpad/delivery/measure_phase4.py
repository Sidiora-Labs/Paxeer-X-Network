import argparse
import asyncio
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import sqlite3
import ssl
import sys
import time
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tools/launchpad/production/load'))
from phase2_feeder import PostgresObserver
sys.path.insert(0,str(ROOT/'tools/launchpad/production'))
from qualify import snapshot as candidate_snapshot, canonical_hash as candidate_hash, ignored_source_directory

HEX = re.compile(r'^[0-9a-f]{64}$')
NAME = re.compile(r'^[A-Za-z0-9_-]{1,63}$')


def require(condition, message):
    if not condition:
        raise ValueError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def sha_file(path):
    digest=hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda:source.read(1<<20),b''):
            digest.update(block)
    return digest.hexdigest()


def pinned_file(item):
    path = Path(item['path'])
    require(path.is_absolute() and not path.is_symlink() and path.is_file(), 'pinned regular provider file required')
    require(HEX.fullmatch(item['sha256']) and sha_file(path) == item['sha256'], 'provider file source binding changed')
    return path


def private_write(path, raw):
    with path.open('xb') as output:
        os.chmod(path, 0o600)
        output.write(raw)
        output.flush()
        os.fsync(output.fileno())


def references(config):
    values = {}
    for target, reference in config.items():
        require(re.fullmatch(r'[A-Z][A-Z0-9_]{0,127}', target) and re.fullmatch(r'[A-Z][A-Z0-9_]{0,127}', reference), 'explicit credential environment references required')
        require(reference in os.environ and os.environ[reference], 'actual private provider credential unavailable')
        values[target] = os.environ[reference]
    return values


def percentile(histogram, percentile_value):
    require(type(percentile_value) is int and 1<=percentile_value<=100,'bounded exact percentile required')
    require(histogram and all(type(size) is int and size >= 0 and type(count) is int and count > 0 for size, count in histogram.items()), 'actual nonempty integer histogram required')
    total = sum(histogram.values())
    boundary = (total * percentile_value + 99) // 100
    cumulative = 0
    for value, count in sorted(histogram.items()):
        cumulative += count
        if cumulative >= boundary:
            return value
    raise ValueError('histogram boundary unavailable')


def assert_candidate(ledger):
    manifest=json.loads(pinned_file(ledger['candidate_manifest']).read_bytes())
    require(manifest['sha256']==ledger['candidate_sha256'] and candidate_hash(manifest['files'])==manifest['sha256'],'candidate manifest identity differs from approved capacity ledger')
    require(candidate_snapshot(ROOT)==manifest,'actual current source differs from the complete approved candidate')


def private_output(directory):
    resolved=directory.resolve()
    try:
        relative=resolved.relative_to(ROOT)
    except ValueError:
        return
    require(relative.parts and ignored_source_directory(relative),'measurement output must not alter candidate source inventory')


def validate_ledger(ledger):
    require(ledger['version'] == 1 and HEX.fullmatch(ledger['candidate_sha256']) and ledger['approved_by'] and ledger['approval_reference'], 'approved candidate and capacity authority required')
    require(type(ledger['sustained']['rate']) is int and ledger['sustained']['rate'] == 2000 and type(ledger['sustained']['seconds']) is int and ledger['sustained']['seconds'] > 0, 'frozen sustained traffic target required')
    require(type(ledger['burst']['rate']) is int and type(ledger['burst']['seconds']) is int and ledger['burst']['rate'] == 10000 and ledger['burst']['seconds'] == 300, 'frozen 10000 event/s five-minute burst required')
    bounds = ledger['budgets']
    for key in ('max_backlog_bytes', 'database_capacity_bytes', 'database_reserve_bytes', 'drain_seconds', 'reconciliation_seconds', 'generator_delay_p99_ns', 'minimum_runway_seconds', 'max_provider_log_bytes', 'disk_measurement_max_age_ns', 'total_run_timeout_seconds', 'cleanup_seconds'):
        require(type(bounds[key]) is int and bounds[key] > 0, 'approved positive measurement budget required')
    require(bounds['database_reserve_bytes'] < bounds['database_capacity_bytes'], 'database reserve exceeds capacity')
    require(type(bounds['ingest_p99_ns']) is int and type(bounds['browser_p99_ns']) is int and bounds['ingest_p99_ns'] == 500000000 and bounds['browser_p99_ns'] == 1000000000, 'frozen latency targets required')
    require(type(ledger['fault_after_seconds']) is int and 0 < ledger['fault_after_seconds'] < 300, 'bounded real AZ fault point required')
    require(re.fullmatch(r'[a-z][a-z0-9_]{0,62}',ledger['producer_schema']) and NAME.fullmatch(ledger['stream']), 'exact service schema and stream required')
    require(len(ledger['consumers']) >= 2 and len(ledger['pools']) >= 2, 'actual mixed consumer and pool workload required')
    require(len(set(c['consumer_id'] for c in ledger['consumers'])) == len(ledger['consumers']), 'duplicate consumer authority')
    for consumer in ledger['consumers']:
        require(NAME.fullmatch(consumer['consumer_id']) and NAME.fullmatch(consumer['generation']), 'server-owned consumer identity required')
        require(consumer['subjects'] and set(consumer['subjects'])<=set(ledger['subjects']),'exact subscribed consumer subjects required')
        pinned_file(consumer['effect_query'])
    require(len(ledger['subjects']) > 0 and all(re.fullmatch(r'[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*', s) for s in ledger['subjects']), 'exact producer subjects required')
    members = ledger['broker']['members']
    require(len(members) == 3 and len({m['zone_id'] for m in members}) == 3 and len({m['server_name'] for m in members}) == 3 and len({m['server_id'] for m in members}) == 3, 'three distinct pinned physical AZ members required')
    inventory = json.loads(pinned_file(ledger['broker']['physical_inventory']).read_bytes())
    require(inventory['jetstream_unique_tag']=='az:' and inventory['sync_interval']=='always' and inventory['tls_required'] is True and inventory['subject_scoped_credentials'] is True and inventory['separate_admin_identity'] is True,'pinned operated broker persistence and principal policy required')
    require(inventory['candidate_sha256'] == ledger['candidate_sha256'] and inventory['members'] == [{k: m[k] for k in ('server_id', 'server_name', 'zone_id')} for m in members], 'independent physical AZ inventory differs from approved members')
    for member in members:
        parsed = urlparse(member['url'])
        require(parsed.scheme == 'tls' and parsed.hostname and parsed.port and parsed.username is None and not parsed.path and not parsed.query and not parsed.fragment, 'authenticated exact TLS broker member required')
        require(member['tls_handshake'] in ('first', 'after_info'), 'explicit broker TLS handshake authority required')
    for name in ('sustained', 'burst', 'fault_down', 'fault_up', 'hot_replay', 'archive_rebuild', 'object_get'):
        command = ledger['commands'][name]
        require(command['argv'] and all(type(arg) is str and arg for arg in command['argv']) and type(command['timeout_seconds']) is int and command['timeout_seconds'] > 0, 'actual bounded provider command required')
        require(command['argv'][0] == str(pinned_file(command['executable'])), 'command executable authority differs')
        for pin in command['source_files']:
            pinned_file(pin)
    require(ledger['candidate_files'],'actual candidate source bindings required')
    pinned_file(ledger['candidate_manifest'])
    require(bounds['cleanup_seconds']>=ledger['commands']['fault_up']['timeout_seconds'],'approved cleanup must cover the actual AZ restore deadline')
    for pin in ledger['candidate_files']:
        pinned_file(pin)
    for pin in (ledger['postgres_executable'], ledger['archive']['source_time_query'], ledger['hot_replay_query'], ledger['browser_journal_authority']):
        pinned_file(pin)
    require(ledger['archive']['manifest_digests'] and all(HEX.fullmatch(d) for d in ledger['archive']['manifest_digests']), 'actual immutable archive manifests required')
    require(NAME.fullmatch(ledger['archive']['schema']) and NAME.fullmatch(ledger['archive']['consumer']) and NAME.fullmatch(ledger['archive']['original_generation']) and NAME.fullmatch(ledger['archive']['rebuild_generation']) and NAME.fullmatch(ledger['archive']['prefix']) and ledger['archive']['original_generation'] != ledger['archive']['rebuild_generation'], 'separate real archive generation required')
    return ledger


class Capture:
    def __init__(self, directory, binding):
        self.directory, self.binding = directory, binding
        self.sequence, self.previous = 0, '0' * 64
        self.path = directory / 'observations.jsonl'
        self.output = self.path.open('xb')
        os.chmod(self.path, 0o600)

    def record(self, kind, value):
        row = dict(sequence=self.sequence, previous_sha256=self.previous, candidate_sha256=self.binding['candidate_sha256'], capacity_ledger_sha256=self.binding['capacity_ledger_sha256'], monotonic_ns=time.monotonic_ns(), observed_utc_ns=time.time_ns(), kind=kind, value=value)
        raw = canonical(row)
        self.output.write(raw + b'\n')
        self.output.flush()
        self.previous = sha(raw)
        self.sequence += 1

    def close(self):
        self.output.flush()
        os.fsync(self.output.fileno())
        self.output.close()


class NatsSession:
    def __init__(self, reader, writer, member):
        self.reader, self.writer, self.member, self.sid = reader, writer, member, 0

    @classmethod
    async def connect(cls, member, config):
        uri = urlparse(member['url'])
        context = ssl.create_default_context(cafile=str(pinned_file(config['ca'])))
        if 'client_certificate' in config:
            context.load_cert_chain(str(pinned_file(config['client_certificate'])), str(pinned_file(config['client_key'])))
        reader, writer = await asyncio.open_connection(uri.hostname, uri.port, ssl=context if member['tls_handshake'] == 'first' else None, server_hostname=uri.hostname if member['tls_handshake'] == 'first' else None, limit=4 << 20)
        try:
            line = await reader.readline()
            require(line.startswith(b'INFO ') and len(line) <= 65536, 'actual NATS server INFO required')
            info = json.loads(line[5:])
            require(info['server_id'] == member['server_id'] and info['server_name'] == member['server_name'], 'broker member identity drift')
            if member['tls_handshake'] == 'after_info':
                require(info.get('tls_required') is True, 'broker must require verified TLS')
                await writer.start_tls(context, server_hostname=uri.hostname)
            credentials = references(config['credential_environment'])
            require(set(credentials) == {'USER', 'PASSWORD'}, 'explicit measurement broker principal required')
            connect = dict(verbose=False, pedantic=True, tls_required=True, user=credentials['USER'], **{'pass': credentials['PASSWORD']}, name='kindle-delivery-measurement', lang='python', version='1')
            writer.write(b'CONNECT ' + canonical(connect) + b'\r\n')
            await writer.drain()
            return cls(reader, writer, member)
        except BaseException:
            writer.close()
            await writer.wait_closed()
            raise

    async def request(self, subject, body):
        self.sid += 1
        sid = str(self.sid).encode()
        reply = ('_INBOX.measurement.' + os.urandom(16).hex()).encode()
        payload = canonical(body)
        self.writer.write(b'SUB ' + reply + b' ' + sid + b'\r\nUNSUB ' + sid + b' 1\r\nPUB ' + subject.encode() + b' ' + reply + b' ' + str(len(payload)).encode() + b'\r\n' + payload + b'\r\n')
        await self.writer.drain()
        while True:
            line = await self.reader.readline()
            require(line and len(line) <= 65536, 'bounded live broker response required')
            if line == b'PING\r\n':
                self.writer.write(b'PONG\r\n')
                await self.writer.drain()
            elif line.startswith(b'-ERR'):
                raise ValueError('broker authentication or authorization denied')
            elif line.startswith(b'MSG '):
                parts = line.split()
                require(len(parts) in (4, 5) and parts[2] == sid and parts[1] == reply, 'actual broker reply identity required')
                size = int(parts[-1])
                require(0 < size <= 4 << 20, 'broker response size bound')
                raw = await self.reader.readexactly(size + 2)
                require(raw[-2:] == b'\r\n', 'broker response framing invalid')
                response = json.loads(raw[:-2])
                require(not response.get('error'), 'broker persistence API unavailable or denied')
                return response
            elif not (line.startswith(b'INFO ') or line in (b'+OK\r\n', b'PONG\r\n')):
                raise ValueError('unexpected broker protocol frame')

    async def close(self):
        self.writer.close()
        await asyncio.wait_for(self.writer.wait_closed(),5)


class Broker:
    def __init__(self, config):
        self.config, self.pool = config, asyncio.Queue()
        self.slots = asyncio.Semaphore(32)
        self.sessions = set()

    async def request(self, subject, body):
        async with self.slots:
            try:
                session = self.pool.get_nowait()
            except asyncio.QueueEmpty:
                session = None
            for member in [None,*self.config['members']]:
                try:
                    if session is None:
                        if member is None:
                            continue
                        session = await asyncio.wait_for(NatsSession.connect(member, self.config), 5)
                        self.sessions.add(session)
                    result = await asyncio.wait_for(session.request(subject, body), 5)
                    self.pool.put_nowait(session)
                    return result
                except (OSError, ValueError, asyncio.TimeoutError, asyncio.IncompleteReadError):
                    if session is not None:
                        self.sessions.discard(session)
                        await session.close()
                        session = None
            raise ValueError('approved broker members unavailable')

    async def info(self, stream, fault=False):
        result = await self.request('$JS.API.STREAM.INFO.' + stream, {})
        config, cluster = result['config'], result['cluster']
        require(config['storage'] == 'file' and config['retention']=='limits' and config['discard']=='new' and 0<config['max_msg_size']<=1048576 and config['num_replicas'] == 3 and config['max_age'] >= 72 * 3600 * 10**9, 'actual replicated 72-hour file-backed stream required')
        names = {cluster['leader']} | {peer['name'] for peer in cluster['replicas']}
        require(names == {member['server_name'] for member in self.config['members']}, 'actual stream replica inventory differs')
        unavailable = sum(1 for peer in cluster['replicas'] if peer.get('offline') or not peer.get('current'))
        require(unavailable <= (1 if fault else 0), 'actual persistence replicas not healthy')
        result['measurement_unavailable_replicas'] = unavailable
        return result

    async def close(self):
        await asyncio.gather(*(s.close() for s in self.sessions), return_exceptions=True)


async def command(ledger, name, output, **substitutions):
    config = ledger['commands'][name]
    for pin in [config['executable'], *config['source_files']]:
        pinned_file(pin)
    substitutions['output'] = str(output)
    argv = []
    for value in config['argv']:
        for key, replacement in substitutions.items():
            value = value.replace('{{' + key + '}}', str(replacement))
        require('{{' not in value and '}}' not in value, 'provider command substitution unresolved')
        argv.append(value)
    environment = references(config['environment'])
    log = output / (name + '-' + str(time.monotonic_ns()) + '.log')
    process = await asyncio.create_subprocess_exec(*argv, env=environment, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.STDOUT, start_new_session=True)
    async def capture():
        count = 0
        with log.open('xb') as target:
            os.chmod(log, 0o600)
            while True:
                block = await process.stdout.read(65536)
                if not block:
                    break
                count += len(block)
                require(count <= ledger['budgets']['max_provider_log_bytes'], 'provider output budget exceeded')
                target.write(block)
        require(await process.wait() == 0, 'actual provider action failed; inspect private action log')
    try:
        await asyncio.wait_for(capture(), config['timeout_seconds'])
        return dict(action=name, exit_code=process.returncode, log_path=str(log), log_sha256=sha_file(log))
    finally:
        if process.returncode is None:
            for sig in (signal.SIGTERM, signal.SIGKILL):
                try:
                    os.killpg(process.pid, sig)
                except ProcessLookupError:
                    break
                try:
                    await asyncio.wait_for(process.wait(), 1)
                    break
                except asyncio.TimeoutError:
                    pass
            await process.wait()


def sql_file(pin, ids):
    text = pinned_file(pin).read_text()
    require(text.lstrip().upper().startswith('SELECT ') and ';' not in text and '--' not in text and '/*' not in text and '{{event_ids}}' in text, 'pinned single read-only actual effect query required')
    require(ids and all(HEX.fullmatch(value) for value in ids), 'exact canonical event identities required')
    return text.replace('{{event_ids}}', ','.join("'" + value + "'" for value in ids))


def read_receipts(path, db, profile, rate, seconds, generator_budget):
    require(path.is_file() and not path.is_symlink(), 'real current-profile receipt journal unavailable')
    histogram, first, last, count = {}, None, None, 0
    with path.open() as source:
        for line in source:
            require(len(line) <= 2 << 20, 'bounded actual chain receipt required')
            row = json.loads(line)
            scheduled, observed = int(row['scheduled_utc_ns']), int(row['observed_utc_ns'])
            require(scheduled > 0 and observed >= scheduled and re.fullmatch(r'0x[0-9a-f]{64}', row['transaction_hash']) and re.fullmatch(r'0x[0-9a-f]{64}', row['block_hash']), 'actual scheduled committed receipt identity required')
            first = scheduled if first is None else min(first, scheduled)
            last = scheduled if last is None else max(last, scheduled)
            delay = observed - scheduled
            bound=(delay+999999)//1000000*1000000
            histogram[bound] = histogram.get(bound, 0) + 1
            require(row['logs'], 'relevant committed receipt logs unavailable')
            db.execute('INSERT INTO receipts VALUES(?,?,?,?,?,?)', (row['transaction_hash'], row['block_hash'], profile, scheduled, observed, sha(canonical(row))))
            count += 1
    require(count > 0 and last - first >= (seconds - 1) * 10**9, 'complete timed actual workload receipt range required')
    require(percentile(histogram, 99) <= generator_budget, 'generator saturation or coordinated omission exceeds approved budget')
    db.commit()
    return dict(profile=profile, receipts=count, scheduled_from_ns=first, scheduled_to_ns=last, receipt_sha256=sha_file(path), generator_delay_p99_ns=percentile(histogram, 99), requested_event_rate=rate)


async def snapshot(observer, broker, ledger, capture, fault):
    schema = ledger['producer_schema']
    sql = "SELECT json_build_object('utc_ns',(extract(epoch FROM clock_timestamp())*1000000000)::numeric(30,0)::text,'accepted_rows',count(*),'accepted_bytes',COALESCE(sum(octet_length(envelope)),0),'published_rows',count(*) FILTER(WHERE state='published'),'published_bytes',COALESCE(sum(octet_length(envelope)) FILTER(WHERE state='published'),0),'backlog_bytes',COALESCE(sum(octet_length(envelope)) FILTER(WHERE state<>'published'),0),'quarantined',count(*) FILTER(WHERE state='quarantined'),'database_bytes',pg_database_size(current_database()),'index_bytes',pg_indexes_size('" + schema + ".delivery_outbox'::regclass)) FROM " + schema + '.delivery_outbox'
    rows = await observer.query(sql)
    require(len(rows) == 1, 'actual database backlog snapshot unavailable')
    row = rows[0]
    require(row['backlog_bytes'] <= ledger['budgets']['max_backlog_bytes'] and row['database_bytes'] <= ledger['budgets']['database_capacity_bytes'] - ledger['budgets']['database_reserve_bytes'] and row['quarantined'] == 0, 'actual backlog, database reserve or quarantine budget exceeded')
    info = await broker.info(ledger['stream'], fault)
    capture.record('database', row)
    capture.record('broker_topology', info)
    return row, info


async def run_profile(name, ledger, observer, broker, capture, db, output):
    config = ledger[name]
    before, _ = await snapshot(observer, broker, ledger, capture, False)
    action = asyncio.create_task(command(ledger, name, output))
    fault_action = None
    fault_active = False
    started = time.monotonic()
    samples = [before]
    unavailable_seen = False
    try:
        while not action.done():
            if name == 'burst' and fault_action is None and time.monotonic() - started >= ledger['fault_after_seconds']:
                fault_action = True
                fault_active = True
                capture.record('fault_down', await command(ledger, 'fault_down', output))
            row, info = await snapshot(observer, broker, ledger, capture, fault_active)
            samples.append(row)
            unavailable_seen |= info['measurement_unavailable_replicas'] == 1
            await asyncio.sleep(1)
        capture.record('workload', await action)
    finally:
        if not action.done():
            action.cancel()
            await asyncio.gather(action, return_exceptions=True)
        if fault_active:
            capture.record('fault_up', await command(ledger, 'fault_up', output))
    require(time.monotonic() - started >= config['seconds'], 'actual workload did not sustain its complete duration')
    if name == 'burst':
        require(fault_action and unavailable_seen, 'actual one-AZ loss was not observed during accepted workload')
    receipts = read_receipts(output / (name + '-receipts.jsonl'), db, name, config['rate'], config['seconds'], ledger['budgets']['generator_delay_p99_ns'])
    capture.record('receipt_journal', receipts)
    deadline = time.monotonic() + ledger['budgets']['drain_seconds']
    drain_start = time.monotonic_ns()
    peak = max(s['backlog_bytes'] for s in samples)
    while True:
        row, info = await snapshot(observer, broker, ledger, capture, fault_action is not None)
        samples.append(row)
        if row['backlog_bytes'] <= before['backlog_bytes'] and info['measurement_unavailable_replicas']==0:
            break
        require(time.monotonic() < deadline, 'backlog exceeded approved drain deadline')
        await asyncio.sleep(1)
    growing_runways = []
    for previous, current in zip(samples, samples[1:]):
        elapsed_ns = int(current['utc_ns']) - int(previous['utc_ns'])
        require(elapsed_ns > 0 and current['accepted_bytes'] >= previous['accepted_bytes'] and current['published_bytes'] >= previous['published_bytes'], 'immutable measurement counters or clock regressed')
        growth = current['accepted_bytes'] - previous['accepted_bytes'] - current['published_bytes'] + previous['published_bytes']
        if growth > 0:
            free = ledger['budgets']['database_capacity_bytes'] - ledger['budgets']['database_reserve_bytes'] - current['database_bytes']
            runway = free * elapsed_ns // growth // 10**9
            growing_runways.append(runway)
            require(runway >= ledger['budgets']['minimum_runway_seconds'], 'measured growing backlog runway below approved budget')
    require(row['index_bytes']>=before['index_bytes'],'index measurement regressed during workload')
    return dict(profile=name, peak_backlog_bytes=peak, drain_ns=time.monotonic_ns()-drain_start, index_delta_bytes=row['index_bytes']-before['index_bytes'], receipt=receipts, growing_runway_seconds=growing_runways)


async def reconcile_sources(observer, ledger, db, capture, profiles):
    histogram, buckets, source_count, durable_buckets = {}, {}, {}, {}
    cursor = None
    while True:
        rows = await observer.committed(after=cursor)
        if not rows:
            break
        for row in rows:
            cursor = row['event_id']
            receipt = db.execute('SELECT profile,scheduled_ns,observed_ns FROM receipts WHERE transaction_hash=? AND block_hash=?', (row['transaction_hash'],row['block_hash'])).fetchone()
            if receipt is None:
                continue
            profile, scheduled, observed = receipt
            committed = int(row['durable_commit_utc_ns'])
            require(committed >= observed and row['timestamp_authority']=='postgres_transaction_commit', 'independently observed commit and durable timestamp inconsistent')
            latency = committed-observed
            bound=(latency+999999)//1000000*1000000
            histogram[bound] = histogram.get(bound,0)+1
            db.execute('INSERT INTO sources VALUES(?,?,?,?)',(row['event_id'],profile,committed,latency))
            db.execute('INSERT INTO source_receipts VALUES(?,?)',(row['event_id'],row['transaction_hash']))
            start = profiles[profile]['receipt']['scheduled_from_ns']
            bucket = (scheduled-start)//10**9
            buckets[(profile,bucket)] = buckets.get((profile,bucket),0)+1
            durable_bucket=(committed-start)//10**9
            durable_buckets[(profile,durable_bucket)]=durable_buckets.get((profile,durable_bucket),0)+1
            source_count[profile] = source_count.get(profile,0)+1
            capture.record('canonical_commit',row)
        db.commit()
    for name in ('sustained','burst'):
        config=ledger[name]
        require(source_count.get(name,0)>=config['rate']*config['seconds'], 'actual relevant committed event rate below frozen profile')
        for second in range(config['seconds']):
            require(buckets.get((name,second),0)>=config['rate'],'scheduled relevant-event bucket below frozen target')
        for second in range(1,config['seconds']):
            require(durable_buckets.get((name,second),0)>=config['rate'],'actual durable committed event rate below frozen target')
    require(percentile(histogram,99)<=ledger['budgets']['ingest_p99_ns'],'observed commit to durable ingest p99 exceeds frozen target')
    require(db.execute('SELECT count(*) FROM receipts r WHERE NOT EXISTS(SELECT 1 FROM sources s JOIN source_receipts x ON x.source_id=s.source_id WHERE x.transaction_hash=r.transaction_hash)').fetchone()[0]==0,'accepted receipt has no relevant canonical delivery')
    return dict(committed_sources=source_count,durable_per_second={name:{str(second):count for (profile,second),count in durable_buckets.items() if profile==name} for name in ('sustained','burst')},ingest_p99_ns=percentile(histogram,99),histogram_upper_bounds_ns={str(k):v for k,v in histogram.items()},histogram_resolution_ns=1000000)


async def reconcile_delivery(observer, broker, ledger, db, capture):
    histogram={}
    schema=ledger['producer_schema']
    cursor=''
    while True:
        sources=[r[0] for r in db.execute('SELECT source_id FROM sources WHERE source_id>? ORDER BY source_id LIMIT 1000',(cursor,))]
        if not sources:
            break
        require(all(HEX.fullmatch(value) for value in sources),'canonical source identities malformed')
        ids=','.join("'"+value+"'" for value in sources)
        subjects=','.join("'"+value+"'" for value in ledger['subjects'])
        query="SELECT json_build_object('event_id',event_id,'subject',subject,'wire',encode(envelope,'base64'),'wire_hash',payload_hash,'state',state,'stream',broker_stream,'sequence',broker_sequence,'source_id',convert_from(envelope,'UTF8')::jsonb->'payload'->>'sourceFillId') FROM "+schema+".delivery_outbox WHERE subject IN ("+subjects+") AND convert_from(envelope,'UTF8')::jsonb->'payload'->>'sourceFillId' IN ("+ids+") ORDER BY event_id"
        rows=await observer.query(query)
        require(rows,'canonical sources have no transactional outbox publications')
        async def check(row):
            require(row['state']=='published' and row['stream']==ledger['stream'] and type(row['sequence']) is int and row['sequence']>0,'accepted outbox identity lacks durable persistence acknowledgement')
            wire=base64.b64decode(row['wire'])
            message=(await broker.request('$JS.API.STREAM.MSG.GET.'+ledger['stream'],{'seq':row['sequence']}))['message']
            require(message['seq']==row['sequence'] and message['subject']==row['subject'] and base64.b64decode(message['data'])==wire,'acknowledged immutable broker identity lost or changed')
            envelope=json.loads(wire)
            require(envelope['event_id']==row['event_id'] and envelope['chain_id']==125 and envelope['schema_version']==1 and envelope['payload_hash']==row['wire_hash'],'persisted envelope binding conflict')
            existing=db.execute('SELECT source_id,wire_hash FROM deliveries WHERE event_id=?',(row['event_id'],)).fetchone()
            require(existing is None or existing==(row['source_id'],sha(wire)),'replayed immutable publication changed')
            if existing is None:
                db.execute('INSERT INTO deliveries VALUES(?,?,?)',(row['event_id'],row['source_id'],sha(wire)))
            histogram[len(wire)]=histogram.get(len(wire),0)+1
            capture.record('broker_persistence',dict(event_id=row['event_id'],source_id=row['source_id'],stream=row['stream'],sequence=row['sequence'],wire_sha256=sha(wire),serialized_bytes=len(wire)))
        for offset in range(0,len(rows),32):
            tasks=[asyncio.create_task(check(row)) for row in rows[offset:offset+32]]
            try:
                await asyncio.gather(*tasks)
            except BaseException:
                for task in tasks:
                    task.cancel()
                await asyncio.gather(*tasks,return_exceptions=True)
                raise
        for consumer in ledger['consumers']:
            expected={row['event_id'] for row in rows if row['subject'] in consumer['subjects']}
            if not expected:
                continue
            effects=await observer.query(sql_file(consumer['effect_query'],sorted(expected)))
            seen=set()
            for effect in effects:
                require(effect['consumer_id']==consumer['consumer_id'] and effect['generation']==consumer['generation'] and effect['event_id'] in expected and effect['event_id'] not in seen and type(effect['effect_count']) is int and effect['effect_count']==1 and effect['envelope_hash']==db.execute('SELECT wire_hash FROM deliveries WHERE event_id=?',(effect['event_id'],)).fetchone()[0], 'actual per-consumer effect/inbox identity conflict')
                seen.add(effect['event_id'])
                if db.execute('SELECT 1 FROM effects WHERE consumer_id=? AND event_id=?',(consumer['consumer_id'],effect['event_id'])).fetchone() is None:
                    db.execute('INSERT INTO effects VALUES(?,?)',(consumer['consumer_id'],effect['event_id']))
                capture.record('consumer_effect',effect)
            require(seen==expected,'consumer lost accepted durable event identities')
        db.commit()
        cursor=sources[-1]
    require(db.execute('SELECT count(*) FROM sources s WHERE NOT EXISTS(SELECT 1 FROM deliveries d WHERE d.source_id=s.source_id)').fetchone()[0]==0,'accepted relevant source lacks durable publication')
    for consumer in ledger['consumers']:
        require(db.execute('SELECT count(*) FROM effects WHERE consumer_id=?',(consumer['consumer_id'],)).fetchone()[0]>0,'mixed workload left a required consumer without actual effects')
    return histogram


async def reconcile_archive(observer, ledger, capture, output):
    archive=ledger['archive'];schema=archive['schema']
    count,first,last=0,None,None
    for digest in archive['manifest_digests']:
        rows=await observer.query("SELECT json_build_object('manifest',convert_from(manifest,'UTF8')::json,'manifest_wire',encode(manifest,'base64'),'digest',manifest_digest) FROM "+schema+".delivery_archive_manifests WHERE manifest_digest='"+digest+"'")
        require(len(rows)==1,'actual archive manifest authority unavailable')
        manifest=rows[0]['manifest']
        from qualify_phase2 import validate_archive_manifest
        validate_archive_manifest(manifest)
        require(manifest['sourceStream']==ledger['source_stream'] and manifest['deployment']==archive['deployment'] and manifest['decoderDigest']==archive['decoder_sha256'] and manifest['abiDigest']==archive['abi_sha256'] and manifest['codeDigest']==archive['code_sha256'],'archive provenance differs from approved source authority')
        require(last is None or manifest['fromHeight']==last+1,'archive manifest range has a silent gap')
        first=manifest['fromHeight'] if first is None else first;last=manifest['toHeight']
        for kind,object_digest,expected_size in [('manifests',digest,None)]+[('segments',b['segmentHash'],b['segmentBytes']) for b in manifest['blocks']]:
            destination=output/('archive-'+object_digest)
            if not destination.exists():
                capture.record('archive_object_get',await command(ledger,'object_get',output,key=archive['prefix']+'/'+kind+'/'+object_digest,destination=destination))
            raw=destination.read_bytes()
            require(sha(raw)==object_digest and (expected_size is None or len(raw)==expected_size),'immutable archive checksum or segment size mismatch')
            if kind=='manifests':
                require(raw==base64.b64decode(rows[0]['manifest_wire']),'archive object and database manifest bytes differ')
        count+=manifest['rawCount']
        for block in manifest['blocks']:
            actual=await observer.query("SELECT json_build_object('block_hash',block_hash,'raw_count',raw_count) FROM indexer.indexer_block_completeness_v1 WHERE chain_id=125 AND stream_id='"+manifest['sourceStream']+"' AND height="+str(block['height'])+" AND fetch_complete AND required_decode_complete")
            require(len(actual)==1 and actual[0]['block_hash']==block['blockHash'] and actual[0]['raw_count']==block['rawCount'],'archive range lacks matching complete canonical authority')
            records=await observer.query("SELECT json_build_object('TransactionHash',transaction_hash,'BlockHash',block_hash,'Emitter',emitter,'Height',height,'TransactionIndex',transaction_index,'LogIndex',log_index,'Topics',topics,'Data',replace(encode(data,'base64'),E'\\n','')) FROM indexer.indexer_raw_logs_v1 WHERE chain_id=125 AND height="+str(block['height'])+" ORDER BY transaction_index,log_index")
            raw=b''.join(json.dumps({key:record[key] for key in ('TransactionHash','BlockHash','Emitter','Height','TransactionIndex','LogIndex','Topics','Data')},separators=(',',':'),ensure_ascii=False).encode()+b'\n' for record in records)
            require(len(records)==block['rawCount'] and len(raw)==block['segmentBytes'] and sha(raw)==block['segmentHash'],'archive contents differ from complete canonical raw source')
        capture.record('archive_manifest',dict(digest=digest,from_height=manifest['fromHeight'],to_height=manifest['toHeight'],raw_count=manifest['rawCount']))
    source_time=pinned_file(archive['source_time_query']).read_text()
    require(source_time.lstrip().upper().startswith('SELECT ') and ';' not in source_time and '--' not in source_time and '/*' not in source_time,'actual canonical archive timestamp query required')
    times=await observer.query(source_time)
    require(len(times)==1 and times[0]['from_height']==first and times[0]['to_height']==last and int(times[0]['to_utc_ns'])>int(times[0]['from_utc_ns']) and time.time_ns()-int(times[0]['to_utc_ns'])>72*3600*10**9,'actual archived range must end beyond broker retention')
    generations=await observer.query("SELECT json_build_object('generation',generation,'status',status,'raw_count',raw_count,'projection_digest',projection_digest,'final_watermark',final_watermark,'next_height',next_height) FROM "+schema+".delivery_rebuilds WHERE consumer_id='"+archive['consumer']+"' AND generation IN ('"+archive['original_generation']+"','"+archive['rebuild_generation']+"')")
    by_generation={row['generation']:row for row in generations}
    require(len(by_generation)==2,'actual independently rebuilt generations unavailable')
    original=by_generation[archive['original_generation']];rebuilt=by_generation[archive['rebuild_generation']]
    require(rebuilt['status']=='verified' and original['status'] in ('verified','active','retired') and original['raw_count']==rebuilt['raw_count']==count and original['projection_digest']==rebuilt['projection_digest'] and HEX.fullmatch(rebuilt['projection_digest']) and type(rebuilt['final_watermark']) is dict and rebuilt['final_watermark'].get('height')==str(last) and original['final_watermark']==rebuilt['final_watermark'] and original['next_height']==rebuilt['next_height']==last+1,'archive rebuild counts, watermark or deterministic projection hash conflict')
    capture.record('archive_generations',generations)
    return dict(raw_count=count,final_height=last,projection_sha256=rebuilt['projection_digest'])


async def prepare(ledger,output,binding):
    private_output(output)
    assert_candidate(ledger)
    require(not output.exists(),'exclusive private harness preparation output required')
    output.mkdir(mode=0o700,parents=True)
    capture=Capture(output,binding)
    broker=Broker(ledger['broker'])
    observer=PostgresObserver(str(pinned_file(ledger['postgres_executable'])),ledger['postgres_executable']['sha256'],references(ledger['postgres_environment']),stream_id=ledger['source_stream'],pools=ledger['pools'])
    report=dict(task='4.08',**binding,passed=False,measurement_harness_prepared=False,full_load_acceptance=False,global_acceptance_owner='serial9.1')
    try:
        capture.record('postgres_authority',await observer.readiness())
        await snapshot(observer,broker,ledger,capture,False)
        rows=await observer.committed(limit=1)
        require(len(rows)==1,'actual canonical commit provider needs a retained real event')
        capture.record('canonical_provider',rows[0])
        for consumer in ledger['consumers']:
            actual=await observer.query(sql_file(consumer['effect_query'],[rows[0]['event_id']]))
            capture.record('consumer_provider_preflight',dict(consumer_id=consumer['consumer_id'],generation=consumer['generation'],returned_rows=len(actual),query_sha256=consumer['effect_query']['sha256']))
        for measurement in ledger['broker']['disk_measurements']:
            pinned_file(measurement)
        assert_candidate(ledger)
        report['passed']=True
        report['measurement_harness_prepared']=True
    except Exception as error:
        report['failure_class']=type(error).__name__
        report['failure']=str(error) if isinstance(error,ValueError) else 'actual harness provider unavailable; inspect private captures'
    finally:
        await broker.close();capture.close()
        report['observations_sha256']=sha_file(capture.path)
        report['observations_final_chain_sha256']=capture.previous
        private_write(output/'result.json',canonical(report)+b'\n')
    return 0 if report['passed'] else 1


async def measure(ledger, output, binding):
    private_output(output)
    assert_candidate(ledger)
    require(not output.exists(),'exclusive private measurement output required')
    output.mkdir(mode=0o700,parents=True)
    capture=Capture(output,binding)
    db=sqlite3.connect(output/'identities.sqlite3')
    os.chmod(output/'identities.sqlite3',0o600)
    db.executescript('CREATE TABLE receipts(transaction_hash TEXT PRIMARY KEY,block_hash TEXT NOT NULL,profile TEXT NOT NULL,scheduled_ns INTEGER NOT NULL,observed_ns INTEGER NOT NULL,wire_hash TEXT NOT NULL);CREATE TABLE sources(source_id TEXT PRIMARY KEY,profile TEXT NOT NULL,committed_ns INTEGER NOT NULL,latency_ns INTEGER NOT NULL);CREATE TABLE source_receipts(source_id TEXT PRIMARY KEY,transaction_hash TEXT NOT NULL);CREATE TABLE deliveries(event_id TEXT PRIMARY KEY,source_id TEXT NOT NULL,wire_hash TEXT NOT NULL);CREATE INDEX delivery_source ON deliveries(source_id);CREATE TABLE effects(consumer_id TEXT NOT NULL,event_id TEXT NOT NULL,PRIMARY KEY(consumer_id,event_id));CREATE TABLE browser_publications(source_id TEXT PRIMARY KEY,frame_hash TEXT NOT NULL);')
    observer=PostgresObserver(str(pinned_file(ledger['postgres_executable'])),ledger['postgres_executable']['sha256'],references(ledger['postgres_environment']),stream_id=ledger['source_stream'],pools=ledger['pools'])
    broker=Broker(ledger['broker'])
    report=dict(task='4.08',**binding,passed=False,lane_measurement_passed=False,full_load_acceptance=False,global_acceptance_owner='serial9.1')
    try:
        capture.record('postgres_authority',await observer.readiness())
        profiles={}
        for name in ('sustained','burst'):
            profiles[name]=await run_profile(name,ledger,observer,broker,capture,db,output)
        report['sources']=await reconcile_sources(observer,ledger,db,capture,profiles)
        sizes=await asyncio.wait_for(reconcile_delivery(observer,broker,ledger,db,capture),ledger['budgets']['reconciliation_seconds'])
        report['profiles']=profiles
        report['serialized_p99_bytes']=percentile(sizes,99)
        index_bytes=sum(profile['index_delta_bytes'] for profile in profiles.values())
        accepted=sum(report['sources']['committed_sources'].values())
        pg_index_per_event=(index_bytes+accepted-1)//accepted
        index_per_event=0
        observed_nodes=set()
        disk=ledger['broker']['disk_measurements']
        require(len(disk)==3,'actual node disk measurements unavailable')
        for measurement in disk:
            pin=pinned_file(measurement)
            value=json.loads(pin.read_bytes())
            require(value['server_id'] in {m['server_id'] for m in ledger['broker']['members']} and value['server_id'] not in observed_nodes and value['candidate_sha256']==ledger['candidate_sha256'] and 0<=time.time_ns()-int(value['measured_utc_ns'])<=ledger['budgets']['disk_measurement_max_age_ns'] and type(value['index_and_metadata_bytes']) is int and value['index_and_metadata_bytes']>=0 and type(value['stored_events']) is int and value['stored_events']>0,'actual independent node storage measurement missing, stale or unbound')
            observed_nodes.add(value['server_id'])
            index_per_event=max(index_per_event,(value['index_and_metadata_bytes']+value['stored_events']-1)//value['stored_events'])
            capture.record('broker_disk',value)
        required_logical=2000*72*3600*(report['serialized_p99_bytes']+index_per_event)
        required_per_node=(required_logical*125+99)//100
        for measurement in disk:
            value=json.loads(pinned_file(measurement).read_bytes())
            require(type(value['free_bytes']) is int and type(value['reserve_bytes']) is int and value['reserve_bytes']>=0 and value['free_bytes']-value['reserve_bytes']>=required_per_node,'measured replicated broker retention runway insufficient')
        info=await broker.info(ledger['stream'])
        require(info['config']['max_bytes']>=required_per_node,'actual stream logical retention budget insufficient')
        report['capacity']=dict(producer_pg_index_bytes_per_event=pg_index_per_event,broker_metadata_index_bytes_per_event=index_per_event,logical_bytes=required_logical,replicated_bytes=required_logical*3,required_bytes_per_node=required_per_node)
        capture.record('hot_replay',await command(ledger,'hot_replay',output))
        hot_query=pinned_file(ledger['hot_replay_query']).read_text()
        require(hot_query.lstrip().upper().startswith('SELECT ') and ';' not in hot_query and '--' not in hot_query and '/*' not in hot_query,'actual hot replay reconciliation query required')
        hot_rows=await observer.query(hot_query)
        require(len(hot_rows)==len(ledger['consumers']),'complete actual hot replay consumer coverage required')
        hot_consumers=set()
        for row in hot_rows:
            require(row['consumer_id'] in {c['consumer_id'] for c in ledger['consumers']} and row['consumer_id'] not in hot_consumers and int(row['to_utc_ns'])-int(row['from_utc_ns'])>=72*3600*10**9 and row['expected_count']>0 and row['expected_count']==row['replayed_count'] and HEX.fullmatch(row['expected_identity_sha256']) and row['expected_identity_sha256']==row['replayed_identity_sha256'] and row['expected_effect_sha256']==row['replayed_effect_sha256'] and HEX.fullmatch(row['expected_effect_sha256']) and type(row['expected_watermark']) is dict and row['expected_watermark'] and row['expected_watermark']==row['replayed_watermark'],'72-hour replay identities, per-consumer effects or committed watermark conflict')
            hot_consumers.add(row['consumer_id'])
            capture.record('hot_replay_reconciliation',row)
        report['hot_replay']=hot_rows
        await asyncio.wait_for(reconcile_delivery(observer,broker,ledger,db,capture),ledger['budgets']['reconciliation_seconds'])
        alias_query="SELECT json_build_object('generation',generation,'prior_generation',prior_generation,'revision',revision) FROM "+ledger['archive']['schema']+".delivery_read_aliases WHERE consumer_id='"+ledger['archive']['consumer']+"'"
        aliases_before=await observer.query(alias_query)
        require(len(aliases_before)==1 and aliases_before[0]['generation']==ledger['archive']['original_generation'],'actual active archive generation alias required')
        capture.record('archive_alias_before',aliases_before)
        capture.record('archive_rebuild',await command(ledger,'archive_rebuild',output))
        report['archive']=await reconcile_archive(observer,ledger,capture,output)
        aliases_after=await observer.query(alias_query)
        require(aliases_after==aliases_before,'archive rebuild changed the active read alias before comparison acceptance')
        capture.record('archive_alias_after',aliases_after)
        public_cursor=''
        while True:
            public_ids=[row[0] for row in db.execute('SELECT source_id FROM sources WHERE source_id>? ORDER BY source_id LIMIT 1000',(public_cursor,))]
            if not public_ids:
                break
            quoted=','.join("'"+identity+"'" for identity in public_ids)
            public_rows=await observer.query("SELECT json_build_object('source_id',o.event_id,'frame',convert_from(o.frame,'UTF8')::json,'topic',p.topic) FROM api_stream.stream_delivery_outbox o JOIN api_stream.stream_projection p USING(partition_id) WHERE o.event_id IN ("+quoted+") AND p.topic LIKE 'market.trades.%'")
            require(len(public_rows)==len(public_ids),'accepted relevant fills lack individual committed public trade frames')
            for row in public_rows:
                require(row['source_id'] in public_ids and row['frame']['eventId']==row['source_id'] and row['frame']['topic']==row['topic'] and row['frame']['coalesced'] is False,'actual public trade source identity differs')
                db.execute('INSERT INTO browser_publications VALUES(?,?)',(row['source_id'],sha(canonical(row['frame']))))
            db.commit();public_cursor=public_ids[-1]
        browser_path=output/'browser-receipts.jsonl'
        require(browser_path.is_file(),'actual browser delivery receipt journal unavailable')
        browser_hist={};seen=set()
        with browser_path.open() as source:
            for line in source:
                require(len(line)<=1<<20,'bounded browser delivery receipt required')
                row=json.loads(line)
                require(row['candidate_sha256']==ledger['candidate_sha256'] and row['event_id'] not in seen,'browser receipt source binding or duplicate identity conflict')
                committed=db.execute('SELECT s.committed_ns,b.frame_hash FROM sources s JOIN browser_publications b ON b.source_id=s.source_id WHERE s.source_id=?',(row['event_id'],)).fetchone()
                require(committed is not None and int(row['observed_utc_ns'])>=committed[0] and sha(canonical(row['frame']))==committed[1],'browser receipt lacks actual committed public trade authority')
                latency=int(row['observed_utc_ns'])-committed[0]
                bound=(latency+999999)//1000000*1000000
                browser_hist[bound]=browser_hist.get(bound,0)+1;seen.add(row['event_id'])
        require(len(seen)==db.execute('SELECT count(*) FROM sources').fetchone()[0] and percentile(browser_hist,99)<=ledger['budgets']['browser_p99_ns'],'accepted browser event coverage or ingest-to-browser p99 failed')
        capture.record('browser_receipts',dict(path=str(browser_path),sha256=sha_file(browser_path),samples=len(seen),reader_authority_sha256=ledger['browser_journal_authority']['sha256']))
        report['browser_p99_ns']=percentile(browser_hist,99)
        report['browser_histogram_resolution_ns']=1000000
        for pin in ledger['candidate_files']:
            pinned_file(pin)
        assert_candidate(ledger)
        report['passed']=True
        report['lane_measurement_passed']=True
    except Exception as error:
        report['failure_class']=type(error).__name__
        report['failure']=str(error) if isinstance(error,ValueError) else 'actual provider measurement failed; inspect private captures'
    finally:
        await broker.close()
        db.commit();db.close();capture.close()
        report['observations_sha256']=sha_file(capture.path)
        report['observations_final_chain_sha256']=capture.previous
        report['identities_sha256']=sha_file(output/'identities.sqlite3')
        private_write(output/'result.json',canonical(report)+b'\n')
    return 0 if report['passed'] else 1


def main(argv=None):
    parser=argparse.ArgumentParser()
    parser.add_argument('--ledger',type=Path,required=True)
    parser.add_argument('--ledger-sha256',required=True)
    parser.add_argument('--candidate-sha256',required=True)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--prepare-harness',action='store_true')
    args=parser.parse_args(argv)
    raw=args.ledger.read_bytes()
    require(HEX.fullmatch(args.ledger_sha256) and sha(raw)==args.ledger_sha256,'approved capacity ledger digest required')
    ledger=validate_ledger(json.loads(raw))
    require(args.candidate_sha256==ledger['candidate_sha256'],'exact candidate digest required')
    runner=prepare if args.prepare_harness else measure
    async def bounded():
        loop=asyncio.get_running_loop()
        operation=asyncio.create_task(runner(ledger,args.output,dict(candidate_sha256=args.candidate_sha256,capacity_ledger_sha256=args.ledger_sha256)))
        for sig in (signal.SIGTERM,signal.SIGINT):
            loop.add_signal_handler(sig,operation.cancel)
        try:
            return await asyncio.wait_for(operation,ledger['budgets']['total_run_timeout_seconds'])
        finally:
            for sig in (signal.SIGTERM,signal.SIGINT):
                loop.remove_signal_handler(sig)
    return asyncio.run(bounded())


if __name__=='__main__':
    raise SystemExit(main())
