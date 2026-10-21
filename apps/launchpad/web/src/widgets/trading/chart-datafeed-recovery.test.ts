import { describe, expect, it } from 'vitest';
import { chartReplayListenerCount, createReplayUdfDatafeed, resolutionToSeconds, resolutionToWsTimeframe } from './udf-datafeed';

const pool = `0x${'a'.repeat(40)}`;
describe('owned replay chart resource lifecycle', () => {
  it('releases all owned resources across one hundred mount/unmount generations', () => {
    const baseline = chartReplayListenerCount();
    for (let cycle = 0; cycle < 100; cycle++) {
      const feed = createReplayUdfDatafeed(pool);
      feed.subscribeBars({}, '1D', () => {}, 'chart');
      expect(feed.resourceCounts().subscriptions).toBe(1);
      expect(feed.resourceCounts().timers).toBe(1);
      feed.subscribeBars({}, '1W', () => {}, 'chart');
      expect(feed.resourceCounts().subscriptions).toBe(1);
      expect(feed.resourceCounts().timers).toBe(1);
      feed.unsubscribeBars('chart');
      expect(feed.resourceCounts()).toEqual({ subscriptions: 0, requests: 0, sockets: 0, timers: 0 });
      feed.dispose(); feed.dispose();
      expect(chartReplayListenerCount()).toBe(baseline);
    }
  });
  it('starts degraded without authenticated replay authority and never implies live', () => {
    const states: string[] = [];
    const feed = createReplayUdfDatafeed(pool, 'price', freshness => states.push(freshness.connectionStatus));
    try {
      feed.subscribeBars({}, '1D', () => {}, 'daily');
      expect(states).toEqual(['degraded']);
      expect(states).not.toContain('live');
    } finally { feed.dispose(); }
  });
  it('daily and weekly recovery windows retain the exact declared interval', () => {
    for (const [resolution, interval, seconds] of [['1D', '1d', 86400], ['1W', '1w', 604800]] as const) {
      expect(resolutionToWsTimeframe(resolution)).toBe(interval);
      expect(resolutionToSeconds(resolution) * 3 + 60).toBe(seconds * 3 + 60);
    }
  });
});

import { readFileSync } from 'node:fs';
import { createHash, createHmac } from 'node:crypto';
import { createServer, request as httpRequest, type IncomingMessage } from 'node:http';
import type { Duplex } from 'node:stream';
import { once } from 'node:events';
import { encodeFunctionData, getAddress, parseAbi } from 'viem';
import { browserStreamTransportV2 } from '@kindle/sdk';
import { decodeV2Contract, SdkClient } from '@kindle/sdk';
import { canonicalCandleToDisplayBar, canonicalCandleTableRow, confirmedChartMarkers, CanonicalCandleRevisionMerge, fetchCanonicalCandlePage, registerChartReplayTransport } from './udf-datafeed';

