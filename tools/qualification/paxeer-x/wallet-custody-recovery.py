#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[3]
NODE = Path('/root/lx-toolchains/node24/bin/node')
PNPM = Path('/root/lx-toolchains/node24/private-pnpm/corepack/v1/pnpm/9.15.9/bin/pnpm.cjs')
EVIDENCE = Path(os.environ.get('WALLET_CUSTODY_RECOVERY_EVIDENCE_ROOT', '/root/lx-ops/paxeer-x-integration-2026-10-03/task-14.11-evidence'))
SOURCES = ['human/wallet/sdk/src/provider.ts', 'human/wallet/gateway/src/routes/sign.ts',
           'human/wallet/gateway/src/custody/authorization.ts',
           'human/wallet/gateway/src/agent/actions/orchestrator.ts',
           'human/wallet/gateway/migrations/013_wallet_custody_authorizations.sql',
           'human/wallet/gateway/test/custody-authorization.test.ts',
           'tools/qualification/paxeer-x/wallet-custody-recovery.py']


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def sources():
    paths = set(SOURCES)
    for tree in ('human/wallet/sdk/src', 'human/wallet/gateway/src'):
        paths.update(str(p.relative_to(ROOT)) for p in (ROOT / tree).rglob('*.ts'))
    for package in ('human/wallet/sdk', 'human/wallet/gateway'):
        paths.update(package + '/' + f for f in ('package.json', 'tsconfig.json'))
    return {p: digest(ROOT / p) for p in sorted(paths)}


def artifacts():
    result = {}
    for tree in ('human/wallet/sdk/dist', 'human/wallet/gateway/dist'):
        paths = list((ROOT / tree).rglob('*.js'))
        require(paths, 'actual compiled SDK and gateway artifacts required')
        result.update({str(p.relative_to(ROOT)): digest(p) for p in paths})
    return result


def protected(path):
    metadata = path.lstat()
    require(stat.S_ISREG(metadata.st_mode) and stat.S_IMODE(metadata.st_mode) == 0o600
            and metadata.st_uid == os.getuid() and metadata.st_nlink == 1 and metadata.st_size <= 131072,
            'owner-only genuine custody fixture required')
    return json.loads(path.read_text())


def run(command, log, environment, seconds):
    with log.open('wb') as stream:
        log.chmod(0o600)
        result = subprocess.run(command, cwd=ROOT, env=environment, stdout=stream,
                                stderr=subprocess.STDOUT, timeout=seconds)
    print(json.dumps({'command': command[:4], 'exit_code': result.returncode, 'log_path': str(log)}), flush=True)
    return result.returncode


