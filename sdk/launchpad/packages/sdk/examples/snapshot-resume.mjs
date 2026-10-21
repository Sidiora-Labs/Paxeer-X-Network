import fs from 'node:fs';
import ActualWebSocket from 'ws';
import { SdkClient, Operations } from '../dist/index.js';
const config = JSON.parse(fs.readFileSync(process.env.SDK_CONNECTED_CONFIG, 'utf8'));
const session = config.accessToken ? { mode: 'bearer', token: () => config.accessToken } : undefined;
globalThis.WebSocket = class extends ActualWebSocket { constructor(url, protocols) { super(url, protocols, { origin: config.applicationOrigin }); } };
const browserFetch = (url, init) => { const headers = new Headers(init?.headers); headers.set('Origin', config.applicationOrigin); return fetch(url, { ...init, headers }); };
const client = new SdkClient({ ...config.transport, ...(session ? { session } : {}) }, browserFetch);
const api = new Operations(client);
const signal = AbortSignal.timeout(30000);
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { SnapshotStreamV2, ReconnectingStream, browserStreamTransportV2 } from '../dist/index.js';
const identities = new Map();
const stream = new SnapshotStreamV2({ transport: browserStreamTransportV2(client, { applicationOrigin: config.applicationOrigin, snapshotPath: '/v2/snapshots' }, config.authenticated), installSnapshot: snapshot => console.log(JSON.stringify({ snapshot })), applyEvent: event => { if (identities.has(event.eventId)) throw new Error('Duplicate resumed identity'); identities.set(event.eventId, event); console.log(JSON.stringify({ event })); }, onState: (state, reason) => console.error(JSON.stringify({ state, reason })) });
const connected = new ReconnectingStream(stream, config.authenticated);
async function until(predicate) { while (!predicate()) { signal.throwIfAborted(); if (connected.status.authenticationRequired) throw new Error('Authentication required'); await new Promise(resolve => setTimeout(resolve,20)); } }
async function provider(name) {
  const command = config.commands?.[name];
  if (!command?.argv?.length || !/^[0-9a-f]{64}$/.test(command.executableSha256)) throw new Error('Real restart/resume provider required');
  assert.equal(crypto.createHash('sha256').update(fs.readFileSync(command.argv[0])).digest('hex'), command.executableSha256);
  const output = await promisify(execFile)(command.argv[0], command.argv.slice(1), { timeout: 30000, signal, env: { ...process.env, ...command.env } });
  return JSON.parse(output.stdout);
}
try {
  await connected.start(config.topics, signal); await until(() => connected.status.state === 'live');
  if (process.env.SDK_RESUME === '1') {
    await provider('restartGateway');
    await connected.retry();
    const expected = await provider('fillAfterGatewayRestart');
    await until(() => connected.status.state === 'live' && identities.has(expected.eventId));
    assert.deepEqual(identities.get(expected.eventId).payload, expected.payload);
    console.log(JSON.stringify({ resumedEventId: expected.eventId, exactDecimalStrings: true }));
  }
} finally { connected.dispose(); }
