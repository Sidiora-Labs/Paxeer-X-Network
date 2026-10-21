import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { createServer, type Server, type IncomingMessage } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { Duplex } from 'node:stream';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { SdkClient, decodeV2Contract, type V2ContractTypes, type V2StreamEvent, type V2TopicSnapshot, type ReconnectStatus } from '@kindle/sdk';
import { subscribeMarketTopics, marketTopics, TopicConnectionPolicyError, RANKING_CATEGORIES, PUBLIC_SNAPSHOT_PATH } from './data-stream';
import { rankingList } from '@/hooks/market/use-ranking';

/**
 * The public v2 market stream (the replacement for the legacy `/ws` hub) run
 * for real against a node server that only replays the exact production wire
 * bytes recorded from core/api's production handlers
 * (src/core/test-fixtures/market-production/core-api-wire.json: GET
 * /v2/snapshots from rest.RegisterProductionReads, WS /v2/stream frames from
 * ws.Hub.HandleV2, the heartbeat from broker.StreamControl).
 */
interface ServerSocket { send(data: string): void; on(event: 'message', listener: (data: unknown) => void): void; on(event: 'close', listener: () => void): void; terminate(): void }
interface WebSocketServerInstance { clients: Set<ServerSocket>; handleUpgrade(request: IncomingMessage, socket: Duplex, head: Buffer, done: (socket: ServerSocket) => void): void; close(): void }
type WsModule = { WebSocketServer: new (options: { noServer: true; handleProtocols: (protocols: Set<string>) => string | false }) => WebSocketServerInstance };
const { WebSocketServer } = createRequire(import.meta.url)('ws') as WsModule;

type Recorded = { path: string; status: number; headers: Record<string, string>; body: unknown };
interface Wire {
  snapshot: Recorded;
  subscribe: { op: 'subscribe'; subscriptionId: string; topics: string[]; afterCursor: string };
  replayFrames: unknown[];
  liveFrames: unknown[];
  heartbeat: { request: string; frameBytes: string };
}
const wire = JSON.parse(readFileSync(new URL('../test-fixtures/market-production/core-api-wire.json', import.meta.url), 'utf8')) as Wire;
const pool = '0xce20da50f92d21d8bee497910c782e9aa25b6263';
const topics = [marketTopics.price(pool), marketTopics.stats(pool), marketTopics.trades(pool)];
const applicationOrigin = 'http://127.0.0.1:3050';

let server: Server;
let sockets: WebSocketServerInstance;
let origin = '';
const snapshotRequests: string[] = [];
const subscribes: unknown[] = [];
let opened = 0;
let closed = 0;
let live: ServerSocket | null = null;
let pings = 0;

