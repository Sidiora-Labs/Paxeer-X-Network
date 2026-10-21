import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { HttpClient, HttpError } from '../dist/index.js';

test('real HTTP transport preserves decimal strings and propagates failures', async () => {
  const pool = '0x' + '1'.repeat(40);
  const amount = '999999999999999999999999999999999999';
  const server = createServer((req, res) => {
    if (req.url === `/bff/token/${pool}/trades`) {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({pool, trades:[{id:'1',sender:pool,isBuy:true,amountIn:amount,amountOut:'1',price:'0.0000001',fee:'1',blockTimestamp:123,txHash:'0x123'}]}));
    } else {
      res.writeHead(503); res.end('database unavailable');
    }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const client = new HttpClient();
    await assert.rejects(client.get('/x'), /Configure/);
    client.configure({baseUrl:`http://127.0.0.1:${server.address().port}/`});
    assert.equal((await client.recentTrades(pool)).trades[0].amountIn, amount);
    await assert.rejects(client.get('/unavailable'), error => error instanceof HttpError && error.status === 503 && error.body === 'database unavailable');
    await assert.rejects(client.recentTrades('invalid'), /Invalid pool/);
    await assert.rejects(client.get('//other-host'), /resource path/);
    const controller = new AbortController(); controller.abort();
    await assert.rejects(client.recentTrades(pool, {signal:controller.signal}), {name:'AbortError'});
  } finally { await new Promise(resolve => server.close(resolve)); }
});
