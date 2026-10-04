import fs from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { createHash } from 'node:crypto';

const fail = message => { throw new Error(message); };
const addressPattern = /^0x[0-9a-fA-F]{40}$/;
const hashPattern = /^0x[0-9a-fA-F]{64}$/;
let rpcSequence = 0;
let context;
let currentPhase = 'manifest';
let extensionHome;

function localUrl(value) {
  const url = new URL(value);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password
      || !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)) fail('non-isolated URL');
  return url.toString();
}

async function absoluteExisting(value, directory = false) {
  if (typeof value !== 'string' || !path.isAbsolute(value)) fail('absolute artifact path required');
  const stat = await fs.lstat(value);
  if (stat.isSymbolicLink() || (directory ? !stat.isDirectory() : !stat.isFile())) fail('invalid artifact');
  return value;
}

async function rpc(url, method, params) {
  const id = ++rpcSequence;
  const response = await fetch(url, {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id, method, params }),
    signal: AbortSignal.timeout(20_000),
  });
  if (!response.ok) fail('real RPC HTTP refusal');
  const text = await response.text();
  if (text.length > 1_048_576) fail('RPC response bound exceeded');
  const result = JSON.parse(text);
  if (result.jsonrpc !== '2.0' || result.id !== id || result.error || !('result' in result)) {
    fail(`real RPC refused ${method}`);
  }
  return result.result;
}

async function receipt(url, hash, milliseconds = 30_000) {
  const deadline = Date.now() + milliseconds;
  do {
    const result = await rpc(url, 'eth_getTransactionReceipt', [hash]);
    if (result !== null) {
      if (result.transactionHash?.toLowerCase() !== hash.toLowerCase()
          || !['0x0', '0x1'].includes(result.status)) fail('malformed real receipt');
      return result;
    }
    await new Promise(resolve => setTimeout(resolve, 200));
  } while (Date.now() < deadline);
  return null;
}

function locator(page, step) {
  if (typeof step.selector === 'string' && step.selector.length > 0 && step.selector.length <= 1_024) {
    return page.locator(step.selector);
  }
  if (typeof step.role === 'string' && typeof step.name === 'string') {
    return page.getByRole(step.role, { name: step.name, exact: true });
  }
  fail('official extension locator required');
}

function officialActions() {
  const click = id => ({ action: 'click', selector: `[data-testid="${id}"]` });
  const fill = (id, valueFrom) => ({ action: 'fill', selector: `[data-testid="${id}"]`, valueFrom });
  const network = id => [fill('network-form-network-name', `network${id}`),
    fill('network-form-chain-id', `chain${id}`),
    { action: 'fill', selector: '#nativeCurrency', valueFrom: 'symbol' },
    click('test-add-rpc-drop-down'), { action: 'click', role: 'button', name: 'Add RPC URL' },
    fill('rpc-url-input-test', `rpc${id}`), fill('rpc-name-input-test', `network${id}`),
    click('page-container-footer-next'), click('page-container-footer-next')];
  const switchNetwork = id => [click('dapp-connection-control-bar__network-button'),
    click(`Paxeer isolated ${id}`)];
  return {
    onboarding: [click('onboarding-create-wallet'), click('onboarding-create-with-srp-button'),
      fill('create-password-new-input', 'password'), fill('create-password-confirm-input', 'password'),
      click('create-password-terms'), click('create-password-submit'), click('passkey-maybe-later-button'),
      click('recovery-phrase-remind-later'),
      { action: 'uncheckIfChecked', selector: '[data-testid="metametrics-checkbox"]' },
      click('metametrics-i-agree'), click('onboarding-complete-done')],
    add125: network(125), add126: network(126),
    connectRejection: [click('cancel-btn')], connectApproval: [click('confirm-btn')],
    reconnectApproval: [click('confirm-btn')],
    switch125: switchNetwork(125), switch126: switchNetwork(126),
    lock: [click('account-options-menu-button'), click('global-menu-lock')],
    unlock: [fill('unlock-password', 'password'), click('unlock-submit')],
    transactionApproval: [click('confirm-footer-button')],
  };
}

