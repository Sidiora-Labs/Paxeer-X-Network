from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


class PersistenceError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise PersistenceError(message)


def load_bound(path, digest):
    path = Path(path)
    require(path.is_absolute() and not path.is_symlink() and path.is_file(), 'regular absolute input required')
    raw = path.read_bytes()
    require(len(raw) <= 4 * 1024 * 1024 and hashlib.sha256(raw).hexdigest() == digest, 'input binding differs')
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'duplicate JSON field')
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=unique)


class PostgreSQL:
    def __init__(self, configuration):
        self.configuration = configuration
        tool = Path(configuration['tool']['path'])
        require(tool.is_absolute() and tool.is_file() and not tool.is_symlink(), 'bound psql executable required')
        require(hashlib.sha256(tool.read_bytes()).hexdigest() == configuration['tool']['sha256'], 'psql executable changed')
        self.tool = str(tool)
        service = Path(configuration['service_file'])
        require(service.is_absolute() and service.is_file() and not service.is_symlink(), 'private PostgreSQL service file required')
        require(hashlib.sha256(service.read_bytes()).hexdigest() == configuration['service_sha256'], 'PostgreSQL service authority changed')
        require(re.fullmatch(r'[a-zA-Z0-9_-]{1,64}', configuration['service']) is not None, 'bounded service required')
        import configparser
        import ipaddress
        services = configparser.ConfigParser(interpolation=None)
        services.read(service)
        require(configuration['service'] in services, 'PostgreSQL service entry absent')
        selected = services[configuration['service']]
        require(ipaddress.ip_address(selected.get('host', '')).is_loopback and selected.get('dbname') == configuration['database'], 'PostgreSQL fixture must be loopback and exact database')
        require(selected.get('port', '').isdigit() and 1024 <= int(selected['port']) <= 65535, 'allocated PostgreSQL fixture port required')
        self.env = {**os.environ, 'PGSERVICEFILE': str(service), 'PGSERVICE': configuration['service'], 'PGCONNECT_TIMEOUT': '3', 'PGOPTIONS': '-c statement_timeout=10000 -c default_transaction_read_only=on'}

    def query(self, sql, values):
        argv = [self.tool, '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1']
        for key, value in values.items():
            require(re.fullmatch('[a-z_]+', key) and isinstance(value, str) and len(value) <= 512, 'invalid SQL parameter')
            argv += ['-v', key + '=' + value]
        result = subprocess.run(argv, input=sql.encode(), env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False)
        require(result.returncode == 0, 'actual PostgreSQL read failed; exit=' + str(result.returncode))
        require(len(result.stdout) <= 4 * 1024 * 1024, 'PostgreSQL output bound exceeded')
        return json.loads(result.stdout)

    def snapshot(self, wallet, pool, marker):
        require(re.fullmatch('0x[0-9a-f]{40}', wallet) and re.fullmatch('0x[0-9a-f]{40}', pool), 'normalized wallet and pool required')
        return self.query(SNAPSHOT_SQL, {'wallet': wallet, 'pool': pool, 'marker': marker})


