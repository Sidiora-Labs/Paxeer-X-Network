#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[3]
NODE = Path('/root/lx-toolchains/node24/bin/node')
EVIDENCE = Path(os.environ.get('WALLET_SUPPORTED_DEMO_EVIDENCE_ROOT', '/root/lx-ops/paxeer-x-integration-2026-10-03/task-14.7'))
MANIFEST = EVIDENCE / 'candidate.json'
DEMO = ROOT / 'human/wallet/demo'
SDK = ROOT / 'human/wallet/sdk'


def require(value, message):
    if not value:
        raise RuntimeError(message)


def protected(path):
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and not path.is_symlink() and info.st_uid == os.getuid()
            and info.st_nlink == 1 and info.st_mode & 0o077 == 0, 'protected ordinary fixture required')
    return json.loads(path.read_text())


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def sources():
    paths = []
    for folder in (SDK / 'src', DEMO / 'src'):
        paths.extend(path for path in folder.rglob('*') if path.is_file())
    paths.extend((SDK / 'package.json', SDK / 'tsconfig.json', DEMO / 'package.json',
                  DEMO / 'next.config.mjs', ROOT / 'tools/paxeer-x/route-catalogue.json',
                  Path(__file__).resolve()))
    return {str(path.relative_to(ROOT)): digest(path) for path in sorted(paths)}


def artifacts():
    paths = []
    for folder in (SDK / 'dist', DEMO / '.next/static', DEMO / '.next/server'):
        require(folder.is_dir(), 'actual SDK and production demo artifacts required')
        paths.extend(path for path in folder.rglob('*') if path.is_file())
    paths.append(DEMO / '.next/BUILD_ID')
    return {str(path.relative_to(ROOT)): digest(path) for path in sorted(paths)}


def run(argv, name, seconds, cwd=ROOT, environment=None):
    with (EVIDENCE / name).open('wb') as output:
        result = subprocess.run(argv, cwd=cwd, env=environment, stdout=output,
                                stderr=subprocess.STDOUT, timeout=seconds)
    print(json.dumps({'exit_code': result.returncode, 'log_path': str(EVIDENCE / name)}), flush=True)
    return result.returncode


def loopback(value):
    url = urlparse(value)
    require(url.scheme == 'http' and url.hostname in ('localhost', '127.0.0.1', '::1')
            and url.port and not url.username and not url.password, 'isolated loopback fixture origin required')
    return url


CONTRACT = r'''
import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
const sdk = await import(pathToFileURL(process.env.SUPPORTED_SDK_INDEX).href);
assert.equal(typeof sdk.FundedUnsupportedError, 'function');
for (const [method,args] of [['listFundedTiers',[]],['getFundedSelf',[]],['provisionFundedAccount',[]],
  ['signFundedTransaction',[{}]],['sendFundedTransaction',[{}]],['signFundedMessage',['retired']]]) {
  await assert.rejects(sdk.PaxeerWallet.prototype[method](...args), error =>
    error instanceof sdk.FundedUnsupportedError && error.code === 'FUNDED_UNSUPPORTED' && error.status === 410);
}
for (const name of ['PaxeerProvider','WalletInterface','discoverProviders','HumanClient','KernelAvailability','decodeCustodyAuthorization']) {
  assert.equal(typeof sdk[name], 'function');
}
console.log('Six retired API calls refused locally; supported production exports present.');
'''

