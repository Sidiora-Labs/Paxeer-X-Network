'use strict';

const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');

const digest = (bytes) => crypto.createHash('sha256').update(bytes).digest('hex');
const requireValue = (condition, code) => {
  if (!condition) throw new Error(code);
};

async function main() {
  const args = process.argv.slice(2);
  requireValue(args.length === 6 && args[0] === '--config' && args[2] === '--config-sha256' && args[4] === '--output', 'explicit_browser_config_digest_output_required');
  const raw = fs.readFileSync(args[1]);
  requireValue(raw.length <= 1048576 && digest(raw) === args[3], 'browser_configuration_digest_mismatch');
  const config = JSON.parse(raw.toString('utf8'));
  const browserConfig = config.browser;
  requireValue(config.realm === 'isolated-chain125' && config.allow_mutations === true && browserConfig, 'isolated_browser_realm_required');
  const marker = process.env.L47_RUN_MARKER;
  requireValue(/^l47-[0-9a-f-]{36}$/.test(marker || ''), 'current_browser_run_marker_required');
  const output = path.resolve(args[5]);
  requireValue(!fs.existsSync(output), 'fresh_browser_result_required');
  for (const field of ['playwright_module', 'axe_script']) {
    const file = browserConfig[field];
    requireValue(path.isAbsolute(file) && digest(fs.readFileSync(file)) === browserConfig.file_sha256[file], `unbound_actual_${field}`);
  }
  const cdp = new URL(browserConfig.cdp_url);
  requireValue(cdp.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(cdp.hostname) && !cdp.username && !cdp.password, 'existing_loopback_browser_required');
  const pageURL = new URL(browserConfig.page_url);
  requireValue((pageURL.protocol === 'https:' || (pageURL.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(pageURL.hostname))) && !pageURL.username && !pageURL.password, 'invalid_actual_page_url');
  const started = BigInt(Date.now()) * 1000000n;
  const { chromium } = require(browserConfig.playwright_module);
  const browser = await chromium.connectOverCDP(browserConfig.cdp_url, { timeout: 10000 });
  const context = browser.contexts()[browserConfig.context_index];
  requireValue(context, 'actual_wallet_browser_context_missing');
  const page = await context.newPage();
  const responses = [];
  const popups = [];
  const popup = (created) => popups.push(created.url());
  context.on('page', popup);
  try {
    await page.goto(browserConfig.page_url, { waitUntil: 'domcontentloaded', timeout: 15000 });
    await page.locator(browserConfig.market_selector).waitFor({ state: 'visible', timeout: 10000 });
    const actual = await page.evaluate(async () => {
      const provider = window.ethereum;
      if (!provider || typeof provider.request !== 'function') throw new Error('genuine_wallet_provider_missing');
      const chain = await provider.request({ method: 'eth_chainId' });
      const accounts = await provider.request({ method: 'eth_accounts' });
      const original = provider.request;
      const tracked = new Set(['eth_sendTransaction', 'eth_signTransaction', 'personal_sign', 'eth_sign', 'eth_signTypedData', 'eth_signTypedData_v3', 'eth_signTypedData_v4', 'wallet_sendCalls', 'wallet_switchEthereumChain', 'eth_requestAccounts']);
      window.__l47WalletRequests = [];
      provider.request = function (request) {
        if (tracked.has(request.method)) window.__l47WalletRequests.push({ method: request.method, at: performance.now() });
        return Reflect.apply(original, this, [request]);
      };
      if (provider.request === original) throw new Error('wallet_request_instrumentation_unavailable');
      window.__l47RestoreWalletInstrumentation = () => { provider.request = original; };
      return { chain, accounts };
    });
    requireValue(actual.chain === '0x7d' && Array.isArray(actual.accounts) && actual.accounts.some((account) => account.toLowerCase() === config.beneficiary), 'actual_chain125_owner_wallet_required');
    const cap = await page.request.get(new URL('/v2/control/caps/pool_creation', browserConfig.page_url).toString(), { timeout: 5000 });
    requireValue(cap.ok(), 'actual_browser_cap_response_failed');
    const capBytes = await cap.body();
    const capBody = JSON.parse(capBytes.toString('utf8'));
    requireValue(capBody.data.capability === 'pool_creation' && capBody.data.allowed === false && capBody.data.stale === false, 'browser_cap_not_disabled');
    responses.push({ path: '/v2/control/caps/pool_creation', status: cap.status(), sha256: digest(capBytes) });
    for (const input of browserConfig.form_inputs) {
      requireValue(typeof input.selector === 'string' && typeof input.value === 'string' && !/seed|mnemonic|private.?key|password/i.test(input.selector), 'prohibited_browser_input');
      await page.locator(input.selector).fill(input.value);
    }
    const create = page.locator(browserConfig.create_button_selector);
    requireValue(await create.count() === 1 && await create.isVisible(), 'actual_creation_control_missing');
    const disabled = await create.isDisabled();
    if (!disabled) await create.click({ timeout: 5000 });
    await page.locator(browserConfig.warning_selector).waitFor({ state: 'visible', timeout: 5000 });
    requireValue((await page.locator(browserConfig.warning_selector).innerText()).trim().length > 0, 'precise_cap_warning_missing');
    await page.waitForTimeout(1000);
    const walletRequests = await page.evaluate(() => window.__l47WalletRequests);
    requireValue(walletRequests.length === 0 && popups.length === 0, 'creation_reached_wallet_before_cap_rejection');
    await page.addScriptTag({ content: fs.readFileSync(browserConfig.axe_script, 'utf8') });
    const accessibility = await page.evaluate(async () => {
      if (!window.axe || typeof window.axe.run !== 'function') throw new Error('real_axe_engine_missing');
      return window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } });
    });
    requireValue(accessibility.violations.length === 0, 'actual_control_journey_accessibility_failed');
    const screenshot = output + '.png';
    requireValue(!fs.existsSync(screenshot), 'fresh_browser_screenshot_required');
    await page.screenshot({ path: screenshot, fullPage: true });
    const report = {
      run_marker: marker,
      candidate_source_digest: config.candidate_source_digest,
      artifact_digest: config.artifact_digest,
      configuration_revision: config.configuration_revision,
      started_at_unix_ns: started.toString(),
      finished_at_unix_ns: (BigInt(Date.now()) * 1000000n).toString(),
      wallet_requests: walletRequests.length,
      actual_wallet_account: config.beneficiary,
      actual_wallet_chain: actual.chain,
      cap_disabled: true,
      market_visible: await page.locator(browserConfig.market_selector).isVisible(),
      warning_visible: await page.locator(browserConfig.warning_selector).isVisible(),
      creation_control_disabled: disabled,
      axe_violations: accessibility.violations,
      axe_engine_version: accessibility.testEngine.version,
      responses,
      screenshot: { path: screenshot, sha256: digest(fs.readFileSync(screenshot)) },
      global_capacity_qualified: false,
    };
    fs.writeFileSync(output, JSON.stringify(report) + '\n', { flag: 'wx', mode: 0o600 });
  } finally {
    context.off('page', popup);
    await page.evaluate(() => window.__l47RestoreWalletInstrumentation?.()).catch(() => {});
    await page.close();
    await browser.close();
  }
}

main().catch((error) => {
  process.stderr.write(JSON.stringify({ browser_exit: 1, code: error.message }) + '\n');
  process.exitCode = 1;
});
