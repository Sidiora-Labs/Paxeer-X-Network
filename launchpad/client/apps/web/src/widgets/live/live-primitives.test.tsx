import React from 'react';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import type { V2ContractTypes } from '@kindle/sdk';
import { compareDescending, formatMarketCap, formatViewers, rawToBigInt, shortId } from './format';
import { LiveBadge } from './live-badge';
import { ViewerCountChip } from './viewer-count-chip';
import { MarketCapChip } from './market-cap-chip';

type PoolStats = V2ContractTypes['PoolStats'];

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const html = (node: React.ReactElement) => renderToStaticMarkup(node);
const now = new Date().toISOString();
const pool = (n: number) => `0x${String(n).repeat(40)}`;
const wallet = (n: number) => `0x${String(n).repeat(39)}f`;
const uuid = (n: number) => `1111111${n}-1111-4111-8111-11111111111${n}`;
const hex64 = (c: string) => `0x${c.repeat(64)}`;

function liveRow(n: number, viewerCount: number | null, thumbnailUrl?: string | null) {
  return {
    revision: '7', generatedAt: now, streamId: uuid(n), poolAddress: pool(n), creatorWallet: wallet(n), title: `Broadcast ${n}`, status: 'live',
    playbackUrl: `https://livepeercdn.studio/hls/stream${n}/index.m3u8`, viewerCount, viewerQuality: viewerCount === null ? 'unavailable' : 'verified',
    ...(thumbnailUrl === undefined ? {} : { thumbnailUrl }),
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

// A real HTTP listener stands in for the livestream host. It must be bound and
// published through the environment before livestream-api is first evaluated,
// because that module reads NEXT_PUBLIC_LIVESTREAM_API_URL once at load.
const requests: string[] = [];
let endless = false;
const server = createServer((request, response) => {
  const url = new URL(request.url ?? '/', 'http://127.0.0.1');
  requests.push(url.pathname + url.search);
  if (url.pathname !== '/v2/streams/live') { response.writeHead(404, { 'Content-Type': 'application/json' }); response.end('{}'); return; }
  const cursor = url.searchParams.get('cursor');
  const body = endless
    ? { streams: [liveRow(1, 1)], nextCursor: `c${requests.length}`, generatedAt: now }
    : cursor === null
      ? { streams: [liveRow(1, 20), liveRow(2, 35), { ...liveRow(5, 0), status: 'ending', playbackUrl: null }], nextCursor: 'page-2', generatedAt: now }
      : { streams: [liveRow(3, null), liveRow(1, 20)], nextCursor: null, generatedAt: now };
  response.writeHead(200, { 'Content-Type': 'application/json' });
  response.end(JSON.stringify(body));
});
await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
process.env.NEXT_PUBLIC_LIVESTREAM_API_URL = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
const api = await import('@/core/clients/livestream-api');
const directory = await import('./use-live-directory');
const player = await import('./stream-player');
afterAll(() => new Promise<void>(resolve => server.close(() => resolve())));

describe('live formatters', () => {
  it('pluralises viewers and never hides zero', () => {
    expect(formatViewers(0)).toBe('0 viewers');
    expect(formatViewers(1)).toBe('1 viewer');
    expect(formatViewers(12345)).toBe('12.3K viewers');
    expect(formatViewers(null)).toBe('No viewer count yet');
    expect(formatViewers(5, 'unavailable')).toBe('No viewer count yet');
  });
  it('formats raw USDL market cap compactly and refuses non-integers', () => {
    expect(formatMarketCap('9230000000')).toBe('$9.23K');
    expect(formatMarketCap('297000000000')).toBe('$297K');
    expect(formatMarketCap(null)).toBeNull();
    expect(formatMarketCap('abc')).toBeNull();
  });
  it('derives the 8 character short id unless a display name exists', () => {
    expect(shortId('0xAbCdEf0123456789abcdef0123456789abcdef01')).toBe('AbCdEf01');
    expect(shortId('0xAbCdEf0123456789abcdef0123456789abcdef01', '  ')).toBe('AbCdEf01');
    expect(shortId('0xAbCdEf0123456789abcdef0123456789abcdef01', 'alice')).toBe('alice');
  });
  it('orders descending with nulls last', () => {
    expect([null, 3n, 10n, null, 7n].sort(compareDescending)).toEqual([10n, 7n, 3n, null, null]);
    expect(rawToBigInt('-1')).toBeNull();
    expect(rawToBigInt('42')).toBe(42n);
  });
});

describe('live chips and badge', () => {
  it('render role text without border utilities', () => {
    for (const markup of [
      html(<LiveBadge />), html(<LiveBadge variant="dot" />),
      html(<ViewerCountChip count={0} quality="verified" />), html(<ViewerCountChip count={1} variant="media" />), html(<ViewerCountChip count={null} quality="unavailable" />),
      html(<MarketCapChip raw="9230000000" variant="media" />), html(<MarketCapChip raw={null} />),
    ]) expect(markup).not.toMatch(BORDER);
    expect(html(<LiveBadge />)).toContain('LIVE');
    expect(html(<LiveBadge variant="dot" />)).toContain('Live');
    expect(html(<ViewerCountChip count={0} quality="verified" />)).toContain('0 viewers');
    expect(html(<ViewerCountChip count={1} />)).toContain('1 viewer<');
    expect(html(<ViewerCountChip count={9} quality="stale" />)).toContain('estimate updating');
    expect(html(<ViewerCountChip count={null} quality="unavailable" />)).toContain('No viewer count yet');
    const media = html(<MarketCapChip raw="9230000000" variant="media" />);
    expect(media).toContain('$9.23K');
    expect(media).toContain('text-success');
    expect(html(<MarketCapChip raw={null} />)).toContain('not available yet');
  });
});

describe('stream thumbnail projection', () => {
  it('accepts an https thumbnail, defaults to null and rejects unsafe URLs', () => {
    expect(api.parseStream(liveRow(1, 3, 'https://thumbs.example.net/stream1.png')).thumbnailUrl).toBe('https://thumbs.example.net/stream1.png');
    expect(api.parseStream(liveRow(1, 3)).thumbnailUrl).toBeNull();
    expect(api.parseStream(liveRow(1, 3, null)).thumbnailUrl).toBeNull();
    for (const bad of ['http://thumbs.example.net/a.png', 'https://user:secret@thumbs.example.net/a.png', 'https://thumbs.example.net/a.png#frag', 'not a url', 'https://']) {
      expect(() => api.parseStream(liveRow(1, 3, bad))).toThrow('thumbnail');
    }
    const legacy = { id: uuid(2), poolAddress: pool(2), creatorWallet: wallet(2), title: 'Legacy', playbackUrl: 'https://livepeercdn.studio/hls/legacy/index.m3u8', playbackId: 'legacy', isLive: true, viewerCount: 1, startedAt: 1, endedAt: null, createdAt: 1 };
    expect(api.parseStream(legacy).thumbnailUrl).toBeNull();
    expect(api.parseStream({ ...legacy, thumbnailUrl: 'https://thumbs.example.net/legacy.jpg' }).thumbnailUrl).toBe('https://thumbs.example.net/legacy.jpg');
  });
});

describe('player lifecycle from the service projection', () => {
  it('maps every status onto an honest state and only attaches playback to a live stream with a playback URL', () => {
    const live = api.parseStream(liveRow(1, 3));
    expect(player.lifecycleState(null, true)).toBe('unavailable');
    expect(player.lifecycleState(null, false)).toBe('loading');
    expect(player.lifecycleState(live, false)).toBe('playable');
    expect(player.lifecycleState({ ...live, playbackUrl: null }, false)).toBe('connecting');
    expect(player.lifecycleState(api.parseStream({ ...liveRow(1, null), status: 'starting', playbackUrl: null }), false)).toBe('connecting');
    const ending = api.parseStream({ ...liveRow(1, null), status: 'ending', playbackUrl: null });
    expect(player.lifecycleState(ending, false)).toBe('ended');
    expect(player.lifecycleState(api.parseStream({ ...liveRow(1, null), status: 'ended', playbackUrl: null }), false)).toBe('ended');
    expect(player.lifecycleState(api.parseStream({ ...liveRow(1, null), status: 'failed', playbackUrl: null }), false)).toBe('failed');
    expect(player.lifecycleState(api.parseStream({ ...liveRow(1, null), status: 'suspended', playbackUrl: null }), false)).toBe('blocked');
    const legacyEnded = api.parseStream({ id: uuid(2), poolAddress: pool(2), creatorWallet: wallet(2), title: 'Legacy', playbackUrl: '', playbackId: 'legacy', isLive: false, viewerCount: 0, startedAt: 1, endedAt: 2, createdAt: 1 });
    expect(player.lifecycleState(legacyEnded, false)).toBe('ended');
    expect(player.playerLabel('ended', ending)).toContain('ending');
    expect(player.playerLabel('ended', live)).toBe('Stream ended');
    expect(player.playerLabel('connecting', live)).toBe('Connecting...');
    expect(player.playerLabel('connecting', { ...live, isLive: false })).toBe('The stream is starting');
    for (const state of ['loading', 'live', 'buffering', 'blocked', 'failed', 'unavailable'] as const) expect(player.playerLabel(state, live).length).toBeGreaterThan(0);
  });
});

describe('directory assembly and ranking', () => {
  it('joins streams with stats and metadata, degrading every missing field to null', () => {
    const streams = [liveRow(1, 20), liveRow(2, 35, 'https://thumbs.example.net/2.png'), liveRow(3, null), liveRow(4, 35)].map(api.parseStream).filter(api.isDirectoryEntry);
    expect(streams).toHaveLength(4);
    const stats = new Map([[pool(1), poolStats(pool(1), '297000000000')], [pool(2), poolStats(pool(2), '9230000000')], [pool(3), poolStats(pool(3), null)]]);
    const token1 = `0x${'c'.repeat(40)}`;
    const tokens = new Map([[pool(1), token1]]);
    const metadata = { [token1]: { token_address: token1, name: 'Alpha', symbol: 'ALP', images: { logo: 'https://cdn.example.net/alpha.png' } } };
    const entries = directory.assembleDirectory(streams, stats, tokens, metadata);
    const first = entries.find(entry => entry.poolAddress === pool(1));
    const second = entries.find(entry => entry.poolAddress === pool(2));
    const fourth = entries.find(entry => entry.poolAddress === pool(4));
    expect(first).toMatchObject({ tokenAddress: token1, name: 'Alpha', symbol: 'ALP', imageUrl: 'https://cdn.example.net/alpha.png', mcapRaw: '297000000000', mcapQuality: 'verified', thumbnailUrl: null });
    expect(second).toMatchObject({ tokenAddress: null, name: null, symbol: null, imageUrl: null, mcapRaw: '9230000000', thumbnailUrl: 'https://thumbs.example.net/2.png' });
    expect(fourth).toMatchObject({ mcapRaw: null, mcapQuality: 'unavailable', metadata: null });
    expect(directory.rankByViewers(entries).map(entry => entry.stream.id)).toEqual([uuid(2), uuid(4), uuid(1), uuid(3)]);
    expect(directory.rankByMarketCap(entries).map(entry => entry.stream.id)).toEqual([uuid(1), uuid(2), uuid(4), uuid(3)]);
  });
});

describe('live page walk against a real HTTP listener', () => {
  it('follows the cursor once per page, keeps only live rows once each and narrows to directory entries', async () => {
    requests.length = 0;
    const result = await directory.collectLivePages();
    expect(requests).toEqual(['/v2/streams/live?limit=100', '/v2/streams/live?limit=100&cursor=page-2']);
    expect(result.pages).toBe(2);
    expect(result.truncated).toBe(false);
    expect(result.nextCursor).toBeNull();
    expect(result.streams.map(stream => stream.id)).toEqual([uuid(1), uuid(2), uuid(3)]);
    for (const stream of result.streams) { expect(stream.status).toBe('live'); expect(stream.revision).toBe('7'); expect(stream.thumbnailUrl).toBeNull(); }
  });

  it('stops at the page cap and reports truncation instead of looping', async () => {
    endless = true;
    requests.length = 0;
    try {
      const result = await directory.collectLivePages();
      expect(requests).toHaveLength(directory.liveDirectoryMaxPages);
      expect(result.pages).toBe(directory.liveDirectoryMaxPages);
      expect(result.truncated).toBe(true);
      expect(result.streams).toHaveLength(1);
      expect(result.nextCursor).not.toBeNull();
      requests.length = 0;
      const more = await directory.collectLivePages(undefined, result.nextCursor, 1);
      expect(requests).toEqual([`/v2/streams/live?limit=100&cursor=${encodeURIComponent(result.nextCursor ?? '')}`]);
      expect(more.pages).toBe(1);
      expect(more.truncated).toBe(true);
    } finally { endless = false; }
  });

  it('sends the server-side sort and page size and rejects out-of-range sizes before any request', async () => {
    requests.length = 0;
    await api.getLivePage(undefined, { sort: 'mcap', limit: 24 });
    expect(requests).toEqual(['/v2/streams/live?sort=mcap&limit=24']);
    await expect(api.getLivePage(undefined, { limit: 0 })).rejects.toThrow('page size');
    await expect(api.getLivePage(undefined, { limit: 101 })).rejects.toThrow('page size');
    expect(requests).toHaveLength(1);
  });
});
