#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
NODE = Path('/root/lx-toolchains/node24/bin/node')
PNPM = Path('/root/lx-toolchains/node24/private-pnpm/corepack/v1/pnpm/9.15.9/bin/pnpm.cjs')
SOURCES = [
    'human/apps/wallet/src/surfaces/useSurface.ts', 'human/apps/wallet/src/surfaces/FeeChoice.tsx',
    'human/wallet/sdk/src/modules/gas-station.ts', 'human/wallet/sdk/src/provider.ts',
    'human/wallet/gateway/src/routes/sign.ts', 'tools/qualification/paxeer-x/wallet-sponsored-fees.py',
    *[f'human/apps/wallet/src/surfaces/{name}View.tsx' for name in ('Bridge', 'Exchange', 'Launchpad', 'WebData')],
]
CASES = ['first_use', 'cancel_consent', 'expired_quote', 'account_changed', 'chain_changed',
         'calldata_changed', 'station_unavailable', 'retry', 'lost_response', 'reload', 'fee_paths_distinct']


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(path):
    result = hashlib.sha256()
    with Path(path).open('rb') as source:
        for chunk in iter(lambda: source.read(1048576), b''):
            result.update(chunk)
    return result.hexdigest()


def private(path):
    require(path.exists() and not path.stat().st_mode & 0o077, 'protected fixture/evidence path required')
    return path


def local_url(value):
    url = urllib.parse.urlparse(value)
    require(url.scheme in ('http', 'https') and url.hostname in ('127.0.0.1', 'localhost', '::1')
            and not url.username and not url.password, 'genuine fixture endpoints must be isolated loopback services')
    return value


def evidence_dir():
    path = Path(os.environ['WALLET_SPONSORED_EVIDENCE_ROOT']).resolve()
    private(path)
    require(path.is_dir(), 'private evidence directory required')
    return path


def artifacts():
    paths = []
    for directory in ('human/wallet/sdk/dist', 'human/wallet/gateway/dist', 'human/apps/wallet/.next/server', 'human/apps/wallet/.next/static'):
        tree = ROOT / directory
        require(tree.is_dir(), 'missing genuine build artifact tree: ' + directory)
        paths.extend(p for p in tree.rglob('*') if p.is_file() and p.suffix in ('.js', '.mjs', '.json'))
    require(paths, 'actual compiled artifacts required')
    return {str(path.relative_to(ROOT)): digest(path) for path in sorted(paths)}


def build():
    evidence = evidence_dir()
    require(NODE.is_file() and PNPM.is_file(), 'installed official Node24 and pnpm9.15.9 required')
    require(subprocess.check_output([str(NODE), str(PNPM), '--version'], text=True).strip() == '9.15.9', 'pnpm9.15.9 required')
    env = os.environ.copy()
    env['PATH'] = str(NODE.parent) + ':' + env.get('PATH', '')
    env['NEXT_TELEMETRY_DISABLED'] = '1'
    targets = [('sdk', 'human/wallet/sdk', ['exec', 'tsc', '-p', 'tsconfig.json']),
               ('gateway', 'human/wallet/gateway', ['run', 'build']),
               ('wallet', 'human/apps/wallet', ['run', 'build'])]
    generated = [ROOT / p for p in ('human/apps/wallet/next-env.d.ts', 'human/apps/wallet/tsconfig.json',
                                  'human/apps/wallet/public/manifest.json', 'human/apps/wallet/public/sw.js')]
    original = {path: path.read_bytes() if path.exists() else None for path in generated}
    results = []
    try:
        for name, directory, args in targets:
            log = evidence / ('wallet-sponsored-build-' + name + '.log')
            with log.open('wb') as output:
                log.chmod(0o600)
                result = subprocess.run([str(NODE), str(PNPM), '--dir', directory, *args], cwd=ROOT,
                                        env=env, stdout=output, stderr=subprocess.STDOUT, timeout=600)
            results.append({'target': name, 'exit_code': result.returncode, 'log_path': str(log)})
            if result.returncode:
                print(json.dumps({'build': results, 'qualified': False}))
                return result.returncode
    finally:
        for path, before in original.items():
            after = path.read_bytes() if path.exists() else None
            if after != before:
                if before is None:
                    path.unlink(missing_ok=True)
                else:
                    path.write_bytes(before)
    binding = {'sources': {path: digest(ROOT / path) for path in SOURCES}, 'artifacts': artifacts()}
    manifest = evidence / 'wallet-sponsored-candidate.json'
    manifest.write_text(json.dumps(binding, sort_keys=True))
    manifest.chmod(0o600)
    print(json.dumps({'build': results, 'candidate_manifest': str(manifest), 'qualified': False}))
    return 0


