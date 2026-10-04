#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import os
from pathlib import Path
import ssl
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('human_boundary', ROOT / 'tools/qualification/paxeer-x/human-api-boundary.py')
B = importlib.util.module_from_spec(spec)
spec.loader.exec_module(B)

CLIENT = r'''
import {readFile,writeFile} from 'node:fs/promises';
import {pathToFileURL} from 'node:url';
import {createHash,randomBytes} from 'node:crypto';
const [root,manifestPath,outputPath,phase,previousPath] = process.argv.slice(2);
const fixture=JSON.parse(await readFile(manifestPath,'utf8'));
const previous=phase==='recover'?JSON.parse(await readFile(previousPath,'utf8')):undefined;
const {createHumanApiClient,HumanApiError}=await import(pathToFileURL(root+'/human/apps/web/src/api/generated/index.ts').href);
const {conformance}=await import(pathToFileURL(root+'/human/apps/web/src/api/generated/conformance.ts').href);
let tests=0;const stable=x=>JSON.stringify(x,(_,v)=>typeof v==='bigint'?v.toString():v);
function check(v,m){if(!v)throw new Error(m);tests++;}
const jars=new Map(),clients=new Map();
for(const [name,session]of Object.entries(fixture.sessions)){
 const jar=new Map(Object.entries(previous?.cookies[name]??session.cookies));jars.set(name,jar);
 clients.set(name,createHumanApiClient({baseUrl:fixture.url,csrfToken:()=>jar.get('__Host-layerx_csrf'),trace:()=> 'trc_'+randomBytes(16).toString('hex'),fetch:async(url,init)=>{
  const headers=new Headers(init.headers);headers.set('Origin',fixture.origin);headers.set('Cookie',[...jar].map(([k,v])=>k+'='+v).join('; '));
  const response=await fetch(url,{...init,headers,redirect:'error',signal:init.signal??AbortSignal.timeout(20000)});
  for(const cookie of response.headers.getSetCookie()){const pair=cookie.split(';')[0],index=pair.indexOf('=');jar.set(pair.slice(0,index),pair.slice(index+1));}return response;
 }}));
}
const client=clients.get(fixture.principal),foreign=clients.get(fixture.other_principal);
check(client&&foreign&&fixture.principal!==fixture.other_principal,'actual distinct authenticated owners');
async function denied(run,codes){let error;try{await run();}catch(e){error=e;}check(error instanceof HumanApiError&&codes.includes(error.detail.code),'typed actual refusal');}
async function invoke(row){check(clients.has(row.principal),'case authenticated owner');return conformance[row.operation]({client:clients.get(row.principal),params:row.params,body:row.body,idempotencyKey:row.idempotency_key});}
async function evidence(ref){const bytes=await client.evidenceGet(ref.evidence_id),raw=Buffer.from(bytes.bytes_base64,'base64');check(raw.length>0&&'evd_'+createHash('sha256').update(raw).digest('hex')===ref.evidence_id,'actual exported immutable digest');check(bytes.verification===ref.verification&&bytes.class===ref.class,'honest achieved evidence level');await denied(()=>foreign.evidenceGet(ref.evidence_id),['forbidden','not-found']);}
const start=await client.streamOpen();
const all=[],cursors=new Set();let cursor='start';
while(cursor!==''){check(!cursors.has(cursor)&&cursors.size<128,'finite actual pagination without duplicate cursor');cursors.add(cursor);const page=await client.approvalProgramList(cursor);check(page.approvals.length<=100,'native page bound');all.push(...page.approvals);cursor=page.next_cursor;}
check(all.length>=4&&new Set(all.map(x=>x.approval_id)).size===all.length,'genuine four operation producer inventory');
const kinds=new Set(),semantics=new Set(),observed=new Map();
for(const item of all){
 check(/^apr_[a-f0-9]{64}$/.test(item.approval_id)&&/^agt_[a-f0-9]{64}$/.test(item.agent_id),'strict real owner/child IDs');
 check(Number.isFinite(Date.parse(item.created_at))&&Number.isFinite(Date.parse(item.expires_at)),'genuine wall clock timestamps');
 kinds.add(item.operation.kind);semantics.add(item.semantics);
 check(item.semantics==='authorized-limits'?item.authorized_limits.length>0:item.authorized_limits.length===0,'no invented operation-only amount');
 const detail=await client.approvalProgramGet(item.approval_id);check(detail.held_digest===item.held_digest&&stable(detail.operation)===stable(item.operation)&&detail.agent_id===item.agent_id,'shared list/detail real canonical operation');
 const material=await client.approvalProgramMaterial(item.approval_id);check(material.provenance==='local-owned'&&material.held_digest===item.held_digest,'actual local held material');
 for(const ref of [material.canonical_unsigned,material.immutable_carrier,material.budget_reservation]){check(ref.verification==='unverified','owned held bytes never promoted');await evidence(ref);}
 check(material.immutable_carrier.evidence_id==='evd_'+item.held_digest,'actual held digest bound');
 if(detail.budget){const budget=await client.approvalProgramBudget(item.approval_id);check(stable(budget)===stable(detail.budget)&&budget.within_bound&&budget.age_sequences<=budget.maximum_age_sequences,'same truthful bounded budget');check(['checkpoint-finalised','settlement-anchored'].includes(budget.verification),'independently verified proof');check(budget.evidence.some(r=>r.evidence_id==='evd_'+budget.proof_digest),'actual raw budget proof reference');for(const ref of budget.evidence)await evidence(ref);}
 await denied(()=>foreign.approvalProgramGet(item.approval_id),['not-found','forbidden']);
 await denied(()=>foreign.approvalProgramMaterial(item.approval_id),['not-found','forbidden']);
 observed.set(item.approval_id,detail);
}
check(['deploy','upgrade','call','wind-down'].every(k=>kinds.has(k)),'all genuine native Programs payloads');
check(semantics.has('operation-only')&&semantics.has('authorized-limits'),'both genuine semantics profiles');
check(Array.isArray(fixture.observations)&&fixture.observations.length>=4,'actual source-backed producer observations');
for(const row of fixture.observations){const value=observed.get(row.approval_id);check(value&&value.agent_id===row.agent_id&&value.held_digest===row.held_digest&&value.operation.kind===row.kind,'authenticated producer lineage');check(Date.parse(value.created_at)===row.created_at_unix_seconds*1000&&Date.parse(value.expires_at)===row.activity_expires_at_unix_milliseconds,'exact genuine clock units');if(row.budget){check(value.budget&&value.budget.remaining===BigInt(row.budget.remaining)&&value.budget.source_account===row.budget.source_account&&value.budget.asset_id===row.budget.asset_id&&value.budget.proof_digest===row.budget.proof_digest,'real post-reservation accounting and proof');}}
const home=await client.homeSummary();check(stable(home.program_approvals)===stable(all.slice(0,100)),'home shares canonical first Programs page');
if(!previous){
 check(Array.isArray(fixture.disclosures)&&fixture.disclosures.length>=2,'actual prospective decision disclosures');
 for(const row of fixture.disclosures){const disclosed=await client.approvalProgramDisclosure(row.approval_id,{decision:row.decision,held_digest:row.held_digest,idempotency_key:row.idempotency_key});check(disclosed.held_digest===row.held_digest&&disclosed.decision===row.decision&&disclosed.confirms===row.actual_authentication_digest,'server disclosure binds genuine independently observed authentication digest');await denied(()=>foreign.approvalProgramDisclosure(row.approval_id,{decision:row.decision,held_digest:row.held_digest,idempotency_key:row.idempotency_key}),['forbidden','not-found']);}
}
const decisions=[];
check(Array.isArray(fixture.decisions)&&fixture.decisions.length>=2,'real approve/reject ceremony cases');
const decisionOps=new Set();
for(const row of fixture.decisions){check(['approval.program.approve','approval.program.reject'].includes(row.operation),'real schema decision');decisionOps.add(row.operation);const result=await invoke(row);check(result.money_moved===false&&result.held_digest===row.body.held_digest&&result.state===row.state,'permission never execution');check(stable(await invoke(row))===stable(result),'durable exact idempotent decision');if(previous)check(stable(previous.decisions.find(x=>x.id===row.id)?.result)===stable(result),'restart retains immutable decision');decisions.push({id:row.id,result});}
check(decisionOps.size===2,'real both decisions exercised');
if(!previous){
 const required=new Set(['wrong-digest','cross-owner','missing-step-up','expired','opposite-decision','stale-proof','altered-proof']);
 check(Array.isArray(fixture.refusals)&&fixture.refusals.length<=64,'bounded authentic refusal inputs');
 for(const row of fixture.refusals){check(required.delete(row.case),'unique required authentic negative case');await denied(()=>invoke(row),[row.code]);}
 check(required.size===0,'all required real refusal authorities');
}
const stream=client.streamSubscribe(previous?.cursor??start.cursor,{signal:AbortSignal.timeout(20000)});let last=previous?.cursor??start.cursor,delivered=0;
for await(const event of stream){check(!event.program_approval||event.kind.startsWith('program-approval-'),'typed genuine Programs journal');if(event.program_approval){check(observed.has(event.program_approval.approval_id),'owner-bound durable Programs push');delivered++;last=event.cursor;if(delivered>=fixture.minimum_stream_events)break;}}
check(delivered>=fixture.minimum_stream_events&&fixture.minimum_stream_events>=1,'real durable push without polling');
await writeFile(outputPath,stable({tests,decisions,cursor:phase==='recover'?last:start.cursor,cookies:Object.fromEntries([...jars].map(([k,v])=>[k,Object.fromEntries(v)]))}),{mode:0o600});
'''


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--candidate-manifest', required=True)
    args = parser.parse_args()
    sys.path.insert(0, str(ROOT / 'tools/paxeer-x'))
    from candidate import catalogue, load_private, validate
    candidate = load_private(args.candidate_manifest)
    validate(candidate, catalogue(ROOT / 'spec/paxeer-x/spec.kvx'))
    fixture = os.environ.get('PAXEER_X_HUMAN_PROGRAM_APPROVALS_MANIFEST')
    B.require(fixture, 'genuine protected Programs process authority fixture required')
    material = B.load(fixture)
    B.require(material['schema'] == 'layerx-human-program-approvals.v1', 'closed process fixture profile')
    revision = B.git('rev-parse', 'HEAD').decode().strip()
    B.require(candidate['source']['revision'] == revision and not candidate['source']['dirty'], 'genuine published candidate')
    required = {
        'human/crates/layerx-human-service/src/server/' + name + '.rs'
        for name in ('agent_runtime', 'production_components', 'schema', 'mod', 'projection', 'stream_journal')
    } | {
        'human/schema/human-api/' + name + '.kvx'
        for name in ('program-approvals', 'v1', 'home', 'stream')
    } | {
        'human/apps/web/src/api/generated/' + name + '.ts' for name in ('index', 'conformance')
    } | {'tools/paxeer-x/route-catalogue.json', 'tools/qualification/paxeer-x/human_program_approvals.py', 'human/apps/web/copy/catalog.ts', 'human/apps/web/copy/messages.generated.ts'}
    required |= {'human/schema/human-api/golden/approval.program.' + operation + '.' + variant + '.json'
                 for operation in ('list', 'get', 'material', 'budget', 'approve', 'reject', 'disclosure')
                 for variant in ('request', 'response', 'failure')}
    inventory = material['source_files']
    B.require(isinstance(inventory, dict) and required <= inventory.keys() and len(inventory) <= 20000, 'complete frozen task and real transitive source inventory')
    digest = hashlib.sha256()
    for name, expected in sorted(inventory.items()):
        path = ROOT / name
        B.require(not Path(name).is_absolute() and '..' not in Path(name).parts and path.is_file() and not path.is_symlink(), 'source inventory path')
        B.require(not name.startswith('human/apps/web/') or name in required, 'exclude concurrent UI implementation from frozen backend source')
        actual = B.digest(path)
        B.require(actual == expected, 'actual frozen source hash')
        digest.update(name.encode() + b'\0' + actual.encode() + b'\n')
    source = digest.hexdigest()
    directory = B.private(material['evidence_directory'], True)
    B.require(not any(directory.iterdir()), 'fresh private evidence directory')
    roles = ('service', 'components', 'agent', 'core', 'paxeer')
    artifacts = {name: B.executable(row, revision, source) for name, row in material['artifacts'].items()}
    B.require(all(role in artifacts for role in roles), 'actual source-bound production artifacts')
    node = Path(material['node'])
    B.require(node.is_absolute() and node.is_file() and os.access(node, os.X_OK) and B.digest(node) == material['node_sha256'], 'pinned genuine Node runtime')
    ca = B.private(material['ca_pem'])
    context = ssl.create_default_context(cafile=str(ca))
    url = B.endpoint(material['url'])
    B.require(5 <= len(material['processes']) <= 32, 'bounded genuine production processes')
    by_role = {row['artifact']: row for row in material['processes']}
    B.require(all(role in by_role for role in roles), 'real process roles')
    service = by_role['service']['environment']
    B.require(service['LAYERX_HUMAN_LISTENER'] == 'tls' and service['LAYERX_HUMAN_BIND'] == '127.0.0.1:' + str(url.port) and service['LAYERX_HUMAN_WEB_ORIGIN'] == material['origin'], 'real isolated authenticated TLS boundary')
    B.private(service['LAYERX_HUMAN_TLS_CERT_DER'])
    B.private(service['LAYERX_HUMAN_TLS_KEY_DER'])
    processes = B.Processes(directory, artifacts)
    def ready():
        until = time.monotonic() + min(90, B.remaining())
        while True:
            B.require(all(child.poll() is None for child, _, _ in processes.children.values()), 'real processes remain alive')
            try:
                status, response = B.request(url, context, 'GET', '/readyz')
                if status == 200 and response.get('result', {}).get('ready') is True:
                    return
            except (OSError, B.http.client.HTTPException):
                pass
            B.require(time.monotonic() < until, 'real readiness bound')
            time.sleep(0.1)
    try:
        for row in material['processes']:
            processes.start(row)
        ready()
        runner = directory / 'program-approvals.mts'
        runner.write_text(CLIENT)
        runner.chmod(0o600)
        previous = directory / 'initial.json'
        for phase in ('initial', 'recover'):
            if phase == 'recover':
                row = processes.stop(by_role['service']['name'])
                row = dict(row, name=row['name'] + '-restart')
                processes.start(row)
                ready()
            output = directory / (phase + '.json')
            with (directory / (phase + '.log')).open('xb') as log:
                os.chmod(log.name, 0o600)
                result = subprocess.run([str(node), '--experimental-strip-types', str(runner), str(ROOT), fixture, str(output), phase, str(previous)], cwd=ROOT,
                    env=dict(os.environ, NODE_EXTRA_CA_CERTS=str(ca), NODE_TLS_REJECT_UNAUTHORIZED='1', NODE_OPTIONS=''), stdout=log, stderr=log, timeout=min(900, B.remaining()))
            B.require(result.returncode == 0, 'genuine generated Programs consumer ' + phase)
            observed = B.load(output)
            B.require(type(observed['tests']) is int and observed['tests'] >= 50, 'actual positive/refusal/recovery cases without skips')
        print('human-program-approvals: PASS revision=' + revision)
    finally:
        processes.close()


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('human-program-approvals: FAIL: ' + str(error), file=sys.stderr)
        raise SystemExit(1)