function object(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('Expected actual joined configuration object');
  return Object.fromEntries(Object.entries(value));
}
function string(value: unknown): string { if (typeof value !== 'string' || !value) throw new Error('Missing actual joined configuration field'); return value; }
function loopback(value: string): string {
  const url = new URL(value);
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) || url.username || url.password) throw new Error('Owned loopback authority required');
  return url.origin;
}
async function eventually(predicate: () => boolean | Promise<boolean>, timeout = 45000) {
  const end = Date.now() + timeout;
  while (!(await predicate())) { if (Date.now() >= end) throw new Error('Real joined chart scenario timed out'); await new Promise(resolve => setTimeout(resolve, 50)); }
}
function actualJoined() {
  const path = string(process.env.JOINED_CONFIG_FILE);
  const bytes = readFileSync(path);
  if (createHash('sha256').update(bytes).digest('hex') !== process.env.JOINED_CONFIG_SHA256) throw new Error('Actual joined runtime configuration digest mismatch');
  if (process.env.JOINED_ALLOW_CHAIN_MUTATION !== '1') throw new Error('Explicit private joined chain mutation authority required');
  const config = object(JSON.parse(bytes.toString()));
  const chain = object(JSON.parse(readFileSync(string(process.env.JOINED_CHAIN_FILE), 'utf8')));
  if (config.schema_version !== 1 || chain.realm !== 'private-anvil125' || chain.productionActivation !== false) throw new Error('Fresh private chain authority required');
  if (!Array.isArray(config.processes)) throw new Error('Missing real joined process inventory');
  const processes = config.processes.map(object);
  const processByRole = (role: string) => { const item = processes.find(value => value.role === role); if (!item) throw new Error('Missing actual joined role ' + role); return item; };
  const roleOrigin = (role: string) => loopback(string(processByRole(role).ready_url));
  const roles = object(object(chain.manifest).roles);
  return { config, chain, applicationOrigin: loopback(string(config.browser_origin)), api: loopback(string(config.api_origin)), sockets: roleOrigin('api-sockets'), identity: roleOrigin('gateway'), charts: roleOrigin('trading-charts'), rpc: loopback(string(config.rpc_url)), pool: getAddress(string(chain.pool)), account: getAddress(string(chain.account)), router: getAddress(string(object(roles.Router).address)), signingKey: string(object(processByRole('api-rest').env).STREAM_SIGNING_KEY) };
}
class FrameReader {
  private pending = Buffer.alloc(0);
  constructor(private readonly receive: (frame: Record<string, unknown>) => void) {}
  write(chunk: Buffer) {
    this.pending = Buffer.concat([this.pending, chunk]);
    if (this.pending.length > 512 * 1024) throw new Error('Observed real WebSocket frame exceeded bound');
    while (this.pending.length >= 2) {
      const opcode = this.pending[0] & 15;
      const masked = (this.pending[1] & 128) !== 0;
      let length = this.pending[1] & 127, offset = 2;
      if (length === 126) { if (this.pending.length < 4) return; length = this.pending.readUInt16BE(2); offset = 4; }
      if (length === 127) { if (this.pending.length < 10) return; const exact = this.pending.readBigUInt64BE(2); if (exact > 256n * 1024n) throw new Error('Unbounded real frame'); length = Number(exact); offset = 10; }
      const maskBytes = masked ? 4 : 0;
      if (this.pending.length < offset + maskBytes + length) return;
      const mask = this.pending.subarray(offset, offset + maskBytes); offset += maskBytes;
      const body = Buffer.from(this.pending.subarray(offset, offset + length));
      this.pending = this.pending.subarray(offset + length);
      if (masked) for (let index = 0; index < body.length; index++) body[index] ^= mask[index % 4];
      if (opcode === 1) this.receive(object(JSON.parse(body.toString())));
    }
  }
}
class JoinedNetworkFaults {
  readonly server = createServer();
  readonly sockets = new Set<Duplex>();
  readonly streamSockets = new Set<Duplex>();
  readonly requests: URL[] = [];
  readonly subscriptions: Record<string, unknown>[] = [];
  readonly checkpoints: Record<string, unknown>[] = [];
  realtimeUnavailable = false;
  httpUnavailable = false;
  delayHistory = false;
  delaySnapshot = false;
  delayedSnapshots = 0;
  delayedHistory = 0;
  cancelledHistory = 0;
  private releases: Array<() => void> = [];
  origin = '';
  constructor(private readonly actual: ReturnType<typeof actualJoined>) {
    this.server.on('connection', socket => { this.sockets.add(socket); socket.on('close', () => this.sockets.delete(socket)); });
    this.server.on('request', (request, response) => {
      const url = new URL(request.url ?? '/', 'http://127.0.0.1'); this.requests.push(url);
      if (this.httpUnavailable) { request.socket.destroy(); return; }
      const forward = () => {
        if (response.destroyed) return;
        const target = new URL(url.pathname + url.search, this.target(url.pathname));
        const upstream = httpRequest(target, { method: request.method, headers: this.headers(request, target) }, reply => {
          if (this.delaySnapshot && url.pathname === '/v2/snapshots') {
            const chunks: Buffer[] = []; reply.on('data', chunk => chunks.push(Buffer.from(chunk))); reply.on('end', () => { this.delayedSnapshots++; this.releases.push(() => { if (!response.destroyed) { response.writeHead(reply.statusCode ?? 502, reply.headers); response.end(Buffer.concat(chunks)); } }); });
          } else { response.writeHead(reply.statusCode ?? 502, reply.headers); reply.pipe(response); }
        });
        upstream.on('error', () => response.destroy());
        response.on('close', () => upstream.destroy()); request.pipe(upstream);
      };
      if (this.delayHistory && /\/candles$/.test(url.pathname)) {
        this.delayedHistory++; response.on('close', () => { if (!response.writableEnded) this.cancelledHistory++; }); this.releases.push(forward);
      } else forward();
    });
    this.server.on('upgrade', (request, socket, head) => {
      if (this.realtimeUnavailable) { socket.destroy(); return; }
      const target = new URL(request.url ?? '/v2/stream', this.actual.sockets);
      const upstream = httpRequest(target, { headers: this.headers(request, target) });
      upstream.on('upgrade', (response, peer, upstreamHead) => {
        this.streamSockets.add(socket); this.streamSockets.add(peer);
        socket.on('close', () => { this.streamSockets.delete(socket); peer.destroy(); });
        peer.on('close', () => { this.streamSockets.delete(peer); socket.destroy(); });
        socket.on('error', () => peer.destroy()); peer.on('error', () => socket.destroy());
        const clientFrames = new FrameReader(frame => { if (frame.op === 'subscribe') this.subscriptions.push(frame); });
        const serverFrames = new FrameReader(frame => { if (frame.op === 'checkpoint') this.checkpoints.push(frame); });
        socket.on('data', chunk => clientFrames.write(chunk)); peer.on('data', chunk => serverFrames.write(chunk));
        socket.write(`HTTP/1.1 ${response.statusCode} ${response.statusMessage}\r\n${Object.entries(response.headers).map(([key, value]) => `${key}: ${Array.isArray(value) ? value.join(', ') : value}`).join('\r\n')}\r\n\r\n`);
        if (upstreamHead.length) { serverFrames.write(upstreamHead); socket.write(upstreamHead); }
        if (head.length) { clientFrames.write(head); peer.write(head); }
        socket.pipe(peer); peer.pipe(socket);
      });
      upstream.on('response', response => { response.resume(); socket.destroy(); });
      upstream.on('error', () => socket.destroy()); upstream.end();
    });
  }
  private target(path: string) { return path.startsWith('/v2/auth/') || path === '/v2/stream/tickets' ? this.actual.identity : path.startsWith('/v2/udf/') ? this.actual.charts : this.actual.api; }
  private headers(request: IncomingMessage, target: URL) { return { ...request.headers, host: target.host, origin: this.actual.applicationOrigin }; }
  async start() { this.server.listen(0, '127.0.0.1'); await once(this.server, 'listening'); const address = this.server.address(); if (!address || typeof address === 'string') throw new Error('Actual fault listener missing'); this.origin = `http://127.0.0.1:${address.port}`; }
  disconnect() { for (const socket of this.streamSockets) socket.destroy(); }
  releaseHistory() { this.delayHistory = false; this.delaySnapshot = false; const releases = this.releases; this.releases = []; releases.forEach(release => release()); }
  async close() { this.releaseHistory(); for (const socket of this.sockets) socket.destroy(); this.disconnect(); await new Promise<void>((resolve, reject) => this.server.close(error => error ? reject(error) : resolve())); }
}
async function rpc(actual: ReturnType<typeof actualJoined>, method: string, params: unknown[]): Promise<unknown> {
  const response = await fetch(actual.rpc, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }) });
  if (!response.ok) throw new Error('Actual chain RPC failed'); const result = object(await response.json()); if (result.error) throw new Error('Actual chain call rejected'); return result.result;
}
async function actualBuy(actual: ReturnType<typeof actualJoined>) {
  expect(await rpc(actual, 'eth_chainId', [])).toBe('0x7d');
  const latest = object(await rpc(actual, 'eth_getBlockByNumber', ['latest', false]));
  const deadline = BigInt(string(latest.timestamp)) + 600n;
  const data = encodeFunctionData({ abi: parseAbi(['function buy(address pool,uint256 usdlAmountIn,uint256 minTokensOut,uint256 deadline) returns (uint256)']), functionName: 'buy', args: [actual.pool, 1000000n, 1n, deadline] });
  const transaction = string(await rpc(actual, 'eth_sendTransaction', [{ from: actual.account, to: actual.router, data }]));
  await eventually(async () => { const receipt = await rpc(actual, 'eth_getTransactionReceipt', [transaction]); if (receipt === null) return false; expect(object(receipt).status).toBe('0x1'); return true; });
  return transaction;
}
function expiredFixtureCursor(cursor: string, key: string): string {
  const [encoded, signature] = cursor.split('.'); if (!encoded || !signature) throw new Error('Actual signed cursor required');
  const value = object(JSON.parse(Buffer.from(encoded, 'base64url').toString()));
  const now = Math.floor(Date.now() / 1000); value.issuedAt = now - 73 * 3600; value.expiresAt = now - 3600;
  const body = Buffer.from(JSON.stringify(value)).toString('base64url');
  return `${body}.${createHmac('sha256', key).update('sidiora.v2.resume\0').update(body).digest('base64url')}`;
}

