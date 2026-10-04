#!/usr/bin/env python3
import importlib.util
import json
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
import {randomBytes,createHash} from 'node:crypto';
const [root,manifest,output,phase,previous] = process.argv.slice(2);
const fixture = JSON.parse(await readFile(manifest,'utf8'));
const {createHumanApiClient,HumanApiError} = await import(pathToFileURL(root+'/human/apps/web/src/api/generated/index.ts').href);
const {conformance} = await import(pathToFileURL(root+'/human/apps/web/src/api/generated/conformance.ts').href);
const stable = value => JSON.stringify(value,(_,item)=>typeof item==='bigint'?item.toString():item);
let tests=0;
function check(value,message){if(!value)throw new Error(message);tests++;}
const outputs = new Map(previous==='-'?[]:JSON.parse(await readFile(previous,'utf8')).outputs);
const jars = new Map(Object.entries(fixture.sessions).map(([name,row])=>[name,new Map(Object.entries(row.cookies))]));
const clients = new Map();
for(const [name,jar] of jars){clients.set(name,createHumanApiClient({baseUrl:fixture.url,
csrfToken:()=>jar.get('__Host-layerx_csrf'),trace:()=> 'trc_'+randomBytes(16).toString('hex'),
fetch:async(url,init)=>{const headers=new Headers(init.headers);headers.set('Origin',fixture.origin);
headers.set('Authorization','Bearer '+fixture.sessions[name].assertion);
headers.set('Cookie',[...jar].map(([k,v])=>k+'='+v).join('; '));
const response=await fetch(url,{...init,headers,redirect:'error'});
for(const cookie of response.headers.getSetCookie()){const pair=cookie.split(';')[0],at=pair.indexOf('=');jar.set(pair.slice(0,at),pair.slice(at+1));}
return response;}}));}
function resolve(value){if(Array.isArray(value))return value.map(resolve);if(value&&typeof value==='object'){
if(Object.keys(value).length===1&&typeof value.$result==='string'){const [id,...parts]=value.$result.split('/');let item=outputs.get(id);for(const part of parts)item=item?.[part];check(item!==undefined,'actual predecessor result');return item;}
return Object.fromEntries(Object.entries(value).map(([key,item])=>[key,resolve(item)]));}return value;}
const cases=fixture.cases[phase];check(Array.isArray(cases)&&cases.length>0&&cases.length<=32,'bounded real cases');
for(const row of cases){check(typeof row.id==='string'&&!outputs.has(row.id)&&clients.has(row.principal)&&conformance[row.operation],'real schema-owned authenticated operation');
const input={client:clients.get(row.principal),params:resolve(row.params??{}),...(row.body===undefined?{}:{body:resolve(row.body)}),...(row.idempotency_key===undefined?{}:{idempotencyKey:row.idempotency_key})};
if(row.error_codes){let error;try{await conformance[row.operation](input);}catch(caught){error=caught;}
check(error instanceof HumanApiError&&row.error_codes.includes(error.detail.code),'real owner/provider/disclosure typed refusal');outputs.set(row.id,{refused:true});continue;}
const result=await conformance[row.operation](input);outputs.set(row.id,result);
if(row.operation==='intent.plan'){
check(/^[a-f0-9]{64}$/.test(result.plan_digest)&&result.legs.length>0,'actual deterministic sealed plan');
check(result.total_fee.amount===BigInt(row.expected_fee)&&result.total_fee.currency===row.currency,'exact real fee disclosure');
check(result.legs[0].money.amount===BigInt(row.expected_amount)&&result.legs[0].money.currency===row.currency,'exact actual amount disclosure');
if(row.coverage==='native-send')check(result.legs.length===1&&result.legs[0].domain==='layerx'&&result.legs[0].mechanism==='send','native SEND with no custody movement leg');
if(row.coverage==='wallet')check(result.legs.some(leg=>leg.domain==='paxeer'),'real bound wallet movement path');
}
if(row.replay_of)check(stable(result)===stable(outputs.get(row.replay_of)),'durable exact idempotent outcome after restart');
if(row.operation==='journey.get'){
check(result.state==='done','actual canonical verified final journey required');
const refs=[...result.evidence,...result.stages.flatMap(stage=>stage.evidence)];
check(refs.some(ref=>['receipt-verified','checkpoint-finalised','settlement-anchored'].includes(ref.verification)),'real verified protocol outcome');
for(const ref of refs.filter(ref=>['receipt-verified','checkpoint-finalised','settlement-anchored'].includes(ref.verification))){
const material=await input.client.evidenceGet(ref.evidence_id);const bytes=Buffer.from(material.bytes_base64,'base64');
check(bytes.length>0&&material.verification===ref.verification,'actual tenant-owned verified receipt export');
if(/^evd_[a-f0-9]{64}$/.test(ref.evidence_id))check('evd_'+createHash('sha256').update(bytes).digest('hex')===ref.evidence_id,'exact actual outcome evidence digest');
}}
}
await writeFile(output,stable({tests,outputs:[...outputs]}),{mode:0o600});
'''


def main():
    B.require(len(sys.argv) == 1, 'no arguments accepted')
    candidate_path = os.environ.get('PAXEER_X_CANDIDATE_MANIFEST')
    fixture_path = os.environ.get('PAXEER_X_HUMAN_ATTESTOR_INTENTS_MANIFEST')
    B.require(candidate_path and fixture_path, 'genuine protected candidate and attestor signing fixture required')
    sys.path.insert(0, str(ROOT / 'tools/paxeer-x'))
    from candidate import catalogue, load_private, validate
    candidate = load_private(candidate_path)
    validate(candidate, catalogue(ROOT / 'spec/paxeer-x/spec.kvx'))
    revision, source = B.identity()
    B.require(candidate['source']['revision'] == revision and not candidate['source']['dirty'], 'genuine clean candidate source')
    fixture = B.load(fixture_path)
    B.require(fixture['schema'] == 'layerx-human-attestor-intents.v1', 'closed real-process fixture profile')
    directory = B.private(fixture['evidence_directory'], True)
    B.require(not any(directory.iterdir()), 'fresh private evidence directory')
    state_root = B.private(fixture['disposable_state_directory'], True)
    B.require(state_root != directory and not any(state_root.iterdir()), 'fresh disposable qualification authority state')
    artifacts = {name: B.executable(row, revision, source) for name, row in fixture['artifacts'].items()}
    roles = ('service', 'components', 'agent', 'core', 'paxeer', 'gateway', 'attestor')
    B.require(all(role in artifacts for role in roles), 'actual gateway Human core wallet attestor production artifacts')
    node = Path(fixture['node'])
    B.require(node.is_absolute() and node.is_file() and os.access(node, os.X_OK) and B.digest(node) == fixture['node_sha256'], 'pinned genuine Node runtime')
    ca = B.private(fixture['ca_pem'])
    context = ssl.create_default_context(cafile=str(ca))
    url = B.endpoint(fixture['url'])
    B.require(fixture['principal'] != fixture['other_principal'], 'distinct authentic owner sessions')
    B.require(11 <= len(fixture['processes']) <= 32, 'bounded actual process inventory')
    by_role = {row['artifact']: row for row in fixture['processes']}
    B.require(all(role in by_role for role in roles), 'all actual process roles required')
    B.require(sum(row['artifact'] == 'attestor' for row in fixture['processes']) == 5, 'actual five-node signing quorum')
    components = by_role['components']['environment']
    B.require(len(json.loads(components['LAYERX_HUMAN_ATTESTOR_NODES'])) == 5 and components['LAYERX_HUMAN_PROTOCOL_VERSION'] == '3', 'actual production AttestorKms selected')
    for row in fixture['processes']:
        B.require(row['state_directory'].startswith(str(state_root) + '/'), 'qualification process state isolated from funded live services')
        for endpoint in row['qualification_endpoints']:
            B.endpoint(endpoint)
    required = {'native-send', 'wallet', 'wrong-principal', 'changed-disclosure', 'missing-wallet', 'provider-refusal', 'restart-replay', 'verified-native-outcome'}
    coverage = {row['coverage'] for phase in ('live', 'restart') for row in fixture['cases'][phase]}
    B.require(required <= coverage, 'all actual positive refusal restart and verified outcome cases required without skips')
    for phase in ('live', 'restart'):
        for row in fixture['cases'][phase]:
            if row['coverage'] in ('native-send', 'wallet'):
                B.require(row['operation'] == 'intent.plan' and not row.get('error_codes'), 'real positive planning coverage')
            elif row['coverage'] in ('wrong-principal', 'changed-disclosure', 'missing-wallet', 'provider-refusal'):
                B.require(row['operation'] in ('intent.plan', 'intent.submit') and isinstance(row.get('error_codes'), list) and row['error_codes'], 'actual refusal case required')
            elif row['coverage'] == 'restart-replay':
                B.require(phase == 'restart' and row['operation'] == 'intent.submit' and row.get('replay_of'), 'actual same submission restart replay')
            elif row['coverage'] == 'verified-native-outcome':
                B.require(row['operation'] == 'journey.get' and not row.get('error_codes'), 'actual canonical verified native outcome')

    processes = B.Processes(directory, artifacts)
    def ready():
        until = time.monotonic() + min(90, B.remaining())
        while time.monotonic() < until:
            B.require(all(child.poll() is None for child, _, _ in processes.children.values()), 'actual production process exited')
            try:
                status, result = B.request(url, context, 'GET', '/readyz')
                if status == 200 and result.get('result', {}).get('ready') is True:
                    return
            except (OSError, B.http.client.HTTPException):
                pass
            time.sleep(0.1)
        raise RuntimeError('actual unified endpoint readiness deadline')
    try:
        for row in fixture['processes']:
            processes.start(row)
        ready()
        runner = directory / 'attestor-intents.mts'
        runner.write_text(CLIENT)
        runner.chmod(0o600)
        previous = '-'
        for phase in ('live', 'restart'):
            if phase == 'restart':
                stopped = [processes.stop(by_role[name]['name']) for name in ('service', 'components')]
                for row in reversed(stopped):
                    (directory / (row['name'] + '.log')).rename(directory / (row['name'] + '-before-restart.log'))
                    processes.start(row)
                ready()
            output = directory / (phase + '-result.json')
            with (directory / (phase + '.log')).open('xb') as log:
                os.chmod(log.name, 0o600)
                result = subprocess.run([str(node), '--experimental-strip-types', str(runner), str(ROOT), fixture_path, str(output), phase, previous], cwd=ROOT, env=dict(os.environ, NODE_EXTRA_CA_CERTS=str(ca), NODE_TLS_REJECT_UNAUTHORIZED='1', NODE_OPTIONS=''), stdout=log, stderr=log, timeout=min(600, B.remaining()))
            B.require(result.returncode == 0, 'actual gateway-to-attestor production case failed')
            result = B.load(output)
            B.require(type(result['tests']) is int and result['tests'] >= 4, 'real assertions required without skips')
            B.COUNT += result['tests']
            previous = str(output)
        print('human-attestor-intents: PASS tests=' + str(B.COUNT) + ' revision=' + revision)
    finally:
        processes.close()


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('human-attestor-intents: FAIL: ' + str(error), file=sys.stderr)
        raise SystemExit(1)
