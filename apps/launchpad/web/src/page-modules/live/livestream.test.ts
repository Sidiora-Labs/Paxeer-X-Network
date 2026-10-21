import { createElement, type ReactElement } from 'react';
import { createServer, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { V2ContractTypes } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { poolPath } from '@/core/app-foundation/navigation';
import { loadable } from '@/core/market/unsupported';
import type { LiveDirectoryData } from '@/widgets/live/use-live-directory';

type PoolStats = V2ContractTypes['PoolStats'];

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const now = new Date().toISOString();
const pool = (n: number) => `0x${String(n).repeat(40)}`;
const wallet = (n: number) => `0x${String(n).repeat(39)}f`;
const uuid = (n: number) => `1111111${n}-1111-4111-8111-11111111111${n}`;
const hex64 = (c: string) => `0x${c.repeat(64)}`;
const legacyRow = { id: uuid(1), poolAddress: pool(1), creatorWallet: wallet(1), title: 'Fixture', playbackUrl: 'https://livepeercdn.studio/hls/fixture/index.m3u8', playbackId: 'fixture', isLive: true, viewerCount: 1, startedAt: 1, endedAt: null, createdAt: 1 };

function liveRow(n: number, viewerCount: number | null, extra: Record<string, unknown> = {}) {
  return {
    revision: '3', generatedAt: now, streamId: uuid(n), poolAddress: pool(n), creatorWallet: wallet(n), title: `Broadcast ${n}`, status: 'live',
    playbackUrl: `https://livepeercdn.studio/hls/stream${n}/index.m3u8`, viewerCount, viewerQuality: viewerCount === null ? 'unavailable' : 'verified', ...extra,
  };
}

function poolStats(address: string, mcap: string | null): PoolStats {
  const position = { blockHash: hex64('a'), chainId: '125' as const, height: '1', logIndex: '0', transactionHash: hex64('b'), transactionIndex: '0' };
  return {
    buys24h: '1', changeBaselineQuality: 'complete', circulatingMarketCapUsdlRaw: mcap, holderCount: '10', indicativeTvlUsdlRaw: null, poolAddress: address,
    priceChangeBps24h: null, realUsdlBalanceRaw: '0', sells24h: '0', sourceUpperPosition: position, supplyValuationQuality: mcap === null ? 'unavailable' : 'verified',
    totalSupplyValuationUsdlRaw: mcap, valuationChangeUsdlRaw: null, valuationMethod: null, volume24hRaw: '0',
    watermark: { blockHash: hex64('a'), chainId: '125', height: '1', integrity: 'healthy', observedAt: now, projectionRevision: '1' }, windowEnd: now, windowStart: now,
  };
}

// A real HTTP listener plays the livestream host. It is bound and published
// before livestream-api is evaluated because the module reads the origin once.
type Reply = (response: ServerResponse) => void;
const json = (body: unknown, status = 200): Reply => response => { response.writeHead(status, { 'Content-Type': 'application/json' }); response.end(JSON.stringify(body)); };
let reply: Reply = json({ streams: [] });
const requests: string[] = [];
const server = createServer((request, response) => { requests.push(request.url ?? '/'); reply(response); });
await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
process.env.NEXT_PUBLIC_LIVESTREAM_API_URL = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
const api = await import('@/core/clients/livestream-api');
const directory = await import('@/widgets/live/use-live-directory');
const card = await import('@/widgets/live/stream-card');
const row = await import('@/widgets/live/ranked-row');
const hero = await import('@/widgets/live/hero-carousel');
const page = await import('./index');
afterAll(() => new Promise<void>(resolve => server.close(() => resolve())));
beforeEach(() => { requests.length = 0; reply = json({ streams: [] }); });

function render(node: ReactElement, seed?: (client: QueryClient) => void): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  seed?.(client);
  return renderToStaticMarkup(createElement(QueryClientProvider, { client }, node));
}

