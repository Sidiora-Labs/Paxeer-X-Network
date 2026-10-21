from __future__ import annotations
import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from decimal import Decimal
from fractions import Fraction
import hashlib
import http.client
import importlib
import json
import os
from pathlib import Path
import re
import ssl
import sys
import tempfile
import time
from urllib.parse import urlencode, urlsplit
from . import v1
from .attestations import SignatureVerifier, regular_bytes
from .evidence import artifact, verify_bundle
from .providers import Kubernetes, connected_plan, validate_connected_plan


LOAD_FILES = ('capture.py','phase2_types.py','phase2_http.py','workload.py','final-shared-capture.schema.json')
NATIVE_FIELDS = set(v1.METRICS.values()) | {
    'postgres_allocated_bytes','postgres_free_bytes','postgres_derived_bytes','postgres_wal_bytes',
    'postgres_raw_payload_bytes','postgres_raw_row_index_bytes','postgres_application_connections',
    'raw_event_envelope_bytes','nats_stream_retention_seconds','nats_stream_replicas',
    'nats_stream_retained_logical_bytes',
    'postgres_vacuum_workspace_bytes','nats_allocated_bytes','nats_configured_store_bytes',
    'nats_broker_overhead_permille','postgres_active_writers','duplicate_database_effects',
    'lost_acknowledged_writes','api_media_payload_bytes','dynamic_requests_total',
    'dynamic_failed_valid_requests_total','write_requests_total','quote_requests_total',
    'read_latency_p99_ms','quote_latency_p99_ms','api_completed_requests_total',
    'socket_file_descriptor_limit','socket_memory_limit_bytes','socket_fanout_limit_bytes'}
NS = 1000000000


def quantity(value):
    v1.text(value,'Kubernetes quantity',64)
    exponent=re.fullmatch(r'([0-9]+(?:\.[0-9]+)?)[eE]([+-]?[0-9]{1,2})',value)
    if exponent is not None:
        power=int(exponent.group(2));v1.need(-18<=power<=18,'Kubernetes exponent outside exact resource bounds')
        result=Fraction(exponent.group(1))*(10**power if power>=0 else Fraction(1,10**(-power)))
        v1.need(result>0,'positive Kubernetes allocation required');return result
    match=re.fullmatch(r'([0-9]+(?:\.[0-9]+)?)([numkKMGTPE]|[KMGTPE]i)?',value)
    v1.need(match is not None,'unsupported exact Kubernetes quantity')
    decimal=Fraction(match.group(1)); suffix=match.group(2) or ''
    scales={'':1,'n':Fraction(1,10**9),'u':Fraction(1,10**6),'m':Fraction(1,1000),'k':1000,'K':1000}
    scales.update({unit:1000**(index+2) for index,unit in enumerate('MGTPE')})
    scales.update({unit+'i':1024**(index+1) for index,unit in enumerate('KMGTPE')})
    result=decimal*scales[suffix]
    v1.need(result>0,'positive Kubernetes allocation required')
    return result


def bytes_quantity(value):
    result=quantity(value)
    v1.need(result.denominator==1,'integral storage/memory byte allocation required')
    return result.numerator


def bound_ndjson(reference):
    v1.keys(reference,['path','sha256'],'raw capture reference'); v1.sha(reference['sha256'],'raw capture digest')
    path=Path(reference['path'])
    v1.need(path.is_file() and not path.is_symlink(),'ordinary raw capture required')
    digest=hashlib.sha256()
    with path.open('rb') as stream:
        while True:
            row=stream.readline(2*1024*1024+1)
            if not row: break
            v1.need(len(row)<=2*1024*1024 and row.endswith(b'\n'),'bounded complete raw sample required')
            digest.update(row)
            yield v1.decode(row)
    v1.need(digest.hexdigest()==reference['sha256'],'raw capture changed or has incorrect byte identity')