SNAPSHOT_SQL = """
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT jsonb_build_object(
 'database', current_database(), 'observed_at', clock_timestamp(),
 'watchlist', (SELECT coalesce(jsonb_agg(to_jsonb(w) ORDER BY w.pool),'[]') FROM users.account_watch w JOIN users.account_state a USING(account_id) WHERE a.wallet=:'wallet' AND w.pool=:'pool'),
 'account_outbox', (SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY o.revision,o.event_id),'[]') FROM users.account_outbox o JOIN users.account_state a USING(account_id) WHERE a.wallet=:'wallet'),
 'chat', (SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.created_at,m.id),'[]') FROM chat.chat_messages m WHERE m.conversation_id='pool:' || :'pool' AND m.sender=:'wallet' AND m.content=:'marker'),
 'created_pools', (SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.created_block,p.pool_address),'[]') FROM indexer.pools p WHERE p.creator=:'wallet'),
 'chat_outbox', (SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY o.event_id),'[]') FROM chat.chat_outbox o JOIN chat.chat_messages m ON m.id=o.message_id WHERE m.conversation_id='pool:' || :'pool' AND m.sender=:'wallet' AND m.content=:'marker'),
 'fills', (SELECT coalesce(jsonb_agg(payload ORDER BY log_index),'[]') FROM indexer.indexer_accounting_fills WHERE pool_address=:'pool'),
 'stats', (SELECT coalesce(jsonb_agg(to_jsonb(f) ORDER BY height,transaction_index,log_index),'[]') FROM stats.trade_facts f WHERE f.pool=:'pool'),
 'buckets', (SELECT coalesce(jsonb_agg(to_jsonb(b) ORDER BY start,projection_version),'[]') FROM stats.minute_buckets b WHERE b.pool=:'pool'),
 'stats_outbox', (SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY event_id,projection_version),'[]') FROM stats.outbox o WHERE o.event_id IN (SELECT event_id FROM stats.trade_facts WHERE pool=:'pool')),
 'charts', (SELECT coalesce(jsonb_agg(snapshot ORDER BY interval,start),'[]') FROM candles.chart_exact WHERE pool=:'pool'),
 'chart_outbox', (SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY id),'[]') FROM candles.chart_outbox o WHERE pool=:'pool'),
 'source_outbox', (SELECT coalesce(jsonb_agg(jsonb_build_object('event_id',o.event_id,'subject',o.subject,'trace_id',convert_from(o.envelope,'UTF8')::jsonb->>'trace_id','envelope_base64',encode(o.envelope,'base64'),'state',o.state,'broker_stream',o.broker_stream,'broker_sequence',o.broker_sequence) ORDER BY broker_sequence),'[]') FROM indexer.delivery_outbox o WHERE convert_from(o.envelope,'UTF8')::jsonb->'payload'->>'sourceFillId' IN (SELECT source_fill_id FROM indexer.indexer_accounting_fills WHERE pool_address=:'pool')),
 'stats_inbox', (SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY event_id),'[]') FROM stats.delivery_inbox i),
 'pnl_inbox', (SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY event_id),'[]') FROM pnl.delivery_inbox i),
 'candles_inbox', (SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY event_id),'[]') FROM candles.delivery_inbox i),
 'positions', (SELECT coalesce(jsonb_agg(jsonb_build_object('partition',partition,'state',state)),'[]') FROM pnl.inventory_l16 WHERE partition LIKE '125:' || :'wallet' || ':%')
);
COMMIT;
"""


def assert_persistence(snapshot, browser):
    require(len(snapshot['watchlist']) == 1, 'browser watchlist did not persist exactly once in PostgreSQL')
    require(len(snapshot['chat']) == 1, 'browser chat did not persist exactly once in PostgreSQL')
    require(len(snapshot['chat_outbox']) == 1 and snapshot['chat_outbox'][0]['message_id'] == snapshot['chat'][0]['id'], 'real chat transaction outbox missing')
    require(any(frame.get('type')=='message_persisted' and frame.get('id')==snapshot['chat'][0]['id'] and frame.get('client_message_id')==snapshot['chat'][0]['client_id'] for frame in browser['frames']), 'browser durable message acknowledgement does not match PostgreSQL')
    require(any(row['kind'] == 'watchlist.changed' for row in snapshot['account_outbox']), 'watchlist transaction outbox missing')
    require(snapshot['stats'] and snapshot['fills'] and snapshot['source_outbox'], 'actual indexed trade and transport evidence missing')
    captured_hashes = {item['transactionHash'].lower() for item in browser['transactions'] if item['status'] == '0x1' and item['journey'] == 'trade'}
    actual_hashes = {item['position']['transactionHash'].lower() for item in snapshot['fills']}
    require(captured_hashes and captured_hashes <= actual_hashes, 'browser successful receipt absent from canonical persisted facts')
    require(browser['http_request_ids'], 'actual API trace IDs missing')
    creations = {item['transactionHash'].lower() for item in browser['transactions'] if item['journey'] == 'create'}
    require(creations and creations <= {row['tx_hash'].lower() for row in snapshot['created_pools']}, 'browser creation missing from actual indexed pool persistence')
    records = []
    for row in snapshot['source_outbox']:
        require(row['state'] == 'published' and row['broker_sequence'] > 0 and row['broker_stream'], 'source outbox not durably acknowledged')
        require(row['trace_id'], 'source envelope trace identity missing')
        facet = 'market' if row['subject']=='accounting.market.v1' else 'inventory'
        consumers = ('stats_inbox','candles_inbox') if facet=='market' else ('pnl_inbox',)
        for consumer in consumers:
            require(any(item['event_id']==row['event_id'] for item in snapshot[consumer]), 'actual source publication missing from committed consumer inbox: '+consumer)
        records.append({'publication_id':row['event_id'],'trace_id':row['trace_id'],'broker_stream':row['broker_stream'],'broker_sequence':row['broker_sequence'],'consumers':list(consumers)})
    return {'persisted_watchlist': 1, 'persisted_chat': 1, 'receipt_transactions': len(captured_hashes), 'source_publications': len(snapshot['source_outbox']), 'publication_join': records, 'browser_request_ids': browser['http_request_ids'], 'chat_message_id':snapshot['chat'][0]['id']}


