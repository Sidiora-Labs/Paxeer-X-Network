// @vitest-environment jsdom
import React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { CapabilityUnavailableError, HttpError } from '@kindle/sdk';
import type { Callout, FeedCard, FeedPage } from '@/core/market/use-feed';
import { queryKeys } from '@/core/query-keys';
import { clearAccountSession } from '@/widgets/profile/account-transport';
import { calloutPools, calloutWindows, feedCardView, marketCapRaw, poolCalloutView, withinWindow, type PoolStats } from './callout-model';
import { fetchFollowers, fetchFollowStats, followRoute, parseOperationStats, parseProxyStats, parseProxyFollowers, resetFollowRoute, setFollowing, SocialTransportError, toTransportError, type FollowStatsResponse } from './social-transport';
import { followErrorMessage, followStatsKey } from './use-follow';
import { describeCreateError } from './callout-composer';
import { CalloutCard, CalloutCardSkeleton } from './callout-card';
import { FollowButton } from './follow-button';
import { HotCalloutsPanel } from './hot-callouts-panel';
import { CalloutsRail } from './callouts-rail';
import { ReportMenu } from './report-menu';
import { EmbeddedCoinCard } from './embedded-coin-card';

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const author = '0xAbCdEf0123456789abcdef0123456789ABCDEF01';
const pool = '0x1111111111111111111111111111111111111111';
const now = Date.parse('2026-10-12T12:00:00Z');

const feedCard: FeedCard = {
  Body: 'Early on this one. '.repeat(12),
  PositionDisclosure: 'Holding 1,200 tokens',
  PositionDisclosureKind: 'declared',
  PositionVerified: true,
  State: 'visible',
  Source: { Authorized: true, Corrected: false, CreatorPublicID: author, EventID: 'callout:c1:1', ID: 'c1', Kind: 'callout', Pools: [pool], Public: true, PublishedAt: '2026-10-12T11:30:00Z', SourceRevision: 1, Tombstone: false, VisibilityRevision: 1 },
};
const poolCallout: Callout = { authorWallet: author, body: 'Watching the curve.', calloutId: 'p1', createdAt: '2026-10-05T11:30:00Z', direction: 'bearish', disclosure: 'creator', poolAddress: pool, revision: '2' };
const stats: PoolStats = {
  buys24h: '5', changeBaselineQuality: 'complete', circulatingMarketCapUsdlRaw: '29700000000', holderCount: '12', indicativeTvlUsdlRaw: '1000000', poolAddress: pool,
  priceChangeBps24h: '120', realUsdlBalanceRaw: '1000000', sells24h: '2', sourceUpperPosition: { chainId: '125', height: '20', transactionIndex: '1', logIndex: '0', transactionHash: '0x' + '2'.repeat(64), blockHash: '0x' + '3'.repeat(64) },
  supplyValuationQuality: 'verified', totalSupplyValuationUsdlRaw: '143000000000', valuationChangeUsdlRaw: null, valuationMethod: 'total_supply', volume24hRaw: '5000000',
  watermark: { chainId: '125', height: '20', blockHash: '0x' + '3'.repeat(64), projectionRevision: '1', integrity: 'healthy', observedAt: '2026-10-12T11:59:00Z' }, windowEnd: '2026-10-12T12:00:00Z', windowStart: '2026-10-11T12:00:00Z',
};

