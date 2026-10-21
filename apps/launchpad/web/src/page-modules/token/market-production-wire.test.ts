import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import type { IncomingMessage } from 'node:http';
import type { Duplex } from 'node:stream';
import { SdkClient, decodeV2Contract, browserStreamTransportV2, STREAM_PING_FRAME, STREAM_PING_INTERVAL_MS, type V2ContractTypes } from '@kindle/sdk';
import { MarketSession } from './market-session';
import { freshness, dataAgeMs, HEARTBEAT_LIVENESS_MS } from './market-presentation';
import { validateCandles } from '@/core/market/use-candles';
import { canonicalCandleToDisplayBar, createReplayUdfDatafeed, fetchCanonicalCandlePage } from '@/widgets/trading/udf-datafeed';
import { tradeHistoryAligned } from '@/widgets/trading/transaction-controls/history-alignment';

/**
 * The fixture is the exact wire output of the production Go code paths, recorded
 * with `go test -overlay` (scratch test files only) against the container-free
 * Postgres fixture:
 *  - api_stream rows written by core/stats-workers' production encoders
 *    (RecordMarketPublicationTx -> append_market_trade; PublicStatsProjector ->
 *    append_market_stats / append_market_price) for the real chain-125 pool in
 *    core/stats-workers/internal/projectionstats/testdata/real_pool_125.json,
 *  - served by core/api's production handlers: rest.RegisterProductionReads
 *    (GET /v2/snapshots, GET /v2/pools/{p}/stats), ws.Hub.HandleV2 (WS
 *    /v2/stream) and rest.RegisterAccountingV2WithAdmission (GET
 *    /v2/pools/{p}/trades, /candles).
 * The node server below only replays those recorded bytes.
 */
// `ws` ships no type declarations; type the slice this replay server uses.
interface ServerSocket { send(data: string): void; once(event: 'message', listener: (data: unknown) => void): void; on(event: 'message', listener: (data: unknown) => void): void; terminate(): void }
interface WebSocketServerInstance { clients: Set<ServerSocket>; handleUpgrade(request: IncomingMessage, socket: Duplex, head: Buffer, done: (socket: ServerSocket) => void): void; close(): void }
type WsModule = { WebSocketServer: new (options: { noServer: true; handleProtocols: (protocols: Set<string>) => string | false }) => WebSocketServerInstance };
const { WebSocketServer } = createRequire(import.meta.url)('ws') as WsModule;

type Recorded = { path: string; status: number; headers: Record<string, string>; body: unknown };
interface Wire {
  origin: string;
  snapshot: Recorded;
  stats: Recorded;
  subscribe: { op: 'subscribe'; subscriptionId: string; topics: string[]; afterCursor: string };
  replayFrames: unknown[];
  liveFrames: unknown[];
  trades: Recorded;
  tradesFirstPage: Recorded;
  candles: Recorded;
  heartbeat: { provenance: string; request: string; frameBytes: string };
}
const wire = JSON.parse(readFileSync(new URL('../../core/test-fixtures/market-production/core-api-wire.json', import.meta.url), 'utf8')) as Wire;
const pool = '0xce20da50f92d21d8bee497910c782e9aa25b6263';

const sortedParams = (url: URL) => [...url.searchParams.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([key, value]) => `${key}=${value}`).join('&');
const sameRequest = (actual: URL, recordedPath: string) => {
  const recorded = new URL(recordedPath, 'http://recorded.invalid');
  return actual.pathname === recorded.pathname && sortedParams(actual) === sortedParams(recorded);
};

let server: Server;
let sockets: WebSocketServerInstance;
let origin = '';
const requests: string[] = [];
const subscriptions: unknown[] = [];
let streamLive = false;
let liveSocket: ServerSocket | null = null;
/** Client text frames received after subscribe (pings), with arrival time. */
const actions: { at: number; raw: string }[] = [];