def assert_duplicate_invariant(before, after):
    for key in ('fills', 'stats', 'buckets', 'stats_outbox', 'charts', 'chart_outbox', 'positions', 'stats_inbox', 'candles_inbox', 'pnl_inbox'):
        require(before[key] == after[key], 'duplicate delivery changed durable business state: ' + key)


ADVERSARIAL_SQL = """
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT jsonb_build_object(
 'database',current_database(),'observed_at',clock_timestamp(),
 'user_images',(SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY id),'[]') FROM users.user_images i WHERE wallet_address=:'wallet'),
 'user_profiles',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY wallet_address),'[]') FROM users.user_profiles p WHERE wallet_address=:'wallet'),
 'streams',(SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY id),'[]') FROM livestream.stream_operations s WHERE wallet=:'wallet'),
 'stream_outbox',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY o.id),'[]') FROM livestream.stream_outbox o JOIN livestream.stream_operations s ON s.id=o.operation_id WHERE s.wallet=:'wallet'),
 'stream_inbox',(SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY event_id),'[]') FROM livestream.stream_inbox i),
 'heads',(SELECT coalesce(jsonb_agg(to_jsonb(h) ORDER BY stream_id,height),'[]') FROM indexer.indexer_committed_heads h WHERE chain_id=125),
 'quarantine',(SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY stream_id,height,log_index),'[]') FROM indexer.indexer_quarantine q WHERE chain_id=125 AND required AND resolution='unresolved')
);
COMMIT;
"""


def adversarial_snapshot(database, wallet):
    require(re.fullmatch('0x[0-9a-f]{40}', wallet), 'normalized wallet required')
    return database.query(ADVERSARIAL_SQL, {'wallet': wallet})


def assert_webhook_unchanged(before, after):
    for key in ('streams', 'stream_outbox', 'stream_inbox', 'user_images', 'user_profiles'):
        require(before[key] == after[key], 'denied webhook changed persisted lifecycle: '+key)


def assert_acknowledged_preserved(before, after):
    identity = {'watchlist':'pool', 'account_outbox':'event_id', 'chat':'id',
                'chat_outbox':'event_id', 'created_pools':'pool_address',
                'fills':'sourceFillId', 'stats':'event_id', 'stats_outbox':'event_id',
                'chart_outbox':'id', 'stats_inbox':'event_id', 'pnl_inbox':'event_id',
                'candles_inbox':'event_id'}
    require(before['database'] == after['database'], 'recovery database identity differs')
    for key, column in identity.items():
        recovered = {str(row[column]):row for row in after[key]}
        require(len(recovered) == len(after[key]), 'duplicate durable identity after recovery: '+key)
        for row in before[key]:
            require(str(row[column]) in recovered and row == recovered[str(row[column])], 'acknowledged durable row lost or changed: '+key)
    for key in ('charts', 'buckets', 'positions'):
        require(before[key] == after[key], 'recovery changed quiesced business projection: '+key)


