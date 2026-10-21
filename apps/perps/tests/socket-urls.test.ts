import { test, type TestContext } from 'node:test';
import assert from 'node:assert/strict';

const FEED_HOST = 'feed.example.test';
const GATEWAY = 'https://gateway.example.test';
process.env.NEXT_PUBLIC_MARKET_PROXY_HOST = FEED_HOST;
process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL = GATEWAY;

const modules = (async () => ({
  feeds: await import('../src/core/feeds'),
  gateway: await import('../src/core/network/gateway'),
  markets: await import('../src/hooks/perps/useMarketsStream'),
  perpsReference: await import('../src/hooks/perps/useSidPaxMarketStream'),
  spotReference: await import('../src/hooks/spot/useSpotMarketStream'),
  explorer: await import('../src/hooks/chain/useExplorerStream'),
  spotUser: await import('../src/hooks/spot/useSpotUserStream'),
}))();

class Socket extends EventTarget {
  static OPEN = 1;
  static CONNECTING = 0;
  static instances: Socket[] = [];
  readyState = 0;
  sent: string[] = [];
  constructor(public url: string) {
    super();
    Socket.instances.push(this);
  }
  send(value: string) {
    this.sent.push(value);
  }
  close() {
    this.readyState = 3;
    this.dispatchEvent(new Event('close'));
  }
}

async function withBrowserSocket(t: TestContext, run: () => Promise<void> | void) {
  t.mock.timers.enable({ apis: ['setTimeout', 'setInterval', 'Date'], now: 1000 });
  const keys = ['window', 'document', 'WebSocket'];
  const descriptors = keys.map(key => Object.getOwnPropertyDescriptor(globalThis, key));
  Socket.instances = [];
  Object.defineProperty(globalThis, 'window', { configurable: true, value: new EventTarget() });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: Object.assign(new EventTarget(), { hidden: false }) });
  Object.defineProperty(globalThis, 'WebSocket', { configurable: true, value: Socket });
  try {
    await run();
  } finally {
    keys.forEach((key, i) => (descriptors[i] ? Object.defineProperty(globalThis, key, descriptors[i]!) : Reflect.deleteProperty(globalThis, key)));
    t.mock.timers.reset();
  }
}

test('chainflowSurfacesUseFeedOrigin', async t => {
  const { feeds, gateway, markets, perpsReference, spotReference, explorer } = await modules;
  const { buildSymbolWsUrl } = spotReference;
  const { MARKETS_WS_URL } = markets;
  const { EXPLORER_WS_URL } = explorer;
  const feedHttps = `https://${FEED_HOST}`;
  const feedWss = `wss://${FEED_HOST}`;
  assert.equal(feeds.FEED_ORIGIN, feedHttps);
  assert.equal(feeds.MARKET_DATA_URL, `${feedHttps}/api/markets`);
  const restSurfaces = [
    `${feeds.MARKET_DATA_URL}/ticker/BTC`,
    `${feeds.MARKET_DATA_URL}/open-interest`,
    `${feeds.MARKET_DATA_URL}/tickers`,
    `${feeds.FEED_ORIGIN}/api/explorer/health`,
  ];
  for (const url of restSurfaces) assert.equal(new URL(url).origin, feedHttps);
  assert.equal(buildSymbolWsUrl('SID'), `${feedWss}/ws/sid`);
  assert.equal(MARKETS_WS_URL, `${feedWss}/ws/markets`);

  await withBrowserSocket(t, async () => {
    const stop = perpsReference.subscribeKline('BTC', '1m', {});
    try {
      await Promise.resolve();
      assert.equal(Socket.instances.length, 1);
      assert.equal(Socket.instances[0]!.url, `${feedWss}/ws/btc`);
    } finally {
      stop();
    }
  });

  const gatewayHost = new URL(gateway.gatewayUrls().origin).host;
  for (const url of [...restSurfaces, buildSymbolWsUrl('SID'), MARKETS_WS_URL, EXPLORER_WS_URL]) {
    assert.notEqual(new URL(url).host, gatewayHost, url);
  }
});

test('explorerStreamFromFeedOrigin', async () => {
  const { feeds, explorer: { EXPLORER_WS_URL } } = await modules;
  assert.equal(EXPLORER_WS_URL, `wss://${FEED_HOST}/ws/explorer`);
  assert.equal(EXPLORER_WS_URL, `${feeds.FEED_ORIGIN.replace(/^https/, 'wss')}/ws/explorer`);
});

test('accountStreamsOnGateway', async t => {
  const { gateway, spotUser: { SPOT_USER_WS_URL, subscribeSpotUserStream } } = await modules;
  assert.equal(SPOT_USER_WS_URL, 'wss://gateway.example.test/v1/ws');
  assert.equal(SPOT_USER_WS_URL, gateway.gatewayUrls().spotUserWsUrl);

  await withBrowserSocket(t, async () => {
    const address = '0xABCDEF0000000000000000000000000000000001';
    const stops = (['trade', 'position', 'pnl', 'order', 'closed-leg'] as const).map(stream =>
      subscribeSpotUserStream(stream, address, () => {}),
    );
    try {
      await Promise.resolve();
      assert.equal(Socket.instances.length, 1);
      const ws = Socket.instances[0]!;
      assert.equal(ws.url, 'wss://gateway.example.test/v1/ws');
      ws.readyState = 1;
      ws.dispatchEvent(new Event('open'));
      const subscribes = ws.sent.map(frame => JSON.parse(frame)).filter(frame => frame.op === 'subscribe');
      assert.deepEqual(
        subscribes.map(frame => [frame.stream, frame.address]).sort(),
        ['closed-leg', 'order', 'pnl', 'position', 'trade'].map(stream => [stream, address.toLowerCase()]),
      );
    } finally {
      stops.forEach(stop => stop());
    }
  });
});