HTTP_CASE = r'''
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {spawn} from 'node:child_process';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';
const root=process.cwd(), require=createRequire(root+'/human/wallet/gateway/package.json');
const {Pool}=require('pg');
const {PaxeerProvider,decodeCustodyAuthorization}=await import(pathToFileURL(root+'/human/wallet/sdk/dist/provider.js'));
const store=await import(pathToFileURL(root+'/human/wallet/gateway/dist/custody/authorization.js'));
const fixture=JSON.parse(fs.readFileSync(process.env.WALLET_CUSTODY_AUTHORIZATION_FIXTURE,'utf8'));
const runtime=fixture.runtime;
assert(runtime&&runtime.environment&&runtime.owner_token&&runtime.foreign_token,'genuine JWT/process fixture required');
assert.equal(runtime.environment.DATABASE_URL,fixture.database_url);
const url=new URL(runtime.gateway_url),rpc=new URL(runtime.rpc_url);
for(const u of [url,rpc])assert(['http:','https:'].includes(u.protocol)&&['localhost','127.0.0.1','[::1]'].includes(u.hostname)&&!u.username&&!u.password);
assert.equal(url.protocol,'http:');assert(url.port&&url.pathname==='/');
assert.equal(runtime.environment.HYPERPAXEER_RPC_URL,runtime.rpc_url);
assert.equal(BigInt(runtime.environment.HYPERPAXEER_CHAIN_ID),decodeCustodyAuthorization(fixture.signed.identity.custody).chainId);
assert(decodeCustodyAuthorization(fixture.signed.identity.custody).deadline<=BigInt(Math.floor(Date.now()/1000)), 'genuine retained expired consent required');
const pool=new Pool({connectionString:fixture.database_url});
const rows=[fixture.signed.identity.id,fixture.unknown.identity.id];
let ownsRows=false,child=null,log=null;
async function transaction(work){const c=await pool.connect();try{await c.query('begin');const r=await work(c);await c.query('commit');return r;}catch(e){await c.query('rollback');throw e;}finally{c.release();}}
async function start(){
 const code=`const {buildApp}=await import(${JSON.stringify(pathToFileURL(root+'/human/wallet/gateway/dist/index.js').href)});const app=await buildApp();await app.listen({host:'127.0.0.1',port:${Number(url.port)}});`;
 log=fs.openSync(process.env.CUSTODY_PROCESS_LOG,'a',0o600);
 child=spawn(process.execPath,['--input-type=module','--eval',code],{cwd:root+'/human/wallet/gateway',env:{...process.env,...runtime.environment,NODE_ENV:'test',API_WORKERS:'1'},stdio:['ignore',log,log]});
 fs.closeSync(log);log=null;
 for(let i=0;i<150;i++){
  assert(child.exitCode===null,'actual candidate gateway exited before admission');
  try{const r=await fetch(new URL('/healthz',url));if(r.ok&&r.headers.get('x-served-by')==='paxeer-wallet-gateway'){const h=await r.json();assert.equal(h.chain_id,Number(runtime.environment.HYPERPAXEER_CHAIN_ID));return;}}catch{}
  await new Promise(r=>setTimeout(r,200));
 }
 throw Error('actual candidate gateway readiness unavailable');
}
async function stop(){if(!child)return;const p=child;child=null;p.kill('SIGTERM');await new Promise(resolve=>{if(p.exitCode!==null)return resolve();p.once('exit',resolve);setTimeout(()=>{p.kill('SIGKILL');},5000).unref();});}
async function request(path,method='GET',body,token=runtime.owner_token){const r=await fetch(new URL(path,url),{method,headers:{Authorization:'Bearer '+token,...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined});return {status:r.status,body:await r.json()};}
function provider(){return new PaxeerProvider({gatewayUrl:runtime.gateway_url,rpcUrl:runtime.rpc_url,chainId:Number(runtime.environment.HYPERPAXEER_CHAIN_ID),token:()=>runtime.owner_token});}
try{
 const present=await pool.query('select id from wallet_custody_authorizations where id=any($1::text[])',[rows]);assert.equal(present.rowCount,0);
 ownsRows=true;
 await transaction(async c=>{await store.retainCustodyAuthorization(c,fixture.signed.identity);await store.completeCustodyAuthorization(c,fixture.signed.identity,fixture.signed.signature,fixture.signed.evidence);await store.retainCustodyAuthorization(c,fixture.unknown.identity);});
 await start();
 let sdk=provider();const accounts=await sdk.request({method:'eth_requestAccounts'});assert.equal(accounts[0].toLowerCase(),fixture.signed.identity.address.toLowerCase());
 const recover=()=>sdk.request({method:'paxeer_recoverCustody',params:[{custody:fixture.signed.identity.custody}]});
 assert.equal(await recover(),fixture.signed.signature);
 const replay=await request('/v1/wallet/sign-custody','POST',{custody:fixture.signed.identity.custody});assert.equal(replay.status,200);assert.equal(replay.body.signature,fixture.signed.signature);assert(!('tx_hash' in replay.body));
 sdk.disconnect();await stop();await start();sdk=provider();await sdk.request({method:'eth_requestAccounts'});assert.equal(await recover(),fixture.signed.signature);
 assert.equal(await sdk.request({method:'paxeer_signCustody',params:[{custody:fixture.signed.identity.custody}]}),fixture.signed.signature);
 const pending=await request('/v1/wallet/sign-custody','POST',{custody:fixture.unknown.identity.custody});assert.equal(pending.status,409);assert.equal(pending.body.error,'custody_authorization_unknown');
 await assert.rejects(sdk.request({method:'paxeer_signCustody',params:[{custody:fixture.unknown.identity.custody}]}),e=>e.data?.status===409&&e.data.body.error==='custody_authorization_unknown');
 const ambiguous=await transaction(c=>store.readCustodyAuthorization(c,fixture.unknown.identity));assert.equal(ambiguous.state,'signing_unknown');assert.equal(ambiguous.signature,null);assert.equal(ambiguous.evidence,null);
 const foreign=await request('/v1/wallet/custody-authorization/'+fixture.signed.identity.id,'GET',undefined,runtime.foreign_token);assert([403,404].includes(foreign.status));assert(!foreign.body.signature);
 assert.equal((await request('/v1/wallet/custody-authorization/0x01')).status,400);
 assert.equal((await request('/v1/wallet/sign-custody','POST',{custody:'0x01'})).status,400);
 await assert.rejects(sdk.request({method:'paxeer_recoverCustody',params:[{custody:'0x01'}]}));
 console.log('actual storage, SDK, authenticated gateway, expired retained proof, process restart, replay and durable unknown paths passed');
}finally{await stop();if(ownsRows)await pool.query('delete from wallet_custody_authorizations where id=any($1::text[])',[rows]);await pool.end();}
'''


