import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import WebSocket from 'ws';
import { decodeV2Contract } from '../dist/index.js';
const digest = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
const configPath = process.env.SDK_PHASE4_CONFIG;
if (!configPath || !process.env.SDK_PHASE4_RUN_DIR) throw new Error('BOUND_PHASE4_INPUTS_REQUIRED');
const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
const fixture = JSON.parse(fs.readFileSync(process.env.SDK_CONNECTED_CONFIG, 'utf8'));
assert.equal(fixture.realm, 'private_fixture'); assert.equal(fixture.sourceSha256, config.sourceSha256);
const output = path.resolve(process.env.SDK_PHASE4_RUN_DIR);
const workloadBytes = fs.readFileSync(config.workload.path), deviceBytes = fs.readFileSync(config.device.path);
assert.equal(digest(workloadBytes), config.workload.sha256); assert.equal(digest(deviceBytes), config.device.sha256);
const workload = JSON.parse(workloadBytes), device = JSON.parse(deviceBytes);
assert.equal(workload.frozen, true); assert.equal(device.classification, 'midrange'); assert.equal(device.physicalDevice, true);
assert.ok(typeof device.deviceId === 'string' && device.deviceId.length > 0 && typeof device.browserProduct === 'string' && typeof device.userAgent === 'string');
assert.ok(Number.isSafeInteger(device.hardwareConcurrency) && device.hardwareConcurrency > 0 && Number.isFinite(device.deviceMemory) && device.deviceMemory > 0);
assert.ok(Number.isFinite(device.screenWidth) && device.screenWidth > 0 && Number.isFinite(device.screenHeight) && device.screenHeight > 0 && Number.isFinite(device.devicePixelRatio) && device.devicePixelRatio > 0);
assert.ok(Number.isInteger(workload.minimumFrames) && workload.minimumFrames >= 100);
assert.ok(Number.isInteger(workload.maximumFrames) && workload.maximumFrames >= workload.minimumFrames);
assert.ok(Number.isFinite(workload.maximumInterEventGapMs) && workload.maximumInterEventGapMs > 0 && workload.maximumInterEventGapMs < workload.durationMs);
assert.ok(Number.isInteger(workload.eventCount) && workload.eventCount >= 100);
assert.ok(Number.isInteger(workload.maximumEvents) && workload.maximumEvents >= workload.eventCount + 1);
assert.ok(Number.isInteger(workload.durationMs) && workload.durationMs >= 1000 && workload.durationMs <= 600000);
const bundle = fs.readFileSync(config.browserBundle.path, 'utf8'); assert.equal(digest(bundle), config.browserBundle.sha256);
const cdpUrl = new URL(config.cdpUrl);
assert.ok(['ws:', 'wss:'].includes(cdpUrl.protocol) && !cdpUrl.username && !cdpUrl.password && !cdpUrl.search && !cdpUrl.hash);
function archive(name, value) { const bytes = typeof value === 'string' ? value : JSON.stringify(value, null, 2) + '\n'; if (fixture.accessToken && bytes.includes(fixture.accessToken)) throw new Error('CREDENTIAL_IN_ARTIFACT'); fs.writeFileSync(path.join(output, name), bytes, { mode: 0o600 }); return digest(bytes); }
async function provider(name) {
  const command = config.commands?.[name];
  if (!command?.argv?.length || !/^[0-9a-f]{64}$/.test(command.executableSha256)) throw new Error(`REAL_PROVIDER_REQUIRED:${name}`);
  assert.equal(digest(fs.readFileSync(command.argv[0])), command.executableSha256);
  if (!command.sourceInputs || !Object.keys(command.sourceInputs).length) throw new Error('PROVIDER_SOURCE_INPUTS_REQUIRED');
  for (const [input, expected] of Object.entries(command.sourceInputs)) assert.equal(digest(fs.readFileSync(input)), expected);
  for (const argument of command.argv.slice(1)) if (fs.existsSync(argument) && fs.statSync(argument).isFile()) assert.equal(digest(fs.readFileSync(argument)), command.sourceInputs[argument], 'UNBOUND_PROVIDER_ARGUMENT');
  const result = await promisify(execFile)(command.argv[0], command.argv.slice(1), { timeout: 600000, maxBuffer: 64 * 1024 * 1024, env: { ...process.env, ...command.env, SDK_CONNECTED_CONFIG: process.env.SDK_CONNECTED_CONFIG, SDK_STREAM_WORKLOAD_PATH: config.workload.path, SDK_STREAM_WORKLOAD_SHA256: config.workload.sha256, SDK_REFERENCE_DEVICE_SHA256: config.device.sha256 } });
  return JSON.parse(result.stdout);
}
function verifyReceipts(result) {
  assert.ok(Array.isArray(result.receiptRefs) && result.receiptRefs.length > 0, 'ACTUAL_PROVIDER_RECEIPTS_REQUIRED');
  for (const receipt of result.receiptRefs) {
    assert.equal(receipt.sourceSha256, fixture.sourceSha256);
    assert.ok(typeof receipt.path === 'string' && /^[0-9a-f]{64}$/.test(receipt.sha256));
    assert.equal(digest(fs.readFileSync(receipt.path)), receipt.sha256, 'PROVIDER_RECEIPT_CHANGED');
  }
}
class CDP {
  pending = new Map(); observers = new Map(); next = 0;
  constructor(url) { this.socket = new WebSocket(url); this.socket.on('message', bytes => { const message = JSON.parse(bytes.toString()); if (message.id) { const entry = this.pending.get(message.id); if (!entry) return; this.pending.delete(message.id); clearTimeout(entry.timer); message.error ? entry.reject(new Error(`CDP_ERROR:${message.error.code}`)) : entry.resolve(message.result); } else { for (const observer of this.observers.get(message.method) ?? []) observer(message.params); } }); this.socket.on('close', () => { for (const entry of this.pending.values()) { clearTimeout(entry.timer); entry.reject(new Error('CDP_CLOSED')); } this.pending.clear(); }); }
  ready() { return new Promise((resolve, reject) => { const timer = setTimeout(() => reject(new Error('CDP_CONNECTION_DEADLINE')), 30000); this.socket.once('open', () => { clearTimeout(timer); resolve(); }); this.socket.once('error', () => { clearTimeout(timer); reject(new Error('CDP_CONNECTION_FAILED')); }); }); }
  send(method, params = {}) { return new Promise((resolve, reject) => { const id = ++this.next; const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(`CDP_DEADLINE:${method}`)); }, 30000); this.pending.set(id, { resolve, reject, timer }); this.socket.send(JSON.stringify({ id, method, params })); }); }
  once(method) { return new Promise((resolve, reject) => { const timer = setTimeout(() => { this.observers.get(method)?.delete(observer); reject(new Error(`CDP_EVENT_DEADLINE:${method}`)); }, 30000); const observer = value => { clearTimeout(timer); this.observers.get(method).delete(observer); resolve(value); }; if (!this.observers.has(method)) this.observers.set(method, new Set()); this.observers.get(method).add(observer); }); }
  async evaluate(expression) { const result = await this.send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }); if (result.exceptionDetails) throw new Error('BROWSER_EVALUATION_FAILED'); return result.result.value; }
  async call(method, argument) { const result = await this.send('Runtime.callFunctionOn', { objectId: this.globalObject, functionDeclaration: 'function(method, argument) { return globalThis.L24Measurement[method](argument); }', arguments: [{ value: method }, { value: argument }], returnByValue: true, awaitPromise: true }); if (result.exceptionDetails) throw new Error(`BROWSER_METHOD_FAILED:${method}`); return result.result.value; }
  close() { if (this.socket.readyState === WebSocket.CONNECTING) this.socket.terminate(); else this.socket.close(); }
}
function canonical(value) { if (Array.isArray(value)) return value.map(canonical); if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([key, child]) => [key, canonical(child)])); return value; }
async function until(predicate) { const deadline = Date.now() + 60000; for (;;) { const state = await browser.call('state'); if (state.failure) throw new Error(state.failure); if (predicate(state)) return state; if (Date.now() >= deadline) throw new Error('BROWSER_CONDITION_DEADLINE'); await new Promise(resolve => setTimeout(resolve, 50)); } }
function frameMath(trace, requiredFrames, workload) {
  const markers = trace.traceEvents.filter(event => /^L24\.frame\.\d+$/.test(event.name) && Number.isFinite(event.ts)).sort((a, b) => a.ts - b.ts);
  assert.ok(markers.length >= requiredFrames + 1, 'INSUFFICIENT_RAW_FRAME_MARKERS');
  const thread = { pid: markers[0].pid, tid: markers[0].tid };
  assert.ok(markers.every(marker => marker.pid === thread.pid && marker.tid === thread.tid));
  const renderer = trace.traceEvents.find(event => event.name === 'thread_name' && event.pid === thread.pid && event.tid === thread.tid);
  assert.equal(renderer?.args?.name, 'CrRendererMain');
  const tasks = trace.traceEvents.filter(event => event.pid === thread.pid && event.tid === thread.tid && event.ph === 'X' && /^(?:RunTask|ThreadControllerImpl::RunTask|ThreadController::RunTask)$/.test(event.name) && Number.isFinite(event.ts) && Number.isFinite(event.dur) && event.dur >= 0).map(event => [event.ts, event.ts + event.dur]).sort((a, b) => a[0] - b[0]);
  assert.ok(tasks.length > 0, 'MISSING_RENDERER_MAIN_TASKS');
  const merged = [];
  for (const [begin, end] of tasks) { const previous = merged.at(-1); if (previous && begin <= previous[1]) previous[1] = Math.max(end, previous[1]); else merged.push([begin, end]); }
  const arrivals = trace.traceEvents.filter(event => /^L24\.event\.\d+$/.test(event.name) && event.pid === thread.pid && event.tid === thread.tid && Number.isFinite(event.ts)).sort((a, b) => a.ts - b.ts);
  assert.equal(arrivals.length, workload.eventCount, 'IMMUTABLE_WORKLOAD_ARRIVAL_COUNT');
  const workloadBeginUs = arrivals[0].ts, workloadEndUs = arrivals.at(-1).ts;
  assert.ok((workloadEndUs - workloadBeginUs) / 1000 >= workload.durationMs, 'LOADED_BROWSER_DURATION_TOO_SHORT');
  for (let index = 1; index < arrivals.length; index++) assert.ok((arrivals[index].ts - arrivals[index - 1].ts) / 1000 <= workload.maximumInterEventGapMs, 'FROZEN_WORKLOAD_TRAFFIC_GAP');
  const samples = [];
  let taskIndex = 0;
  for (let index = 1; index < markers.length; index++) { const begin = markers[index - 1].ts, end = markers[index].ts; assert.ok(end > begin); if (begin < workloadBeginUs || end > workloadEndUs) continue; let work = 0; while (taskIndex < merged.length && merged[taskIndex][1] <= begin) taskIndex++; for (let task = taskIndex; task < merged.length && merged[task][0] < end; task++) work += Math.max(0, Math.min(end, merged[task][1]) - Math.max(begin, merged[task][0])); samples.push({ frame: index - 1, beginUs: begin, endUs: end, frameWorkMs: work / 1000, intervalMs: (end - begin) / 1000 }); }
  assert.ok(samples.length >= requiredFrames, 'INSUFFICIENT_LOADED_FRAME_SAMPLES');
  const sorted = samples.map(sample => sample.frameWorkMs).sort((a, b) => a - b);
  const p95 = sorted[Math.ceil(sorted.length * 0.95) - 1];
  return { renderer: thread, samples, method: 'nearest_rank_p95_union_of_renderer_RunTask_intervals_per_actual_frame', p95Ms: p95, boundMs: 8 };
}
const browser = new CDP(cdpUrl.toString());
let tracing = false, tracePath, summary, failureRecord;
async function captureTrace() {
  const complete = browser.once('Tracing.tracingComplete');
  await browser.send('Tracing.end'); const capture = await complete; tracing = false;
  let traceBytes = '';
  for (;;) { const chunk = await browser.send('IO.read', { handle: capture.stream, size: 1024 * 1024 }); traceBytes += chunk.base64Encoded ? Buffer.from(chunk.data, 'base64').toString('utf8') : chunk.data; if (Buffer.byteLength(traceBytes) > 256 * 1024 * 1024) throw new Error('TRACE_CAPTURE_BOUND'); if (chunk.eof) break; }
  await browser.send('IO.close', { handle: capture.stream });
  const sensitive = await browser.call('sensitiveValues'); let credentialLeaks = 0;
  for (const secret of sensitive) { if (typeof secret !== 'string' || secret.length === 0) continue; if (traceBytes.includes(secret)) { credentialLeaks++; traceBytes = traceBytes.split(secret).join('[REDACTED]'); } }
  tracePath = archive('browser-trace.json', traceBytes);
  return { traceBytes, sensitive, credentialLeaks };
}
try {
  await browser.ready(); await browser.send('Runtime.enable');
  const global = await browser.send('Runtime.evaluate', { expression: 'globalThis' }); browser.globalObject = global.result.objectId;
  assert.equal(await browser.evaluate('location.origin'), fixture.applicationOrigin);
  const actualVersion = await browser.send('Browser.getVersion');
  assert.equal(actualVersion.product, device.browserProduct); assert.equal(actualVersion.userAgent, device.userAgent);
  const attestation = await provider('attestReferenceDevice'); verifyReceipts(attestation);
  assert.equal(attestation.deviceProfileSha256, config.device.sha256); assert.equal(attestation.deviceId, device.deviceId); assert.equal(attestation.physicalDevice, true); assert.equal(attestation.cdpTargetUrl, config.cdpUrl); assert.ok(Array.isArray(attestation.receiptRefs) && attestation.receiptRefs.length);
  await browser.evaluate(bundle);
  await browser.call('start', { transport: fixture.transport, applicationOrigin: fixture.applicationOrigin, accessToken: fixture.accessToken, authorizationScope: fixture.authorizationScope, publicTopics: fixture.publicTopics, privateTopics: fixture.privateTopics, tradeTopics: fixture.tradeTopics, candleTopics: fixture.candleTopics, maximumEvents: workload.maximumEvents, maximumFrames: workload.maximumFrames });
  const initial = await until(state => state.publicStatus?.state === 'live' && state.privateStatus?.state === 'live' && state.subscriptions === 2);
  const accessibility = await browser.send('Accessibility.getFullAXTree');
  assert.ok(accessibility.nodes.some(node => node.role?.value === 'status' && node.name?.value === 'Stream observer' && node.ignored === false), 'OBSERVER_STATUS_ACCESSIBILITY_UNAVAILABLE');
  assert.equal(initial.device.userAgent, device.userAgent); assert.equal(initial.device.hardwareConcurrency, device.hardwareConcurrency); assert.equal(initial.device.deviceMemory, device.deviceMemory); assert.equal(initial.device.width, device.screenWidth); assert.equal(initial.device.height, device.screenHeight); assert.equal(initial.device.devicePixelRatio, device.devicePixelRatio);
  await browser.send('Tracing.start', { categories: 'toplevel,devtools.timeline,disabled-by-default-devtools.timeline,blink.user_timing', transferMode: 'ReturnAsStream' }); tracing = true;
  await browser.call('begin');
  const producer = await provider('emitFrozenStreamWorkload'); verifyReceipts(producer);
  assert.equal(producer.workloadSha256, config.workload.sha256); assert.equal(producer.sourceSha256, fixture.sourceSha256); assert.ok(Array.isArray(producer.receiptRefs) && producer.receiptRefs.length); assert.equal(producer.events.length, workload.eventCount);
  assert.ok(Number.isFinite(producer.startedAtEpochMs) && Number.isFinite(producer.completedAtEpochMs) && producer.completedAtEpochMs - producer.startedAtEpochMs >= workload.durationMs, 'FROZEN_WORKLOAD_DURATION_NOT_OBSERVED');
  for (const event of producer.events) { decodeV2Contract('StreamEvent', event); assert.ok(event.topic.startsWith('market.trades.'), 'REFERENCE_IMMUTABLE_FILL_REQUIRED'); }
  assert.equal(new Set(producer.events.map(event => event.eventId)).size, producer.events.length);
  await until(state => state.arrivals.length === workload.eventCount && state.frames.length >= workload.minimumFrames + 1);
  await browser.call('end');
  const restart = await provider('restartGateway'); verifyReceipts(restart); assert.ok(Array.isArray(restart.receiptRefs) && restart.receiptRefs.length);
  const postRestart = await provider('fillAfterGatewayRestart'); verifyReceipts(postRestart); decodeV2Contract('StreamEvent', postRestart.event);
  assert.ok(postRestart.event.topic.startsWith('market.trades.'));
  const expectedEvents = [...producer.events, postRestart.event];
  assert.equal(new Set(expectedEvents.map(event => event.eventId)).size, expectedEvents.length);
  await browser.call('retry');
  const reference = await provider('readStreamReference'); verifyReceipts(reference);
  assert.equal(reference.sourceSha256, fixture.sourceSha256); assert.ok(Array.isArray(reference.receiptRefs) && reference.receiptRefs.length); decodeV2Contract('TopicSnapshot', reference.snapshot);
  const reconstructed = await until(state => state.publicStatus?.state === 'live' && expectedEvents.every(expected => state.events.some(event => event.eventId === expected.eventId)) && reference.snapshot.topics.every(topic => state.latest.some(entry => entry.topic === topic.topic && entry.sequence === topic.sequence)));
  const baselineIdentities = new Set(initial.events.map(event => event.eventId));
  assert.equal(reconstructed.events.filter(event => !baselineIdentities.has(event.eventId)).length, expectedEvents.length, 'UNEXPECTED_OR_MISSING_RECONSTRUCTED_FILLS');
  for (const expected of expectedEvents) assert.deepEqual(canonical(reconstructed.events.find(event => event.eventId === expected.eventId)), canonical(expected));
  assert.deepEqual(canonical([...reconstructed.latest].sort((a, b) => a.topic.localeCompare(b.topic))), canonical([...reference.snapshot.topics].sort((a, b) => a.topic.localeCompare(b.topic))));
  assert.ok(reconstructed.candleCallbacks > 0 && reconstructed.observerErrors > 0);
  await browser.call('remount'); await until(state => state.subscriptions === 2);
  const { traceBytes, sensitive, credentialLeaks } = await captureTrace();
  assert.equal(credentialLeaks, 0, 'CREDENTIAL_IN_CAPTURED_TELEMETRY');
  const metric = frameMath(JSON.parse(traceBytes), workload.minimumFrames, workload);
  const frameMathSha256 = archive('frame-math.json', metric);
  assert.ok(Number.isFinite(metric.p95Ms) && metric.p95Ms < 8, `FRAME_WORK_P95_EXCEEDED:${metric.p95Ms}`);
  const beforeExpiry = await browser.call('state');
  const background = await provider('backgroundReferenceTab'); verifyReceipts(background);
  assert.equal(await browser.evaluate('document.visibilityState'), 'hidden', 'ACTUAL_BACKGROUND_VISIBILITY_REQUIRED');
  const expiry = await provider('expireCursor73HoursAndRevokeSession'); verifyReceipts(expiry); assert.equal(expiry.cursorAgeHours, 73); assert.equal(expiry.sessionRevoked, true); assert.ok(Number.isFinite(Date.parse(expiry.cursorIssuedAt)) && Number.isFinite(Date.parse(expiry.observedAt)) && Date.parse(expiry.observedAt) - Date.parse(expiry.cursorIssuedAt) >= 73 * 3600000, 'ACTUAL_RETAINED_CURSOR_AGE_REQUIRED'); assert.ok(Array.isArray(expiry.receiptRefs) && expiry.receiptRefs.length);
  const foreground = await provider('foregroundReferenceTab'); verifyReceipts(foreground);
  assert.equal(await browser.evaluate('document.visibilityState'), 'visible', 'ACTUAL_FOREGROUND_VISIBILITY_REQUIRED');
  await browser.call('restore');
  const final = await until(state => state.publicStatus?.state === 'live' && state.publicSnapshots > beforeExpiry.publicSnapshots && state.privateStatus?.authenticationRequired);
  assert.equal(final.privateHasCursor, false);
  const privateCount = final.privateDeliveries;
  await browser.call('restore');
  const stable = await browser.call('state'); assert.equal(stable.privateDeliveries, privateCount);
  const browserArtifact = { initial, reconstructed, final, stable, device, workload, reference, producer, restart, postRestart, expiry, background, foreground, attestation, browserVersion: actualVersion, accessibility }; 
  const serialized = JSON.stringify(browserArtifact);
  assert.ok(sensitive.every(secret => !secret || !serialized.includes(secret)), 'CREDENTIAL_IN_BROWSER_STATE');
  summary = { task: '4.24', scope: 'lane_browser_stream_measurement', sharedLoadAcceptanceDeferredTo: '9.1', sourceSha256: fixture.sourceSha256, workloadSha256: config.workload.sha256, deviceProfileSha256: config.device.sha256, browserBundleSha256: config.browserBundle.sha256, traceSha256: tracePath, rawStateSha256: archive('browser-state.json', browserArtifact), frameMathSha256, p95Ms: metric.p95Ms, immutableEvents: reconstructed.events.length, exitCode: 0 };
  archive('measurement-result.json', summary);
 } catch (error) {
  const message = typeof error?.message === 'string' ? error.message.split('\n')[0] : '';
  failureRecord = { task: '4.24', sourceSha256: fixture.sourceSha256, traceSha256: tracePath ?? null, reason: /^[A-Z][A-Z0-9_:.-]{0,255}$/.test(message) ? message : error instanceof assert.AssertionError ? 'MEASUREMENT_ASSERTION_FAILED' : 'MEASUREMENT_INPUT_OR_RUNTIME_FAILED', exitCode: 1 };
  process.exitCode = 1;
} finally {
  try { if (tracing) await captureTrace(); } catch { if (failureRecord) failureRecord.traceCaptureFailed = true; process.exitCode = 1; }
  if (failureRecord) { failureRecord.traceSha256 = tracePath ?? null; archive('measurement-failure.json', failureRecord); }
  try { if (browser.globalObject) { const cleanup = await browser.call('dispose'); assert.equal(cleanup.subscriptions, 0); assert.equal(cleanup.topics, 0); } } catch { process.exitCode = 1; if (summary) { summary.exitCode = 1; summary.cleanupFailed = true; archive('measurement-result.json', summary); } }
  browser.close();
}
if (summary && process.exitCode !== 1) process.stdout.write(JSON.stringify(summary) + '\n');
