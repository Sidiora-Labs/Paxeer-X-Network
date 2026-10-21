import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import readline from 'node:readline';
import { createRequire } from 'node:module';
import { spawn } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { installMarketObserver, percentile } from './phase4-observer.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../launchpad');
const require = createRequire(import.meta.url);
const sha = raw => crypto.createHash('sha256').update(raw).digest('hex');
const hex = /^[0-9a-f]{64}$/;
function boundFile(input, maximum = 67108864) {
  assert.ok(input && path.isAbsolute(input.path) && hex.test(input.sha256), 'Actual absolute hash-bound input required');
  assert.ok(fs.lstatSync(input.path).isFile() && !fs.lstatSync(input.path).isSymbolicLink());
  assert.ok(fs.statSync(input.path).size <= maximum, 'Input budget exceeded');
  const raw = fs.readFileSync(input.path); assert.equal(sha(raw), input.sha256, 'Input digest differs'); return raw;
}
function fileDigest(file, maximum = policy.budgets.artifact_max_bytes) {
  const info = fs.lstatSync(file); assert.ok(info.isFile() && !info.isSymbolicLink() && info.size <= maximum);
  const fd = fs.openSync(file, 'r'), buffer = Buffer.alloc(1048576), hash = crypto.createHash('sha256');
  try { let read; while ((read = fs.readSync(fd, buffer, 0, buffer.length, null)) > 0) hash.update(buffer.subarray(0, read)); }
  finally { fs.closeSync(fd); }
  return hash.digest('hex');
}
function document(input, maximum) { return JSON.parse(boundFile(input, maximum)); }
function origin(raw) { const value = new URL(raw); assert.equal(value.protocol, 'https:'); assert.equal(value.origin, raw); assert.equal(value.username, ''); assert.equal(value.password, ''); return value.origin; }
function decimal(raw) { assert.ok(typeof raw === 'string' && /^(0|[1-9][0-9]{0,77})$/.test(raw)); return BigInt(raw); }
function relativeFile(base, name) { const result = path.resolve(base, name); assert.ok(result.startsWith(base + path.sep), 'Artifact escapes owned directory'); return result; }
const policy = JSON.parse(fs.readFileSync(new URL('./phase4-policy.json', import.meta.url)));
const args = process.argv.slice(2); assert.equal(args.length, 6); assert.equal(args[0], '--config'); assert.equal(args[2], '--config-sha256'); assert.equal(args[4], '--output');
const config = document({ path: path.resolve(args[1]), sha256: args[3] }, 4194304);
assert.equal(config.task, '4.30'); assert.equal(config.workload_version, policy.workload_version);
const applicationOrigin = origin(config.application_origin), apiOrigin = origin(config.api_origin);
for (const name of ['candidate', 'interface', 'topology', 'generator']) assert.match(config.bindings[name], hex);
const candidate = document(config.candidate_manifest); assert.equal(config.candidate_manifest.sha256, config.bindings.candidate);
assert.equal(config.interface_manifest.sha256, config.bindings.interface); boundFile(config.interface_manifest);
assert.equal(config.topology_manifest.sha256, config.bindings.topology); boundFile(config.topology_manifest);
assert.ok(Array.isArray(candidate.files) && candidate.files.length > 0);
for (const file of candidate.files) { assert.ok(!path.isAbsolute(file.path)); const actual = relativeFile(root, file.path); boundFile({ path: actual, sha256: file.sha256 }); }
const ownedSources = ['client/qualification/market-ui/phase4-browser.mjs', 'client/qualification/market-ui/phase4-observer.mjs', 'client/qualification/market-ui/phase4-policy.json', 'client/apps/web/src/page-modules/token/market-session.ts', 'client/apps/web/src/widgets/trading/token-market-layout.tsx'];
for (const source of ownedSources) assert.ok(candidate.files.some(file => file.path === source), `Candidate omits measured source ${source}`);
const corpus = document(config.trace_corpus, 4194304), device = document(config.browser_device, 1048576);
assert.equal(corpus.task, '4.30'); assert.equal(corpus.candidate, config.bindings.candidate);
assert.equal(device.browser, 'chromium'); assert.equal(device.reference_device_class, 'midrange');
assert.ok(Number.isFinite(device.cpu_throttling_rate) && device.cpu_throttling_rate >= 1);
assert.ok(Number.isFinite(device.device_pixel_ratio) && device.device_pixel_ratio > 0, 'Actual reference device pixel ratio required');
assert.ok(Number.isSafeInteger(device.clock_uncertainty_ns) && device.clock_uncertainty_ns >= 0 && device.clock_uncertainty_ns <= 5000000, 'Actual browser clock calibration required');
assert.ok(Number.isFinite(config.heap_limits.maximum_used_bytes) && config.heap_limits.maximum_used_bytes > 0);
assert.ok(Number.isFinite(config.heap_limits.maximum_cycle_growth_bytes) && config.heap_limits.maximum_cycle_growth_bytes >= 0);
assert.ok(Array.isArray(config.hot_topics) && config.hot_topics.length === policy.budgets.steady_topics && new Set(config.hot_topics).size === config.hot_topics.length);
assert.ok(config.hot_topics.every(topic => /^market\.[a-z]+\.[a-zA-Z0-9_.-]+$/.test(topic)));
assert.ok(config.hot_topics.some(topic => topic.startsWith('market.price.')) && config.hot_topics.some(topic => topic.startsWith('market.trades.')), 'Real price and durable-fill topics required');
assert.equal(config.node_executable.path, process.execPath); assert.equal(fileDigest(process.execPath), config.node_executable.sha256);
boundFile(config.playwright_module); const { chromium } = require(config.playwright_module.path);
boundFile(config.sdk_module); const sdk = await import(pathToFileURL(config.sdk_module.path).href); assert.equal(typeof sdk.decodeContract, 'function'); assert.equal(typeof sdk.decodeV2Contract, 'function');
boundFile(config.axe_script); boundFile(config.browser_session);
const backendScript = path.resolve(config.backend_runner.path); boundFile(config.backend_runner);
assert.ok(backendScript.startsWith(path.join(root, 'tools/production/load') + path.sep));
assert.ok(['phase2_runner.py', 'phase3_runner.py', 'phase4_runner.py'].includes(path.basename(backendScript)));
const backendConfig = document(config.backend_config);
assert.deepEqual(backendConfig.bindings, config.bindings);
assert.equal(backendConfig.application_origin, applicationOrigin);
assert.ok(Array.isArray(config.backend_environment_keys) && config.backend_environment_keys.every(key => /^[A-Z][A-Z0-9_]{0,79}$/.test(key)));
assert.ok(path.isAbsolute(config.python_executable.path)); boundFile(config.python_executable);
const output = path.resolve(args[5]), allowedOutput = path.join(root, 'evidence/production-system/L30/phase4');
assert.ok(output.startsWith(allowedOutput + path.sep)); fs.mkdirSync(output, { recursive: true, mode: 0o700 });
assert.equal(fs.readdirSync(output).length, 0, 'Fresh owned evidence directory required');
const streams = new Map(), counts = new Map(), digests = new Map(); let bytes = 0;
function write(name, value) {
  const raw = Buffer.from(JSON.stringify(value) + '\n'); bytes += raw.length; assert.ok(bytes <= policy.budgets.artifact_max_bytes, 'Raw artifact budget exhausted');
  let fd = streams.get(name); if (fd === undefined) { fd = fs.openSync(relativeFile(output, name), 'wx', 0o600); streams.set(name, fd); }
  fs.writeSync(fd, raw); counts.set(name, (counts.get(name) ?? 0) + 1);
}
const samples = [], interactions = [], priceTimes = new Map(), longTasks = [], layoutShifts = [], sourceEvents = new Map(), freshnessRows = [], priceCommits = [];
const sockets = new Map(), pendingResponses = new Set(), failures = [], outcomes = [], cycles = [], screenshots = [], accessibility = [];
let browser, context, page, cdp, backend, metricsTimer, steadyStarted = null, steadyEnded = null, networkCount = 0, resourceFailure = null;
const startedUtcMs = Date.now(); let finishedUtcMs = null;
function terminateBackend() {
  if (!backend?.pid || backend.exitCode !== null) return;
  process.kill(-backend.pid, 'SIGTERM');
  const kill = setTimeout(() => { if (backend.exitCode === null) process.kill(-backend.pid, 'SIGKILL'); }, 2000); kill.unref();
}
const deadline = setTimeout(() => { failures.push('Harness exceeded30-minute deadline'); void browser?.close(); terminateBackend(); }, policy.budgets.run_deadline_ms);
function activeTopics() { return new Set([...sockets.values()].flatMap(socket => [...socket.active.values()].flatMap(topics => [...topics]))); }
function capture(records) {
  assert.ok(Array.isArray(records) && records.length <= 512);
  for (const row of records) {
    write('browser-observations.ndjson', row);
    if (row.kind === 'observer_failure') failures.push(row.reason);
    if (row.kind === 'dom') { samples.push(row); assert.ok(samples.length <= 3600); for (const list of row.lists) if (list.rows > policy.budgets.rows_per_list) failures.push(`DOM list ${list.label} has${list.rows} rows`); for (const session of row.sessions) { if (session.fill_cache > policy.budgets.fill_cache_rows || session.pending_fills > policy.budgets.fill_cache_rows) failures.push('Actual fill cache exceeded1000'); if (session.inactive_pools > policy.budgets.inactive_pools) failures.push('Inactive pool cache exceeded20'); } }
    if (row.kind === 'local_interaction' && row.terminal && row.trusted) interactions.push(row);
    if (row.kind === 'freshness') freshnessRows.push(row);
    if (row.kind === 'price_commit') priceCommits.push(row);
    if (row.kind === 'price_commit' && !row.initial && row.stream === 'live') { if (!priceTimes.has(row.topic)) priceTimes.set(row.topic, []); priceTimes.get(row.topic).push(row); }
    if (row.kind === 'longtask') longTasks.push(row);
    if (row.kind === 'layout_shift' && !row.had_recent_input) layoutShifts.push(row);
  }
}
async function listenerCounts() {
  const result = {};
  for (const target of ['document', 'window']) { const evaluated = await cdp.send('Runtime.evaluate', { expression: target, objectGroup: 'market-listeners' }); assert.ok(evaluated.result.objectId); const listeners = await cdp.send('DOMDebugger.getEventListeners', { objectId: evaluated.result.objectId }); result[target] = listeners.listeners.map(item => item.type).sort(); }
  await cdp.send('Runtime.releaseObjectGroup', { objectGroup: 'market-listeners' }); return result;
}
async function heap() { const result = await cdp.send('Runtime.getHeapUsage'); assert.ok(Number.isFinite(result.usedSize)); write('heap.ndjson', { observed_utc_ms: Date.now(), used_bytes: result.usedSize, total_bytes: result.totalSize, topics: [...activeTopics()] }); if (result.usedSize > config.heap_limits.maximum_used_bytes) failures.push('Heap budget exhausted'); return result.usedSize; }
async function observedState() { await page.evaluate(() => window.__kindleMarketCapture.flush()); return { listeners: await listenerCounts(), heap: await heap(), active_topics: [...activeTopics()].sort(), scroll: await page.evaluate(() => ({ x: scrollX, y: scrollY })) }; }
function stepsRequired(value) { assert.ok(Array.isArray(value) && value.length > 0 && value.length <= 512, 'Complete bounded actual action corpus required'); return value; }
async function steps(actions, group) {
  for (const [index, action] of stepsRequired(actions).entries()) {
    const before = networkCount, started = performance.now();
    assert.ok(['goto', 'click', 'fill', 'press', 'visible', 'text', 'attribute', 'back', 'scroll', 'anchor'].includes(action.op));
    if (action.op === 'goto') { const destination = new URL(action.path, applicationOrigin); assert.equal(destination.origin, applicationOrigin); await page.goto(destination.href, { waitUntil: 'domcontentloaded' }); }
    else if (action.op === 'back') await page.goBack({ waitUntil: 'domcontentloaded' });
    else {
      assert.ok(typeof action.selector === 'string' && action.selector.length > 0 && action.selector.length <= 512);
      const locator = page.locator(action.selector); assert.equal(await locator.count(), 1, `Real selector must identify one element:${action.selector}`);
      if (action.op === 'click') await locator.click();
      if (action.op === 'fill') { assert.ok(typeof action.value === 'string' && action.value.length <= 4096); await locator.fill(action.value); }
      if (action.op === 'press') await locator.press(action.key);
      if (action.op === 'visible') await locator.waitFor({ state: 'visible', timeout: 15000 });
      if (action.op === 'text') assert.equal((await locator.innerText()).trim(), action.value);
      if (action.op === 'attribute') assert.equal(await locator.getAttribute(action.name), action.value);
      if (action.op === 'scroll') { assert.ok(Number.isFinite(action.top) && action.top >= 0); await locator.evaluate((element, top) => { element.scrollTop = top; }, action.top); }
      if (action.op === 'anchor') { assert.ok(typeof action.id === 'string'); const state = await page.evaluate(({ id, selector }) => window.__kindleMarketCapture.anchor(id, selector), action); if (action.expected_identity) assert.equal(state.identity, action.expected_identity); }
    }
    if (action.expected_path) assert.equal(new URL(page.url()).pathname, action.expected_path);
    const outcome = { group, index, op: action.op, selector: action.selector ?? null, pathname: new URL(page.url()).pathname, duration_ms: performance.now() - started, network_begin: before, network_end: networkCount, observed_utc_ms: Date.now() };
    write('control-outcomes.ndjson', outcome); outcomes.push(outcome);
  }
}
async function inventory() {
  const selector = 'main button, main a, main input, main select, main summary, main [role="tab"]';
  const controls = await page.locator(selector).evaluateAll(elements => elements.map((element, index) => ({ index, visible: element.getBoundingClientRect().width > 0 && element.getBoundingClientRect().height > 0, tag: element.tagName, label: (element.getAttribute('aria-label') ?? element.textContent ?? '').trim().slice(0, 256), disabled: element.hasAttribute('disabled'), href: element.getAttribute('href'), title: element.getAttribute('title') })).filter(element => element.visible));
  const actionSelectors = [...new Set(corpus.controls.flatMap(control => control.steps.filter(action => ['click', 'fill', 'press'].includes(action.op)).map(action => action.selector)))];
  for (const control of controls) {
    control.matched_actions = [];
    for (const actionSelector of actionSelectors) {
      const candidate = page.locator(actionSelector);
      if (await candidate.count() !== 1) continue;
      const handle = await candidate.elementHandle();
      try { if (await page.locator(selector).nth(control.index).evaluate((element, candidateElement) => element === candidateElement, handle)) control.matched_actions.push(actionSelector); }
      finally { await handle?.dispose(); }
    }
    assert.ok(control.disabled || control.matched_actions.length > 0, `No actual outcome corpus for visible control:${control.label}`);
  }
  write('control-inventory.ndjson', { observed_utc_ms: Date.now(), pathname: new URL(page.url()).pathname, controls }); return controls;
}
async function inspect(role, width) {
  await page.addScriptTag({ path: config.axe_script.path });
  const result = await page.evaluate(async () => { if (!window.axe?.run) throw new Error('Actual pinned axe source missing'); return window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } }); });
  write('accessibility.ndjson', { role, width, observed_utc_ms: Date.now(), violations: result.violations, incomplete: result.incomplete, passes: result.passes.map(item => item.id) }); accessibility.push({ role, width, violation_count: result.violations.length });
  assert.equal(result.violations.length, 0, 'Observed accessibility violations');
  const geometry = await page.evaluate(() => ({ width: innerWidth, document_width: document.documentElement.scrollWidth, focus: document.activeElement?.getAttribute('aria-label') ?? document.activeElement?.tagName, cls: window.__kindleMarketCapture.cls() }));
  assert.ok(geometry.document_width <= geometry.width, 'Responsive viewport overflow');
  const name = `${role}-${width}.png`; await page.screenshot({ path: relativeFile(output, name), fullPage: true }); screenshots.push({ role, width, file: name, geometry });
}
async function corpusCorrelation() {
  assert.ok(Array.isArray(corpus.data_assertions) && corpus.data_assertions.length > 0);
  for (const observation of corpus.data_assertions) {
    assert.ok(typeof observation.path === 'string' && /^\/v[12]\//.test(observation.path));
    const url = new URL(observation.path, apiOrigin); assert.equal(url.origin, apiOrigin);
    const response = await context.request.get(url.href, { maxRedirects: 0, timeout: 15000 }); assert.equal(response.status(), 200);
    const raw = await response.body(); assert.ok(raw.length <= policy.budgets.network_body_max_bytes); const value = observation.path.startsWith('/v2/') ? sdk.decodeV2Contract(observation.schema, JSON.parse(raw)) : sdk.decodeContract(observation.schema, JSON.parse(raw));
    assert.ok(value.requestId && value.quality !== 'quarantined');
    let field = value; for (const key of observation.field_path) { assert.ok(typeof key === 'string' && Object.hasOwn(field, key)); field = field[key]; }
    assert.ok(typeof field === 'string', 'Exact authoritative scalar required');
    const locator = page.locator(observation.selector); assert.equal(await locator.count(), 1);
    if (observation.attribute) assert.equal(await locator.getAttribute(observation.attribute), field.toLowerCase()); else assert.ok((await locator.innerText()).includes(field));
    write('authoritative-correlation.ndjson', { pathname: new URL(page.url()).pathname, path: url.pathname, request_id: value.requestId, revision: value.revision, generated_at: value.generatedAt, watermark: value.watermark, response_sha256: sha(raw), field_path: observation.field_path, scalar: field, selector: observation.selector });
  }
}
async function interruption() {
  stepsRequired(corpus.interruption.open_steps); stepsRequired(corpus.interruption.reconnect_steps);
  await steps(corpus.interruption.open_steps, 'interruption-open');
  await page.locator('[data-market-stream="live"][data-market-freshness="current"]').waitFor({ state: 'visible', timeout: 30000 });
  const offlineStarted = Date.now(); await context.setOffline(true); await page.waitForTimeout(31000);
  await page.locator('[data-market-freshness="stale"]').waitFor({ state: 'visible', timeout: 5000 });
  write('interruption.ndjson', { kind: 'offline-stale', started_utc_ms: offlineStarted, observed_utc_ms: Date.now() });
  await context.setOffline(false); await steps(corpus.interruption.reconnect_steps, 'actual-network-reconnect');
  await page.locator('[data-market-stream="live"][data-market-freshness="current"]').waitFor({ state: 'visible', timeout: 30000 });
  const background = await context.newPage(); await background.goto('about:blank'); await background.bringToFront();
  await page.waitForFunction(() => document.visibilityState === 'hidden', null, { timeout: 5000 });
  const hiddenStarted = Date.now(); await background.waitForTimeout(31000); await page.bringToFront();
  const hiddenEnded = Date.now(); await page.waitForFunction(() => document.visibilityState === 'visible');
  await page.locator('[data-market-stream="live"][data-market-freshness="current"]').waitFor({ state: 'visible', timeout: 30000 });
  await page.evaluate(() => window.__kindleMarketCapture.flush());
  assert.equal(priceCommits.filter(row => row.observed_utc_ms >= hiddenStarted && row.observed_utc_ms < hiddenEnded && row.pathname !== 'blank').length, 0, 'Hidden page committed price visual updates');
  assert.ok(freshnessRows.some(row => row.observed_utc_ms >= offlineStarted && row.freshness === 'stale'));
  write('interruption.ndjson', { kind: 'actual-hidden-tab-resume', hidden_started_utc_ms: hiddenStarted, hidden_ended_utc_ms: hiddenEnded, resumed_utc_ms: Date.now(), continuity: await page.locator('[data-market-pool]').getAttribute('data-market-stream') });
  await background.close();
}
async function backendEvidence() {
  const resultRaw = fs.readFileSync(path.join(config.backend_output, 'result.json'), 'utf8'), result = JSON.parse(resultRaw); assert.deepEqual(result.bindings, config.bindings); assert.equal(result.workload_version, policy.workload_version); assert.ok(result.duration_ns >= policy.budgets.steady_minimum_ms * 1000000);
  const exactTimestamp = field => { const matches = [...resultRaw.matchAll(new RegExp('"' + field + '"\\s*:\\s*([0-9]+)', 'g'))]; assert.equal(matches.length, 1, 'Exactly one raw backend timestamp required'); return decimal(matches[0][1]); };
  const backendStart = exactTimestamp('started_utc_ns'), backendEnd = exactTimestamp('ended_utc_ns');
  const ledger = JSON.parse(fs.readFileSync(path.join(config.backend_output, 'workload-ledger.json'))); assert.equal(ledger.version, policy.workload_version); assert.equal(ledger.peak_http_rps, 10000); assert.equal(ledger.peak_mix.quotes, 2000); assert.equal(ledger.peak_mix.writes, 2000); assert.equal(Object.values(ledger.peak_mix).reduce((total, value) => total + value, 0), 10000);
  const hashes = JSON.parse(fs.readFileSync(path.join(config.backend_output, 'artifact-sha256.json'))); assert.equal(sha(fs.readFileSync(path.join(config.backend_output, 'artifact-sha256.json'))), result.artifact_manifest_sha256);
  for (const [name, hash] of Object.entries(hashes)) assert.equal(fileDigest(relativeFile(path.resolve(config.backend_output), name)), hash);
  assert.ok(BigInt(steadyStarted) * 1000000n <= backendStart && BigInt(steadyEnded) * 1000000n >= backendEnd, 'Browser profile must cover entire actual backend workload interval');
  assert.ok(hashes[config.backend_raw_events], 'Actual backend raw event artifact missing');
  const rates = new Map(), unique = new Set();
  for await (const line of readline.createInterface({ input: fs.createReadStream(relativeFile(path.resolve(config.backend_output), config.backend_raw_events)), crlfDelay: Infinity })) {
    assert.ok(line.length <= policy.budgets.source_frame_max_bytes); const row = JSON.parse(line);
    assert.equal(row.timestamp_authority, 'postgres_transaction_commit'); assert.ok(typeof row.event_id === 'string' && !unique.has(row.event_id)); unique.add(row.event_id);
    const stamp = decimal(row.durable_commit_utc_ns); const second = stamp / 1000000000n;
    assert.ok(stamp >= backendStart && stamp <= backendEnd); rates.set(second.toString(), (rates.get(second.toString()) ?? 0) + 1);
    assert.ok(hex.test(row.source_fact_digest));
  }
  assert.ok(rates.size > 0 && Math.max(...rates.values()) >= policy.budgets.platform_burst_events_per_second, 'Actual10k relevant event/s burst absent');
  return { bindings: result.bindings, started_utc_ns: backendStart.toString(), ended_utc_ns: backendEnd.toString(), started_utc_ms: Number(backendStart / 1000000n), ended_utc_ms: Number(backendEnd / 1000000n), duration_ns: result.duration_ns, host_id: result.host_id ?? null, accepted_connections: result.accepted_connections, actual_http: result.http, measurement_scope: 'actual source-bound backend generator; shared workload acceptance deferred to9.1', artifact_manifest_sha256: result.artifact_manifest_sha256, event_count: unique.size, peak_events_per_second: Math.max(...rates.values()) };
}
try {
  assert.equal(fileDigest(config.browser_executable.path, 536870912), config.browser_executable.sha256); browser = await chromium.launch({ headless: true, executablePath: config.browser_executable.path });
  assert.equal(await browser.version(), device.browser_version, 'Reference browser identity differs');
  context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, storageState: config.browser_session.path, reducedMotion: 'reduce', deviceScaleFactor: device.device_pixel_ratio });
  await context.tracing.start({ screenshots: true, snapshots: true, sources: false });
  await context.exposeBinding('__kindleMarketWrite', (_source, records) => capture(records));
  await context.addInitScript(installMarketObserver, policy);
  page = await context.newPage(); cdp = await context.newCDPSession(page); await cdp.send('Emulation.setCPUThrottlingRate', { rate: device.cpu_throttling_rate });
  page.on('pageerror', error => failures.push(`Actual page exception:${error.name}`));
  page.on('response', response => {
    const task = (async () => { const url = new URL(response.url()); if (![applicationOrigin, apiOrigin].includes(url.origin)) return; const headers = await response.allHeaders(); networkCount++; const row = { index: networkCount, observed_utc_ms: Date.now(), origin: url.origin, pathname: url.pathname, method: response.request().method(), status: response.status(), request_id: headers['x-request-id'] ?? headers['x-trace-id'] ?? null }; if (/^\/v[12]\//.test(url.pathname) && response.headers()['content-type']?.includes('json')) { const raw = await response.body(); assert.ok(raw.length <= policy.budgets.network_body_max_bytes); row.response_sha256 = sha(raw); } write('network.ndjson', row); })(); pendingResponses.add(task); task.catch(error => { failures.push(error.message); }).finally(() => pendingResponses.delete(task));
  });
  page.on('websocket', socket => {
    const id = crypto.randomUUID(), socketUrl = new URL(socket.url()), state = { topics: new Set(), requested: new Map(), active: new Map(), url: socketUrl.origin }; sockets.set(id, state);
    const decode = (name, value) => socketUrl.pathname.startsWith('/v2/') ? sdk.decodeV2Contract(name, value) : sdk.decodeContract(name, value);
    socket.on('framesent', frame => { try { const raw = String(frame.payload); assert.ok(Buffer.byteLength(raw) <= policy.budgets.source_frame_max_bytes); const value = JSON.parse(raw); if (value.op === 'subscribe') { decode('Subscribe', value); assert.ok(Array.isArray(value.topics) && value.topics.length <= policy.budgets.topics_per_socket); state.requested.set(value.subscriptionId, new Set(value.topics)); state.topics = new Set([...state.requested.values()].flatMap(topics => [...topics])); assert.ok(state.topics.size <= policy.budgets.topics_per_socket); write('socket-control.ndjson', { socket_id: id, op: value.op, subscription_id: value.subscriptionId, topics: value.topics, observed_utc_ms: Date.now() }); } } catch (error) { failures.push(error.message); } });
    socket.on('framereceived', frame => { try { const raw = Buffer.isBuffer(frame.payload) ? frame.payload.toString('utf8') : frame.payload; assert.ok(Buffer.byteLength(raw) <= policy.budgets.source_frame_max_bytes); const value = JSON.parse(raw); if (value.topic?.startsWith('market.')) { const event = decode('StreamEvent', value); assert.ok(state.topics.has(event.topic)); decimal(event.sequence); decimal(event.revision); write('stream-events.ndjson', { socket_id: id, observed_utc_ms: Date.now(), event }); if (event.topic.startsWith('market.trades.')) { const fill = event.payload, key = `${fill.position.chainId}:${fill.position.transactionHash.toLowerCase()}:${fill.position.logIndex}`; sourceEvents.set(key, { pool: fill.poolAddress, event_id: fill.eventId }); assert.ok(sourceEvents.size <= 1000000); } } else if (value.op) { const event = decode('StreamControl', value); if (event.op === 'subscribed') { assert.ok(state.requested.has(event.subscriptionId)); state.active.set(event.subscriptionId, state.requested.get(event.subscriptionId)); } if (event.op === 'unsubscribed' || event.op === 'subscription_closed') { state.active.delete(event.subscriptionId); state.requested.delete(event.subscriptionId); state.topics = new Set([...state.requested.values()].flatMap(topics => [...topics])); } write('stream-transitions.ndjson', { socket_id: id, observed_utc_ms: Date.now(), op: event.op, subscription_id: event.subscriptionId, code: event.code ?? null }); } } catch (error) { failures.push(error.message); } });
    socket.on('close', () => { write('socket-control.ndjson', { socket_id: id, op: 'closed', topics: [...state.topics], observed_utc_ms: Date.now() }); sockets.delete(id); });
  });
  await steps(corpus.hot_steps, 'hot-session'); await page.locator(corpus.ready_selector).waitFor({ state: 'visible', timeout: 30000 }); await page.waitForTimeout(1000);
  for (const topic of config.hot_topics) assert.ok(activeTopics().has(topic), `Required actual topic missing:${topic}`);
  steadyStarted = Date.now();
  const environment = { PATH: process.env.PATH }; for (const key of config.backend_environment_keys) { assert.ok(process.env[key], `Actual backend environment missing:${key}`); environment[key] = process.env[key]; }
  assert.ok(path.isAbsolute(config.backend_output)); assert.ok(!fs.existsSync(config.backend_output), 'Backend must run into a fresh real-owned evidence directory');
  backend = spawn(config.python_executable.path, [backendScript, '--config', config.backend_config.path, '--config-sha256', config.backend_config.sha256, '--output', config.backend_output], { cwd: root, env: environment, detached: true, stdio: ['ignore', 'pipe', 'pipe'] });
  backend.stdout.on('data', raw => write('backend-process.ndjson', { observed_utc_ms: Date.now(), stream: 'stdout', bytes: raw.length, sha256: sha(raw) })); backend.stderr.on('data', raw => write('backend-process.ndjson', { observed_utc_ms: Date.now(), stream: 'stderr', bytes: raw.length, sha256: sha(raw) }));
  let backendFinished = false, backendFailure = null;
  const backendDone = new Promise(resolve => { backend.once('error', error => { backendFailure = error; backendFinished = true; resolve(); }); backend.once('exit', (code, signal) => { if (code !== 0) backendFailure = new Error(`Actual backend failed:${code}/${signal}`); backendFinished = true; resolve(); }); });
  metricsTimer = setInterval(() => { void heap().then(() => { const topics = activeTopics(); write('active-topics.ndjson', { observed_utc_ms: Date.now(), topics: [...topics] }); for (const topic of config.hot_topics) if (!topics.has(topic)) failures.push(`Steady topic detached:${topic}`); }).catch(error => { resourceFailure = error.message; }); }, 1000);
  stepsRequired(corpus.steady_steps);
  while (!backendFinished) { await steps(corpus.steady_steps, 'steady-terminal-interactions'); await page.waitForTimeout(1000); }
  await backendDone; if (backendFailure) throw backendFailure; steadyEnded = Date.now(); clearInterval(metricsTimer); metricsTimer = null;
  assert.ok(steadyEnded - steadyStarted >= policy.budgets.steady_minimum_ms); await page.evaluate(() => window.__kindleMarketCapture.flush());
  const backendObserved = await backendEvidence();
  await interruption();
  assert.ok(Array.isArray(corpus.controls) && corpus.controls.length > 0);
  const registry = fs.readFileSync(path.join(root, 'client/apps/web/src/widgets/terminal/panel-registry.ts'), 'utf8');
  const panelIds = [...registry.matchAll(/panelId:\s*'([^']+)'/g)].map(match => match[1]);
  for (const id of panelIds) assert.ok(corpus.controls.some(control => control.panel_id === id), `Missing actual control outcome for:${id}`);
  for (const control of corpus.controls) { await steps(control.steps, control.panel_id); write('panel-outcomes.ndjson', { panel_id: control.panel_id, observed_utc_ms: Date.now(), inventory: await inventory() }); }
  await steps(corpus.routes.discovery, 'cycle-baseline'); await cdp.send('HeapProfiler.collectGarbage'); const baseline = await observedState();
  assert.ok(corpus.cycle_steps.every(action => action.op !== 'goto'), 'Route lifecycle must use real client navigation, not reload');
  for (let index = 0; index < policy.budgets.route_cycles; index++) { const begin = outcomes.length; await steps(corpus.cycle_steps, `cycle-${index}`); const paths = outcomes.slice(begin).map(row => row.pathname); assert.ok(paths.includes('/terminal') && paths.some(value => /^\/token\/0x[0-9a-fA-F]{40}$/.test(value)), 'Each cycle must actually visit token and terminal'); assert.equal(new URL(page.url()).pathname, '/'); await page.waitForTimeout(1000); const current = await observedState(); assert.deepEqual(current.listeners, baseline.listeners, 'Document/window listener retention grew'); assert.deepEqual(current.active_topics, baseline.active_topics, 'Inactive market socket topics retained'); assert.equal(current.scroll.y, baseline.scroll.y, 'Discovery scroll was not restored'); cycles.push({ index, ...current }); write('route-cycles.ndjson', cycles.at(-1)); }
  await cdp.send('HeapProfiler.collectGarbage'); const afterGc = await heap(); assert.ok(afterGc - baseline.heap <= config.heap_limits.maximum_cycle_growth_bytes, 'Measured retained heap growth exceeds declared bound');
  for (const width of policy.responsive_widths) { await page.setViewportSize({ width, height: 1000 }); for (const role of policy.routes) { await steps(corpus.routes[role], `${role}-${width}`); await inspect(role, width); } }
  await corpusCorrelation();
  assert.ok(Array.isArray(corpus.historical_fill_requests) && corpus.historical_fill_requests.length > 0);
  const provenFills = new Set();
  for (const endpoint of corpus.historical_fill_requests) {
    const url = new URL(endpoint, apiOrigin); assert.equal(url.origin, apiOrigin); assert.match(url.pathname, /^\/v2\/pools\/0x[0-9a-f]{40}\/trades$/);
    const pool = url.pathname.split('/')[3], expected = [...sourceEvents].filter(([, event]) => event.pool.toLowerCase() === pool).map(([key]) => key), cursors = new Set(); let pages = 0;
    while (pages++ < policy.budgets.history_page_maximum) {
      const response = await context.request.get(url.href, { maxRedirects: 0, timeout: 15000 }); assert.equal(response.status(), 200); const value = sdk.decodeV2Contract('TradesResponse', await response.json()); assert.ok(value.quality !== 'quarantined' && value.requestId);
      for (const fill of value.data.items) { assert.equal(fill.poolAddress.toLowerCase(), pool); const key = `${fill.position.chainId}:${fill.position.transactionHash.toLowerCase()}:${fill.position.logIndex}`; if (sourceEvents.has(key)) { assert.equal(sourceEvents.get(key).event_id, fill.eventId, 'Canonical immutable fill identity disagrees'); provenFills.add(key); } }
      write('historical-fill-queries.ndjson', { endpoint: url.pathname, request_id: value.requestId, revision: value.revision, response_cursor_sha256: value.nextCursor ? sha(value.nextCursor) : null, items: value.data.items.map(fill => ({ event_id: fill.eventId, pool: fill.poolAddress, position: fill.position })) });
      if (expected.every(key => provenFills.has(key)) || !value.nextCursor) break;
      assert.ok(!cursors.has(value.nextCursor), 'Historical cursor repeated without progress'); cursors.add(value.nextCursor); url.searchParams.set('cursor', value.nextCursor);
    }
    assert.ok(expected.every(key => provenFills.has(key)), 'Observed pool fills are absent from bounded historical pagination');
  }
  assert.ok(sourceEvents.size > 0); assert.equal(provenFills.size, sourceEvents.size, 'Every observed durable fill must remain queryable through actual historical pages');
  const actualInteractions = interactions.filter(row => row.observed_utc_ms >= backendObserved.started_utc_ms && row.observed_utc_ms <= backendObserved.ended_utc_ms); assert.ok(actualInteractions.length >= 20, 'Actual steady terminal interactions missing'); const latencyP95 = percentile(actualInteractions.map(row => row.duration_ms), .95); assert.ok(latencyP95 <= policy.budgets.terminal_input_p95_ms);
  for (const topic of config.hot_topics.filter(topic => topic.startsWith('market.price.'))) { const rows = (priceTimes.get(topic) ?? []).filter(row => row.observed_utc_ms >= backendObserved.started_utc_ms && row.observed_utc_ms <= backendObserved.ended_utc_ms); assert.ok(rows.length > 0, `Actual price repaint absent:${topic}`); for (let first = 0, last = 0; last < rows.length; last++) { while (rows[last].observed_utc_ms - rows[first].observed_utc_ms >= 1000) first++; assert.ok(last - first + 1 <= policy.budgets.price_repaints_per_second_per_topic, `Price repaint exceeds2Hz:${topic}`); } }
  const steadyLongTasks = longTasks.filter(row => row.observed_utc_ms >= backendObserved.started_utc_ms && row.observed_utc_ms <= backendObserved.ended_utc_ms); assert.ok(steadyLongTasks.every(row => row.duration_ms <= policy.budgets.steady_long_task_max_ms));
  const steadyDom = samples.filter(row => row.observed_utc_ms >= backendObserved.started_utc_ms && row.observed_utc_ms <= backendObserved.ended_utc_ms); assert.ok(steadyDom.some(row => row.lists.some(list => list.mounted_data_rows >= policy.budgets.steady_mounted_data_rows)), 'Actual100-data-row hot market occupancy missing');
  const clsP75 = percentile(screenshots.map(row => row.geometry.cls), .75); assert.ok(clsP75 <= policy.budgets.cls_p75);
  await page.evaluate(() => window.__kindleMarketCapture.stop()); await Promise.all([...pendingResponses]);
  await context.tracing.stop({ path: relativeFile(output, 'browser-trace.zip') }); await context.close(); context = null; await browser.close(); browser = null;
  await Promise.all([...pendingResponses]); if (resourceFailure) failures.push(resourceFailure); assert.equal(failures.length, 0, failures.join('\n')); finishedUtcMs = Date.now();
  write('result.json', { task: '4.30', observed: true, lane_measurements_passed: true, capacity_qualified: false, shared_workload_qualified: false, final_capacity_task: '9.1', bindings: config.bindings, config_sha256: args[3], trace_corpus_sha256: config.trace_corpus.sha256, device_sha256: config.browser_device.sha256, policy_sha256: sha(fs.readFileSync(new URL('./phase4-policy.json', import.meta.url))), started_utc_ms: startedUtcMs, ended_utc_ms: finishedUtcMs, steady_started_utc_ms: steadyStarted, steady_ended_utc_ms: steadyEnded, terminal_input_p95_ms: latencyP95, interaction_count: actualInteractions.length, cls_p75: clsP75, source_fill_count: sourceEvents.size, queried_fill_count: provenFills.size, route_cycles: cycles.length, control_outcome_count: outcomes.length, observation_counts: Object.fromEntries(counts), freshness_transitions: freshnessRows, responsive: screenshots, accessibility, backend: backendObserved, failures });
} catch (error) {
  failures.push(error.message); write('failure.json', { task: '4.30', observed: true, lane_measurements_passed: false, capacity_qualified: false, bindings: config.bindings, started_utc_ms: startedUtcMs, ended_utc_ms: Date.now(), failures }); process.exitCode = 1;
} finally {
  clearTimeout(deadline); if (metricsTimer) clearInterval(metricsTimer); terminateBackend();
  if (context) { await Promise.all([...pendingResponses]); await context.tracing.stop({ path: relativeFile(output, 'browser-trace.zip') }); await context.close(); }
  await browser?.close();
  for (const fd of streams.values()) { fs.fsyncSync(fd); fs.closeSync(fd); }
  let artifactBytes = 0;
  for (const name of fs.readdirSync(output)) { const file = relativeFile(output, name); if (fs.statSync(file).isFile()) { artifactBytes += fs.statSync(file).size; assert.ok(artifactBytes <= policy.budgets.artifact_max_bytes); digests.set(name, fileDigest(file)); } }
  fs.writeFileSync(relativeFile(output, 'artifact-sha256.json'), JSON.stringify(Object.fromEntries(digests), null, 2) + '\n', { flag: 'wx', mode: 0o600 });
}
