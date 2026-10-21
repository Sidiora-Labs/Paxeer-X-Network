import { test } from 'node:test';
import assert from 'node:assert/strict';
import { jitter, scheduleActive, sequenceGuard, onStreamResync, notifyStreamResync, watchSocket } from '../src/core/net/realtime';
import { subscribeKline } from '../src/hooks/perps/useSidPaxMarketStream';

test('symbol socket is shared and closes 15 seconds after its last subscriber leaves', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'setInterval', 'Date'], now: 1000 });
  const keys = ['window', 'document', 'WebSocket'];
  const descriptors = keys.map(key => Object.getOwnPropertyDescriptor(globalThis, key));
  class Socket extends EventTarget {
    static OPEN = 1; static CONNECTING = 0; static instances: Socket[] = [];
    readyState = 1; closed = false; sent: string[] = [];
    constructor(public url: string) { super(); Socket.instances.push(this); }
    send(value: string) { this.sent.push(value); }
    close() { this.closed = true; this.readyState = 3; this.dispatchEvent(new Event('close')); }
  }
  Object.defineProperty(globalThis, 'window', { configurable: true, value: new EventTarget() });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: Object.assign(new EventTarget(), { hidden: false }) });
  Object.defineProperty(globalThis, 'WebSocket', { configurable: true, value: Socket });
  let stopA = () => {}, stopB = () => {};
  try {
    stopA = subscribeKline('BTC', '1m', {});
    stopB = subscribeKline('BTC', '1m', {});
    await Promise.resolve();
    assert.equal(Socket.instances.length, 1);
    const ws = Socket.instances[0]!;
    ws.dispatchEvent(new Event('open'));
    stopA(); t.mock.timers.tick(15000);
    assert.equal(ws.closed, false);
    stopB(); t.mock.timers.tick(14999);
    assert.equal(ws.closed, false);
    t.mock.timers.tick(1);
    assert.equal(ws.closed, true);
    assert.ok(ws.sent.some(frame => JSON.parse(frame).event === 'unsubscribe'));
  } finally {
    stopA(); stopB();
    keys.forEach((key, i) => descriptors[i] ? Object.defineProperty(globalThis, key, descriptors[i]!) : Reflect.deleteProperty(globalThis, key));
    t.mock.timers.reset();
  }
});

test('full jitter is bounded and capped', () => {
  assert.equal(jitter(1000, () => 0), 0);
  assert.equal(jitter(1000, () => 0.5), 500);
  assert.equal(jitter(120000, () => 0.5), 30000);
});

test('sequence tracking detects gaps and server restarts, snapshots reset baseline', () => {
  const accept = sequenceGuard();
  assert.equal(accept(null), true);
  assert.equal(accept({ topic: 'BTC', seq: 10, snapshotId: 'a', snapshot: true }), true);
  assert.equal(accept({ topic: 'BTC', seq: 11, snapshotId: 'a' }), true);
  assert.equal(accept({ topic: 'BTC', seq: 13, snapshotId: 'a' }), false);
  assert.equal(accept({ topic: 'BTC', seq: 12, snapshotId: 'b' }), false);
  assert.equal(accept({ topic: 'BTC', seq: 20, snapshotId: 'b', snapshot: true }), true);
});

test('resync callbacks are isolated and unsubscribed', () => {
  let count = 0;
  const stop = onStreamResync(url => { assert.equal(url, 'test'); count++; });
  notifyStreamResync('test'); stop(); notifyStreamResync('test');
  assert.equal(count, 1);
});

