#!/usr/bin/env python3
"""Offline version-1 topology compiler. No network, deployment, or credential access."""
from __future__ import annotations
import argparse
import copy
import hashlib
import json
import re
from datetime import datetime, timezone
from fractions import Fraction
from pathlib import Path
from urllib.parse import urlsplit

TIB = 1024**4
GIB = 1024**3
REQS = tuple(f'D14_{i:03}' for i in range(1, 19))
SERVICES = {'api': (24,48,2,4,8), 'sockets': (30,60,2,4,1),
            'indexer': (3,3,2,4,4), 'charts': (6,12,4,8,2),
            'stats': (6,12,4,8,2), 'pnl': (6,12,4,8,2),
            'ranking': (6,12,4,8,2)}
IMAGE_NAMES = set(SERVICES) | {'postgres','nats','redis'}
RUNTIME_ROLES = {'api':'apid:rest','sockets':'apid:sockets','indexer':'indexerd','charts':'chartsd','stats':'statsd','pnl':'pnld','ranking':'rankingd','gateway':'mediagatewayd','user':'userd','social':'sociald','metadata':'metadatad','livestream':'livestreamd'}
def workloads(c):
    return SERVICES if c['schemaVersion']==1 else {k:tuple(c['workloads'][k][f] for f in ('replicas','maxReplicas','cpu','memoryGiB','poolConnections')) for k in RUNTIME_ROLES}
DIGEST = re.compile(r'[a-f0-9]{64}\Z')
NAME = re.compile(r'[a-z][a-z0-9-]{0,61}[a-z0-9]\Z')
IMAGE = re.compile(r'[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}\Z')
class Invalid(ValueError): pass

def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True).encode()
def digest(value): return hashlib.sha256(canonical(value)).hexdigest()
def need(ok, message):
    if not ok: raise Invalid(message)
def keys(value, expected, context):
    need(type(value) is dict, f'{context}: expected object')
    need(set(value)==set(expected), f'{context}: missing/unknown keys {sorted(set(value)^set(expected))}')
def integer(value, low, high, context):
    need(type(value) is int and low<=value<=high, f'{context}: integer outside {low}..{high}')
def text(value, context, limit=128):
    need(type(value) is str and 0<len(value)<=limit and not any(ord(c)<32 for c in value), f'{context}: bounded nonempty string required')
def name(value, context):
    text(value,context,63);need(bool(NAME.fullmatch(value)),f'{context}: invalid DNS label')
def sha(value, context): need(type(value) is str and bool(DIGEST.fullmatch(value)),f'{context}: sha256 required')
def https(value, context):
    text(value,context,512); u=urlsplit(value)
    need(u.scheme=='https' and bool(u.hostname) and not u.username and not u.password and not u.fragment and not u.query, f'{context}: credential-free HTTPS URL required')

def decode(data):
    def pairs(items):
        result={}
        for k,v in items:
            need(k not in result,f'duplicate JSON key: {k}');result[k]=v
        return result
    need(len(data)<=2*1024*1024,'configuration/evidence exceeds 2MiB')
    try: return json.loads(data,object_pairs_hook=pairs,parse_constant=lambda x: (_ for _ in ()).throw(Invalid('nonfinite JSON number')))
    except (json.JSONDecodeError,UnicodeDecodeError,RecursionError) as ex: raise Invalid('invalid JSON') from ex

def load(path):
    with Path(path).open('rb') as stream:
        return decode(stream.read(2*1024*1024+1))

