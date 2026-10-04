#!/usr/bin/env python3
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import time
import urllib.parse

ROOT = Path(__file__).resolve().parents[3]
NODE = Path('/root/lx-toolchains/node24/bin/node')
WEB = ROOT / 'human/apps/web'
EVIDENCE = Path(os.environ.get('PRIVATE_HUMAN_SPONSORED_EVIDENCE_ROOT', '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task146'))
MANIFEST = EVIDENCE / 'build-manifest.json'
SDK_MANIFEST = Path(os.environ.get('PRIVATE_HUMAN_SPONSORED_SDK_MANIFEST', '/root/lx-ops/paxeer-x-integration-2026-10-03/task-14.3-evidence/wallet-sponsored-sdk-candidate.json'))
SHARED_MANIFEST = Path(os.environ.get('PRIVATE_HUMAN_SPONSORED_SHARED_BUILD', '/root/lx-ops/paxeer-x-integration-2026-10-03/qualification/task145/build-manifest.json'))
CASES = ['first_use', 'domain_changed', 'nonce_changed', 'expiry_changed', 'chain_changed', 'account_changed',
         'target_changed', 'amount_changed', 'calldata_changed', 'stale_consent', 'restart_retry', 'reload', 'expired_retry']


def require(value, message):
    if not value:
        raise RuntimeError(message)


def private(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and not path.is_symlink() and info.st_nlink == 1
            and info.st_uid == os.getuid() and info.st_mode & 0o077 == 0, 'protected regular fixture/evidence required')
    return path


def digest(path):
    require(path.is_file() and not path.is_symlink(), 'regular source/artifact required')
    return hashlib.sha256(path.read_bytes()).hexdigest()


