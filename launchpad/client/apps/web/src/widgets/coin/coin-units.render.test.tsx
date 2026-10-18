import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { afterAll, describe, expect, it, vi } from 'vitest';
import { SdkClient, decodeV2Contract } from '@kindle/sdk';
import { v2Reads } from '@/core/test-fixtures/market-production/v2-reads';

// vitest.config.ts sets `deps.interopDefault: false` (required by @kindle/sdk), so the default import of
// CJS `next/image` (Avatar, TokenImage) arrives as its whole `exports` object. Re-expose the real
// component as the default, as Next's bundler does (same recipe as ui/shared/token-image.test.tsx).
vi.mock('next/image', async importOriginal => {
  const mod = await importOriginal<{ default: unknown }>();
  const cjs = mod.default as { default?: unknown } | undefined;
  return { ...mod, default: cjs && typeof cjs === 'object' && 'default' in cjs ? cjs.default : mod.default };
});

// Node fetch plus browser globals from a real jsdom window (recipe of terminal/coin-view.render.test.tsx)
// so the resolver, the SDK and the app's providers all reach the local origin below.
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
browserGlobals.Event = dom.window.Event;
browserGlobals.CustomEvent = dom.window.CustomEvent;
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;

const pool = '0xce20da50f92d21d8bee497910c782e9aa25b6263';
const token = '0x0f92fdd087c96dd3413ef98646b94d0d986192fa';
const holder = '0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154';
const requestId = '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01';

/**
 * Share rows of the stats host `GET /stats/{p}/holders` for production pool 0xCE20…6263 (core/stats-workers
 * pool_holders.go holderList shape). Chain 125 truth: the one holder owns 4,803,406.95256 of 1B tokens (0.48%,
 * 48 basis points).
 */
const statsHostHolders = { holders: [{ holderAddress: holder, balance: '4803406952560', pctOfSupply: '48', rank: 1 }] };

const position = { chainId: '125', height: '4200', transactionIndex: '1', logIndex: '0', transactionHash: '0x' + '5'.repeat(64), blockHash: '0x' + '6'.repeat(64) };
/** Token metadata as IF_METADATA_GET serves it: 6 verified decimals, 1B supply. */
const metadata = { requestId, generatedAt: '2026-10-13T00:00:00.000Z', nextCursor: null, quality: 'complete', revision: '1', watermark: null, data: { description: '', displayName: 'Probe', displaySymbol: 'PRB', socialLinks: [], revision: '1', tags: [], token: {
  address: token, decimals: 6, decimalsQuality: 'verified', logoAssetId: null, metadataRevision: '1', name: 'Probe', sourcePosition: position, supplyQuality: 'verified', symbol: 'PRB', totalSupplyRaw: '1000000000000000',
} } };
const poolDetail = v2Reads.responses.poolDetailReal;

