#!/usr/bin/env bash
set -euo pipefail
exec python3 - "$@" <<'PY'
import base64,fcntl,hashlib,json,os,re,shutil,ssl,stat,subprocess,sys,time,urllib.error,urllib.parse,urllib.request
from pathlib import Path
os.umask(0o077)
ROOT=Path.cwd()
OUT=Path(os.environ.get('PAXEER_X_WEBHOOK_DASHBOARD_ARTIFACTS','/root/lx-ops/paxeer-x-integration-2026-10-03/task-104.17.5-artifacts'))
OUT.mkdir(mode=0o700,parents=True,exist_ok=True)
NODE='/root/lx-toolchains/node24/bin/node'
CARGO='/root/.cargo/bin/cargo'
TARGET=Path(os.environ.get('CARGO_TARGET_DIR','/root/lx-target/platform-webhook-dashboard'))
def require(ok,reason):
 if not ok:raise RuntimeError(reason)
def sha(path):return hashlib.sha256(Path(path).read_bytes()).hexdigest()
def git(*args):return subprocess.check_output(['git','--no-optional-locks',*args],cwd=ROOT,text=True).strip()
def sources():
 paths=subprocess.check_output(['git','ls-files','-z'],cwd=ROOT).split(b'\0')
 return {os.fsdecode(path):sha(ROOT/os.fsdecode(path)) for path in paths if path and not any(part.startswith(b'.env') for part in path.split(b'/')) and (path.startswith((b'platform/',b'agent/crates/')) or path==b'tools/paxeer-x/gates/104.17.5.sh')}
def run(argv,label,timeout=600,env=None):
 with (OUT/(label+'.log')).open('w') as log:
  result=subprocess.run(argv,cwd=ROOT,stdout=log,stderr=subprocess.STDOUT,timeout=timeout,env=env)
 require(result.returncode==0,label+' exit '+str(result.returncode)+'; '+str(OUT/(label+'.log')))
 return (OUT/(label+'.log')).read_text()
def private(path):
 path=Path(path);require(not any(part.startswith('.env') for part in path.parts),'credential filename forbidden')
 fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
 try:
  info=os.fstat(fd);require(stat.S_ISREG(info.st_mode) and info.st_uid==os.geteuid() and info.st_nlink==1 and not info.st_mode&0o077,'private owned input required')
  require(info.st_size<=4*1024*1024,'input exceeds bound')
  with os.fdopen(fd,'r') as stream:fd=-1;return stream.read()
 finally:
  if fd>=0:os.close(fd)
def build():
 require(not git('status','--porcelain=v1'),'published immutable source required')
 before=sources();env=dict(os.environ,PATH='/root/lx-toolchains/node24/bin:/root/.cargo/bin:'+os.environ.get('PATH',''),RUSTUP_TOOLCHAIN='1.91.1',CARGO_BUILD_JOBS='2',CARGO_TARGET_DIR=str(TARGET))
 with Path('/root/lx-cargo/platform-tooling.lock').open('a') as lock:
  fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
  raw=run([CARGO,'test','--locked','--manifest-path','platform/Cargo.toml','-p','layerx-platform-webhooks','-p','layerx-platform-dashboard','--test','plain_listener','--no-run','--message-format=json'],'rust-build',env=env)
  artifacts=[]
  for line in raw.splitlines():
   try:row=json.loads(line)
   except ValueError:continue
   if row.get('reason')=='compiler-artifact' and row.get('executable'):
    source=Path(row['executable']);name=row['target']['name'];kind=row['target']['kind'];identity=str(row['package_id'])
    if name=='plain_listener' or name in ('layerx-webhooks','layerx-dashboard','verify_delivery'):
     captured=OUT/('dashboard-' if 'dashboard' in identity else 'webhooks-')/source.name
     captured.parent.mkdir(mode=0o700,exist_ok=True);shutil.copyfile(source,captured);captured.chmod(0o700)
     artifacts.append({'original':str(source),'path':str(captured),'sha256':sha(captured),'name':name,'kind':kind})
  require(sum(row['name']=='plain_listener' for row in artifacts)==2,'both genuine listener corpora required')
  require({row['name'] for row in artifacts}>={'layerx-webhooks','layerx-dashboard','verify_delivery'},'actual production executables required')
  run(['/root/lx-toolchains/node24/bin/npm','--prefix','platform/hosted/dashboard/web','run','build'],'dashboard-build',env=env)
  require(before==sources(),'source changed during compilation')
  (OUT/'manifest.json').write_text(json.dumps({'revision':git('rev-parse','HEAD'),'sources':before,'artifacts':artifacts},indent=2)+'\n')
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None
def request(url,headers,context,method='GET'):
 require(url.startswith('https://'),'HTTPS genuine service required')
 try:
  with urllib.request.build_opener(NoRedirect(),urllib.request.HTTPSHandler(context=context)).open(urllib.request.Request(url,headers=headers,method=method,data=b'' if method=='POST' else None),timeout=20) as response:
   require(response.geturl()==url,'redirect refused');require(response.headers.get_content_type()=='application/json','JSON service reply required');raw=response.read(1024*1024+1);require(len(raw)<=1024*1024,'service reply bound');return response.status,json.loads(raw)
 except urllib.error.HTTPError as error:return error.code,json.loads(error.read(1024*1024))
