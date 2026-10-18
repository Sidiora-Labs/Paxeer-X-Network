/**
 * Routes no backing service answers (development) reply
 * `503 DEPENDENCY_UNAVAILABLE {"retryable":false}` with no Retry-After. These
 * suites run the real profile widgets against a real local HTTP origin that
 * answers exactly that, with a real wagmi connection and a real account
 * session, and check that each one renders its unavailable state (never an
 * error), that reads are not retried, and that a wallet without a saved
 * profile can create one at revision 0. Owner writes carry the operation
 * envelope the production media/user authorizer requires, signed by the wallet
 * that proved the login; the fixture checks it the way shared/auth does.
 */
import { createServer, type IncomingMessage } from 'node:http';
import type { AddressInfo } from 'node:net';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createRequire } from 'node:module';
import { createHash } from 'node:crypto';
import { privateKeyToAccount } from 'viem/accounts';
import { verifyMessage } from 'viem';

// Same environment recipe as portfolio-widgets.test.tsx: node fetch/AbortController with the browser
// globals Node lacks taken from a real jsdom window.
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
// Node's own navigator has no onLine; the watchlist store treats that as offline.
if (typeof navigator !== 'undefined' && navigator.onLine === undefined) Object.defineProperty(navigator, 'onLine', { value: true, configurable: true });

const owner = privateKeyToAccount('0x4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318');
const me = owner.address.toLowerCase();
const pool = '0x' + '33'.repeat(20);
const notServed = { code: 'DEPENDENCY_UNAVAILABLE', message: 'no backing service in development', retryable: false, requestId: 'req-dev' };
const savedRevision = '1760000000';

interface Seen { method: string; url: string; headers: IncomingMessage['headers']; body: string }
const seen: Seen[] = [];
let profileRow: { displayName: string; bio: string } | null = null;
let flaky = 0;

function presentation() {
  return { wallet: me, displayName: profileRow?.displayName ?? '', bio: profileRow?.bio ?? '', avatarAssetId: null, bannerAssetId: null, website: null, socials: {}, creatorPools: null, revision: savedRevision };
}
function envelope(data: unknown, revision: string | null = null) {
  return { data, generatedAt: new Date().toISOString(), nextCursor: null, quality: 'complete', requestId: 'req-1', revision, watermark: null };
}

/** The shared/auth OperationAuthorizer checks a write makes: envelope binding, exact body hash, owner signature. */
async function operationAccepted(headers: IncomingMessage['headers'], method: string, path: string, action: string, body: string) {
  const encoded = headers['x-kindle-operation'], signature = headers['x-kindle-operation-signature'];
  if (typeof encoded !== 'string' || typeof signature !== 'string' || headers.authorization !== 'Bearer access-token-1') return false;
  const envelope = JSON.parse(Buffer.from(encoded, 'base64url').toString('utf8')) as Record<string, unknown>;
  const now = Math.floor(Date.now() / 1000);
  if (envelope.version !== 2 || envelope.chainId !== '125' || envelope.environment !== 'production' || envelope.actor !== me || envelope.action !== action || envelope.method !== method || envelope.path !== path) return false;
  if (envelope.bodyHash !== createHash('sha256').update(body).digest('hex') || typeof envelope.issuedAt !== 'number' || typeof envelope.expiresAt !== 'number' || envelope.expiresAt - envelope.issuedAt > 300 || envelope.expiresAt <= now) return false;
  return verifyMessage({ address: owner.address, message: `KindleLaunch signed operation v2\n${Buffer.from(encoded, 'base64url').toString('utf8')}`, signature: signature as `0x${string}` });
}
const denied = { code: 'SCOPE_DENIED', requestId: 'req-403' };

