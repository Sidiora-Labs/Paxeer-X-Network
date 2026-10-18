/**
 * Wallet identity enrichment against a real local HTTP origin standing in for
 * media/user. A wallet with no profile row is a normal answer: it is resolved
 * once, remembered for the session, never re-requested on re-render or on a
 * fresh query cache (a new page), and renders as the shortened wallet. Other
 * statuses keep their real behaviour (503 retried only when retryable).
 */
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { afterAll, beforeEach, describe, expect, it } from 'vitest';

interface BrowserDom { window: Window & typeof globalThis }
type JsdomModule = { JSDOM: new (html: string, options: { url: string; pretendToBeVisual: boolean }) => BrowserDom };
const { JSDOM } = createRequire(import.meta.url)('jsdom') as JsdomModule;
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: 'http://localhost:3000', pretendToBeVisual: true });
const browserGlobals = globalThis as unknown as Record<string, unknown>;
for (const key of Object.getOwnPropertyNames(dom.window)) {
  if (key in globalThis || key.startsWith('_')) continue;
  const value = (dom.window as unknown as Record<string, unknown>)[key];
  browserGlobals[key] = typeof value === 'function' && !/^[A-Z]/.test(key) ? value.bind(dom.window) : value;
}
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;

const absent = '0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154';
const named = '0x2222222222222222222222222222222222222222';
const flakyWallet = '0x3333333333333333333333333333333333333333';
const downWallet = '0x4444444444444444444444444444444444444444';
const fallbackWallet = '0x5555555555555555555555555555555555555555';
const headerWallet = '0x6666666666666666666666666666666666666666';

const seen: string[] = [];
let batchServed = true;
let flaky = 0;

function envelope(data: unknown) {
  return { data, generatedAt: new Date().toISOString(), nextCursor: null, quality: 'complete', requestId: '6f1c2a54-1b9e-4c55-9d0e-3f1a2b3c4d5e', revision: null, watermark: null };
}
function profileOf(wallet: string) {
  return { wallet, displayName: 'hdegroot', bio: '', avatarAssetId: null, bannerAssetId: null, website: null, revision: '7' };
}
const notFound = { code: 'NOT_FOUND', message: 'NOT_FOUND', requestId: '6f1c2a54-1b9e-4c55-9d0e-3f1a2b3c4d50', retryable: false, retryAfterMs: null, fieldErrors: [], details: null };

