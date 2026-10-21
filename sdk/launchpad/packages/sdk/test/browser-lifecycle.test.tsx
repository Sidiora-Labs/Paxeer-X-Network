import { StrictMode, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import { test, expect } from 'vitest';
import fs from 'node:fs';
import crypto from 'node:crypto';
import ActualWebSocket from 'ws';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { CanonicalDataStream } from '../../../../../apps/launchpad/web/src/core/realtime/data-stream';
import { useCanonicalDataStream } from '../../../../../apps/launchpad/web/src/core/realtime/use-data-stream';
import { SdkClient } from '../src/transport';
const path = process.env.SDK_CONNECTED_CONFIG;
if (!path) throw new Error('Real connected fixture required for React lifecycle qualification');
const config = JSON.parse(fs.readFileSync(path,'utf8'));
globalThis.WebSocket = class extends ActualWebSocket { constructor(url: string | URL, protocols?: string | string[]) { super(url, protocols, { origin: config.applicationOrigin }); } } as unknown as typeof WebSocket;
const browserFetch: typeof fetch = (input, init) => { const headers = new Headers(init?.headers); headers.set('Origin', config.applicationOrigin); return fetch(input, { ...init, headers }); };
async function produce(name: string): Promise<{ eventIds: string[] }> {
  const command = config.commands[name];
  if (!command?.argv?.length || !/^[0-9a-f]{64}$/.test(command.executableSha256)) throw new Error('Bound real producer required');
  expect(crypto.createHash('sha256').update(fs.readFileSync(command.argv[0])).digest('hex')).toBe(command.executableSha256);
  const output = await promisify(execFile)(command.argv[0], command.argv.slice(1), { timeout: 30000, env: { ...process.env, ...command.env } });
  return JSON.parse(output.stdout);
}
const pause = (ms: number) => new Promise(resolve => setTimeout(resolve,ms));
test('StrictMode owns one logical subscription and preserves100real fills despite throwing observer', async () => {
  const stream = new CanonicalDataStream(new SdkClient({...config.transport,session:{mode:'bearer',token:()=>config.accessToken}}, browserFetch),{applicationOrigin:config.applicationOrigin,snapshotPath:'/v2/snapshots'},config.authenticated,config.authorizationScope);
  const identities = new Set<string>(); let candleCallbacks = 0;
  const immutable = stream.subscribe({topics:config.tradeTopics,installSnapshot:()=>{},onEvent:event=>identities.add(event.eventId)});
  const element = document.createElement('div'); document.body.appendChild(element); const root=createRoot(element);
  function CandleConsumer() { useCanonicalDataStream(stream,{topics:config.candleTopics,installSnapshot:()=>{},onEvent:()=>{candleCallbacks++;throw new Error('Consumer exception is isolated');}}); return null; }
  try {
    await act(async()=>root.render(createElement(StrictMode,null,createElement(CandleConsumer))));
    expect(stream.subscriptionCount).toBe(2);
    const deadline=Date.now()+30000; while(stream.status.state!=='live'){if(Date.now()>deadline)throw new Error('Real stream not live');await pause(20);}
    const expected=(await produce('emit100Fills')).eventIds; expect(new Set(expected).size).toBe(100);
    const end=Date.now()+30000;while(expected.some(id=>!identities.has(id))){if(Date.now()>end)throw new Error('Lost immutable fill');await pause(20);}
    expect(identities.size).toBe(100); expect(candleCallbacks).toBeGreaterThan(0);
    await act(async()=>root.unmount()); expect(stream.subscriptionCount).toBe(1);
    const remounted = createRoot(element); await act(async()=>remounted.render(createElement(StrictMode,null,createElement(CandleConsumer)))); expect(stream.subscriptionCount).toBe(2); await act(async()=>remounted.unmount()); expect(stream.subscriptionCount).toBe(1);
    immutable(); expect(stream.subscriptionCount).toBe(0); expect(stream.topicCount).toBe(0);
  } finally { stream.dispose(); element.remove(); }
});

test('real shared consumer mutation, status errors and logout cannot cross ownership', async () => {
  const client = new SdkClient({ ...config.transport, session: { mode: 'bearer', token: () => config.accessToken } }, browserFetch);
  const stream = new CanonicalDataStream(client, { applicationOrigin: config.applicationOrigin, snapshotPath: '/v2/snapshots' }, config.authenticated, config.authorizationScope);
  const controller = new AbortController();
  const identities = new Set<string>(); let disruptive = 0, errors = 0;
  const disrupt = stream.subscribe({ topics: config.tradeTopics, signal: controller.signal,
    installSnapshot: snapshot => { for (const topic of snapshot.topics) topic.sequence = '0'; },
    onStatus: () => { throw new Error('Status observer interrupted'); },
    onError: () => { errors++; },
    onEvent: event => { disruptive++; event.eventId = 'consumer-owned-change'; throw new Error('Observer interrupted'); },
  });
  const healthy = stream.subscribe({ topics: config.tradeTopics, installSnapshot: () => {}, onEvent: event => identities.add(event.eventId) });
  try {
    const deadline = Date.now() + 30000;
    while (stream.status.state !== 'live') { if (Date.now() > deadline) throw new Error('Actual shared stream not live'); await pause(20); }
    const expected = (await produce('emit100Fills')).eventIds;
    expect(new Set(expected).size).toBe(100);
    const end = Date.now() + 30000;
    while (expected.some(identity => !identities.has(identity))) { if (Date.now() > end) throw new Error('Consumer corrupted immutable fill'); await pause(20); }
    expect(disruptive).toBe(100); expect(errors).toBeGreaterThanOrEqual(100); expect(identities.has('consumer-owned-change')).toBe(false);
    expect(stream.subscriptionCount).toBe(2); controller.abort(); expect(stream.subscriptionCount).toBe(1);
    stream.logout(); expect(stream.subscriptionCount).toBe(0); expect(stream.topicCount).toBe(0);
    disrupt(); healthy(); expect(stream.subscriptionCount).toBe(0);
    expect(stream.status.authenticationRequired).toBe(true);
    expect(JSON.stringify(stream.status).includes(config.accessToken)).toBe(false);
    stream.dispose(); expect(() => stream.subscribe({ topics: config.tradeTopics, installSnapshot: () => {}, onEvent: () => {} })).toThrow('STREAM_DISPOSED');
  } finally { controller.abort(); disrupt(); healthy(); stream.dispose(); }
});

test('logout from a loading observer cancels before browser network activity', () => {
  let requests = 0;
  const observedFetch: typeof fetch = (input, init) => { requests++; return browserFetch(input, init); };
  const stream = new CanonicalDataStream(new SdkClient(config.transport, observedFetch), { applicationOrigin: config.applicationOrigin, snapshotPath: '/v2/snapshots' }, false, '');
  const owner = new AbortController();
  let installed = 0;
  try {
    const release = stream.subscribe({ topics: config.publicTopics, signal: owner.signal, installSnapshot: () => { installed++; }, onEvent: () => { throw new Error('Disposed observer received an event'); }, onStatus: status => { if (status.state === 'loading') stream.logout(); } });
    expect(stream.subscriptionCount).toBe(0); expect(stream.topicCount).toBe(0);
    expect(stream.status.state).toBe('closed'); expect(stream.status.authenticationRequired).toBe(true);
    expect(requests).toBe(0); expect(installed).toBe(0);
    owner.abort(); release(); expect(stream.subscriptionCount).toBe(0);
  } finally { owner.abort(); stream.dispose(); }
});
