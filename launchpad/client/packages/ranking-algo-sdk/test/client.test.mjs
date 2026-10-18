import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import test from 'node:test';
import { RankingHttpClient, RankingHttpError, httpClient } from '../src/index.ts';

test('real HTTP round trip preserves decimal strings, pagination, headers, and errors', async () => {
  const requests = [];
  const server = createServer((req, res) => {
    requests.push({ url: req.url, authorization: req.headers.authorization });
    if (req.url.startsWith('/rankings/unusual')) { res.writeHead(503); res.end('unavailable'); return; }
    if (req.url.startsWith('/rankings/movers')) { res.end('{"category":"movers","items":"invalid"}'); return; }
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ category: 'trending', items: [{poolAddress:'0xabc',score: 2.5,rank:3,
      stats:{price:'0.000000000000000001',holderCount:2}}],total:10,limit:1,offset:2 }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  try {
    const client = new RankingHttpClient();
    client.configure({baseUrl:`http://127.0.0.1:${server.address().port}/`,headers:{Authorization:'Bearer local-test'}});
    const result = await client.getRankings('trending',{limit:1,offset:2});
    assert.equal(result.items[0].stats.price,'0.000000000000000001');
    assert.deepEqual(requests[0],{url:'/rankings/trending?limit=1&offset=2',authorization:'Bearer local-test'});
    await assert.rejects(client.getRankings('unusual'), e => e instanceof RankingHttpError && e.status === 503 && e.body === 'unavailable');
    await assert.rejects(client.getRankings('movers'), /Invalid rankings response/);
    await assert.rejects(client.getRankings('trending',{offset:-1}),RangeError);
    await assert.rejects(client.getRankings('unknown'),TypeError);
    const controller = new AbortController(); controller.abort();
    await assert.rejects(client.getRankings('trending',{signal:controller.signal}),{name:'AbortError'});
  } finally { server.close(); await once(server,'close'); }
});

test('configuration fails explicitly and accepts query-provider configure interface',async () => {
  await assert.rejects(new RankingHttpClient().getRankings('trending'),/not configured/);
  assert.throws(() => httpClient.configure({baseUrl:'file:///tmp'}),TypeError);
  httpClient.configure({baseUrl:'https://example.test'});
});