async function extensionAction(manifest, name, timeout = 20_000) {
  currentPhase = name;
  if (name === 'add125' || name === 'add126') {
    await extensionHome.goto(`chrome-extension://${manifest.extensionId}/home.html#/networks?view=add`);
  } else if (name.startsWith('switch') || name === 'lock') {
    await extensionHome.goto(`chrome-extension://${manifest.extensionId}/home.html#`);
  }
  const steps = manifest.extensionActions[name];
  const values = {
    password: manifest.walletPassword, rpc125: manifest.rpc125, rpc126: manifest.rpc126,
    chain125: '125', chain126: '126', network125: 'Paxeer isolated 125',
    network126: 'Paxeer isolated 126', symbol: 'PAX',
  };
  for (let index = 0; index < steps.length; index += 1) {
    currentPhase = `${name}.step${index}`;
    const step = steps[index];
    const deadline = Date.now() + timeout;
    let selected;
    let selectedPage;
    while (!selected && Date.now() < deadline) {
      for (const page of context.pages()) {
        if (!page.url().startsWith(`chrome-extension://${manifest.extensionId}/`)
            || (step.urlPath && !new URL(page.url()).pathname.startsWith(step.urlPath))) continue;
        const candidate = locator(page, step);
        if (await candidate.count() === 1 && await candidate.isVisible()) {
          selected = candidate;
          selectedPage = page;
          break;
        }
      }
      if (!selected) await new Promise(resolve => setTimeout(resolve, 100));
    }
    if (!selected) fail(`official extension UI unavailable: ${name} step ${index}`);
    await selectedPage.bringToFront();
    if (step.action === 'click') await selected.click({ timeout });
    else if (step.action === 'check') await selected.check({ timeout });
    else if (step.action === 'fill') {
      const value = step.valueFrom ? values[step.valueFrom] : step.value;
      if (typeof value !== 'string') fail('undeclared extension input');
      await selected.fill(value, { timeout });
    } else if (step.action === 'press') {
      if (!['Enter', 'Tab', 'Escape'].includes(step.key)) fail('unsupported extension key');
      await selected.press(step.key, { timeout });
    } else if (step.action === 'uncheckIfChecked') {
      if (await selected.getAttribute('data-checked') === 'true') await selected.click({ timeout });
    } else if (step.action !== 'waitVisible') fail('unsupported extension action');
  }
}

async function state(app) {
  const read = async id => (await app.getByTestId(id).textContent({ timeout: 5_000 }))?.trim() ?? '';
  return {
    status: await read('wallet-status'), address: await read('wallet-address'),
    mode: await read('wallet-mode'), chain: await read('wallet-chain'),
    chainStatus: await read('wallet-chain-status'), operationError: await read('operation-error'),
    transactionHash: await read('transaction-hash'), embeddedConfiguration: await read('embedded-config'),
  };
}

async function until(callback, milliseconds = 20_000) {
  const deadline = Date.now() + milliseconds;
  do {
    const value = await callback();
    if (value) return value;
    await new Promise(resolve => setTimeout(resolve, 100));
  } while (Date.now() < deadline);
  fail('actual browser observation timed out');
}

async function connect(app, manifest, phase) {
  await app.bringToFront();
  const matching = app.locator('[data-testid="injected-connect"][data-provider-rdns="io.metamask"]');
  if (await matching.count() !== 1) fail('official injected provider discovery is ambiguous');
  await matching.click();
  if (phase === 'reconnectApproval') {
    const outcome = await until(async () => {
      const observed = await state(app);
      if (observed.status === 'ready' && addressPattern.test(observed.address)) return 'existing-permission';
      for (const page of context.pages()) {
        if (page.url().startsWith(`chrome-extension://${manifest.extensionId}/`)
            && await page.getByTestId('parent-selector-connect-page').isVisible()) return 'approval';
      }
      return null;
    });
    if (outcome === 'existing-permission') return;
  }
  await extensionAction(manifest, phase);
}