describe('real joined canonical chart recovery', () => {
  it('replays three missed actual commits, rejects a genuinely signed expired cursor and converges to authoritative history', async () => {
    const actual = actualJoined(); const faults = new JoinedNetworkFaults(actual); await faults.start();
    const authorization = new AbortController();
    const client = new SdkClient({ apiOrigin: faults.origin, identityOrigin: faults.origin, mediaOrigin: faults.origin, streamOrigin: faults.origin.replace(/^http/, 'ws'), approvedStreamOrigins: [faults.origin.replace(/^http/, 'ws')], production: false });
    const release = registerChartReplayTransport({ transport: browserStreamTransportV2(client, { applicationOrigin: actual.applicationOrigin, snapshotPath: '/v2/snapshots' }, false), signal: authorization.signal });
    const ticks: ReturnType<typeof canonicalCandleToDisplayBar>[] = []; const states: string[] = [];
    const feed = createReplayUdfDatafeed(actual.pool, 'price', value => states.push(value.connectionStatus), undefined, { apiOrigin: faults.origin, valuationOrigin: faults.origin });
    const authoritative = async () => { const now = Math.floor(Date.now() / 1000); return fetchCanonicalCandlePage(actual.pool, '1D', new Date((now - 3 * 86400) * 1000).toISOString(), new Date((now + 60) * 1000).toISOString(), 50, undefined, undefined, faults.origin); };
    try {
      feed.subscribeBars({}, '1D', bar => ticks.push(bar), 'joined');
      await eventually(() => feed.replayStatus()[0]?.replayComplete === true && faults.checkpoints.length > 0);
      const acceptedCursor = string(faults.checkpoints.at(-1)?.cursor); const before = await authoritative();
      let revision = BigInt(before.data.items.at(-1)?.revision ?? '0');
      faults.realtimeUnavailable = true; faults.disconnect();
      await eventually(() => states.includes('resync_required'));
      for (let index = 0; index < 3; index++) {
        await actualBuy(actual);
        await eventually(async () => { const page = await authoritative(); const next = BigInt(page.data.items.at(-1)?.revision ?? '0'); if (next <= revision) return false; revision = next; return true; });
      }
      faults.realtimeUnavailable = false;
      await eventually(() => feed.replayStatus()[0]?.replayComplete === true && faults.subscriptions.some(frame => frame.afterCursor === acceptedCursor));
      const final = await authoritative(); const candle = final.data.items.at(-1); expect(candle).toBeDefined(); if (!candle) throw new Error('Actual committed candle missing');
      await eventually(() => JSON.stringify(ticks.at(-1)) === JSON.stringify(canonicalCandleToDisplayBar(candle)));
      const expired = expiredFixtureCursor(string(faults.checkpoints.at(-1)?.cursor), actual.signingKey);
      const resyncs = states.filter(value => value === 'resync_required').length;
      feed.resumeFromCursor('joined', expired);
      await eventually(() => states.filter(value => value === 'resync_required').length > resyncs);
      await eventually(() => feed.replayStatus()[0]?.replayComplete === true);
      expect(ticks.at(-1)).toEqual(canonicalCandleToDisplayBar(candle));
      expect(feed.replayStatus()[0].pending).toBe(0);
    } finally { feed.dispose(); authorization.abort(); release(); await faults.close(); }
  }, 150000);

  it('cancels delayed history on pool replacement and preserves values through realtime then HTTP failure', async () => {
    const actual = actualJoined(); const faults = new JoinedNetworkFaults(actual); await faults.start();
    const authorization = new AbortController();
    const client = new SdkClient({ apiOrigin: faults.origin, identityOrigin: faults.origin, mediaOrigin: faults.origin, streamOrigin: faults.origin.replace(/^http/, 'ws'), approvedStreamOrigins: [faults.origin.replace(/^http/, 'ws')], production: false });
    const release = registerChartReplayTransport({ transport: browserStreamTransportV2(client, { applicationOrigin: actual.applicationOrigin, snapshotPath: '/v2/snapshots' }, false), signal: authorization.signal });
    const authority = { apiOrigin: faults.origin, valuationOrigin: faults.origin };
    const abandoned = createReplayUdfDatafeed(actual.pool, 'price', () => {}, undefined, authority);
    let lateBars = 0;
    let feed: ReturnType<typeof createReplayUdfDatafeed> | undefined;
    try {
      faults.delayHistory = true;
      const now = Math.floor(Date.now() / 1000);
      abandoned.getBars({}, '1D', { from: now - 3 * 86400, to: now + 60 }, () => lateBars++, () => lateBars++);
      await eventually(() => faults.delayedHistory > 0); abandoned.dispose(); await eventually(() => faults.cancelledHistory > 0); faults.releaseHistory();
      const ticks: ReturnType<typeof canonicalCandleToDisplayBar>[] = []; const states: string[] = [];
      feed = createReplayUdfDatafeed(actual.pool, 'price', value => states.push(value.connectionStatus), undefined, authority);
      feed.subscribeBars({}, '1D', bar => ticks.push(bar), 'joined');
      await eventually(() => feed?.replayStatus()[0]?.replayComplete === true && ticks.length > 0);
      const last = ticks.at(-1);
      faults.realtimeUnavailable = true; faults.disconnect(); faults.httpUnavailable = true;
      feed.retry(); await eventually(() => states.includes('offline'));
      expect(ticks.at(-1)).toEqual(last); expect(lateBars).toBe(0);
      faults.httpUnavailable = false; faults.realtimeUnavailable = false; feed.retry();
      await eventually(() => feed?.replayStatus()[0]?.replayComplete === true);
      for (const resolution of ['1D', '1W']) {
        faults.realtimeUnavailable = true; faults.disconnect();
        feed.subscribeBars({}, resolution, bar => ticks.push(bar), 'joined');
        await eventually(() => faults.requests.some(url => url.pathname.endsWith('/candles') && url.searchParams.get('interval') === resolutionToWsTimeframe(resolution)));
        const request = faults.requests.filter(url => url.pathname.endsWith('/candles') && url.searchParams.get('interval') === resolutionToWsTimeframe(resolution)).at(-1); if (!request) throw new Error('Actual history request missing');
        expect(Date.parse(string(request.searchParams.get('to'))) - Date.parse(string(request.searchParams.get('from')))).toBe((resolutionToSeconds(resolution) * 3 + 60) * 1000);
        expect(feed.resourceCounts().subscriptions).toBe(1);
      }
    } finally { abandoned.dispose(); feed?.dispose(); authorization.abort(); release(); await faults.close(); }
  }, 150000);
});

