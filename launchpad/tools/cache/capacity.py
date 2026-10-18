from __future__ import annotations

import argparse
import asyncio
import base64
import dataclasses
from datetime import datetime, timezone
import hashlib
import http.client
import json
import math
import os
from pathlib import Path
import secrets
import ssl
import sys
import time
import uuid
from urllib.parse import urlencode

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tools/production/load'))
from phase2_accounts import WalletSigner, Account
from phase2_feeder import PostgresObserver
from phase2_runner import resolve_routes, route_category
from phase2_types import Bindings, bound_document, canonical, digest, origin, unique, validate_v2
from phase3_runner import Histogram, IsolatedFaultControl, pool_for
from scheduler import Sample
from workload import MIX, VERSION, ledger
from calibration import linux_cpu_snapshot, cpu_fraction, nic_fraction


def source_binding():
    paths = [ROOT / 'tools/cache/capacity.py', ROOT / 'core/api/internal/cache/adapter.go']
    paths += sorted((ROOT / 'tools/production/load').glob('*.py'))
    return digest(canonical({str(p.relative_to(ROOT)): digest(p.read_bytes()) for p in paths}))


def allocation(config):
    profile = config['profile']
    if profile not in ('soak', 'surge', 'recovery'):
        raise ValueError('explicit soak, surge or recovery profile required')
    duration = config['duration_seconds']
    if type(duration) is not int or not 0 < duration <= 86400 or (profile == 'soak' and duration != 86400) or (profile != 'soak' and duration < 60):
        raise ValueError('exact twenty-four-hour soak or at least sixty-second surge/recovery required')
    rate = 20000 if profile == 'surge' else 10000
    if config['aggregate_rps'] != rate or config['generator_hosts'] != 4 or type(config['host_index']) is not int or not 0 <= config['host_index'] < 4:
        raise ValueError('frozen aggregate rate and four independent generators required')
    if type(config['max_inflight']) is not int or not 1 <= config['max_inflight'] <= 4096 or type(config['request_timeout_seconds']) not in (int,float) or not math.isfinite(config['request_timeout_seconds']) or not 0 < config['request_timeout_seconds'] <= 10:
        raise ValueError('explicit bounded generator resources required')
    if not 0 <= config['target_hit_fraction'] <= 1 or not 0 <= config['hit_fraction_tolerance'] <= .01:
        raise ValueError('explicit tight configured hit distribution required')
    if type(config['account_count']) is not int or not 2 <= config['account_count'] <= 10000: raise ValueError('bounded actual identity population required')
    if not isinstance(config['eviction_queries'],dict) or not set(config['eviction_queries']) <= set(MIX): raise ValueError('source-bound accepted public query corpus required')
    if not 0 < config['max_origin_qps'] or not 0 < config['max_pool_wait_ns']:
        raise ValueError('explicit approved origin and database wait budgets required')
    return rate // 4, duration


class CaptureHTTP:
    def __init__(self, config):
        self.base = origin(config['gateway_origin'], ('http','https') if config['owned_local'] else ('https',))
        if self.base.scheme == 'http' and self.base.hostname not in ('127.0.0.1','localhost','::1'):
            raise ValueError('plain HTTP must use owned loopback')
        origin(config['application_origin'])
        self.application_origin = config['application_origin']
        self.timeout = config['request_timeout_seconds']
        self.ssl = ssl.create_default_context(cafile=config['ca_file'])

    def request(self, method, path, body=None, *, bearer=None, headers=None):
        if not path.startswith('/') or path.startswith('//') or any(c in path for c in '\r\n\\') or len(path) > 8192:
            raise ValueError('bounded same-origin path required')
        raw = None if body is None else canonical(body)
        if raw is not None and len(raw) > 1048576:
            raise ValueError('request byte bound')
        h = {'Origin': self.application_origin, 'Accept':'application/json', 'X-Request-ID':str(uuid.uuid4())}
        if raw is not None: h['Content-Type'] = 'application/json'
        if bearer is not None: h['Authorization'] = 'Bearer '+bearer
        for k,v in (headers or {}).items():
            if k.lower() in ('host','authorization','origin') or '\r' in k+v or '\n' in k+v: raise ValueError('forbidden header override')
            h[k] = v
        conn = (http.client.HTTPSConnection(self.base.hostname,self.base.port,timeout=self.timeout,context=self.ssl) if self.base.scheme == 'https' else http.client.HTTPConnection(self.base.hostname,self.base.port,timeout=self.timeout))
        started=time.monotonic()
        try:
            conn.connect()
            remaining=self.timeout-(time.monotonic()-started)
            if remaining<=0: raise TimeoutError('HTTP total deadline')
            conn.sock.settimeout(remaining)
            conn.request(method,path,body=raw,headers=h)
            remaining=self.timeout-(time.monotonic()-started)
            if remaining<=0: raise TimeoutError('HTTP total deadline')
            conn.sock.settimeout(remaining)
            r = conn.getresponse()
            chunks,count=[],0
            while True:
                remaining=self.timeout-(time.monotonic()-started)
                if remaining<=0: raise TimeoutError('HTTP total deadline')
                if conn.sock is not None: conn.sock.settimeout(remaining)
                chunk=r.read1(min(65536,1048577-count))
                if not chunk: break
                chunks.append(chunk);count+=len(chunk)
                if count>1048576: raise ValueError('response byte bound')
            b=b''.join(chunks)
            value = json.loads(b,object_pairs_hook=unique) if b else None
            return r.status,dict((k.lower(),v) for k,v in r.getheaders()),value,digest(b)
        finally: conn.close()

    async def call(self,*args,**kwargs):
        return await asyncio.wait_for(asyncio.to_thread(self.request,*args,**kwargs),self.timeout+.1)


