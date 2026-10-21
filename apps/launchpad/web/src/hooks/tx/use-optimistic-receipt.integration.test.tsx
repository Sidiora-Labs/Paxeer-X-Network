import { appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { createRequire } from 'node:module';
import { createServer, type IncomingMessage } from 'node:http';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import type { Duplex } from 'node:stream';
import { afterAll, expect, test } from 'vitest';
import React, { act } from 'react';
import { createPublicClient, http as viemHttp, type Hex } from 'viem';
import { paxeerWalletChain } from '@/core/wallet-sdk/client';

// Actual React + wagmi receipt query against an owned local EVM. No receipt,
// wagmi hook, chain response or completion status is mocked.
const { JSDOM } = createRequire(import.meta.url)('jsdom') as {
  JSDOM: new (html: string, options: { url: string }) => { window: Window & typeof globalThis & { close(): void } };
};
const url = process.env.WALLET_TEST_RPC_URL ?? 'http://127.0.0.1:18525';
if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(url).hostname)) throw new Error('Owned loopback fixture required');
if (process.env.WALLET_TEST_ALLOW_CHAIN_MUTATION !== '1') throw new Error('Dedicated fixture mutation authorization required');
const rpc = createPublicClient({ chain: paxeerWalletChain, transport: viemHttp(url, { retryCount: 0 }) });
async function nodeRpc<T>(method: string, params: unknown[] = []): Promise<T> {
  const response = await fetch(url, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }) });
  const body = await response.json() as { result: T; error?: { message: string } };
  if (body.error) throw new Error(body.error.message);
  return body.result;
}
const dom = new JSDOM('<!doctype html><html><body><main id="root"></main></body></html>', { url: 'http://localhost:3050' });
const originalWindow = globalThis.window, originalDocument = globalThis.document;
Object.defineProperty(globalThis, 'window', { value: dom.window, configurable: true });
Object.defineProperty(globalThis, 'document', { value: dom.window.document, configurable: true });
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { createConfig, http, WagmiProvider } = await import('wagmi');
const { createRoot } = await import('react-dom/client');
// A real loopback core/api v2 market stream sends trade notifications; these are
// read hints, never fabricated chain receipts. It replays the recorded production
// wire (src/core/test-fixtures/market-production/core-api-wire.json): the
// GET /v2/snapshots body narrowed to the pool's market.trades topic, the
// subscribed/checkpoint frames, then the recorded live Trade frame re-addressed
// to the transaction under test with the next sequence. The production stream
// parser/hook is active.
interface ServerSocket { send(data: string): void; on(event: 'message', listener: (data: unknown) => void): void; on(event: 'close', listener: () => void): void; terminate(): void }
interface WebSocketServerInstance { handleUpgrade(request: IncomingMessage, socket: Duplex, head: Buffer, done: (socket: ServerSocket) => void): void; close(): void }
const { WebSocketServer } = createRequire(import.meta.url)('ws') as { WebSocketServer: new (options: { noServer: true; handleProtocols: (protocols: Set<string>) => string | false }) => WebSocketServerInstance };
type WireFrame = Record<string, unknown> & { payload: Record<string, unknown> & { position: Record<string, string> } };
const wire = JSON.parse(readFileSync(new URL('../../core/test-fixtures/market-production/core-api-wire.json', import.meta.url), 'utf8')) as { snapshot: { status: number; headers: Record<string, string>; body: { data: { snapshot: { topics: { topic: string; sequence: string }[] } } } }; replayFrames: unknown[]; liveFrames: WireFrame[] };
const pool = '0xce20da50f92d21d8bee497910c782e9aa25b6263';
const tradesTopic = `market.trades.${pool}`;
const tradeSnapshot = { ...wire.snapshot.body, data: { snapshot: { ...wire.snapshot.body.data.snapshot, topics: wire.snapshot.body.data.snapshot.topics.filter(entry => entry.topic === tradesTopic) } } };
const recordedTrade = wire.liveFrames.find(frame => frame.topic === tradesTopic) as WireFrame;
const sockets = new Map<ServerSocket, { next: bigint }>();
const wss = new WebSocketServer({ noServer: true, handleProtocols: protocols => protocols.has('sidiora.v2') ? 'sidiora.v2' : false });
const stream = createServer((request, response) => {
  const url = new URL(request.url ?? '/', 'http://fixture.invalid');
  if (url.pathname === '/v2/snapshots' && url.searchParams.get('topics') === tradesTopic) { response.writeHead(wire.snapshot.status, wire.snapshot.headers); response.end(JSON.stringify(tradeSnapshot)); return; }
  response.writeHead(404, { 'Content-Type': 'application/json' }); response.end('{}');
});
stream.on('upgrade', (request, socket, head) => {
  if (request.url !== '/v2/stream') { socket.destroy(); return; }
  wss.handleUpgrade(request, socket, head, client => {
    let subscribed = false;
    client.on('close', () => sockets.delete(client));
    client.on('message', () => {
      if (subscribed) return;
      subscribed = true;
      for (const frame of wire.replayFrames) client.send(JSON.stringify(frame));
      sockets.set(client, { next: BigInt(tradeSnapshot.data.snapshot.topics[0].sequence) + 1n });
    });
  });
});
await new Promise<void>((resolve) => stream.listen(0, '127.0.0.1', resolve));
const streamAddress = stream.address();
if (!streamAddress || typeof streamAddress === 'string') throw new Error('Loopback notification listener unavailable');
const originalDataUrl = process.env.NEXT_PUBLIC_DATA_API_URL;
process.env.NEXT_PUBLIC_DATA_API_URL = `http://127.0.0.1:${streamAddress.port}`;
const { useOptimisticReceipt } = await import('./use-optimistic-receipt');
function notify(hash: Hex) {
  for (const [socket, state] of sockets) {
    const sequence = state.next++;
    const eventId = createHash('sha256').update(`${hash}:${sequence}`).digest('hex');
    const frame = { ...recordedTrade, eventId, sequence: String(sequence), revision: String(sequence), payload: { ...recordedTrade.payload, eventId, position: { ...recordedTrade.payload.position, transactionHash: hash } } };
    const payload = JSON.stringify(frame);
    socket.send(payload);
    if(process.env.L26_PHASE4==='1'){if(!process.env.L26_MEASUREMENT_OUTPUT)throw new Error('Raw notification output authority absent');appendFileSync(resolve(process.env.L26_MEASUREMENT_OUTPUT,'notification-frames.ndjson'),JSON.stringify({sentAt:Date.now(),socketCount:sockets.size,wirePayload:payload})+'\n');}
  }
}
const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
const config = createConfig({ chains: [paxeerWalletChain], connectors: [], multiInjectedProviderDiscovery: false, pollingInterval: 100, transports: { 125: http(url, { retryCount: 0 }) } });
const root = createRoot(document.getElementById('root')!);
let current: ReturnType<typeof useOptimisticReceipt> | undefined;
function Probe({ hash }: { hash?: Hex }) { current = useOptimisticReceipt(hash, { poolAddress: pool }); return null; }
async function render(hash?: Hex) {
  await act(async () => { root.render(<WagmiProvider config={config} reconnectOnMount={false}><QueryClientProvider client={query}><Probe hash={hash} /></QueryClientProvider></WagmiProvider>); });
}
async function waitUntil(predicate: () => boolean) {
  for (let i = 0; i < 100; i++) { if (predicate()) return; await act(async () => { await new Promise((r) => setTimeout(r, 100)); }); }
  throw new Error('Actual receipt query did not settle within 10 seconds');
}
afterAll(async () => {
  await nodeRpc('evm_setAutomine', [true]);
  await act(async () => { root.unmount(); }); query.clear(); dom.window.close();
  for (const socket of sockets.keys()) socket.terminate();
  wss.close();
  await new Promise<void>((resolve) => stream.close(() => resolve()));
  if (originalDataUrl === undefined) delete process.env.NEXT_PUBLIC_DATA_API_URL; else process.env.NEXT_PUBLIC_DATA_API_URL = originalDataUrl;
  Object.defineProperty(globalThis, 'window', { value: originalWindow, configurable: true });
  Object.defineProperty(globalThis, 'document', { value: originalDocument, configurable: true });
});