BROWSER = r'''
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {createRequire} from 'node:module';
const require=createRequire(process.env.WALLET_APP_PACKAGE);
const {chromium}=require('@playwright/test');
const config=JSON.parse(fs.readFileSync(process.env.WALLET_SPONSORED_CONFIG,'utf8'));
const browser=await chromium.launch({headless:true});
try {
 for(const name of JSON.parse(process.env.WALLET_SPONSORED_CASES)) {
  const testcase=config.cases[name];
  assert.ok(testcase&&Array.isArray(testcase.actions)&&testcase.actions.length>0,`genuine scenario missing: ${name}`);
  const context=await browser.newContext({storageState:config.storage_state});
  const page=await context.newPage();const signatures=[];const submits=[];let statuses=0;
  page.on('request',request=>{
   const path=new URL(request.url()).pathname;
   if(path==='/v1/wallet/sign-digest')signatures.push(request.postDataJSON()?.construction);
   if(path==='/v1/wallet/sponsored/submit')submits.push(request.postDataJSON());
   if(path==='/v1/wallet/sponsored/status')statuses++;
   assert.ok(!['/v1/wallet/sign','/v1/wallet/sign-message'].includes(path),'generic signing fallback refused');
  });
  await page.goto(new URL(testcase.path,config.app_url).toString());
  for(const action of testcase.actions){
   assert.ok(action&&typeof action==='object');
   if(action.kind==='fill')await page.locator(action.selector).fill(action.value);
   else if(action.kind==='click')await page.locator(action.selector).click();
   else if(action.kind==='reload')await page.reload();
   else if(action.kind==='offline')await context.setOffline(action.value===true);
   else if(action.kind==='phase')await page.locator('[data-role="sponsored-phase"]').filter({hasText:action.value}).waitFor({timeout:30000});
   else if(action.kind==='reason')await page.locator('[data-role="sponsored-reason"]').filter({hasText:action.value}).waitFor({timeout:30000});
   else if(action.kind==='wait_expiry'){
    const text=await page.locator('[data-role="sponsored-construction"]').innerText();
    const deadline=/expires ([0-9]+)/u.exec(text);assert.ok(deadline,'real quote expiry absent');
    const wait=Number(BigInt(deadline[1])*1000n-BigInt(Date.now()))+100;
    assert.ok(wait>=0&&wait<=60000,'isolated fixture quote must expire within one minute');await page.waitForTimeout(wait);
   } else throw new Error('unrecognized fixture action');
  }
  const phase=await page.locator('[data-role="sponsored-phase"]').innerText();
  if(['cancel_consent','expired_quote','account_changed','chain_changed','calldata_changed','station_unavailable'].includes(name)){
   assert.ok(['cancelled','refused','unknown'].includes(phase));assert.equal(submits.length,0,'refused construction reached submit');
   if(name!=='calldata_changed')assert.equal(signatures.length,0,'refused construction reached signer');
   const reason=await page.locator('[data-role="sponsored-reason"]').innerText();
   assert.ok(/cancelled|expired_quote|session_changed|construction_changed|unavailable|safe_first_delegation/u.test(reason),'typed refusal absent');
  }else if(name!=='fee_paths_distinct'){
   assert.ok(['submitted','confirmed','recovering'].includes(phase));
   assert.equal(signatures.filter(c=>c?.kind==='sponsored_batch').length,1,'batch was not signed exactly once');
   assert.equal(signatures.filter(c=>c?.kind==='eip7702_authorization').length,1,'safe authorization was not signed exactly once');
   for(const construction of signatures){assert.ok(construction.chainId&&construction.nonce);if(construction.kind==='sponsored_batch')assert.ok(construction.account&&construction.calls?.length&&construction.quote?.deadline);else assert.ok(construction.address);}
   assert.ok(submits.length>=1,'production adapter never received construction');
   assert.equal(new Set(submits.map(body=>JSON.stringify(body))).size,1,'retry changed exact signed submission');
   for(const body of submits)assert.ok(body.authorization&&body.construction&&body.account_signature&&body.relayer_signature&&body.quote_decimals===6);
   if(['retry','lost_response','reload'].includes(name))assert.ok(statuses>0,'recovery did not consult durable station status');
   assert.ok(await page.locator('[data-role="sponsored-sid"]').innerText());
   assert.match(await page.locator('[data-role="sponsored-pax"]').innerText(),/PAX/u);
  }else{
   assert.equal(submits.length,0);assert.equal(signatures.length,0);
   assert.equal(await page.locator('[data-fee-choice="pax_gas"]').count(),1);
   assert.equal(await page.locator('[data-fee-choice="sid_native"]').count(),1);
  }
  await context.close();console.log(JSON.stringify({case:name,passed:true}));
 }
}finally{await browser.close();}
'''


