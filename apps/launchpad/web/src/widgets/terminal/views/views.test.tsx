import { createServer, type IncomingHttpHeaders, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import type { QueryClient as QueryClientType } from '@tanstack/react-query';
import { createRequire } from 'node:module';
import { afterAll, describe, expect, it } from 'vitest';
import { SdkClient, type V2ContractTypes } from '@kindle/sdk';
import { sameRead, v2Reads } from '@/core/test-fixtures/market-production/v2-reads';
import { statsChanges } from '@/core/market/pool-stats-display';

// Node environment with a real jsdom window's globals installed (see widgets/profile/leaderboard-panel.test.tsx):
// the jsdom environment swaps AbortSignal for its own class, which Node's fetch rejects.
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

type Responder = (method: string, url: string, headers: IncomingHttpHeaders, body: string) => { status: number; body?: unknown; headers?: Record<string, string> };
type Served = { server: Server; origin: string; paths: string[] };

function serve(responder: Responder): Promise<Served> {
  const paths: string[] = [];
  const server = createServer((request, response) => {
    paths.push(`${request.method} ${request.url ?? ''}`);
    const chunks: Buffer[] = [];
    request.on('data', chunk => chunks.push(chunk as Buffer));
    request.on('end', () => {
      const answer = responder(request.method ?? 'GET', request.url ?? '', request.headers, Buffer.concat(chunks).toString('utf8'));
      response.statusCode = answer.status;
      for (const [name, value] of Object.entries(answer.headers ?? {})) response.setHeader(name, value);
      response.setHeader('Access-Control-Allow-Origin', '*');
      if (answer.body === undefined) { response.end(); return; }
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify(answer.body));
    });
  });
  return new Promise(resolve => {
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address() as AddressInfo;
      resolve({ server, origin: `http://127.0.0.1:${port}`, paths });
    });
  });
}

type Watermark = V2ContractTypes['Watermark'];
type PoolStats = V2ContractTypes['PoolStats'];
type StatsBatchResponse = V2ContractTypes['StatsBatchResponse'];

const poolA = '0x' + 'a1'.repeat(20);
const poolB = '0x' + 'b2'.repeat(20);
const viewer = '0x' + 'e5'.repeat(20);
const followedA = '0x' + 'f6'.repeat(20);
const followedB = '0x' + '07'.repeat(20);
const now = Date.now();
const iso = new Date(now).toISOString();
const watermark: Watermark = { chainId: '125', height: '4200', blockHash: '0x' + '6'.repeat(64), projectionRevision: '7', integrity: 'healthy', observedAt: iso };

function poolStats(pool: string, mcapRaw: string, holders: string): PoolStats {
  return {
    poolAddress: pool, buys24h: '12', sells24h: '3', changeBaselineQuality: 'complete', circulatingMarketCapUsdlRaw: mcapRaw, totalSupplyValuationUsdlRaw: mcapRaw, holderCount: holders,
    indicativeTvlUsdlRaw: null, priceChangeBps24h: '1234', realUsdlBalanceRaw: '1000000',
    sourceUpperPosition: { blockHash: '0x' + '2'.repeat(64), chainId: '125', height: '4200', logIndex: '0', transactionHash: '0x' + '3'.repeat(64), transactionIndex: '0' },
    supplyValuationQuality: 'verified', valuationChangeUsdlRaw: null, valuationMethod: null, volume24hRaw: '5000000', watermark,
    windowStart: new Date(now - 86_400_000).toISOString(), windowEnd: iso,
  };
}

const statsBatch: StatsBatchResponse = {
  requestId: '1f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01', generatedAt: iso, nextCursor: null, quality: 'complete', revision: '7', watermark,
  data: { items: [poolStats(poolA, '3120000000', '41'), poolStats(poolB, '764000000000', '9')], missing: [] },
};

const followingLegacy = { wallet_address: viewer, following: [{ wallet: followedA.toUpperCase().replace('0X', '0x'), created_at: new Date(now - 3_600_000).toISOString() }, { wallet: followedB, created_at: 'not a time' }, { wallet: followedA, created_at: iso }] };
const followingEnvelope = { requestId: 'r', data: { items: [{ wallet: followedB, created_at: iso }] }, nextCursor: 'next-1', watermark: null, revision: null, quality: 'complete', generatedAt: iso };