async def authenticate(http, signer, wallet=None):
    wallet = wallet or (await signer.call('create'))['wallet']
    status,_,value,_ = await http.call('POST','/v2/auth/challenges',{'wallet':wallet,'origin':http.application_origin},headers={'Idempotency-Key':str(uuid.uuid4())})
    if status != 201: raise ValueError('actual wallet challenge unavailable')
    validate_v2('ChallengeResponse',value)
    challenge = value['data']['challenge']
    if challenge['wallet'].lower() != wallet or challenge['chainId'] != '125' or challenge['purpose'] != 'login': raise ValueError('actual challenge identity mismatch')
    signature = (await signer.call('sign',wallet=wallet,message=challenge['message']))['signature']
    status,_,value,_ = await http.call('POST','/v2/auth/sessions',{'challengeId':challenge['challengeId'],'proof':{'kind':'eoa_eip191','wallet':wallet,'signature':signature},'deviceLabel':'Cache capacity generator'})
    if status != 200: raise ValueError('actual wallet login unavailable')
    validate_v2('LoginResponse',value)
    data = value['data']
    if data['session']['wallet'].lower() != wallet: raise ValueError('wallet session mismatch')
    return Account(wallet,data['session']['sessionId'],data['accessToken'],data['session']['accessExpiresAt'])


async def write_headers(config, signer, account, route, path, body):
    envelope = {'version':2,'chainId':'125','environment':config['operation_environment'],'actor':account.wallet,'action':'watchlist.update','method':route['method'],'path':path,'bodyHash':digest(canonical(body)),'nonce':secrets.token_hex(32),'issuedAt':int(time.time()),'expiresAt':int(time.time())+120}
    statement = 'KindleLaunch signed operation v2\n'+canonical(envelope).decode()
    signature = (await signer.call('sign',wallet=account.wallet,message=statement))['signature']
    return {'X-Kindle-Operation':base64.urlsafe_b64encode(canonical(envelope)).decode().rstrip('='),'X-Kindle-Operation-Signature':signature}


def append(output,row):
    output.write(canonical(row).decode()+'\n')


def histogram_delta(current, previous):
    if len(current) != 32 or len(previous) != 32 or any(type(v) is not int or v < 0 for v in current+previous): raise ValueError('actual histogram required')
    result = [a-b for a,b in zip(current,previous)]
    if min(result) < 0: raise ValueError('measurement counter reset')
    return result


def percentile_buckets(buckets,fraction):
    total = sum(buckets)
    if not total: return None
    target,count = math.ceil(total*fraction),0
    for i,n in enumerate(buckets):
        count += n
        if count >= target: return (1<<i)*1000
    raise ValueError('histogram incomplete')


