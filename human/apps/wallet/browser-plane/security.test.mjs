import assert from 'node:assert/strict';
import test from 'node:test';
import { isPrivateAddress, normalizePublicHttpsUrl } from './security.mjs';

test('private IPv4 and IPv6 ranges are rejected', () => {
  for (const address of [
    '0.0.0.0',
    '10.1.2.3',
    '100.64.1.1',
    '127.0.0.1',
    '169.254.1.2',
    '172.16.0.1',
    '192.168.1.1',
    '198.18.0.1',
    '224.0.0.1',
    '::',
    '::1',
    '::ffff:127.0.0.1',
    '::ffff:7f00:1',
    'fc00::1',
    'fe80::1',
  ]) {
    assert.equal(isPrivateAddress(address), true, address);
  }
  assert.equal(isPrivateAddress('1.1.1.1'), false);
  assert.equal(isPrivateAddress('2606:4700:4700::1111'), false);
});

test('browser destinations require credential-free public HTTPS URLs', async () => {
  await assert.rejects(() => normalizePublicHttpsUrl('http://example.com'), /HTTPS/);
  await assert.rejects(
    () => normalizePublicHttpsUrl('https://user:pass@example.com'),
    /credentials/,
  );
  await assert.rejects(
    () => normalizePublicHttpsUrl('https://127.0.0.1'),
    /Private network/,
  );
  await assert.rejects(
    () => normalizePublicHttpsUrl('https://localhost'),
    /Private network/,
  );
  const publicUrl = await normalizePublicHttpsUrl('https://example.com/path?q=1#secret');
  assert.equal(publicUrl.toString(), 'https://example.com/path?q=1');
});