class AllocationReader:
    def __init__(self, provider, plan, target):
        self.provider,self.plan,self.target=provider,plan,target

    def snapshot(self):
        self.provider.qualification_target(self.target['namespaceUID'])
        raw_pods=v1.decode(self.provider._invoke(['get','pods','-o','json']))
        raw_claims=v1.decode(self.provider._invoke(['get','pvc','-o','json']))
        claims={p['metadata']['name']:p for p in raw_claims['items']}
        nodes={}; volumes={}; replicas=[]; storage=[]; used_claims=set()
        for pod in raw_pods['items']:
            metadata=pod['metadata'];labels=metadata.get('labels',{})
            role='postgres' if labels.get('cnpg.io/cluster')=='postgres' else labels.get('app.kubernetes.io/name')
            if role not in set(v1.RUNTIME_ROLES)|{'postgres','nats','redis'}: continue
            node_id=pod['spec'].get('nodeName')
            zone=None
            if node_id is not None:
                if node_id not in nodes:
                    nodes[node_id]=v1.decode(self.provider._invoke(['get','node',node_id,'-o','json']))
                zone=nodes[node_id]['metadata']['labels'].get('topology.kubernetes.io/zone')
                v1.need(zone in self.target['zones'],'observed replica outside approved failure zones')
            if role in v1.RUNTIME_ROLES:
                v1.need(metadata.get('annotations',{}).get('kindle.io/config-sha256')==self.plan['configurationDigest'],
                        'observed runtime candidate drift')
            containers=[]
            for container in pod['spec']['containers']:
                resources=container['resources']
                v1.need(bool(v1.IMAGE.fullmatch(container['image'])),'runtime image is mutable')
                requests=resources['requests'];limits=resources['limits']
                cpu=quantity(requests['cpu'])*1000; maximum_cpu=quantity(limits['cpu'])*1000
                v1.need(cpu.denominator==maximum_cpu.denominator==1,'integral millicore allocation required')
                containers.append({'name':container['name'],'image':container['image'],'cpuMilli':cpu.numerator,
                    'cpuLimitMilli':maximum_cpu.numerator,'memoryBytes':bytes_quantity(requests['memory']),
                    'memoryLimitBytes':bytes_quantity(limits['memory'])})
            replicas.append({'id':metadata['name'],'uid':metadata['uid'],'role':role,'node':node_id,'zone':zone,
                'ready':any(c.get('type')=='Ready' and c.get('status')=='True' for c in pod.get('status',{}).get('conditions',[])),
                'terminating':bool(metadata.get('deletionTimestamp')),'containers':containers})
            if role not in ('postgres','nats'): continue
            for volume in pod['spec'].get('volumes',[]):
                if 'persistentVolumeClaim' not in volume: continue
                claim_name=volume['persistentVolumeClaim']['claimName'];claim=claims[claim_name]
                v1.need(claim_name not in used_claims and claim['status'].get('phase')=='Bound',
                        'durable members require independently bound volumes')
                used_claims.add(claim_name)
                volume_id=claim['spec']['volumeName']
                physical=v1.decode(self.provider._invoke(['get','persistentvolume',volume_id,'-o','json']))
                volumes[volume_id]=physical
                physical_zones=set()
                for term in physical['spec'].get('nodeAffinity',{}).get('required',{}).get('nodeSelectorTerms',[]):
                    for expression in term.get('matchExpressions',[]):
                        if expression['key'] in ('topology.kubernetes.io/zone','failure-domain.beta.kubernetes.io/zone'):
                            v1.need(expression['operator']=='In','unsupported physical volume zone affinity')
                            physical_zones.update(expression['values'])
                v1.need(len(physical_zones)==1 and physical_zones<=set(self.target['zones']),
                        'independently measured single-zone physical volume required')
                storage_zone=next(iter(physical_zones))
                v1.need(zone is None or zone==storage_zone,'pod/physical volume zone differs')
                storage.append({'id':metadata['name'],'role':role,'zone':storage_zone,'claim':claim_name,
                    'claimUID':claim['metadata']['uid'],'volume':claim['spec']['volumeName'],
                    'storageClass':claim['spec']['storageClassName'],
                    'allocatedBytes':bytes_quantity(claim['status']['capacity']['storage'])})
        v1.need(replicas and storage,'actual application and durable storage inventory absent')
        return {'schemaVersion':4,'observedAtNs':str(time.time_ns()),'configurationDigest':self.plan['configurationDigest'],
                'deploymentDigest':self.plan['deploymentDigest'],'replicas':replicas,'storage':storage,
                'nativeResponseDigests':{'pods':v1.digest(raw_pods),'claims':v1.digest(raw_claims),
                    'nodes':{node:v1.digest(value) for node,value in nodes.items()},
                    'volumes':{volume:v1.digest(value) for volume,value in volumes.items()}}}