class Metrics:
    def __init__(self, config, output):
        self.config,self.output = config,output
        self.previous = {}
        self.origins,self.waits,self.hits,self.reads,self.max_bytes,self.evictions = 0,[0]*32,0,0,0,0
        self.outage_rejections = 0
        self.maximum_origin_qps = 0
        if len(config['replicas']) != 10 or len({r['replica_id'] for r in config['replicas']}) != 10: raise ValueError('ten actual replica metric sources required')

    async def sample(self):
        for replica in self.config['replicas']:
            http = CaptureHTTP({**self.config,'gateway_origin':replica['origin']})
            token = os.environ.get(replica['token_env'],'')
            if not token: raise ValueError('actual administrative metrics credential unavailable')
            status,_,row,_ = await http.call('GET',replica['path'],bearer=token)
            if status != 200 or row['replicaId'] != replica['replica_id'] or row['candidateDigest'] != self.config['bindings']['candidate']: raise ValueError('actual current replica measurement binding mismatch')
            observed = datetime.fromisoformat(row['observedAt'].replace('Z','+00:00')).timestamp()
            if abs(time.time()-observed) > 5: raise ValueError('stale replica metrics')
            stats = row['stats']
            if stats['Bytes'] > row['maxBytes'] or row['maxBytes'] <= 0: raise ValueError('cache memory budget exceeded')
            self.max_bytes = max(self.max_bytes,stats['Bytes'])
            now = time.monotonic_ns()
            old = self.previous.get(replica['replica_id'])
            series = {(s['route'],s['class'],s['event']):s for s in row['series']}
            if old:
                before,then = old
                origin_count = 0
                if not set(before).difference({('__stats__',)}) <= set(series): raise ValueError('replica measurement series disappeared')
                for key,s in series.items():
                    prior = before.get(key,{'count':0,'durationBuckets':[0]*32})
                    delta = s['count']-prior['count']
                    if delta < 0: raise ValueError('replica metric reset')
                    buckets = histogram_delta(s['durationBuckets'],prior['durationBuckets'])
                    if key[2] == 'origin': origin_count += delta; self.origins += delta
                    if key[2] == 'database_pool_wait': self.waits = [a+b for a,b in zip(self.waits,buckets)]
                    if key[2] in ('hit','stale','origin'): self.reads += delta
                    if key[2] == 'hit': self.hits += delta
                    if key[2] in ('request_rejected','origin_rejected','redis_unavailable'): self.outage_rejections += delta
                qps = origin_count*1e9/(now-then)
                self.maximum_origin_qps = max(self.maximum_origin_qps,qps)
                if qps > self.config['max_origin_qps']: raise ValueError('allocated per-replica origin QPS exceeded')
            self.previous[replica['replica_id']] = series,now
            self.evictions += stats['Evictions']-(old[0].get(('__stats__',),{}).get('Evictions',0) if old else stats['Evictions'])
            series[('__stats__',)] = stats
            append(self.output,{'observed_monotonic_ns':now,'capture':row})
        if self.maximum_origin_qps>self.config['max_origin_qps']: raise ValueError('origin query budget exceeded')


async def truth(observer):
    queries = []
    for name,table in [('balances','pnl.portfolio_snapshots'),('cursors','indexer.indexer_replay_checkpoints'),('mutations','users.operation_ledger')]:
        queries.append("SELECT json_build_object('class','"+name+"','count',count(*),'rows',COALESCE(json_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::json)) FROM "+table+' t')
    rows = await observer.query(' UNION ALL '.join(queries))
    if len(rows) != 3 or any(r['count'] <= 0 for r in rows): raise ValueError('actual populated durable balances,cursors,mutation records required')
    return [{'class':r['class'],'count':r['count'],'sha256':digest(canonical(r['rows']))} for r in rows]

