#!/usr/bin/env python3
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import ssl
import subprocess
import sys
import time
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location(
    'human_boundary', ROOT / 'tools/qualification/paxeer-x/human-api-boundary.py')
B = importlib.util.module_from_spec(spec)
spec.loader.exec_module(B)
if os.environ.get('PAXEER_X_TASK_DEADLINE_UNIX'):
    B.DEADLINE = min(B.DEADLINE, time.monotonic()
                     + float(os.environ['PAXEER_X_TASK_DEADLINE_UNIX']) - time.time())
GATE_REPORT = None
GATE_DIRECTORY = None

CLIENT = r'''
import {readFile,writeFile} from 'node:fs/promises';
import {pathToFileURL} from 'node:url';
import {randomBytes} from 'node:crypto';
const [root,manifest,output,phase,prior,marker,release] = process.argv.slice(2);
const fixture = JSON.parse(await readFile(manifest,'utf8'));
const {createHumanApiClient,HumanApiError,decodeJourney,decodeDepositStartRequest,
  decodeDepositConfirmRequest} = await import(pathToFileURL(root+'/human/apps/web/src/api/generated/index.ts').href);
let tests=0;
const cases=[];
function check(value,message){if(!value)throw new Error(message);tests++;}
const stable=value=>JSON.stringify(value,(_,item)=>typeof item==='bigint'?item.toString():item);
const previous=prior==='-'?{}:JSON.parse(await readFile(prior,'utf8'));
const jars=new Map(Object.entries(fixture.sessions).map(([name,row])=>
  [name,new Map(Object.entries(previous.cookies?.[name]??row.cookies))]));
const clients=new Map();
class LostAcknowledgement extends Error {}
let held=false;
function projection(value){
  check(value!==null&&typeof value==='object'&&!Array.isArray(value),'real journey object');
  const allowed=new Set(['journey_id','kind','state','state_copy_key','stages','evidence',
    'started_at','updated_at','refusal','wallet_request']);
  check(Object.keys(value).every(key=>allowed.has(key)),'closed journey response schema');
  const journey=decodeJourney(value,'actual deposit response');
  check(/^jrn_[a-z0-9]+$/.test(journey.journey_id)&&journey.kind==='deposit','original outer deposit ID');
  check(journey.state==='waiting-for-you'&&journey.state_copy_key==='status.waiting-for-you',
    'initial waiting-wallet state');
  check(journey.stages.length===1&&journey.stages[0].state==='waiting-for-you'
    &&journey.stages[0].copy_key==='deposit.stage.wallet'&&journey.stages[0].stage_id.length>0,
    'authoritative initial continuation stage');
  check(Object.keys(value.stages[0]).every(key=>
    ['stage_id','copy_key','state','evidence'].includes(key)),'closed initial stage schema');
  for(const key of ['started_at','updated_at']){
    check(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(journey[key])
      &&Number.isFinite(Date.parse(journey[key])),'RFC3339 authoritative timestamp');
  }
  check(Date.parse(journey.started_at)===Date.parse(journey.updated_at),'coherent initial persisted timestamps');
  check(!journey.refusal&&journey.evidence.length===0&&journey.stages[0].evidence.length===0,
    'no fabricated completed custody receipt');
  return journey;
}
for(const [name,jar] of jars){
  clients.set(name,createHumanApiClient({baseUrl:fixture.url,
    csrfToken:()=>jar.get('__Host-layerx_csrf'),trace:()=> 'trc_'+randomBytes(16).toString('hex'),
    fetch:async(url,init)=>{
      const headers=new Headers(init.headers);
      headers.set('Origin',fixture.origin);
      headers.set('Authorization','Bearer '+fixture.sessions[name].assertion);
      headers.set('Cookie',[...jar].map(([key,value])=>key+'='+value).join('; '));
      const response=await fetch(url,{...init,headers,redirect:'error',signal:AbortSignal.timeout(20000)});
      const envelope=await response.clone().json();
      check(response.headers.get('Cache-Control')==='no-store','private transport response');
      check(envelope.trace===headers.get('X-LayerX-Trace')
        &&response.headers.get('X-LayerX-Trace')===envelope.trace,'real generated-client transport trace');
      for(const cookie of response.headers.getSetCookie()){
        const parts=cookie.split(';').map(part=>part.trim()),pair=parts[0],at=pair.indexOf('=');
        const key=pair.slice(0,at);
        check(key.startsWith('__Host-layerx_')&&parts.includes('Secure')&&parts.includes('Path=/')
          &&parts.includes('SameSite=Strict')&&(key==='__Host-layerx_csrf'||parts.includes('HttpOnly')),
          'real session cookie security');
        jar.set(key,pair.slice(at+1));
      }
      if(phase==='crash'&&name===fixture.principal&&new URL(url).pathname==='/v1/deposits'
        &&init.method==='POST'){
        check(!held&&response.status===200&&envelope.ok===true,'genuine durable creation before lost acknowledgement');
        held=true;
        const original=projection(envelope.result);
        await writeFile(marker,stable({original,tests,cookies:Object.fromEntries(
          [...jars].map(([owner,cookies])=>[owner,Object.fromEntries(cookies)]))}),{mode:0o600,flag:'wx'});
        for(let attempt=0;attempt<900;attempt++){
          try{const control=JSON.parse(await readFile(release,'utf8'));
            check(control.discard===true,'actual process crash discards original response');
            throw new LostAcknowledgement();
          }catch(error){if(error instanceof LostAcknowledgement)throw error;
            if(error.code!=='ENOENT')throw error;}
          await new Promise(resolve=>setTimeout(resolve,100));
        }
        throw new Error('actual crash supervisor deadline');
      }
      return response;
    }}));
}
const owner=clients.get(fixture.principal),foreign=clients.get(fixture.other_principal);
check(owner&&foreign&&fixture.principal!==fixture.other_principal,'distinct authentic principals');
async function refusal(action,code,status,label){
  let error;try{await action();}catch(caught){error=caught;}
  check(error instanceof HumanApiError&&(Array.isArray(code)?code.includes(error.detail.code):error.detail.code===code)
    &&status.includes(error.status)&&['structural','final'].includes(error.detail.retry),label);
  cases.push({case:label,result:'PASS',code:error.detail.code,status:error.status});
}
const body=decodeDepositStartRequest(fixture.start.body,'genuine qualification deposit request');
if(phase==='live'){
  const first=projection(await owner.depositStart(body,fixture.start.idempotency_key));
  const duplicate=projection(await owner.depositStart(body,fixture.start.idempotency_key));
  check(stable(first)===stable(duplicate),'same body and key retain exact initial semantics');
  cases.push({case:'first-response',result:'PASS'},{case:'duplicate',result:'PASS'});
  const conflict=decodeDepositStartRequest(fixture.conflict_body,'changed actual request');
  check(stable(conflict)!==stable(body),'conflicting request content required');
  await refusal(()=>owner.depositStart(conflict,fixture.start.idempotency_key),['forbidden','conflict'],[403,409],
    'changed-content-refused');
  await refusal(()=>foreign.journeyGet(first.journey_id),'not-found',[404],'foreign-get-refused');
  const confirmation=decodeDepositConfirmRequest(fixture.foreign_confirmation,'genuine supplied wallet transaction');
  await refusal(()=>foreign.depositConfirm(first.journey_id,confirmation),'not-found',[404],
    'foreign-confirm-refused');
  const after=projection(await owner.depositStart(body,fixture.start.idempotency_key));
  check(stable(first)===stable(after),'refusals preserve original owner deposit');
  await writeFile(output,stable({tests,cases,original:first,cookies:Object.fromEntries(
    [...jars].map(([name,cookies])=>[name,Object.fromEntries(cookies)]))}),{mode:0o600,flag:'wx'});
}else if(phase==='crash'){
  const request=decodeDepositStartRequest(fixture.crash.body,'genuine acknowledgement-gap request');
  let error;try{await owner.depositStart(request,fixture.crash.idempotency_key);}catch(caught){error=caught;}
  check(held&&error instanceof LostAcknowledgement,'caller never receives initial created journey');
  await writeFile(output,stable({tests,cases:[{case:'lost-acknowledgement',result:'PASS'}],
    interrupted:true}),{mode:0o600,flag:'wx'});
}else if(phase==='restart'){
  const request=decodeDepositStartRequest(fixture.crash.body,'identical acknowledgement-gap retry');
  const replay=projection(await owner.depositStart(request,fixture.crash.idempotency_key));
  check(stable(replay)===stable(previous.original),'restart recovers exact lost original response');
  const twice=projection(await owner.depositStart(request,fixture.crash.idempotency_key));
  check(stable(twice)===stable(replay),'restart retry does not duplicate deposit');
  await refusal(()=>foreign.journeyGet(replay.journey_id),'not-found',[404],'restart-foreign-get-refused');
  await refusal(()=>foreign.depositConfirm(replay.journey_id,
    decodeDepositConfirmRequest(fixture.foreign_confirmation,'actual wallet transaction')),
    'not-found',[404],'restart-foreign-confirm-refused');
  await writeFile(output,stable({tests,cases,original:replay}),{mode:0o600,flag:'wx'});
}else{throw new Error('unknown gate phase');}
'''