def runtime():
 value=os.environ.get('PAXEER_X_WEBHOOK_DASHBOARD_FIXTURE')
 if not value:print('genuine disposable webhook/dashboard/receiver fixture required',file=sys.stderr);raise SystemExit(78)
 fixture=json.loads(private(value));require(fixture['schema']=='paxeer-x.webhook-dashboard-fixture.v1' and fixture['disposable'] is True and fixture['source_revision']==git('rev-parse','HEAD'),'current-source disposable authority required')
 public=ssl.create_default_context(cafile=fixture['public_ca_file']);internal=ssl.create_default_context(cafile=fixture['ingress_ca_file']);internal.load_cert_chain(fixture['producer_certificate_file'],fixture['producer_key_file'])
 session=private(fixture['session_cookie_file']).strip();csrf=private(fixture['csrf_file']).strip();token=private(fixture['source_trigger_token_file']).strip();receiver=private(fixture['receiver_token_file']).strip()
 headers={'Cookie':'__Host-layerx-session='+session,'Accept':'application/json'};producer={'Authorization':'Bearer '+token,'Accept':'application/json'}
 origin=fixture['unified_origin'].rstrip('/');ingress=fixture['ingress_origin'].rstrip('/');endpoint=fixture['endpoint_id'];require(set(fixture['events'])=={'journey','payment','approval','program'},'all four actual event families required')
 for route in ('overview','keys','usage','requests','webhooks','webhook-deliveries','webhook-dead-letters','test-payments'):
  status,_=request(origin+'/v1/dashboard/'+route,headers,public);require(status==200,'unified dashboard route unavailable: '+route)
 status,page=request(origin+'/v1/webhooks/endpoints/'+endpoint+'/events?limit=1',headers,public);require(status==200 and isinstance(page['next_cursor'],str),'stable cursor required');cursor=page['next_cursor']
 for kind,events in fixture['events'].items():
  require(set(events)=={'first','second','stale'},'actual fault corpus identifiers required')
  for label in ('first','second'):
   status,_=request(ingress+'/internal/v1/events/'+kind+'/'+events[label],producer,internal,'POST');require(status==202,'canonical event admission failed')
  status,result=request(ingress+'/internal/v1/events/'+kind+'/'+events['first'],producer,internal,'POST');require(status==202 and result['duplicate'] is True,'duplicate event must remain idempotent')
  status,_=request(ingress+'/internal/v1/events/'+kind+'/'+events['stale'],producer,internal,'POST');require(status==409,'out-of-order source must refuse')
  status,_=request(origin+'/internal/v1/events/'+kind+'/'+events['first'],producer,public,'POST');require(status==404,'private producer cannot be public')
 deadline=time.monotonic()+180
 while True:
  status,observed=request(fixture['receiver_observations_url']+'?endpoint='+urllib.parse.quote(endpoint),{'Authorization':'Bearer '+receiver},public)
  require(status==200,'genuine receiver unavailable')
  selected=observed['deliveries']
  if all(any(row['source_event_id']==event['second'] for row in selected) for event in fixture['events'].values()):break
  require(time.monotonic()<deadline,'ordered delivery deadline exceeded');time.sleep(1)
 status,registrations=request(origin+'/v1/webhooks/endpoints/'+endpoint+'/keys',headers,public);require(status==200 and registrations,'real published signing keys required')
 keys={row['key_id']:row['public_key'] for row in registrations}
 verifier=next(row['path'] for row in json.loads(private(OUT/'manifest.json'))['artifacts'] if row['name']=='verify_delivery' and 'bin' in row['kind'])
 signed_count=0
 for received in selected:
  require('body_base64' in received and 'headers' in received,'genuine receiver raw signed delivery bytes required')
  input=json.dumps({'body_base64':received['body_base64'],'headers':received['headers'],'keys':keys}).encode()
  result=subprocess.run([verifier],input=input,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=10)
  require(result.returncode==0,'real signature/replay/tamper verifier refused delivery');signed_count+=1
 require(signed_count>=8,'all four event-family signed deliveries required')
 for event in fixture['events'].values():
  first=[i for i,row in enumerate(selected) if row['source_event_id']==event['first']];second=[i for i,row in enumerate(selected) if row['source_event_id']==event['second']];require(first and second and min(first)<min(second),'per-subject order violated')
 status,deliveries=request(origin+'/v1/dashboard/webhook-deliveries?limit=200',headers,public);require(status==200,'delivery log unavailable')
 require(any(any(attempt.get('failure') in ('unreachable','timeout') for attempt in delivery['attempts']) and delivery['state']['state']=='delivered' for delivery in deliveries),'real dropped-delivery retry evidence required')
 mutation=dict(headers,**{'X-LayerX-CSRF':csrf,'Idempotency-Key':'104175-'+fixture['run_id']})
 url=origin+'/v1/webhooks/endpoints/'+endpoint+'/redeliveries?cursor='+urllib.parse.quote(cursor)+'&limit=200'
 status,replayed=request(url,mutation,public,'POST');require(status==202 and replayed['queued'],'real missed-event redelivery required')
 status,duplicate=request(url,mutation,public,'POST');require(status==202 and replayed['queued']==duplicate['queued'],'redelivery idempotency changed identities')
 status,payments=request(origin+'/v1/dashboard/test-payments?limit=200',headers,public);require(status==200 and payments,'real payments required')
 receipts=0
 for payment in payments:
  for fact in payment['facts']:
   require(fact['verification'] in ('unverified','receipt-verified','checkpoint-finalised','paxeer-finalised'),'unknown fact verification')
   require((fact['verification']=='unverified')==(fact['receipt_digest'] is None),'fact evidence mismatch')
   if fact['name']=='activity_id' and fact['verification']!='unverified' and payment['settled']:
    status,result=request(origin+'/v1/dashboard/receipts/'+fact['value'],headers,public);require(status==200 and result['settled'] is True and result['receipt_digest']==fact['receipt_digest'],'receipt owner mismatch');receipts+=1
 require(receipts>0,'real verified receipt lookup required')
 (OUT/'runtime-result.json').write_text(json.dumps({'families':4,'receipts':receipts,'ordered':True,'duplicate_refusal':True,'drop_retry':True,'redelivery':True,'revision':git('rev-parse','HEAD')})+'\n')