async def traffic(config,http,signer,accounts,output,start,rate,duration):
    routes = resolve_routes(config['routes'])
    hist = {key:Histogram() for key in ('all','public_reads','quotes','writes')}
    categories = dict.fromkeys(MIX,0)
    statuses,stale = {},0
    pending = set()
    async def execute(sample,category,group,pool):
        nonlocal stale
        account = accounts[sample.id%len(accounts)]
        route = routes[category]
        path = route['template']
        for key,value in {'wallet':account.wallet,'poolAddress':pool,'tokenAddress':config['tokens'][sample.id%len(config['tokens'])],'category':config['ranking_category']}.items(): path = path.replace('{'+key+'}',value)
        if '{' in path or '}' in path: raise ValueError('unresolved actual route authority')
        query = dict(route.get('query') or {})
        eviction_queries = config['eviction_queries'].get(category,[])
        if eviction_queries and group == 'public_reads': query.update(eviction_queries[sample.id%len(eviction_queries)])
        if query: path += '?'+urlencode(query)
        sample.attempted_ns = time.monotonic_ns()
        status,response_digest,cache_stale,retry_after = 0,None,False,None
        try:
            expiry = datetime.fromisoformat(account.expires_at.replace('Z','+00:00')).timestamp()
            if expiry <= time.time(): raise ValueError('actual session expired')
            body = route.get('body')
            if route['request_schema']: validate_v2(route['request_schema'],body)
            headers = await write_headers(config,signer,account,route,path,body) if category == 'writes' else {}
            status,response_headers,value,response_digest = await http.call(route['method'],path,body,bearer=account.access_token if route['authenticated'] else None,headers=headers)
            retry_after = response_headers.get('retry-after')
            cache_stale = response_headers.get('x-cache-stale') == 'true'
            if cache_stale and group != 'public_reads': raise ValueError('sensitive stale response')
            if status == 429 and (retry_after is None or not retry_after.isdecimal() or int(retry_after) < 1): raise ValueError('explicit positive Retry-After required')
            if status == 200:
                validate_v2(route['response_schema'],value)
                sample.status = 'received'
            else: sample.status,sample.error = 'error','http_'+str(status)
        except TimeoutError: sample.status = 'timeout'
        except Exception as error: sample.status,sample.error = 'error',type(error).__name__
        finally:
            sample.completed_ns = time.monotonic_ns()
            statuses[status] = statuses.get(status,0)+1
            stale += int(cache_stale)
            hist['all'].add(sample)
            if group in hist: hist[group].add(sample)
            append(output,{**dataclasses.asdict(sample),'category':category,'group':group,'scheduled_utc_ns':config['start_utc_ns']+sample.scheduled_ns-start,'http_status':status,'stale':cache_stale,'retry_after':retry_after,'response_sha256':response_digest})
    for index in range(rate*duration):
        sample = Sample(index,start+index*1000000000//rate)
        await asyncio.sleep(max(0,(sample.scheduled_ns-time.monotonic_ns())/1e9))
        category = route_category(index*4+config['host_index'])
        route = routes[category]
        group = 'quotes' if category == 'quotes' else ('writes' if category == 'writes' else ('public_reads' if route['method']=='GET' and not route['authenticated'] else 'private_reads'))
        categories[category] += 1
        pool = pool_for(index,config['pools'])
        if len(pending) >= config['max_inflight']:
            sample.completed_ns = time.monotonic_ns()
            hist['all'].add(sample)
            if group in hist: hist[group].add(sample)
            append(output,{**dataclasses.asdict(sample),'category':category,'group':group,'scheduled_utc_ns':config['start_utc_ns']+sample.scheduled_ns-start,'http_status':0,'stale':False,'retry_after':None,'response_sha256':None})
        else:
            task = asyncio.create_task(execute(sample,category,group,pool))
            pending.add(task)
            task.add_done_callback(pending.discard)
    if pending: await asyncio.gather(*tuple(pending))
    return {'categories':categories,'groups':{k:h.result() for k,h in hist.items()},'http_statuses':statuses,'stale_responses':stale,'offered_requests':rate*duration}


async def run(config,output,harness_check=False):
    rate,duration = allocation(config)
    bindings = Bindings(**config['bindings'])
    if bindings.generator != source_binding(): raise ValueError('current measurement/provider source binding mismatch')
    out = Path(output).resolve()
    if not out.is_relative_to((ROOT/'evidence/production-system/L09/phase4').resolve()): raise ValueError('lane-owned evidence output required')
    out.mkdir(mode=0o700,parents=True,exist_ok=False)
    files = {name:(out/(name+'.ndjson')).open('x',encoding='utf-8') for name in ('http-samples','metrics','fault-control','control-requests')}
    signer = WalletSigner(config['node'],config['node_sha256'],config['ethers_module'])
    http = CaptureHTTP(config)
    observer = PostgresObserver(config['psql'],config['psql_sha256'],config['postgres_environment'],stream_id=config['canonical_stream'],pools=config['pools'])
    controller = None
    tasks = []
    try:
        resolve_routes(config['routes'])
        await observer.readiness()
        metrics = Metrics(config,files['metrics'])
        await metrics.sample()
        if harness_check:
            accounts = [await authenticate(http,signer) for _ in range(2)]
            for account in accounts:
                append(files['control-requests'],{'event':'real_wallet_login','wallet_pseudonym':digest(account.wallet.encode()),'session_pseudonym':digest(account.session_id.encode())})
            return {'task':'4.09','harness_inputs_checked':True,'capacity_qualified':False,'final_capacity_task':'9.1','bindings':dataclasses.asdict(bindings)}
        from calibration import evaluate
        calibration = bound_document(config['calibration_path'],config['calibration_sha256'])
        if not evaluate(calibration,candidate=bindings.candidate,topology=bindings.topology,generator=bindings.generator)['generator_calibrated']: raise ValueError('actual four-host generator calibration failed')
        required = rate*duration*512
        if config['raw_disk_budget_bytes'] < required or os.statvfs(out).f_bavail*os.statvfs(out).f_frsize < required: raise ValueError('raw arrival capture disk allocation unavailable')
        accounts = [await authenticate(http,signer) for _ in range(config['account_count'])]
        if len(accounts) < 2: raise ValueError('distinct actual wallet accounts required')
        cpu_before = linux_cpu_snapshot()
        nic = Path('/sys/class/net')/config['generator_nic']
        tx_before,rx_before = int((nic/'statistics/tx_bytes').read_text()),int((nic/'statistics/rx_bytes').read_text())
        speed = int((nic/'speed').read_text())*1000000
        started_utc = config['start_utc_ns']
        if type(started_utc) is not int or started_utc-time.time_ns() < 1000000000: raise ValueError('agreed future four-host UTC start required')
        start = time.monotonic_ns()+started_utc-time.time_ns()
        async def monitor():
            for tick in range(duration//5+1):
                await asyncio.sleep(max(0,(start+tick*5000000000-time.monotonic_ns())/1e9))
                await metrics.sample()
        async def sessions():
            for tick in range(1,duration//30+1):
                await asyncio.sleep(max(0,(start+tick*30000000000-time.monotonic_ns())/1e9))
                for i,account in enumerate(tuple(accounts)):
                    expires = datetime.fromisoformat(account.expires_at.replace('Z','+00:00')).timestamp()
                    if expires-time.time() <= 90:
                        accounts[i] = await authenticate(http,signer,account.wallet)
                        append(files['control-requests'],{'event':'session_reauthenticated','observed_utc_ns':time.time_ns(),'wallet_pseudonym':digest(account.wallet.encode())})
        fault_result = None
        if config['profile'] == 'recovery' and config['host_index'] == 0:
            controller = IsolatedFaultControl(config['fault_control'])
            async def fault():
                nonlocal fault_result
                at,length = config['redis_loss_at_seconds'],config['redis_loss_seconds']
                if not 5 <= at < duration or not 12 <= length <= duration-at-config['request_timeout_seconds']-6: raise ValueError('bounded actual outage and recovery window required')
                await asyncio.sleep(max(0,(start+at*1000000000-time.monotonic_ns())/1e9))
                paused = await controller.pause('redis')
                began = time.monotonic_ns()
                await asyncio.sleep(config['request_timeout_seconds']+1)
                before = await truth(observer)
                await asyncio.sleep(length)
                after = await truth(observer)
                restored = await controller.resume('redis')
                ended = time.monotonic_ns()
                if before != after: raise ValueError('Redis loss changed persisted business truth')
                fault_result = {'started_ns':began,'ended_ns':ended,'started_utc_ns':paused['observed_utc_ns'],'ended_utc_ns':restored['observed_utc_ns'],'paused':paused,'restored':restored,'truth_before':before,'truth_after':after}
                append(files['fault-control'],fault_result)
            tasks.append(asyncio.create_task(fault()))
        tasks += [asyncio.create_task(monitor()),asyncio.create_task(sessions())]
        traffic_task = asyncio.create_task(traffic(config,http,signer,accounts,files['http-samples'],start,rate,duration))
        tasks.append(traffic_task)
        await asyncio.gather(*tasks)
        result = traffic_task.result()
        ended = time.monotonic_ns()
        cpu = cpu_fraction(cpu_before,linux_cpu_snapshot())
        nic_use = nic_fraction(tx_before=tx_before,rx_before=rx_before,tx_after=int((nic/'statistics/tx_bytes').read_text()),rx_after=int((nic/'statistics/rx_bytes').read_text()),duration_ns=ended-start,speed_bps=speed)
        public = result['groups']['public_reads']
        p99 = public['latency_ns_upper_bound']['0.99']
        lag = result['groups']['all']['scheduler_p99_ns_upper_bound']
        waits = percentile_buckets(metrics.waits,.99)
        hit = metrics.hits/metrics.reads if metrics.reads else None
        failures = []
        if cpu > .7 or nic_use > .7 or lag is None or lag > 10000000: failures.append('generator capacity or scheduler budget')
        if p99 is None or p99 > 400000000: failures.append('public read p99')
        if hit is None or abs(hit-config['target_hit_fraction']) > config['hit_fraction_tolerance']: failures.append('configured hit distribution')
        if waits is None or waits > config['max_pool_wait_ns']: failures.append('database pool wait budget')
        if config['profile']!='recovery':
            if public['availability'] is None or public['availability'] < .9995: failures.append('read success rate')
            for group in ('quotes','writes'):
                availability=result['groups'][group]['availability']
                if availability is None or availability < .999: failures.append(group+' success rate')
        if config['profile']=='recovery':
            if config['host_index']==0 and fault_result is None: failures.append('missing actual Redis outage')
            if metrics.evictions <= 0: failures.append('actual memory eviction not observed')
            if metrics.outage_rejections <= 0: failures.append('explicit outage admission or rejection not observed')
        return {'task':'4.09','profile':config['profile'],'duration_seconds':duration,'rate':rate,'host_id':config['host_id'],'host_index':config['host_index'],'started_utc_ns':started_utc,'ended_utc_ns':time.time_ns(),'bindings':dataclasses.asdict(bindings),'workload_version':VERSION,'http':result,'metrics':{'cache_bytes_peak':metrics.max_bytes,'evictions':metrics.evictions,'origin_queries':metrics.origins,'maximum_per_replica_origin_qps':metrics.maximum_origin_qps,'pool_wait_p99_upper_ns':waits,'hit_ratio':hit,'rejection_429_rate':result['http_statuses'].get(429,0)/result['offered_requests'],'stale_response_rate':result['stale_responses']/result['offered_requests']},'redis_fault':fault_result,'generator_cpu_fraction':cpu,'generator_nic_fraction':nic_use,'failures':failures,'lane_measurement_passed':not failures,'capacity_qualified':False,'monthly_availability_qualified':False,'final_capacity_task':'9.1'}
    finally:
        for task in tasks:
            if not task.done(): task.cancel()
        if tasks: await asyncio.gather(*tasks,return_exceptions=True)
        try:
            if controller is not None: await asyncio.shield(controller.restore())
        finally:
            signer.close()
            for file in files.values(): file.flush();os.fsync(file.fileno());file.close()


def aggregate(reports):
    if len(reports)!=4 or {r['host_index'] for r in reports}!={0,1,2,3} or len({r['host_id'] for r in reports})!=4: raise ValueError('four distinct actual hosts required')
    first = reports[0]
    for r in reports:
        if r['bindings']!=first['bindings'] or r['profile']!=first['profile'] or r['duration_seconds']!=first['duration_seconds'] or not r['lane_measurement_passed']: raise ValueError('host measurement mismatch or failed budget')
        if r['http']['offered_requests'] != r['rate']*r['duration_seconds'] or sum(r['http']['categories'].values()) != r['http']['offered_requests']: raise ValueError('offered load omitted or double counted')
    total = sum(r['http']['offered_requests'] for r in reports)
    rate = 20000 if first['profile']=='surge' else 10000
    if total != rate*first['duration_seconds'] or min(r['ended_utc_ns'] for r in reports)-max(r['started_utc_ns'] for r in reports) < first['duration_seconds']*1000000000: raise ValueError('shared wallclock traffic interval incomplete')
    return {'task':'4.09','profile':first['profile'],'offered_requests':total,'quotes_included':sum(r['http']['categories']['quotes'] for r in reports),'writes_included':sum(r['http']['categories']['writes'] for r in reports),'bindings':first['bindings'],'lane_measurement_passed':True,'capacity_qualified':False,'final_capacity_task':'9.1'}


def aggregate_bound_reports(paths):
    reports=[]
    for path in paths:
        path=Path(path)
        if path.is_symlink() or not path.is_file(): raise ValueError('actual report file required')
        report=json.loads(path.read_bytes(),object_pairs_hook=unique)
        for name,expected in report['artifact_sha256'].items():
            target=path.parent/name
            if Path(name).name != name or target.parent != path.parent or target.is_symlink(): raise ValueError('unsafe artifact path')
            h=hashlib.sha256()
            with target.open('rb') as source:
                for block in iter(lambda:source.read(1048576),b''): h.update(block)
            if h.hexdigest()!=expected: raise ValueError('actual raw artifact hash mismatch')
        counts=dict.fromkeys(MIX,0)
        hist={k:Histogram() for k in ('all','public_reads','quotes','writes')}
        total=0
        with (path.parent/'http-samples.ndjson').open() as raw:
            for line in raw:
                row=json.loads(line,object_pairs_hook=unique)
                fields={k:row[k] for k in Sample.__dataclass_fields__}
                sample=Sample(**fields)
                if sample.id!=total or row['category']!=route_category(total*4+report['host_index']) or row['scheduled_utc_ns']!=report['started_utc_ns']+total*1000000000//report['rate']: raise ValueError('raw offered arrival identity/schedule differs')
                if sample.status=='received' and row['http_status']!=200: raise ValueError('rejection claimed admitted')
                if sample.attempted_ns is not None and sample.completed_ns<sample.attempted_ns: raise ValueError('raw completion precedes attempt')
                counts[row['category']]+=1
                hist['all'].add(sample)
                if row['group'] in hist: hist[row['group']].add(sample)
                total+=1
        if total!=report['http']['offered_requests'] or counts!=report['http']['categories'] or {k:h.result() for k,h in hist.items()}!=report['http']['groups']: raise ValueError('raw arrival summary mismatch')
        reports.append(report)
    result=aggregate(reports)
    if result['profile']=='recovery':
        owners=[r['redis_fault'] for r in reports if r['redis_fault'] is not None]
        if len(owners)!=1: raise ValueError('one actual Redis outage controller required')
        fault=owners[0]
        rejections,arrivals=0,0
        for path in paths:
            with (Path(path).parent/'http-samples.ndjson').open() as samples:
                for line in samples:
                    row=json.loads(line,object_pairs_hook=unique)
                    # Boundary arrivals can overlap work already admitted before pause.
                    if fault['started_utc_ns']+11000000000 <= row['scheduled_utc_ns'] < fault['ended_utc_ns']:
                        arrivals+=1
                        if row['group']=='writes' and row['http_status']==200: raise ValueError('Redis outage reported mutation success')
                        if row['http_status'] in (429,503): rejections+=1
        if arrivals==0 or rejections==0 or fault['truth_before']!=fault['truth_after']: raise ValueError('outage rejection/truth evidence absent')
        result['redis_loss_arrivals']=arrivals
        result['redis_loss_rejections']=rejections
    return result


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--aggregate-reports',nargs='+')
    parser.add_argument('--config')
    parser.add_argument('--config-sha256')
    parser.add_argument('--output',required=True)
    parser.add_argument('--harness-check',action='store_true')
    args=parser.parse_args()
    if args.aggregate_reports:
        result=aggregate_bound_reports(args.aggregate_reports)
        Path(args.output).write_text(json.dumps(result,indent=2,allow_nan=False)+'\n')
        return
    if not args.config or not args.config_sha256: parser.error('actual source-bound configuration required')
    config=bound_document(args.config,args.config_sha256)
    output=Path(args.output)
    try:
        result=asyncio.run(run(config,output,args.harness_check))
    except BaseException as error:
        if output.is_dir(): (output/'failure.json').write_text(json.dumps({'task':'4.09','failure':type(error).__name__,'capacity_qualified':False})+'\n')
        raise
    checksums={}
    for path in sorted(output.iterdir()):
        if path.is_file():
            h=hashlib.sha256()
            with path.open('rb') as source:
                for block in iter(lambda:source.read(1048576),b''): h.update(block)
            checksums[path.name]=h.hexdigest()
    result['artifact_sha256']=checksums
    (output/'result.json').write_text(json.dumps(result,indent=2,allow_nan=False)+'\n')
    if result.get('failures'): raise SystemExit(1)


if __name__=='__main__': main()