function render(node: React.ReactElement, seed?: (client: QueryClient) => void) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  seed?.(client);
  return renderToStaticMarkup(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

afterEach(() => { resetFollowRoute(); clearAccountSession(false); });

describe('callout model', () => {
  it('normalises a discovery feed card and a pool callout to one view', () => {
    const feed = feedCardView(feedCard);
    expect(feed).toMatchObject({ id: 'c1', authorWallet: author.toLowerCase(), poolAddress: pool, createdAt: '2026-10-12T11:30:00Z', positionDisclosure: 'Holding 1,200 tokens', positionVerified: true, removed: false, source: 'feed' });
    const own = poolCalloutView(poolCallout);
    expect(own).toMatchObject({ id: 'p1', direction: 'bearish', disclosure: 'creator', corrected: true, positionVerified: null, source: 'pool' });
    expect(feedCardView({ ...feedCard, Source: { ...feedCard.Source, CreatorPublicID: 'acct_42', Pools: null, Tombstone: true } })).toMatchObject({ authorWallet: null, authorId: 'acct_42', poolAddress: null, removed: true });
  });
  it('bounds the stats batch and filters by publication window', () => {
    const views = Array.from({ length: 60 }, (_, i) => feedCardView({ ...feedCard, Source: { ...feedCard.Source, ID: `c${i}`, Pools: [`0x${String(i).padStart(40, '0')}`] } }));
    expect(calloutPools(views)).toHaveLength(50);
    const stale = { ...poolCalloutView(poolCallout), createdAt: '2026-10-01T00:00:00Z' };
    expect(withinWindow([feedCardView(feedCard), stale], calloutWindows['1d'], now).map(c => c.id)).toEqual(['c1']);
    expect(withinWindow([feedCardView(feedCard), stale], calloutWindows['1w'], now)).toHaveLength(1);
    expect(withinWindow([stale], null, now)).toHaveLength(1);
  });
  it('prefers the verified total-supply valuation for the MC chip', () => {
    expect(marketCapRaw(stats)).toBe('143000000000');
    expect(marketCapRaw({ ...stats, supplyValuationQuality: 'partial' })).toBe('29700000000');
    expect(marketCapRaw({ ...stats, supplyValuationQuality: 'unavailable', circulatingMarketCapUsdlRaw: null })).toBeNull();
    expect(marketCapRaw(null)).toBeNull();
  });
});

describe('social transport', () => {
  it('parses the operation response and both proxy shapes', () => {
    const response: FollowStatsResponse = { data: { followers: 3, following: 1, isFollowing: true }, generatedAt: '2026-10-12T12:00:00Z', nextCursor: null, quality: 'complete', requestId: 'r', revision: null, watermark: null };
    expect(parseOperationStats(response, author.toLowerCase())).toEqual({ wallet: author.toLowerCase(), followers: 3, following: 1, isFollowing: true, route: 'operation' });
    expect(() => parseOperationStats({ ...response, quality: 'quarantined' }, author.toLowerCase())).toThrow(SocialTransportError);
    expect(parseProxyStats({ data: { wallet: author, followers: '7', following: '2', is_following: false, revision: '1' }, nextCursor: null }, author.toLowerCase())).toMatchObject({ followers: 7, following: 2, isFollowing: false, route: 'social-proxy' });
    expect(parseProxyStats({ wallet_address: author.toLowerCase(), followers: 4, following: 0, is_following: true }, author.toLowerCase())).toMatchObject({ followers: 4, isFollowing: true });
    expect(() => parseProxyStats({ followers: -1, following: 0, is_following: true }, author.toLowerCase())).toThrow(SocialTransportError);
    expect(() => parseProxyStats({ wallet_address: pool, followers: 1, following: 0, is_following: true }, author.toLowerCase())).toThrow('MALFORMED_RESPONSE');
    expect(parseProxyFollowers({ wallet_address: author, followers: [{ wallet: pool, created_at: 1 }] }, author.toLowerCase())).toMatchObject({ wallets: [pool], nextCursor: null });
    expect(parseProxyFollowers({ data: { items: [pool] }, nextCursor: 'n' }, author.toLowerCase())).toMatchObject({ wallets: [pool], nextCursor: 'n' });
    expect(() => parseProxyFollowers({ followers: ['nope'] }, author.toLowerCase())).toThrow(SocialTransportError);
  });
  it('maps SDK failures onto transport errors without inventing a success', () => {
    expect(toTransportError(new CapabilityUnavailableError('Unavailable endpoint /v2/me/following/0x', 404), 'operation')).toMatchObject({ status: 404, code: 'capability_unavailable', route: 'operation' });
    expect(toTransportError(new HttpError(429, { code: 'RATE_LIMITED', message: 'RATE_LIMITED', requestId: 'r', retryable: true, retryAfterMs: 1000, fieldErrors: [], details: null }), 'operation')).toMatchObject({ status: 429, code: 'RATE_LIMITED' });
    expect(toTransportError(new TypeError('Access token required'), 'operation')).toMatchObject({ status: 401, code: 'AUTH_REQUIRED' });
    expect(followErrorMessage(new SocialTransportError(404, 'capability_unavailable', 'social-proxy'))).toBe('Following is not available yet');
    expect(followErrorMessage(new SocialTransportError(401, 'AUTH_REQUIRED', 'operation'))).toBe('Sign in to follow');
  });
  it('refuses to follow without a session and rejects malformed wallets before any request', async () => {
    await expect(setFollowing(author, false)).rejects.toMatchObject({ status: 401, code: 'AUTH_REQUIRED' });
    expect(() => fetchFollowStats('0x123')).toThrow(TypeError);
    expect(() => fetchFollowers(author, null, 0)).toThrow(RangeError);
    expect(followRoute()).toBeNull();
  });
  it('scopes the stats cache per viewer', () => {
    expect(followStatsKey(author, null)).toEqual(['production-market', 'follow-stats', author.toLowerCase(), 'anonymous']);
    expect(followStatsKey(author, pool)).toContain(pool);
  });
  it('describes callout create failures from the real error classes', () => {
    expect(describeCreateError(new HttpError(503, { code: 'DEPENDENCY_UNAVAILABLE', message: 'DEPENDENCY_UNAVAILABLE', requestId: 'r', retryable: true, retryAfterMs: null, fieldErrors: [], details: null }))).toMatchObject({ state: 'unsupported', operation: 'IF_CONNECTED_DISCOVERY_CALLOUT_CREATE', status: 503 });
    expect(describeCreateError(new HttpError(400, { code: 'INVALID_INPUT', message: 'INVALID_INPUT', requestId: 'r', retryable: false, retryAfterMs: null, fieldErrors: [], details: null }))).toMatchObject({ state: 'error', message: expect.stringContaining('not accepted') });
    expect(describeCreateError(new HttpError(400, { code: 'INVALID_INPUT', message: 'INVALID_INPUT', requestId: 'r', retryable: false, retryAfterMs: null, fieldErrors: [], details: null }))).not.toMatchObject({ message: expect.stringContaining('INVALID_INPUT') });
    expect(describeCreateError(new CapabilityUnavailableError('gone', 501))).toMatchObject({ state: 'unsupported', status: 501 });
    expect(describeCreateError(new HttpError(401, { code: 'UNAUTHENTICATED', message: 'UNAUTHENTICATED', requestId: 'r', retryable: false, retryAfterMs: null, fieldErrors: [], details: null }))).toEqual({ state: 'error', message: 'Sign in to post a callout' });
    expect(describeCreateError(new TypeError('Session required'))).toEqual({ state: 'error', message: 'Sign in to post a callout' });
  });
});

describe('social components render honest states without border utilities', () => {
  it('renders a callout card with declared position, live MC and the follow and report controls', () => {
    const html = render(<CalloutCard callout={feedCardView(feedCard)} stats={{ state: 'ready', stats }} />);
    expect(html).toContain('Holding 1,200 tokens');
    expect(html).toContain('143K USDL');
    expect(html).toContain('Position verified');
    expect(html).toContain('aria-label="Follow 0xabcd...ef01"');
    expect(html).toContain('More actions for callout by');
    expect(html).toContain(`href="/coin/${pool}#trade"`);
    expect(html).not.toMatch(BORDER);
  });
  it('shows the removal placeholder and the unavailable MC chip from the real stats error', () => {
    const removed = render(<CalloutCard callout={feedCardView({ ...feedCard, Source: { ...feedCard.Source, Tombstone: true } })} stats={{ state: 'loading' }} />);
    expect(removed).toContain('This callout was removed');
    expect(removed).not.toContain('Holding 1,200 tokens');
    const unsupported = render(<EmbeddedCoinCard poolAddress={pool} stats={{ state: 'unsupported', operation: 'IF_STATS_BATCH', status: 503, message: 'DEPENDENCY_UNAVAILABLE' }} positionDisclosure={null} positionVerified={null} />);
    expect(unsupported).toContain('Market cap is not available yet');
    expect(unsupported).toContain('Not shared');
    expect(unsupported).not.toContain('IF_STATS_BATCH');
    expect(unsupported).not.toMatch(BORDER);
  });
  it('renders the panel and rail headers with their selectors before data arrives', () => {
    const panel = render(<HotCalloutsPanel poolAddress={pool} />);
    expect(panel).toContain('Hot callouts');
    expect(panel).toContain('Loading callouts');
    expect(panel).toContain('aria-haspopup="menu"');
    expect(panel).not.toMatch(BORDER);
    const rail = render(<CalloutsRail />);
    expect(rail).toContain('Best callouts');
    expect(rail).toContain('role="radiogroup"');
    expect(rail).toContain('>1D<');
    expect(rail).toContain('>1W<');
    expect(rail).not.toMatch(BORDER);
    expect(render(<CalloutCardSkeleton />)).not.toMatch(BORDER);
  });
  it('renders an empty discovery feed as a plain empty state in the panel and the rail', () => {
    const emptyPage: FeedPage = { Algorithm: 'chronological-v1', Cursor: '', Entries: [], FallbackReason: '', Personalized: false, Snapshot: '' };
    const seed = (client: QueryClient) => { client.setQueryData(queryKeys.feed('relevance', null, 50), emptyPage); };
    for (const html of [render(<HotCalloutsPanel />, seed), render(<CalloutsRail />, seed)]) {
      expect(html).toContain('No callouts yet');
      expect(html).toContain('Callouts from traders show up here');
      expect(html).not.toMatch(/IF_CONNECTED_FEED|empty page|did not load|could not load|failed/i);
      expect(html).not.toMatch(BORDER);
    }
    expect(render(<CalloutsRail />, seed)).toContain('href="/explore"');
  });
  it('renders the follow button pending and the report kebab with an accessible name', () => {
    const text = render(<FollowButton wallet={author} handle="hdegroot" />);
    expect(text).toContain('aria-label="Follow hdegroot"');
    expect(text).toContain('aria-busy="true"');
    expect(text).toContain('aria-pressed="false"');
    const icon = render(<FollowButton wallet={author} variant="icon" />);
    expect(icon).toContain('rounded-pill');
    expect(render(<ReportMenu subject={{ targetType: 'profile', targetId: author, label: 'hdegroot' }} />)).toContain('aria-label="More actions for hdegroot"');
    expect(text + icon).not.toMatch(BORDER);
  });
});
