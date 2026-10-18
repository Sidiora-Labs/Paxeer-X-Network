/**
 * The web sign-in flow against a local HTTP origin that answers the media/gateway
 * production identity routes (media/gateway/internal/auth/identity_transport.go):
 * POST /v2/auth/challenges, POST /v2/auth/sessions (sets the __Host-kindle_*
 * cookies and returns X-CSRF-Token), POST /v2/auth/sessions:refresh (refresh
 * cookie + X-CSRF-Token, rotates every secret) and DELETE /v2/auth/sessions/{id}.
 * The page origin IS the fixture origin, the way next.config.ts rewrites
 * /v2/auth/* to the gateway on the web origin.
 */
import { createServer, type IncomingMessage } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import { createHash, randomBytes } from 'node:crypto';
import { afterAll, describe, expect, it } from 'vitest';
import { privateKeyToAccount } from 'viem/accounts';
import { verifyMessage } from 'viem';

const owner = privateKeyToAccount('0x4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318');
const me = owner.address.toLowerCase();
const sessionId = '8f0b7a8e-3a55-4f0f-9a43-1b1d7c3f2a10';
const challengeId = '1c7f2d9e-6b4a-4d8e-9f21-0a3b5c7d9e11';

interface Seen { method: string; url: string; headers: IncomingMessage['headers']; body: string }
const seen: Seen[] = [];
let access = 'access-1', refresh = 'refresh-1', csrf = randomBytes(32).toString('hex');
const session = (minutes: number) => ({ sessionId, wallet: me, deviceLabel: 'Web browser', scopes: ['account'], accessExpiresAt: new Date(Date.now() + minutes * 60_000).toISOString(), refreshExpiresAt: new Date(Date.now() + 30 * 86_400_000).toISOString(), revokedAt: null });
const envelope = (data: unknown) => ({ requestId: 'req-1', data, quality: 'complete', generatedAt: new Date().toISOString() });

const server = createServer((request, response) => {
  let body = '';
  request.on('data', chunk => { body += chunk; });
  request.on('end', async () => {
    const url = request.url ?? '', method = request.method ?? 'GET';
    seen.push({ method, url, headers: request.headers, body });
    const reply = (status: number, payload?: unknown, headers: Record<string, string | string[]> = {}) => {
      for (const [name, value] of Object.entries(headers)) response.setHeader(name, value);
      response.statusCode = status;
      if (payload === undefined) { response.end(); return; }
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify(payload));
    };
    const credentials = () => ({
      'Set-Cookie': [`__Host-kindle_access=${access}; Path=/; HttpOnly; Secure; SameSite=Lax`, `__Host-kindle_refresh=${refresh}; Path=/; HttpOnly; Secure; SameSite=Lax`, `__Host-kindle_csrf=${csrf}; Path=/; Secure; SameSite=Lax`],
      'X-CSRF-Token': csrf,
    });
    if (method === 'POST' && url === '/v2/auth/challenges') {
      const parsed = JSON.parse(body) as { wallet: string; origin: string };
      if (parsed.origin !== origin || parsed.wallet !== me || typeof request.headers['idempotency-key'] !== 'string') return reply(403, { code: 'SCOPE_DENIED' });
      return reply(201, envelope({ challenge: { challengeId, wallet: me, nonce: 'ab'.repeat(32), domain: new URL(origin).host, uri: origin, chainId: '125', purpose: 'login', issuedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 300_000).toISOString(), message: 'signed-bytes' } }));
    }
    if (method === 'POST' && url === '/v2/auth/sessions') {
      const parsed = JSON.parse(body) as { challengeId: string; proof: { wallet: string; signature: string; kind: string }; deviceLabel: string };
      if (parsed.challengeId !== challengeId || parsed.proof.kind !== 'eoa_eip191' || !await verifyMessage({ address: owner.address, message: 'signed-bytes', signature: parsed.proof.signature as `0x${string}` })) return reply(401, { code: 'AUTH_REQUIRED' });
      return reply(200, envelope({ session: session(15), accessToken: access }), credentials());
    }
    if (method === 'POST' && url === '/v2/auth/sessions:refresh') {
      if (request.headers['x-csrf-token'] !== csrf || body !== '{}') return reply(403, { code: 'SCOPE_DENIED' });
      access = 'access-2'; refresh = 'refresh-2'; csrf = randomBytes(32).toString('hex');
      return reply(200, envelope({ session: session(15), accessToken: access }), credentials());
    }
    if (method === 'DELETE' && url === `/v2/auth/sessions/${sessionId}`) return request.headers.authorization === `Bearer ${access}` ? reply(204) : reply(401, { code: 'AUTH_REQUIRED' });
    if (method === 'PUT' && url.startsWith('/v1/me/watchlist/')) {
      const encoded = String(request.headers['x-kindle-operation'] ?? '');
      const decoded = JSON.parse(Buffer.from(encoded, 'base64url').toString('utf8')) as { path: string; bodyHash: string; action: string };
      const signed = await verifyMessage({ address: owner.address, message: `KindleLaunch signed operation v2\n${Buffer.from(encoded, 'base64url').toString('utf8')}`, signature: String(request.headers['x-kindle-operation-signature']) as `0x${string}` });
      if (request.headers.authorization !== `Bearer ${access}` || !signed || decoded.path !== url || decoded.action !== 'watchlist.update' || decoded.bodyHash !== createHash('sha256').update(body).digest('hex')) return reply(403, { code: 'SCOPE_DENIED' });
      return reply(200, envelope({ item: { poolAddress: url.split('/').pop(), alertEnabled: false } }));
    }
    return reply(404, { code: 'NOT_FOUND' });
  });
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
afterAll(() => new Promise<void>(resolve => server.close(() => resolve())));