// GET /v2/social/users/{w}/following on the gateway answers a session only
// (proxy/connected.go SocialConnectedRoutes, Authenticate), in the interactions
// envelope media/social writes (devsocial interactionData).
const trackToken = 'access-token-track';
const followingRoute = (owner: string) => `/v2/social/users/${owner}/following?limit=50`;
// The trenches board reads recorded core/api v2 bytes (core-api-wire.json `v2Reads`):
// IF_RANKINGS new, IF_STATS_BATCH and IF_POOL_LIST sort=new for the real chain-125 pool and the frozen example pool.
const { real: recordedPool, realToken: recordedToken, example: examplePool } = v2Reads.pools;
const exampleToken = '0x' + '3'.repeat(40);
const recordedMetadata = {
  [recordedToken]: { token_address: recordedToken, name: 'Chaotic Rocket', symbol: 'CHAROC', images: { logo: null } },
  [exampleToken]: { token_address: exampleToken, name: 'Lil Black Bull', symbol: 'LILAN', images: { logo: null } },
};
const recordedReads = [v2Reads.responses.rankingsNew, v2Reads.responses.statsBatch, v2Reads.responses.poolListNew];
const backend = await serve((method, url, headers, body) => {
  if (method === 'GET' && url.startsWith('/v2/social/users/') && headers.authorization !== `Bearer ${trackToken}`) return { status: 401, body: { code: 'AUTH_EXPIRED', retryable: false } };
  const read = recordedReads.find(entry => sameRead(entry, method, new URL(url, 'http://replay.invalid'), body));
  if (read) return { status: read.status, body: read.body, headers: read.headers };
  if (method === 'GET' && url.startsWith('/metadata/batch?addresses=')) return { status: 200, body: recordedMetadata };
  if (method === 'GET' && url === followingRoute(viewer)) return { status: 200, body: { ...followingEnvelope, data: { items: followingLegacy.following }, nextCursor: null } };
  if (method === 'GET' && url === followingRoute(followedB)) return { status: 200, body: followingEnvelope };
  return { status: 404 };
});
process.env.NEXT_PUBLIC_DATA_API_URL = backend.origin;
process.env.NEXT_PUBLIC_METADATA_API_URL = backend.origin;
process.env.NEXT_PUBLIC_GATEWAY_URL = backend.origin;

const { createRoot } = await import('react-dom/client');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WagmiProvider, createConfig, http } = await import('wagmi');
const { defineChain } = await import('viem');
const { MarketClientContext } = await import('@/core/market/client');
const trenches = await import('./trenches');
const track = await import('./track');
const { accountEpoch, clearAccountSession, installAccountSession } = await import('@/widgets/profile/account-transport');
const TerminalTrenchesView = trenches.default;
const TerminalTrackView = track.default;

const paxeer = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [paxeer], transports: { [paxeer.id]: http() }, ssr: true, multiInjectedProviderDiscovery: false });
const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

function mount(node: React.ReactElement): { root: Root; container: HTMLDivElement; queryClient: QueryClientType } {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const client = new SdkClient({ apiOrigin: backend.origin, identityOrigin: backend.origin, mediaOrigin: backend.origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(
      <WagmiProvider config={wagmiConfig}>
        <QueryClientProvider client={queryClient}>
          <MarketClientContext.Provider value={client}>{node}</MarketClientContext.Provider>
        </QueryClientProvider>
      </WagmiProvider>,
    );
  });
  return { root, container, queryClient };
}

function unmount({ root, container, queryClient }: { root: Root; container: HTMLDivElement; queryClient: QueryClientType }) {
  act(() => root.unmount());
  queryClient.clear();
  container.remove();
}

async function waitFor(check: () => boolean, timeoutMs = 5000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
}

afterAll(() => { backend.server.close(); });

