// Error classification of SdkClient against a real local HTTP server.
// A server that answered (any status, any body) must surface as an HTTP
// outcome carrying that status; only a request that never got an answer is
// NETWORK_INTERRUPTED.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { SdkClient, HttpError, CapabilityUnavailableError, StreamTransportError } from '../dist/index.js';

async function serve(handler) {
  const server = createServer(handler);
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const origin = `http://127.0.0.1:${server.address().port}`;
  const stream = origin.replace('http', 'ws');
  const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false });
  return { server, client, origin };
}

function goMux(req, res) {
  // Mirrors what the Go services return for unregistered routes and failures.
  if (req.url.startsWith('/v2/unregistered') || req.url.startsWith('/v1/unregistered')) { res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8', 'X-Content-Type-Options': 'nosniff' }); res.end('404 page not found\n'); return; }
  if (req.url.startsWith('/v2/crash')) { res.writeHead(500, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('internal error\n'); return; }
  if (req.url.startsWith('/v2/no-method')) { res.writeHead(405); res.end(); return; }
  if (req.url.startsWith('/v2/moved')) { res.writeHead(301, { Location: '/v2/elsewhere' }); res.end(); return; }
  if (req.url.startsWith('/v2/html')) { res.writeHead(200, { 'Content-Type': 'text/html' }); res.end('<!doctype html><title>x</title>'); return; }
  res.writeHead(200, { 'Content-Type': 'application/json' }); res.end('"ok"');
}

test('404 text/plain from an unregistered route is CapabilityUnavailableError with status 404, not NETWORK_INTERRUPTED', async () => {
  const { server, client } = await serve(goMux);
  try {
    for (const call of [() => client.requestV2('/v2/unregistered/route', 'Error', { idempotentRead: true }), () => client.request('/v1/unregistered', 'UInt256')]) {
      const error = await call().then(() => null, e => e);
      assert.ok(error instanceof CapabilityUnavailableError, String(error));
      assert.equal(error.status, 404);
      assert.ok(!(error instanceof StreamTransportError));
    }
  } finally { server.close(); }
});

test('non-JSON 5xx and empty 405 are HttpError carrying the status', async () => {
  const { server, client } = await serve(goMux);
  try {
    const crash = await client.requestV2('/v2/crash', 'Error').then(() => null, e => e);
    assert.ok(crash instanceof HttpError); assert.equal(crash.status, 500); assert.equal(crash.code, 'TRANSPORT_ERROR');
    const method = await client.requestV2('/v2/no-method', 'Error', { method: 'POST', body: {} }).then(() => null, e => e);
    assert.ok(method instanceof HttpError); assert.equal(method.status, 405);
  } finally { server.close(); }
});

test('a redirect is not followed and is an HTTP outcome, not NETWORK_INTERRUPTED', async () => {
  const seen = [];
  const { server, client } = await serve((req, res) => { seen.push(req.url); goMux(req, res); });
  try {
    const error = await client.requestV2('/v2/moved', 'Error', { idempotentRead: true }).then(() => null, e => e);
    assert.ok(error instanceof HttpError, String(error));
    assert.equal(error.status, 301); assert.equal(error.code, 'UNEXPECTED_REDIRECT');
    assert.deepEqual(seen, ['/v2/moved']);
  } finally { server.close(); }
});

test('a 200 HTML page is HttpError NON_JSON_RESPONSE, not a SyntaxError', async () => {
  const { server, client } = await serve(goMux);
  try {
    const error = await client.requestV2('/v2/html', 'Error').then(() => null, e => e);
    assert.ok(error instanceof HttpError, String(error));
    assert.equal(error.status, 200); assert.equal(error.code, 'NON_JSON_RESPONSE');
  } finally { server.close(); }
});

test('only an unanswered request is NETWORK_INTERRUPTED', async () => {
  const { server, client } = await serve(goMux);
  server.close(); await once(server, 'close');
  const error = await client.requestV2('/v2/anything', 'Error').then(() => null, e => e);
  assert.ok(error instanceof StreamTransportError); assert.equal(error.code, 'NETWORK_INTERRUPTED');
});
