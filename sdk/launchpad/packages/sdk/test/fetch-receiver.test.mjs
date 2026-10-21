// Browser receiver semantics for the default fetcher.
// In a browser `fetch` is a WebIDL operation on Window: calling it with any
// receiver other than the global (or undefined) throws
// "TypeError: Failed to execute 'fetch' on 'Window': Illegal invocation".
// Node's fetch has no such check, so this test installs a real native-fetch
// wrapper on globalThis that enforces the browser rule, then drives SdkClient
// constructed WITHOUT a fetcher against a real local HTTP server.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { SdkClient, StreamTransportError } from '../dist/index.js';

function installBrowserFetch() {
  const original = Object.getOwnPropertyDescriptor(globalThis, 'fetch');
  const native = globalThis.fetch;
  const receivers = [];
  function fetch(input, init) {
    receivers.push(this);
    if (this !== undefined && this !== globalThis) throw new TypeError("Failed to execute 'fetch' on 'Window': Illegal invocation");
    return native(input, init);
  }
  Object.defineProperty(globalThis, 'fetch', { value: fetch, writable: true, configurable: true, enumerable: original?.enumerable ?? false });
  return { receivers, restore: () => Object.defineProperty(globalThis, 'fetch', original) };
}

async function serve() {
  const seen = [];
  const server = createServer((req, res) => { seen.push(req.url); res.writeHead(200, { 'Content-Type': 'application/json' }); res.end('"42"'); });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const origin = `http://127.0.0.1:${server.address().port}`;
  const stream = origin.replace('http', 'ws');
  return { server, seen, origin, stream };
}

test('receiver-checking fetch rejects a detached method call (sanity check of the harness)', () => {
  const browser = installBrowserFetch();
  try {
    const holder = { fetcher: globalThis.fetch };
    assert.throws(() => holder.fetcher('http://127.0.0.1:1/'), /Illegal invocation/);
  } finally { browser.restore(); }
});

test('SdkClient with the default fetcher reaches the network under browser receiver rules', async () => {
  const { server, seen, origin, stream } = await serve();
  const browser = installBrowserFetch();
  try {
    // Fetch is installed BEFORE construction, exactly as in a browser page.
    const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false });
    const value = await client.request('/v1/value', 'UInt256').catch(e => e);
    assert.ok(!(value instanceof StreamTransportError), `request failed before the network: ${value?.code ?? value}`);
    assert.equal(value, '42');
    assert.deepEqual(seen, ['/v1/value']);
    assert.ok(browser.receivers.length >= 1);
    for (const r of browser.receivers) assert.ok(r === undefined || r === globalThis);
  } finally { browser.restore(); server.close(); }
});

test('SdkClient picks up a fetch installed after construction (no captured global)', async () => {
  const { server, seen, origin, stream } = await serve();
  const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false });
  const browser = installBrowserFetch();
  try {
    assert.equal(await client.request('/v1/late', 'UInt256'), '42');
    assert.deepEqual(seen, ['/v1/late']);
    assert.equal(browser.receivers.length, 1);
  } finally { browser.restore(); server.close(); }
});

test('an injected fetcher is still called detached, so a native fetch passed by callers also works', async () => {
  const { server, seen, origin, stream } = await serve();
  const browser = installBrowserFetch();
  try {
    const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: stream, approvedStreamOrigins: [stream], production: false }, globalThis.fetch);
    assert.equal(await client.request('/v1/injected', 'UInt256'), '42');
    assert.deepEqual(seen, ['/v1/injected']);
  } finally { browser.restore(); server.close(); }
});