class MetricReader:
    def __init__(self, signed_authority, verifier, bindings, target):
        v1.keys(signed_authority,['payload','signature'],'native metric authority')
        authority=signed_authority['payload'];verifier.verify(authority,signed_authority['signature'])
        v1.keys(authority,['schemaVersion','origin','ca','clientCertificate','clientKey','bindings','target','metrics'],
                'native metrics authority payload')
        v1.need(type(authority['schemaVersion']) is int and authority['schemaVersion']==4 and
                authority['bindings']==bindings and authority['target']==target,'native authority candidate/target differs')
        self.authority=authority; self.bindings=bindings; self.target=target
        v1.https(authority['origin'],'metrics authority origin')
        parsed=urlsplit(authority['origin'])
        v1.need(parsed.path in ('','/'),'exact metrics origin required');self.origin=parsed
        v1.keys(authority['metrics'],NATIVE_FIELDS,'complete native topology metric mappings')
        for semantic,mapping in authority['metrics'].items():
            v1.keys(mapping,['metric','kind','labels'],'metric mapping')
            v1.need(re.fullmatch(r'[a-zA-Z_:][a-zA-Z0-9_:]{0,127}',mapping['metric']) is not None,
                    'literal Prometheus metric name required')
            expected_kind='counter' if semantic.endswith('_total') else 'gauge'
            v1.need(mapping['kind']==expected_kind,'explicit correct metric temporality required')
            v1.keys(mapping['labels'],['environment','namespace'],'metric deployment selectors')
            v1.need(mapping['labels']=={'environment':'qualification','namespace':target['namespace']},
                    'metrics may not select foreign infrastructure')
        self.tls=ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        self.tls.minimum_version=ssl.TLSVersion.TLSv1_3
        with tempfile.TemporaryDirectory(prefix='kindle-native-metrics-') as directory:
            files=[]
            for field in ('ca','clientCertificate','clientKey'):
                reference=authority[field];v1.keys(reference,['path','sha256'],'metrics TLS reference')
                raw=regular_bytes(reference['path'],65536)
                v1.need(hashlib.sha256(raw).hexdigest()==reference['sha256'],'metrics TLS material changed')
                path=Path(directory)/field;path.write_bytes(raw);path.chmod(0o600);files.append(path)
            self.tls.load_verify_locations(cafile=str(files[0]))
            self.tls.load_cert_chain(str(files[1]),str(files[2]))

    def series(self, item):
        semantic,mapping=item
        selector=','.join(key+'='+json.dumps(value) for key,value in sorted(mapping['labels'].items()))
        query=mapping['metric']+'{'+selector+'}'
        connection=http.client.HTTPSConnection(self.origin.hostname,self.origin.port,timeout=2,context=self.tls)
        started=time.monotonic()
        try:
            connection.request('GET','/api/v1/query?'+urlencode({'query':query}),headers={'Accept':'application/json'})
            response=connection.getresponse();v1.need(response.status==200,'native metrics HTTP rejected')
            data=bytearray()
            while True:
                remaining=2-(time.monotonic()-started);v1.need(remaining>0,'native metric total deadline exceeded')
                if connection.sock:connection.sock.settimeout(remaining)
                chunk=response.read1(min(65536,2*1024*1024+1-len(data)))
                if not chunk:break
                data.extend(chunk);v1.need(len(data)<=2*1024*1024,'native metric response oversized')
            value=v1.decode(bytes(data));v1.need(value.get('status')=='success','native metric query failed')
            result=value['data'];v1.need(result['resultType']=='vector' and type(result['result']) is list and
                1<=len(result['result'])<=2048,'actual bounded native series absent')
            rows=[];identities=set();now=time.time_ns()
            for row in result['result']:
                labels=row['metric'];stamp,encoded=row['value']
                v1.need(all(labels.get(key)==expected for key,expected in mapping['labels'].items()),'foreign metric labels')
                identity=tuple(sorted(labels.items()));v1.need(identity not in identities,'duplicate native metric series');identities.add(identity)
                v1.text(encoded,'exact native metric encoding',128)
                v1.need(set(labels)<={'__name__','environment','namespace','service','zone','replica_id','member_id',
                    'stream','route','method','code','operation','dependency','pool','job','instance'},
                    'unexpected native labels may expose unapproved dimensions')
                v1.need('zone' not in labels or labels['zone'] in self.target['zones'],'foreign metric zone')
                numeric=Decimal(encoded); timestamp=Decimal(str(stamp))*NS
                v1.need(numeric.is_finite() and numeric>=0 and timestamp.is_finite() and
                        abs(Decimal(now)-timestamp)<=2*NS,'nonfinite/negative/stale native metric')
                rows.append({'labels':labels,'timestampNs':str(int(timestamp)),'value':encoded})
            return semantic,{'metric':mapping['metric'],'kind':mapping['kind'],'series':rows}
        finally:connection.close()

    def snapshot(self):
        with ThreadPoolExecutor(max_workers=8) as executor:
            values=dict(executor.map(self.series,self.authority['metrics'].items()))
        return {'observedAtNs':str(time.time_ns()),'bindings':self.bindings,'metrics':values}


def load_capture_adapter(source_root, references):
    v1.keys(references,LOAD_FILES,'pinned D16 capture sources')
    root=Path(source_root).resolve()/'tools/launchpad/production/load'
    for filename,expected in references.items():
        v1.sha(expected,'D16 capture source digest')
        v1.need(hashlib.sha256(regular_bytes(root/filename,4*1024*1024)).hexdigest()==expected,
                'D16 capture source changed: '+filename)
    sys.dont_write_bytecode=True
    sys.path.insert(0,str(root))
    for name in ('capture','phase2_types','phase2_http','workload'):
        if name in sys.modules:
            v1.need(Path(sys.modules[name].__file__).resolve()==root/(name+'.py'),'foreign D16 module loaded')
    return importlib.import_module('capture')


def write_result(path, value):
    path=Path(path);v1.need(not path.is_symlink(),'result symlink rejected')
    path.parent.mkdir(parents=True,exist_ok=True)
    descriptor=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(descriptor,'w') as stream:
        json.dump(value,stream,sort_keys=True,allow_nan=False,indent=2);stream.write('\n');stream.flush();os.fsync(stream.fileno())