async function liveEntries() {
  reply = json({ streams: [liveRow(1, 20), liveRow(2, 35, { thumbnailUrl: 'https://thumbs.example.net/2.png' }), liveRow(3, null), liveRow(4, 0)], nextCursor: null });
  const { streams } = await directory.collectLivePages();
  const token1 = `0x${'c'.repeat(40)}`;
  const stats = new Map([[pool(1), poolStats(pool(1), '297000000000')], [pool(2), poolStats(pool(2), '9230000000')], [pool(3), poolStats(pool(3), null)]]);
  const metadata = { [token1]: { token_address: token1, name: 'Alpha', symbol: 'ALP', images: { logo: 'https://cdn.example.net/alpha.png' } } };
  return directory.assembleDirectory(streams, stats, new Map([[pool(1), token1]]), metadata);
}

describe('livestream public and unavailable boundaries', () => {
  it('distinguishes an unavailable host from an actually empty result', async () => {
    reply = json({}, 503);
    const error = await api.getLivePage().catch((e: unknown) => e);
    expect(error).toBeInstanceOf(api.LivestreamApiError);
    expect((error as InstanceType<typeof api.LivestreamApiError>).status).toBe(503);
    expect(page.directoryFailure(loadable<LiveDirectoryData>({ status: 'error', data: undefined, error: error as Error, refetch: async () => { throw new Error('unused'); } }, 'IF_STREAM_LIST'))).toEqual({ operation: 'IF_STREAM_LIST', status: 503, message: 'Live is not loading right now, try again' });
    reply = json({}, 500);
    const generic = await api.getLivePage().catch((e: unknown) => e as Error);
    expect(page.directoryFailure({ state: 'error', error: generic as Error, retry: () => undefined })).toMatchObject({ operation: 'IF_STREAM_LIST', status: 500 });
    reply = json({ streams: [], nextCursor: null });
    await expect(api.getLivePage()).resolves.toEqual({ streams: [], nextCursor: null });
    expect(page.directoryFailure({ state: 'loading' })).toBeNull();
  });

  it('projects only bounded public fields and rejects credential URLs, foreign CDNs, bad counts and oversized payloads', async () => {
    reply = json({ streams: [{ ...legacyRow, streamKey: 'PRIVATE' }] });
    const [stream] = await api.getLiveStreams();
    expect(stream).not.toHaveProperty('streamKey');
    reply = json({ streams: null });
    expect(await api.getLiveStreams()).toEqual([]);
    for (const body of [{ streams: [{ ...legacyRow, playbackUrl: 'https://user:private@livepeercdn.studio/x' }] }, { streams: [{ ...legacyRow, playbackUrl: 'https://evil.invalid/segment' }] }, { streams: [{ ...legacyRow, viewerCount: -1 }] }, {}, { streams: Array(101).fill(legacyRow) }]) {
      reply = json(body);
      await expect(api.getLiveStreams()).rejects.toThrow();
    }
    reply = response => { response.writeHead(200, { 'Content-Type': 'application/json' }); response.end('x'.repeat(1048577)); };
    await expect(api.getLiveStreams()).rejects.toThrow('limit');
  });

  it('rejects path injection and legacy unsigned controls before any request is dispatched', async () => {
    await expect(api.getPoolStreams('../private')).rejects.toThrow();
    const auth = { wallet: legacyRow.creatorWallet, signature: 'reusable', message: 'anything' };
    await expect(api.createStream(legacyRow.poolAddress, 'title', auth)).rejects.toThrow('authorization');
    await expect(api.goLive(legacyRow.id, auth)).rejects.toThrow('authorization');
    await expect(api.endStream(legacyRow.id, auth)).rejects.toThrow('authorization');
    expect(requests).toEqual([]);
  });

  it('keeps pending termination truthful and strips provider secrets from public state', () => {
    for (const url of ['https://169.254.169.254/hls/x/index.m3u8', 'https://livepeercdn.studio.attacker.invalid/hls/x/index.m3u8', 'https://livepeercdn.studio/hls/x/index.m3u8?streamKey=private', 'https://livepeercdn.studio/private']) expect(() => api.approvedPlaybackURL(url)).toThrow();
    const stream = api.parseStream({ ...liveRow(1, null), status: 'ending', playbackUrl: null, streamKey: 'private', rtmpUrl: 'rtmps://private' });
    expect(stream.status).toBe('ending');
    expect(stream.isLive).toBe(false);
    expect(JSON.stringify(stream)).not.toContain('private');
  });
});

