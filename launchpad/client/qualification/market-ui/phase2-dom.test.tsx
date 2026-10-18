// @vitest-environment jsdom
import { beforeAll, describe, expect, it } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { observeMarketJourney, observeInterruptedMarketJourney, type MarketJourneyInput } from './phase2-service-journey';
import { mergeTradeHistory, appendRankingPage, tradeKey } from '../../apps/web/src/page-modules/token/market-session';
import { holderRows, freshness } from '../../apps/web/src/page-modules/token/market-presentation';
import { MarketHolders } from '../../apps/web/src/page-modules/token/market-metrics';
import { MarketTradeFeed } from '../../apps/web/src/page-modules/token/market-trade-feed';
import { MarketIdentityCard } from '../../apps/web/src/page-modules/token/market-identity-card';
import type { RankingsRequest } from '../../packages/production-contracts/src/types.gen';
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
function required(name: string): string { const value = process.env[name]; if (!value) throw new Error(`${name} must identify an actual owned fixture service`); return value; }
function category(name: string): RankingsRequest['category'] { const value = required(name); if (!['trending', 'breakout', 'volume', 'gainers', 'losers', 'movers', 'unusual', 'new'].includes(value)) throw new Error('Invalid fixture category'); return value as RankingsRequest['category']; }
let observed: Awaited<ReturnType<typeof observeMarketJourney>>;
let journeyInput: MarketJourneyInput;
beforeAll(async () => {
  const streamOrigin = required('KINDLE_MARKET_STREAM_ORIGIN');
  journeyInput = { transport: { apiOrigin: required('KINDLE_MARKET_API_ORIGIN'), identityOrigin: required('KINDLE_MARKET_IDENTITY_ORIGIN'), mediaOrigin: required('KINDLE_MARKET_MEDIA_ORIGIN'), streamOrigin, approvedStreamOrigins: [streamOrigin], production: false }, poolAddress: required('KINDLE_MARKET_POOL'), rankingCategory: category('KINDLE_MARKET_RANKING_CATEGORY'), emptyCategory: category('KINDLE_MARKET_EMPTY_CATEGORY') };
  observed = await observeMarketJourney(journeyInput);
}, 30_000);
describe('actual integrated market service journeys', () => {
  it('recovers real hidden-tab interruption, dropped sequence and revised statistics through snapshot/replay', async () => {
    const interrupted = await observeInterruptedMarketJourney(journeyInput);
    expect(interrupted.gapObserved).toBe(true);
    expect(interrupted.resumed.stream).toBe('live');
    expect(interrupted.resumed.rows.length).toBeLessThanOrEqual(1000);
    expect(interrupted.resumed.statsGeneratedAt).not.toBe(interrupted.before.statsGeneratedAt);
  }, 100_000);
  it('merges older cursor history, out-of-arrival fills and duplicate replay by full chain order', () => {
    const rows = [...observed.head.data.items, ...observed.older.data.items];
    expect(rows.length).toBeGreaterThanOrEqual(3);
    const merged = mergeTradeHistory(observed.older.data.items, [...observed.head.data.items].reverse());
    const replayed = mergeTradeHistory(merged, observed.head.data.items);
    expect(replayed.map(tradeKey)).toEqual(merged.map(tradeKey));
    expect(new Set(replayed.map(tradeKey)).size).toBe(replayed.length);
    const tuples = replayed.map(row => [BigInt(row.position.height), BigInt(row.position.transactionIndex), BigInt(row.position.logIndex)]);
    for (let i = 1; i < tuples.length; i++) {
      const before = tuples[i - 1], after = tuples[i];
      const firstDifference = before.findIndex((value, field) => value !== after[field]);
      expect(firstDifference).toBeGreaterThanOrEqual(0);
      expect(before[firstDifference] > after[firstDifference]).toBe(true);
    }
    expect(rows.every(row => row.position.chainId === '125')).toBe(true);
  });
  it('retains numeric100-before9 order and partial transfer coverage in actual DOM', async () => {
    expect(observed.holders.data.coverage).toBe('partial');
    const rows = holderRows(observed.holders);
    expect(rows.some(row => row.balanceRaw === '100')).toBe(true);
    expect(rows.some(row => row.balanceRaw === '9')).toBe(true);
    expect(rows.findIndex(row => row.balanceRaw === '100')).toBeLessThan(rows.findIndex(row => row.balanceRaw === '9'));
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
    try { await act(async () => root.render(<MarketHolders response={observed.holders} token={observed.detail.data.token} />)); expect(host.textContent).toContain('Coverage: partial'); expect(host.textContent).toContain('Incomplete coverage'); expect(host.querySelectorAll('tbody tr').length).toBeLessThanOrEqual(100); }
    finally { await act(async () => root.unmount()); host.remove(); }
  });
  it('keeps ranking cursor snapshot identity and distinguishes real empty/error/stale inputs', () => {
    expect(observed.empty.data.snapshot.items).toHaveLength(0);
    expect(observed.rejectedCursorStatus).toBe(400);
    const merged = appendRankingPage(observed.rankings, observed.rankingPage);
    expect(merged.data.snapshot.snapshotId).toBe(observed.rankings.data.snapshot.snapshotId);
    const watermark = observed.detail.watermark;
    expect(watermark).not.toBeNull();
    const reference = Math.max(Date.parse(observed.detail.generatedAt), Date.parse(watermark!.observedAt));
    expect(freshness({ now: reference + 31_000, generatedAt: observed.detail.generatedAt, heartbeatAt: reference, contiguous: true, watermark })).toBe('stale');
    expect(freshness({ now: reference, generatedAt: observed.detail.generatedAt, heartbeatAt: reference, contiguous: false, watermark })).toBe('reconnecting');
  });
  it('mounts canonical identity and bounded real trade rows without embedded metadata HTML', async () => {
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
    try { await act(async () => root.render(<><MarketIdentityCard {...observed.detail.data} /><MarketTradeFeed pool={observed.detail.data.pool} token={observed.detail.data.token} rows={mergeTradeHistory(observed.head.data.items, observed.older.data.items)} watermark={observed.head.watermark} contiguous={false} /></>)); expect(host.querySelector('script')).toBeNull(); expect(host.textContent).toContain('Chain 125'); expect(host.textContent).toContain('1 USDL'); expect(host.textContent).toContain('native PAX gas'); expect(host.textContent).toContain('Catching up'); expect(host.querySelectorAll('tbody tr').length).toBeLessThanOrEqual(102); }
    finally { await act(async () => root.unmount()); host.remove(); }
  });
});