def shared():
    spec = importlib.util.spec_from_file_location('private_human_custody_build', ROOT / 'tools/qualification/paxeer-x/private-human-custody.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def sources(helper):
    paths = set(helper.source_paths())
    paths.update([WEB / 'e2e/gas-station.test.ts', Path(__file__).resolve()])
    return helper.hashes(sorted(paths))


def bound_manifest(helper):
    sdk = json.loads(private(SDK_MANIFEST).read_text())
    for group in ('sources', 'artifacts'):
        require(sdk.get(group), 'genuine SDK candidate inventory required')
        for path, expected in sdk[group].items():
            require(digest(ROOT / path) == expected, 'frozen SDK candidate mismatch')
    candidate = json.loads(private(SHARED_MANIFEST).read_text())
    require(candidate.get('version') == 1 and candidate['sources'] == helper.hashes(helper.source_paths()), 'complete shared private web source build required')
    require(candidate['artifacts'] == helper.hashes(helper.artifact_paths()), 'genuine shared web/SDK artifacts changed')
    return {'version': 1, 'sources': sources(helper), 'artifacts': candidate['artifacts'], 'shared_manifest_sha256': digest(SHARED_MANIFEST)}


def run(argv, name, timeout, env=None):
    log = EVIDENCE / (name + '.log')
    with log.open('wb') as output:
        log.chmod(0o600)
        result = subprocess.run(argv, cwd=ROOT, env=env, stdout=output, stderr=subprocess.STDOUT, timeout=timeout)
    print(json.dumps({'command': argv, 'exit_code': result.returncode, 'log_path': str(log)}), flush=True)
    return result.returncode


BROWSER = r'''
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {createRequire} from 'node:module';
const require=createRequire(process.env.PRIVATE_HUMAN_WEB_PACKAGE);
const {chromium}=require('@playwright/test');
const config=JSON.parse(fs.readFileSync(process.env.PRIVATE_HUMAN_SPONSORED_RUNTIME,'utf8'));
const browser=await chromium.launch({headless:true});
try {
 for(const name of JSON.parse(process.env.PRIVATE_HUMAN_SPONSORED_CASES)) {
  const scenario=config.cases[name];
  assert.ok(scenario&&Array.isArray(scenario.actions)&&scenario.actions.length);
  const context=await browser.newContext({storageState:config.storage_state});
  const page=await context.newPage(); const signatures=[]; const submits=[]; let statuses=0; const dialogs=[]; const responses=[];
  page.on('request',request=>{
   const url=new URL(request.url());
   if(url.pathname.startsWith('/v1/wallet/')) assert.equal(url.origin,new URL(config.gateway_url).origin,'wallet authority escaped unified endpoint');
   if(url.pathname==='/v1/wallet/sign-digest') signatures.push(request.postDataJSON()?.construction);
   if(url.pathname==='/v1/wallet/sponsored/submit') submits.push(request.postDataJSON());
   if(url.pathname==='/v1/wallet/sponsored/status') statuses++;
   assert.ok(!['/v1/wallet/sign','/v1/wallet/sign-message'].includes(url.pathname),'generic signing fallback');
  });
  page.on('response',response=>{
   if(new URL(response.url()).pathname==='/v1/wallet/sponsored/submit') responses.push(response);
  });
  page.on('dialog',async dialog=>{dialogs.push(dialog.message());await dialog.accept();});
  await page.goto(new URL(scenario.path,config.app_url).toString());
  let beforeRestart=null;
  for(const action of scenario.actions) {
   if(action.kind==='fill') await page.locator(action.selector).fill(action.value);
   else if(action.kind==='click') await page.locator(action.selector).click();
   else if(action.kind==='reload') {beforeRestart={signatures:signatures.length,submits:submits.length}; await page.reload();}
   else if(action.kind==='visible') await page.locator(action.selector).waitFor({state:'visible',timeout:30000});
   else if(action.kind==='restart') {
    beforeRestart={signatures:signatures.length,submits:submits.length};
    const response=await fetch(new URL('/control/restart',config.control_url),{method:'POST',body:'{}'}); assert.ok(response.ok);
   } else if(action.kind==='wait_expiry') {
    assert.ok(submits.length);const wait=Number(BigInt(submits[0].construction.quote.deadline)*1000n-BigInt(Date.now()))+100;
    assert.ok(wait>=0&&wait<=60000); await page.waitForTimeout(wait);
   } else throw new Error('unknown real private-browser action');
  }
  assert.ok(await page.locator(scenario.result_selector).isVisible());
  if(['first_use','restart_retry','reload'].includes(name)) {
   assert.equal(signatures.filter(c=>c?.kind==='sponsored_batch').length,1);
   assert.equal(signatures.filter(c=>c?.kind==='eip7702_authorization').length,1);
   assert.ok(submits.length>0); assert.equal(new Set(submits.map(body=>JSON.stringify(body))).size,1);
   const batch=signatures.find(c=>c.kind==='sponsored_batch'); const authorization=signatures.find(c=>c.kind==='eip7702_authorization');
   assert.equal(batch.account.toLowerCase(),config.account.toLowerCase()); assert.equal(batch.chainId,String(config.chain_id));
   assert.ok(batch.calls.length>0&&batch.quote.deadline&&batch.quote.sponsor&&batch.quote.tokenAmount&&batch.quote.maxTokenAmount);
   assert.equal(authorization.chainId,batch.chainId);assert.equal(authorization.address.toLowerCase(),config.paymaster.toLowerCase());
   for(const body of submits){assert.deepEqual(body.construction,batch);assert.equal(body.authorization.chainId,authorization.chainId);assert.equal(body.authorization.nonce,authorization.nonce);assert.equal(body.authorization.address.toLowerCase(),authorization.address.toLowerCase());assert.ok(body.account_signature&&body.relayer_signature);}
   assert.ok(dialogs.some(text=>text.includes(batch.account)&&text.includes(batch.chainId)),'account/network consent absent');
   if(name!=='first_use'){assert.ok(beforeRestart);assert.equal(signatures.length,beforeRestart.signatures);assert.ok(statuses>0);}
   assert.ok(responses.length);
  } else if(name==='expired_retry') {
   assert.ok(beforeRestart&&beforeRestart.signatures===2);assert.equal(signatures.length,2);assert.ok(statuses>0);
   assert.equal(submits.length,beforeRestart.submits,'expired bytes resubmitted');
  } else {
   assert.equal(submits.length,0,'altered or stale construction reached adapter');
   assert.ok(signatures.length<=1,'altered or stale construction signed both requests');
  }
  await context.close(); console.log(JSON.stringify({case:name,passed:true}));
 }
} finally {await browser.close();}
'''


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', action='store_true')
    options = parser.parse_args()
    EVIDENCE.mkdir(parents=True, exist_ok=True, mode=0o700)
    helper = shared()
    require(NODE.is_file(), 'existing Node24 required')
    candidate = bound_manifest(helper)
    if options.build:
        MANIFEST.write_text(json.dumps(candidate, sort_keys=True))
        MANIFEST.chmod(0o600)
        print(json.dumps({'exit_code': 0, 'status': 'reused-complete-shared-build', 'manifest': str(MANIFEST), 'shared_manifest': str(SHARED_MANIFEST)}))
        return 0
    require(json.loads(private(MANIFEST).read_text()) == candidate, 'task source/artifact differs from bound complete build')
    code = run([str(NODE), '--test', '--test-reporter=tap', str(WEB / 'e2e/gas-station.test.ts')], 'verify-gas-station', 180)
    if code:
        return code
    supplied = os.environ.get('PRIVATE_HUMAN_SPONSORED_RUNTIME')
    if not supplied:
        print(json.dumps({'exit_code': 78, 'status': 'prerequisite-unavailable', 'reason': 'approved real isolated private browser/gateway/station/attestor/chain fixture required'}))
        return 78
    config_path = private(Path(supplied))
    config = json.loads(config_path.read_text())
    require(config.get('version') == 1 and config.get('isolated') is True and config.get('approved_sponsored_execution') is True
            and config.get('live_funded_transactions') is False, 'explicit real disposable execution authority required')
    require(set(config['cases']) == set(CASES), 'full private browser construction/refusal/recovery inventory required')
    for key in ('app_url', 'gateway_url', 'rpc_url', 'control_url'):
        url = urllib.parse.urlparse(config[key])
        require(url.scheme in ('http', 'https') and url.hostname in ('localhost', '127.0.0.1', '::1') and not url.username and not url.password, 'isolated genuine service endpoints required')
    require(set(service['kind'] for service in config['services']) == {'web', 'gateway', 'station', 'chain', 'attestor'}, 'genuine whole-boundary services required')
    for service in config['services']:
        pid = int(service['pid'])
        os.kill(pid, 0)
        executable = Path(os.readlink(f'/proc/{pid}/exe'))
        require(digest(executable) == service['executable_sha256'], 'real process executable mismatch')
    private(Path(config['storage_state']))
    script = EVIDENCE / 'private-human-sponsored-browser.mjs'
    script.write_text(BROWSER)
    script.chmod(0o600)
    environment = dict(os.environ)
    environment.update({'PRIVATE_HUMAN_WEB_PACKAGE': str(WEB / 'package.json'), 'PRIVATE_HUMAN_SPONSORED_CASES': json.dumps(CASES)})
    code = run([str(NODE), str(script)], 'verify-real-browser', 600, environment)
    require(candidate == bound_manifest(helper), 'qualification changed source or compiled candidate')
    return code


if __name__ == '__main__':
    try:
        sys.exit(main())
    except subprocess.TimeoutExpired:
        print(json.dumps({'exit_code': 124, 'reason': 'bounded task command timeout'}))
        sys.exit(124)
    except (OSError, KeyError, ValueError, RuntimeError) as error:
        print(json.dumps({'exit_code': 2, 'reason': str(error) if isinstance(error, RuntimeError) else type(error).__name__}))
        sys.exit(2)