async function nativeSend(app, manifest, chains, staleConfirmation) {
  await app.getByTestId('wallet-clear-transfer').click();
  await app.getByTestId('send-recipient').fill(manifest.recipient);
  await app.getByTestId('send-amount').fill(manifest.amount);
  await app.getByTestId('wallet-read-chain').click();
  await until(async () => (await state(app)).chainStatus === 'observed');
  const before = await state(app);
  await app.bringToFront();
  await app.getByTestId('confirmation-host').getByRole('button', { name: 'Send native PAX', exact: true }).click();
  const confirmation = app.getByRole('dialog', { name: 'Confirm send transaction', exact: true });
  await confirmation.waitFor({ state: 'visible', timeout: 10_000 });
  if (staleConfirmation) {
    await extensionAction(manifest, 'switch126');
    await app.bringToFront();
    await app.getByTestId('wallet-read-chain').click();
    await until(async () => {
      const observed = await state(app);
      return ['126', '0x7e'].includes(observed.chain) && observed;
    });
    if (!await confirmation.isVisible()) {
      return { outcome: 'confirmation-invalidated', before, after: await state(app), receipts: [] };
    }
  }
  await confirmation.getByRole('button', { name: 'Confirm send', exact: true }).click();
  const submission = await until(async () => {
    const observed = await state(app);
    if (observed.operationError && observed.operationError !== before.operationError) {
      return { refused: true, observed };
    }
    if (hashPattern.test(observed.transactionHash) && observed.transactionHash !== before.transactionHash) {
      return { hash: observed.transactionHash, observed };
    }
    for (const page of context.pages()) {
      if (page.url().startsWith(`chrome-extension://${manifest.extensionId}/`)
          && await page.getByTestId('confirm-footer-button').isVisible()) return { extensionPending: true };
    }
    return null;
  });
  if (submission.extensionPending) await extensionAction(manifest, 'transactionApproval');
  const after = submission.refused ? submission.observed : await until(async () => {
    const observed = await state(app);
    if (observed.operationError && observed.operationError !== before.operationError) return observed;
    return hashPattern.test(observed.transactionHash) && observed.transactionHash !== before.transactionHash
      ? observed : null;
  });
  if (!hashPattern.test(after.transactionHash) || after.transactionHash === before.transactionHash) {
    if (!after.operationError) fail('native submission has no observed outcome');
    return { outcome: 'refused', before, after, receipts: [] };
  }
  const receipts = [];
  const observations = await Promise.all(chains.map(async chain => ({
    chainId: chain.id, receipt: await receipt(chain.url, after.transactionHash),
  })));
  for (const observed of observations) {
    if (!observed.receipt) continue;
    if (observed.receipt.from?.toLowerCase() !== before.address.toLowerCase()
        || observed.receipt.to?.toLowerCase() !== manifest.recipient.toLowerCase()) fail('receipt account mismatch');
    const chain = chains.find(item => item.id === observed.chainId);
    const transaction = await rpc(chain.url, 'eth_getTransactionByHash', [after.transactionHash]);
    if (!transaction || BigInt(transaction.value) !== BigInt(manifest.valueWei)
        || BigInt(transaction.chainId) !== BigInt(observed.chainId)) fail('native transaction binding mismatch');
    receipts.push({ chainId: observed.chainId, transactionHash: observed.receipt.transactionHash,
      status: observed.receipt.status, blockNumber: observed.receipt.blockNumber });
  }
  if (receipts.length !== 1 || receipts[0].status !== '0x1') fail('native receipt unavailable or unsuccessful');
  return { outcome: 'mined', before, after, receipts };
}

