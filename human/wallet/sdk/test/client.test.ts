import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { PaxeerWallet, PaxeerWalletError } from '../src/index.js';

type Seen = { method: string; url: string; authorization: string | undefined };

const seen: Seen[] = [];
let server: Server;
let apiUrl: string;

const tiers = {
  tiers: [
    {
      tier_id: 'starter_25k',
      whitelist: [{ contract: '0x1111111111111111111111111111111111111111', selector: '0x095ea7b3' }],
    },
  ],
};

function handle(req: IncomingMessage, res: ServerResponse): void {
  seen.push({ method: req.method ?? '', url: req.url ?? '', authorization: req.headers.authorization });
  if (req.method === 'GET' && req.url === '/v1/funded/tiers') {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify(tiers));
    return;
  }
  if (req.url === '/broken/v1/funded/tiers') {
    res.writeHead(502, { 'content-type': 'text/plain' });
    res.end('upstream unavailable');
    return;
  }
  res.writeHead(403, { 'content-type': 'application/json' });
  res.end(JSON.stringify({ error: 'CONTRACT_NOT_WHITELISTED', message: 'contract is not whitelisted' }));
}

beforeAll(async () => {
  server = createServer(handle);
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  apiUrl = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

afterAll(async () => {
  await new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
});

function wallet(base: string): PaxeerWallet {
  return new PaxeerWallet({
    apiUrl: base,
    supabaseUrl: 'https://project.supabase.invalid',
    supabaseAnonKey: 'synthetic-publishable-key',
  });
}

describe('PaxeerWallet', () => {
  it('rejects a configuration missing any required field', () => {
    const base = { apiUrl: 'https://wallet.invalid', supabaseUrl: 'https://project.supabase.invalid', supabaseAnonKey: 'k' };
    expect(() => new PaxeerWallet({ ...base, apiUrl: '' })).toThrow('apiUrl required');
    expect(() => new PaxeerWallet({ ...base, supabaseUrl: '' })).toThrow('supabaseUrl required');
    expect(() => new PaxeerWallet({ ...base, supabaseAnonKey: '' })).toThrow('supabaseAnonKey required');
  });

  it('reads a public route without a bearer header and strips a trailing slash from the base', async () => {
    const result = await wallet(`${apiUrl}/`).listFundedTiers();
    expect(result).toEqual(tiers);
    const last = seen[seen.length - 1];
    expect(last).toEqual({ method: 'GET', url: '/v1/funded/tiers', authorization: undefined });
  });

  it('refuses an authenticated call without a session before any request is sent', async () => {
    const before = seen.length;
    const err = await wallet(apiUrl).signMessage('hello').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(PaxeerWalletError);
    expect(err).toMatchObject({ code: 'NO_SESSION', status: 401, message: 'not_authenticated' });
    expect(seen.length).toBe(before);
  });

  it('maps a structured error body to the error code, message, status and detail', async () => {
    const err = await wallet(`${apiUrl}/denied`).listFundedTiers().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(PaxeerWalletError);
    expect(err).toMatchObject({
      code: 'CONTRACT_NOT_WHITELISTED',
      message: 'contract is not whitelisted',
      status: 403,
      detail: { error: 'CONTRACT_NOT_WHITELISTED', message: 'contract is not whitelisted' },
    });
  });

  it('maps a non-JSON error body to an HTTP status code', async () => {
    const err = await wallet(`${apiUrl}/broken`).listFundedTiers().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(PaxeerWalletError);
    expect(err).toMatchObject({ code: 'HTTP_502', message: 'request failed: 502', status: 502, detail: null });
  });
});
