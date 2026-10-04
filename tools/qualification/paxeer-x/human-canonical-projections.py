#!/usr/bin/env python3
import argparse
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
import {readFile, writeFile} from 'node:fs/promises';
import {pathToFileURL} from 'node:url';
import {createHash, randomBytes} from 'node:crypto';
const [root, manifestPath, outputPath] = process.argv.slice(2);
const fixture = JSON.parse(await readFile(manifestPath, 'utf8'));
const {createHumanApiClient, HumanApiError} = await import(pathToFileURL(root + '/human/apps/web/src/api/generated/index.ts').href);
const {conformance} = await import(pathToFileURL(root + '/human/apps/web/src/api/generated/conformance.ts').href);
const {copyEntry} = await import(pathToFileURL(root + '/human/apps/web/copy/runtime.ts').href);
const {copyEntries} = await import(pathToFileURL(root + '/human/apps/web/copy/catalog.ts').href);
const {runtimeMessages} = await import(pathToFileURL(root + '/human/apps/web/copy/messages.generated.ts').href);
let tests = 0;
const stable = (value) => JSON.stringify(value, (_, item) => typeof item === 'bigint' ? item.toString() : item);
function check(value, message) { if (!value) throw new Error(message); tests += 1; }
check(stable(runtimeMessages) === stable(copyEntries.map(({key,message}) => [key,message])), 'runtime copy is generated from the actual authoritative catalog');
function copy(key) {
  check(typeof key === 'string' && copyEntry(key).message.length > 0, 'actual service projection copy resolves through the production catalog');
}
const clients = new Map();
for (const [name, session] of Object.entries(fixture.sessions)) {
  const jar = new Map(Object.entries(session.cookies));
  clients.set(name, createHumanApiClient({baseUrl:fixture.url, csrfToken:() => jar.get('__Host-layerx_csrf'), trace:() => 'trc_' + randomBytes(16).toString('hex'),
    fetch:async (url, init) => {
      const headers = new Headers(init.headers);
      headers.set('Origin', fixture.origin);
      headers.set('Cookie', [...jar].map(([key,value]) => key + '=' + value).join('; '));
      const response = await fetch(url, {...init, headers, redirect:'error', signal:init.signal ?? AbortSignal.timeout(20000)});
      for (const cookie of response.headers.getSetCookie()) {
        const pair = cookie.split(';')[0], index = pair.indexOf('=');
        jar.set(pair.slice(0,index), pair.slice(index+1));
      }
      return response;
    }}));
}
const client = clients.get(fixture.principal), foreign = clients.get(fixture.other_principal);
check(client !== undefined && foreign !== undefined, 'real distinct authenticated sessions');
async function denied(operation, codes) {
  let error; try { await operation(); } catch (caught) { error = caught; }
  check(error instanceof HumanApiError && codes.includes(error.detail.code), 'typed authenticated foreign-owner refusal');
}
function timestamp(value) {
  check(typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{3})?Z$/.test(value) && Number.isFinite(Date.parse(value)), 'real UTC timestamp preserves native precision');
}
const summaryFields = ['approval_id','agent_id','agent_name','counterparty','amount','reason_copy_key','expires_at','state','budget_remaining_after'];
const detailFields = ['approval_id','agent_id','agent_name','state','state_copy_key','reason_copy_key','facts','budget_remaining_after','created_at','evidence'];
function fields(value, expected) { check(stable(Object.keys(value).sort()) === stable([...expected].sort()), 'distinct closed canonical response fields'); }
function summary(value) {
  fields(value, summaryFields);
  check(/^apr_[a-f0-9]{64}$/.test(value.approval_id) && /^agt_[a-f0-9]{64}$/.test(value.agent_id), 'strict approval and actual managed child IDs');
  timestamp(value.expires_at);
  check(value.reason_copy_key === 'approval.reason.policy-required', 'actual policy reason preserved');
  copy(value.reason_copy_key);
  check(['receipt-verified','checkpoint-finalised','settlement-anchored'].includes(value.budget_remaining_after.verification), 'native verified post-reservation budget label');
  check(value.budget_remaining_after.money.currency === value.amount.currency, 'remaining budget retains the actual held asset currency');
}
async function exportEvidence(ref, supplied = client) {
  check(/^evd_[a-f0-9]{64}$/.test(ref.evidence_id), 'strict genuine evidence ID');
  const material = await supplied.evidenceGet(ref.evidence_id);
  check(material.evidence_id === ref.evidence_id, 'actual evidence response retains the requested owned identifier');
  const bytes = Buffer.from(material.bytes_base64, 'base64');
  check(bytes.toString('base64') === material.bytes_base64, 'export uses canonical complete base64 bytes');
  check(bytes.length > 0 && 'evd_' + createHash('sha256').update(bytes).digest('hex') === ref.evidence_id, 'exported actual evidence bytes bind exact digest');
  check(material.verification === ref.verification && material.class === ref.class, 'export retains actual verification and class');
  await denied(() => foreign.evidenceGet(ref.evidence_id), ['not-found','forbidden']);
}
const opened = await client.streamOpen();
const agents = (await client.agentList()).agents;
check(agents.length > 0, 'actual managed agent producer required');
for (const value of agents) {
  check(/^agt_[a-f0-9]{64}$/.test(value.agent_id), 'actual managed ID');
  copy(value.state_copy_key);
  copy(value.limit.enforcement_copy_key);
  for (const key of ['created_at','updated_at']) timestamp(value[key]);
  timestamp(value.spend.period_start); timestamp(value.spend.period_end);
  check(value.spend.verification === 'receipt-verified' || value.spend.verification === 'checkpoint-finalised' || value.spend.verification === 'settlement-anchored', 'native managed verification mapping');
  const point = await client.agentGet(value.agent_id);
  check(stable(point) === stable(value), 'shared managed list and get projection');
  check(value.evidence.length > 0, 'actual managed receipt evidence present');
  for (const ref of value.evidence) await exportEvidence(ref);
  await denied(() => foreign.agentGet(value.agent_id), ['not-found','forbidden']);
}
const inventory = await client.approvalList();
check(inventory.approvals.length > 0 && inventory.approvals.length <= 100, 'actual bounded approval producer inventory');
const seen = new Map();
for (const value of inventory.approvals) {
  summary(value); seen.set(value.approval_id, value);
  const detail = await client.approvalGet(value.approval_id);
  fields(detail, detailFields);
  copy(detail.state_copy_key);
  copy(detail.reason_copy_key);
  check(detail.agent_id === value.agent_id && detail.agent_name === value.agent_name && detail.state === value.state, 'shared list and detail actual association');
  check(detail.facts.amount.amount === value.amount.amount && detail.facts.amount.currency === value.amount.currency && detail.facts.counterparty === value.counterparty && detail.facts.expires_at === value.expires_at, 'same held disclosure facts across distinct projections');
  check(stable(detail.budget_remaining_after) === stable(value.budget_remaining_after), 'shared detail and summary use the same genuine post-reservation budget');
  timestamp(detail.created_at);
  check(detail.evidence.length > 0, 'real hold or budget evidence required');
  for (const ref of detail.evidence) await exportEvidence(ref);
  await denied(() => foreign.approvalGet(value.approval_id), ['not-found','forbidden']);
}
check(Array.isArray(fixture.observations) && fixture.observations.length >= 2, 'genuine legacy and native producer observations required');
const profiles = new Set();
for (const expected of fixture.observations) {
  profiles.add(expected.profile);
  const value = seen.get(expected.approval_id);
  check(value !== undefined && value.agent_id === expected.agent_id && value.counterparty === expected.counterparty && value.amount.amount === BigInt(expected.amount) && value.amount.currency === expected.currency && value.budget_remaining_after.money.amount === BigInt(expected.remaining_after), 'actual authenticated producer observation matches canonical facts and reserved budget');
  check(Date.parse(value.expires_at) === expected.activity_expires_at_unix_milliseconds, 'activity expiry preserves genuine wall-time units');
  const detail = await client.approvalGet(value.approval_id);
  check(Date.parse(detail.created_at) === expected.created_at_unix_seconds * 1000, 'creation time comes from genuine persisted Unix metadata');
  if (expected.profile === 3) {
    const local = detail.evidence.filter((reference) => reference.class === 'approval-hold');
    check(local.length === 2 && local.every((reference) => reference.verification === 'unverified'), 'actual held canonical bytes and local carrier never promoted to protocol verification');
    const allocation = detail.evidence.filter((reference) => reference.class === 'local-journey-state');
    check(allocation.length === 1 && allocation[0].verification === 'unverified', 'allocation report remains honestly local owned');
    const proof = detail.evidence.filter((reference) => reference.class === 'checkpoint-proof');
    check(proof.length >= 1 && proof.every((reference) => ['checkpoint-finalised','settlement-anchored'].includes(reference.verification)), 'real independently verified raw budget package remains separate from local materials');
    check(local.some((reference) => reference.evidence_id === 'evd_' + expected.held_digest), 'held carrier export bound to genuine immutable owner digest');
    check(proof.some((reference) => reference.evidence_id === 'evd_' + expected.budget_proof_digest), 'raw budget export bound to actual independently verified package');
    await denied(() => conformance['approval.approve']({client:foreign,params:{approval_id:expected.approval_id},body:expected.foreign_approve_body,idempotencyKey:expected.foreign_approve_key}), ['not-found','forbidden','step-up-required']);
  }

}
check(profiles.has(2) && profiles.has(3), 'real legacy and generic native producers both executed');
const home = await client.homeSummary();
check(stable(home.agents) === stable(agents) && stable(home.approvals) === stable(inventory.approvals), 'home uses identical canonical projections');
const page = await client.streamNext(opened.cursor);
let delivered = 0;
for (const event of page.events) {
  timestamp(event.observed_at);
  if (event.approval) { summary(event.approval); check(seen.has(event.approval.approval_id), 'durable approval stream uses canonical summary'); check(stable(event.approval) === stable(seen.get(event.approval.approval_id)), 'durable push retains the exact canonical approval facts'); delivered += 1; }
}
check(delivered > 0, 'real durable approval producer append observed');
check(Array.isArray(fixture.decisions) && fixture.decisions.length > 0, 'actual winning and repeated decision cases required');
for (const row of fixture.decisions) {
  check(['approval.approve','approval.reject'].includes(row.operation), 'schema-owned exact decision operation');
  const input = {client, params:{approval_id:row.approval_id}, body:row.body, idempotencyKey:row.idempotency_key};
  const decision = await conformance[row.operation](input);
  fields(decision,['approval_id','state','state_copy_key','money_moved','moved_copy_key','evidence']);
  check(decision.approval_id === row.approval_id && decision.state === row.state && decision.money_moved === false, 'actual decision never claims money moved');
  copy(decision.state_copy_key);
  copy(decision.moved_copy_key);
  check(decision.moved_copy_key === 'approval.decision.no-money-moved', 'decision wording states authorization without claiming execution');
  check(stable(await conformance[row.operation](input)) === stable(decision), 'same decision replay returns exact original outcome');
  const terminal = await client.approvalGet(row.approval_id);
  check(terminal.state === decision.state, 'terminal owner record remains readable through canonical detail');
  fields(terminal, detailFields);
  copy(terminal.state_copy_key);
  check(['receipt-verified','checkpoint-finalised','settlement-anchored'].includes(terminal.budget_remaining_after.verification), 'terminal budget retains its actual achieved verification level');
  for (const reference of terminal.evidence) await exportEvidence(reference);
  const terminalInventory = await client.approvalList();
  const terminalSummary = terminalInventory.approvals.find((value) => value.approval_id === row.approval_id);
  check(terminalSummary !== undefined, 'actual terminal approval remains present in canonical inventory');
  summary(terminalSummary);
  check(terminalSummary.state === terminal.state && stable(terminalSummary.budget_remaining_after) === stable(terminal.budget_remaining_after), 'terminal list and detail share genuine state and budget');
  const terminalHome = await client.homeSummary();
  check(stable(terminalHome.approvals) === stable(terminalInventory.approvals), 'terminal home retains canonical owner inventory');
  await denied(() => foreign.approvalGet(row.approval_id), ['not-found','forbidden']);
}
await writeFile(outputPath, stable({tests}), {mode:0o600});
'''


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--candidate-manifest', required=True)
    args = parser.parse_args()
    sys.path.insert(0, str(ROOT / 'tools/paxeer-x'))
    from candidate import catalogue, load_private, validate
    candidate = load_private(args.candidate_manifest)
    validate(candidate, catalogue(ROOT / 'spec/paxeer-x/spec.kvx'))
    revision, source = B.identity()
    B.require(candidate['source']['revision'] == revision and not candidate['source']['dirty'], 'genuine clean candidate source')
    fixture = os.environ.get('PAXEER_X_HUMAN_CANONICAL_MANIFEST')
    B.require(fixture, 'genuine private canonical projection fixture required')
    material = B.load(fixture)
    B.require(material['schema'] == 'layerx-human-canonical-projections.v1', 'closed real-process fixture profile')
    directory = B.private(material['evidence_directory'], True)
    B.require(not any(directory.iterdir()), 'fresh private evidence directory')
    artifacts = {name:B.executable(row,revision,source) for name,row in material['artifacts'].items()}
    roles = ('service','components','agent','core','paxeer')
    B.require(all(role in artifacts for role in roles), 'actual production artifacts required')
    node = Path(material['node'])
    B.require(node.is_absolute() and node.is_file() and os.access(node,os.X_OK) and B.digest(node) == material['node_sha256'], 'pinned actual Node runtime')
    ca = B.private(material['ca_pem'])
    context = ssl.create_default_context(cafile=str(ca))
    url = B.endpoint(material['url'])
    B.require(material['principal'] != material['other_principal'], 'genuine distinct principal ownership')
    B.require(5 <= len(material['processes']) <= 32, 'bounded actual production processes')
    by_role = {row['artifact']:row for row in material['processes']}
    B.require(all(role in by_role for role in roles), 'actual process roles')
    service = by_role['service']['environment']
    B.require(service['LAYERX_HUMAN_LISTENER'] == 'tls' and service['LAYERX_HUMAN_BIND'] == '127.0.0.1:' + str(url.port) and service['LAYERX_HUMAN_WEB_ORIGIN'] == material['origin'], 'isolated real HTTPS boundary')
    B.private(service['LAYERX_HUMAN_TLS_CERT_DER']); B.private(service['LAYERX_HUMAN_TLS_KEY_DER'])
    processes = B.Processes(directory,artifacts)
    try:
        for row in material['processes']: processes.start(row)
        until = time.monotonic() + min(90,B.remaining())
        while True:
            B.require(all(child.poll() is None for child,_,_ in processes.children.values()), 'actual process remains alive')
            try:
                status,ready = B.request(url,context,'GET','/readyz')
                if status == 200 and ready.get('result',{}).get('ready') is True: break
            except (OSError,B.http.client.HTTPException): pass
            B.require(time.monotonic() < until, 'real readiness deadline')
            time.sleep(0.1)
        runner = directory / 'canonical-projections.mts'; runner.write_text(CLIENT); runner.chmod(0o600)
        output = directory / 'canonical-result.json'
        with (directory / 'canonical-client.log').open('xb') as log:
            os.chmod(log.name,0o600)
            result = subprocess.run([str(node),'--experimental-strip-types',str(runner),str(ROOT),fixture,str(output)],cwd=ROOT,env=dict(os.environ,NODE_EXTRA_CA_CERTS=str(ca),NODE_TLS_REJECT_UNAUTHORIZED='1',NODE_OPTIONS=''),stdout=log,stderr=log,timeout=min(900,B.remaining()))
        B.require(result.returncode == 0,'real canonical production boundary case failed')
        observed = B.load(output)
        B.require(type(observed['tests']) is int and observed['tests'] >= 30,'real bounded positive and negative cases executed without skips')
        print('human-canonical-projections: PASS tests=' + str(observed['tests']) + ' revision=' + revision)
    finally:
        processes.close()


if __name__ == '__main__':
    try: main()
    except Exception as error:
        print('human-canonical-projections: FAIL: ' + str(error),file=sys.stderr)
        raise SystemExit(1)