def assert_rpc_quarantine(before, blocked, affected_stream, conflicting_height):
    require(isinstance(affected_stream,str) and affected_stream and type(conflicting_height) is int and conflicting_height >= 0, 'exact affected stream/height required')
    original = [row for row in before['heads'] if row['stream_id']==affected_stream]
    observed = [row for row in blocked['heads'] if row['stream_id']==affected_stream]
    require(original, 'affected canonical stream had no persisted baseline')
    original_hashes = {str(row['height']):row['block_hash'] for row in original}
    observed_hashes = {str(row['height']):row['block_hash'] for row in observed}
    require(all(observed_hashes.get(height)==digest for height,digest in original_hashes.items()), 'committed history rolled back or replaced during disagreement')
    require(max(int(row['height']) for row in observed) <= max(int(row['height']) for row in original), 'affected canonical advancement continued during RPC disagreement')
    require(any(row['integrity']=='quarantined' for row in observed) or any(row['stream_id']==affected_stream and int(row['height'])<=conflicting_height for row in blocked['quarantine']), 'actual persisted integrity quarantine absent')


def consumer_snapshot(database, consumer):
    schema=consumer['schema']
    require(schema in ('indexer','stats','candles','pnl','users','chat','metadata','livestream','ranking'), 'unadmitted durable consumer schema')
    tables=consumer.get('business_tables',[])
    require(tables and len(tables)==len(set(tables)) and all(re.fullmatch('[a-z][a-z0-9_]{0,62}',table) and not any(word in table for word in ('credential','session','refresh','challenge','secret','key','ticket')) for table in tables), 'exact non-secret business tables required')
    selections=[]
    for table in tables:
        selections.extend(["'"+table+"'",'(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),\'[]\') FROM "'+schema+'"."'+table+'" t)'])
    selections.extend(["'inbox'",'(SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY projection_generation,event_id),\'[]\') FROM "'+schema+'".delivery_inbox i WHERE consumer_id=:\'consumer\')',"'outbox'",'(SELECT coalesce(jsonb_agg(jsonb_build_object(\'event_id\',event_id,\'subject\',subject,\'payload_hash\',payload_hash,\'envelope\',encode(envelope,\'base64\')) ORDER BY event_id),\'[]\') FROM "'+schema+'".delivery_outbox)'])
    result=database.query('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT jsonb_build_object('+','.join(selections)+'); COMMIT;',{'consumer':consumer['id']})
    require(result['inbox'], 'consumer has no actual committed inbox baseline')
    return result


RECOVERY_CLASSES = frozenset({'profiles','chat','reward_ledger','objects_metadata','deployment_registry','business','outbox','inbox','watermarks','broker_positions'})


def recovery_snapshot_sql(tables, *, commit_timestamps):
    require(isinstance(tables,list) and tables and {v['class'] for v in tables}==RECOVERY_CLASSES, 'all non-chain and durable replay recovery classes required')
    names=set(); selections=[]
    for item in tables:
        require(set(item)=={'name','class','schema','table','keys'}, 'exact recovery table inventory required')
        require(all(isinstance(item[key],str) and re.fullmatch('[a-z][a-z0-9_]{0,62}',item[key]) for key in ('name','schema','table')), 'frozen exact recovery table identifiers required')
        require(item['name'] not in names and item['keys'] and len(set(item['keys']))==len(item['keys']) and all(re.fullmatch('[a-z][a-z0-9_]{0,62}',key) for key in item['keys']), 'unique table and primary key inventory required')
        names.add(item['name'])
        require(not any(word in item['table'] for word in ('credential','secret','refresh','session','challenge')), 'private credential tables cannot enter recovery artifacts')
        source='"'+item['schema']+'"."'+item['table']+'"'
        identity='jsonb_build_array('+','.join('t."'+key+'"' for key in item['keys'])+')'
        fields="'identity',"+identity+",'raw_value',to_jsonb(t)::text"
        if commit_timestamps:fields+=",'committed_utc_ns',((extract(epoch FROM pg_xact_commit_timestamp(t.xmin))*1000000000)::numeric(30,0))::text"
        selections.extend(["'"+item['name']+"'",'(SELECT coalesce(jsonb_agg(jsonb_build_object('+fields+') ORDER BY '+identity+"::text),'[]') FROM "+source+' t)'])
    return "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL TIME ZONE 'UTC'; SELECT jsonb_build_object("+','.join(selections)+'); COMMIT;'