test('hidden tabs wait for visibility; cancelled reconnects cannot fire', async () => {
  const doc = Object.assign(new EventTarget(), { hidden: true });
  const win = new EventTarget();
  const previousDocument = Object.getOwnPropertyDescriptor(globalThis, 'document');
  const previousWindow = Object.getOwnPropertyDescriptor(globalThis, 'window');
  Object.defineProperty(globalThis, 'document', { configurable: true, value: doc });
  Object.defineProperty(globalThis, 'window', { configurable: true, value: win });
  let calls = 0;
  const cancel = scheduleActive(() => calls++, 0);
  try {
    await new Promise(resolve => setTimeout(resolve, 10));
    assert.equal(calls, 0);
    doc.hidden = false; doc.dispatchEvent(new Event('visibilitychange'));
    await new Promise(resolve => setTimeout(resolve, 10));
    assert.equal(calls, 1);
    const stop = scheduleActive(() => calls++, 0); stop();
    await new Promise(resolve => setTimeout(resolve, 10));
    assert.equal(calls, 1);
  } finally {
    cancel();
    if (previousDocument) Object.defineProperty(globalThis, 'document', previousDocument); else Reflect.deleteProperty(globalThis, 'document');
    if (previousWindow) Object.defineProperty(globalThis, 'window', previousWindow); else Reflect.deleteProperty(globalThis, 'window');
  }
});

test('half-open socket closes after deadline; open alone does not reset backoff', t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'setInterval', 'Date'], now: 1000 });
  const descriptors = ['window', 'document'].map(key => Object.getOwnPropertyDescriptor(globalThis, key));
  Object.defineProperty(globalThis, 'window', { configurable: true, value: new EventTarget() });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: Object.assign(new EventTarget(), { hidden: false }) });
  class Socket extends EventTarget {
    url = 'wss://test'; readyState = 1; closed = false;
    close() { this.closed = true; this.dispatchEvent(new Event('close')); }
  }
  const ws = new Socket(); let stable = 0;
  try {
    watchSocket(ws as unknown as WebSocket, () => stable++);
    ws.dispatchEvent(new Event('open'));
    assert.equal(stable, 0);
    t.mock.timers.tick(60000);
    assert.equal(ws.closed, true);
    assert.equal(stable, 0);
  } finally {
    ['window', 'document'].forEach((key, i) => descriptors[i] ? Object.defineProperty(globalThis, key, descriptors[i]!) : Reflect.deleteProperty(globalThis, key));
    t.mock.timers.reset();
  }
});

test('retryAfterHonoured', t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'setInterval', 'Date'], now: 1000 });
  const descriptors = ['window', 'document'].map(key => Object.getOwnPropertyDescriptor(globalThis, key));
  Object.defineProperty(globalThis, 'window', { configurable: true, value: new EventTarget() });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: Object.assign(new EventTarget(), { hidden: false }) });
  class Socket extends EventTarget {
    readyState = 1;
    constructor(public url: string) { super(); }
    shut(code: number, reason: string) { this.readyState = 3; this.dispatchEvent(Object.assign(new Event('close'), { code, reason })); }
  }
  const reconnectWaits = (url: string, code: number, reason: string, waitMs: number) => {
    const ws = new Socket(url);
    watchSocket(ws as unknown as WebSocket, () => {});
    ws.dispatchEvent(new Event('open'));
    ws.shut(code, reason);
    let fired = 0;
    const cancel = scheduleActive(() => fired++, 0, url);
    if (waitMs > 0) { t.mock.timers.tick(waitMs - 1); assert.equal(fired, 0, `${url} reconnected before ${waitMs} ms`); }
    t.mock.timers.tick(1);
    assert.equal(fired, 1, `${url} did not reconnect at ${waitMs} ms`);
    cancel();
  };
  try {
    reconnectWaits('wss://feed.test/ws/markets', 1013, '', 10000);
    reconnectWaits('wss://feed.test/ws/btc', 4000, 'retry-after=25', 25000);
    reconnectWaits('wss://feed.test/ws/eth', 1008, 'Retry_After: 600', 120000);
    reconnectWaits('wss://feed.test/ws/sol', 1006, 'abnormal', 0);
  } finally {
    ['window', 'document'].forEach((key, i) => descriptors[i] ? Object.defineProperty(globalThis, key, descriptors[i]!) : Reflect.deleteProperty(globalThis, key));
    t.mock.timers.reset();
  }
});