describe('trenches window change', () => {
  const served = (m5: number | null, h1: number | null) => ({ changes: { ...statsChanges(null), '5m': m5 === null ? { value: null, reason: 'Not enough trades yet for a 5m change' } : { value: m5, reason: null }, '1h': h1 === null ? { value: null, reason: 'Not enough trades yet for a 1h change' } : { value: h1, reason: null } } });
  it('maps 5M and 1H to the v2 window fields', () => {
    expect(trenches.windowChange(served(-2.25, 7.5), '5m')).toEqual({ value: -2.25, reason: null });
    expect(trenches.windowChange(served(-2.25, 7.5), '1h')).toEqual({ value: 7.5, reason: null });
  });
  it('never fills a window that is not served, and passes the baseline reason through', () => {
    const thirty = trenches.windowChange(served(-2.25, 7.5), '30m');
    expect(thirty.value).toBeNull();
    expect(thirty.reason).toMatch(/A 30-minute change is not available/);
    expect(trenches.windowChange(served(-2.25, 7.5), '1m')).toEqual({ value: null, reason: expect.stringMatching(/A 1-minute change is not available/) });
    expect(trenches.windowChange(served(null, null), '5m')).toEqual({ value: null, reason: 'Not enough trades yet for a 5m change' });
    expect(trenches.windowChange(served(null, null), '1h').reason).toBe('Not enough trades yet for a 1h change');
  });
});

describe('trenches bucketing', () => {
  it('places every new-ranking row in New pairs and leaves graduation buckets unavailable without a served field', async () => {
    const { buildExploreRow } = await import('@/widgets/explore/use-explore-rows');
    const rows = [poolA, poolB].map((pool, index) => buildExploreRow({ poolAddress: pool, rank: index + 1, stats: statsBatch.data.items[index], metadata: null, now }));
    const buckets = trenches.trenchBuckets(rows);
    expect(buckets.newPairs.map(row => row.poolAddress)).toEqual([poolA, poolB]);
    expect(buckets.aboutToGraduate).toBeNull();
    expect(buckets.graduated).toBeNull();
    expect(trenches.graduationEvidence.served).toBe(false);
  });

  it('renders the board from recorded v2 ranking, stats and pool-list responses plus metadata', async () => {
    const mounted = mount(<TerminalTrenchesView />);
    try {
      await waitFor(() => mounted.container.querySelectorAll('ul[aria-label="New pairs"] > li').length === 2 && (mounted.container.textContent ?? '').includes('CHAROC') && (mounted.container.textContent ?? '').includes('LILAN'));
      const items = Array.from(mounted.container.querySelectorAll('ul[aria-label="New pairs"] > li'));
      const links = items.map(item => item.querySelector('a')?.getAttribute('href'));
      expect(links).toEqual([`/terminal/${recordedPool}`, `/terminal/${examplePool}`]);
      // Token identity comes from the IF_POOL_LIST registry rows, holders from IF_STATS_BATCH.
      expect(items[0].textContent).toContain('CHAROC');
      expect(items[0].textContent).toContain('Chaotic Rocket');
      expect(items[0].querySelector('[title="Holders"]')?.textContent).toBe('2 holders');
      expect(items[1].textContent).toContain('Lil Black Bull');
      expect(items[1].querySelector('[title="Holders"]')?.textContent).toBe('1 holders');
      // The real pool's 2.2.0 row has an insufficient_history 1h baseline: the cell says so instead of guessing.
      expect(items[0].querySelector('[aria-label^="Change not available yet. Not enough trades yet for a 1h change"]')).not.toBeNull();
      // The frozen pre-2.2.0 example row carries no window fields at all.
      expect(items[1].querySelector('[aria-label^="Change not available yet. 1h change not available yet"]')).not.toBeNull();
      // Market cap is the recorded total-supply valuation (10,098.226981 USDL), top-10 and creator shares are 100%.
      expect(items[0].textContent).toContain('10.1K');
      expect(items[0].textContent).toContain('T10 100%');
      expect(items[0].textContent).toContain('Creator 100%');
      const text = mounted.container.textContent ?? '';
      expect(text).toContain('About to graduate is not available yet');
      expect(text).toContain('Graduated is not available yet');
      expect(text).toContain(trenches.graduationEvidence.message);
      expect(text).not.toContain(trenches.graduationEvidence.operation);
      const thirty = Array.from(mounted.container.querySelectorAll('[role="radio"]')).find(radio => radio.textContent === '30M') as HTMLButtonElement;
      act(() => thirty.click());
      expect(items[0].querySelector('[aria-label^="Change not available yet. A 30-minute change"]')).not.toBeNull();
      const five = Array.from(mounted.container.querySelectorAll('[role="radio"]')).find(radio => radio.textContent === '5M') as HTMLButtonElement;
      act(() => five.click());
      expect(items[0].querySelector('[aria-label^="Change not available yet. Not enough trades yet for a 5m change"]')).not.toBeNull();
      expect(BORDER.test(mounted.container.innerHTML)).toBe(false);
      expect(backend.paths).toEqual(expect.arrayContaining(['GET /v2/rankings/new', 'POST /v2/pools/stats:batch', 'GET /v2/pools?sort=new&limit=100']));
      expect(backend.paths.some(path => path.startsWith('GET /stats/'))).toBe(false);
    } finally {
      unmount(mounted);
    }
  });
});