async function main() {
  if (process.argv.length !== 4 || process.argv[2] !== '--manifest') fail('use --manifest ABS_PRIVATE_FILE');
  const manifestPath = await absoluteExisting(process.argv[3]);
  const manifestStat = await fs.stat(manifestPath);
  if ((manifestStat.mode & 0o077) !== 0 || manifestStat.size > 262_144) fail('private bounded manifest required');
  const source = JSON.parse(await fs.readFile(manifestPath, 'utf8'));
  if (source.schema !== 'paxeer-x.wallet-injected-runtime.v1'
      || !addressPattern.test(source.recipient) || !/^[1-9][0-9]*$/.test(source.value_wei)
      || !source.source_hashes || typeof source.source_hashes !== 'object') fail('invalid fixture contract');
  for (const [file, digest] of Object.entries(source.source_hashes)) {
    if (!/^[0-9a-f]{64}$/.test(digest) || createHash('sha256').update(await fs.readFile(file)).digest('hex') !== digest) {
      fail('fixture source hash mismatch');
    }
  }
  const passwordPath = await absoluteExisting(source.password_file);
  const passwordStat = await fs.stat(passwordPath);
  if ((passwordStat.mode & 0o077) !== 0 || passwordStat.size > 1_024) fail('private password file required');
  const walletPassword = (await fs.readFile(passwordPath, 'utf8')).replace(/\n$/, '');
  if (walletPassword.length < 12) fail('disposable password too short');
  const wei = BigInt(source.value_wei);
  const fraction = (wei % 10n ** 18n).toString().padStart(18, '0').replace(/0+$/, '');
  const amount = `${wei / 10n ** 18n}${fraction ? '.' + fraction : ''}`;
  const manifest = { rpc125: source.rpc125, rpc126: source.rpc126, appUrl: source.app_url,
    extensionPath: source.extension_path, extensionVersion: source.extension_version,
    playwrightModule: source.playwright_module, browserExecutable: source.chromium_executable,
    profileDir: source.profile_dir, outputDir: source.output_dir, recipient: source.recipient,
    amount, valueWei: source.value_wei, walletPassword, playwrightVersion: '1.62.0' };
  manifest.rpc125 = localUrl(manifest.rpc125);
  manifest.rpc126 = localUrl(manifest.rpc126);
  const appUrl = localUrl(manifest.appUrl);
  const extensionPath = await absoluteExisting(manifest.extensionPath, true);
  const browserExecutable = manifest.browserExecutable
    ? await absoluteExisting(manifest.browserExecutable) : undefined;
  const playwrightModule = await absoluteExisting(manifest.playwrightModule);
  const profileDir = await absoluteExisting(manifest.profileDir, true);
  const outputDir = await absoluteExisting(manifest.outputDir, true);
  for (const directory of [profileDir, outputDir]) {
    if ((await fs.stat(directory)).mode & 0o077) fail('private fixture directory required');
  }
  if ((await fs.readdir(profileDir)).length) fail('persistent profile must be fresh and disposable');
  const extensionMetadata = JSON.parse(await fs.readFile(path.join(extensionPath, 'manifest.json'), 'utf8'));
  if (manifest.extensionVersion !== '13.50.0' || extensionMetadata.version !== '13.50.0.0') {
    fail('official pinned release/Chromium extension version mismatch');
  }
  if (manifest.playwrightVersion !== '1.62.0') fail('repository Playwright pin mismatch');
  const runtimePackage = JSON.parse(await fs.readFile(path.join(path.dirname(playwrightModule), 'package.json'), 'utf8'));
  if (runtimePackage.version !== manifest.playwrightVersion) fail('actual Playwright artifact version mismatch');
  const chains = [{ id: 125, url: manifest.rpc125 }, { id: 126, url: manifest.rpc126 }];
  for (const chain of chains) {
    if (BigInt(await rpc(chain.url, 'eth_chainId', [])) !== BigInt(chain.id)) fail('real isolated chain mismatch');
  }
  const { chromium } = await import(pathToFileURL(playwrightModule).href);
  currentPhase = 'browser';
  context = await chromium.launchPersistentContext(profileDir, {
    executablePath: browserExecutable, channel: 'chromium', headless: true, timeout: 30_000,
    args: [`--disable-extensions-except=${extensionPath}`, `--load-extension=${extensionPath}`,
      '--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE localhost, EXCLUDE 127.0.0.1'],
  });
  const worker = context.serviceWorkers().find(item => item.url().startsWith('chrome-extension://'))
    ?? await context.waitForEvent('serviceworker', { timeout: 20_000 });
  manifest.extensionId = new URL(worker.url()).hostname;
  if (!/^[a-p]{32}$/.test(manifest.extensionId)) fail('official extension ID unavailable');
  extensionHome = await context.newPage();
  await extensionHome.goto(`chrome-extension://${manifest.extensionId}/home.html`);
  manifest.extensionActions = officialActions();
  await extensionAction(manifest, 'onboarding');
  await extensionAction(manifest, 'add125');
  await extensionAction(manifest, 'add126');
  await context.addInitScript(({ recipient, amount }) => {
    window.__PAXEER_INJECTED_FIXTURE__ = { recipient, amount };
  }, { recipient: manifest.recipient, amount: manifest.amount });
  const app = await context.newPage();
  await app.goto(appUrl, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  await app.getByTestId('injected-discovery').waitFor({ state: 'visible', timeout: 20_000 });
  await until(async () => await app.getByTestId('injected-connect').count() > 0);
  const initial = await state(app);
  if (initial.embeddedConfiguration !== 'unavailable'
      || await app.getByTestId('bootstrap-config-mode').textContent() !== 'default') {
    fail('missing embedded configuration was not exercised');
  }
  await connect(app, manifest, 'connectRejection');
  const rejected = await until(async () => {
    const observed = await state(app);
    return observed.operationError && observed;
  });
  if (addressPattern.test(rejected.address)) fail('rejected connection adopted an account');
  await connect(app, manifest, 'connectApproval');
  const connected = await until(async () => {
    const observed = await state(app);
    return addressPattern.test(observed.address) && observed;
  });
  await app.bringToFront();
  await extensionAction(manifest, 'switch125');
  await app.bringToFront();
  await app.getByTestId('wallet-read-chain').click();
  await until(async () => (await state(app)).chain === '125');
  for (const chain of chains) {
    const accounts = await rpc(chain.url, 'eth_accounts', []);
    if (!Array.isArray(accounts) || !addressPattern.test(accounts[0])) fail('isolated funding account unavailable');
    const fundingHash = await rpc(chain.url, 'eth_sendTransaction', [{
      from: accounts[0], to: connected.address, value: '0xde0b6b3a7640000',
    }]);
    if (!hashPattern.test(fundingHash) || (await receipt(chain.url, fundingHash))?.status !== '0x1') {
      fail('genuine disposable account funding failed');
    }
  }
  await app.getByTestId('wallet-refresh').click();
  const correctChain = await nativeSend(app, manifest, chains, false);
  if (correctChain.outcome !== 'mined' || correctChain.receipts[0].chainId !== 125) {
    fail('genuine chain125 baseline transaction unavailable');
  }
  const baseline = await nativeSend(app, manifest, chains, true);
  await extensionAction(manifest, 'switch125');
  await app.bringToFront();
  await app.reload({ waitUntil: 'domcontentloaded' });
  const reloaded = await until(async () => {
    const observed = await state(app);
    return !['loading', 'connecting'].includes(observed.status) && observed;
  });
  await extensionAction(manifest, 'lock');
  await extensionHome.getByTestId('unlock-password').waitFor({ state: 'visible', timeout: 10_000 });
  await app.bringToFront();
  await app.getByTestId('wallet-read-chain').click();
  const locked = await until(async () => {
    const observed = await state(app);
    return observed.chainStatus !== 'loading' && observed;
  });
  await app.getByTestId('wallet-sign-out').click();
  const disconnected = await until(async () => {
    const observed = await state(app);
    return observed.status === 'signed-out' && !observed.address && observed;
  });
  await extensionAction(manifest, 'unlock');
  await connect(app, manifest, 'reconnectApproval');
  const reconnected = await until(async () => {
    const observed = await state(app);
    return observed.address.toLowerCase() === connected.address.toLowerCase() && observed;
  });
  const cases = { initial, rejected, connected, correctChain, baseline,
    reloaded, locked, disconnected, reconnected };
  const report = {
    schema: 'paxeer-x.wallet-injected-baseline.v1', fixture_ready: true,
    completed_cases: Object.keys(cases).length, skipped_cases: 0, source_hashes: source.source_hashes,
    genuineExtension: { id: manifest.extensionId, version: manifest.extensionVersion },
    playwrightVersion: manifest.playwrightVersion, chains: [125, 126],
    cases,
    baseline_wrong_chain_observed: baseline.receipts.some(item => item.chainId === 126),
  };
  await fs.writeFile(path.join(outputDir, 'baseline.json'), JSON.stringify(report, null, 2) + '\n',
    { flag: 'wx', mode: 0o600 });
  process.stdout.write(JSON.stringify({ fixture_ready: true,
    baseline_wrong_chain_observed: report.baseline_wrong_chain_observed }) + '\n');
}

try {
  await main();
} catch {
  process.stderr.write(`wallet-injected-fixture: ${currentPhase}: genuine fixture operation failed\n`);
  process.exitCode = 1;
} finally {
  if (context) await context.close().catch(() => {});
}