BROWSER = r'''
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {pathToFileURL} from 'node:url';
const fixture=JSON.parse(fs.readFileSync(process.env.SUPPORTED_DEMO_FIXTURE,'utf8'));
const {chromium}=await import(pathToFileURL(process.env.SUPPORTED_PLAYWRIGHT).href);
const browser=await chromium.connectOverCDP(fixture.browser_cdp_url);
const context=browser.contexts()[fixture.browser_context_index];
assert(context, 'Dedicated real extension browser context required');
const page=await context.newPage();
const requests=[];
page.on('request',request=>requests.push(request.url()));
const result=()=>page.locator('[data-wallet-result]');
async function answer(selector) {
  await page.locator(selector).click();
  if (fixture.injected_confirmation) {
    const approval=fixture.injected_confirmation;
    const until=Date.now()+15000;
    while(Date.now()<until) {
      const popup=context.pages().find(p=>p!==page&&p.url().startsWith('chrome-extension://'+approval.extension_id+'/'));
      if(popup) {
        const button=popup.locator(approval.selector);
        if(await button.isVisible()) { await button.click(); break; }
      }
      if(await result().getAttribute('role')==='alert') break;
      await new Promise(resolve=>setTimeout(resolve,100));
    }
  }
  await page.waitForFunction(()=>!document.querySelector('[data-wallet-accounts]')?.disabled);
  assert.equal(await result().getAttribute('role'),'status','Actual supported action must succeed');
  return result().innerText();
}
page.on('dialog',async dialog=> {
  assert(fixture.approved_signing===true,'Explicit genuine qualification consent required');
  await dialog.accept();
});
async function absentFunded() {
  assert.equal(await page.getByText('Funded Account',{exact:true}).count(),0);
  assert.equal(await page.getByText('Funded',{exact:true}).count(),0);
  assert(!requests.some(url=>new URL(url).pathname.startsWith('/v1/funded/')),'Removed route must never be fetched');
}
try {
  await page.goto(fixture.demo_origin,{waitUntil:'networkidle'});
  await page.locator('[data-wallet-connect]').first().click();
  await absentFunded();
  await page.locator('[data-wallet-embedded]').click();
  assert(await page.getByLabel('Email',{exact:true}).isVisible(),'Fresh real owner sign-in required');
  await page.getByLabel('Email',{exact:true}).fill(fixture.owner_email);
  const sent=Date.now();
  await page.getByRole('button',{name:'Send magic link',exact:true}).click();
  await page.getByRole('heading',{name:'Check your email',exact:true}).waitFor();
  let link;
  const deadline=Date.now()+45000;
  while(Date.now()<deadline) {
    try {
      const info=fs.lstatSync(fixture.delivered_magic_link_file);
      assert(!info.isSymbolicLink()&&(info.mode&0o077)===0&&info.uid===process.getuid());
      const delivered=JSON.parse(fs.readFileSync(fixture.delivered_magic_link_file,'utf8'));
      if(info.mtimeMs>=sent&&delivered.recipient===fixture.owner_email) {
        assert.equal(new URL(delivered.url).origin,new URL(fixture.supabase_url).origin);
        link=delivered.url; break;
      }
    } catch {}
    await new Promise(resolve=>setTimeout(resolve,250));
  }
  assert(link,'Genuine newly delivered Supabase magic link required');
  await page.goto(link,{waitUntil:'networkidle'});
  await page.locator('[data-wallet-connect]').first().click();
  await page.locator('[data-wallet-embedded]').click();
  await page.locator('[data-wallet-provision]').waitFor();
  await page.locator('[data-wallet-provision]').click();
  await page.locator('[data-supported-wallet="embedded"]').waitFor();
  const embedded=await answer('[data-wallet-accounts]');
  assert(/^0x[0-9a-fA-F]{40}$/.test(embedded),'Real provisioned account required');
  await page.getByLabel('Transaction recipient',{exact:true}).fill(fixture.transaction.to);
  await page.getByLabel('Amount in wei',{exact:true}).fill(fixture.transaction.value);
  assert(/^0x[0-9a-fA-F]{64}$/.test(await answer('[data-wallet-send]')),'Actual transaction hash required');
  await page.getByLabel('Typed data JSON',{exact:true}).fill(JSON.stringify(fixture.typed_data));
  assert(/^0x[0-9a-fA-F]{130}$/.test(await answer('[data-wallet-typed]')),'Actual typed-data signature required');
  const changed=structuredClone(fixture.typed_data);changed.domain.chainId=126;
  await page.getByLabel('Typed data JSON',{exact:true}).fill(JSON.stringify(changed));
  await page.locator('[data-wallet-typed]').click();
  await page.waitForFunction(()=>document.querySelector('[data-wallet-result]')?.getAttribute('role')==='alert');
  await page.getByLabel('Amount in wei',{exact:true}).fill(fixture.custody.value);
  await page.getByLabel('Canonical calldata',{exact:true}).fill(fixture.custody.data);
  await answer('[data-custody-prepare]');
  const retained=await page.locator('[data-retained-custody]').innerText();
  assert(/^0x[0-9a-fA-F]+$/.test(retained));
  assert(/^0x[0-9a-fA-F]{130}$/.test(await answer('[data-custody-sign]')),'Actual custody approval required');
  await page.reload({waitUntil:'networkidle'});
  await page.locator('[data-wallet-connect]').first().click();
  await page.locator('[data-wallet-embedded]').click();
  await answer('[data-wallet-accounts]');
  assert.equal(await page.locator('[data-retained-custody]').innerText(),retained,'Reload must preserve exact custody bytes');
  await answer('[data-custody-recover]');
  assert(/^0x[0-9a-fA-F]{64}$/.test(await answer('[data-custody-send]')));
  const status=JSON.parse(await answer('[data-custody-status]'));
  assert(['pending','confirmed','reverted'].includes(status.status));
  await page.getByLabel('Kernel intent JSON',{exact:true}).fill(JSON.stringify(fixture.plan));
  const planned=JSON.parse(await answer('[data-kernel-plan]'));
  assert(planned.plan_digest&&Array.isArray(planned.legs)&&planned.legs.length,'Actual kernel plan required; unavailable does not qualify');
  await page.getByRole('button',{name:'Wallet choices',exact:false}).click();
  const injected=page.locator('[data-wallet-injected]').filter({hasText:fixture.injected_provider_name});
  assert.equal(await injected.count(),1,'Actual EIP-6963 extension provider required');
  await injected.click();
  await answer('[data-wallet-accounts]');
  assert.equal(await page.locator('[data-custody-sign]').count(),0,'Injected custody must remain unavailable');
  await page.getByLabel('Transaction recipient',{exact:true}).fill(fixture.transaction.to);
  await page.getByLabel('Amount in wei',{exact:true}).fill(fixture.transaction.value);
  assert(/^0x[0-9a-fA-F]{64}$/.test(await answer('[data-wallet-send]')));
  await page.getByLabel('Typed data JSON',{exact:true}).fill(JSON.stringify(fixture.typed_data));
  assert(/^0x[0-9a-fA-F]{130}$/.test(await answer('[data-wallet-typed]')));
  await absentFunded();
  console.log('Actual sign-in, provisioning, embedded/injected transactions and typed data, custody approval/reload/recovery, kernel plan and removed-route refusals passed.');
} finally {await page.close();await browser.close();}
'''