def validate(c):
    expected_keys = ['schemaVersion','revision','environment','namespace','region','zones','storageClasses','images','secretRefs','publicHost','edgeNamespace','rpcEndpoints','mediaCdn','operator','storage','streams','connections','admission','release']
    if c.get('schemaVersion')==2: expected_keys+=['workloads','runtimeDocuments']
    keys(c,expected_keys,'topology')
    need(c['schemaVersion'] in (1,2) and type(c['schemaVersion']) is int,'unsupported schema version')
    if c['schemaVersion']==2:
        keys(c['workloads'],RUNTIME_ROLES,'workloads');keys(c['runtimeDocuments'],RUNTIME_ROLES,'runtimeDocuments')
        for role,w in c['workloads'].items():
            keys(w,['replicas','maxReplicas','cpu','memoryGiB','poolConnections'],'workload '+role)
            for field in w: integer(w[field],1,200,'workload '+role+' '+field)
            need(w['maxReplicas']>=w['replicas'],'workload maximum below replicas')
            d=c['runtimeDocuments'][role];keys(d,['budgetSHA256','expectedSchemaSHA256','poolId'],'runtime documents')
            sha(d['budgetSHA256'],'budgetSHA256');sha(d['expectedSchemaSHA256'],'expectedSchemaSHA256');name(d['poolId'],'poolId')
    integer(c['revision'],1,2**31-1,'revision');need(c['environment'] in ('qualification','production'),'environment')
    for k in ('namespace','region','edgeNamespace'): name(c[k], k)
    need(type(c['zones']) is list and len(c['zones'])==3 and all(type(z) is str for z in c['zones']) and len(set(c['zones']))==3,'exactly three distinct provider zones required')
    for z in c['zones']: name(z,'zone')
    keys(c['storageClasses'],['postgres','nats'],'storageClasses')
    for v in c['storageClasses'].values(): name(v,'storageClass')
    keys(c['images'],set(workloads(c))|{'postgres','nats','redis'},'images')
    for k,v in c['images'].items(): need(type(v) is str and bool(IMAGE.fullmatch(v)),f'image {k}: immutable sha256 reference required')
    keys(c['secretRefs'],['runtime','postgresBootstrap','natsTls','natsAuth','redis','edgeTls'],'secretRefs')
    keys(c['secretRefs']['runtime'],workloads(c),'per-service runtime secrets')
    for v in c['secretRefs']['runtime'].values(): name(v,'runtime secretRef')
    for k,v in c['secretRefs'].items():
        if k!='runtime':name(v,'secretRef')
    all_secrets=list(c['secretRefs']['runtime'].values())+[v for k,v in c['secretRefs'].items() if k!='runtime']
    need(len(set(all_secrets))==len(all_secrets),'distinct service/bootstrap secrets required')
    text(c['publicHost'],'publicHost',253)
    need(bool(re.fullmatch(r'[a-z0-9]+(?:[.-][a-z0-9]+)*\.[a-z]{2,}',c['publicHost'])),'publicHost DNS required')
    need(type(c['rpcEndpoints']) is list and 2<=len(c['rpcEndpoints'])<=8 and all(type(u) is str for u in c['rpcEndpoints']) and len(set(c['rpcEndpoints']))==len(c['rpcEndpoints']),'2..8 distinct committed RPC endpoints required')
    for u in c['rpcEndpoints']: https(u,'RPC')
    https(c['mediaCdn'],'mediaCdn')
    keys(c['operator'],['cnpgVersion','crdDigest','fencingEvidenceDigest'],'operator')
    text(c['operator']['cnpgVersion'],'cnpgVersion',32);need(bool(re.fullmatch(r'1\.29\.[0-9]+',c['operator']['cnpgVersion'])),'CNPG 1.29.x schema required')
    for k in ('crdDigest','fencingEvidenceDigest'): sha(c['operator'][k],k)
    keys(c['storage'],['postgresBytesPerMember','natsBytesPerMember','derivedPgBytesPerMember','walBytesPerMember','vacuumBytesPerMember','freeHeadroomPermille','brokerOverheadPermille'],'storage')
    for k in ('postgresBytesPerMember','natsBytesPerMember'): integer(c['storage'][k],2*TIB,100*TIB,k)
    for k in ('derivedPgBytesPerMember','walBytesPerMember','vacuumBytesPerMember'): integer(c['storage'][k],1,100*TIB,k)
    integer(c['storage']['freeHeadroomPermille'],200,500,'freeHeadroomPermille')
    integer(c['storage']['brokerOverheadPermille'],1,1000,'brokerOverheadPermille')
    need(type(c['streams']) is list and 1<=len(c['streams'])<=32,'derived stream ledger required')
    seen=set()
    for s in c['streams']:
        keys(s,['name','normalEventsPerDay','qualificationEventsPerSecond','envelopeBytes'],'stream');name(s['name'],'stream name');need(s['name'] not in seen and s['name']!='raw-chain','duplicate/reserved stream');seen.add(s['name'])
        integer(s['normalEventsPerDay'],1,10**10,'normalEventsPerDay');integer(s['qualificationEventsPerSecond'],1,100000,'qualificationEventsPerSecond');integer(s['envelopeBytes'],1,1024*1024,'envelopeBytes')
    keys(c['connections'],['poolerReplicas','serverConnectionsPerPooler','otherDirectConnections','acquireTimeoutMs'],'connections')
    integer(c['connections']['poolerReplicas'],3,3,'poolerReplicas');need(c['connections']['poolerReplicas']==3,'three poolers required');integer(c['connections']['serverConnectionsPerPooler'],1,120,'serverConnectionsPerPooler');integer(c['connections']['otherDirectConnections'],0,180,'otherDirectConnections');integer(c['connections']['acquireTimeoutMs'],1,1000,'acquireTimeoutMs')
    need(3*c['connections']['serverConnectionsPerPooler']+c['connections']['otherDirectConnections']<=(540 if c['schemaVersion']==1 else 480),'application direct-server budget exceeds admitted headroom')
    keys(c['admission'],['apiInflightPerReplica','apiQueuePerReplica','quoteRps','writeRps','totalRps','burstRps','socketConnectionsPerReplica','socketSendQueueBytes','coldReadInflightPerReplica'],'admission')
    for k in ('apiInflightPerReplica','apiQueuePerReplica','coldReadInflightPerReplica'): integer(c['admission'][k],1,1024,k)
    for k in ('totalRps','burstRps','quoteRps','writeRps','socketConnectionsPerReplica'): integer(c['admission'][k],1,20000,k)
    need(c['admission']['totalRps']==10000 and c['admission']['burstRps']==20000,'required total RPS target')
    need(c['admission']['quoteRps']==2000 and c['admission']['writeRps']==2000,'quote/write subsets must be 2000 each')
    need(c['admission']['socketConnectionsPerReplica']==5000,'socket admission target 5000')
    integer(c['admission']['socketSendQueueBytes'],1024,1024*1024,'socketSendQueueBytes')
    keys(c['release'],['sourceDigest','interfaceDigest','qualificationMaxAgeSeconds','canaryPercent','canarySeconds'],'release')
    for k in ('sourceDigest','interfaceDigest'): sha(c['release'][k],k)
    integer(c['release']['qualificationMaxAgeSeconds'],600,7*86400,'qualificationMaxAgeSeconds')
    integer(c['release']['canaryPercent'],5,5,'canaryPercent');integer(c['release']['canarySeconds'],600,86400,'canarySeconds')
    need(c['release']['canaryPercent']==5 and c['release']['canarySeconds']>=600 and c['release']['canarySeconds']<=86400,'canary requires 5 percent for at least ten minutes')
    capacity(c) # Reject arithmetic-fit failure, not just syntactic invalidity.
    return c

def ceil_div(n,d): return (n+d-1)//d

