import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const sha = raw => crypto.createHash('sha256').update(raw).digest('hex');
const bind = reference => {
  assert.ok(path.isAbsolute(reference.path));
  assert.equal(fs.lstatSync(reference.path).isSymbolicLink(), false);
  const raw = fs.readFileSync(reference.path);
  assert.equal(sha(raw), reference.sha256);
  return raw;
};
const configuration = JSON.parse(bind({ path: process.argv[2], sha256: process.argv[3] }));
assert.equal(configuration.schema_version, 1);
const intervals = { '1m': ['1m', 60], '5m': ['5m', 300], '15m': ['15m', 900], '1h': ['1h', 3600], '1D': ['1d', 86400], '1W': ['1w', 604800] };
assert.deepEqual(intervals[configuration.interval_label], [configuration.interval, configuration.interval_seconds]);
assert.match(configuration.pool_address, /^0x[0-9a-f]{40}$/);
assert.ok(configuration.duration_seconds > 0 && configuration.duration_seconds <= 90000);
const playwright = bind(configuration.playwright_entry);
assert.ok(playwright.length > 0);
const { chromium } = require(configuration.playwright_entry.path);
const origin = new URL(configuration.application_origin);
assert.equal(origin.protocol, 'https:');
assert.equal(origin.username, ''); assert.equal(origin.password, '');
const oracleOrigin = new URL(configuration.api_origin);
assert.equal(oracleOrigin.protocol, 'https:');
assert.equal(oracleOrigin.username, ''); assert.equal(oracleOrigin.password, '');
const output = path.resolve(configuration.capture_output);
assert.equal(fs.existsSync(output), false);
fs.mkdirSync(output, { recursive: true, mode: 0o700 });
const raw = fs.createWriteStream(path.join(output, 'browser-capture.ndjson'), { flags: 'wx', mode: 0o600 });
let bytes = 0;
let captureFailure;
raw.on('error', error => { captureFailure = error; });
const write = async row => {
  if (captureFailure) throw captureFailure;
  const encoded = JSON.stringify({ ...row, receiver_id: configuration.receiver_id, run_id: configuration.run_id, bindings: configuration.bindings }) + '\n';
  bytes += Buffer.byteLength(encoded);
  assert.ok(bytes <= configuration.capture_max_bytes, 'Browser raw capture exceeded declared storage budget');
  if (!raw.write(encoded)) await new Promise((resolve, reject) => { const drained = () => { raw.removeListener('error', failed); resolve(); }; const failed = error => { raw.removeListener('drain', drained); reject(error); }; raw.once('drain', drained); raw.once('error', failed); });
};
bind(configuration.browser_executable);
const browser = await chromium.launch({ headless: true, executablePath: configuration.browser_executable.path });
let context;
try {
  const storageState = JSON.parse(bind(configuration.storage_state));
  context = await browser.newContext({ storageState, ignoreHTTPSErrors: false, viewport: { width: configuration.viewport.width, height: configuration.viewport.height }, hasTouch: configuration.touch });
  await context.tracing.start({ screenshots: true, snapshots: true });
  const page = await context.newPage();
  let failure;
  page.on('pageerror', error => { failure = error; });
  await page.exposeFunction('__actualChartCapture', row => write(row));
  await page.addInitScript(() => {
    window.__chartCaptureState = new Map();
    window.__chartCaptureReplay = new Map();
    window.__chartCaptureTasks = new Set();
    window.__chartCaptureError = null;
    window.addEventListener('sidiora:chart-measurement-v1', event => {
      const value = event.detail;
      if (value.kind === 'candle_state') window.__chartCaptureState.set(value.topic + ':' + value.payload.start, value);
      if (value.kind === 'replay_state') window.__chartCaptureReplay.set(value.topic, value);
      const task = window.__actualChartCapture(value).catch(error => { window.__chartCaptureError = String(error); }).finally(() => window.__chartCaptureTasks.delete(task));
      window.__chartCaptureTasks.add(task);
      if (window.__chartCaptureTasks.size > 256) window.__chartCaptureError = 'Browser capture collector backlog exceeds256';
    });
  });
  await page.goto(new URL('/token/' + configuration.pool_address, origin).href, { waitUntil: 'domcontentloaded', timeout: 30000 });
  const interval = page.getByRole('button', { name: 'Chart interval ' + configuration.interval_label, exact: true });
  await interval.click();
  await page.getByRole('button', { name: 'Inspect chart data', exact: true }).click();
  await page.getByRole('region', { name: 'Canonical candle prices and volumes', exact: true }).waitFor({ timeout: 30000 });
  await page.waitForFunction(() => [...window.__chartCaptureReplay.values()].some(row => row.replay_complete), null, { timeout: 30000 });
  const startedUtcNs = (BigInt(Date.now()) * 1000000n).toString();
  await write({ kind: 'browser_started', started_utc_ns: startedUtcNs, browser_version: browser.version(), viewport: configuration.viewport, touch: configuration.touch });
  const end = performance.now() + configuration.duration_seconds * 1000;
  while (performance.now() < end) {
    await new Promise(resolve => setTimeout(resolve, Math.min(1000, Math.max(1, end - performance.now()))));
    assert.equal(failure, undefined, 'Actual application browser error');
    if (captureFailure) throw captureFailure;
    assert.equal(await page.evaluate(() => window.__chartCaptureError), null);
  }
  const finish = await page.evaluate(async () => {
    await Promise.all([...window.__chartCaptureTasks]);
    if (window.__chartCaptureError) throw new Error(window.__chartCaptureError);
    return { candles: [...window.__chartCaptureState.values()], replay: [...window.__chartCaptureReplay.values()], captured_utc_ns: (BigInt(Math.round(performance.timeOrigin * 1000000)) + BigInt(Math.round(performance.now() * 1000000))).toString() };
  });
  assert.ok(finish.candles.length > 0);
  for (const row of finish.replay) { assert.equal(row.replay_complete, true); assert.equal(row.pending_bytes, 0); }
  const topic = 'market.candles.' + configuration.pool_address.toLowerCase() + '.' + configuration.interval;
  const candles = finish.candles.filter(row => row.topic === topic).sort((a, b) => a.payload.start.localeCompare(b.payload.start));
  assert.ok(candles.length > 0);
  const last = candles.at(-1).payload;
  const start = Date.parse(last.start), endUtc = start + configuration.interval_seconds * 1000;
  const url = new URL('/v2/pools/' + configuration.pool_address + '/candles', oracleOrigin);
  for (const [name, value] of Object.entries({ interval: configuration.interval, from: new Date(start).toISOString(), to: new Date(endUtc).toISOString(), limit: '50' })) url.searchParams.set(name, value);
  const response = await page.request.get(url.href, { headers: { 'X-Schema-Version': '2' }, timeout: 10000 });
  assert.equal(response.ok(), true); assert.equal(response.headers()['x-schema-version'], '2');
  const authoritative = await response.json();
  assert.equal(authoritative.quality, 'complete'); assert.equal(authoritative.watermark.integrity, 'healthy');
  const matched = authoritative.data.items.find(candle => candle.start === last.start);
  assert.ok(matched);
  await write({ kind: 'end_state_comparison', topic, browser_payload: last, authoritative_payload: matched, captured_utc_ns: finish.captured_utc_ns, authoritative_request_id: authoritative.requestId, replay: finish.replay });
  await page.screenshot({ path: path.join(output, 'chart-end-state.png'), fullPage: true });
  await context.tracing.stop({ path: path.join(output, 'chart-browser-trace.zip') });
  await write({ kind: 'browser_finished', started_utc_ns: startedUtcNs, ended_utc_ns: (BigInt(Date.now()) * 1000000n).toString(), duration_seconds: configuration.duration_seconds, screenshot_sha256: sha(fs.readFileSync(path.join(output, 'chart-end-state.png'))), trace_sha256: sha(fs.readFileSync(path.join(output, 'chart-browser-trace.zip'))) });
} finally {
  if (context) await context.close();
  await browser.close();
  await new Promise((resolve, reject) => { if (captureFailure) { raw.destroy(); reject(captureFailure); return; } raw.once('error', reject); raw.end(resolve); });
}
console.log(JSON.stringify({ raw_capture: path.join(output, 'browser-capture.ndjson'), sha256: sha(fs.readFileSync(path.join(output, 'browser-capture.ndjson'))), capacity_qualified: false }));