interface BrowserDom { window: Window & typeof globalThis }
const { JSDOM } = createRequire(import.meta.url)('jsdom') as { JSDOM: new (html: string, options: { url: string }) => BrowserDom };
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: origin });
const browserGlobals = globalThis as unknown as Record<string, unknown>;
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
process.env.NEXT_PUBLIC_GATEWAY_URL = origin;

const transport = await import('./account-transport');

describe('same-origin gateway sign-in', () => {
  it('signs in on the page origin, rotates the access token with the CSRF secret, signs writes with the new bearer and signs out', async () => {
    const signal = new AbortController().signal;
    const challenge = await transport.loginChallenge(owner.address, signal);
    expect(challenge.challengeId).toBe(challengeId);
    const signature = await owner.signMessage({ message: challenge.message });
    const login = await transport.loginProof(challenge.challengeId, me, signature, 'eoa_eip191', signal);
    transport.installAccountSession(login.data.session, login.data.accessToken, transport.accountEpoch(), { signMessage: (_account, message) => owner.signMessage({ message }) });
    const scope = transport.accountSession()!;
    expect(scope.accessToken).toBe('access-1');

    const issuedCsrf = csrf;
    await transport.refreshAccountSession();
    const refreshCall = seen.find(entry => entry.url === '/v2/auth/sessions:refresh')!;
    expect(refreshCall.headers['x-csrf-token']).toBe(issuedCsrf);
    expect(refreshCall.headers['x-csrf-token']).toMatch(/^[0-9a-f]{64}$/);
    // The scope survives the rotation; only its token changed.
    expect(transport.accountSession()).toBe(scope);
    expect(scope.accessToken).toBe('access-2');

    const pool = '0xce20da50f92d21d8bee497910c782e9aa25b6263';
    await expect(transport.putWatch(pool, 'mutation-1')).resolves.toMatchObject({ data: { item: { poolAddress: pool } } });

    await transport.signOutAccount();
    expect(transport.accountSession()).toBeNull();
    const logout = seen.find(entry => entry.method === 'DELETE')!;
    expect(logout.headers.authorization).toBe('Bearer access-2');
    expect(logout.headers['x-csrf-token']).toBe(csrf);
    // Every identity call went to the page origin; nothing addressed a separate identity host.
    expect(seen.filter(entry => entry.url.startsWith('/v2/auth/')).map(entry => entry.headers.host)).toEqual(Array(4).fill(new URL(origin).host));
  });

  it('refuses an owner write when the session has no wallet signer', async () => {
    const signal = new AbortController().signal;
    const challenge = await transport.loginChallenge(owner.address, signal);
    const login = await transport.loginProof(challenge.challengeId, me, await owner.signMessage({ message: challenge.message }), 'eoa_eip191', signal);
    transport.installAccountSession(login.data.session, login.data.accessToken, transport.accountEpoch());
    const before = seen.length;
    await expect(transport.putWatch('0xce20da50f92d21d8bee497910c782e9aa25b6263')).rejects.toMatchObject({ code: 'WALLET_SIGNATURE_REQUIRED' });
    expect(seen.length).toBe(before);
    transport.clearAccountSession(false);
  });
});