def verify():
 count=0
 manifest=json.loads(private(OUT/'manifest.json'));require(manifest['revision']==git('rev-parse','HEAD') and manifest['sources']==sources(),'published source provenance mismatch')
 for row in manifest['artifacts']:require(sha(row['path'])==row['sha256'] and sha(row['original'])==row['sha256'],'compiled artifact changed')
 for row in manifest['artifacts']:
  if row['name']=='plain_listener':
   output=run([row['path'],'--test-threads=1'],Path(row['path']).parent.name+'-listeners',180);match=re.search(r'test result: ok\. (\d+) passed; 0 failed; 0 ignored; 0 measured; 0 filtered out',output);require(match is not None,'complete retained listener corpus required');count+=int(match[1])
 run([NODE,'platform/hosted/dashboard/web/tests/ui-contract.mjs'],'dashboard-ui-contract',30);count+=1
 output=run([NODE,'--experimental-strip-types','platform/hosted/dashboard/web/tests/evidence-decoding.mjs'],'dashboard-evidence',30);match=re.search(r'dashboard evidence refusals: (\d+) passed',output);require(match is not None,'evidence refusal corpus required');count+=int(match[1])
 runtime();count+=1;print(f'PAXEER_X_GATE tests={count} skipped=0')
try:
 require(sys.argv[1:] in ([],['--build']),'unknown selector argument')
 build() if sys.argv[1:]==['--build'] else verify()
except (RuntimeError,OSError,ValueError,KeyError,subprocess.SubprocessError) as error:
 print('webhook/dashboard: '+str(error),file=sys.stderr);raise SystemExit(1)
PY
