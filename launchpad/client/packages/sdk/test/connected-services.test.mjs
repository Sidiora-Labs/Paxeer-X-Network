import test from 'node:test';
import ActualWebSocket from 'ws';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import crypto from 'node:crypto';
import { spawn } from 'node:child_process';
import { SdkClient, Operations, SnapshotStreamV2, browserStreamTransportV2, ReconnectingStream, StreamTransportError, HttpError, CapabilityUnavailableError, decimalString, sequence } from '../dist/index.js';
const location = process.env.SDK_CONNECTED_CONFIG;
if (!location) throw new Error('SDK_CONNECTED_CONFIG must identify the coordinator real-service fixture');
const config = JSON.parse(fs.readFileSync(location, 'utf8'));
if (config.realm !== 'private_fixture' || !/^[0-9a-f]{64}$/.test(config.sourceSha256) || !config.commands) throw new Error('Unbound connected fixture');
if (!config.applicationOrigin?.startsWith('https://')) throw new Error('Actual HTTPS front door required');
const browserFetch = (input, init) => { const headers = new Headers(init?.headers); headers.set('Origin', config.applicationOrigin); return fetch(input, { ...init, headers }); };
const client = new SdkClient({ ...config.transport, session: { mode: 'bearer', token: () => config.accessToken } }, browserFetch);
globalThis.WebSocket = class extends ActualWebSocket { constructor(url, protocols) { super(url, protocols, { origin: config.applicationOrigin }); } };
const operations = new Operations(client);
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
async function until(predicate, timeout = 30000) { const deadline = Date.now() + timeout; while (!predicate()) { if (Date.now() >= deadline) throw new Error('Real-service condition deadline'); await pause(20); } }
async function command(name) {
  const definition = config.commands[name];
  if (!definition?.argv?.length || !/^[0-9a-f]{64}$/.test(definition.executableSha256)) throw new Error(`Missing real provider command ${name}`);
  const bytes = fs.readFileSync(definition.argv[0]);
  assert.equal(crypto.createHash('sha256').update(bytes).digest('hex'), definition.executableSha256);
  return new Promise((resolve, reject) => {
    const process = spawn(definition.argv[0], definition.argv.slice(1), { shell: false, env: { ...globalThis.process.env, ...definition.env }, stdio: ['ignore','pipe','pipe'] });
    const timer = setTimeout(() => process.kill('SIGTERM'), 30000); let output = '', error = '';
    process.stdout.on('data', bytes => { output += bytes; }); process.stderr.on('data', bytes => { error += bytes; });
    process.once('error', reject); process.once('exit', code => { clearTimeout(timer); if (code !== 0) reject(new Error(`${name} failed (${code}): ${error.slice(0,512)}`)); else { try { resolve(JSON.parse(output)); } catch (error) { reject(error); } } });
  });
}
function stream(installSnapshot, applyEvent, authenticated = config.authenticated) {
  if (typeof globalThis.WebSocket !== 'function') throw new Error('Native actual WebSocket implementation required');
  return new SnapshotStreamV2({ transport: browserStreamTransportV2(client, { applicationOrigin: config.applicationOrigin, snapshotPath: '/v2/snapshots' }, authenticated), installSnapshot, applyEvent });
}
test('generated decimal boundaries retain exact strings', () => {
  for (const value of ['9007199254740993', ((1n << 256n)-1n).toString()]) assert.equal(JSON.parse(JSON.stringify(decimalString(value))), value);
  assert.equal(sequence('9007199254740993'), '9007199254740993');
});
test('real snapshot/ack fill race applies one canonical event', async () => {
  const barrier = await command('armSnapshotAckBarrier');
  assert.equal(barrier.armed, true);
  const received = [];
  const reader = stream(() => {}, event => received.push(event));
  try {
    const pending = reader.start(config.topics, AbortSignal.timeout(30000));
    const expected = await command('fillDuringSnapshotAck');
    await pending; await until(() => reader.state === 'live' && received.some(event => event.eventId === expected.eventId));
    assert.equal(received.filter(event => event.eventId === expected.eventId).length, 1);
    const event = received.find(event => event.eventId === expected.eventId);
    assert.deepEqual(event.payload, expected.payload);
  } finally { reader.close(); }
});
test('real wrong-origin ticket remains typed', async () => {
  const { streamTopicFilterDigest } = await import('../dist/index.js');
  await assert.rejects(client.requestV2('/v2/stream/tickets','StreamTicketIssueResponse',{ method:'POST', service:'identity', authenticated:true, body:{ topics:config.topics,schemaMajor:2,filterDigest:await streamTopicFilterDigest(config.topics),origin:config.wrongOrigin,endpoint:'/v2/stream'} }), error => error instanceof HttpError && error.status === 403 && typeof error.code === 'string');
});
test('actually issued ticket expiry remains typed', async () => {
  const adapter = browserStreamTransportV2(client, { applicationOrigin: config.applicationOrigin, snapshotPath: '/v2/snapshots' }, true);
  const ticket = await adapter.ticket(config.topics, AbortSignal.timeout(10000));
  const expiresAt = Date.parse(ticket.expiresAt);
  assert.ok(expiresAt > Date.now() && expiresAt <= Date.now()+30000);
  await pause(Math.max(0, expiresAt-Date.now())+10);
  await assert.rejects(adapter.connect({ signal:AbortSignal.timeout(10000), ticket, receive:()=>{}, disconnected:()=>{} }), error => error instanceof StreamTransportError && error.code === 'AUTH_EXPIRED');
});
test('unknown capability never becomes an empty collection', async () => {
  await assert.rejects(client.requestV2('/v2/capability-that-is-unavailable','TradesResponse'), CapabilityUnavailableError);
});
test('route-obsolete real delayed response is aborted', async () => {
  await command('armDelayedTokenA');
  const controller = new AbortController(); let installedA = false;
  const obsolete = operations.call('IF_POOL_DETAIL',{path:{poolAddress:config.tokenAPool}},{signal:controller.signal}).then(() => { installedA = true; });
  const rejected = assert.rejects(obsolete);
  controller.abort();
  const current = await operations.call('IF_POOL_DETAIL',{path:{poolAddress:config.tokenBPool}});
  await rejected; assert.equal(installedA,false); assert.ok(current.data);
});
test('real gateway restart resumes durable identities', async () => {
  const identities = new Map();
  const reader = stream(() => {}, event => { assert.equal(identities.has(event.eventId),false); identities.set(event.eventId,event); });
  const reconnect = new ReconnectingStream(reader,config.authenticated);
  try {
    await reconnect.start(config.topics); await until(() => reader.state === 'live');
    await command('restartGateway');
    const expected = await command('fillAfterGatewayRestart');
    await until(() => reader.state === 'live' && identities.has(expected.eventId));
    assert.deepEqual(identities.get(expected.eventId).payload, expected.payload);
  } finally { reconnect.dispose(); }
});
test('actual public visibility cancellation keeps one snapshot operation and no detached success', async () => {
  let active = 0, peak = 0, completed = 0;
  const requests = [];
  const observedFetch = async (input, init) => {
    const url = new URL(input);
    const snapshot = url.pathname === '/v2/snapshots';
    let released = false;
    const release = () => { if (snapshot && !released) { released = true; active--; } };
    if (snapshot) { active++; peak = Math.max(peak, active); }
    init.signal.addEventListener('abort', release, { once: true });
    requests.push({ url: url.toString(), authenticated: new Headers(init.headers).has('Authorization') });
    try { return await browserFetch(input, init); }
    finally { release(); init.signal.removeEventListener('abort', release); }
  };
  const observedClient = new SdkClient(config.transport, observedFetch);
  const reader = new SnapshotStreamV2({ transport: browserStreamTransportV2(observedClient, { applicationOrigin: config.applicationOrigin, snapshotPath: '/v2/snapshots' }, false), installSnapshot: () => { completed++; }, applyEvent: () => {} });
  const connection = new ReconnectingStream(reader, false);
  try {
    await connection.start(config.publicTopics); await until(() => reader.state === 'live');
    const baseline = completed;
    const attempts = Array.from({ length: 10 }, () => connection.restoreVisibility());
    await Promise.all(attempts); await until(() => reader.state === 'live');
    assert.equal(completed, baseline + 1); assert.equal(active, 0); assert.equal(peak, 1);
    const telemetry = JSON.stringify({ requests, status: connection.status });
    assert.equal(telemetry.includes(config.accessToken), false);
    assert.equal(requests.some(request => request.authenticated), false);
    connection.stop(); const stopped = completed;
    await connection.restoreVisibility(); await connection.retry();
    assert.equal(completed, stopped); assert.equal(reader.hasResumeCursor, false);
  } finally { connection.dispose(); }
});
test('real private logout destroys cursor and prevents reconnect delivery', async () => {
  const delivered = [];
  const reader = stream(() => {}, event => delivered.push(event), true);
  const connection = new ReconnectingStream(reader, true);
  try {
    await connection.start(config.privateTopics); await until(() => reader.state === 'live');
    assert.equal(reader.hasResumeCursor, true);
    connection.logout(); const before = delivered.length;
    assert.equal(reader.hasResumeCursor, false); assert.equal(connection.status.authenticationRequired, true);
    await connection.restoreVisibility(); await connection.retry();
    assert.equal(delivered.length, before); assert.equal(reader.state, 'closed');
    assert.equal(JSON.stringify(connection.status).includes(config.accessToken), false);
  } finally { connection.dispose(); }
});
test('delayed real tokenA snapshot cannot overwrite tokenB after owner disposal', async () => {
  const armed = await command('armDelayedSnapshotTokenA'); assert.equal(armed.armed, true);
  if (!Array.isArray(config.tokenATopics) || !Array.isArray(config.tokenBTopics)) throw new Error('Actual tokenA/tokenB snapshot topics required');
  const installed = [], states = [];
  const reader = new SnapshotStreamV2({ transport: browserStreamTransportV2(client, { applicationOrigin: config.applicationOrigin, snapshotPath: '/v2/snapshots' }, false), installSnapshot: snapshot => installed.push(snapshot), applyEvent: () => {}, onState: state => states.push(state) });
  const ownerA = new AbortController();
  try {
    const pendingA = reader.start(config.tokenATopics, ownerA.signal);
    const waiting = await command('waitDelayedSnapshotTokenA'); assert.equal(waiting.waiting, true);
    ownerA.abort();
    await reader.start(config.tokenBTopics); await until(() => reader.state === 'live');
    const count = installed.length, statusCount = states.length;
    const released = await command('releaseDelayedSnapshotTokenA'); assert.equal(released.released, true);
    await pendingA;
    assert.equal(installed.length, count); assert.equal(states.length, statusCount);
    assert.equal(count, 1); assert.deepEqual(installed[0].topics.map(topic => topic.topic).sort(), [...config.tokenBTopics].sort());
  } finally { ownerA.abort(); reader.close(); }
});
test('real73hour retained history resnapshots public and reauthenticates revoked private scope', async () => {
  let publicSnapshots = 0;
  const publicReader = stream(() => { publicSnapshots++; }, () => {}, false);
  const privateReader = stream(() => {}, () => {}, true);
  const publicConnection = new ReconnectingStream(publicReader,false);
  const privateConnection = new ReconnectingStream(privateReader,true);
  try {
    await publicConnection.start(config.publicTopics); await privateConnection.start(config.privateTopics);
    await until(() => publicReader.state === 'live' && privateReader.state === 'live');
    const before = publicSnapshots;
    const evidence = await command('expireCursor73HoursAndRevokeSession');
    assert.equal(evidence.cursorAgeHours,73); assert.equal(evidence.sessionRevoked,true);
    await publicConnection.restoreVisibility(); await privateConnection.restoreVisibility();
    await until(() => publicReader.state === 'live' && publicSnapshots > before && privateConnection.status.authenticationRequired);
    await assert.rejects(operations.call('IF_SESSIONS',{}), error => error instanceof HttpError && error.status === 401);
  } finally { publicConnection.dispose(); privateConnection.dispose(); }
});
