import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { StatsHttpClient, StatsHttpError, StatsResponseError } from '../dist/index.js';
import { poolStatsFields } from '../dist/types.js';
const pool = '0x' + 'a'.repeat(40);
const fixture = Object.fromEntries(Object.entries(poolStatsFields).map(([key, kind]) => [key, kind === 'number' ? 0 : kind === 'string | null' ? null : '900719925474099300000']));
async function serve(handler, run) {
  const server = createServer(handler);
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const c = new StatsHttpClient();
  const baseUrl = `http://127.0.0.1:${server.address().port}`;
  c.configure({baseUrl});
  try { await run(c, baseUrl); }
  finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
}
function json(response, value) { response.setHeader('Content-Type','application/json'); response.end(JSON.stringify(value)); }
test('built SDK requests exact route and preserves decimal strings', async () => {
  await serve((req,res) => { assert.equal(req.url, '/stats/'+pool); json(res,fixture); }, async c => {
    assert.equal((await c.getPoolStats(pool)).price,'900719925474099300000');
  });
});
test('batch query and missing server rows preserved', async () => {
  await serve((req,res) => {
    assert.equal(new URL(req.url,'http://localhost').searchParams.get('pools'),pool);
    json(res,{});
  }, async c => assert.deepEqual(await c.getPoolStatsBatch([pool]),{}));
});
test('HTTP error retains actual status/body', async () => {
  await serve((_req,res) => {res.writeHead(503);res.end('unavailable');}, async c => {
    await assert.rejects(c.getPoolStats(pool), e => e instanceof StatsHttpError && e.status===503 && e.body==='unavailable');
  });
});
test('malformed HTTP JSON and incompatible DTO reject', async () => {
  for (const body of ['{',JSON.stringify({...fixture,price:123})]) {
    await serve((_req,res) => res.end(body), async c => assert.rejects(c.getPoolStats(pool),StatsResponseError));
  }
});
test('actual stalled HTTP response obeys request deadline', async () => {
  let received = false;
  await serve((_req,res) => {received=true;res.writeHead(200);res.write('{');}, async (c,baseUrl) => {
    c.configure({baseUrl,timeoutMs:100});
    await assert.rejects(c.getPoolStats(pool), e => e instanceof DOMException && ['TimeoutError','AbortError'].includes(e.name));
    assert.equal(received,true);
  });
});
test('caller cancellation aborts an active HTTP request', async () => {
  const controller = new AbortController();
  await serve((_req,res) => {res.writeHead(200);res.write('{');controller.abort();}, async c => {
    await assert.rejects(c.getPoolStats(pool,{signal:controller.signal}),e => e instanceof DOMException && e.name==='AbortError');
  });
});
test('invalid inputs do not reach HTTP service', async () => {
  let requests=0;
  await serve((_req,res) => {requests++;res.end('{}');}, async (c,baseUrl) => {
    assert.throws(() => c.configure({baseUrl,timeoutMs:0}));
    await assert.rejects(c.getPoolStats('bad'),TypeError);
    await assert.rejects(c.getPoolStatsBatch([]),TypeError);
    assert.equal(requests,0);
  });
});
