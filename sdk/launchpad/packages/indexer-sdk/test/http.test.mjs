import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { HttpClient, HttpError, httpClient, getRecentTrades } from '../dist/index.js';

test('real HTTP transport preserves decimal strings and propagates failures', async () => {
  const pool = '0x' + '1'.repeat(40);
  const amount = '999999999999999999999999999999999999';
  const server = createServer((req, res) => {
    if (req.method === 'GET' && req.url === `/stats/${pool}/transactions`) {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({transactions:[{id:'1',poolAddress:pool,sender:pool,isBuy:true,amountIn:amount,amountOut:'1',price:'0.0000001',fee:'1',blockTimestamp:123,txHash:'0x123'}],limit:50,offset:0}));
    } else if (req.url.startsWith('/bff/')) {
      res.writeHead(404); res.end('route removed');
    } else {
      res.writeHead(503); res.end('database unavailable');
    }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const client = new HttpClient();
    await assert.rejects(client.get('/x'), /Configure/);
    client.configure({baseUrl:`http://127.0.0.1:${server.address().port}/`});
    const recent = await client.recentTrades(pool);
    assert.equal(recent.pool, pool);
    assert.equal(recent.trades[0].amountIn, amount);
    httpClient.configure({baseUrl:`http://127.0.0.1:${server.address().port}/`});
    assert.deepEqual(await getRecentTrades(pool), recent);
    await assert.rejects(client.get('/unavailable'), error => error instanceof HttpError && error.status === 503 && error.body === 'database unavailable');
    await assert.rejects(client.recentTrades('invalid'), /Invalid pool/);
    await assert.rejects(client.get('//other-host'), /resource path/);
    const controller = new AbortController(); controller.abort();
    await assert.rejects(client.recentTrades(pool, {signal:controller.signal}), {name:'AbortError'});
  } finally { await new Promise(resolve => server.close(resolve)); }
});

test('recent trades accepts empty transactions and rejects invalid response contracts', async () => {
  const pool = '0x' + '2'.repeat(40);
  let body = { transactions: [], limit: 50, offset: 0 };
  const server = createServer((req, res) => {
    if (req.method !== 'GET' || req.url !== `/stats/${pool}/transactions`) {
      res.writeHead(404); res.end();
      return;
    }
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify(body));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const client = new HttpClient();
    client.configure({baseUrl:`http://127.0.0.1:${server.address().port}`});
    assert.deepEqual(await client.recentTrades(pool), { pool, trades: [] });
    const trade = {id:'1',sender:pool,isBuy:false,amountIn:'123',amountOut:'1',price:'0.5',fee:'1',blockTimestamp:123,txHash:'0x123'};
    for (const invalid of [
      null,
      { pool, trades: [trade] },
      { transactions: null },
      { transactions: {} },
      { transactions: [null] },
      ...Object.keys(trade).map(key => ({ transactions: [{ ...trade, [key]: null }] })),
      { transactions: [{ ...trade, amountIn: 123 }] },
      { transactions: [{ ...trade, blockTimestamp: 1.5 }] },
      { transactions: [{ ...trade, blockTimestamp: Number.MAX_SAFE_INTEGER + 1 }] },
    ]) {
      body = invalid;
      await assert.rejects(client.recentTrades(pool), /Invalid recent trades response/);
    }
  } finally { await new Promise(resolve => server.close(resolve)); }
});
