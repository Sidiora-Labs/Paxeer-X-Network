import { test, before, after, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { createServer, type Server } from 'node:http';
import { createHash } from 'node:crypto';
import type { Duplex } from 'node:stream';
import type { AddressInfo } from 'node:net';

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

test('subscribeFrameIsDataProtocol', async () => {
  const book = track<import('../src/core/net/venueStream').VenueBook>(l => venue.subscribeVenueBook('btc', l));
  track<import('../src/core/net/venueStream').VenueFill[]>(l => venue.subscribeVenueFills('BTC', l));
  const peer = await openPeer();
  assert.equal(peer.path, '/ws');
  await waitFor(() => peer.received.length === 2, 'subscribe frames');
  assert.deepEqual(peer.received, [
    { event: 'subscribe', topic: 'BTC@perp_orderbook' },
    { event: 'subscribe', topic: 'BTC@perp_trade' },
  ]);
  book.release();
  await waitFor(() => peer.received.length === 3, 'unsubscribe frame');
  assert.deepEqual(peer.received[2], { event: 'unsubscribe', topic: 'BTC@perp_orderbook' });
  assert.equal(peer.ended, false);
});

test('snapshotThenDeltas', async () => {
  const book = track<import('../src/core/net/venueStream').VenueBook>(l => venue.subscribeVenueBook('BTC', l));
  const peer = await openPeer();
  await waitFor(() => subscribes(peer).length === 1, 'subscribe');
  peer.send({ event: 'welcome', ts: Date.now(), snapshotId: 'proc-a' });
  peer.send(bookFrame('BTC@perp_orderbook', 10, { asks: [[101, 2]], bids: [[99, 3]] }, { snapshot: true }));
  await waitFor(() => book.last()?.asks[0]?.price === 101, 'snapshot');
  peer.send(bookFrame('BTC@perp_orderbook', 11, { asks: [[102, 4]], bids: [[98, 5]] }));
  await waitFor(() => book.last()?.asks[0]?.price === 102, 'delta 11');
  const count = book.values.length;
  peer.send(bookFrame('BTC@perp_orderbook', 11, { asks: [[500, 1]], bids: [] }));
  peer.send(bookFrame('BTC@perp_orderbook', 10, { asks: [[600, 1]], bids: [] }));
  peer.send(bookFrame('BTC@perp_orderbook', 12, { asks: [[103, 6]], bids: [[97, 7]] }));
  await waitFor(() => book.last()?.asks[0]?.price === 103, 'delta 12');
  assert.equal(book.values.length, count + 1);
  assert.deepEqual(book.last()?.asks, [{ price: 103, size: 6 }]);
  assert.deepEqual(book.last()?.bids, [{ price: 97, size: 7 }]);
  assert.equal(peer.closeCode, null);
});

test('gapResyncsFromRest', async () => {
  restBook = { success: true, data: { asks: [{ price: 201, quantity: 1.5 }, { price: 200, quantity: 2 }], bids: [{ price: 198, quantity: 4 }, { price: 199, quantity: 3 }] } };
  const book = track<import('../src/core/net/venueStream').VenueBook>(l => venue.subscribeVenueBook('BTC', l));
  const first = await openPeer(0);
  await waitFor(() => subscribes(first).length === 1, 'subscribe');
  first.send({ event: 'welcome', ts: Date.now(), snapshotId: 'proc-a' });
  first.send(bookFrame('BTC@perp_orderbook', 10, { asks: [[101, 2]], bids: [[99, 3]] }, { snapshot: true }));
  first.send(bookFrame('BTC@perp_orderbook', 11, { asks: [[102, 2]], bids: [[99, 3]] }));
  await waitFor(() => book.last()?.asks[0]?.price === 102, 'delta 11');
  first.send(bookFrame('BTC@perp_orderbook', 13, { asks: [[900, 2]], bids: [[99, 3]] }));
  await waitFor(() => first.closeCode !== null, 'gap close');
  assert.equal(first.closeCode, venue.GAP_CLOSE_CODE);
  assert.equal(first.closeCode, 4000);
  assert.ok(book.values.every(value => value?.asks[0]?.price !== 900));
  await waitFor(() => restRequests.length === 1, 'REST snapshot');
  assert.equal(restRequests[0], '/api/orderbook/BTC?depth=100');
  const second = await openPeer(1);
  await waitFor(() => subscribes(second).length === 1, 'resubscribe');
  assert.deepEqual(subscribes(second), ['BTC@perp_orderbook']);
  await waitFor(() => book.last()?.asks[0]?.price === 200, 'REST book');
  assert.deepEqual(book.last()?.asks, [{ price: 200, size: 2 }, { price: 201, size: 1.5 }]);
  assert.deepEqual(book.last()?.bids, [{ price: 199, size: 3 }, { price: 198, size: 4 }]);
  second.send({ event: 'welcome', ts: Date.now(), snapshotId: 'proc-a' });
  second.send(bookFrame('BTC@perp_orderbook', 20, { asks: [[300, 1]], bids: [[299, 1]] }, { snapshot: true }));
  await waitFor(() => book.last()?.asks[0]?.price === 300, 'resubscribe snapshot');
  const count = book.values.length;
  second.send(bookFrame('BTC@perp_orderbook', 20, { asks: [[777, 1]], bids: [] }));
  second.send(bookFrame('BTC@perp_orderbook', 21, { asks: [[301, 1]], bids: [[298, 1]] }));
  await waitFor(() => book.last()?.asks[0]?.price === 301, 'delta 21');
  assert.equal(book.values.length, count + 1);
  assert.equal(second.closeCode, null);
});

test('restartDropsState', async () => {
  const book = track<import('../src/core/net/venueStream').VenueBook>(l => venue.subscribeVenueBook('ETH', l));
  const fills = track<import('../src/core/net/venueStream').VenueFill[]>(l => venue.subscribeVenueFills('ETH', l));
  const peer = await openPeer();
  await waitFor(() => subscribes(peer).length === 2, 'subscribes');
  peer.send({ event: 'welcome', ts: Date.now(), snapshotId: 'proc-a' });
  peer.send(bookFrame('ETH@perp_orderbook', 5, { asks: [[10, 1]], bids: [[9, 1]] }, { snapshot: true }));
  peer.send(bookFrame('ETH@perp_trade', 7, { trades: [{ id: 'f1', side: 'BUY', price: 10, contracts: 1, notional: 10, liquidation: false, trade_time: 1 }] }, { snapshot: true }));
  await waitFor(() => book.last() && fills.last()?.length === 1, 'snapshots');
  peer.received.length = 0;
  peer.send(bookFrame('ETH@perp_orderbook', 6, { asks: [[11, 1]], bids: [] }, { snapshotId: 'proc-b' }));
  await waitFor(() => peer.received.length === 4, 'resubscribe frames');
  assert.equal(book.last(), null);
  assert.equal(fills.last(), null);
  assert.deepEqual(peer.received, [
    { event: 'unsubscribe', topic: 'ETH@perp_orderbook' },
    { event: 'subscribe', topic: 'ETH@perp_orderbook' },
    { event: 'unsubscribe', topic: 'ETH@perp_trade' },
    { event: 'subscribe', topic: 'ETH@perp_trade' },
  ]);
  assert.ok(book.values.every(value => value?.asks[0]?.price !== 11));
  peer.send(bookFrame('ETH@perp_orderbook', 1, { asks: [[12, 2]], bids: [[8, 2]] }, { snapshot: true, snapshotId: 'proc-b' }));
  await waitFor(() => book.last()?.asks[0]?.price === 12, 'new process snapshot');
  assert.equal(fills.last(), null);
  assert.equal(peer.closeCode, null);
});

test('gatewaySocketIsShared', async () => {
  track(l => venue.subscribeVenueBook('BTC', l));
  track(l => venue.subscribeVenueFills('BTC', l));
  track(l => venue.subscribeVenueBook('ETH', l));
  const second = track(l => venue.subscribeVenueBook('BTC', l));
  const peer = await openPeer();
  await waitFor(() => subscribes(peer).length === 3, 'subscribes');
  await tick();
  assert.equal(peers.length, 1);
  assert.equal(peer.path, new URL((await import('../src/core/network/gateway')).gatewayUrls().venueWsUrl).pathname);
  assert.deepEqual(subscribes(peer), ['BTC@perp_orderbook', 'BTC@perp_trade', 'ETH@perp_orderbook']);
  second.release();
  await tick();
  assert.equal(peer.received.filter(frame => frame.event === 'unsubscribe').length, 0);
});

test('perpBookFromGateway', async () => {
  const book = track<import('../src/core/net/venueStream').VenueBook>(l => venue.subscribeVenueBook('SOL', l));
  const peer = await openPeer();
  await waitFor(() => subscribes(peer).length === 1, 'subscribe');
  peer.send({ event: 'subscribed', topic: 'SOL@perp_orderbook', ts: Date.now(), snapshotId: 'proc-a' });
  peer.send(bookFrame('SOL@perp_orderbook', 3, {
    symbol: 'SOL', mark_price: 150.5, index_price: 150.25, basis_bps: 16.6,
    asks: [[151, 30], [150.75, 25], ['bad', 1]],
    bids: [[149.5, 40], [150, 10]],
  }, { snapshot: true, ts: 1700000000000 }));
  await waitFor(() => book.last(), 'book');
  assert.deepEqual(book.last(), {
    asks: [{ price: 150.75, size: 25 }, { price: 151, size: 30 }],
    bids: [{ price: 150, size: 10 }, { price: 149.5, size: 40 }],
    markPrice: 150.5,
    indexPrice: 150.25,
    basisBps: 16.6,
    ts: 1700000000000,
  });
});

test('perpFillsFromGateway', async () => {
  const fills = track<import('../src/core/net/venueStream').VenueFill[]>(l => venue.subscribeVenueFills('BTC', l));
  const peer = await openPeer();
  await waitFor(() => subscribes(peer).length === 1, 'subscribe');
  assert.deepEqual(subscribes(peer), ['BTC@perp_trade']);
  const trades = Array.from({ length: venue.FILL_CAP }, (_, i) => ({ id: `t${i}`, side: i % 2 ? 'SELL' : 'BUY', price: 77000 - i, contracts: i + 1, notional: (i + 1) * 10, liquidation: i === 0, trade_time: 2000 - i }));
  peer.send(bookFrame('BTC@perp_trade', 40, { symbol: 'BTC', trades }, { snapshot: true }));
  await waitFor(() => fills.last()?.length === venue.FILL_CAP, 'fill snapshot');
  assert.deepEqual(fills.last()?.[0], { id: 't0', side: 'BUY', price: 77000, contracts: 1, notional: 10, liquidation: true, trade_time: 2000 });
  peer.send(bookFrame('BTC@perp_trade', 41, { symbol: 'BTC', side: 'sell', price: 77182.4, contracts: 25, notional: 250, liquidation: false, trade_time: 3000 }));
  await waitFor(() => fills.last()?.[0]?.trade_time === 3000, 'live fill');
  assert.equal(fills.last()?.length, venue.FILL_CAP);
  assert.deepEqual(fills.last()?.[0], { id: null, side: 'SELL', price: 77182.4, contracts: 25, notional: 250, liquidation: false, trade_time: 3000 });
  assert.equal(fills.last()?.[1]?.id, 't0');
  assert.equal(fills.last()?.[venue.FILL_CAP - 1]?.id, `t${venue.FILL_CAP - 2}`);
});

test('referenceFallbackAndUnsubscribe', async () => {
  const unlisted = track(l => venue.subscribeVenueBook('DOGE', l));
  assert.deepEqual(unlisted.values, [null]);
  assert.equal(venue.isVenueSymbol('DOGE'), false);
  assert.equal(venue.isVenueSymbol('btc'), true);

  const book = track<import('../src/core/net/venueStream').VenueBook>(l => venue.subscribeVenueBook('BTC', l));
  assert.deepEqual(book.values, [null]);
  const xau = track<import('../src/core/net/venueStream').VenueBook>(l => venue.subscribeVenueBook('XAU', l));
  const peer = await openPeer();
  await waitFor(() => subscribes(peer).length === 2, 'subscribes');
  assert.deepEqual(subscribes(peer), ['BTC@perp_orderbook', 'XAU@perp_orderbook']);
  await tick();
  assert.equal(peers.length, 1);

  peer.send(bookFrame('XAU@perp_orderbook', 1, { asks: [[2400, 1]], bids: [[2399, 1]] }, { snapshot: true }));
  await waitFor(() => xau.last(), 'xau book');
  peer.send({ event: 'error', message: 'unknown_topic', topic: 'XAU@perp_orderbook', ts: Date.now() });
  await waitFor(() => xau.last() === null, 'unlisted fallback');
  assert.equal(peer.ended, false);

  xau.release();
  await tick();
  assert.deepEqual(peer.received.filter(frame => frame.event === 'unsubscribe'), []);

  peer.send(bookFrame('BTC@perp_orderbook', 2, { asks: [[101, 1]], bids: [[99, 1]] }, { snapshot: true }));
  await waitFor(() => book.last(), 'btc book');
  peer.close(1001);
  await waitFor(() => book.last() === null, 'socket-closed fallback');
  await waitFor(() => peer.ended, 'first socket end');

  const reconnected = await openPeer(1);
  await waitFor(() => subscribes(reconnected).length === 1, 'resubscribe after reconnect');
  assert.deepEqual(subscribes(reconnected), ['BTC@perp_orderbook']);
  book.release();
  await waitFor(() => reconnected.closeCode !== null, 'idle socket close');
  assert.deepEqual(reconnected.received.at(-1), { event: 'unsubscribe', topic: 'BTC@perp_orderbook' });
  assert.equal(reconnected.closeCode, 1000);
  assert.equal(venue.MAX_SUBSCRIPTIONS, 200);
});
