import { test, before, after, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { createServer, type Server } from 'node:http';
import { createHash } from 'node:crypto';
import type { Duplex } from 'node:stream';
import type { AddressInfo } from 'node:net';
import { readFileSync } from 'node:fs';

type VenueModule = typeof import('../src/core/net/venueStream');

const WS_GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11';

class Peer {
  received: Record<string, unknown>[] = [];
  closeCode: number | null = null;
  ended = false;
  private buffer = Buffer.alloc(0);
  constructor(readonly path: string, private readonly socket: Duplex) {
    socket.on('data', chunk => { this.buffer = Buffer.concat([this.buffer, chunk]); this.drain(); });
    socket.on('close', () => { this.ended = true; });
    socket.on('error', () => { this.ended = true; });
  }
  private drain() {
    for (;;) {
      if (this.buffer.length < 2) return;
      const opcode = this.buffer[0]! & 0x0f;
      let length = this.buffer[1]! & 0x7f;
      let offset = 2;
      if (length === 126) { if (this.buffer.length < 4) return; length = this.buffer.readUInt16BE(2); offset = 4; }
      else if (length === 127) { if (this.buffer.length < 10) return; length = Number(this.buffer.readBigUInt64BE(2)); offset = 10; }
      if (this.buffer.length < offset + 4 + length) return;
      const mask = this.buffer.subarray(offset, offset + 4);
      const payload = Buffer.from(this.buffer.subarray(offset + 4, offset + 4 + length));
      for (let i = 0; i < payload.length; i++) payload[i] = payload[i]! ^ mask[i % 4]!;
      this.buffer = this.buffer.subarray(offset + 4 + length);
      if (opcode === 1) this.received.push(JSON.parse(payload.toString('utf8')));
      if (opcode === 8) {
        this.closeCode = payload.length >= 2 ? payload.readUInt16BE(0) : 1005;
        this.writeFrame(0x88, payload.subarray(0, 2));
        this.socket.end();
      }
    }
  }
  private writeFrame(head: number, payload: Buffer) {
    if (this.ended) return;
    const header = payload.length < 126
      ? Buffer.from([head, payload.length])
      : payload.length < 65536
        ? Buffer.from([head, 126, payload.length >> 8, payload.length & 0xff])
        : (() => { const h = Buffer.alloc(10); h[0] = head; h[1] = 127; h.writeBigUInt64BE(BigInt(payload.length), 2); return h; })();
    this.socket.write(Buffer.concat([header, payload]));
  }
  send(frame: unknown) { this.writeFrame(0x81, Buffer.from(JSON.stringify(frame), 'utf8')); }
  close(code: number) { const p = Buffer.alloc(2); p.writeUInt16BE(code); this.writeFrame(0x88, p); }
}

let server: Server;
let venue: VenueModule;
let origin = '';
let peers: Peer[] = [];
const restRequests: string[] = [];
let restBook: unknown = { success: true, data: { asks: [], bids: [] } };
const releases: (() => void)[] = [];

async function waitFor<T>(probe: () => T | undefined | false | null, label: string, timeoutMs = 3000): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = probe();
    if (value) return value;
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${label}`);
    await new Promise(resolve => setTimeout(resolve, 5));
  }
}

const tick = () => new Promise(resolve => setTimeout(resolve, 30));

function track<T>(subscribe: (listener: (value: T | null) => void) => () => void) {
  const values: (T | null)[] = [];
  const release = subscribe(value => { values.push(value); });
  releases.push(release);
  return { values, release, last: () => values[values.length - 1] };
}

async function openPeer(index = 0): Promise<Peer> {
  return waitFor(() => peers[index], `socket ${index}`);
}

function subscribes(peer: Peer) {
  return peer.received.filter(frame => frame.event === 'subscribe').map(frame => frame.topic);
}

before(async () => {
  Object.defineProperty(globalThis, 'window', { configurable: true, value: new EventTarget() });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: Object.assign(new EventTarget(), { hidden: false }) });
  server = createServer((req, res) => {
    restRequests.push(req.url ?? '');
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(restBook));
  });
  server.on('upgrade', (req, socket) => {
    const accept = createHash('sha1').update(`${req.headers['sec-websocket-key']}${WS_GUID}`).digest('base64');
    socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`);
    peers.push(new Peer(req.url ?? '', socket));
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL = origin;
  venue = await import('../src/core/net/venueStream');
});