def main():
    EVIDENCE.mkdir(parents=True, exist_ok=True, mode=0o700)
    require(EVIDENCE.stat().st_mode & 0o077 == 0, 'private evidence directory required')
    require(NODE.is_file(), 'actual Node24 tooling required')
    environment = os.environ.copy()
    environment['PATH'] = str(NODE.parent) + ':' + environment.get('PATH', '')
    if sys.argv[1:] == ['--build']:
        before = sources()
        require((ROOT / 'agent/sdk/typescript/dist/src/index.js').is_file(), 'prebuilt actual Agent SDK prerequisite required')
        require((SDK / 'node_modules/typescript/bin/tsc').is_file(), 'locked SDK TypeScript dependency required')
        require((DEMO / 'node_modules/next/dist/bin/next').is_file(), 'locked demo Next dependency required')
        code = run([str(NODE), str(SDK / 'node_modules/typescript/bin/tsc'), '-p', str(SDK / 'tsconfig.json')],
                   'build-sdk.log', 240, environment=environment)
        if code:
            return code
        code = run([str(NODE), str(DEMO / 'node_modules/next/dist/bin/next'), 'build'],
                   'build-demo.log', 330, cwd=DEMO, environment=environment)
        if code:
            return code
        require(sources() == before, 'task sources changed while compiling')
        MANIFEST.write_text(json.dumps({'sources': before, 'artifacts': artifacts()}, sort_keys=True))
        MANIFEST.chmod(0o600)
        return 0
    require(not sys.argv[1:], 'only --build or the declared verification command is supported')
    binding = protected(MANIFEST)
    require(binding['sources'] == sources() and binding['artifacts'] == artifacts(), 'whole current source and actual prebuilt artifacts must match')
    for name in ('WalletModal.tsx', 'ConnectButton.tsx', 'FundedAccountPanel.tsx'):
        text = (DEMO / 'src/components' / name).read_text()
        require('useFundedAccount' not in text and '/v1/funded/' not in text and 'Funded Account' not in text,
                'removed demo branch or route restored')
    panel = (DEMO / 'src/components/FundedAccountPanel.tsx').read_text()
    require('NEXT_PUBLIC_PAXEER_WALLET_API' in panel and 'NEXT_PUBLIC_PAXEER_HUMAN_API' not in panel
            and 'NEXT_PUBLIC_PAXEER_RPC_URL' not in panel, 'one configured unified origin required')
    catalogue = json.loads((ROOT / 'tools/paxeer-x/route-catalogue.json').read_text())
    require('/v1/funded/' not in json.dumps(catalogue), 'treasury route must remain removed')
    for path in (DEMO / '.next/static').rglob('*.js'):
        text = path.read_text()
        for name in ('SUPABASE_SERVICE_ROLE_KEY', 'AGENT_JWT_SECRET', 'ATTESTOR_SHARED_SECRET', 'API_ENCRYPTION_KEY'):
            require(name not in text, 'server credential reference must not enter the demo bundle')
    contract = EVIDENCE / 'exported-contract.mjs'
    contract.write_text(CONTRACT)
    environment['SUPPORTED_SDK_INDEX'] = str(SDK / 'dist/index.js')
    code = run([str(NODE), str(contract)], 'exported-contract.log', 60, environment=environment)
    if code:
        return code
    supplied = os.environ.get('WALLET_SUPPORTED_DEMO_FIXTURE')
    require(supplied, 'genuine isolated wallet/services/browser fixture required')
    fixture = protected(Path(supplied))
    require(fixture.get('version') == 1 and fixture.get('isolated') is True
            and fixture.get('approved_signing') is True, 'explicit approved isolated real wallet fixture required')
    for name in ('unified_origin', 'demo_origin', 'browser_cdp_url', 'supabase_url'):
        loopback(fixture[name])
    origin = loopback(fixture['unified_origin'])
    require(origin.path in ('', '/') and not origin.query and not origin.fragment, 'canonical unified origin required')
    demo = loopback(fixture['demo_origin'])
    require(demo.path in ('', '/') and demo.port != origin.port, 'independent local production-demo listener required')
    require(fixture['unified_origin'].rstrip('/') == environment.get('NEXT_PUBLIC_PAXEER_WALLET_API', '').rstrip('/'),
            'compiled public unified configuration must match the actual fixture')
    runtime = protected(fixture['source_artifact_manifest'])
    require(runtime.get('candidate') == binding, 'fixture must name this exact whole-source and prebuilt candidate')
    for relative, expected in runtime['service_artifacts'].items():
        path = ROOT / relative
        require(path.resolve().is_relative_to(ROOT) and path.is_file() and not path.is_symlink()
                and not any(part.startswith('.env') for part in path.parts), 'real source-bound fixture service required')
        require(digest(path) == expected, 'real service candidate mismatch')
    require(runtime['service_artifacts'] and fixture.get('browser_context_index') == 0, 'dedicated real fixture services and browser profile required')
    protected(fixture['delivered_magic_link_file'])
    playwright = ROOT / 'human/apps/wallet/node_modules/@playwright/test/index.mjs'
    require(playwright.is_file(), 'preinstalled actual Playwright production-browser tooling required')
    browser = EVIDENCE / 'supported-demo-browser.mjs'
    browser.write_text(BROWSER)
    environment['SUPPORTED_DEMO_FIXTURE'] = str(Path(supplied).resolve())
    environment['SUPPORTED_PLAYWRIGHT'] = str(playwright)
    with (EVIDENCE / 'demo-process.log').open('wb') as output:
        process = subprocess.Popen([str(NODE), str(DEMO / 'node_modules/next/dist/bin/next'), 'start',
                                    '--hostname', '127.0.0.1', '--port', str(demo.port)], cwd=DEMO,
                                   env=environment, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                require(process.poll() is None, 'actual prebuilt demo exited before readiness')
                try:
                    with socket.create_connection((demo.hostname, demo.port), timeout=1):
                        break
                except OSError:
                    time.sleep(0.1)
            else:
                raise RuntimeError('actual demo readiness deadline')
            return run([str(NODE), str(browser)], 'supported-flows.log', 600, environment=environment)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=10)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (RuntimeError, KeyError, ValueError, OSError, subprocess.TimeoutExpired) as error:
        EVIDENCE.mkdir(parents=True, exist_ok=True, mode=0o700)
        (EVIDENCE / 'prerequisite.log').write_text(type(error).__name__ + ': bounded prerequisite or gate unavailable\n')
        print(json.dumps({'exit_code': 78, 'qualification': 'UNQUALIFIED', 'log_path': str(EVIDENCE / 'prerequisite.log')}))
        sys.exit(78)