def capacity(c):
    s=c['storage']; seconds=72*3600; replication=3
    rawNormal=10_000_000*3*1200; rawQualification=2000*seconds*1200
    derivedNormal=sum(x['normalEventsPerDay']*3*x['envelopeBytes'] for x in c['streams'])
    derivedQualification=sum(x['qualificationEventsPerSecond']*seconds*x['envelopeBytes'] for x in c['streams'])
    broker={}
    for mode,raw,derived in [('normal',rawNormal,derivedNormal),('qualification',rawQualification,derivedQualification)]:
        replicated=(raw+derived)*replication
        overhead=ceil_div(replicated*s['brokerOverheadPermille'],1000)
        logical_streams=[raw]+[(x['normalEventsPerDay']*3 if mode=='normal' else x['qualificationEventsPerSecond']*seconds)*x['envelopeBytes'] for x in c['streams']]
        required=sum(ceil_div(n*(1000+s['brokerOverheadPermille'])*2,1000)*3 for n in logical_streams)
        provisioned=(s['natsBytesPerMember']*9//10)*3
        need(required<=provisioned,f'{mode} broker storage exceeds provisioned bytes ({required}>{provisioned})')
        broker[mode]={'rawLogicalBytes':raw,'rawR3Bytes':raw*3,'rawR3HeadroomBytes':raw*6,'derivedLogicalBytes':derived,'derivedR3Bytes':derived*3,'brokerOverheadR3Bytes':overhead,'requiredBytesWithTwofoldHeadroom':required,'provisionedBytes':provisioned,'remainingBytes':provisioned-required}
    pgRaw=(29*10_000_000+2000*86400)*1200
    pgAmp=pgRaw*3
    pgUsed=pgAmp+s['derivedPgBytesPerMember']+s['walBytesPerMember']+s['vacuumBytesPerMember']
    pgRequired=ceil_div(pgUsed*1000,1000-s['freeHeadroomPermille'])
    need(pgRequired<=s['postgresBytesPerMember'],f'PostgreSQL storage requires expansion ({pgRequired}>{s["postgresBytesPerMember"]})')
    average=Fraction(500_000*2*900,86400)
    return {'classification':'arithmetic_plan_not_measured_capacity','dau':500000,'sessionsPerDay':1000000,'dynamicRequestsPerDay':50000000,'averageConcurrentSessions':{'numerator':average.numerator,'denominator':average.denominator,'decimalRounded2':'10416.67'},'peakClients':100000,'surgeClients':200000,'apiNormalSurvivors':16,'apiRpsPerSurvivor':625,'socketNormalSurvivors':20,'socketsPerSurvivor':5000,'marketPayloadBytesPerSecond':100000000,'marketBurstPayloadBytesPerSecond':400000000,'mediaCdnPayloadBitsPerSecond':40000000000,'peak24hRawBytes':2000*86400*1200,'broker':broker,'postgres':{'thirtyDayRawBytes':pgRaw,'rowIndexAmplifiedBytesPerMember':pgAmp,'rowIndexAmplifiedBytesThreeCopies':pgAmp*3,'derivedBytesPerMember':s['derivedPgBytesPerMember'],'walBytesPerMember':s['walBytesPerMember'],'vacuumBytesPerMember':s['vacuumBytesPerMember'],'requiredBytesPerMember':pgRequired,'provisionedBytesPerMember':s['postgresBytesPerMember'],'freeHeadroomPermille':s['freeHeadroomPermille']},'connections':{'serverGlobalCeiling':600,'reserved':60,'applicationCeiling':540,'declaredApplicationServerBudget':3*c['connections']['serverConnectionsPerPooler']+c['connections']['otherDirectConnections'],'apiMaxPotentialPooledClients':48*8,'allWorkloadPotentialPooledClients':sum(v[1]*v[4] for v in workloads(c).values())}}

def obj(kind,name,c,spec=None,api='v1',**kw):
    x={'apiVersion':api,'kind':kind,'metadata':{'name':name,'namespace':c['namespace'],'labels':{'kindle.io/topology':'v1'}}}
    if spec is not None:x['spec']=spec
    x.update(kw);return x

def placement(c,service,strict=True):
    labels={'app.kubernetes.io/name':service}
    return {'nodeAffinity':{'requiredDuringSchedulingIgnoredDuringExecution':{'nodeSelectorTerms':[{'matchExpressions':[{'key':'topology.kubernetes.io/zone','operator':'In','values':c['zones']},{'key':'topology.kubernetes.io/region','operator':'In','values':[c['region']]}]}]}},'podAntiAffinity':({'requiredDuringSchedulingIgnoredDuringExecution':[{'labelSelector':{'matchLabels':labels},'topologyKey':'topology.kubernetes.io/zone'}]} if strict else {'preferredDuringSchedulingIgnoredDuringExecution':[{'weight':100,'podAffinityTerm':{'labelSelector':{'matchLabels':labels},'topologyKey':'kubernetes.io/hostname'}}]})}

def resources(cpu,mem):return {'requests':{'cpu':str(cpu),'memory':f'{mem}Gi'},'limits':{'cpu':str(cpu),'memory':f'{mem}Gi'}}

def compile_plan(config):
    c=validate(copy.deepcopy(config)); items=[]
    items.append({'apiVersion':'v1','kind':'Namespace','metadata':{'name':c['namespace'],'labels':{'pod-security.kubernetes.io/enforce':'restricted','kindle.io/topology':'v1'}}})
    runtime={'CHAIN_ID':'125','COMMITTED_FINALITY':'true','CONFIRMATION_WAIT_BLOCKS':'0','RPC_CONFLICT_POLICY':'quarantine_endpoint_no_rollback','RPC_HEAD_INGEST_P99_MS':'500','RPC_ENDPOINTS_JSON':json.dumps(c['rpcEndpoints']),'MEDIA_CDN_BASE_URL':c['mediaCdn'],'MEDIA_PROXY_PAYLOAD':'false','TOPOLOGY_REVISION':str(c['revision']),'CONFIG_SHA256':digest(c),'PG_ACQUIRE_TIMEOUT_MS':str(c['connections']['acquireTimeoutMs']),'PG_TLS_MODE':'verify-full','PG_POOL_MODE':'transaction','PG_MAX_CONNECTIONS_GLOBAL':'600','PG_RESERVED_CONNECTIONS':'60','REDIS_AUTHORITY':'expendable','REDIS_CACHE_SHARD_MODE':'consistent_hash','RATE_LIMIT_UNAVAILABLE_POLICY':'deny_sensitive_mutation','REDIS_REBUILD_CONCURRENCY':str(c['admission']['coldReadInflightPerReplica']),'READINESS_DEPENDENCIES':'postgres,nats,rpc_identity','LIVENESS_DEPENDENCIES':'process_only','SHUTDOWN_PROPAGATION_SECONDS':'5','SHUTDOWN_DRAIN_SECONDS':'30','SOCKET_RECONNECT_MODE':'durable_cursor_replay','INDEXER_LEADER_MODE':'durable_fenced_lease','EVENT_ACK_MODE':'database_commit_before_broker_ack','SOCKET_MAX_CONNECTIONS':str(c['admission']['socketConnectionsPerReplica']),'SOCKET_SEND_QUEUE_BYTES':str(c['admission']['socketSendQueueBytes']),'API_MAX_INFLIGHT':str(c['admission']['apiInflightPerReplica']),'API_MAX_QUEUE':str(c['admission']['apiQueuePerReplica']),'GLOBAL_DYNAMIC_RPS':'10000','GLOBAL_WRITE_RPS':'2000','GLOBAL_QUOTE_RPS':'2000','ADMISSION_PRIORITY':'durable_mutation,quote,read,nonessential','STALE_READ_METADATA':'required','PG_DSN_SECRET_KEY':'DATABASE_URL','NATS_URL_SECRET_KEY':'NATS_URL','PG_POOL_HOST':'postgres-pooler','NATS_AUTH_MODE':'nkey_per_service'}
    items.append(obj('ConfigMap','runtime-topology',c,data=runtime,immutable=True))
    for service,(replicas,maxReplicas,cpu,mem,pool) in workloads(c).items():
        labels={'app.kubernetes.io/name':service,'kindle.io/client':'true'}
        items.append(obj('ServiceAccount',service,c,automountServiceAccountToken=False))
        container={'name':service,'image':c['images'][service],'imagePullPolicy':'IfNotPresent','ports':[{'name':'http','containerPort':8080}],'resources':resources(cpu,mem),'securityContext':{'allowPrivilegeEscalation':False,'readOnlyRootFilesystem':True,'runAsNonRoot':True,'runAsUser':10001,'capabilities':{'drop':['ALL']}},'envFrom':[{'configMapRef':{'name':'runtime-topology'}},{'secretRef':{'name':c['secretRefs']['runtime'][service]}}],'env':[{'name':'SERVICE_NAME','value':service},{'name':'PG_POOL_MAX_CONNS','value':str(pool)}],'livenessProbe':{'httpGet':{'path':'/health/live','port':'http'},'timeoutSeconds':2,'failureThreshold':3,'periodSeconds':10},'readinessProbe':{'httpGet':{'path':'/health/ready','port':'http'},'timeoutSeconds':2,'failureThreshold':3,'periodSeconds':5},'startupProbe':{'httpGet':{'path':'/health/live','port':'http'},'timeoutSeconds':2,'failureThreshold':30,'periodSeconds':2},'volumeMounts':[{'name':'tmp','mountPath':'/tmp'}]}
        if c['schemaVersion']==2:
            d=c['runtimeDocuments'][service]
            container['env'] += [{'name':'NODE_ENV','value':'production'},{'name':'RUNTIME_PROFILE','value':'container'},{'name':'BIND_ADDRESS','value':'0.0.0.0'},{'name':'PORT','value':'8080'},{'name':'SECRETS_PROVIDER','value':'env'},{'name':'SECRETS_REFRESH_SECONDS','value':'30'},{'name':'DB_BUDGET_FILE','value':'/run/kindle/budget.json'},{'name':'DB_BUDGET_SHA256','value':d['budgetSHA256']},{'name':'EXPECTED_SCHEMA_FILE','value':'/run/kindle/readiness.json'},{'name':'EXPECTED_SCHEMA_SHA256','value':d['expectedSchemaSHA256']},{'name':'DB_POOL_ID','value':d['poolId']},{'name':'INSTANCE_ID','valueFrom':{'fieldRef':{'fieldPath':'metadata.name'}}}]
            if service in ('api','sockets'):container['env'].append({'name':'API_ROLE','value':'rest' if service=='api' else 'sockets'})
            container['volumeMounts'].append({'name':'runtime-evidence','mountPath':'/run/kindle','readOnly':True})
        pod={'serviceAccountName':service,'automountServiceAccountToken':False,'terminationGracePeriodSeconds':45,'securityContext':{'runAsNonRoot':True,'runAsUser':10001,'seccompProfile':{'type':'RuntimeDefault'}},'affinity':placement(c,service,False),'topologySpreadConstraints':[{'maxSkew':1,'minDomains':3,'topologyKey':'topology.kubernetes.io/zone','whenUnsatisfiable':'DoNotSchedule','nodeAffinityPolicy':'Honor','nodeTaintsPolicy':'Honor','labelSelector':{'matchLabels':{'app.kubernetes.io/name':service}}}],'containers':[container],'volumes':[{'name':'tmp','emptyDir':{'sizeLimit':'256Mi'}}]}
        if c['schemaVersion']==2:pod['volumes'].append({'name':'runtime-evidence','secret':{'secretName':c['secretRefs']['runtime'][service]}})
        items.append(obj('Deployment',service,c,{'replicas':replicas,'revisionHistoryLimit':3,'strategy':{'type':'RollingUpdate','rollingUpdate':{'maxSurge':1,'maxUnavailable':0}},'selector':{'matchLabels':{'app.kubernetes.io/name':service}},'template':{'metadata':{'labels':labels,'annotations':{'kindle.io/config-sha256':digest(c)}},'spec':pod}},api='apps/v1'))
        items.append(obj('Service',service,c,{'type':'ClusterIP','selector':{'app.kubernetes.io/name':service},'ports':[{'name':'http','port':8080,'targetPort':'http'}]}))
        items.append(obj('PodDisruptionBudget',service,c,{'minAvailable':replicas*2//3,'selector':{'matchLabels':{'app.kubernetes.io/name':service}}},api='policy/v1'))
        items.append(obj('HorizontalPodAutoscaler',service,c,{'minReplicas':replicas,'maxReplicas':maxReplicas,'scaleTargetRef':{'apiVersion':'apps/v1','kind':'Deployment','name':service},'metrics':[{'type':'Resource','resource':{'name':'cpu','target':{'type':'Utilization','averageUtilization':60}}}],'behavior':{'scaleDown':{'stabilizationWindowSeconds':600},'scaleUp':{'stabilizationWindowSeconds':0,'policies':[{'type':'Percent','value':100,'periodSeconds':60}]}}},api='autoscaling/v2'))
    items.append(obj('Cluster','postgres',c,{'instances':3,'imageName':c['images']['postgres'],'enableSuperuserAccess':False,'resources':resources(16,64),'storage':{'size':str(c['storage']['postgresBytesPerMember']),'storageClass':c['storageClasses']['postgres'],'resizeInUseVolumes':True},'affinity':{'enablePodAntiAffinity':True,'podAntiAffinityType':'required','topologyKey':'topology.kubernetes.io/zone','nodeSelector':{'topology.kubernetes.io/region':c['region']}},'postgresql':{'parameters':{'max_connections':'600','superuser_reserved_connections':'60','synchronous_commit':'on'},'synchronous':{'method':'any','number':1,'dataDurability':'required','failoverQuorum':True}},'bootstrap':{'initdb':{'database':'kindle','owner':'kindle_app','secret':{'name':c['secretRefs']['postgresBootstrap']},'dataChecksums':True}},'managed':{'services':{'disabledDefaultServices':['ro','r']}}},api='postgresql.cnpg.io/v1'))
    items.append(obj('Pooler','postgres-pooler',c,{'cluster':{'name':'postgres'},'instances':3,'type':'rw','pgbouncer':{'poolMode':'transaction','parameters':{'default_pool_size':'1','max_db_connections':str(c['connections']['serverConnectionsPerPooler']),'max_user_connections':str(c['connections']['serverConnectionsPerPooler']),'max_client_conn':'1024','query_wait_timeout':'1','server_tls_sslmode':'verify-full'}},'template':{'metadata':{'labels':{'app.kubernetes.io/name':'postgres-pooler'}},'spec':{'affinity':placement(c,'postgres-pooler'),'containers':[{'name':'pgbouncer','resources':resources(2,2)}]}}},api='postgresql.cnpg.io/v1'))
    natsconf='''server_name: $POD_NAME
port: 4222
http_port: 8222
max_payload: 1048576
max_connections: 20000
jetstream { store_dir: /data/jetstream; max_memory_store: 1GB; max_file_store: NATS_STORE_LIMIT }
tls { cert_file: /tls/tls.crt; key_file: /tls/tls.key; ca_file: /tls/ca.crt; verify: true; timeout: 2 }
include ../auth/accounts.conf
cluster { name: kindle; port: 6222; routes: [ nats://nats-0.nats:6222, nats://nats-1.nats:6222, nats://nats-2.nats:6222 ]; tls { cert_file: /tls/tls.crt; key_file: /tls/tls.key; ca_file: /tls/ca.crt; verify: true; timeout: 2 } }
'''.replace('NATS_STORE_LIMIT',str(c['storage']['natsBytesPerMember']*9//10))
    items.append(obj('ConfigMap','nats-config',c,data={'nats.conf':natsconf},immutable=True))
    stream_contracts=[]
    for stream_name,rate,size in [('raw-chain',2000,1200)]+[(x['name'],x['qualificationEventsPerSecond'],x['envelopeBytes']) for x in c['streams']]:
        stream_contracts.append({'name':stream_name.replace('-','_').upper(),'subjects':['kindle.'+stream_name+'.>'],'retention':'limits','storage':'file','num_replicas':3,'max_age':259200*10**9,'max_bytes':ceil_div(rate*259200*size*(1000+c['storage']['brokerOverheadPermille'])*2,1000),'max_msg_size':1048576,'discard':'new','deny_delete':True,'deny_purge':True,'duplicate_window':120*10**9,'placement':{'cluster':'kindle'}})
    items.append(obj('ConfigMap','jetstream-stream-contracts',c,data={'streams.json':json.dumps(stream_contracts,sort_keys=True)},immutable=True))
    for service,cpu,mem,replicas in [('nats',8,32,3),('redis',4,16,3)]:
        labels={'app.kubernetes.io/name':service}
        items.append(obj('ServiceAccount',service,c,automountServiceAccountToken=False))
        volumes=[];mounts=[];args=[]
        if service=='nats':
            args=['--config','/config/nats.conf'];volumes=[{'name':'config','configMap':{'name':'nats-config'}},{'name':'tls','secret':{'secretName':c['secretRefs']['natsTls']}},{'name':'auth','secret':{'secretName':c['secretRefs']['natsAuth']}}];mounts=[{'name':'config','mountPath':'/config','readOnly':True},{'name':'tls','mountPath':'/tls','readOnly':True},{'name':'auth','mountPath':'/auth','readOnly':True},{'name':'data','mountPath':'/data'}];ports=[{'name':'client','containerPort':4222},{'name':'route','containerPort':6222},{'name':'monitor','containerPort':8222}]
        else:
            # AUTH and TLS configuration is an externally provisioned secret, never CLI plaintext.
            args=['redis-server','/config/redis.conf'];volumes=[{'name':'config','secret':{'secretName':c['secretRefs']['redis']}}];mounts=[{'name':'config','mountPath':'/config','readOnly':True}];ports=[{'name':'redis','containerPort':6379}]
        container={'name':service,'image':c['images'][service],'args':args,'ports':ports,'resources':resources(cpu,mem),'env':[{'name':'POD_NAME','valueFrom':{'fieldRef':{'fieldPath':'metadata.name'}}}],'securityContext':{'runAsNonRoot':True,'runAsUser':10001,'allowPrivilegeEscalation':False,'readOnlyRootFilesystem':True,'capabilities':{'drop':['ALL']}},'volumeMounts':mounts}
        if service=='nats': container['readinessProbe']={'httpGet':{'path':'/healthz?js-enabled-only=true','port':'monitor'},'timeoutSeconds':2,'failureThreshold':3}
        else: container['readinessProbe']={'tcpSocket':{'port':'redis'},'timeoutSeconds':2,'failureThreshold':3}
        pod={'serviceAccountName':service,'automountServiceAccountToken':False,'terminationGracePeriodSeconds':45,'securityContext':{'runAsNonRoot':True,'runAsUser':10001,'fsGroup':10001,'seccompProfile':{'type':'RuntimeDefault'}},'affinity':placement(c,service),'containers':[container],'volumes':volumes}
        spec={'replicas':replicas,'serviceName':service,'selector':{'matchLabels':labels},'template':{'metadata':{'labels':labels},'spec':pod}}
        if service=='nats':spec['volumeClaimTemplates']=[{'metadata':{'name':'data'},'spec':{'accessModes':['ReadWriteOnce'],'storageClassName':c['storageClasses']['nats'],'resources':{'requests':{'storage':str(c['storage']['natsBytesPerMember'])}}}}]
        items.append(obj('StatefulSet',service,c,spec,api='apps/v1'))
        items.append(obj('Service',service,c,{'type':'ClusterIP','clusterIP':'None','selector':labels,'ports':[{'name':p['name'],'port':p['containerPort'],'targetPort':p['name']} for p in ports]}))
        items.append(obj('PodDisruptionBudget',service,c,{'minAvailable':2,'selector':{'matchLabels':labels}},api='policy/v1'))
    items.append(obj('NetworkPolicy','default-deny',c,{'podSelector':{},'policyTypes':['Ingress','Egress']},api='networking.k8s.io/v1'))
    items.append(obj('NetworkPolicy','dns',c,{'podSelector':{},'policyTypes':['Egress'],'egress':[{'to':[{'namespaceSelector':{'matchLabels':{'kubernetes.io/metadata.name':'kube-system'}},'podSelector':{'matchLabels':{'k8s-app':'kube-dns'}}}],'ports':[{'protocol':'UDP','port':53},{'protocol':'TCP','port':53}]}]},api='networking.k8s.io/v1'))
    # Namespace-local traffic grants are explicit by role and port; no public data-plane ingress.
    for target,ports,sources in [('api',[8080],['sockets']),('postgres',[5432],['postgres','postgres-pooler']),('postgres-pooler',[5432],list(workloads(c))),('nats',[4222],list(workloads(c))),('nats',[6222],['nats']),('redis',[6379],list(workloads(c)))]:
        label={'cnpg.io/cluster':'postgres'} if target=='postgres' else {'app.kubernetes.io/name':target}
        peers=[{'podSelector':{'matchLabels':({'cnpg.io/cluster':'postgres'} if s=='postgres' else {'app.kubernetes.io/name':s})}} for s in sources]
        ps=[{'protocol':'TCP','port':p} for p in ports]
        ident=f'allow-{target}-{ports[0]}'
        items.append(obj('NetworkPolicy',ident,c,{'podSelector':{'matchLabels':label},'policyTypes':['Ingress'],'ingress':[{'from':peers,'ports':ps}]},api='networking.k8s.io/v1'))
        for source in sources:
            sl={'cnpg.io/cluster':'postgres'} if source=='postgres' else {'app.kubernetes.io/name':source}
            items.append(obj('NetworkPolicy',f'egress-{source}-{target}-{ports[0]}',c,{'podSelector':{'matchLabels':sl},'policyTypes':['Egress'],'egress':[{'to':[{'podSelector':{'matchLabels':label}}],'ports':ps}]},api='networking.k8s.io/v1'))
    for service in (('api','sockets') if c['schemaVersion']==1 else ('api','sockets','gateway','user','metadata','livestream')):
        items.append(obj('NetworkPolicy',f'edge-{service}',c,{'podSelector':{'matchLabels':{'app.kubernetes.io/name':service}},'policyTypes':['Ingress'],'ingress':[{'from':[{'namespaceSelector':{'matchLabels':{'kubernetes.io/metadata.name':c['edgeNamespace']}}}],'ports':[{'protocol':'TCP','port':8080}]}]},api='networking.k8s.io/v1'))
    items.append(obj('Ingress','edge',c,{'ingressClassName':'kindle-edge','tls':[{'hosts':[c['publicHost']],'secretName':c['secretRefs']['edgeTls']}],'rules':[{'host':c['publicHost'],'http':{'paths':[{'path':'/v1/stream','pathType':'Prefix','backend':{'service':{'name':'sockets','port':{'number':8080}}}},{'path':'/','pathType':'Prefix','backend':{'service':{'name':'api','port':{'number':8080}}}}]}}]},api='networking.k8s.io/v1'))
    if c['schemaVersion']==2:
        ingress=items[-1]
        def route(path,service):return {'path':path,'pathType':'Prefix','backend':{'service':{'name':service,'port':{'number':8080}}}}
        ingress['spec']['rules'][0]['http']['paths']=[route('/v2/auth','gateway'),route('/v2/stream/tickets','gateway'),route('/v2/stream','sockets'),route('/v2/snapshots','sockets'),route('/v1/me','user'),route('/v1/profiles','user'),route('/v2/tokens','metadata'),route('/v2/streams','livestream'),route('/social','gateway'),route('/','api')]
        for source,target in [('api','stats'),('sockets','gateway'),('user','gateway'),('social','gateway'),('livestream','gateway'),('gateway','social')]:
            port=[{'protocol':'TCP','port':8080}];sl={'app.kubernetes.io/name':source};tl={'app.kubernetes.io/name':target}
            items.append(obj('NetworkPolicy',f'v2-egress-{source}-{target}',c,{'podSelector':{'matchLabels':sl},'policyTypes':['Egress'],'egress':[{'to':[{'podSelector':{'matchLabels':tl}}],'ports':port}]},api='networking.k8s.io/v1'))
            items.append(obj('NetworkPolicy',f'v2-ingress-{source}-{target}',c,{'podSelector':{'matchLabels':tl},'policyTypes':['Ingress'],'ingress':[{'from':[{'podSelector':{'matchLabels':sl}}],'ports':port}]},api='networking.k8s.io/v1'))
    manifest={'apiVersion':'v1','kind':'List','items':items}
    plan={'schemaVersion':1,'configurationDigest':digest(c),'deploymentDigest':digest(manifest),'sourceDigest':c['release']['sourceDigest'],'interfaceDigest':c['release']['interfaceDigest'],'classification':'declarative_unqualified','capacity':capacity(c),'resources':manifest,'requirements':{r:{'phase1':'declaration_and_offline_validation','runtimeEvidence':'required_not_present'} for r in REQS},'prerequisites':['Kubernetes API and CNI network-policy enforcement','pinned CNPG CRDs/operator and independently verified failover fencing','provider three-zone independent compute and encrypted storage classes','per-service runtime secrets and TLS/auth account permissions','L42 runtime hooks implemented by serial integrator','L43 metrics and L48/L49 measured fault/capacity evidence','operator controller ingress/egress rules and explicit RPC/CDN egress approved by security lane']}
    validate_rendered(plan,c)
    return plan

def validate_rendered(plan,c):
    need(type(plan) is dict and type(plan.get('schemaVersion')) is int and plan.get('schemaVersion')==1,'plan schema')
    need(plan.get('configurationDigest')==digest(c),'plan configuration digest mismatch')
    expected=plan.get('deploymentDigest');sha(expected,'deploymentDigest');need(expected==digest(plan.get('resources')),'deployment digest mismatch')
    seen=set(); items=plan['resources'].get('items');need(type(items) is list and 1<=len(items)<=512,'bounded resource list')
    for x in items:
        kind=x.get('kind');m=x.get('metadata',{}); ident=(kind,m.get('name'));need(ident not in seen,f'duplicate resource {ident}');seen.add(ident)
        if kind!='Namespace':need(m.get('namespace')==c['namespace'],f'{ident}: foreign namespace')
        spec=x.get('spec',{})
        need(not spec.get('externalIPs'),f'{ident}: public externalIPs forbidden')
        if kind=='Service': need(spec.get('type')=='ClusterIP',f'{ident}: public service forbidden')
        if kind=='Ingress':need(m['name']=='edge' and all(p['backend']['service']['name'] in (('api','sockets') if c['schemaVersion']==1 else ('api','sockets','gateway','user','metadata','livestream')) for r in spec['rules'] for p in r['http']['paths']),f'{ident}: unauthorized public backend')
        if kind in ('Deployment','StatefulSet'):
            pod=spec['template']['spec'];need(not pod.get('hostNetwork') and not pod.get('hostPID'),'host isolation required')
            need(pod.get('automountServiceAccountToken') is False,'service account tokens forbidden')
            for container in pod['containers']:
                need(bool(IMAGE.fullmatch(container.get('image',''))),f'{ident}: mutable image')
                need(container.get('securityContext',{}).get('allowPrivilegeEscalation') is False,'privilege escalation forbidden')
    need(('NetworkPolicy','default-deny') in seen,'default deny missing')
    # Exact compiler comparison is the allowlist: changing any nested policy or resource fails closed.
    return True

def assert_exact_plan(plan,c):
    validate_rendered(plan,c)
    need(plan==compile_plan(c),'rendered plan differs from reviewed compiler output')

METRICS={
'D14_001':'zone_available_replicas','D14_002':'capacity_assumption_revision','D14_003':'api_inflight_requests','D14_004':'socket_open_connections','D14_005':'socket_payload_bytes_total','D14_006':'postgres_replication_lag_bytes','D14_007':'postgres_server_connections','D14_008':'jetstream_replica_current','D14_009':'jetstream_storage_used_bytes','D14_010':'consumer_oldest_unprocessed_age_seconds','D14_011':'redis_cache_hit_ratio','D14_012':'admission_rejected_requests_total','D14_013':'deployment_digest_mismatch_total','D14_014':'shutdown_drain_timeout_total','D14_015':'service_readiness_dependency_failures_total','D14_016':'rpc_quarantine_total','D14_017':'media_control_requests_total','D14_018':'infrastructure_drift_resources'}

def timestamp(value,ctx):
    text(value,ctx,40)
    try:dt=datetime.fromisoformat(value.replace('Z','+00:00'))
    except ValueError as e:raise Invalid(f'{ctx}: timestamp required') from e
    need(dt.tzinfo is not None and dt.utcoffset().total_seconds()==0,f'{ctx}: UTC timestamp required');return dt

def validate_evidence(e,plan,c,now=None):
    """Validate coordinator-attested results; this does not authenticate the attester.

    The promotion pipeline MUST verify the external signed attestation before calling
    this function. Offline synthetic test evidence is never deploy authorization.
    """
    assert_exact_plan(plan,c)
    keys(e,['schemaVersion','classification','configurationDigest','deploymentDigest','sourceDigest','interfaceDigest','attestationDigest','startedAt','finishedAt','inventoryDigest','scenarios','measurements','measuredStreams'],'evidence')
    need(type(e['schemaVersion']) is int and e['schemaVersion']==1 and e['classification']=='production_shaped_measured','measured evidence required')
    for k in ('configurationDigest','deploymentDigest','sourceDigest','interfaceDigest'):need(e[k]==plan[k],f'evidence {k} mismatch')
    for k in ('attestationDigest','inventoryDigest'):sha(e[k],k)
    start=timestamp(e['startedAt'],'startedAt');end=timestamp(e['finishedAt'],'finishedAt');now=now or datetime.now(timezone.utc)
    need(start<=end<=now,'evidence time order/future invalid');need((now-end).total_seconds()<=c['release']['qualificationMaxAgeSeconds'],'evidence expired')
    need(type(e['scenarios']) is list and len(e['scenarios'])==18,'all eighteen runtime scenarios required')
    seen=set()
    for x in e['scenarios']:
        keys(x,['requirement','passed','startedAt','finishedAt','inputDigest','artifactDigest','reconciliationDigest','metric','sampleCount'],'scenario')
        r=x['requirement'];need(r in REQS and r not in seen,'unknown or duplicate requirement evidence');seen.add(r)
        need(x['passed'] is True,f'{r} failed');need(start<=timestamp(x['startedAt'],'scenario start')<=timestamp(x['finishedAt'],'scenario finish')<=end,'scenario interval invalid')
        for k in ('inputDigest','artifactDigest','reconciliationDigest'):sha(x[k],k)
        need(x['metric']==METRICS[r],f'{r} required metric missing');integer(x['sampleCount'],2,10000000,'metric sampleCount')
    m=e['measurements']; required={'apiRpsPerReplicaZoneLoss','socketConnectionsPerReplicaZoneLoss','peakDynamicRps','burstDynamicRps','peakConnectedClients','surgeConnectedClients','framePayloadBytesPerSecond','burstFramePayloadBytesPerSecond','postgresApplicationConnections','activePostgresWriters','duplicateDatabaseEffects','lostAcknowledgedWrites','ingestP99Ms','drainSeconds','probeTimeoutMs','recoverySeconds','mediaCdnBitsPerSecond','apiMediaPayloadBytes','brokerOverheadPermille','postgresDerivedBytesPerMember','walBytesPerMember','vacuumBytesPerMember','soakSeconds','replayRetentionSeconds'}
    keys(m,required,'measurements')
    for k,v in m.items():integer(v,0,2**63-1,k)
    minimum={'apiRpsPerReplicaZoneLoss':625,'socketConnectionsPerReplicaZoneLoss':5000,'peakDynamicRps':10000,'burstDynamicRps':20000,'peakConnectedClients':100000,'surgeConnectedClients':200000,'framePayloadBytesPerSecond':100000000,'burstFramePayloadBytesPerSecond':400000000,'mediaCdnBitsPerSecond':40000000000,'soakSeconds':86400,'replayRetentionSeconds':259200}
    need((end-start).total_seconds()>=m['soakSeconds'],'claimed soak exceeds evidence interval')
    for k,v in minimum.items():need(m[k]>=v,f'measured {k} below target')
    for k,v in {'postgresApplicationConnections':540,'ingestP99Ms':500,'drainSeconds':30,'probeTimeoutMs':2000,'recoverySeconds':900}.items():need(m[k]<=v,f'measured {k} exceeds ceiling')
    need(m['activePostgresWriters']==1,'exactly one PostgreSQL writer required')
    for k in ('duplicateDatabaseEffects','lostAcknowledgedWrites','apiMediaPayloadBytes'):need(m[k]==0,f'{k} must be zero')
    need(m['brokerOverheadPermille']>0,'measured broker overhead absent')
    for measured,configured in [('brokerOverheadPermille','brokerOverheadPermille'),('postgresDerivedBytesPerMember','derivedPgBytesPerMember'),('walBytesPerMember','walBytesPerMember'),('vacuumBytesPerMember','vacuumBytesPerMember')]:
        need(0<m[measured]<=c['storage'][configured],f'measured {measured} absent or exceeds declared budget')
    streams=e['measuredStreams'];need(type(streams) is list and len(streams)==len(c['streams']),'complete measured derived stream ledger required')
    configured={x['name']:x for x in c['streams']}; observed=set()
    for x in streams:
        keys(x,['name','normalEventsPerDay','qualificationEventsPerSecond','envelopeBytes','retainedLogicalBytes'],'measured stream')
        n=x['name'];need(n in configured and n not in observed,'unknown/duplicate measured stream');observed.add(n)
        for k in ('normalEventsPerDay','qualificationEventsPerSecond','envelopeBytes','retainedLogicalBytes'):integer(x[k],1,2**63-1,k)
        for k in ('normalEventsPerDay','qualificationEventsPerSecond','envelopeBytes'):need(x[k]<=configured[n][k],f'measured stream {n} {k} exceeds budget')
        need(x['retainedLogicalBytes']<=configured[n]['qualificationEventsPerSecond']*259200*configured[n]['envelopeBytes'],f'measured stream {n} retained bytes exceed budget')
    return {'schemaVersion':1,'status':'evidence_structurally_valid_external_signature_required','deploymentDigest':plan['deploymentDigest'],'evidenceDigest':digest(e),'notDeploymentAuthorization':True}

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('command',choices=['validate','render','verify-plan','check-evidence']);parser.add_argument('--config',required=True);parser.add_argument('--plan');parser.add_argument('--evidence');parser.add_argument('--output')
    a=parser.parse_args()
    try:
        c=validate(load(a.config))
        if a.command=='validate':out={'status':'valid_arithmetic_plan_unqualified','configurationDigest':digest(c),'capacity':capacity(c)}
        elif a.command=='render':out=compile_plan(c)
        elif a.command=='verify-plan':need(bool(a.plan),'--plan required');assert_exact_plan(load(a.plan),c);out={'status':'exact_reviewed_plan_unqualified'}
        else:need(bool(a.plan) and bool(a.evidence),'--plan and --evidence required');out=validate_evidence(load(a.evidence),load(a.plan),c)
        data=json.dumps(out,indent=2,sort_keys=True)+'\n'
        if a.output:Path(a.output).write_text(data)
        else:print(data,end='')
        return 0
    except (Invalid,OSError,TypeError,KeyError) as ex:
        print(json.dumps({'status':'rejected','error':str(ex)}));return 1
if __name__=='__main__':raise SystemExit(main())
