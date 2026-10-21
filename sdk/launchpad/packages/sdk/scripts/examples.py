#!/usr/bin/env python3
import argparse
import hashlib
import json
import pathlib


def emit(root):
    contracts = root / 'spec/production-system/integration/contracts/v2/openapi.json'
    document = json.loads(contracts.read_bytes())
    operations = {operation['operationId'] for methods in document['paths'].values() for method, operation in methods.items() if method in ('get','post','patch','put','delete')}
    required = {'IF_TOPIC_SNAPSHOT_READ','IF_STREAM_TICKET_ISSUE','IF_TRADES'}
    if not required.issubset(operations): raise ValueError('Selected contract lacks required example operations')
    common = "import fs from 'node:fs';\nimport ActualWebSocket from 'ws';\nimport { SdkClient, Operations } from '../dist/index.js';\nconst config = JSON.parse(fs.readFileSync(process.env.SDK_CONNECTED_CONFIG, 'utf8'));\nconst session = config.accessToken ? { mode: 'bearer', token: () => config.accessToken } : undefined;\nglobalThis.WebSocket = class extends ActualWebSocket { constructor(url, protocols) { super(url, protocols, { origin: config.applicationOrigin }); } };\nconst browserFetch = (url, init) => { const headers = new Headers(init?.headers); headers.set('Origin', config.applicationOrigin); return fetch(url, { ...init, headers }); };\nconst client = new SdkClient({ ...config.transport, ...(session ? { session } : {}) }, browserFetch);\nconst api = new Operations(client);\nconst signal = AbortSignal.timeout(30000);\n"
    files = {
        'pagination.mjs': common + "let cursor;\nconst identities = new Set();\nfor (let page = 0; page < 100; page++) {\n  const response = await api.call('IF_TRADES', { path: { poolAddress: config.poolAddress }, query: { limit: 100, ...(cursor ? { cursor } : {}) } }, { signal });\n  for (const trade of response.data.items) { if (identities.has(trade.eventId)) throw new Error('Duplicate paginated trade'); identities.add(trade.eventId); console.log(JSON.stringify(trade)); }\n  if (!response.nextCursor) break;\n  if (response.nextCursor === cursor) throw new Error('Pagination cursor did not advance');\n  cursor = response.nextCursor;\n  if (page === 99) throw new Error('Pagination bound exceeded');\n}\n",
        'snapshot-resume.mjs': common + """import assert from 'node:assert/strict';
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
"""
    }
    output = root / 'client/packages/sdk/examples'; output.mkdir(parents=True,exist_ok=True)
    for name, source in files.items(): (output / name).write_text(source)
    return {'openapiSha256': hashlib.sha256(contracts.read_bytes()).hexdigest(), 'outputs': {name:hashlib.sha256(source.encode()).hexdigest() for name,source in files.items()}}

if __name__ == '__main__':
    parser = argparse.ArgumentParser(); parser.add_argument('--root', type=pathlib.Path, default=pathlib.Path(__file__).resolve().parents[4]); args = parser.parse_args()
    print(json.dumps(emit(args.root),sort_keys=True))