describe('track rows', () => {
  it('parses the legacy following row, dedupes and drops unparseable timestamps', () => {
    const page = track.parseFollowing(followingLegacy, viewer);
    expect(page.items).toEqual([{ wallet: followedA, followedAt: followingLegacy.following[0].created_at }, { wallet: followedB, followedAt: null }]);
    expect(page.nextCursor).toBeNull();
  });
  it('parses the interactions envelope with its cursor and rejects a foreign owner or malformed list', () => {
    expect(track.parseFollowing(followingEnvelope, followedB)).toEqual({ owner: followedB, items: [{ wallet: followedB, followedAt: iso }], nextCursor: 'next-1' });
    expect(() => track.parseFollowing(followingLegacy, followedB)).toThrow(/owner/);
    expect(() => track.parseFollowing({ data: { items: [{ wallet: 'nope' }] } }, viewer)).toThrow();
    expect(() => track.parseFollowing({ data: {} }, viewer)).toThrow();
  });
  it('reads a legacy following list serialized as null as an empty list', () => {
    expect(track.parseFollowing({ wallet_address: viewer, following: null }, viewer)).toEqual({ owner: viewer.toLowerCase(), items: [], nextCursor: null });
    expect(track.parseFollowing({ data: { items: null }, nextCursor: null }, viewer)).toEqual({ owner: viewer.toLowerCase(), items: [], nextCursor: null });
  });
  it('maps followed wallets to rows with names when resolved and short addresses otherwise', () => {
    const page = track.parseFollowing(followingLegacy, viewer);
    const rows = track.trackRows(page, {
      [followedA]: { wallet: followedA, displayName: 'hdegroot', avatarUrl: null, shortAddress: '', profile: null, resolution: 'profile' },
    });
    expect(rows[0]).toMatchObject({ wallet: followedA, label: 'hdegroot', hasName: true, href: `/profile/${followedA}` });
    expect(rows[1]).toMatchObject({ wallet: followedB, label: '0x0707...0707', hasName: false, href: `/profile/${followedB}`, followedAt: null });
  });
  it('reads the following list from the gateway social route with the bearer session', async () => {
    // Signed out, nothing is sent: the route answers a session only.
    const before = backend.paths.length;
    await expect(track.fetchFollowing(viewer)).rejects.toMatchObject({ status: 401 });
    expect(backend.paths.length).toBe(before);
    installAccountSession({ accessExpiresAt: new Date(Date.now() + 3_600_000).toISOString(), deviceLabel: 'Web browser', refreshExpiresAt: new Date(Date.now() + 86_400_000).toISOString(), revokedAt: null, scopes: ['account'], sessionId: 'sess-track', wallet: viewer }, trackToken, accountEpoch());
    try {
      const page = await track.fetchFollowing(viewer.toUpperCase().replace('0X', '0x'));
      expect(page.items.map(item => item.wallet)).toEqual([followedA, followedB]);
      expect(backend.paths.at(-1)).toBe(`GET ${followingRoute(viewer.toLowerCase())}`);
      await expect(track.fetchFollowing(followedA)).rejects.toMatchObject({ status: 404 });
    } finally {
      clearAccountSession(false);
    }
  });
  it('asks a signed-out visitor to sign in instead of reading anything', () => {
    const before = backend.paths.length;
    const mounted = mount(<TerminalTrackView />);
    try {
      expect(mounted.container.textContent).toContain('Sign in to track wallets');
      expect(Array.from(mounted.container.querySelectorAll('button')).some(button => button.textContent === 'Sign in')).toBe(true);
      expect(backend.paths.slice(before).some(path => path.includes('/following'))).toBe(false);
    } finally {
      unmount(mounted);
    }
  });
});