test('pending beyond two seconds stays unresolved; only actual success/revert receipts settle the hook', async () => {
  expect(await rpc.getChainId()).toBe(125);
  const [from, to] = await nodeRpc<Hex[]>('eth_accounts');
  await nodeRpc('evm_setAutomine', [false]);
  const hash = await nodeRpc<Hex>('eth_sendTransaction', [{ from, to, value: '0x7b', gas: '0x5208' }]);
  await render(hash);
  await act(async () => { await new Promise((r) => setTimeout(r, 2300)); });
  expect(current?.receipt).toBeUndefined(); expect(current?.isPending).toBe(true); expect(current?.isOptimistic).toBe(false); expect(current?.state).toBe('checking');
  await waitUntil(() => sockets.size > 0); notify(hash);
  await act(async () => { await new Promise((r) => setTimeout(r, 150)); });
  expect(current?.receipt).toBeUndefined(); expect(current?.isPending).toBe(true);
  await nodeRpc('evm_mine'); await waitUntil(() => current?.receipt?.transactionHash === hash);
  expect(current?.receipt?.status).toBe('success'); expect(current?.isPending).toBe(false);
  expect(current?.receipt).toMatchObject(await rpc.getTransactionReceipt({ hash }));

  const deployHash=await nodeRpc<Hex>('eth_sendTransaction',[{from,data:'0x6005600c60003960056000f360006000fd',gas:'0x186a0'}]);
  await nodeRpc('evm_mine');
  const deployed=await rpc.getTransactionReceipt({hash:deployHash});
  expect(deployed.status).toBe('success');
  const reverter=deployed.contractAddress;
  expect(reverter).toBeTruthy();
  const revertedHash = await nodeRpc<Hex>('eth_sendTransaction', [{ from, to: reverter, value: '0x0', gas: '0x186a0' }]);
  await render(revertedHash);
  expect(current?.state).toBe('submitted');
  expect(current?.receipt).toBeUndefined(); expect(current?.isPending).toBe(true);
  notify(deployHash);notify(revertedHash);
  await act(async()=>{await new Promise(done=>setTimeout(done,150));});
  expect(current?.receipt).toBeUndefined();expect(current?.isPending).toBe(true);
  await nodeRpc('evm_mine'); await waitUntil(() => current?.receipt?.transactionHash === revertedHash);
  expect(current?.receipt?.status).toBe('reverted'); expect(current?.isPending).toBe(false);
  expect(current?.receipt).toMatchObject(await rpc.getTransactionReceipt({ hash: revertedHash }));
  await render(undefined); expect(current?.state).toBe('idle'); expect(current?.receipt).toBeUndefined(); expect(current?.isPending).toBe(false);
}, 20000);
