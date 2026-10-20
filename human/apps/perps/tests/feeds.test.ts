import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { NextApiRequest, NextApiResponse } from 'next';
import handler from '../src/pages/api/candle/[...path]';
import { getPaxeerRpcUrls } from '../src/core/rpc';
import { candleFeedUrl, FEED_SYMBOLS } from '../src/core/feeds';

test('all chart reads target gateway directly, never the app-domain candle proxy', () => {
  for (const symbol of FEED_SYMBOLS) {
    const url = new URL(candleFeedUrl('history', `PERP_${symbol}_USDC`, new URLSearchParams({ resolution: '1', from: '1', to: '2' })));
    assert.equal(url.origin, 'https://data.feed.chainflowtrading.com');
    assert.equal(url.pathname, `/api/${symbol.toLowerCase()}/history`);
    assert.equal(url.searchParams.get('symbol'), symbol);
  }
  assert.equal(candleFeedUrl('config'), 'https://data.feed.chainflowtrading.com/api/sid/config');
  assert.throws(() => candleFeedUrl('symbols', '../admin'));
});

test('RPC defaults include all 16 owned EVM nodes', () => {
  assert.equal(getPaxeerRpcUrls().length, 16);
  assert.equal(getPaxeerRpcUrls()[15], 'https://api16.mainnet-beta.paxeer.network');
});
test('candle relay normalizes symbols and uses keyless gateway origin', async () => {
  const oldFetch = global.fetch;
  let requested = '';
  let sentHeaders: Record<string, string> = {};
  global.fetch = (async (url, options) => {
    requested = String(url); sentHeaders = options?.headers as Record<string, string>;
    return new Response(JSON.stringify({ s: 'ok', t: [1], c: [10] }), { headers: { 'content-type': 'application/json' } });
  }) as typeof fetch;
  let status = 0; let data: unknown;
  const res = { setHeader() {}, status(code: number) { status = code; return this; }, json(value: unknown) { data = value; }, send(value: unknown) { data = value; } } as unknown as NextApiResponse;
  const req = { method: 'GET', headers: {}, socket: { remoteAddress: '127.0.0.1' }, query: { path: ['history'], symbol: 'PERP_PAX_USDC', resolution: '1', from: '1', to: '2' } } as unknown as NextApiRequest;
  const oldOrigin = process.env.MARKET_PROXY_ORIGIN;
  process.env.MARKET_PROXY_ORIGIN = 'https://perps.test.example';
  try {
    await handler(req, res);
    assert.equal(status, 200); assert.ok(data);
    assert.ok(requested.startsWith('https://data.feed.chainflowtrading.com/api/pax/history?'));
    assert.equal(new URL(requested).searchParams.get('symbol'), 'PAX');
    assert.equal(sentHeaders.Origin, 'https://perps.test.example');
    assert.equal(sentHeaders['X-API-Key'], undefined);
    req.query = { path: ['config'] }; await handler(req, res);
    assert.equal(requested, 'https://data.feed.chainflowtrading.com/api/sid/config');
    req.query = { path: ['search'], query: '' }; await handler(req, res);
    assert.equal((data as unknown[]).length, 18);
  } finally {
    global.fetch = oldFetch;
    if (oldOrigin === undefined) delete process.env.MARKET_PROXY_ORIGIN; else process.env.MARKET_PROXY_ORIGIN = oldOrigin;
  }
});

test('candle relay answers 503 without calling upstream when MARKET_PROXY_ORIGIN is unset or invalid', async () => {
  const oldFetch = global.fetch;
  const oldOrigin = process.env.MARKET_PROXY_ORIGIN;
  let calls = 0;
  global.fetch = (async () => { calls += 1; return new Response('{}'); }) as typeof fetch;
  let status = 0; let data: unknown;
  const res = { setHeader() {}, status(code: number) { status = code; return this; }, json(value: unknown) { data = value; }, send(value: unknown) { data = value; } } as unknown as NextApiResponse;
  const req = { method: 'GET', headers: {}, socket: { remoteAddress: '127.0.0.2' }, query: { path: ['history'], symbol: 'PERP_PAX_USDC', resolution: '1', from: '1', to: '2' } } as unknown as NextApiRequest;
  try {
    for (const value of [undefined, '', 'https://perps.test.example/', 'https://perps.test.example/path', 'ftp://perps.test.example', 'not a url']) {
      if (value === undefined) delete process.env.MARKET_PROXY_ORIGIN; else process.env.MARKET_PROXY_ORIGIN = value;
      status = 0; data = undefined;
      await handler(req, res);
      assert.equal(status, 503, `origin ${String(value)}`);
      assert.deepEqual(data, { error: 'MARKET_PROXY_ORIGIN is not configured' });
    }
    assert.equal(calls, 0);
  } finally {
    global.fetch = oldFetch;
    if (oldOrigin === undefined) delete process.env.MARKET_PROXY_ORIGIN; else process.env.MARKET_PROXY_ORIGIN = oldOrigin;
  }
});