describe('real joined chart adversarial revisions and confirmed annotations', () => {
  it('retains revision five when an older genuine snapshot arrives after newer history and reconciles markers', async () => {
    const actual = actualJoined(); const faults = new JoinedNetworkFaults(actual); await faults.start();
    const authorization = new AbortController();
    const client = new SdkClient({ apiOrigin: faults.origin, identityOrigin: faults.origin, mediaOrigin: faults.origin, streamOrigin: faults.origin.replace(/^http/, 'ws'), approvedStreamOrigins: [faults.origin.replace(/^http/, 'ws')], production: false });
    const release = registerChartReplayTransport({ transport: browserStreamTransportV2(client, { applicationOrigin: actual.applicationOrigin, snapshotPath: '/v2/snapshots' }, false), signal: authorization.signal });
    const ticks: ReturnType<typeof canonicalCandleToDisplayBar>[] = [];
    const views: import('./udf-datafeed').ChartAccessibleView[] = [];
    const feed = createReplayUdfDatafeed(actual.pool, 'price', () => {}, undefined, { apiOrigin: faults.origin, valuationOrigin: faults.origin }, view => views.push(view));
    const now = Math.floor(Date.now() / 1000);
    const history = () => fetchCanonicalCandlePage(actual.pool, '1D', new Date((now - 3 * 86400) * 1000).toISOString(), new Date((now + 60) * 1000).toISOString(), 50, undefined, undefined, faults.origin);
    try {
      const before = await history(); const first = before.data.items.at(-1); if (!first) throw new Error('Actual canonical candle required');
      faults.delaySnapshot = true; feed.subscribeBars({}, '1D', bar => ticks.push(bar), 'racing');
      await eventually(() => faults.delayedSnapshots === 1);
      let revision = BigInt(first.revision);
      for (let index = 0; index < 5; index++) { await actualBuy(actual); await eventually(async () => { const next = BigInt((await history()).data.items.at(-1)?.revision ?? '0'); if (next <= revision) return false; revision = next; return true; }); }
      const latest = (await history()).data.items.at(-1); if (!latest) throw new Error('Actual newest candle missing');
      const merge = new CanonicalCandleRevisionMerge();
      expect(merge.accept(latest, actual.pool, '1d')).toBe('apply');
      expect(merge.accept(first, actual.pool, '1d')).toBe('ignore');
      expect(merge.accept({ ...latest, snapshotId: first.snapshotId }, actual.pool, '1d')).toBe('ignore');
      expect(merge.accept({ ...latest, volumeUsdlRaw: (BigInt(latest.volumeUsdlRaw) + 1n).toString() }, actual.pool, '1d')).toBe('resync');
      faults.releaseHistory(); await eventually(() => feed.replayStatus()[0]?.replayComplete === true);
      expect(ticks.at(-1)).toEqual(canonicalCandleToDisplayBar(latest));
      await eventually(() => views.at(-1)?.markers.some(marker => marker.transactionHash === latest.close.source.position?.transactionHash) === true);
      const url = new URL(`/v2/pools/${actual.pool}/trades`, actual.api); url.searchParams.set('limit', '50');
      const response = await fetch(url, { headers: { 'X-Schema-Version': '2' } }); expect(response.ok).toBe(true);
      const page = decodeV2Contract('TradesResponse', await response.json());
      const joined = confirmedChartMarkers([latest], page.data.items);
      expect(joined.markers.length).toBeGreaterThan(0);
      for (const marker of joined.markers) { const trade = page.data.items.find(trade => `${latest.interval}:${trade.eventId}` === marker.id); expect(trade).toBeDefined(); expect(marker.transactionHash).toBe(trade?.position.transactionHash); const receipt = object(await rpc(actual, 'eth_getTransactionReceipt', [marker.transactionHash])); expect(receipt.status).toBe('0x1'); expect(marker.actorBasis).toBe(trade?.recipientAttribution === 'unavailable' ? 'event_sender' : trade?.recipientAttribution); }
      const row = canonicalCandleTableRow(latest);
      expect(BigInt(row.volumeUsdl.split('.')[0]) * 1000000n + BigInt((row.volumeUsdl.split('.')[1] ?? '').padEnd(6, '0'))).toBe(BigInt(latest.volumeUsdlRaw));
      expect(row.revision).toBe(latest.revision);
      expect(confirmedChartMarkers([latest], []).markers).toHaveLength(0);
      expect(confirmedChartMarkers([latest], []).partial).toBe(true);
      const trade = page.data.items[0]; if (!trade) throw new Error('Actual canonical fill required');
      for (const status of ['pending', 'failed']) expect(() => confirmedChartMarkers([latest], [{ ...trade, status }])).toThrow();
      const block = object(await rpc(actual, 'eth_getBlockByNumber', ['latest', false]));
      const failedCall = encodeFunctionData({ abi: parseAbi(['function buy(address pool,uint256 usdlAmountIn,uint256 minTokensOut,uint256 deadline) returns (uint256)']), functionName: 'buy', args: [actual.pool, 1000000n, (1n << 256n) - 1n, BigInt(string(block.timestamp)) + 600n] });
      const failedHash = string(await rpc(actual, 'eth_sendTransaction', [{ from: actual.account, to: actual.router, data: failedCall, gas: '0x989680' }]));
      await eventually(async () => { const receipt = await rpc(actual, 'eth_getTransactionReceipt', [failedHash]); if (!receipt) return false; expect(object(receipt).status).toBe('0x0'); return true; });
      const nonce = BigInt(string(await rpc(actual, 'eth_getTransactionCount', [actual.account, 'pending']))) + 5n;
      const pendingHash = string(await rpc(actual, 'eth_sendTransaction', [{ from: actual.account, to: actual.router, data: failedCall, gas: '0x989680', nonce: '0x' + nonce.toString(16) }]));
      try {
        expect(await rpc(actual, 'eth_getTransactionReceipt', [pendingHash])).toBeNull();
        const confirmedResponse = await fetch(url, { headers: { 'X-Schema-Version': '2' } }); expect(confirmedResponse.ok).toBe(true);
        const confirmed = decodeV2Contract('TradesResponse', await confirmedResponse.json());
        const annotations = confirmedChartMarkers([latest], confirmed.data.items).markers;
        expect(annotations.some(marker => marker.transactionHash.toLowerCase() === pendingHash.toLowerCase())).toBe(false);
        expect(annotations.some(marker => marker.transactionHash.toLowerCase() === failedHash.toLowerCase())).toBe(false);
        expect(confirmed.data.items.some(trade => [pendingHash.toLowerCase(), failedHash.toLowerCase()].includes(trade.position.transactionHash.toLowerCase()))).toBe(false);
      } finally { await rpc(actual, 'anvil_dropTransaction', [pendingHash]); }
      const markerBefore = views.at(-1)?.markers;
      faults.httpUnavailable = true; feed.retry(); await eventually(() => feed.replayStatus()[0]?.freshness.connectionStatus === 'offline');
      expect(views.at(-1)?.markers).toEqual(markerBefore);
    } finally { feed.dispose(); authorization.abort(); release(); await faults.close(); }
  }, 180000);
});

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
const browserAdversarialJourney = String.raw`
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const configuration = JSON.parse(fs.readFileSync(process.env.PHASE2_CONFIG_FILE, 'utf8'));
assert.equal(configuration.realm, 'private-anvil125');
const joined = JSON.parse(fs.readFileSync(process.env.JOINED_CONFIG_FILE, 'utf8'));
assert.equal(configuration.browser_origin, joined.browser_origin);
assert.equal(configuration.rpc_url, joined.rpc_url);
const origin = new URL(configuration.browser_origin);
assert.ok(['127.0.0.1', 'localhost', '[::1]'].includes(origin.hostname));
const { chromium } = require(configuration.playwright_module);
const output = process.env.CHART_BROWSER_OUTPUT_DIR;
assert.ok(output, 'Owned chart browser output directory required');
fs.mkdirSync(output, { recursive: true });
const chain = JSON.parse(fs.readFileSync(process.env.JOINED_CHAIN_FILE, 'utf8'));
assert.equal(chain.productionActivation, false);
const pool = chain.pool;
const browser = await chromium.launch({ headless: true });
try {
 for (const touch of [false, true]) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: touch ? { width: 390, height: 844 } : { width: 1440, height: 1000 }, hasTouch: touch, isMobile: touch });
  await context.tracing.start({ screenshots: true, snapshots: true });
  const page = await context.newPage();
  await page.route('**/charting_library/charting_library.standalone.js', route => route.abort());
  await page.goto(new URL('/token/' + pool, origin).href, { waitUntil: 'domcontentloaded' });
  await page.getByRole('alert').filter({ hasText: "Chart can't load right now" }).waitFor();
  const inspect = page.getByRole('button', { name: 'Show price list' });
  if (touch) await inspect.tap(); else { await inspect.focus(); await page.keyboard.press('Enter'); assert.equal(await inspect.getAttribute('aria-expanded'), 'true'); }
  const daily = page.getByRole('button', { name: 'Chart interval 1D', exact: true });
  if (touch) await daily.tap(); else { await daily.focus(); await page.keyboard.press('Enter'); }
  assert.equal(await daily.getAttribute('aria-pressed'), 'true');
  const region = page.getByRole('region', { name: 'Prices and volume', exact: true });
  await region.waitFor(); await region.focus();
  assert.equal(await region.evaluate(element => element === document.activeElement), true);
  const now = Math.floor(Date.now() / 1000);
  const request = new URL('/v2/pools/' + pool + '/candles', origin);
  request.searchParams.set('interval', '1d'); request.searchParams.set('from', new Date(Math.max(0, now - 86400 * 50) * 1000).toISOString()); request.searchParams.set('to', new Date((now + 60) * 1000).toISOString()); request.searchParams.set('limit', '50');
  const response = await page.request.get(request.href, { headers: { 'X-Schema-Version': '2' } });
  assert.equal(response.ok(), true); assert.equal(response.headers()['x-schema-version'], '2');
  const envelope = await response.json(); assert.ok(envelope.data.items.length > 0);
  const exact = (n, d) => { const a = BigInt(n), b = BigInt(d); let remainder = a % b, digits = ''; for (let i = 0; i < 160 && remainder !== 0n; i++) { remainder *= 10n; digits += String(remainder / b); remainder %= b; } return remainder !== 0n ? n + '/' + d : String(a / b) + (digits ? '.' + digits : ''); };
  for (const candle of envelope.data.items) {
   const row = region.getByRole('row').filter({ has: page.locator('time[datetime="' + candle.start + '"]') });
   await row.waitFor();
   const cells = await row.getByRole('cell').allTextContents();
   assert.deepEqual(cells.map(value => value.trim()), [candle.open, candle.high, candle.low, candle.close].map(price => exact(price.numerator, price.denominator)).concat([exact(candle.volumeUsdlRaw, '1000000'), candle.volumeTokenRaw, candle.revision]));
  }
  const totalValue = page.getByRole('button', { name: 'Total supply value', exact: true });
  if (touch) await totalValue.tap(); else { await totalValue.focus(); await page.keyboard.press('Enter'); }
  assert.equal(await daily.getAttribute('aria-pressed'), 'true');
  const price = page.getByRole('button', { name: 'Price', exact: true });
  if (touch) await price.tap(); else { await price.focus(); await page.keyboard.press('Enter'); }
  assert.equal(await daily.getAttribute('aria-pressed'), 'true');
  await region.getByRole('row').filter({ has: page.locator('time') }).first().waitFor();
  await page.screenshot({ path: path.join(output, touch ? 'chart-touch-table.png' : 'chart-keyboard-table.png'), fullPage: true });
  await context.tracing.stop({ path: path.join(output, touch ? 'chart-touch-trace.zip' : 'chart-keyboard-trace.zip') });
  await context.close();
 }
 const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1440, height: 1000 } });
 await context.tracing.start({ screenshots: true, snapshots: true });
 const page = await context.newPage();
 await page.addInitScript(() => {
  let api;
  Object.defineProperty(window, 'TradingView', { configurable: true, get: () => api, set: value => {
   api = value;
   if (typeof value?.widget !== 'function') return;
   const constructor = value.widget;
   value.widget = new Proxy(constructor, { construct(target, argumentsList, newTarget) {
    const widget = Reflect.construct(target, argumentsList, newTarget);
    const observation = { widget, ready: false };
    window.__actualChartObservation = observation;
    widget.onChartReady(() => { observation.ready = true; });
    return widget;
   } });
  } });
 });
 await page.goto(new URL('/token/' + pool, origin).href, { waitUntil: 'domcontentloaded' });
 await page.waitForFunction(() => window.__actualChartObservation?.ready === true);
 const daily = page.getByRole('button', { name: 'Chart interval 1D', exact: true });
 await daily.click();
 await page.waitForFunction(() => window.__actualChartObservation?.ready === true);
 const chartRange = await page.evaluate(async () => {
  const chart = window.__actualChartObservation.widget.activeChart();
  const range = chart.getVisibleRange();
  await chart.setVisibleRange({ from: range.from + (range.to - range.from) / 4, to: range.to });
  return chart.getVisibleRange();
 });
 await page.getByRole('button', { name: 'Total supply value', exact: true }).click();
 await page.waitForFunction(() => window.__actualChartObservation?.ready === true);
 await page.waitForFunction(expected => {
  const range = window.__actualChartObservation.widget.activeChart().getVisibleRange();
  return Math.abs(range.from - expected.from) <= 1 && Math.abs(range.to - expected.to) <= 1;
 }, chartRange);
 assert.equal(await daily.getAttribute('aria-pressed'), 'true');
 await page.getByRole('button', { name: 'Price', exact: true }).click();
 await page.waitForFunction(() => window.__actualChartObservation?.ready === true);
 await page.waitForFunction(expected => {
  const range = window.__actualChartObservation.widget.activeChart().getVisibleRange();
  return Math.abs(range.from - expected.from) <= 1 && Math.abs(range.to - expected.to) <= 1;
 }, chartRange);
 assert.equal(await daily.getAttribute('aria-pressed'), 'true');
 await page.screenshot({ path: path.join(output, 'chart-mode-viewport.png'), fullPage: true });
 await context.tracing.stop({ path: path.join(output, 'chart-mode-viewport-trace.zip') });
 await context.close();
} finally { await browser.close(); }
`;
describe('actual browser chart fallback interactions', () => {
  it('keyboard and touch inspect exact canonical values while the vendor script is unavailable', async () => {
    actualJoined();
    const run = promisify(execFile);
    await run(process.execPath, ['--input-type=module', '-e', browserAdversarialJourney], { env: process.env, timeout: 180000, maxBuffer: 1024 * 1024 });
  }, 185000);
});