def recovery_checksums(snapshot, tables):
    require(set(snapshot)=={v['name'] for v in tables}, 'missing authoritative recovery table output')
    checksums={}
    for item in tables:
        rows=snapshot[item['name']]
        require(isinstance(rows,list), 'actual recovery row inventory required')
        seen=set(); normalized=[]
        for row in rows:
            require(isinstance(row,dict) and set(row) in ({'identity','raw_value'},{'identity','raw_value','committed_utc_ns'}), 'recovery row shape differs')
            identity=json.dumps(row['identity'],sort_keys=True,separators=(',',':'),allow_nan=False)
            require(identity not in seen and None not in row['identity'], 'missing or duplicated durable recovery identity')
            seen.add(identity)
            require(isinstance(row['raw_value'],str) and isinstance(json.loads(row['raw_value'],parse_float=str),dict) and row['raw_value']!='{}', 'exact PostgreSQL raw JSON text required; no binary-float normalization')
            normalized.append({'identity':row['identity'],'raw_value':row['raw_value']})
        raw=json.dumps(sorted(normalized,key=lambda r:json.dumps(r['identity'],sort_keys=True,separators=(',',':'))),sort_keys=True,separators=(',',':'),allow_nan=False).encode()
        checksums[item['name']]={'class':item['class'],'count':len(rows),'sha256':hashlib.sha256(raw).hexdigest()}
    require(all(any(checksums[t['name']]['count']>0 for t in tables if t['class']==group) for group in RECOVERY_CLASSES), 'empty recovery data class cannot establish restore correctness')
    return checksums


def assert_restored_checkpoint(expected, actual, tables):
    before=recovery_checksums(expected,tables)
    after=recovery_checksums(actual,tables)
    require(before==after, 'restored authoritative business, outbox, inbox or broker cursor checksum differs')
    commit_times=[]
    for rows in expected.values():
        for row in rows:
            value=row.get('committed_utc_ns')
            require(isinstance(value,str) and re.fullmatch('[1-9][0-9]{0,29}',value), 'actual PostgreSQL transaction commit timestamp unavailable')
            commit_times.append(int(value))
    require(commit_times, 'no acknowledged committed recovery rows')
    return {'checksums':before,'last_restored_commit_utc_ns':str(max(commit_times)),'acknowledged_rows_lost':0}


def assert_candle_recovery(before, blocked, after, browser):
    require(before['charts'] == blocked['charts'] and before['chart_outbox'] == blocked['chart_outbox'], 'uncommitted candle was visible or published')
    require(len(browser['candle_fill_hashes']) == 2 and len(set(browser['candle_fill_hashes'])) == 2, 'two distinct concurrent mined fills required')
    hashes = {f['position']['transactionHash'].lower() for f in after['fills']}
    require(set(browser['candle_fill_hashes']) <= hashes, 'concurrent fills missing after recovery')
    require(len(after['charts']) >= 7 and after['chart_outbox'], 'seven interval committed candle/outbox outputs required')