describe('live page structure', () => {
  it('uses the provider thumbnail, else the coin image (thumbnail absent, not empty), else the placeholder block', async () => {
    const entries = await liveEntries();
    const byPool = (n: number) => entries.find(entry => entry.poolAddress === pool(n))!;
    expect(byPool(2).thumbnailUrl).toBe('https://thumbs.example.net/2.png');
    expect(card.streamTile(byPool(2))).toEqual({ kind: 'thumbnail', src: 'https://thumbs.example.net/2.png' });
    expect(byPool(1).thumbnailUrl).toBeNull();
    expect(card.streamTile(byPool(1))).toEqual({ kind: 'coin', src: 'https://cdn.example.net/alpha.png' });
    expect(card.streamTile(byPool(3))).toEqual({ kind: 'none' });
    expect(card.coinLabel(byPool(1))).toBe('Alpha');
    expect(card.coinLabel(byPool(3))).toBe('0x3333...3333');
    expect(card.streamCardLabel(byPool(4))).toBe('Broadcast 4, 0x4444...4444, live, 0 viewers');
    expect(card.streamCardLabel(byPool(3))).toContain('No viewer count yet');
  });

  it('renders a card with every overlay, a coin link and no border utilities', async () => {
    const entries = await liveEntries();
    const alpha = entries.find(entry => entry.poolAddress === pool(1))!;
    const markup = render(createElement(card.StreamCard, { entry: alpha, creatorLabel: 'alice' }));
    expect(markup).toContain(`href="${poolPath(125, pool(1))}"`);
    expect(markup).toContain('LIVE');
    expect(markup).toContain('$297K');
    expect(markup).toContain('20 viewers');
    expect(markup).toContain('ALP');
    expect(markup).toContain('alice');
    expect(markup).toContain('Broadcast 1');
    expect(markup).toContain('src="https://cdn.example.net/alpha.png"');
    expect(markup).not.toMatch(BORDER);
  });

  it('ranks rows by viewers and by market cap, shows four cards and links to the show-all grid', async () => {
    const entries = await liveEntries();
    const viewers = render(createElement(row.RankedRow, { sort: 'viewers', entries: directory.rankByViewers(entries), creatorLabel: (w: string) => w.slice(2, 10) }));
    const order = [...viewers.matchAll(/data-stream-id="([^"]+)"/g)].map(match => match[1]);
    expect(order).toEqual([uuid(2), uuid(1), uuid(4), uuid(3)]);
    expect(viewers).toContain('href="/live/all?sort=viewers"');
    expect(viewers).toContain('Most viewers');
    const mcap = render(createElement(row.RankedRow, { sort: 'mcap', entries: directory.rankByMarketCap(entries), creatorLabel: (w: string) => w.slice(2, 10) }));
    expect([...mcap.matchAll(/data-stream-id="([^"]+)"/g)].map(match => match[1]).slice(0, 2)).toEqual([uuid(1), uuid(2)]);
    expect(mcap).toContain('href="/live/all?sort=mcap"');
    expect(row.parseLiveSort('mcap')).toBe('mcap');
    expect(row.parseLiveSort('anything')).toBe('viewers');
    expect(row.parseLiveSort(null)).toBe('viewers');
  });

  it('mounts exactly one player for the active hero slide and one dot per featured stream', async () => {
    const featured = directory.rankByViewers(await liveEntries());
    const markup = render(createElement(hero.HeroCarousel, { entries: featured, creatorLabel: (w: string) => w.slice(2, 10) }));
    expect(markup.match(/<video/g)).toHaveLength(1);
    expect(markup).toContain(`data-stream-id="${uuid(2)}"`);
    expect(markup.match(/aria-label="Show stream \d of 4/g)).toHaveLength(4);
    expect(markup.match(/aria-current="true"/g)).toHaveLength(1);
    expect(markup).toContain('Next stream: Broadcast 1');
    expect(markup).not.toMatch(BORDER);
    expect(hero.stepIndex(0, -1, 6)).toBe(5);
    expect(hero.stepIndex(5, 1, 6)).toBe(0);
    expect(hero.activeSlideIndex(featured, uuid(4))).toBe(2);
    expect(hero.activeSlideIndex(featured, uuid(9))).toBe(0);
    expect(hero.shouldAutoAdvance({ slides: 6, interacted: false, reducedMotion: false, hovered: false })).toBe(true);
    expect(hero.shouldAutoAdvance({ slides: 6, interacted: true, reducedMotion: false, hovered: false })).toBe(false);
    expect(hero.shouldAutoAdvance({ slides: 6, interacted: false, reducedMotion: true, hovered: false })).toBe(false);
    expect(hero.shouldAutoAdvance({ slides: 1, interacted: false, reducedMotion: false, hovered: false })).toBe(false);
  });

  it('renders the page from a directory refresh: hero, both rows, and the empty state when nobody is live', async () => {
    const entries = await liveEntries();
    const data = { entries, fetchedAt: now, pages: 1, truncated: false, sources: { stats: { state: 'ready' as const }, identity: { state: 'ready' as const }, metadata: { state: 'ready' as const } } };
    const full = render(createElement(page.default), client => client.setQueryData(queryKeys.liveDirectory(), data));
    expect(full).toContain('aria-roledescription="carousel"');
    expect(full.match(/<video/g)).toHaveLength(1);
    expect(full).toContain('Most viewers');
    expect(full).toContain('Highest market cap');
    expect(full).not.toContain('No live streams');
    expect(full).not.toMatch(BORDER);
    const empty = render(createElement(page.default), client => client.setQueryData(queryKeys.liveDirectory(), { ...data, entries: [] }));
    expect(empty).toContain('No live streams right now');
    expect(empty).not.toContain('<video');
    // Nobody live keeps the hero frame and both row frames instead of replacing the page.
    expect(empty).toContain('data-live-hero-frame');
    expect(empty).toContain('data-live-row="viewers"');
    expect(empty).toContain('data-live-row="mcap"');
    expect(empty.match(/data-live-row-notice/g)).toHaveLength(2);
    expect(empty.match(/data-state="empty"/g)).toHaveLength(2);
    const loading = render(createElement(page.default));
    expect(loading).toContain('Loading streams');
  });

  it('keeps the hero frame and both row frames when the directory is unavailable, with plural copy and no fabricated stream', async () => {
    reply = json({}, 503);
    const error: Error = await api.getLivePage().then(() => new Error('expected the 503 to reject'), (e: unknown) => e as Error);
    expect(error).toBeInstanceOf(api.LivestreamApiError);
    const markup = render(createElement(page.default), client => {
      // A settled failure: without retryOnMount the observer reports the stored error instead of an optimistic refetch.
      client.setDefaultOptions({ queries: { retry: false, retryOnMount: false } });
      client.getQueryCache().build(client, { queryKey: queryKeys.liveDirectory() }).setState({ status: 'error', error, fetchStatus: 'idle', data: undefined, errorUpdatedAt: Date.now(), errorUpdateCount: 1, fetchFailureCount: 1 });
    });
    expect(markup).toContain('Live streams are not available yet');
    expect(markup).not.toContain('Live streams is not available');
    // The copy comes from the shared states primitive (plural title), announced as a status inside the hero frame.
    const heroStart = markup.indexOf('data-live-hero-frame');
    const rowsStart = markup.indexOf('data-live-row="viewers"');
    const heroMarkup = markup.slice(heroStart, rowsStart);
    expect(heroStart).toBeGreaterThan(-1);
    expect(rowsStart).toBeGreaterThan(heroStart);
    expect(heroMarkup).toContain('role="status"');
    expect(heroMarkup).toContain('Live streams are not available yet');
    expect(heroMarkup).toContain('Try again');
    expect(markup.indexOf('data-live-row="mcap"')).toBeGreaterThan(rowsStart);
    // Each row frame carries the same compact pill (no retry of its own); the hero pill owns "Try again".
    expect(markup.match(/data-live-row-notice/g)).toHaveLength(2);
    expect(markup.match(/>Live streams are not available yet</g)).toHaveLength(3);
    expect(markup.match(/Try again/g)).toHaveLength(1);
    expect(markup.match(/data-state="unavailable"/g)).toHaveLength(3);
    expect(heroMarkup).toContain('bg-surface-3');
    expect(markup).toContain('Try again');
    expect(markup).toContain('data-live-hero-frame');
    expect(markup).toContain('Most viewers');
    expect(markup).toContain('Highest market cap');
    expect(markup).toContain('href="/live/all?sort=viewers"');
    expect(markup.match(/data-placeholder=""/g)).toHaveLength(2 * row.rankedRowSize);
    // Static frames only: nothing shimmers, no player, no stream link, no number stands in for a stream.
    expect(markup).not.toContain('sl-skeleton');
    expect(markup).not.toContain('<video');
    expect(markup).not.toContain('data-stream-id');
    expect(markup).not.toMatch(/\$\d/);
    expect(markup).not.toMatch(/\d viewers?</);
    expect(markup).not.toMatch(BORDER);
    // Developer detail stays an attribute outside production; it is never body text.
    expect(markup).not.toContain('IF_STREAM_LIST');
    expect(markup).not.toMatch(/>[^<]*IF_STREAM_LIST/);
    // The page title is for assistive tech only; the hero starts under the ticker like the reference.
    const headings = [...markup.matchAll(/<h1([^>]*)>([^<]*)<\/h1>/g)];
    expect(headings).toHaveLength(1);
    expect(headings[0][1]).toContain('sr-only');
    expect(headings[0][2]).toBe('Live streams');
  });

  it('shows one row of cards per breakpoint: two on phones, three from md, four from xl', async () => {
    expect(row.streamGridClass).toBe('grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-4');
    expect([0, 1, 2, 3].map(row.rankedCardVisibility)).toEqual([undefined, undefined, 'hidden md:block', 'hidden xl:block']);
    const entries = directory.rankByViewers(await liveEntries());
    const markup = render(createElement(row.RankedRow, { sort: 'viewers', entries, creatorLabel: (w: string) => w.slice(2, 10) }));
    expect(markup).toContain('hidden md:block');
    expect(markup).toContain('hidden xl:block');
    const frames = render(createElement(row.RankedRow, { sort: 'mcap', entries: [], creatorLabel: (w: string) => w, placeholder: true }));
    expect(frames.match(/data-placeholder=""/g)).toHaveLength(row.rankedRowSize);
    expect(frames).toContain('Highest market cap: no streams to show');
    expect(frames).not.toContain('sl-skeleton');
  });

  it('frames the hero as one surface with the player and the meta column, and a still peek of the next slide', async () => {
    const featured = directory.rankByViewers(await liveEntries());
    const markup = render(createElement(hero.HeroCarousel, { entries: featured, creatorLabel: (w: string) => w.slice(2, 10) }));
    expect(markup).toContain('data-live-hero-frame');
    expect(markup).toContain('max-w-[1000px]');
    expect(markup).toContain('lg:grid-cols-[minmax(0,1.9fr)_minmax(0,1fr)]');
    expect(markup).not.toContain('Open coin');
    expect(markup).not.toMatch(BORDER);
  });

  it('names degraded enrichment sources in plain words, without operation ids or codes', () => {
    const data = { entries: [], fetchedAt: now, pages: 1, truncated: false, nextCursor: null, sources: { stats: { state: 'unsupported' as const, operation: 'IF_STATS_BATCH', status: 503, message: 'projection missing' }, identity: { state: 'skipped' as const }, metadata: { state: 'error' as const, message: 'timeout' } } };
    expect(page.degradedSources(data)).toEqual(['Market caps', 'Coin names and images']);
  });
});