def write_private(path, value):
    with path.open('x', encoding='utf8') as handle:
        os.chmod(path, 0o600)
        json.dump(value, handle, sort_keys=True)
        handle.write('\n')


def persisted_deposits(store_root, principal, missing_empty=False):
    B.require(re.fullmatch('[a-z0-9_-]{1,128}', principal), 'actual principal store namespace')
    location = store_root / 'principals' / principal / 'store.bin'
    if missing_empty and not location.exists():
        B.require(not location.is_symlink(), 'absent genuine principal state has no alias')
        return {}, None
    path = B.private(str(location))
    B.require(path.stat().st_size <= B.MAX_BYTES, 'bounded actual Human store')
    raw = path.read_bytes()
    offset = 0

    def take(length):
        nonlocal offset
        B.require(0 <= length <= len(raw) - offset, 'actual store frame bounds')
        value = raw[offset:offset + length]
        offset += length
        return value

    def number(length):
        return int.from_bytes(take(length), 'big')

    def blob():
        return take(number(4))

    B.require(take(4) == b'LXHP' and number(4) == 2, 'actual Human principal store v2')
    rows, deposits = set(), {}
    count = number(4)
    B.require(count <= 100000, 'bounded actual store rows')
    for _ in range(count):
        table, key, written_at, value = number(1), blob().decode(), number(8), blob()
        B.require(table in (1, 2, 4, 5, 6, 7, 8) and (table, key) not in rows,
                  'actual distinct typed store rows')
        rows.add((table, key))
        if table == 1 and key.startswith('deposit-journey-'):
            record = json.loads(value)
            B.require(record['version'] == 3 and record['schema'] == 'current',
                      'actual current durable deposit record')
            B.require(record['journey_id'] not in deposits, 'one durable row per outer deposit ID')
            deposits[record['journey_id']] = {'row': key, 'written_at': written_at,
                                             'record': record}
    audit_keys = set()
    count = number(4)
    B.require(count <= 100000, 'bounded actual audit rows')
    for _ in range(count):
        key, _, _ = blob().decode(), number(8), blob()
        B.require(key not in audit_keys, 'actual distinct audit keys')
        audit_keys.add(key)
        disposition, references = number(1), number(4)
        B.require((disposition == 0 and references == 0)
                  or (disposition == 1 and references <= len(rows)), 'actual audit disposition')
        for _ in range(references):
            B.require((number(1), blob().decode()) in rows, 'actual pinned audit reference')
    B.require(offset == len(raw), 'actual store has no trailing bytes')
    return deposits, hashlib.sha256(raw).hexdigest()