const server = createServer((request, response) => {
  let body = '';
  request.on('data', chunk => { body += chunk; });
  request.on('end', async () => {
    const url = request.url ?? '';
    const method = request.method ?? 'GET';
    seen.push({ method, url, headers: request.headers, body });
    const reply = (status: number, payload?: unknown) => {
      response.setHeader('Access-Control-Allow-Origin', '*');
      response.statusCode = status;
      if (payload === undefined) { response.end(); return; }
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify(payload));
    };
    if (method === 'OPTIONS') {
      response.setHeader('Access-Control-Allow-Origin', '*');
      response.setHeader('Access-Control-Allow-Methods', 'GET,PUT,PATCH,POST,DELETE');
      response.setHeader('Access-Control-Allow-Headers', '*');
      response.statusCode = 204; response.end(); return;
    }
    if (url.startsWith('/v2/discovery/callouts')) return reply(503, notServed);
    if (url.startsWith('/v2/me/rewards')) return reply(503, notServed);
    if (url === '/v3/me/preferences') return reply(503, notServed);
    if (url.startsWith('/v1/me/watchlist/') && method === 'PUT' && !await operationAccepted(request.headers, 'PUT', url, 'watchlist.update', body)) return reply(403, denied);
    if (url.startsWith('/v1/me/watchlist')) return reply(503, notServed);
    if (url === '/v2/me/flaky') {
      flaky++;
      return flaky === 1 ? reply(503, { ...notServed, retryable: true, retryAfterMs: 10 }) : reply(200, { ok: true });
    }
    if (url === `/v3/profiles/${me}` && method === 'GET') return profileRow ? reply(200, envelope({ profile: presentation() }, savedRevision)) : reply(404, { code: 'NOT_FOUND', message: 'profile not found', requestId: 'req-404' });
    if (url === `/v3/profiles/${me}` && method === 'PATCH') {
      if (!await operationAccepted(request.headers, 'PATCH', url, 'profile.update', body)) return reply(403, denied);
      const patch = JSON.parse(body) as { expectedRevision: string; displayName?: string; bio?: string };
      const current = profileRow ? savedRevision : '0';
      const match = request.headers['if-match'];
      if (patch.expectedRevision !== current || (match && match !== `"${current}"` && match !== `"${me}:${current}"`)) return reply(412, { code: 'PRECONDITION_FAILED', requestId: 'req-412' });
      profileRow = { displayName: patch.displayName ?? '', bio: patch.bio ?? '' };
      return reply(200, envelope({ profile: presentation() }, savedRevision));
    }
    return reply(404, { code: 'NOT_FOUND', requestId: 'req-miss' });
  });
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['DATA_API_URL', 'GATEWAY_URL', 'USER_API_URL', 'METADATA_API_URL', 'PNL_API_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WagmiProvider, createConfig, http } = await import('wagmi');
const { connect, disconnect } = await import('wagmi/actions');
const { injected } = await import('wagmi/connectors');
const { defineChain } = await import('viem');
const { accountEpoch, accountRequest, clearAccountSession, installAccountSession } = await import('./account-transport');
const { UtilityRows } = await import('./utility-rows');
const { default: AccountPreferences } = await import('./account-preferences');
const { default: ProfileEditModal } = await import('./profile-edit-modal');
const { mutateWatch, refreshWatchlist, watchState } = await import('./watchlist-state');

/** A small real EIP-1193 wallet on chain 125 holding `me`. */
class TestBrowserWallet {
  private readonly listeners = new Map<string, Set<(...args: unknown[]) => void>>();
  async request({ method }: { method: string }): Promise<unknown> {
    if (method === 'eth_chainId') return '0x7d';
    if (method === 'eth_accounts' || method === 'eth_requestAccounts') return [me];
    if (method === 'wallet_requestPermissions') return [{ parentCapability: 'eth_accounts' }];
    throw Object.assign(new Error(`Unsupported method ${method}`), { code: 4200 });
  }
  on(event: string, listener: (...args: unknown[]) => void) { (this.listeners.get(event) ?? this.listeners.set(event, new Set()).get(event)!).add(listener); return this; }
  removeListener(event: string, listener: (...args: unknown[]) => void) { this.listeners.get(event)?.delete(listener); return this; }
}
(window as unknown as { ethereum: unknown }).ethereum = new TestBrowserWallet();
const chain = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [chain], connectors: [injected()], transports: { 125: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, ssr: false, multiInjectedProviderDiscovery: false });

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

function signIn() {
  installAccountSession({ accessExpiresAt: new Date(Date.now() + 3_600_000).toISOString(), deviceLabel: 'Web browser', refreshExpiresAt: new Date(Date.now() + 86_400_000).toISOString(), revokedAt: null, scopes: ['account'], sessionId: 'sess-dev', wallet: me }, 'access-token-1', accountEpoch(), { signMessage: (_account, message) => owner.signMessage({ message }) });
}

function mount(node: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  act(() => { root.render(<WagmiProvider config={wagmiConfig}><QueryClientProvider client={queryClient}>{node}</QueryClientProvider></WagmiProvider>); });
  return { root, container, queryClient };
}
function unmount(view: ReturnType<typeof mount>) {
  act(() => view.root.unmount());
  view.queryClient.clear();
  view.container.remove();
}
async function waitFor(check: () => boolean, timeoutMs = 4000) {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`Condition not met in time: ${document.body.textContent?.slice(0, 600)}`);
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}
const pause = (ms: number) => act(async () => { await new Promise(resolve => setTimeout(resolve, ms)); });
const count = (method: string, prefix: string) => seen.filter(row => row.method === method && row.url.startsWith(prefix)).length;
function classesOf(root: ParentNode): string[] {
  return Array.from(root.querySelectorAll('[class]')).flatMap(element => (element.getAttribute('class') ?? '').split(/\s+/));
}
function typeInto(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  act(() => { setter.call(input, value); input.dispatchEvent(new dom.window.Event('input', { bubbles: true })); });
}