def capture_run(reader, allocation, run, plan, output, authority_digest):
    v1.keys(run,['runId','target','durationSeconds','intervalSeconds','scope'],'capture run')
    v1.name(run['runId'],'run id');v1.integer(run['durationSeconds'],60,259200,'capture duration')
    v1.integer(run['intervalSeconds'],1,60,'sample interval')
    v1.need(run['durationSeconds']%run['intervalSeconds']==0 and run['scope'] in ('isolated_component','final_shared'),
            'exact capture interval and scope required')
    directory=Path(output);v1.need(not directory.exists(),'capture output must be new')
    directory.mkdir(parents=True,mode=0o700)
    started=time.time_ns();clock=time.monotonic_ns();rows=0
    raw=directory/'topology-native.ndjson'
    try:
        with raw.open('x') as stream:
            for index in range(run['durationSeconds']//run['intervalSeconds']+1):
                deadline=clock+index*run['intervalSeconds']*NS
                delay=deadline-time.monotonic_ns()
                if delay>0:time.sleep(delay/NS)
                sample={'schemaVersion':4,'runId':run['runId'],'index':index,
                        'metrics':reader.snapshot(),'allocations':allocation.snapshot()}
                v1.need(time.monotonic_ns()-deadline<run['intervalSeconds']*NS,'native generator/sampler cannot maintain interval')
                stream.write(v1.canonical(sample).decode()+'\n');stream.flush();os.fsync(stream.fileno());rows+=1
        finished=time.time_ns()
        manifest={'schemaVersion':4,'runId':run['runId'],'scope':run['scope'],'configurationDigest':plan['configurationDigest'],
            'deploymentDigest':plan['deploymentDigest'],'sourceDigest':plan['sourceDigest'],'interfaceDigest':plan['interfaceDigest'],
            'startedAtNs':str(started),'finishedAtNs':str(finished),'intervalSeconds':run['intervalSeconds'],'sampleCount':rows,
            'target':run['target'],'metricAuthorityDigest':authority_digest,'samples':{'path':str(raw.resolve()),'sha256':sha_stream(raw)},
            'capacity_qualified':False,'final_gate':'9.1'}
        write_result(directory/'capture.json',manifest)
        return manifest
    except Exception as exc:
        write_result(directory/'failure.json',failure_report(type(exc).__name__,plan))
        raise


def sha_stream(path):
    result=hashlib.sha256()
    with Path(path).open('rb') as stream:
        for chunk in iter(lambda:stream.read(1048576),b''):result.update(chunk)
    return result.hexdigest()


def failure_report(reason, plan=None):
    return {'schemaVersion':4,'passed':False,'capacity_qualified':False,'final_gate':'9.1',
            'deploymentDigest':plan.get('deploymentDigest') if plan else None,
            'limitations':[reason],'requirements':{req:{'passed':False,'reason':reason} for req in v1.REQS}}


def exact_series(metric):
    v1.keys(metric,['metric','kind','series'],'native metric record')
    v1.need(metric['kind'] in ('counter','gauge') and type(metric['series']) is list and metric['series'],
            'native metric observations absent')
    result={}
    for row in metric['series']:
        v1.keys(row,['labels','timestampNs','value'],'raw native series')
        v1.need(type(row['labels']) is dict,'native metric labels missing')
        key=tuple(sorted(row['labels'].items()))
        v1.text(row['value'],'exact native numeric encoding',128)
        v1.text(row['timestampNs'],'native metric timestamp',30)
        v1.need(row['timestampNs'].isdigit() and int(row['timestampNs'])>0,'positive native metric time required')
        numeric=Decimal(row['value']);v1.need(numeric.is_finite() and numeric>=0,'invalid native numeric value')
        v1.need(key not in result,'duplicate native metric identity');result[key]=numeric
    return result


def total(metric):
    return sum(exact_series(metric).values(),Decimal(0))


def counter_rate(before, after, elapsed_ns):
    v1.need(before['kind']==after['kind']=='counter' and before['metric']==after['metric'],'counter mapping drift')
    left,right=exact_series(before),exact_series(after)
    v1.need(set(left)==set(right),'counter replica/epoch identity changed without reconciliation')
    v1.need(elapsed_ns>0 and all(right[key]>=left[key] for key in left),'counter reset or time inversion')
    return {key:Fraction(right[key]-left[key])*NS/elapsed_ns for key in left}


def assess_storage(allocations, metrics, plan, config):
    summary={}
    for role in ('postgres','nats'):
        members=[member for member in allocations['storage'] if member['role']==role]
        v1.need(len(members)==3 and len({m['id'] for m in members})==3 and
                {m['zone'] for m in members}==set(config['zones']),'three independently allocated durable members required')
        v1.need(len({m['volume'] for m in members})==3,'durable members share a physical volume')
        allocation_rows=exact_series(metrics[role+'_allocated_bytes'])
        def value(field, member):
            rows=exact_series(metrics[field])
            match=[value for labels,value in rows.items() if dict(labels).get('member_id')==member['id']]
            v1.need(len(match)==1 and match[0]==match[0].to_integral_value(),'exact per-member measurement missing: '+field)
            return int(match[0])
        for member in members:
            v1.integer(member['allocatedBytes'],1,100*v1.TIB,'actual member storage')
            v1.need(value(role+'_allocated_bytes',member)==member['allocatedBytes'],'metric/PVC allocation mismatch')
            if role=='postgres':
                v1.need(value('postgres_raw_payload_bytes',member)==555360000000,
                        'actual thirty-day raw fixture payload absent')
                used=value('postgres_raw_row_index_bytes',member)
                v1.need(used>=1666080000000,'actual PostgreSQL fixture amplification absent')
                for field,budget in (('postgres_derived_bytes','derivedPgBytesPerMember'),
                                      ('postgres_wal_bytes','walBytesPerMember'),
                                      ('postgres_vacuum_workspace_bytes','vacuumBytesPerMember')):
                    measured=value(field,member);v1.need(0<measured<=config['storage'][budget],'measured PostgreSQL workspace absent/exceeded')
                    used+=measured
                free=value('postgres_free_bytes',member)
                reserved=v1.ceil_div(member['allocatedBytes']*config['storage']['freeHeadroomPermille'],1000)
                v1.need(used+reserved<=member['allocatedBytes'] and reserved<=free<=member['allocatedBytes']-used,
                        'measured PostgreSQL fixture/workspace/free-headroom requires expansion')
            else:
                store=value('nats_configured_store_bytes',member)
                v1.need(0<store<=member['allocatedBytes']*9//10,'broker configured store exceeds usable volume')
                summary.setdefault('configuredNatsBytes',0);summary['configuredNatsBytes']+=store
        summary[role]=members
    overhead_values=exact_series(metrics['nats_broker_overhead_permille']).values()
    v1.need(all(value>0 for value in overhead_values),'broker member overhead absent')
    overhead=max(overhead_values)
    v1.need(0<overhead<=config['storage']['brokerOverheadPermille'],'measured broker overhead absent or exceeds budget')
    for mode in ('normal','qualification'):
        v1.need(summary['configuredNatsBytes']>=plan['capacity']['broker'][mode]['requiredBytesWithTwofoldHeadroom'],
                'measured '+mode+' storage insufficient')
    envelope=exact_series(metrics['raw_event_envelope_bytes'])
    v1.need(all(value==1200 for value in envelope.values()),'authoritative raw envelope mismatch')
    expected_streams={'raw-chain'}|{stream['name'] for stream in config['streams']}
    for field,expected in (('nats_stream_retention_seconds',259200),('nats_stream_replicas',3)):
        rows=exact_series(metrics[field]); streams={dict(labels).get('stream') for labels in rows}
        v1.need(streams==expected_streams and all(value==expected for value in rows.values()),
                'actual JetStream retention/replication contract differs')
    retained=exact_series(metrics['nats_stream_retained_logical_bytes'])
    v1.need({dict(labels).get('stream') for labels in retained}==expected_streams and
            all(value>0 for value in retained.values()),'actual derived/raw retained stream measurement missing')
    summary['normalLedger']=plan['capacity']['broker']['normal']
    summary['qualificationLedger']=plan['capacity']['broker']['qualification']
    return summary


def assess_allocations(snapshot, config, profile):
    v1.need(snapshot['configurationDigest']==v1.digest(config),'allocation configuration changed')
    active=[row for row in snapshot['replicas'] if row['ready'] and not row['terminating']]
    v1.need(len({row['uid'] for row in active})==len(active),'replica identity duplicated')
    if profile=='zoneLoss':
        application_zones={row['zone'] for row in active if row['role'] in v1.RUNTIME_ROLES}
        v1.need(len(application_zones)==2,'zone loss did not remove every application replica in one zone')
    targets={'api':16 if profile=='zoneLoss' else 48 if profile=='burst' else 24,
             'sockets':20 if profile=='zoneLoss' else 60 if profile=='burst' else 30}
    for role,count in targets.items():
        rows=[row for row in active if row['role']==role]
        v1.need(len(rows)==count,'measured allocation differs for '+profile+'/'+role)
        for row in rows:
            v1.need(row['zone'] in config['zones'],'replica outside approved zones')
            containers=[c for c in row['containers'] if c['name']==role]
            v1.need(len(containers)==1,'application resource container missing')
            container=containers[0]
            v1.need(container['image']==config['images'][role] and container['cpuMilli']>=2000 and
                    container['memoryBytes']>=4*v1.GIB and container['cpuMilli']<=container['cpuLimitMilli'] and
                    container['memoryBytes']<=container['memoryLimitBytes'],'candidate resource envelope below target')
    return active


def native_summary(manifest, plan, config, windows, bindings, expected_authority):
    v1.keys(manifest,['schemaVersion','runId','scope','configurationDigest','deploymentDigest','sourceDigest',
                     'interfaceDigest','startedAtNs','finishedAtNs','intervalSeconds','sampleCount','target',
                     'metricAuthorityDigest','samples','capacity_qualified','final_gate'],'native capture manifest')
    v1.need(type(manifest['schemaVersion']) is int and manifest['schemaVersion']==4 and
            manifest['capacity_qualified'] is False and manifest['final_gate']=='9.1','native capture scope/version differs')
    for field in ('configurationDigest','deploymentDigest','sourceDigest','interfaceDigest'):
        v1.need(manifest[field]==plan[field],'native capture identity changed')
    v1.need(manifest['metricAuthorityDigest']==expected_authority,'native metric authority changed')
    v1.integer(manifest['intervalSeconds'],1,60,'native interval')
    v1.integer(manifest['sampleCount'],2,259201,'native sample count')
    start,end=int(manifest['startedAtNs']),int(manifest['finishedAtNs'])
    v1.need(start<end,'native capture has no real interval')
    v1.keys(windows,['soak','burst','zoneLoss'],'observed capacity windows')
    for kind,window in windows.items():
        v1.keys(window,['startNs','endNs'],'observed window')
        left,right=int(window['startNs']),int(window['endNs'])
        v1.need(start<=left<right<=end,'capacity window lies outside native capture')
        minimum={'soak':86400,'burst':600,'zoneLoss':60}[kind]
        v1.need(right-left>=minimum*NS,'capacity window shorter than required wallclock duration')
        if kind!='soak':
            v1.need(int(windows['soak']['startNs'])<=left and right<=int(windows['soak']['endNs']),
                    'burst/zone loss must occur during the peak soak')
    v1.need(int(windows['burst']['endNs'])<=int(windows['zoneLoss']['startNs']) or
            int(windows['zoneLoss']['endNs'])<=int(windows['burst']['startNs']),
            'burst and zone-loss allocation measurements overlap')
    counts=dict.fromkeys(windows,0); coverage={key:[None,None] for key in windows}
    previous=None;index=0;storage=None
    for sample in bound_ndjson(manifest['samples']):
        v1.keys(sample,['schemaVersion','runId','index','metrics','allocations'],'native sample')
        v1.need(type(sample['schemaVersion']) is int and sample['schemaVersion']==4 and type(sample['index']) is int and sample['index']==index and
                sample['runId']==manifest['runId'],'native sample order/run differs');index+=1
        v1.keys(sample['metrics'],['observedAtNs','bindings','metrics'],'native metric snapshot')
        v1.need(sample['metrics']['bindings']==bindings,'native sampler source binding differs')
        metrics=sample['metrics']['metrics'];v1.keys(metrics,NATIVE_FIELDS,'complete measured topology fields')
        instant=int(sample['metrics']['observedAtNs'])
        for metric in metrics.values():
            exact_series(metric)
            v1.need(all(abs(instant-int(row['timestampNs']))<=2*NS for row in metric['series']),
                    'raw native series is stale relative to the correlated snapshot')
        v1.need(start<=instant<=end and abs(instant-int(sample['allocations']['observedAtNs']))<=manifest['intervalSeconds']*NS,
                'native allocation and metrics are not contemporaneous')
        v1.need(sample['allocations']['deploymentDigest']==plan['deploymentDigest'],'measured deployment changed')
        if previous is not None:
            elapsed=instant-int(previous['metrics']['observedAtNs'])
            v1.need(0<elapsed<=manifest['intervalSeconds']*NS+2*NS,'native capture has sample gap')
            before=previous['metrics']['metrics']
            for field in ('postgres_active_writers','duplicate_database_effects','lost_acknowledged_writes','api_media_payload_bytes'):
                expected=1 if field=='postgres_active_writers' else 0
                v1.need(total(metrics[field])==expected,'authoritative state invariant failed: '+field)
            storage=assess_storage(sample['allocations'],metrics,plan,config)
            left=int(previous['metrics']['observedAtNs'])
            for kind,window in windows.items():
                wstart,wend=int(window['startNs']),int(window['endNs'])
                if instant<wstart or left>wend:continue
                profile=kind
                if kind=='soak':
                    if any(int(windows[special]['startNs'])<=instant<=int(windows[special]['endNs']) for special in ('burst','zoneLoss')):
                        profile='burst' if int(windows['burst']['startNs'])<=instant<=int(windows['burst']['endNs']) else 'zoneLoss'
                active=assess_allocations(sample['allocations'],config,profile)
                rate=lambda field:sum(counter_rate(before[field],metrics[field],elapsed).values(),Fraction(0))
                offered=rate('dynamic_requests_total'); failed=rate('dynamic_failed_valid_requests_total')
                target=20000 if profile=='burst' else 10000
                v1.need(offered-failed>=target and failed*1000<=offered,'valid dynamic throughput/error rate failed')
                v1.need(rate('write_requests_total')>=2000 and rate('quote_requests_total')>=2000 and
                        rate('write_requests_total')+rate('quote_requests_total')<=offered,'write/quote subset accounting failed')
                v1.need(total(metrics['postgres_server_connections'])<=600 and total(metrics['postgres_application_connections'])<540,'application server connection ceiling exceeded')
                v1.need(max(exact_series(metrics['read_latency_p99_ms']).values())<=400 and
                        max(exact_series(metrics['quote_latency_p99_ms']).values())<=750,'measured read/quote latency target failed')
                payload=rate('socket_payload_bytes_total');required=400000000 if profile=='burst' else 100000000
                v1.need(payload>=required,'measured market payload below target')
                sockets=200000 if profile=='burst' else 100000
                v1.need(total(metrics['socket_open_connections'])>=sockets,'measured socket fleet below target')
                if profile=='zoneLoss':
                    rates=counter_rate(before['api_completed_requests_total'],metrics['api_completed_requests_total'],elapsed)
                    api_ids={row['id'] for row in active if row['role']=='api'}
                    observed={dict(labels).get('replica_id'):value for labels,value in rates.items()}
                    v1.need(set(observed)==api_ids and all(value>=625 for value in observed.values()),
                            'each surviving API replica must demonstrate 625 RPS')
                counts[kind]+=1
                coverage[kind][0]=left if coverage[kind][0] is None else coverage[kind][0]
                coverage[kind][1]=instant
        previous=sample
    v1.need(index==manifest['sampleCount'],'native capture sample count differs')
    for kind,window in windows.items():
        v1.need(counts[kind]>0 and coverage[kind][0]<=int(window['startNs']) and
                coverage[kind][1]>=int(window['endNs']),'native observations do not cover '+kind+' interval')
    return {'samples':index,'windows':counts,'storage':storage,'sourceBoundary':'native_provider',
            'capacity_qualified':False}


def shared_summary(adapter, manifest, windows):
    v1.need(manifest['scope']=='final_shared' and manifest['final_gate']=='9.1',
            'final topology release needs the actual final shared run')
    native_controls={item['sha256'] for item in manifest['artifacts'] if item['kind']=='native_provider'}
    faults=list(adapter.iter_artifact(manifest,'fault_control',record='FaultInterval'))
    v1.need(faults,'real D16 adversarial control observations absent')
    ids=set(); zone_fault=False
    for fault in faults:
        v1.need(fault['run_id']==manifest['run_id'] and fault['fault_id'] not in ids and
                fault['outcome']=='recovered' and fault['native_control_sha256'] in native_controls,
                'fault outcome/control artifact is missing, changed or unreconciled')
        ids.add(fault['fault_id'])
        left,right,recovery=map(int,(fault['observed_start_utc_ns'],fault['observed_end_utc_ns'],fault['recovery_utc_ns']))
        v1.need(int(manifest['started_utc_ns'])<=left<right<=recovery<=int(manifest['ended_utc_ns']) and
                recovery-right<=900*NS,'observed fault/recovery timeline invalid')
        if (fault['action']=='replica_loss' and left<=int(windows['zoneLoss']['startNs']) and
                int(windows['zoneLoss']['endNs'])<=right):
            zone_fault=True
    v1.need(zone_fault,'zone-loss measurements have no matching native failure interval')
    result=adapter.gateway_summary(manifest)
    seconds=result['seconds'];start=int(manifest['started_utc_ns']);end=int(manifest['ended_utc_ns'])
    v1.need(end-start>=86400*NS,'D16 shared capture shorter than the peak soak')
    coverage={kind:0 for kind in windows}
    previous=None
    for encoded,entry in sorted(seconds.items(),key=lambda pair:int(pair[0])):
        second=int(encoded)
        v1.need(previous is None or second==previous+1,'shared gateway fleet capture has a missing second')
        previous=second;instant=start+second*NS
        for kind,window in windows.items():
            left,right=int(window['startNs']),int(window['endNs'])
            if not left<=instant<right:continue
            profile=kind
            if kind=='soak' and int(windows['burst']['startNs'])<=instant<int(windows['burst']['endNs']):profile='burst'
            clients=200000 if profile=='burst' else 100000
            payload=400000000 if profile=='burst' else 100000000
            frames=1000000 if profile=='burst' else 250000
            v1.need(entry['connections']>=clients and entry['payload_bytes']>=payload and entry['frames']>=frames,
                    'actual shared socket/frame/payload envelope below target')
            v1.need(entry['wire_bytes']>entry['payload_bytes'],'measured gateway transport overhead missing')
            coverage[kind]+=1
    for kind,window in windows.items():
        minimum=(int(window['endNs'])-int(window['startNs']))//NS
        v1.need(coverage[kind]>=minimum,'actual D16 fleet coverage incomplete: '+kind)
    return {'gatewayTotals':result['totals'],'observedSeconds':coverage,'boundary':result['boundary'],
            'faultIds':sorted(ids)}


def release_measurements(signed, plan, config, verifier, adapter, expected_generator, expected_authority, now=None):
    v1.keys(signed,['payload','signature'],'signed phase four measurement references')
    payload=signed['payload'];verifier.verify(payload,signed['signature'])
    v1.keys(payload,['schemaVersion','runId','configurationDigest','deploymentDigest','sourceDigest',
                     'interfaceDigest','phase3Evidence','capture','sharedCapture','windows'],
            'phase four measurements')
    v1.need(type(payload['schemaVersion']) is int and payload['schemaVersion']==4,'phase four evidence version')
    for field in ('configurationDigest','deploymentDigest','sourceDigest','interfaceDigest'):
        v1.need(payload[field]==plan[field],'phase four measurement candidate differs')
    v1.name(payload['runId'],'measurement run id')
    phase3=artifact(payload['phase3Evidence']);v1.need(phase3['schemaVersion']==3,'interpreted adversarial evidence required')
    qualification=verify_bundle(phase3,plan,config,verifier,now=now)
    native=artifact(payload['capture'])
    v1.need(native['runId']==payload['runId'] and native['scope']=='final_shared','native run/scope differs')
    bindings=adapter.Bindings(plan['sourceDigest'],plan['interfaceDigest'],plan['deploymentDigest'],expected_generator)
    shared=adapter.read_manifest(payload['sharedCapture']['path'],payload['sharedCapture']['sha256'],bindings=bindings)
    v1.need(shared['run_id']==payload['runId'] and 'L42' in shared['required_lane_ids'],
            'D16 shared manifest does not bind this topology lane/run')
    for window in payload['windows'].values():
        v1.need(int(shared['started_utc_ns'])<=int(window['startNs'])<int(window['endNs'])<=int(shared['ended_utc_ns']),
                'native/provider measurement window does not match D16 run')
    target=phase3['payload']['inventory'];inventory=artifact(target)
    v1.need(all(native['target'][field]==inventory['qualificationTarget'][field]
                for field in ('context','namespace','namespaceUID','environment')),
            'capacity capture target differs from restored/reconciled infrastructure')
    native_result=native_summary(native,plan,config,payload['windows'],vars(bindings),expected_authority)
    shared_result=shared_summary(adapter,shared,payload['windows'])
    finish=int(native['finishedAtNs']);now_ns=int((now or datetime.now(timezone.utc)).timestamp())*NS
    v1.need(finish<=now_ns and now_ns-finish<=config['release']['qualificationMaxAgeSeconds']*NS,
            'native capacity evidence future/stale')
    return {'schemaVersion':4,'passed':True,'capacity_qualified':False,'monthly_availability_qualified':False,
            'final_gate':'9.1','runId':payload['runId'],'deploymentDigest':plan['deploymentDigest'],
            'measurementDigest':v1.digest(signed),'sharedManifestDigest':payload['sharedCapture']['sha256'],
            'adversarialEvidenceDigest':qualification['evidenceDigest'],'native':native_result,'shared':shared_result,
            'limitations':['Owned topology probes passed; shared serial 9.1 signoff and operating budget remain required.'],
            'requirements':{req:{'passed':True,'phase':4,'full_requirement_qualified':False,
                                 'metric':v1.METRICS[req],'deploymentDigest':plan['deploymentDigest']} for req in v1.REQS}}


def main(argv=None):
    parser=argparse.ArgumentParser()
    parser.add_argument('action',choices=('capture','check'))
    parser.add_argument('--inputs',required=True);parser.add_argument('--source-root',required=True)
    parser.add_argument('--measurements');parser.add_argument('--output',required=True)
    args=parser.parse_args(argv);plan=None
    try:
        inputs=v1.load(args.inputs)
        v1.keys(inputs,['schemaVersion','operatorInputs','loadSources','expectedGeneratorSHA256','metricAuthority','run'],
                'phase four capacity inputs')
        v1.need(type(inputs['schemaVersion']) is int and inputs['schemaVersion']==4,'capacity input version')
        v1.sha(inputs['expectedGeneratorSHA256'],'actual D16 generator digest')
        operator_inputs=artifact(inputs['operatorInputs']);config=v1.validate(operator_inputs['topology'])
        v1.need(config['release']['sourceDigest']==operator_inputs['expectedSourceDigest'] and
                config['release']['interfaceDigest']==operator_inputs['expectedInterfaceDigest'],'prior source/interface authority differs')
        plan=connected_plan(config,args.source_root,operator_inputs['adapterSHA256'],operator_inputs['documents'],
                            operator_inputs['chainFile'],operator_inputs['chainSHA256'])
        validate_connected_plan(plan,config)
        verifier=SignatureVerifier(operator_inputs['openssl']['path'],operator_inputs['openssl']['sha256'],operator_inputs['trust'])
        run=inputs['run'];v1.keys(run['target'],['context','namespace','namespaceUID','environment','zones'],'native target')
        target=run['target'];v1.need(target['environment']=='qualification' and target['namespace']==config['namespace'] and
            target['context']==operator_inputs['kubectl']['context'] and target['zones']==config['zones'],
            'measurement requires independently selected isolated topology')
        bindings={'candidate':plan['sourceDigest'],'interface':plan['interfaceDigest'],
                  'topology':plan['deploymentDigest'],'generator':inputs['expectedGeneratorSHA256']}
        if args.action=='capture':
            signed_authority=artifact(inputs['metricAuthority'])
            reader=MetricReader(signed_authority,verifier,bindings,target)
            provider=Kubernetes(operator_inputs['kubectl']['path'],operator_inputs['kubectl']['sha256'],target['context'],
                                target['namespace'],Path(args.output).parent/('native-invocations-'+run['runId']))
            result=capture_run(reader,AllocationReader(provider,plan,target),run,plan,args.output,inputs['metricAuthority']['sha256'])
        else:
            v1.need(bool(args.measurements),'actual signed phase four measurement references required')
            signed_authority=artifact(inputs['metricAuthority'])
            verifier.verify(signed_authority['payload'],signed_authority['signature'])
            v1.need(signed_authority['payload']['bindings']==bindings and signed_authority['payload']['target']==target,
                    'current native authority differs from the captured candidate/target')
            adapter=load_capture_adapter(args.source_root,inputs['loadSources'])
            result=release_measurements(v1.load(args.measurements),plan,config,verifier,adapter,inputs['expectedGeneratorSHA256'],inputs['metricAuthority']['sha256'])
            write_result(args.output,result)
        return 0
    except Exception as exc:
        reason=str(exc)[:1024] if isinstance(exc,v1.Invalid) else type(exc).__name__
        failure=failure_report(reason,plan)
        output=Path(args.output)
        destination=output/'failure.json' if output.is_dir() else output
        if not destination.exists():write_result(destination,failure)
        print('topology capacity measurements failed; see explicit requirement results',file=sys.stderr)
        return 1


if __name__=='__main__':
    raise SystemExit(main())