def assert_record(rows, projection, request):
    B.require(projection['journey_id'] in rows, 'returned outer ID exists in persisted actual store')
    row = rows[projection['journey_id']]
    record = row['record']
    key = hashlib.sha256(b'layerx-human/movement-action/v1\0'
                         + request['idempotency_key'].encode()).digest()
    B.require(row['row'] == 'deposit-journey-' + key.hex()
              and record['idempotency_key'] == list(key), 'genuine immutable request key binding')
    B.require(record['phase'] == 'ready' and record['transaction'] is None
              and record['activity'] is None, 'initial deposit has no wallet or credit effect')
    for field in ('started_at', 'updated_at'):
        expected = datetime.fromtimestamp(record[field], timezone.utc)
        actual = datetime.fromisoformat(projection[field].replace('Z', '+00:00'))
        B.require(actual == expected, 'projection uses exact authoritative persisted timestamp')
    B.require(row['written_at'] == record['updated_at']
              and record['amount'] == int(request['body']['money']['amount'])
              and record['currency'] == request['body']['money']['currency'],
              'exact persisted amount currency and continuation write time')
    return row


def main():
    global GATE_REPORT, GATE_DIRECTORY
    B.require(len(sys.argv) == 1, 'no arguments accepted')
    os.umask(0o077)
    candidate_path = os.environ.get('PAXEER_X_CANDIDATE_MANIFEST')
    fixture_path = os.environ.get('PAXEER_X_HUMAN_DEPOSIT_INITIAL_MANIFEST')
    B.require(candidate_path and fixture_path,
              'genuine protected candidate and deposit authority process fixture required')
    sys.path.insert(0, str(ROOT / 'tools/paxeer-x'))
    from candidate import catalogue, load_private, validate
    candidate = load_private(candidate_path)
    validate(candidate, catalogue(ROOT / 'spec/paxeer-x/spec.kvx'))
    revision, source = B.identity()
    B.require(candidate['source']['revision'] == revision and not candidate['source']['dirty'],
              'genuine clean candidate revision required')
    fixture = B.load(fixture_path)
    B.require(fixture['schema'] == 'layerx-human-deposit-initial-response.v1', 'closed fixture profile')
    directory = B.private(fixture['evidence_directory'], True)
    B.require(not any(directory.iterdir()), 'fresh private evidence directory')
    state = B.private(fixture['disposable_state_directory'], True)
    B.require(state != directory, 'private disposable qualification authority state')
    source_paths = [
        'human/crates/layerx-human-service/src/server/production_components.rs',
        'human/crates/layerx-human-service/src/journeys/deposit.rs',
        'human/crates/layerx-human-service/src/server/movement_provider.rs',
        'human/crates/layerx-human-service/src/store/mod.rs',
        'human/crates/layerx-human-service/src/store/codec.rs',
        'human/apps/web/src/api/generated/index.ts',
        'human/apps/web/src/api/generated/conformance.ts',
        'human/schema/human-api/movement.kvx',
        'tools/qualification/paxeer-x/human-api-boundary.py',
        'tools/qualification/paxeer-x/human_deposit_initial_response.py',
    ]
    report = {'revision': revision, 'source_digest': source,
              'command': 'timeout 30m python3 tools/qualification/paxeer-x/human_deposit_initial_response.py',
              'source_files': {name: B.digest(ROOT / name) for name in source_paths},
              'cases': [], 'skipped': 0, 'status': 'FAIL'}
    GATE_REPORT, GATE_DIRECTORY = report, directory
    artifacts = {name: B.executable(row, revision, source)
                 for name, row in fixture['artifacts'].items()}
    roles = ('service', 'components', 'agent', 'core', 'paxeer', 'gateway', 'attestor')
    B.require(all(role in artifacts for role in roles), 'real production authority artifacts required')
    B.require(7 <= len(fixture['processes']) <= 32, 'bounded actual process inventory')
    by_role = {row['artifact']: row for row in fixture['processes']}
    B.require(all(role in by_role for role in roles), 'actual production process roles required')
    for row in fixture['processes']:
        process_state = B.private(row['state_directory'], True)
        B.require(process_state.is_relative_to(state), 'process uses only disposable qualification state')
        B.require(row['qualification_endpoints'], 'actual isolated process endpoints required')
        for endpoint in row['qualification_endpoints']:
            B.endpoint(endpoint)
    store_root = B.private(by_role['components']['environment']['LAYERX_HUMAN_STORE_ROOT'], True)
    B.require(store_root.is_relative_to(state), 'actual Human-owned disposable store only')
    node = Path(fixture['node'])
    B.require(node.is_absolute() and node.is_file() and not node.is_symlink()
              and os.access(node, os.X_OK) and B.digest(node) == fixture['node_sha256'],
              'pinned genuine Node runtime')
    ca = B.private(fixture['ca_pem'])
    context = ssl.create_default_context(cafile=str(ca))
    url = B.endpoint(fixture['url'])
    B.endpoint(fixture['origin'])
    service = by_role['service']['environment']
    B.require(service['LAYERX_HUMAN_LISTENER'] == 'tls'
              and service['LAYERX_HUMAN_BIND'] == '127.0.0.1:' + str(url.port)
              and service['LAYERX_HUMAN_WEB_ORIGIN'] == fixture['origin'],
              'genuine production HTTPS listener and origin')
    for key in ('LAYERX_HUMAN_TLS_CERT_DER', 'LAYERX_HUMAN_TLS_KEY_DER'):
        B.private(service[key])
    for name in (fixture['principal'], fixture['other_principal']):
        session = fixture['sessions'][name]
        B.require(session['assertion'] and session['cookies']['__Host-layerx_csrf'],
                  'actual authenticated disposable principal authority')
    B.require(fixture['principal'] != fixture['other_principal'], 'foreign principal required')
    B.require(fixture['start']['idempotency_key'] != fixture['crash']['idempotency_key'],
              'independent actual creation and crash requests')
    B.require(re.fullmatch('0x[0-9a-fA-F]{64}', fixture['foreign_confirmation']['wallet_transaction']),
              'actual provisioned disposable wallet transaction required for foreign refusal')
    processes = B.Processes(directory, artifacts)
    runner = directory / 'deposit-initial-client.mts'
    runner.write_text(CLIENT)
    runner.chmod(0o600)
    marker, release = directory / 'lost-ack.json', directory / 'discard-ack.json'
    child, child_log = None, None

    def ready():
        until = time.monotonic() + min(90, B.remaining())
        while time.monotonic() < until:
            B.require(all(item[0].poll() is None for item in processes.children.values()),
                      'actual production process exited')
            try:
                status, result = B.request(url, context, 'GET', '/readyz')
                if status == 200 and result.get('result', {}).get('ready') is True:
                    return
            except (OSError, B.http.client.HTTPException):
                pass
            time.sleep(0.1)
        raise RuntimeError('actual production readiness deadline')

    def client(phase, previous='-', asynchronous=False):
        output = directory / (phase + '-result.json')
        log = (directory / (phase + '-client.log')).open('xb')
        os.chmod(log.name, 0o600)
        argv = [str(node), '--experimental-strip-types', str(runner), str(ROOT), fixture_path,
                str(output), phase, str(previous), str(marker), str(release)]
        env = dict(os.environ, NODE_EXTRA_CA_CERTS=str(ca), NODE_TLS_REJECT_UNAUTHORIZED='1',
                   NODE_OPTIONS='')
        launched = subprocess.Popen(argv, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                                    stdout=log, stderr=log, start_new_session=True)
        if asynchronous:
            return launched, log, output
        try:
            B.require(launched.wait(timeout=min(300, B.remaining())) == 0,
                      'actual generated-client deposit phase failed: ' + phase)
        finally:
            if launched.poll() is None:
                os.killpg(launched.pid, signal.SIGKILL)
                launched.wait(timeout=10)
            log.close()
        observed = B.load(str(output))
        B.require(type(observed['tests']) is int and observed['tests'] > 0,
                  'real generated-client assertions required without skips')
        report['cases'].extend(observed['cases'])
        return observed, output

    try:
        for row in fixture['processes']:
            processes.start(row)
        ready()
        baseline, _ = persisted_deposits(store_root, fixture['principal_store_id'], True)
        B.require(not baseline, 'disposable authority fixture starts without economic deposit journeys')
        live, live_path = client('live')
        created, store_hash = persisted_deposits(store_root, fixture['principal_store_id'])
        assert_record(created, live['original'], fixture['start'])
        B.require(set(created) - set(baseline) == {live['original']['journey_id']},
                  'first duplicate conflict and foreign calls create exactly one actual deposit')
        write_private(directory / 'live-durable.json', {'store_sha256': store_hash,
                      'deposit': created[live['original']['journey_id']]})
        child, child_log, child_output = client('crash', live_path, True)
        until = time.monotonic() + min(90, B.remaining())
        while not marker.exists():
            B.require(child.poll() is None and time.monotonic() < until,
                      'actual durable creation acknowledgement marker missing')
            time.sleep(0.05)
        lost = B.load(str(marker))
        before_crash, store_hash = persisted_deposits(store_root, fixture['principal_store_id'])
        assert_record(before_crash, lost['original'], fixture['crash'])
        B.require(set(before_crash) - set(created) == {lost['original']['journey_id']},
                  'lost response belongs to exactly one genuinely persisted deposit')
        write_private(directory / 'crash-durable.json', {'store_sha256': store_hash,
                      'deposit': before_crash[lost['original']['journey_id']]})
        stopped = []
        for role in ('service', 'components'):
            name = by_role[role]['name']
            actual, log, row = processes.children.pop(name)
            B.require(actual.poll() is None, 'genuine live process required at crash window')
            os.killpg(actual.pid, signal.SIGKILL)
            B.require(actual.wait(timeout=10) == -signal.SIGKILL, 'actual ungraceful process crash')
            log.close()
            (directory / (name + '.log')).rename(directory / (name + '-before-crash.log'))
            stopped.append(row)
        write_private(release, {'discard': True})
        B.require(child.wait(timeout=min(30, B.remaining())) == 0, 'caller lost acknowledgement phase')
        child_log.close()
        interrupted = B.load(str(child_output))
        B.require(interrupted['interrupted'] is True, 'initial response never delivered to caller')
        report['cases'].extend(interrupted['cases'])
        for row in reversed(stopped):
            processes.start(row)
        ready()
        replay, _ = client('restart', marker)
        final, store_hash = persisted_deposits(store_root, fixture['principal_store_id'])
        assert_record(final, replay['original'], fixture['crash'])
        B.require(final == before_crash, 'restart retries and foreign refusals leave exact durable deposits')
        B.require(replay['original'] == lost['original'], 'original ID timestamps and continuation survive crash')
        report['cases'].extend([{'case': 'persisted-timestamps', 'result': 'PASS'},
                                {'case': 'crash-restart-exact-retry', 'result': 'PASS'},
                                {'case': 'no-second-economic-effect', 'result': 'PASS'}])
        write_private(directory / 'restart-durable.json', {'store_sha256': store_hash,
                      'deposit': final[replay['original']['journey_id']]})
        report['status'] = 'PASS'
        report['tests'] = live['tests'] + interrupted['tests'] + replay['tests'] + B.COUNT
        print('PAXEER_X_GATE tests=' + str(report['tests'])
              + ' skipped=0 human-deposit-initial-response revision=' + revision
              + ' cases=' + str(len(report['cases'])))
    finally:
        if child is not None and child.poll() is None:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait(timeout=10)
        if child_log is not None and not child_log.closed:
            child_log.close()
        processes.close()
        report['evidence_paths'] = sorted(str(path) for path in directory.iterdir())
        write_private(directory / 'report.json', report)


if __name__ == '__main__':
    def interrupted(_signal, _frame):
        raise RuntimeError('bounded deposit gate interrupted')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        main()
    except Exception as error:
        if GATE_REPORT is not None and GATE_DIRECTORY is not None:
            failure = {'type': type(error).__name__, 'detail': str(error)[:2048]}
            report_path = GATE_DIRECTORY / 'report.json'
            if not report_path.exists():
                GATE_REPORT['failure'] = failure
                GATE_REPORT['evidence_paths'] = sorted(str(path) for path in GATE_DIRECTORY.iterdir())
                write_private(report_path, GATE_REPORT)
            else:
                write_private(GATE_DIRECTORY / 'failure.json', failure)
        print('Human deposit initial response gate refused; inspect protected candidate, '
              'authority fixture and private process/client evidence.', file=sys.stderr)
        raise SystemExit(1)