beforeAll(async () => {
  server = createServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://fixture.invalid');
    requests.push(`${request.method} ${url.pathname}${url.search}`);
    const recorded = [wire.snapshot, wire.stats, wire.trades, wire.tradesFirstPage, wire.candles].find(entry => sameRequest(url, entry.path));
    if (!recorded) { response.writeHead(404, { 'Content-Type': 'application/json' }); response.end('{}'); return; }
    response.writeHead(recorded.status, recorded.headers);
    response.end(JSON.stringify(recorded.body));
  });
  sockets = new WebSocketServer({ noServer: true, handleProtocols: protocols => protocols.has('sidiora.v2') ? 'sidiora.v2' : false });
  server.on('upgrade', (request, socket, head) => {
    if (request.url !== '/v2/stream') { socket.destroy(); return; }
    sockets.handleUpgrade(request, socket, head, client => {
      let subscribed = false;
      client.on('message', raw => {
        if (subscribed) {
          // core/api answers every {"op":"ping"} with one heartbeat control frame.
          actions.push({ at: Date.now(), raw: String(raw) });
          if (String(raw) === wire.heartbeat.request) client.send(wire.heartbeat.frameBytes);
          return;
        }
        subscribed = true;
        const frame: unknown = JSON.parse(String(raw));
        subscriptions.push(frame);
        for (const value of wire.replayFrames) client.send(JSON.stringify(value));
        liveSocket = client;
        streamLive = true;
      });
    });
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});
afterAll(async () => {
  for (const client of sockets.clients) client.terminate();
  sockets.close();
  await new Promise<void>(resolve => server.close(() => resolve()));
});

async function eventually(check: () => boolean, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not reached');
    await new Promise(resolve => setTimeout(resolve, 20));
  }
}

function client() {
  const stream = origin.replace(/^http/, 'ws');
  // production:false only because the replay server listens on plain http/ws.
  return new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false });
}