def main():
    EVIDENCE.mkdir(parents=True, exist_ok=True, mode=0o700)
    require(not EVIDENCE.stat().st_mode & 0o077, 'private evidence directory required')
    environment = os.environ.copy()
    environment['PATH'] = str(NODE.parent) + ':' + environment.get('PATH', '')
    require(NODE.is_file() and PNPM.is_file(), 'actual Node24/pnpm9.15.9 tooling required')
    manifest = EVIDENCE / 'wallet-custody-candidate.json'
    if sys.argv[1:] == ['--build']:
        require(subprocess.check_output([str(NODE), str(PNPM), '--version'], env=environment, text=True).strip() == '9.15.9', 'pnpm9.15.9 required')
        before = sources()
        for name in ('sdk', 'gateway'):
            code = run([str(NODE), str(PNPM), '--dir', 'human/wallet/' + name, 'exec', 'tsc', '-p', 'tsconfig.json'],
                       EVIDENCE / ('build-' + name + '.log'), environment, 600)
            if code:
                return code
        require(sources() == before, 'source changed during compilation')
        manifest.write_text(json.dumps({'sources': before, 'artifacts': artifacts()}, sort_keys=True))
        manifest.chmod(0o600)
        print(json.dumps({'build_exit_code': 0, 'candidate_manifest': str(manifest)}))
        return 0
    require(not sys.argv[1:], 'only --build or the declared verification command is supported')
    binding = protected(manifest)
    require(binding['sources'] == sources() and binding['artifacts'] == artifacts(), 'candidate does not match whole current source and actual artifacts')
    path = os.environ.get('WALLET_CUSTODY_AUTHORIZATION_FIXTURE')
    require(path, 'missing genuine protected isolated PostgreSQL, attestor signature, JWT, RPC and runtime fixture')
    fixture = protected(Path(path))
    parsed = urlparse(fixture['database_url'])
    require(fixture.get('isolated_database') is True and parsed.scheme in ('postgres', 'postgresql')
            and parsed.hostname in ('localhost', '127.0.0.1', '::1')
            and parsed.path.startswith('/wallet_custody_qualification_'), 'dedicated genuine local fixture database required')
    require(isinstance(fixture.get('runtime'), dict), 'actual JWT and candidate process environment required')
    code = run([str(NODE), str(PNPM), '--dir', 'human/wallet/gateway', 'exec', 'vitest', 'run', 'test/custody-authorization.test.ts'],
               EVIDENCE / 'storage.log', environment, 180)
    if code:
        return code
    script = EVIDENCE / 'actual-custody-http.mjs'
    script.write_text(HTTP_CASE)
    script.chmod(0o600)
    environment['CUSTODY_PROCESS_LOG'] = str(EVIDENCE / 'process.log')
    return run([str(NODE), str(script)], EVIDENCE / 'http.log', environment, 240)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (RuntimeError, KeyError, ValueError, OSError, subprocess.TimeoutExpired):
        print(json.dumps({'exit_code': 78, 'qualification': 'UNQUALIFIED', 'observed': 'required genuine fixture, source-matched candidate or bounded target unavailable'}))
        sys.exit(78)
