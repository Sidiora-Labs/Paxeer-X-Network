// Client-side timeout floor for generated operations, against a real local HTTP server.
// IF_PLATFORM carries a 1000 ms contract budget; a browser answer slower than that but
// well inside the floor must arrive as the server's answer, not as "Request timed out".
import { test } from 'node:test';
import { operationDefinitions } from '../dist/operations.gen.js';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { SdkClient, Operations, CapabilityUnavailableError, operationTimeoutMs, OPERATION_READ_TIMEOUT_FLOOR_MS, OPERATION_WRITE_TIMEOUT_FLOOR_MS } from '../dist/index.js';

async function serve(delayMs) {
  const seen = [];
  const server = createServer((req, res) => {
    seen.push(req.url);
    const timer = setTimeout(() => { res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('404 page not found\n'); }, delayMs);
    res.on('close', () => clearTimeout(timer));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const origin = `http://127.0.0.1:${server.address().port}`;
  const stream = origin.replace('http', 'ws');
  const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false });
  return { server, operations: new Operations(client), seen };
}

test('floors apply over the generated budget, larger budgets and explicit overrides win', () => {
  assert.equal(operationDefinitions.IF_PLATFORM.timeoutMs, 1000);
  assert.equal(operationTimeoutMs(operationDefinitions.IF_PLATFORM), OPERATION_READ_TIMEOUT_FLOOR_MS);
  assert.equal(operationTimeoutMs({ method: 'POST', timeoutMs: 2500 }), OPERATION_WRITE_TIMEOUT_FLOOR_MS);
  assert.equal(operationTimeoutMs({ method: 'GET', timeoutMs: 20000 }), 20000);
  assert.equal(operationTimeoutMs({ method: 'GET', timeoutMs: 1000 }, 300), 300);
  assert.ok(OPERATION_READ_TIMEOUT_FLOOR_MS >= 8000 && OPERATION_WRITE_TIMEOUT_FLOOR_MS >= 10000);
});

test('a read answered after its 1000 ms contract budget surfaces the server answer, not a timeout', async () => {
  const { server, operations, seen } = await serve(1600);
  try {
    const started = Date.now();
    const error = await operations.call('IF_PLATFORM', { query: { window: '24h' } }).then(() => null, e => e);
    assert.ok(error instanceof CapabilityUnavailableError, String(error));
    assert.equal(error.status, 404);
    assert.ok(Date.now() - started >= 1500);
    assert.deepEqual(seen, ['/v2/platform/metrics?window=24h']);
  } finally { server.close(); }
});

test('an explicit timeoutMs override still times the request out', async () => {
  const { server, operations } = await serve(1600);
  try {
    const started = Date.now();
    const error = await operations.call('IF_PLATFORM', { query: { window: '7d' } }, { timeoutMs: 200 }).then(() => null, e => e);
    assert.equal(error?.name, 'TimeoutError', String(error));
    assert.ok(Date.now() - started < 1500);
  } finally { server.close(); }
});

test('the caller AbortSignal cancels before the floor', async () => {
  const { server, operations } = await serve(5000);
  try {
    const controller = new AbortController();
    const reason = new DOMException('left the page', 'AbortError');
    setTimeout(() => controller.abort(reason), 150);
    const started = Date.now();
    const error = await operations.call('IF_PLATFORM', { query: { window: '30d' } }, { signal: controller.signal }).then(() => null, e => e);
    assert.equal(error, reason);
    assert.ok(Date.now() - started < 2000);
  } finally { server.closeAllConnections(); server.close(); }
});