def assert_accounting(snapshot, wallet, token):
    from fractions import Fraction
    from oracle import INTERVALS
    fills = sorted(snapshot['fills'], key=lambda f: tuple(int(f['position'][key]) for key in ('height', 'transactionIndex', 'logIndex')))
    require(fills and len({f['sourceFillId'] for f in fills}) == len(fills), 'canonical fill identities missing or duplicated')
    import base64
    inventory_attribution = {}
    for row in snapshot['source_outbox']:
        publication = json.loads(base64.b64decode(row['envelope_base64']))['payload']
        if publication['facet'] == 'inventory':
            enrichment = publication['enrichment']
            require(enrichment['sourceFillId'] == publication['sourceFillId'] and enrichment['sourceFactDigest'] == publication['sourceFactDigest'], 'inventory enrichment source binding differs')
            require(publication['sourceFillId'] not in inventory_attribution, 'multiple inventory attribution revisions require explicit reconciliation')
            inventory_attribution[publication['sourceFillId']] = enrichment['attribution']
    expected = {}
    known, cost, realized = 0, 0, 0
    for fill in fills:
        require(fill['settlement']['decimals'] == 6 and fill['token']['address'].lower() == token.lower(), 'accounting assets differ')
        quantity, usdl = int(fill['tokenAmountRaw']), int(fill['usdlAmountRaw'])
        require(quantity > 0 and usdl > 0, 'positive actual execution amounts required')
        price = Fraction(usdl * 10 ** fill['token']['decimals'], quantity * 10 ** 6)
        timestamp = int(fill['blockTimestampSeconds'])
        order = tuple(int(fill['position'][key]) for key in ('height', 'transactionIndex', 'logIndex'))
        for interval, seconds in INTERVALS.items():
            anchor = 345600 if interval == '1w' else 0
            opened = (timestamp-anchor)//seconds*seconds+anchor
            key = (fill['poolAddress'], interval, opened)
            if key not in expected:
                expected[key] = {'open':price, 'high':price, 'low':price, 'close':price, 'volumeUsdlRaw':0, 'volumeTokenRaw':0, 'buyVolumeUsdlRaw':0, 'sellVolumeUsdlRaw':0, 'trades':0, 'first':order, 'last':order}
            bar = expected[key]
            bar['high'] = max(bar['high'], price)
            bar['low'] = min(bar['low'], price)
            bar['close'] = price
            bar['volumeUsdlRaw'] += usdl
            bar['volumeTokenRaw'] += quantity
            bar[('buy' if fill['side']=='buy' else 'sell')+'VolumeUsdlRaw'] += usdl
            bar['trades'] += 1
            bar['last'] = order
        attribution = inventory_attribution.get(fill['sourceFillId'])
        require(attribution and attribution['quality'] in ('exact_receipt', 'qualified_calldata') and attribution['evidence'], 'real source-bound inventory recipient provenance required')
        if fill['side'] == 'buy' and (attribution.get('recipient') or '').lower() == wallet:
            known += quantity
            cost += usdl
        elif fill['side'] == 'sell' and (attribution.get('payer') or '').lower() == wallet:
            require(known >= quantity, 'complete known inventory history required')
            allocated = cost if quantity == known else cost * quantity // known
            known -= quantity
            cost -= allocated
            realized += usdl - allocated
    actual = {(row['poolAddress'], row['interval'], row['start']):row for row in snapshot['charts']}
    require(len(actual) == len(snapshot['charts']) and actual.keys() == expected.keys(), 'actual candle bucket inventory differs from independent computation')
    for key, want in expected.items():
        got = actual[key]
        for field in ('open', 'high', 'low', 'close'):
            require(Fraction(int(got[field]['numerator']), int(got[field]['denominator'])) == want[field], 'actual exact candle price differs: '+field)
        for field in ('volumeUsdlRaw', 'volumeTokenRaw', 'buyVolumeUsdlRaw', 'sellVolumeUsdlRaw', 'trades'):
            require(int(got[field]) == want[field], 'actual exact candle fold differs: '+field)
        for field, oracle_key in (('firstPosition','first'),('lastPosition','last')):
            require(tuple(int(got[field][name]) for name in ('height','transactionIndex','logIndex')) == want[oracle_key], 'actual candle canonical order differs')
    versions = {row['projection_version'] for row in snapshot['stats']}
    require(versions, 'actual statistics projection absent')
    for version in versions:
        rows = [row for row in snapshot['stats'] if row['projection_version']==version]
        require(len(rows)==len(fills), 'statistics counted an execution more or less than once')
        require({row['event_id'] for row in rows}=={fill['sourceFillId'] for fill in fills}, 'statistics execution identities differ')
        require(sum(int(row['volume']) for row in rows)==sum(int(fill['usdlAmountRaw']) for fill in fills), 'statistics volume differs from independent canonical fill sum')
    positions = [row['state'] for row in snapshot['positions'] if row['partition']=='125:'+wallet+':'+token.lower()]
    require(len(positions)==1, 'real recipient inventory position missing or duplicated')
    position = positions[0]
    require(position['unknownQuantityRaw']=='0', 'unknown transfer basis cannot be represented as known zero cost')
    require(int(position['knownQuantityRaw'])==known and int(position['knownRemainingCostUsdlRaw'])==cost and int(position['knownRealizedSubtotalUsdlRaw'])==realized, 'recipient remaining inventory basis or realized subtotal differs from independent ledger')
    return {'candles':len(actual), 'fills':len(fills), 'remaining_quantity_raw':str(known), 'remaining_cost_usdl_raw':str(cost), 'known_realized_usdl_raw':str(realized)}