const server = createServer((request, response) => {
  const url = request.url ?? '';
  seen.push(`${request.method} ${url}`);
  const reply = (status: number, payload?: unknown, headers: Record<string, string> = {}) => {
    response.setHeader('Access-Control-Allow-Origin', '*');
    for (const [key, value] of Object.entries(headers)) response.setHeader(key, value);
    response.statusCode = status;
    if (payload === undefined) { response.end(); return; }
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify(payload));
  };
  const parsed = new URL(url, 'http://fixture');
  if (parsed.pathname === '/v2/profiles:batch') {
    if (!batchServed) return reply(404);
    const wallets = (parsed.searchParams.get('wallets') ?? '').split(',');
    if (wallets.includes(downWallet)) return reply(503, { code: 'DEPENDENCY_UNAVAILABLE', message: 'DEPENDENCY_UNAVAILABLE', requestId: '6f1c2a54-1b9e-4c55-9d0e-3f1a2b3c4d53', retryable: false, retryAfterMs: null, fieldErrors: [], details: null });
    if (wallets.includes(flakyWallet) && ++flaky === 1) return reply(503, { code: 'DEPENDENCY_UNAVAILABLE', message: 'DEPENDENCY_UNAVAILABLE', requestId: '6f1c2a54-1b9e-4c55-9d0e-3f1a2b3c4d53', retryable: true, retryAfterMs: 0, fieldErrors: [], details: null }, { 'Retry-After': '0' });
    return reply(200, envelope({ items: wallets.filter(wallet => wallet === named || wallet === flakyWallet).map(profileOf) }));
  }
  if (parsed.pathname.startsWith('/v3/profiles/')) return reply(404, notFound);
  return reply(404, notFound);
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['DATA_API_URL', 'GATEWAY_URL', 'USER_API_URL', 'METADATA_API_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { HttpError } = await import('@kindle/sdk');
const { useWalletIdentities, loadWalletProfile } = await import('./use-wallet-identities');
const { fetchProfile } = await import('./use-profile');
const { isProfileAbsent, resetProfileAbsenceForSession } = await import('./profile-absence');
const { shortWallet } = await import('@/page-modules/token/market-presentation');

function Names({ wallets, tick }: { wallets: string[]; tick: number }) {
  const { label } = useWalletIdentities(wallets);
  return <ul data-tick={tick}>{wallets.map(wallet => <li key={wallet}>{label(wallet)}</li>)}</ul>;
}

function mountPage() {
  // A fresh QueryClient per mount is what a full page load gives the browser.
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const host = document.createElement('div'); document.body.append(host);
  const root: Root = createRoot(host);
  const render = (wallets: string[], tick: number) => act(() => { root.render(<QueryClientProvider client={client}><Names wallets={wallets} tick={tick} /></QueryClientProvider>); });
  return { host, render, unmount: () => act(() => { root.unmount(); host.remove(); }) };
}

async function until(check: () => boolean) {
  for (let i = 0; i < 200 && !check(); i++) await act(() => new Promise(resolve => setTimeout(resolve, 10)));
  expect(check()).toBe(true);
}
async function settle() {
  for (let i = 0; i < 10; i++) await act(() => new Promise(resolve => setTimeout(resolve, 10)));
}
const requestsFor = (wallet: string) => seen.filter(line => line.includes(wallet));

beforeEach(() => { seen.length = 0; batchServed = true; resetProfileAbsenceForSession(); });
afterAll(() => new Promise<void>(resolve => server.close(() => resolve())));

describe('useWalletIdentities', () => {
  it('resolves a profileless wallet once as data (200 batch, no 404), falls back to the short wallet, and never asks again', async () => {
    const page = mountPage();
    await page.render([absent, named], 0);
    await until(() => page.host.textContent === `${shortWallet(absent)}hdegroot`);
    expect(seen).toEqual([`GET /v2/profiles:batch?wallets=${absent},${named}`]);
    expect(seen.some(line => line.includes('/v3/profiles/'))).toBe(false);
    expect(isProfileAbsent(absent)).toBe(true);
    expect(window.sessionStorage.getItem('sidiora.profile-absent.v1')).toContain(absent);

    // Re-renders (new props, same wallets) issue nothing.
    for (let tick = 1; tick <= 5; tick++) await page.render([named, absent], tick);
    await settle();
    expect(seen).toHaveLength(1);
    await page.unmount();

    // A new page (fresh query cache) still knows the wallet has no profile.
    const next = mountPage();
    await next.render([absent], 0);
    await settle();
    expect(next.host.textContent).toBe(shortWallet(absent));
    expect(requestsFor(absent)).toHaveLength(1);
    await next.unmount();
  });

  it('batches wallets asked for by separate components into one request', async () => {
    const results = await Promise.all([loadWalletProfile(absent), loadWalletProfile(named), loadWalletProfile(absent)]);
    expect(results[0]).toBeNull();
    expect(results[1]?.displayName).toBe('hdegroot');
    expect(results[1]?.source).toBe('v2');
    expect(results[2]).toBeNull();
    expect(seen).toHaveLength(1);
  });

  it('keeps a non-retryable 503 an error: not cached as absent, asked again next time', async () => {
    await expect(loadWalletProfile(downWallet)).rejects.toSatisfy((error: unknown) => error instanceof HttpError && error.status === 503);
    expect(isProfileAbsent(downWallet)).toBe(false);
    expect(requestsFor(downWallet)).toHaveLength(1);
    await expect(loadWalletProfile(downWallet)).rejects.toBeInstanceOf(HttpError);
    expect(requestsFor(downWallet)).toHaveLength(2);
  });

  it('retries a retryable 503 and resolves the profile', async () => {
    flaky = 0;
    const profile = await loadWalletProfile(flakyWallet);
    expect(profile?.displayName).toBe('hdegroot');
    expect(requestsFor(flakyWallet)).toHaveLength(2);
  });

  it('falls back to the per-wallet read where the batch route is not served, and caches its 404 the same way', async () => {
    batchServed = false;
    expect(await loadWalletProfile(fallbackWallet)).toBeNull();
    expect(requestsFor(fallbackWallet)).toEqual([`GET /v2/profiles:batch?wallets=${fallbackWallet}`, `GET /v3/profiles/${fallbackWallet}`]);
    expect(await loadWalletProfile(fallbackWallet)).toBeNull();
    expect(requestsFor(fallbackWallet)).toHaveLength(2);
  });
});

describe('fetchProfile (wallet button)', () => {
  it('treats a v3 404 as no profile once per session', async () => {
    expect(await fetchProfile(headerWallet)).toBeNull();
    expect(await fetchProfile(headerWallet)).toBeNull();
    expect(requestsFor(headerWallet)).toEqual([`GET /v3/profiles/${headerWallet}`]);
  });
});
