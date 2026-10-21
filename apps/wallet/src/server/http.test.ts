import { afterEach, describe, expect, it } from 'vitest';
import { NextRequest } from 'next/server';
import {
  HttpBoundaryError,
  readBoundedJson,
  readBoundedUpstream,
  requirePushAdmin,
  trustedClientIdentity,
} from './http';

const originalAdminKey = process.env.PUSH_ADMIN_KEY;
const originalProxySecret = process.env.TRUSTED_PROXY_SECRET;

afterEach(() => {
  if (originalAdminKey === undefined) delete process.env.PUSH_ADMIN_KEY;
  else process.env.PUSH_ADMIN_KEY = originalAdminKey;
  if (originalProxySecret === undefined) delete process.env.TRUSTED_PROXY_SECRET;
  else process.env.TRUSTED_PROXY_SECRET = originalProxySecret;
});

describe('server HTTP boundary', () => {
  it('enforces content type and actual JSON body bounds', async () => {
    const wrongType = new NextRequest('https://wallet.example/api', {
      method: 'POST',
      headers: { 'Content-Type': 'text/plain' },
      body: '{}',
    });
    await expect(readBoundedJson(wrongType, 100)).rejects.toMatchObject({
      status: 415,
      code: 'CONTENT_TYPE_INVALID',
    });

    const oversized = new NextRequest('https://wallet.example/api', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ value: 'x'.repeat(200) }),
    });
    await expect(readBoundedJson(oversized, 100)).rejects.toMatchObject({
      status: 413,
      code: 'BODY_TOO_LARGE',
    });
  });

  it('stops oversized upstream streams without relying on content-length', async () => {
    const response = new Response('x'.repeat(200), {
      headers: { 'Content-Type': 'application/json' },
    });
    await expect(
      readBoundedUpstream(response, {
        maxBytes: 100,
        allowedContentTypes: new Set(['application/json']),
      }),
    ).rejects.toMatchObject({
      status: 502,
      code: 'UPSTREAM_TOO_LARGE',
    });
  });

  it('rejects ambiguous admin credentials and accepts one constant-time source', () => {
    process.env.PUSH_ADMIN_KEY = 'a'.repeat(32);
    const valid = new NextRequest('https://wallet.example/api', {
      headers: { Authorization: `Bearer ${'a'.repeat(32)}` },
    });
    expect(() => requirePushAdmin(valid)).not.toThrow();

    const ambiguous = new NextRequest('https://wallet.example/api', {
      headers: {
        Authorization: `Bearer ${'a'.repeat(32)}`,
        'X-Push-Admin-Key': 'a'.repeat(32),
      },
    });
    expect(() => requirePushAdmin(ambiguous)).toThrow(HttpBoundaryError);
  });

  it('ignores spoofable client identity without authenticated proxy context', () => {
    process.env.TRUSTED_PROXY_SECRET = 's'.repeat(32);
    const spoofed = new NextRequest('https://wallet.example/api', {
      headers: {
        'X-Forwarded-For': '203.0.113.5',
        'X-Paxport-Client-Id': 'client-1234',
      },
    });
    expect(trustedClientIdentity(spoofed)).toBe('anonymous');

    const trusted = new NextRequest('https://wallet.example/api', {
      headers: {
        'X-Paxport-Client-Id': 'client-1234',
        'X-Paxport-Proxy-Secret': 's'.repeat(32),
      },
    });
    expect(trustedClientIdentity(trusted)).toBe('proxy:client-1234');
  });
});
