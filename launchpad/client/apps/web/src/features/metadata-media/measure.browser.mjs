import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {pathToFileURL, fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';

const assert = (condition, code) => {if (!condition) throw new Error(code);};
const sha = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
const inputChunks = [];
let inputBytes = 0;
for await (const chunk of process.stdin) {inputBytes += chunk.length;assert(inputBytes <= 1048576, 'browser_input_bound');inputChunks.push(chunk);}
const input = JSON.parse(Buffer.concat(inputChunks).toString('utf8'));
const checkFile = reference => {
 assert(reference && path.isAbsolute(reference.path) && /^[a-f0-9]{64}$/.test(reference.sha256), 'browser_file_reference');
 let current = reference.path;
 while (current !== path.dirname(current)) {assert(!fs.lstatSync(current).isSymbolicLink(), 'browser_symlink_denied');current = path.dirname(current);}
 const bytes = fs.readFileSync(reference.path);assert(sha(bytes) === reference.sha256, 'browser_file_digest');return bytes;
};
const nativeCapture = phase => {
 const config = input.native_probe;
 checkFile(config.executable);
 assert(config.kind === 'native_provider_capture' && config.fault_authorization_id && config.owned_scope, 'browser_fault_authority');
 for (const name of config.required_environment) assert(process.env[name], 'browser_provider_credentials');
 const result = spawnSync(config.executable.path, config.argv, {input: JSON.stringify({phase, bindings: input.bindings, owned_scope: config.owned_scope, observed_utc_ns: String(BigInt(Date.now()) * 1000000n)}), encoding: 'utf8', timeout: 30000, maxBuffer: 8388608});
 assert(result.status === 0, 'browser_native_capture_failed');
 const record = JSON.parse(result.stdout);
 assert(Object.keys(input.bindings).every(key => record.bindings[key] === input.bindings[key]) && Object.keys(record.bindings).length === Object.keys(input.bindings).length && record.phase === phase && record.owned_scope === config.owned_scope, 'browser_native_binding');
 return record;
};
const startedUtcNs = String(BigInt(Date.now()) * 1000000n);
const state = JSON.parse(checkFile(input.storage_state).toString('utf8'));
const imageBytes = checkFile(input.image);
checkFile(input.license_receipt);checkFile(input.source_receipt);
assert(imageBytes.length > 0 && imageBytes.length <= 6291456, 'actual_image_size');
assert(['image/jpeg', 'image/png', 'image/webp'].includes(input.mime), 'actual_image_mime');
assert(/^0x[a-f0-9]{40}$/.test(input.token_address), 'token_identity');
const creatorUrl = new URL(input.creator_url);
const viewerUrl = new URL(input.viewer_url);
assert(creatorUrl.protocol === 'https:' && viewerUrl.protocol === 'https:' && creatorUrl.origin === viewerUrl.origin && !creatorUrl.username && !creatorUrl.password && !viewerUrl.username && !viewerUrl.password, 'browser_owned_https_origin');
const moduleManifest = JSON.parse(checkFile(input.playwright_package).toString('utf8'));
assert(moduleManifest.name === 'playwright', 'actual_playwright_package');
checkFile(input.playwright_entry);
assert(path.dirname(input.playwright_entry.path) === path.dirname(input.playwright_package.path), 'playwright_entry_identity');
const playwright = await import(pathToFileURL(input.playwright_entry.path).href);
const axeBytes = checkFile(input.axe_source);
const sourceRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../../..');
const allowedOutput = path.join(sourceRoot, 'evidence/production-system/L39/phase4');
assert(input.output && path.isAbsolute(input.output) && path.resolve(input.output).startsWith(allowedOutput + path.sep) && !fs.existsSync(input.output), 'browser_unique_output');
let browser;
let context;
let metadataFaultStarted = false;
const network = [];
try {
 checkFile(input.chromium_executable);
 assert(path.isAbsolute(input.browser_profile_path) && fs.statSync(input.browser_profile_path).isDirectory() && !fs.lstatSync(input.browser_profile_path).isSymbolicLink(), 'real_leased_wallet_profile');
 checkFile(input.wallet_extension_manifest);
 const extensionDirectory = path.dirname(input.wallet_extension_manifest.path);
 assert(JSON.parse(fs.readFileSync(input.wallet_extension_manifest.path, 'utf8')).manifest_version >= 2, 'actual_wallet_extension');
 context = await playwright.chromium.launchPersistentContext(input.browser_profile_path, {executablePath: input.chromium_executable.path, headless: false, args: [`--disable-extensions-except=${extensionDirectory}`, `--load-extension=${extensionDirectory}`], viewport: {width: 390, height: 844}, serviceWorkers: 'block'});
 await context.addCookies(state.cookies);
 await context.addInitScript(origins => {for (const origin of origins) if (location.origin === origin.origin) for (const item of origin.localStorage) localStorage.setItem(item.name, item.value);}, state.origins);
 const page = await context.newPage();
 page.setDefaultTimeout(60000);
 await page.addInitScript(() => {
  globalThis.__metadataLayoutShift = 0;
  new PerformanceObserver(list => {for (const entry of list.getEntries()) if (!entry.hadRecentInput) globalThis.__metadataLayoutShift += entry.value;}).observe({type: 'layout-shift', buffered: true});
 });
 page.on('request', request => {
  if (/\/v2\/tokens\/0x[a-f0-9]{40}\/(?:assets|metadata)/.test(request.url())) network.push({event: 'request', at_ms: Date.now(), method: request.method(), url: request.url(), operation_id: request.headers()['idempotency-key'] ?? null});
 });
 page.on('response', response => {
  if (/\/v2\/tokens\/0x[a-f0-9]{40}\/(?:assets|metadata)/.test(response.url())) network.push({event: 'response', at_ms: Date.now(), method: response.request().method(), url: response.url(), status: response.status(), operation_id: response.request().headers()['idempotency-key'] ?? null});
 });
 await page.goto(input.creator_url, {waitUntil: 'networkidle'});
 const region = page.getByRole('region', {name: 'Creator image', exact: true});
 await region.waitFor({state: 'visible'});
 const chooser = region.getByLabel('Choose image', {exact: true});
 assert(await chooser.getAttribute('aria-describedby'), 'associated_upload_error');
 const inputId = await chooser.getAttribute('id');
 assert(inputId && await page.locator('label').evaluateAll((labels, id) => labels.some(label => label.htmlFor === id), inputId), 'keyboard_image_label');
 await chooser.focus();
 const fileChooser = page.waitForEvent('filechooser');
 await page.keyboard.press('Enter');
 await (await fileChooser).setFiles({name: path.basename(input.image.path), mimeType: input.mime, buffer: imageBytes});
 await chooser.focus();
 await page.keyboard.press('Tab');
 assert(await region.getByRole('button', {name: 'Upload or retry', exact: true}).evaluate(button => button === document.activeElement), 'keyboard_upload_focus');
 const began = page.waitForRequest(request => request.method() === 'PUT' && request.url().includes(`/v2/tokens/${input.token_address}/assets/`));
 const interrupt = page.waitForRequest(request => request.method() === 'PUT' && request.url().includes(`/v2/tokens/${input.token_address}/assets/`)).then(async request => {
  await context.setOffline(true);
  return request.headers()['idempotency-key'];
 });
 await page.keyboard.press('Enter');
 await began;
 const operation = await interrupt;
 assert(operation && /^[a-f0-9-]{36}$/.test(operation), 'actual_upload_operation');
 await region.getByRole('alert').filter({hasText: 'Upload unavailable.'}).waitFor({state: 'visible'});
 assert(!(await region.textContent()).includes('Approved at revision'), 'no_optimistic_approval');
 await context.setOffline(false);
 await region.getByRole('button', {name: 'Upload or retry', exact: true}).focus();
 const retried = page.waitForRequest(request => request.method() === 'PUT' && request.url().includes(`/v2/tokens/${input.token_address}/assets/`));
 await page.keyboard.press('Enter');
 assert((await retried).headers()['idempotency-key'] === operation, 'retry_operation_changed');
 await page.keyboard.press('Tab');
 const cancel = region.getByRole('button', {name: 'Cancel', exact: true});
 assert(await cancel.evaluate(button => button === document.activeElement && !button.disabled), 'keyboard_cancel_focus');
 await page.keyboard.press('Enter');
 await region.getByRole('alert').filter({hasText: 'Upload cancelled.'}).waitFor({state: 'visible'});
 assert(!(await region.textContent()).includes('Approved at revision'), 'cancel_published_late_approval');
 await region.getByRole('button', {name: 'Upload or retry', exact: true}).focus();
 const finalRetry = page.waitForRequest(request => request.method() === 'PUT' && request.url().includes(`/v2/tokens/${input.token_address}/assets/`));
 await page.keyboard.press('Enter');
 assert((await finalRetry).headers()['idempotency-key'] === operation, 'cancel_retry_operation_changed');
 if (input.wallet_approval_selector) {
  assert(typeof input.wallet_approval_selector === 'string', 'wallet_approval_selector');
  const walletPage = await new Promise((resolve, reject) => {
   const existing = context.pages().find(candidate => candidate.url().startsWith('chrome-extension://'));
   if (existing) {resolve(existing);return;}
   const timeout = setTimeout(() => {context.off('page', opened);reject(new Error('actual_wallet_approval_page_missing'));}, 60000);
   const opened = candidate => {candidate.waitForURL(/^chrome-extension:\/\//).then(() => {clearTimeout(timeout);context.off('page', opened);resolve(candidate);}).catch(reject);};
   context.on('page', opened);
  });
  await walletPage.locator(input.wallet_approval_selector).click();
 }
 await region.getByRole('status').filter({hasText: /^Approved at revision [1-9][0-9]*\.$/}).waitFor({state: 'visible', timeout: 120000});
 const active = await region.textContent();
 const approvedRevision = /Approved at revision ([1-9][0-9]*)\./.exec(active)?.[1];
 assert(approvedRevision && active.includes(`Active revision: ${approvedRevision}`) && !active.includes('validation pending'), 'approved_revision_pending_not_cleared');
 const committed = network.filter(row => row.method === 'PATCH' && row.event === 'response' && row.status === 200);
 assert(committed.length === 1, 'exactly_one_committed_image_revision');
 assert(network.filter(row => row.method === 'PUT' && row.event === 'request').every(row => row.operation_id === operation), 'multiple_asset_allocations');
 await page.goto(input.viewer_url, {waitUntil: 'networkidle'});
 const token = page.getByRole('region', {name: 'Token metadata', exact: true});
 assert((await token.textContent()).includes(input.token_address), 'viewer_token_identity');
 const image = token.getByRole('img', {name: 'Creator approved token image'});
 await image.waitFor({state: 'visible'});
 const dimensions = await image.evaluate(element => ({width: element.getAttribute('width'), height: element.getAttribute('height'), currentSrc: element.currentSrc, loaded: element.complete && element.naturalWidth > 0}));
 assert(Number(dimensions.width) > 0 && Number(dimensions.height) > 0 && dimensions.loaded && /\/approved\/[a-f0-9]{64}\/(256|512|1024)\/[a-f0-9]{64}\.png$/.test(new URL(dimensions.currentSrc).pathname), 'responsive_approved_image');
 await page.addScriptTag({content: axeBytes.toString('utf8')});
 const axe = await page.evaluate(async () => globalThis.axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa']}}));
 assert(axe.violations.length === 0, 'actual_accessibility_violations');
 const layoutShift = await page.evaluate(() => globalThis.__metadataLayoutShift);
 assert(Number.isFinite(layoutShift) && layoutShift <= 0.1, 'responsive_layout_shift');
 metadataFaultStarted = true;
 const outage = nativeCapture('browser-metadata-loss');
 assert(outage.metadata_outage_observed === true, 'actual_metadata_outage_missing');
 await page.reload({waitUntil: 'domcontentloaded'});
 assert((await token.textContent()).includes(input.token_address), 'metadata_outage_lost_token');
 assert(await page.getByLabel('Trading', {exact: true}).isVisible(), 'metadata_outage_lost_trading');
 await token.getByRole('alert').filter({hasText: 'Metadata unavailable. Trading remains accessible.'}).waitFor({state: 'visible'});
 const result = {bindings: input.bindings, started_utc_ns: startedUtcNs, ended_utc_ns: String(BigInt(Date.now()) * 1000000n), token_address: input.token_address, content_sha256: input.image.sha256, license_receipt_sha256: input.license_receipt.sha256, source_receipt_sha256: input.source_receipt.sha256, operation_id: operation, approved_revision: approvedRevision, network, keyboard_upload_passed: true, keyboard_cancel_passed: true, network_interruption_recovered: true, metadata_outage_trade_accessible: true, axe_violations: axe.violations.length, layout_shift: layoutShift, viewport: {width: 390, height: 844}, image: dimensions};
 nativeCapture('browser-metadata-restore');
 metadataFaultStarted = false;
 fs.writeFileSync(input.output, JSON.stringify(result) + '\n', {flag: 'wx', mode: 0o600});
} catch (error) {
 fs.writeFileSync(input.output, JSON.stringify({bindings: input.bindings, failure_class: error.name, code: /^[a-z0-9_]+$/.test(error.message) ? error.message : 'actual_browser_journey_failed', qualified: false, network}) + '\n', {flag: 'wx', mode: 0o600});
 process.exitCode = 1;
} finally {
 try {if (metadataFaultStarted) nativeCapture('browser-metadata-restore');} catch {process.exitCode = 1;}
 await context?.close();
 await browser?.close();
}
