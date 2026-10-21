import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { createServer, type Server } from 'node:http';
import { createHash } from 'node:crypto';
import type { Duplex } from 'node:stream';
import type { AddressInfo } from 'node:net';
import { readFileSync } from 'node:fs';

const WS_GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11';

class Peer {
  received: Record<string, unknown>[] = [];
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
      if (opcode === 8) { this.writeFrame(0x88, payload.subarray(0, 2)); this.socket.end(); }
    }
  }
  private writeFrame(head: number, payload: Buffer) {
    if (this.ended) return;
    const header = payload.length < 126
      ? Buffer.from([head, payload.length])
      : Buffer.from([head, 126, payload.length >> 8, payload.length & 0xff]);
    this.socket.write(Buffer.concat([header, payload]));
  }
  send(frame: unknown) { this.writeFrame(0x81, Buffer.from(JSON.stringify(frame), 'utf8')); }
  destroy() { this.socket.destroy(); }
  subscribed(topic: string) { return this.received.some(frame => frame.event === 'subscribe' && frame.topic === topic); }
}

type Book = { bids: { price: number; size: number }[]; asks: { price: number; size: number }[]; ts: number };

let server: Server;
let port = 0;
const peers: Peer[] = [];
const NativeWebSocket = globalThis.WebSocket;

async function waitFor<T>(probe: () => T | undefined | false | null, label: string, timeoutMs = 5000): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = probe();
    if (value) return value;
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${label}`);
    await new Promise(resolve => setTimeout(resolve, 5));
  }
}

const livePeer = (topic: string, after: Peer[]) =>
  waitFor(() => peers.find(peer => !after.includes(peer) && !peer.ended && peer.subscribed(topic)), `subscribe ${topic}`);

const levels = (book: Book | null) => book && { asks: book.asks.map(l => [l.price, l.size]), bids: book.bids.map(l => [l.price, l.size]) };

before(async () => {
  Object.defineProperty(globalThis, 'window', { configurable: true, value: new EventTarget() });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: Object.assign(new EventTarget(), { hidden: false }) });
  server = createServer((_req, res) => { res.writeHead(404); res.end(); });
  server.on('upgrade', (req, socket) => {
    const accept = createHash('sha1').update(`${req.headers['sec-websocket-key']}${WS_GUID}`).digest('base64');
    socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`);
    peers.push(new Peer(req.url ?? '', socket));
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  port = (server.address() as AddressInfo).port;
  process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL = `http://127.0.0.1:${port}`;
  const { FEED_ORIGIN } = await import('../src/core/feeds');
  const feedWs = FEED_ORIGIN.replace(/^http/, 'ws');
  class LocalFeedWebSocket extends NativeWebSocket {
    constructor(url: string | URL, protocols?: string | string[]) {
      const target = String(url);
      super(target.startsWith(feedWs) ? `ws://127.0.0.1:${port}${target.slice(feedWs.length)}` : target, protocols);
    }
  }
  Object.defineProperty(globalThis, 'WebSocket', { configurable: true, writable: true, value: LocalFeedWebSocket });
});

after(async () => {
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  Object.defineProperty(globalThis, 'WebSocket', { configurable: true, writable: true, value: NativeWebSocket });
  Reflect.deleteProperty(globalThis, 'window');
  Reflect.deleteProperty(globalThis, 'document');
});

async function disconnectResetsBook(subscribe: (cb: (book: Book | null) => void) => () => void, topic: string) {
  const values: (Book | null)[] = [];
  const seen = [...peers];
  const release = subscribe(book => { values.push(book); });
  try {
    const first = await livePeer(topic, seen);
    assert.equal(first.path, '/ws/sid');
    first.send({ topic, ts: Date.now(), data: { asks: [[1.02, 5], [1.01, 3]], bids: [[0.99, 4], [0.98, 6]], symbol: 'SID' } });
    await waitFor(() => values.length === 1, 'snapshot');
    assert.deepEqual(levels(values[0]!), { asks: [[1.01, 3], [1.02, 5]], bids: [[0.99, 4], [0.98, 6]] });

    first.destroy();
    await waitFor(() => values.length === 2, 'disconnect reset');
    assert.equal(values[1], null);

    const second = await livePeer(topic, [...seen, first]);
    await new Promise(resolve => setTimeout(resolve, 30));
    assert.equal(values.length, 2);
    second.send({ topic, ts: Date.now(), data: { asks: [[1.05, 2]], bids: [[0.95, 7]], symbol: 'SID' } });
    await waitFor(() => values.length === 3, 'reconnect snapshot');
    assert.deepEqual(levels(values[2]!), { asks: [[1.05, 2]], bids: [[0.95, 7]] });
  } finally {
    release();
  }
}

test('referencePerpBookResetsOnDisconnect', async () => {
  const { subscribeOrderbook } = await import('../src/hooks/perps/useSidPaxMarketStream');
  await disconnectResetsBook(cb => subscribeOrderbook('SID', cb), 'SID@perp_orderbook');
});

test('referenceSpotBookResetsOnDisconnect', async () => {
  const { subscribeSpotOrderbook } = await import('../src/hooks/spot/useSpotMarketStream');
  await disconnectResetsBook(cb => subscribeSpotOrderbook('SID', cb), 'SID@orderbook');
});

test('spotOrderbookNullBranchDropsQueuedFrame', () => {
  const source = readFileSync(new URL('../src/widgets/spot/SpotOrderbook.tsx', import.meta.url), 'utf8');
  const branch = /if \(!spotBook\) \{([\s\S]*?)setBook\(null\);\s*return;/.exec(source);
  assert.ok(branch, 'null source branch');
  assert.match(branch[1]!, /pendingBookRef\.current = null;/);
  assert.match(branch[1]!, /cancelAnimationFrame\(rafIdRef\.current\)/);
});