def verify():
    evidence = evidence_dir()
    manifest = json.loads(private(evidence / 'wallet-sponsored-candidate.json').read_text())
    require(manifest['sources'] == {path: digest(ROOT / path) for path in SOURCES}, 'candidate source changed after build')
    require(manifest['artifacts'] == artifacts(), 'candidate compiled artifacts changed after build')
    bundle = private(Path(os.environ['WALLET_SPONSORED_FIXTURE_BUNDLE']).resolve())
    require(bundle.is_dir(), 'genuine protected sponsored browser fixture missing')
    require(not any(p.name == '.env' or p.name.startswith('.env.') for p in bundle.rglob('*')), 'fixture may not carry env files')
    config_path = private(bundle / 'wallet-sponsored-fixture.json')
    config = json.loads(config_path.read_text())
    require(config.get('version') == 1 and config.get('isolated') is True and config.get('live_funded_transactions') is False,
            'disposable isolated real-process fixture required; live funded transactions prohibited')
    require(set(config['cases']) == set(CASES), 'all consent/refusal/recovery cases required')
    for key in ('app_url', 'gateway_url', 'station_url', 'rpc_url'):
        local_url(config[key])
    require(set(service['kind'] for service in config['services']) == {'wallet', 'gateway', 'station', 'chain', 'attestor', 'supabase'}, 'genuine whole-boundary processes required')
    for service in config['services']:
        os.kill(int(service['pid']), 0)
        executable = Path(os.readlink(f"/proc/{int(service['pid'])}/exe"))
        require(digest(executable) == service['executable_sha256'], 'genuine service artifact mismatch')
    private(Path(config['storage_state']))
    request = urllib.request.Request(config['rpc_url'], data=json.dumps({'jsonrpc':'2.0','id':1,'method':'eth_getBalance','params':[config['account'],'latest']}).encode(), headers={'content-type':'application/json'})
    with urllib.request.urlopen(request, timeout=10) as response:
        balance = json.load(response)
    require(balance.get('jsonrpc') == '2.0' and balance.get('id') == 1 and balance.get('result') == '0x0', 'fixture account must have no live native funding')
    script = evidence / 'wallet-sponsored-browser.mjs'
    script.write_text(BROWSER)
    script.chmod(0o600)
    env = os.environ.copy()
    env.update({'WALLET_APP_PACKAGE':str(ROOT / 'human/apps/wallet/package.json'), 'WALLET_SPONSORED_CONFIG':str(config_path), 'WALLET_SPONSORED_CASES':json.dumps(CASES)})
    log = evidence / 'wallet-sponsored-browser.log'
    with log.open('wb') as output:
        log.chmod(0o600)
        result = subprocess.run([str(NODE), str(script)], cwd=ROOT, env=env, stdout=output, stderr=subprocess.STDOUT, timeout=840)
    print(json.dumps({'exit_code':result.returncode,'log_path':str(log),'skipped':0}))
    return result.returncode


if __name__ == '__main__':
    try:
        require(sys.argv[1:] in ([], ['--build']), 'only --build or focused verification is supported')
        sys.exit(build() if sys.argv[1:] else verify())
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(3)
