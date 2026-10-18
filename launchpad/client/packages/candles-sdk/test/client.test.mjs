import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { CandlesClient, CandlesHttpError, UdfResponseError, parseHistory, parseConfig, parseSymbol } from '../dist/index.js';

const config = {
  supports_search: true, supports_group_request: false, supports_marks: false,
  supports_timescale_marks: false, supports_time: true,
  exchanges: [{ value: 'sidiora', name: 'Sidiora', desc: 'Sidiora Launchpad' }],
  symbols_types: [{ name: 'token', value: 'token' }],
  supported_resolutions: ['1', '5', '15', '60', '240', '1D', '1W'],
  currency_codes: [{ id: 'USDL', code: 'USDL', description: 'USDL Stablecoin' }],
};
const symbol = {
  name: 'SID', full_name: 'SID/USDL', description: 'Token SID', type: 'token', session: '24x7',
  exchange: 'sidiora', listed_exchange: 'sidiora', timezone: 'Etc/UTC',
  has_intraday: true, has_weekly_and_monthly: true,
  supported_resolutions: ['1', '5', '15', '60', '240', '1D', '1W'],
  pricescale: 100000000, ticker: 'SID', currency_code: 'USDL', has_empty_bars: false,
};
const bars = { s: 'ok', t: [60, 120], o: [1, 2], h: [3, 4], l: [0.5, 1], c: [2, 3], v: [100, 200] };

async function fixture(handler, run) {
  const server = createServer(handler);
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  assert.notEqual(address, null);
  assert.notEqual(typeof address, 'string');
  try { await run(`http://127.0.0.1:${address.port}`); }
  finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
}
function json(response, value, status = 200) {
  response.writeHead(status, { 'Content-Type': 'application/json' });
  response.end(JSON.stringify(value));
}

test('configured UDF prefix is used exactly once for all current routes', async () => {
  const seen = [];
  await fixture((request, response) => {
    const url = new URL(request.url, 'http://localhost');
    seen.push(url);
    assert.equal(request.headers.accept, 'application/json');
    assert.equal(request.headers.cookie, undefined);
    if (url.pathname === '/udf/config') json(response, config);
    else if (url.pathname === '/udf/symbols') json(response, symbol);
    else if (url.pathname === '/udf/history') json(response, bars);
    else if (url.pathname === '/udf/time') json(response, 1800000000);
    else json(response, { error: 'unregistered fixture route' }, 404);
  }, async base => {
    const client = new CandlesClient();
    client.configure({ baseUrl: `${base}/udf/` });
    assert.deepEqual(await client.getConfig(), config);
    assert.deepEqual(await client.getSymbol('SID & USDL'), symbol);
    assert.deepEqual(await client.getHistory({ symbol: 'SID', resolution: '1D', to: 1800000000, countback: 2 }), bars);
    assert.equal(await client.getTime(), 1800000000);
  });
  assert.deepEqual(seen.map(url => url.pathname), ['/udf/config', '/udf/symbols', '/udf/history', '/udf/time']);
  assert.equal(seen[1].searchParams.get('symbol'), 'SID & USDL');
  assert.equal(seen[2].searchParams.get('resolution'), '1D');
  assert.equal(seen[2].searchParams.get('countback'), '2');
  assert.equal(seen[2].searchParams.has('from'), false);
});

test('HTTP failures retain status and never become no_data or zero', async () => {
  await fixture((_request, response) => json(response, { error: 'history query failed' }, 503), async base => {
    const client = new CandlesClient();
    client.configure({ baseUrl: base });
    await assert.rejects(client.getHistory({ symbol: 'SID', resolution: '1', from: 0, to: 120 }),
      error => error instanceof CandlesHttpError && error.status === 503);
  });
});

test('response schema failures propagate through a real HTTP response', async () => {
  await fixture((_request, response) => json(response, { ...bars, c: [2] }), async base => {
    const client = new CandlesClient();
    client.configure({ baseUrl: base });
    await assert.rejects(client.getHistory({ symbol: 'SID', resolution: '1', from: 0, to: 120 }), UdfResponseError);
  });
});

test('deadline and caller abort reject rather than invent chart values', async () => {
  await fixture(() => {}, async base => {
    const client = new CandlesClient();
    client.configure({ baseUrl: base, timeoutMs: 100 });
    await assert.rejects(client.getTime(), error => error.name === 'TimeoutError');
    const controller = new AbortController();
    controller.abort();
    await assert.rejects(client.getTime({ signal: controller.signal }), error => error.name === 'AbortError');
  });
});

test('history input rejects unsupported resolutions, unbounded countback and unsafe time', async () => {
  const client = new CandlesClient();
  await assert.rejects(client.getTime(), /Configure candles-sdk/);
  for (const input of [
    { symbol: 'SID', resolution: '1M', from: 0, to: 120 },
    { symbol: 'SID', resolution: '1', countback: 2001, to: 120 },
    { symbol: 'SID', resolution: '1', from: 121, to: 120 },
    { symbol: '', resolution: '1', from: 0, to: 120 },
    { symbol: 'SID', resolution: '1', from: 0, to: Number.MAX_SAFE_INTEGER + 1 },
    { symbol: 'SID', resolution: '1', to: 120 },
  ]) await assert.rejects(client.getHistory(input));
});

test('history parser preserves no_data and rejects corrupt bar alignment or range', () => {
  assert.deepEqual(parseHistory({ s: 'no_data' }), { s: 'no_data' });
  assert.deepEqual(parseHistory({ s: 'no_data', nextTime: 60 }), { s: 'no_data', nextTime: 60 });
  for (const value of [
    { ...bars, t: [120, 60] }, { ...bars, t: [60, 60] },
    { ...bars, h: [0.1, 4] }, { ...bars, l: [4, 1] },
    { ...bars, v: [-1, 1] }, { ...bars, c: [NaN, 3] },
    { ...bars, t: [60.5, 120] }, { ...bars, o: ['1', 2] },
    { s: 'error', errmsg: 'unavailable' }, null,
  ]) assert.throws(() => parseHistory(value), UdfResponseError);
});

test('configuration and symbol schemas accept current Go wire shapes, reject unsafe shapes', () => {
  assert.deepEqual(parseConfig(config), config);
  assert.deepEqual(parseSymbol(symbol), symbol);
  assert.throws(() => parseConfig({ ...config, supports_time: 'true' }), UdfResponseError);
  assert.throws(() => parseSymbol({ ...symbol, pricescale: 0 }), UdfResponseError);
  assert.throws(() => parseSymbol({ ...symbol, supported_resolutions: ['1M'] }), UdfResponseError);
});

test('base configuration rejects non-HTTP targets and URL credential/query ambiguity', () => {
  const client = new CandlesClient();
  for (const baseUrl of ['', '//remote/udf', 'javascript:alert(1)', 'https://user:password@host/udf', '/udf?x=1', 'https://host/udf#fragment']) {
    assert.throws(() => client.configure({ baseUrl }));
  }
  assert.throws(() => client.configure({ baseUrl: '/udf', timeoutMs: 99 }), RangeError);
  client.configure({ baseUrl: '/udf', timeoutMs: 1000 });
});