const server: Server = createServer((request, response) => {
  const url = request.url ?? '';
  request.resume();
  request.on('end', () => {
    const send = (status: number, body: unknown) => { response.statusCode = status; response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify(body)); };
    // IF_POOL_DETAIL: core/api's recorded bytes for this pool (pool, token and the PublicStatsProjector stats).
    if (request.method === 'GET' && url === `/v2/pools/${pool}`) { response.writeHead(poolDetail.status, poolDetail.headers); response.end(JSON.stringify(poolDetail.body)); return; }
    // Stats host (core/stats-workers, the same local origin here): no transfer-coverage page for this pool yet,
    // so the holders tab reads the share rows of `GET /stats/{p}/holders` only.
    if (request.method === 'GET' && url.startsWith(`/v1/pools/${pool}/holders`)) return send(404, { code: 'NOT_FOUND', message: 'NOT_FOUND', requestId, retryable: false, retryAfterMs: null, fieldErrors: [], details: null });
    if (request.method === 'GET' && url === `/stats/${pool}/holders?limit=100`) return send(200, statsHostHolders);
    if (request.method === 'GET' && url === `/v2/tokens/${token}/metadata`) return send(200, metadata);
    response.statusCode = 404;
    response.end();
  });
});
const origin = await new Promise<string>(resolve => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`)));
for (const name of ['DATA_API_URL', 'STATS_API_URL', 'GATEWAY_URL', 'USER_API_URL', 'METADATA_API_URL', 'PNL_API_URL', 'SOCIAL_API_URL', 'RPC_URL']) process.env[`NEXT_PUBLIC_${name}`] = origin;
process.env.NEXT_PUBLIC_STREAM_ORIGIN = 'ws://127.0.0.1:1';

const { createRoot } = await import('react-dom/client');
const AppProviders = (await import('@/ui/providers/app-providers')).default;
const { MarketClientContext } = await import('@/core/market/client');
const TokenHeader = (await import('./token-header')).default;
const HoldersTable = (await import('./holders-table')).default;
const StatsAuditPanel = (await import('./stats-audit-panel')).default;

afterAll(() => { server.close(); });

interface Mounted { root: Root; container: HTMLDivElement }
function mount(node: React.ReactElement): Mounted {
  const client = new SdkClient({ apiOrigin: origin, identityOrigin: origin, mediaOrigin: origin, streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => { root.render(<AppProviders><MarketClientContext.Provider value={client}>{node}</MarketClientContext.Provider></AppProviders>); });
  return { root, container };
}
function unmount({ root, container }: Mounted) { act(() => root.unmount()); container.remove(); }
async function settle(ms: number) { await act(async () => { await new Promise(resolve => setTimeout(resolve, ms)); }); }
async function waitFor(check: () => boolean, timeoutMs = 8000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await settle(25);
  }
}
const text = (view: Mounted) => view.container.textContent ?? '';

/** The `market.stats.<pool>` frame the production stream delivered after the follow-on buy (core-api-wire.json liveFrames). */
const wire = JSON.parse(readFileSync(new URL('../../core/test-fixtures/market-production/core-api-wire.json', import.meta.url), 'utf8')) as { liveFrames: Array<{ topic?: string; payload?: unknown }> };
const liveStatsFrame = decodeV2Contract('PoolStats', wire.liveFrames.find(frame => frame.topic === `market.stats.${pool}`)?.payload);

describe('coin page units for production pool 0xce20…6263', () => {
  it('header shows the 2.2.0 market cap, price, dev share and the 24h change reason from the recorded IF_POOL_DETAIL bytes', async () => {
    const view = mount(<TokenHeader poolAddress={pool} liveStats={null} livePrice={null} now={Date.now()} />);
    await waitFor(() => /Market cap/i.test(text(view)) && /147\.28/.test(text(view)));
    // Market cap is totalSupplyValuationUsdlRaw 10098226981 (10,098.23 USDL, price x total supply); the circulating 48.51 USDL is the hover value.
    const headline = view.container.querySelector<HTMLButtonElement>('button[aria-label^="Market cap"]');
    expect(headline?.textContent).toBe('Market cap10.1K USDL');
    expect(headline?.getAttribute('title')).toBe('Circulating market cap 48.51 USDL');
    expect(text(view)).not.toMatch(/10\.1B|147\.28M/);
    const volume = Array.from(view.container.querySelectorAll('dd, div, span')).map(node => node.textContent ?? '');
    expect(volume.some(value => value.includes('147.28'))).toBe(true);
    // creatorHoldingShareBps 10000.
    expect(view.container.querySelector('[data-dev-share]')?.textContent).toBe('Creator 100%');
    // The 24h baseline is insufficient_history: no number, the reason instead.
    expect(view.container.querySelector('[data-change="24h"] [aria-label]')?.getAttribute('aria-label')).toBe('Change: Not enough trades yet for a 24h change');
    // The price headline reads spotPriceWad 10098226981692 with adaptive significant digits, never $0.00.
    act(() => headline?.click());
    expect(view.container.querySelector('button[aria-label^="Price"]')?.textContent).toBe('Price$0.0₄1009');
    unmount(view);
  });

  it('header follows a live market.stats frame onto the same cells', async () => {
    const view = mount(<TokenHeader poolAddress={pool} liveStats={liveStatsFrame} livePrice={null} now={Date.now()} />);
    await waitFor(() => view.container.querySelector('[data-coin-header="ready"]') !== null);
    expect(view.container.querySelector('button[aria-label^="Market cap"]')?.textContent).toBe('Market cap10.22K USDL');
    expect(view.container.querySelector('[data-change="24h"]')?.textContent).toBe('up+1.22%');
    act(() => view.container.querySelector<HTMLButtonElement>('button[aria-label^="Market cap"]')?.click());
    expect(view.container.querySelector('button[aria-label^="Price"]')?.textContent).toBe('Price$0.0₄1022');
    unmount(view);
  });

  it('holders tab shows 4.8M tokens, 0.48% of supply, the position at the served spot price and the top-10 and dev shares', async () => {
    const view = mount(<HoldersTable poolAddress={pool} />);
    await waitFor(() => view.container.querySelector('tbody tr') !== null);
    const row = view.container.querySelector('tbody tr')?.textContent ?? '';
    expect(row).toContain('4.8M');
    expect(row).toContain('0.48%');
    expect(row).not.toContain('48.00%');
    // 4,803,406.95256 tokens x 0.000010098226981692 USDL (spotPriceWad) = 48.51 USDL.
    expect(row).toContain('48.51 USDL');
    expect(view.container.querySelector('[data-share-chip="Top 10"]')?.textContent).toBe('Top 10Top 10 holders hold 100%');
    expect(view.container.querySelector('[data-share-chip="Creator"]')?.textContent).toBe('CreatorCreator holds 100%');
    // The holder is not the registry creator, so no dev badge on its row.
    expect(view.container.querySelector('tbody [data-dev-badge]')).toBeNull();
    unmount(view);
  });

  it('stats panel shows the 24h volume as 147.28 USDL, traders and transactions, and the change reason', async () => {
    const view = mount(<StatsAuditPanel poolAddress={pool} liveStats={null} />);
    await waitFor(() => /Volume/.test(text(view)) && /147\.28/.test(text(view)));
    expect(text(view)).not.toMatch(/147\.28M|147\.3M/);
    expect(text(view)).toContain('Traders1');
    expect(text(view)).toContain('Txns2');
    expect(view.container.querySelector('[aria-label^="Change:"]')?.getAttribute('aria-label')).toBe('Change: Not enough trades yet for a 24h change');
    unmount(view);
  });
});
