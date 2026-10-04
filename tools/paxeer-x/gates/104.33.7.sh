#!/usr/bin/env bash
# paxeer-x-services: private-profile
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 - <<'GATE'
import hashlib,json,os,pathlib,re,ssl,stat,subprocess,sys,urllib.error,urllib.parse,urllib.request
root=pathlib.Path.cwd();tests=0
sources=['platform/hosted/gateway/Cargo.toml','platform/hosted/gateway/src/main.rs','platform/hosted/gateway/src/native_call.rs','platform/hosted/gateway/src/lib.rs','human/apps/web/src/explorer/client.ts','human/apps/web/src/explorer/model.ts','human/apps/web/src/explorer/program-contract.test.ts','human/crates/layerx-explorer-index/src/programs.rs','tools/paxeer-x/gates/104.33.7.sh']
def require(value,reason):
    if not value:raise RuntimeError(reason)
def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def private(path):
    p=pathlib.Path(path);info=p.lstat()
    require(stat.S_ISREG(info.st_mode) and not p.is_symlink() and info.st_mode&0o077==0,'private regular artifact required')
    require(not any(part=='.env' or part.startswith('.env.') for part in p.parts),'environment files forbidden')
    return p
try:
    raw=os.environ.get('PAXEER_X_HOSTED_PROGRAM_ARTIFACTS')
    require(raw,'actual current prebuilt gateway/explorer artifact manifest required')
    manifest=json.loads(private(raw).read_text());revision=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
    require(manifest['schema']=='layerx.hosted-program-operations-artifacts.v1' and manifest['revision']==revision and manifest['build_exit']==0,'current successful target build required')
    require(set(manifest['source_files'])==set(sources),'complete source closure required')
    for path in sources:require(sha(root/path)==manifest['source_files'][path],'compiled source mismatch: '+path)
    artifacts=manifest['artifacts'];require(set(artifacts)=={'gateway_lib','gateway_bin','explorer_lib'},'complete actual target artifact set required')
    for name,filters in [('gateway_lib',['tests::production_program_routes_are_exact_and_bounded']),('gateway_bin',['programs_wire_tests::','native_call::tests::']),('explorer_lib',['programs::program_call_tests::'])]:
        row=artifacts[name];binary=private(row['path']);require(os.access(binary,os.X_OK) and sha(binary)==row['sha256'],'sealed test artifact required')
        for selector in filters:
            inventory=subprocess.check_output([str(binary),selector,'--list'],text=True,timeout=30)
            names=set(re.findall(r'^(.+): test$',inventory,re.M));require(names,'scoped real test inventory empty')
            run=subprocess.run([str(binary),selector,'--nocapture','--test-threads=1'],capture_output=True,text=True,timeout=600)
            sys.stdout.write(run.stdout);sys.stderr.write(run.stderr);require(run.returncode==0,'actual '+name+' tests failed')
            summary=re.findall(r'test result: ok\. (\d+) passed; (\d+) failed; (\d+) ignored;',run.stdout)
            require(len(summary)==1 and tuple(map(int,summary[0]))==(len(names),0,0),'scoped case omitted or ignored')
            tests+=len(names)
    run=subprocess.run(['node','--test','human/apps/web/src/explorer/program-contract.test.ts'],capture_output=True,text=True,timeout=120)
    sys.stdout.write(run.stdout);sys.stderr.write(run.stderr);require(run.returncode==0,'actual explorer consumer tests failed')
    count=re.findall(r'^# tests (\d+)$',run.stdout,re.M);require(len(count)==1 and int(count[0])==3 and '# fail 0' in run.stdout and '# skipped 0' in run.stdout,'complete explorer consumer corpus required');tests+=3
    # Reuse the genuine producer result without rerunning its historical gate.
    supplied=os.environ.get('PAXEER_X_PROGRAM_BINDINGS_RESULT')
    require(supplied,'existing genuine seven-language binding producer result required')
    bound=json.loads(private(supplied).read_text());require(bound['schema']=='paxeer-x.program-bindings-result.v1' and bound['tests']>0 and bound['skipped']==0 and bound['source']['revision']==revision,'actual current binding result required')
    plan=json.loads(private(bound['manifest']['path']).read_text());require(sha(pathlib.Path(bound['manifest']['path']))==bound['manifest']['sha256'],'binding result provenance changed')
    require({'rust','typescript','python','go','java','swift','csharp'}<=set(plan['languages']),'all seven SDK languages required')
    for row in plan['sdk_outputs']+plan['consumer_sources']:
        require(sha(pathlib.Path(row['path']))==row['sha256'],'binding source/artifact changed')
    required=['LAYERX_PROGRAM_GATEWAY_URL','LAYERX_GATEWAY_CA_FILE','LAYERX_GATEWAY_KEY_ID','LAYERX_GATEWAY_KEY_SECRET','LAYERX_PROGRAM_CALL_ACTIVITY_FILE','LAYERX_PROGRAM_ID','LAYERX_PROGRAM_IDEMPOTENCY_KEY','LAYERX_RECEIPT_VERIFY_BIN']
    require(all(os.environ.get(name) for name in required),'genuine disposable hosted Programs fixture required')
    origin=urllib.parse.urlsplit(os.environ['LAYERX_PROGRAM_GATEWAY_URL']);require(origin.scheme=='https' and origin.hostname in {'localhost','127.0.0.1','::1'} and origin.path in {'','/'} and not origin.username and not origin.password and not origin.query and not origin.fragment,'disposable loopback HTTPS fixture required')
    program=os.environ['LAYERX_PROGRAM_ID'];key=os.environ['LAYERX_PROGRAM_IDEMPOTENCY_KEY'];require(re.fullmatch('[0-9a-f]{64}',program) and re.fullmatch('[0-9a-f]{64}',key),'canonical program/idempotency required')
    credentials=os.environ['LAYERX_GATEWAY_KEY_ID']+':'+os.environ['LAYERX_GATEWAY_KEY_SECRET'];context=ssl.create_default_context(cafile=os.environ['LAYERX_GATEWAY_CA_FILE'])
    def request(method,path,body,content='application/json',idempotency=None):
        headers={'Authorization':'LayerX-Key '+credentials,'Content-Type':content}
        if idempotency is not None:headers['Idempotency-Key']=idempotency
        req=urllib.request.Request(urllib.parse.urlunsplit(origin).rstrip('/')+path,data=body,headers=headers,method=method)
        try:
            with urllib.request.urlopen(req,context=context,timeout=30) as response:
                data=response.read(2097153);require(len(data)<=2097152,'response bound');return response.status,json.loads(data)
        except urllib.error.HTTPError as error:return error.code,json.loads(error.read(65537))
    selector=json.dumps({'program_id':program,'requested_verification_level':'sequencer-signed'}).encode();route='/v1/programs/registry/'+program
    status,discovery=request('GET',route,selector);require(status==200 and discovery['value']['program_id']==program,'actual discovery failed');tests+=1
    status,interface=request('GET',route+'/interface',selector);require(status==200 and interface['value']['program_id']==program and interface['value']['code_hash']==discovery['value']['code_hash'],'actual code-bound interface failed');require(hashlib.sha256(bytes.fromhex(interface['value']['interface'])).hexdigest()==interface['value']['interface_digest'],'interface digest mismatch');tests+=1
    activity=private(os.environ['LAYERX_PROGRAM_CALL_ACTIVITY_FILE']).read_bytes();require(0<len(activity)<=1048576,'signed activity bound')
    status,simulation=request('POST','/v1/programs/simulate',activity,'application/octet-stream');require(status==200 and simulation['value']['committed'] is False,'actual noncommitting simulation failed');tests+=1
    status,missing=request('POST','/v1/programs/call',activity,'application/octet-stream');require(status==400,'missing idempotency must refuse');tests+=1
    status,called=request('POST','/v1/programs/call',activity,'application/octet-stream',key);require(status in {200,202},'actual call failed')
    value=called.get('value',called.get('result',called));require(value.get('state') in {'executed','unknown','pending'},'unknown outcome semantics missing')
    if value['state']!='executed':raise RuntimeError('genuine call outcome pending; no fabricated receipt qualification')
    receipt=bytes.fromhex(value['receipt']);require(receipt,'genuine verified call receipt missing')
    evidence=pathlib.Path(os.environ['PAXEER_X_EVIDENCE_DIR']);receipt_path=evidence/'104.33.7-call-receipt.bin';receipt_path.write_bytes(receipt);receipt_path.chmod(0o600)
    checked=subprocess.run([os.environ['LAYERX_RECEIPT_VERIFY_BIN'],str(receipt_path)],capture_output=True,timeout=60);require(checked.returncode==0,'independent real receipt verification failed');tests+=1
    replay_status,replay=request('POST','/v1/programs/call',activity,'application/octet-stream',key);replayed=replay.get('value',replay.get('result',replay));require(replay_status==200 and replayed.get('receipt')==value['receipt'],'exact idempotent receipt replay failed');tests+=1
    altered=bytearray(activity);altered[-1]^=1
    status,_=request('POST','/v1/programs/call',bytes(altered),'application/octet-stream',key);require(status in {400,403,409},'tampered signed activity must refuse');tests+=1
    for path in sources:require(sha(root/path)==manifest['source_files'][path],'source changed during gate')
    print('PAXEER_X_GATE tests='+str(tests)+' skipped=0')
except (OSError,ValueError,KeyError,RuntimeError,subprocess.SubprocessError) as error:
    print('hosted Programs qualification refused: '+str(error),file=sys.stderr);sys.exit(78)
GATE