beforeAll(async () => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  await connect(wagmiConfig, { connector: wagmiConfig.connectors[0]! });
});
beforeEach(() => { seen.length = 0; signIn(); });
afterAll(async () => { clearAccountSession(false); await disconnect(wagmiConfig).catch(() => undefined); server.close(); });

describe('accountRequest honours the error envelope', () => {
  it('does not retry a 503 read whose envelope says retryable:false', async () => {
    const failure = await accountRequest(`${origin}/v3/me/preferences`).catch((error: unknown) => error);
    expect(failure).toMatchObject({ status: 503, code: 'DEPENDENCY_UNAVAILABLE', retryable: false });
    expect(count('GET', '/v3/me/preferences')).toBe(1);
  });

  it('still retries a transient 503 (retryable:true)', async () => {
    flaky = 0;
    await expect(accountRequest(`${origin}/v2/me/flaky`)).resolves.toEqual({ ok: true });
    expect(count('GET', '/v2/me/flaky')).toBe(2);
  });
});

describe('profile utility rows against a non-retryable 503', () => {
  it('callout history and cashback render their unavailable state, not an error, with one request each', async () => {
    const view = mount(<UtilityRows wallet={me} isOwner />);
    await waitFor(() => view.container.textContent?.includes('Cashback is not available yet') ?? false);
    const viewButton = Array.from(view.container.querySelectorAll('button')).find(button => button.textContent === 'View')!;
    act(() => { viewButton.click(); });
    await waitFor(() => view.container.textContent?.includes('Callout history is not available yet') ?? false);
    await pause(900);
    const text = view.container.textContent ?? '';
    expect(text).not.toContain('unavailable right now');
    expect(text).not.toMatch(/DEPENDENCY_UNAVAILABLE|HTTP 503|\/v2\//);
    expect(view.container.querySelectorAll('[role="alert"]')).toHaveLength(0);
    expect(count('GET', '/v2/discovery/callouts')).toBe(1);
    expect(count('GET', '/v2/me/rewards')).toBe(1);
    expect(classesOf(view.container).filter(name => BORDER.test(name))).toEqual([]);
    unmount(view);
  });
});

describe('account preferences against a non-retryable 503', () => {
  it('renders the settings unavailable state instead of an alert', async () => {
    const view = mount(<AccountPreferences />);
    await waitFor(() => view.container.textContent?.includes('Saving settings is not available yet') ?? false);
    expect(view.container.querySelectorAll('[role="alert"]')).toHaveLength(0);
    expect(view.container.textContent).not.toMatch(/DEPENDENCY_UNAVAILABLE/);
    expect(count('GET', '/v3/me/preferences')).toBe(1);
    expect(classesOf(view.container).filter(name => BORDER.test(name))).toEqual([]);
    unmount(view);
  });
});

describe('watchlist writes against a non-retryable 503', () => {
  it('reports plain copy and drops the pending choice so the star reverts', async () => {
    await refreshWatchlist();
    expect(watchState().error).toBe('Your saved watchlist is not available yet');
    await mutateWatch(pool, true);
    expect(watchState().pending).toHaveLength(0);
    expect(watchState().error).toBe('Saving to your watchlist is not available yet');
    expect(count('PUT', `/v1/me/watchlist/${pool}`)).toBe(1);
  });
});

describe('profile edit modal for a wallet with no saved profile', () => {
  it('treats the 404 read as an empty profile at revision 0 and creates the row on Save', async () => {
    profileRow = null;
    let saved = 0;
    const view = mount(<ProfileEditModal open onClose={() => undefined} onSaved={() => { saved++; }} />);
    const dialog = () => document.body.querySelector<HTMLElement>('[role="dialog"]');
    await waitFor(() => dialog()?.textContent?.includes('Set up your profile') ?? false);
    expect(dialog()!.querySelectorAll('[role="alert"]')).toHaveLength(0);
    const name = document.getElementById('profile-edit-displayName') as HTMLInputElement;
    expect(name.disabled).toBe(false);
    typeInto(name, 'Ada');
    const save = Array.from(dialog()!.querySelectorAll('button')).find(button => button.textContent?.includes('Save changes'))!;
    await waitFor(() => !save.disabled);
    act(() => { save.click(); });
    await waitFor(() => dialog()?.textContent?.includes('Profile saved') ?? false);
    const patch = seen.find(row => row.method === 'PATCH' && row.url === `/v3/profiles/${me}`)!;
    expect(patch.headers['if-match']).toBe('"0"');
    expect(JSON.parse(patch.body)).toMatchObject({ expectedRevision: '0', displayName: 'Ada' });
    expect(patch.headers.authorization).toBe('Bearer access-token-1');
    expect(profileRow).toEqual({ displayName: 'Ada', bio: '' });
    expect(saved).toBe(1);
    unmount(view);
  });
});