beforeAll(async () => {
  server = createServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://fixture.invalid');
    const recorded = new URL(wire.snapshot.path, 'http://fixture.invalid');
    if (url.pathname === recorded.pathname) snapshotRequests.push(url.search);
    if (url.pathname === recorded.pathname && url.searchParams.get('topics') === recorded.searchParams.get('topics')) {
      response.writeHead(wire.snapshot.status, wire.snapshot.headers);
      response.end(JSON.stringify(wire.snapshot.body));
      return;
    }
    response.writeHead(404, { 'Content-Type': 'application/json' });
    response.end('{}');
  });
  sockets = new WebSocketServer({ noServer: true, handleProtocols: protocols => protocols.has('sidiora.v2') ? 'sidiora.v2' : false });
  server.on('upgrade', (request, socket, head) => {
    if (request.url !== '/v2/stream') { socket.destroy(); return; }
    sockets.handleUpgrade(request, socket, head, client => {
      opened++;
      let subscribed = false;
      client.on('close', () => { closed++; });
      client.on('message', raw => {
        if (subscribed) { if (String(raw) === wire.heartbeat.request) { pings++; client.send(wire.heartbeat.frameBytes); } return; }
        subscribed = true;
        subscribes.push(JSON.parse(String(raw)));
        for (const value of wire.replayFrames) client.send(JSON.stringify(value));
        live = client;
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

/** A fresh client per test: the shared connection is keyed by client. production:false only because the replay server is plain http/ws. */
function client() {
  const stream = origin.replace(/^http/, 'ws');
  return new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false });
}

describe('public v2 market stream (production wire replay)', () => {
  it('installs the production snapshot, then delivers live trade, stats and price frames', async () => {
    const snapshots: V2TopicSnapshot[] = [];
    const events: V2StreamEvent[] = [];
    const statuses: ReconnectStatus[] = [];
    const release = subscribeMarketTopics(client(), { topics, onSnapshot: value => snapshots.push(value), onEvent: event => events.push(event), onStatus: status => statuses.push(status) }, applicationOrigin);
    try {
      await eventually(() => statuses.some(status => status.state === 'live'));
      expect(snapshotRequests.at(-1)).toBe(`?topics=${encodeURIComponent([...topics].sort().join(','))}`);
      expect(PUBLIC_SNAPSHOT_PATH).toBe(new URL(wire.snapshot.path, 'http://x.invalid').pathname);
      const subscribe = decodeV2Contract('Subscribe', subscribes.at(-1));
      expect([...subscribe.topics].sort()).toEqual([...topics].sort());
      expect(subscribe.afterCursor).toBe(decodeV2Contract('TopicSnapshotReadResponse', wire.snapshot.body).data.snapshot.resumeCursor);
      expect(snapshots).toHaveLength(1);
      expect(snapshots[0].topics.map(entry => entry.topic).sort()).toEqual([...topics].sort());

      for (const value of wire.liveFrames) live?.send(JSON.stringify(value));
      // Trades fan out at once; stats and price are coalesced to one frame per topic.
      await eventually(() => new Set(events.map(event => event.topic)).size === 3);
      const trade = events.find(event => event.topic === marketTopics.trades(pool))?.payload as V2ContractTypes['Trade'];
      expect(trade.side).toBe('buy');
      const stats = events.find(event => event.topic === marketTopics.stats(pool))?.payload as V2ContractTypes['PoolStats'];
      expect(stats.sells24h).toBe('0');
      expect(statuses.at(-1)?.state).toBe('live');
    } finally { release(); }
    await eventually(() => closed === opened);
  });

  it('shares one connection and snapshot between subscribers and closes it with the last release', async () => {
    const shared = client();
    const openedBefore = opened, snapshotsBefore = snapshotRequests.length;
    const firstStatus: ReconnectStatus[] = [];
    const first = subscribeMarketTopics(shared, { topics, onEvent: () => {}, onStatus: status => firstStatus.push(status) }, applicationOrigin);
    await eventually(() => firstStatus.some(status => status.state === 'live'));
    const tradeSnapshots: V2TopicSnapshot[] = [];
    const tradeEvents: V2StreamEvent[] = [];
    const second = subscribeMarketTopics(shared, { topics: [marketTopics.trades(pool)], onSnapshot: value => tradeSnapshots.push(value), onEvent: event => tradeEvents.push(event) }, applicationOrigin);
    try {
      // The union is unchanged: no new socket or snapshot request; the member gets its topic from the installed snapshot.
      expect(opened - openedBefore).toBe(1);
      expect(snapshotRequests.length - snapshotsBefore).toBe(1);
      expect(tradeSnapshots.at(-1)?.topics.map(entry => entry.topic)).toEqual([marketTopics.trades(pool)]);
      for (const value of wire.liveFrames) live?.send(JSON.stringify(value));
      await eventually(() => tradeEvents.length === 1);
      expect(tradeEvents.every(event => event.topic === marketTopics.trades(pool))).toBe(true);
      first();
      expect(closed).toBeLessThan(opened);
    } finally { second(); first(); }
    await eventually(() => closed === opened);
  });

  it('answers the SDK ping with the production heartbeat on the shared connection', async () => {
    const statuses: ReconnectStatus[] = [];
    const release = subscribeMarketTopics(client(), { topics, onEvent: () => {}, onStatus: status => statuses.push(status) }, applicationOrigin);
    try {
      await eventually(() => statuses.some(status => status.state === 'live'));
      const before = pings;
      // A heartbeat control frame is consumed by the stream without leaving 'live'.
      await eventually(() => pings > before, 6000);
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(statuses.at(-1)?.state).toBe('live');
      expect(statuses.every(status => status.state !== 'resync-required')).toBe(true);
    } finally { release(); }
  }, 15_000);

  it('enforces the 20-topic connection policy and canonical topic names', () => {
    const many = Array.from({ length: 21 }, (_, index) => marketTopics.trades('0x' + (index + 1).toString(16).padStart(40, '0')));
    // One subscription above 20 topics violates the Subscribe contract itself.
    expect(() => subscribeMarketTopics(client(), { topics: many, onEvent: () => {} }, applicationOrigin)).toThrow('more than 20');
    // A union above 20 across subscribers needs an explicit separate connection.
    const shared = client();
    const first = subscribeMarketTopics(shared, { topics: many.slice(0, 15), onEvent: () => {} }, applicationOrigin);
    try {
      expect(() => subscribeMarketTopics(shared, { topics: many.slice(15), onEvent: () => {} }, applicationOrigin)).toThrow(TopicConnectionPolicyError);
    } finally { first(); }
    expect(marketTopics.trades(pool.toUpperCase().replace('0X', '0x'))).toBe(`market.trades.${pool}`);
    expect(RANKING_CATEGORIES.map(category => marketTopics.rankings(category))).toContain('market.rankings.trending');
    expect(() => marketTopics.rankings('hot' as never)).toThrow(TypeError);
    expect(() => marketTopics.stats('0x123')).toThrow(TypeError);
  });

  it('is a no-op without an application origin (server render)', () => {
    const before = opened;
    const release = subscribeMarketTopics(client(), { topics, onEvent: () => {} });
    release();
    expect(opened).toBe(before);
  });

  it('projects a contract-valid ranking snapshot onto the ranking list window', () => {
    const watermark = (decodeV2Contract('TopicSnapshotReadResponse', wire.snapshot.body).data.snapshot.topics.find(entry => entry.watermark)?.watermark) as V2ContractTypes['Watermark'];
    const row = (rank: number, address: string) => ({ category: 'trending' as const, computedAt: watermark.observedAt, modelVersion: '1', poolAddress: address, rank, score: String(100 - rank) });
    const snapshot = decodeV2Contract('RankingSnapshot', { category: 'trending', computedAt: watermark.observedAt, modelVersion: '1', snapshotId: '5f2791ac-96ad-4908-8df6-ee29c17d4766', replace: true, totalEligible: '3', truncated: false, watermark, items: [row(2, '0x' + '2'.repeat(40)), row(1, pool.toUpperCase().replace('0X', '0x')), row(3, '0x' + '3'.repeat(40))] });
    const list = rankingList(snapshot, 2, 0);
    expect(list).toMatchObject({ category: 'trending', total: 3, limit: 2, offset: 0 });
    expect(list.items.map(item => [item.rank, item.poolAddress])).toEqual([[1, pool], [2, '0x' + '2'.repeat(40)]]);
    expect(rankingList(snapshot, 2, 2).items.map(item => item.rank)).toEqual([3]);
    expect(() => rankingList({ ...snapshot, watermark: { ...watermark, integrity: 'quarantined' } }, 2, 0)).toThrow('Rankings are not loading right now');
  });
});