describe('production core/api market wire through the web parsers', () => {
  it('validates every recorded production body against the shared v2 contract', () => {
    expect(decodeV2Contract('TopicSnapshotReadResponse', wire.snapshot.body).quality).toBe('partial');
    expect(decodeV2Contract('StatsResponse', wire.stats.body).data.stats.poolAddress).toBe(pool);
    expect(decodeV2Contract('TradesResponse', wire.trades.body).data.items).toHaveLength(2);
    expect(decodeV2Contract('CandlesResponse', wire.candles.body).quality).toBe('partial');
    for (const frame of [...wire.replayFrames, ...wire.liveFrames]) {
      if (typeof frame === 'object' && frame !== null && 'op' in frame) decodeV2Contract('StreamControl', frame);
      else decodeV2Contract('StreamEvent', frame);
    }
  });

  it('MarketSession installs the production snapshot, merges the trades page and applies live frames', async () => {
    const session = new MarketSession(client(), pool.toUpperCase().replace('0X', '0x'));
    try {
      await session.start();
      await eventually(() => session.getSnapshot().stream === 'live');
      const subscribe = decodeV2Contract('Subscribe', subscriptions.at(-1));
      expect([...subscribe.topics].sort()).toEqual([...wire.subscribe.topics].sort());
      expect(subscribe.afterCursor).toBe(decodeV2Contract('TopicSnapshotReadResponse', wire.snapshot.body).data.snapshot.resumeCursor);
      expect(requests).toContain(`GET ${wire.snapshot.path}`);

      let state = session.getSnapshot();
      expect(state.rows).toHaveLength(1);
      expect(state.rows[0].side).toBe('sell');
      expect(state.stats?.buys24h).toBe('1');
      expect(state.stats?.sells24h).toBe('1');
      expect(state.stats?.realUsdlBalanceRaw).toBe('49721088');
      // Interface 2.2.0 fields from the snapshot's market.stats entry (re-recorded against the 2.2.0 producer).
      expect(state.stats).toMatchObject({ spotPriceWad: '10098226981692', totalSupplyValuationUsdlRaw: '10098226981', circulatingMarketCapUsdlRaw: '48505893', supplyValuationQuality: 'verified', uniqueTraders24h: '1', txCount24h: '2', top10HolderShareBps: 10000, creatorHoldingShareBps: 10000, changeBaselineQuality1h: 'insufficient_history', priceChangeBps1h: null });
      expect(state.price?.source.kind).toBe('gross_execution');
      expect(state.priceRevision).toBe('2');
      expect(state.revision).toBe('2');
      expect(state.heartbeatAt).not.toBeNull();

      // The IF_TRADES page as useMarketTrades requests it, merged by the session.
      const trades = await client().requestV2(`/v2/pools/${pool}/trades?limit=100`, 'TradesResponse', { idempotentRead: true });
      expect(trades.quality).toBe('partial');
      session.mergePage(trades.data.items, trades.revision);
      state = session.getSnapshot();
      expect(state.rows.map(row => row.side)).toEqual(['sell', 'buy']);

      // Live delivery: one trade, one stats and one price frame, then a checkpoint.
      for (const value of wire.liveFrames) liveSocket?.send(JSON.stringify(value));
      await eventually(() => session.getSnapshot().rows.length === 3 && session.getSnapshot().priceRevision === '3' && session.getSnapshot().stats?.watermark.projectionRevision === '2');
      state = session.getSnapshot();
      expect(state.stream).toBe('live');
      expect(state.error).toBeNull();
      expect(state.rows[0].side).toBe('buy');
      expect(state.stats?.sells24h).toBe('0');
      // The live market.stats frame carries the 2.2.0 fields onto the same session state the coin cells read.
      expect(state.stats).toMatchObject({ spotPriceWad: '10222435173566', totalSupplyValuationUsdlRaw: '10222435173', priceChangeBps5m: '122', priceChangeBps1h: '122', priceChangeBps6h: '122', changeBaselineQuality5m: 'complete', changeBaselineQuality1h: 'complete', changeBaselineQuality6h: 'complete', txCount24h: '1', uniqueTraders24h: '1' });
      expect(state.revision).toBe('3');
    } finally { session.close(); }
    expect(streamLive).toBe(true);
  });

  it('a confirmed swap aligns with the production IF_TRADES page (quality "partial")', () => {
    const history = decodeV2Contract('TradesResponse', wire.trades.body);
    const sell = history.data.items.find(trade => trade.side === 'sell');
    if (!sell || !history.watermark) throw new Error('production sell row and watermark required');
    expect(tradeHistoryAligned(history, BigInt(sell.position.height), sell.position.transactionHash.toUpperCase().replace('0X', '0x'))).toBe(true);
    expect(tradeHistoryAligned(history, BigInt(history.watermark.height) + 1n, sell.position.transactionHash)).toBe(false);
    expect(tradeHistoryAligned(history, BigInt(sell.position.height), '0x' + 'e'.repeat(64))).toBe(false);
    expect(tradeHistoryAligned({ ...history, quality: 'stale' }, BigInt(sell.position.height), sell.position.transactionHash)).toBe(false);
  });

  it('a production trades page with a continuation cursor passes the history merge', async () => {
    const page = await client().requestV2(`/v2/pools/${pool}/trades?limit=1`, 'TradesResponse', { idempotentRead: true });
    expect(page.nextCursor).toEqual(expect.any(String));
    expect(page.data.items).toHaveLength(1);
    expect(page.revision).toBe('2');
  });

  it('canonical candle readers accept the production page (quality "partial")', async () => {
    const recorded = new URL(wire.candles.path, 'http://recorded.invalid');
    const from = recorded.searchParams.get('from') as string, to = recorded.searchParams.get('to') as string;
    const page = await fetchCanonicalCandlePage(pool, '1', from, to, 50, undefined, undefined, origin);
    expect(page.data.items).toHaveLength(1);
    const candle: V2ContractTypes['Candle'] = page.data.items[0];
    expect(validateCandles(page, pool, '1m').data.items[0].start).toBe(candle.start);
    const bar = canonicalCandleToDisplayBar(candle);
    expect(bar.time).toBe(Date.parse(candle.start));
    expect(bar.volume).toBeGreaterThan(0);

    const feed = createReplayUdfDatafeed(pool, 'price', () => {}, undefined, { apiOrigin: origin, valuationOrigin: origin });
    try {
      const outcome = await new Promise<{ bars: number } | { error: string }>(resolve => feed.getBars({}, '1', { from: Date.parse(from) / 1000, to: Date.parse(to) / 1000 }, bars => resolve({ bars: bars.length }), error => resolve({ error })));
      expect(outcome).toEqual({ bars: 1 });
    } finally { feed.dispose(); }
  });
});

