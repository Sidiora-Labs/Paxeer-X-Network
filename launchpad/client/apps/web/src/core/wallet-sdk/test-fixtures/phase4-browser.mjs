import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { createRequire } from 'node:module';
const requireModule = createRequire(import.meta.url);
const sha = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
async function main() {
  const configPath = process.argv[2], output = process.argv[3];
  if (!configPath || !output) throw new Error('Explicit admitted browser configuration and output directory are required');
  const inputBytes = fs.readFileSync(configPath), config = JSON.parse(inputBytes);
  if (sha(inputBytes) !== process.env.L25_PHASE4_CONFIG_SHA256) throw new Error('Browser configuration digest mismatch');
  if (!config.playwrightModule || !config.axeSource || !config.axeSha256 || !Array.isArray(config.browserJourneys) || config.browserJourneys.length < 8) throw new Error('Real browser module, accessibility scanner and eight actual journeys required');
  const axe = fs.readFileSync(config.axeSource);
  if (sha(axe) !== config.axeSha256) throw new Error('Accessibility scanner source changed');
  const playwright = requireModule(config.playwrightModule);
  fs.mkdirSync(output, { recursive: true });
  const raw = fs.createWriteStream(path.join(output, 'browser-observations.jsonl'), { flags: 'wx' });
  const write = record => raw.write(JSON.stringify({ observedAt: new Date().toISOString(), ...record }) + '\n');
  const screenshots = [], assertions = [], assets = new Map(), errors = [], assetReads = [];
  for (const journey of config.browserJourneys) {
    if (!Number.isInteger(journey.scenario) || journey.scenario < 1 || journey.scenario > 8 || !journey.cdpUrl || !journey.applicationUrl || !Array.isArray(journey.steps) || journey.steps.length > 100) throw new Error('Bounded real journey configuration required');
    const cdp = new URL(journey.cdpUrl), origin = new URL(journey.applicationUrl);
    if (!['127.0.0.1','localhost','[::1]'].includes(cdp.hostname) || origin.protocol !== 'https:' || origin.origin !== config.applicationOrigin) throw new Error('Explicit loopback CDP and admitted HTTPS application required');
    const browser = await playwright.chromium.connectOverCDP(journey.cdpUrl);
    let networkSession;
    try {
    const contexts = browser.contexts();
    if (contexts.length !== 1) throw new Error('Exactly one real external-wallet browser profile required per CDP endpoint');
    const page = await contexts[0].newPage();
    page.on('pageerror', error => { errors.push({ scenario: journey.scenario, name: error.name }); write({ kind: 'pageerror', scenario: journey.scenario, name: error.name }); });
    page.on('request', request => {
      const url = new URL(request.url());
      if (url.origin === origin.origin && /^\/v2\/auth\//.test(url.pathname)) write({ kind: 'auth_request', scenario: journey.scenario, method: request.method(), path: url.pathname, monotonicMs: performance.now() });
    });
    page.on('response', response => {
      const request = response.request(), url = new URL(response.url());
      if (url.origin === origin.origin && /^\/v2\/auth\//.test(url.pathname)) write({ kind: 'auth_response', scenario: journey.scenario, method: request.method(), path: url.pathname, status: response.status(), monotonicMs: performance.now() });
      if (url.origin === origin.origin && request.resourceType() === 'script') assetReads.push(response.body().then(bytes => assets.set(url.href, sha(bytes))).catch(() => errors.push({ scenario: journey.scenario, name: 'script_body_unavailable' })));
    });
    const navigation = await page.goto(journey.applicationUrl, { waitUntil: 'domcontentloaded', timeout: 60000 });
    if (!navigation || !navigation.ok()) throw new Error('Actual SSR document unavailable');
    const serverDocument = await navigation.body();
    fs.writeFileSync(path.join(output, `ssr-${journey.scenario}.html`), serverDocument);
    write({ kind: 'ssr_document', scenario: journey.scenario, sha256: sha(serverDocument), status: navigation.status() });
    await page.waitForFunction(() => document.querySelector('button[aria-label^="Wallet "]') || Array.from(document.querySelectorAll('button')).some(button => button.textContent.trim() === 'Connect wallet'), { timeout: 60000 });
    const identity = { runId: `${config.runId}_${journey.scenario}`, sourceSha256: config.sourceSha256, deploymentManifestHash: config.deploymentManifestHash };
    let segment = 0;
    const begin = async () => {
      await page.waitForFunction(() => document.querySelector('button[aria-label^="Wallet "]') || Array.from(document.querySelectorAll('button')).some(button => button.textContent.trim() === 'Connect wallet'));
      await page.evaluate(identity => {
        window.__l25Capture = undefined;
        window.addEventListener('kindle:wallet-measurement-result', event => { window.__l25Capture = event.detail; }, { once: true });
        window.dispatchEvent(new CustomEvent('kindle:wallet-measurement-start', { detail: identity }));
      }, identity);
    };
    const finish = async () => {
      await page.evaluate(() => window.dispatchEvent(new CustomEvent('kindle:wallet-measurement-finish')));
      const capture = await page.evaluate(() => window.__l25Capture);
      if (!capture) throw new Error('Actual runtime measurement capture unavailable');
      const file = `measurement-${journey.scenario}-${segment++}.json`;
      fs.writeFileSync(path.join(output, file), JSON.stringify(capture));
      write({ kind: 'capture', scenario: journey.scenario, file });
    };
    await begin();
    for (let index = 0; index < journey.steps.length; index++) {
      const step = journey.steps[index];
      if (page.url().startsWith(config.applicationOrigin) === false) throw new Error('Journey left the admitted application origin');
      const started = performance.now();
      if (step.action === 'click') { const role = step.role || 'button'; if (!['button','tab','link'].includes(role)) throw new Error('Unsupported interactive role'); await page.getByRole(role, { name: step.name, exact: true }).click({ timeout: 60000 }); }
      else if (step.action === 'fill') {
        if (!['Recipient','Amount','Requested amount (optional)','Amount (optional)','Expected wallet address','Your wallet'].includes(step.label)) throw new Error('Only public wallet form inputs are admitted');
        await page.getByLabel(step.label, { exact: true }).fill(step.value);
      } else if (step.action === 'wait_text') await page.getByText(step.text, { exact: true }).waitFor({ state: 'visible', timeout: 60000 });
      else if (step.action === 'reload') { await finish(); await page.reload({ waitUntil: 'domcontentloaded' }); await begin(); }
      else if (step.action === 'assert_text') { if ((step.check === 'pax_exact' && step.text !== '1 PAX') || (step.check === 'usdl_exact' && step.text !== '1 USDL')) throw new Error('Exact native/token display assertion required'); await page.getByText(step.text, { exact: true }).waitFor({ state: 'visible', timeout: 60000 }); assertions.push({ scenario: journey.scenario, check: step.check, text: step.text, verifiedAt: new Date().toISOString() }); }
      else if (step.action === 'keyboard') { if (!['Tab','Shift+Tab','Escape','Enter','Space'].includes(step.key)) throw new Error('Unsupported keyboard probe'); await page.keyboard.press(step.key); }
      else if (step.action === 'network_latency') {
        if (!Number.isInteger(step.latencyMs) || step.latencyMs < 0 || step.latencyMs > 15000) throw new Error('Invalid real transport interruption profile');
        networkSession ||= await page.context().newCDPSession(page);
        await networkSession.send('Network.enable');
        await networkSession.send('Network.emulateNetworkConditions', { offline: false, latency: step.latencyMs, downloadThroughput: -1, uploadThroughput: -1 });
      } else throw new Error('Unsupported real journey action');
      const state = await page.evaluate(() => ({ selectedWallets: Array.from(document.querySelectorAll('button[aria-label^="Wallet "]')).map(button => button.getAttribute('aria-label')), signatureReviewVisible: document.body.textContent.includes('Review the sign-in message.'), signInAvailable: Array.from(document.querySelectorAll('button')).some(button => button.textContent.trim() === 'Sign in to Sidiora'), activeWriteControls: Array.from(document.querySelectorAll('button')).filter(button => button.textContent.trim() === 'Approve transfer in wallet' && !button.disabled).length }));
      if (state.selectedWallets.length > 1) throw new Error('Duplicate selected wallet controls appeared');
      write({ kind: 'wallet_ui_state', scenario: journey.scenario, index, state });
      write({ kind: 'step', scenario: journey.scenario, index, action: step.action, startedMonotonicMs: started, endedMonotonicMs: performance.now() });
      const screenshot = path.join(output, `scenario-${journey.scenario}-step-${index}.png`);
      await page.screenshot({ path: screenshot, fullPage: true });
      screenshots.push({ path: path.basename(screenshot), sha256: sha(fs.readFileSync(screenshot)) });
    }
    await page.addScriptTag({ content: axe.toString('utf8') });
    const accessibility = await page.evaluate(async () => window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a','wcag2aa','wcag21a','wcag21aa'] } }));
    fs.writeFileSync(path.join(output, `accessibility-${journey.scenario}.json`), JSON.stringify(accessibility));
    if (accessibility.violations.length) throw new Error(`Actual accessibility violations in scenario ${journey.scenario}`);
    await finish();
    write({ kind: 'journey', scenario: journey.scenario, actualUrl: page.url(), browserVersion: browser.version(), accessibilityFile: `accessibility-${journey.scenario}.json`, viewport: await page.evaluate(() => ({ width: innerWidth, height: innerHeight, userAgent: navigator.userAgent })) });
    await Promise.all(assetReads);
    await page.close();
    } finally { if (networkSession) { await networkSession.send('Network.emulateNetworkConditions', { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 }); await networkSession.detach(); } await browser.close(); }
  }
  const requiredAssets = config.browserAssets;
  if (!requiredAssets || !Object.keys(requiredAssets).length) throw new Error('Actual current deployed browser bundle hash manifest required');
  for (const [url, digest] of assets) if (requiredAssets[url] !== digest) throw new Error('Observed browser bundle differs from admitted source build');
  for (const url of Object.keys(requiredAssets)) if (!assets.has(url)) throw new Error('An admitted browser bundle was not observed');
  if (errors.length) throw new Error('Browser errors or incomplete script capture were observed');
  raw.end();
  await new Promise(resolve => raw.on('finish', resolve));
  fs.writeFileSync(path.join(output, 'browser-result.json'), JSON.stringify({ schemaVersion: 1, runId: config.runId, sourceSha256: config.sourceSha256, deploymentManifestHash: config.deploymentManifestHash, configSha256: sha(inputBytes), observedAssets: Object.fromEntries(assets), screenshots, assertions, scenarios: config.browserJourneys.map(journey => journey.scenario), physicalDeviceProof: 'not_asserted_by_browser_profile' }, null, 2));
}
main().catch(error => { process.stderr.write(`${error.name}: ${error.message}\n`); process.exitCode = 1; });