afterEach(async () => {
  releases.splice(0).forEach(release => release());
  await waitFor(() => peers.every(peer => peer.ended), 'sockets to close');
  peers = [];
  restRequests.length = 0;
});

after(async () => {
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  Reflect.deleteProperty(globalThis, 'window');
  Reflect.deleteProperty(globalThis, 'document');
});

const bookFrame = (topic: string, seq: number, data: Record<string, unknown>, extra: Record<string, unknown> = {}) =>
  ({ topic, ts: Date.now(), snapshotId: 'proc-a', seq, data, ...extra });

type SpotBook = import('../src/core/net/venueStream').VenueBook;
type SpotTrades = import('../src/core/net/venueStream').SpotVenueTrade[];

const read = (path: string) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8');

test('spotBookAndTradesFromGateway', async () => {
  assert.match(read('src/widgets/spot/SpotOrderbook.tsx'), /useSpotVenueBook\(symbol\)/);
  assert.match(read('src/widgets/spot/SpotOrderbook.tsx'), /useSpotOrderbook\(venueBook \? undefined : symbol\)/);
  assert.match(read('src/widgets/spot/SpotTradesTape.tsx'), /useSpotVenueTrades\(symbol\)/);
  assert.match(read('src/widgets/spot/SpotTradesTape.tsx'), /useSpotTrades\(venueTrades \? undefined : symbol\)/);

  const book = track<SpotBook>(l => venue.subscribeSpotVenueBook('sid', l));
  const trades = track<SpotTrades>(l => venue.subscribeSpotVenueTrades('SID', l));
  const perp = track<SpotBook>(l => venue.subscribeVenueBook('SID', l));
  assert.deepEqual(book.values, [null]);
  assert.deepEqual(trades.values, [null]);
  const peer = await openPeer();
  assert.equal(peer.path, '/ws');
  await waitFor(() => subscribes(peer).length === 3, 'subscribes');
  assert.deepEqual(subscribes(peer), ['SID@orderbook', 'SID@trade', 'SID@perp_orderbook']);
  await tick();
  assert.equal(peers.length, 1);

  peer.send({ event: 'welcome', ts: Date.now(), snapshotId: 'proc-a' });
  peer.send(bookFrame('SID@orderbook', 1, { symbol: 'SID', asks: [[0.52, 100], [0.51, 50], ['bad', 1]], bids: [[0.49, 10], [0.5, 20]] }, { snapshot: true, ts: 1700000000000 }));
  await waitFor(() => book.last(), 'spot book snapshot');
  assert.deepEqual(book.last(), {
    asks: [{ price: 0.51, size: 50 }, { price: 0.52, size: 100 }],
    bids: [{ price: 0.5, size: 20 }, { price: 0.49, size: 10 }],
    markPrice: null,
    indexPrice: null,
    basisBps: null,
    ts: 1700000000000,
  });
  assert.equal(perp.last(), null);

  peer.send(bookFrame('SID@trade', 1, { symbol: 'SID', trades: [
    { id: 's2', side: 'SELL', price: 0.5, size: 3, trade_time: 2000 },
    { side: 'buy', price: 0.51, size: 1.5, trade_time: 1000 },
    { side: 'BUY', price: 'x', size: 1, trade_time: 900 },
  ] }, { snapshot: true }));
  await waitFor(() => trades.last(), 'trade snapshot');
  assert.deepEqual(trades.last(), [
    { id: 's2', side: 'SELL', price: 0.5, size: 3, trade_time: 2000 },
    { id: null, side: 'BUY', price: 0.51, size: 1.5, trade_time: 1000 },
  ]);

  peer.send(bookFrame('SID@trade', 2, { symbol: 'SID', side: 'BUY', price: 0.52, size: 4, trade_time: 3000 }));
  await waitFor(() => trades.last()?.length === 3, 'live trade');
  assert.deepEqual(trades.last()?.[0], { id: null, side: 'BUY', price: 0.52, size: 4, trade_time: 3000 });
  assert.equal(trades.last()?.[1]?.id, 's2');

  peer.send(bookFrame('SID@orderbook', 2, { symbol: 'SID', asks: [[0.53, 7]], bids: [[0.48, 8]] }));
  await waitFor(() => book.last()?.asks[0]?.price === 0.53, 'book delta');
  assert.deepEqual(book.last()?.bids, [{ price: 0.48, size: 8 }]);

  book.release();
  await waitFor(() => peer.received.filter(frame => frame.event === 'unsubscribe').length === 1, 'book unsubscribe');
  trades.release();
  await waitFor(() => peer.received.filter(frame => frame.event === 'unsubscribe').length === 2, 'trade unsubscribe');
  assert.deepEqual(peer.received.filter(frame => frame.event === 'unsubscribe'), [
    { event: 'unsubscribe', topic: 'SID@orderbook' },
    { event: 'unsubscribe', topic: 'SID@trade' },
  ]);
  assert.equal(peer.ended, false);
  assert.equal(peer.closeCode, null);
});

