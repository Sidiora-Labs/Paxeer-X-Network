import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { createMetadataClient, MetadataHttpError, MetadataUploadError, MetadataProtocolError } from '../src/index.ts';

const token = '0x' + 'a'.repeat(40);
const metadata = { token_address: token, pool_address: null, name: null, symbol: null, decimals: 6, total_supply: '1000000000', creator: null, description: null, socials: { website: null, twitter: null, telegram: null, discord: null }, tags: [], images: { logo: null, banner: null }, created_at: null, updated_at: null };

test('configuration and request validation fail explicitly', async () => {
  const client = createMetadataClient();
  await assert.rejects(client.getMetadata(token), /not configured/);
  assert.throws(() => client.configure({ baseUrl: 'file:///metadata' }), TypeError);
  assert.throws(() => client.configure({ baseUrl: 'https://user:password@example.com' }), TypeError);
  assert.throws(() => client.getMetadata('bad'), TypeError);
  assert.throws(() => client.getMetadataBatch(Array.from({ length: 101 }, (_, i) => '0x' + i.toString(16).padStart(40, '0'))), RangeError);
  await assert.rejects(client.uploadMetadata(token, { wallet: token, signature: '', message: '' }), /required/);
});

test('real HTTP transport preserves routes, multipart fields and failure responses', async () => {
  const requests: { url: string; method: string; contentType: string; body: string }[] = [];
  const server = createServer(async (req, res) => {
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(Buffer.from(chunk));
    requests.push({ url: req.url ?? '', method: req.method ?? '', contentType: req.headers['content-type'] ?? '', body: Buffer.concat(chunks).toString() });
    res.setHeader('Content-Type', 'application/json');
    if (req.url?.includes('batch')) {
      res.statusCode = 503;
      res.end('{"error":"unavailable"}');
    } else if (req.method === 'POST') {
      res.end('{"success":false,"metadata_updated":true,"logo_url":null,"banner_url":null,"errors":["logo rejected"]}');
    } else {
      res.end(JSON.stringify(metadata));
    }
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  try {
    const endpoint = server.address();
    assert.ok(endpoint && typeof endpoint !== 'string');
    const client = createMetadataClient({ baseUrl: `http://127.0.0.1:${endpoint.port}/` });
    assert.equal((await client.getMetadata(token)).token_address, token);
    await assert.rejects(client.getMetadataBatch([token, token]), (error: unknown) => error instanceof MetadataHttpError && error.status === 503 && error.body.includes('unavailable'));
    await assert.rejects(client.uploadMetadata(token, { wallet: token, signature: 'signed', message: 'authorized', metadata: { name: 'Token' }, logo: new Blob(['image'], { type: 'image/png' }) }), (error: unknown) => error instanceof MetadataUploadError && error.response.metadata_updated);
    assert.equal(requests[0]?.url, `/metadata/${token}`);
    assert.equal(requests[1]?.url, `/metadata/batch?addresses=${token}`);
    assert.equal(requests[2]?.method, 'POST');
    assert.match(requests[2]?.contentType ?? '', /^multipart\/form-data; boundary=/);
    for (const field of ['wallet', 'signature', 'message', 'metadata', 'logo']) assert.match(requests[2]?.body ?? '', new RegExp(`name="${field}"`));
    assert.match(requests[2]?.body ?? '', /authorized/);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
});

test('real HTTP 200 malformed read, batch and upload DTOs fail closed', async () => {
  let payload: unknown;
  const server = createServer(async (req, res) => {
    for await (const _chunk of req) { /* Drain request before responding. */ }
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify(payload));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  try {
    const endpoint = server.address();
    assert.ok(endpoint && typeof endpoint !== 'string');
    const client = createMetadataClient({ baseUrl: `http://127.0.0.1:${endpoint.port}` });
    for (const invalid of [{}, null, [], { ...metadata, decimals: '6' }, { ...metadata, tags: [1] }, { ...metadata, images: { logo: null } }, { ...metadata, created_at: 1.5 }, { ...metadata, name: undefined }]) {
      payload = invalid;
      await assert.rejects(client.getMetadata(token), MetadataProtocolError);
      payload = { [token]: invalid };
      await assert.rejects(client.getMetadataBatch([token]), MetadataProtocolError);
    }
    payload = {};
    await assert.rejects(client.getMetadataBatch([token]), MetadataProtocolError);
    const upload = { success: true, metadata_updated: true, logo_url: null, banner_url: null };
    for (const invalid of [{}, { ...upload, success: 'false' }, { ...upload, metadata_updated: 1 }, { ...upload, logo_url: false }, { ...upload, banner_url: undefined }, { ...upload, errors: [3] }, { ...upload, errors: null }]) {
      payload = invalid;
      await assert.rejects(client.uploadMetadata(token, { wallet: token, signature: 'signed', message: 'authorized' }), MetadataProtocolError);
    }
    payload = { [token]: metadata };
    assert.deepEqual(await client.getMetadataBatch([token]), payload);
    payload = upload;
    assert.deepEqual(await client.uploadMetadata(token, { wallet: token, signature: 'signed', message: 'authorized' }), upload);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
});