describe('stream liveness from the production heartbeat', () => {
  it('the recorded heartbeat is the production StreamControl answer to the SDK ping frame', () => {
    expect(STREAM_PING_FRAME).toBe(wire.heartbeat.request);
    const control = decodeV2Contract('StreamControl', JSON.parse(wire.heartbeat.frameBytes));
    expect(control.op).toBe('heartbeat');
    expect(control.subscriptionId).toBe(wire.subscribe.subscriptionId);
    expect(control.cursor).toBeNull();
    expect(STREAM_PING_INTERVAL_MS).toBeLessThan(HEARTBEAT_LIVENESS_MS);
    // core/api broker.ActionLimit closes a connection above 10 actions per second.
    expect(1000 / STREAM_PING_INTERVAL_MS).toBeLessThan(10);
  });

  it('the SDK transport pings after subscribe at the configured cadence and stops on close', async () => {
    const transport = browserStreamTransportV2(client(), { applicationOrigin: 'http://127.0.0.1:3050', snapshotPath: '/v2/snapshots' }, false, { pingIntervalMs: 1000 });
    const controller = new AbortController();
    const received: unknown[] = [];
    const before = actions.length, subscribesBefore = subscriptions.length;
    const connection = await transport.connect({ signal: controller.signal, ticket: undefined, receive: value => received.push(value), disconnected: () => {} });
    try {
      await new Promise(resolve => setTimeout(resolve, 1500));
      // No ping before the subscribe frame is on the wire.
      expect(actions.length).toBe(before);
      expect(subscriptions.length).toBe(subscribesBefore);
      connection.send(decodeV2Contract('Subscribe', wire.subscribe));
      await eventually(() => actions.length - before >= 2, 4000);
      const pings = actions.slice(before);
      expect(pings.every(action => action.raw === STREAM_PING_FRAME)).toBe(true);
      expect(pings[1].at - pings[0].at).toBeGreaterThanOrEqual(900);
      await eventually(() => received.filter(value => (value as { op?: string }).op === 'heartbeat').length >= 2, 2000);
    } finally { connection.close(); controller.abort(); }
    const afterClose = actions.length;
    await new Promise(resolve => setTimeout(resolve, 2200));
    expect(actions.length).toBe(afterClose);
    expect(() => browserStreamTransportV2(client(), { applicationOrigin: 'http://127.0.0.1:3050', snapshotPath: '/v2/snapshots' }, false, { pingIntervalMs: 50 })).toThrow(RangeError);
  }, 15_000);

  it('an idle pool on a healthy stream stays current from heartbeats while its data ages', async () => {
    const session = new MarketSession(client(), pool);
    try {
      await session.start();
      await eventually(() => session.getSnapshot().stream === 'live');
      const checkpointAt = session.getSnapshot().heartbeatAt as number;
      const pingsBefore = actions.length;
      // No market frames are sent: only the server's heartbeat replies to the SDK's pings.
      await eventually(() => (session.getSnapshot().heartbeatAt ?? 0) > checkpointAt, STREAM_PING_INTERVAL_MS + 3000);
      expect(actions.length).toBeGreaterThan(pingsBefore);
      const state = session.getSnapshot();
      const now = Date.now();
      // The recorded projection is far older than 30 s (it would read stale by data age) ...
      expect(dataAgeMs(now, state.watermark)).toBeGreaterThan(30_000);
      // ... yet the heartbeat keeps the stream current.
      expect(session.status(now)).toBe('current');
      // Without further heartbeats liveness lapses to reconnecting, then stale.
      const lapsed = { generatedAt: state.generatedAt, heartbeatAt: state.heartbeatAt, contiguous: true, watermark: state.watermark };
      expect(freshness({ ...lapsed, now: (state.heartbeatAt as number) + HEARTBEAT_LIVENESS_MS + 1 })).toBe('reconnecting');
      expect(freshness({ ...lapsed, now: (state.heartbeatAt as number) + 30_001 })).toBe('stale');
    } finally { session.close(); }
  }, 15_000);
});