test('spotReferenceFallbackAndUnsubscribe', async () => {
  assert.equal(venue.isSpotVenueSymbol('BNB'), false);
  assert.equal(venue.isSpotVenueSymbol('pax'), true);
  const unlisted = track<SpotBook>(l => venue.subscribeSpotVenueBook('BNB', l));
  const unlistedTrades = track<SpotTrades>(l => venue.subscribeSpotVenueTrades('ETH', l));
  await tick();
  assert.deepEqual(unlisted.values, [null]);
  assert.deepEqual(unlistedTrades.values, [null]);
  assert.equal(peers.length, 0);

  const book = track<SpotBook>(l => venue.subscribeSpotVenueBook('PAX', l));
  assert.equal(book.values.length, 1);
  assert.equal(book.last(), null);
  const first = await openPeer(0);
  await waitFor(() => subscribes(first).length === 1, 'subscribe');
  assert.deepEqual(subscribes(first), ['PAX@orderbook']);
  first.send(bookFrame('PAX@orderbook', 4, { asks: [[1.01, 2]], bids: [[0.99, 3]] }, { snapshot: true }));
  await waitFor(() => book.last(), 'pax book');
  first.send(bookFrame('PAX@orderbook', 6, { asks: [[9, 9]], bids: [] }));
  await waitFor(() => first.closeCode !== null, 'gap close');
  assert.equal(first.closeCode, 4000);
  await waitFor(() => book.last() === null, 'socket-closed fallback');
  assert.ok(book.values.every(value => value?.asks[0]?.price !== 9));
  assert.equal(restRequests.length, 0);

  const second = await openPeer(1);
  await waitFor(() => subscribes(second).length === 1, 'resubscribe after reconnect');
  assert.deepEqual(subscribes(second), ['PAX@orderbook']);
  second.send(bookFrame('PAX@orderbook', 10, { asks: [[1.02, 1]], bids: [[0.98, 1]] }, { snapshot: true }));
  await waitFor(() => book.last()?.asks[0]?.price === 1.02, 'resubscribe snapshot');
  second.close(1001);
  await waitFor(() => book.last() === null, 'closed again');
  await waitFor(() => second.ended, 'second socket end');

  const third = await openPeer(2);
  await waitFor(() => subscribes(third).length === 1, 'third subscribe');
  book.release();
  await waitFor(() => third.closeCode !== null, 'idle socket close');
  assert.deepEqual(third.received.at(-1), { event: 'unsubscribe', topic: 'PAX@orderbook' });
  assert.equal(third.closeCode, 1000);
});
