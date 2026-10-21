// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import type { PoolStats } from '@/core/market/use-stats-batch';
import { buildExploreRow } from '@/widgets/explore/use-explore-rows';
import { clearRecentPools, parseRecentPools, recentPoolsLimit, recentPoolsStorageKey, recordRecentPool, removeRecentPool } from '../recent-pools';
import { formatRailPrice, railSocials, toRailPairRow, toRecentRow } from './rows';
import { terminalPoolPath } from '../sub-nav';
import { missingRowReason, railTimeframes, timeframeServed, unservedWindowReason, windowChange } from './timeframe';
import { matchesRailFilter } from './discovery-panel';
import { pageCount } from './recent-panel';

const pool = (n: number) => '0x' + n.toString(16).padStart(2, '0').repeat(20);
const stored = () => JSON.parse(window.localStorage.getItem(recentPoolsStorageKey) ?? '[]') as string[];
const now = Date.parse('2026-10-13T12:00:00.000Z');

const watermark: PoolStats['watermark'] = { blockHash: '0x' + '1'.repeat(64), chainId: '125', height: '100', integrity: 'healthy', observedAt: '2026-10-13T11:59:00.000Z', projectionRevision: '7' };
/** A 2.2.0 v2 stats row with complete 5m / 1h baselines (priceChangeBps5m / 1h). */
const stats: PoolStats = {
  poolAddress: pool(0xab), buys24h: '3', sells24h: '1', changeBaselineQuality: 'complete', circulatingMarketCapUsdlRaw: '1560250000', totalSupplyValuationUsdlRaw: '3120500000',
  holderCount: '17', indicativeTvlUsdlRaw: null, priceChangeBps24h: '-725', realUsdlBalanceRaw: '1000000',
  sourceUpperPosition: { blockHash: '0x' + '2'.repeat(64), chainId: '125', height: '100', logIndex: '0', transactionHash: '0x' + '3'.repeat(64), transactionIndex: '0' },
  supplyValuationQuality: 'verified', valuationChangeUsdlRaw: null, valuationMethod: null, volume24hRaw: '9280500000', watermark,
  windowEnd: '2026-10-13T12:00:00.000Z', windowStart: '2026-10-12T12:00:00.000Z',
  priceChangeBps5m: '325', priceChangeBps1h: '1250', changeBaselineQuality5m: 'complete', changeBaselineQuality1h: 'complete',
};
const listing = { poolAddress: pool(0xab), tokenAddress: pool(0xcd), creator: pool(0xef), createdAtMs: now - 42_000 };

describe('recent pools store', () => {
  beforeEach(() => { window.localStorage.clear(); clearRecentPools(); });

  it('prepends newest first, lowercases and dedupes by moving the pool to the front', () => {
    recordRecentPool(pool(1));
    recordRecentPool(pool(2).toUpperCase().replace('0X', '0x'));
    recordRecentPool(pool(1));
    expect(stored()).toEqual([pool(1), pool(2)]);
  });

  it('ignores invalid addresses and caps the list at 20', () => {
    recordRecentPool('0x1234');
    recordRecentPool('not an address');
    expect(window.localStorage.getItem(recentPoolsStorageKey)).toBeNull();
    for (let i = 1; i <= 25; i++) recordRecentPool(pool(i));
    expect(recentPoolsLimit).toBe(20);
    expect(stored()).toHaveLength(20);
    expect(stored()[0]).toBe(pool(25));
    expect(stored().at(-1)).toBe(pool(6));
  });

  it('removes one pool and clears all', () => {
    recordRecentPool(pool(1)); recordRecentPool(pool(2)); recordRecentPool(pool(3));
    removeRecentPool(pool(2).toUpperCase().replace('0X', '0x'));
    expect(stored()).toEqual([pool(3), pool(1)]);
    clearRecentPools();
    expect(stored()).toEqual([]);
  });

  it('parses stored values defensively', () => {
    expect(parseRecentPools(null)).toEqual([]);
    expect(parseRecentPools('{bad')).toEqual([]);
    expect(parseRecentPools(JSON.stringify({ a: 1 }))).toEqual([]);
    expect(parseRecentPools(JSON.stringify([pool(1), 7, 'x', pool(1).toUpperCase().replace('0X', '0x'), pool(2)]))).toEqual([pool(1), pool(2)]);
  });
});

describe('timeframe mapping', () => {
  it('maps 5M and 1H to the v2 window fields and refuses 1M and 30M', () => {
    expect(railTimeframes.map(t => t.label)).toEqual(['1M', '5M', '30M', '1H']);
    expect(timeframeServed('5m')).toBe(true);
    expect(timeframeServed('1h')).toBe(true);
    expect(timeframeServed('1m')).toBe(false);
    expect(timeframeServed('30m')).toBe(false);
    const row = buildExploreRow({ poolAddress: pool(0xab), rank: 1, stats, listing, metadata: null, now });
    expect(windowChange(row, '5m')).toEqual({ value: 3.25, reason: null });
    expect(windowChange(row, '1h')).toEqual({ value: 12.5, reason: null });
    expect(windowChange(row, '1m')).toEqual({ value: null, reason: unservedWindowReason('1m') });
    expect(windowChange(row, '30m')).toEqual({ value: null, reason: unservedWindowReason('30m') });
    expect(unservedWindowReason('30m')).toContain('A 30-minute change is not available');
    expect(unservedWindowReason('30m')).not.toMatch(/IF_|\/stats/);
  });

  it('never invents a value while the baseline is incomplete, the field is not served or the row is missing', () => {
    expect(windowChange(null, '1h')).toEqual({ value: null, reason: missingRowReason });
    const young = buildExploreRow({ poolAddress: pool(0xab), rank: 1, stats: { ...stats, changeBaselineQuality5m: 'insufficient_history', priceChangeBps5m: null }, listing, metadata: null, now });
    expect(windowChange(young, '5m')).toEqual({ value: null, reason: 'Not enough trades yet for a 5m change' });
    // A value on an incomplete baseline is never shown either.
    const partial = buildExploreRow({ poolAddress: pool(0xab), rank: 1, stats: { ...stats, changeBaselineQuality1h: 'unavailable' }, listing, metadata: null, now });
    expect(windowChange(partial, '1h')).toEqual({ value: null, reason: '1h change not available yet' });
    const older = buildExploreRow({ poolAddress: pool(0xab), rank: 1, stats: { ...stats, priceChangeBps5m: undefined, priceChangeBps1h: undefined, changeBaselineQuality5m: undefined, changeBaselineQuality1h: undefined }, listing, metadata: null, now });
    expect(windowChange(older, '5m')).toEqual({ value: null, reason: '5m change not available yet' });
    expect(windowChange(older, '1h').reason).toBe('1h change not available yet');
  });
});

describe('row mapping', () => {
  const explore = buildExploreRow({ poolAddress: pool(0xab), rank: 1, stats, listing, metadata: { token_address: pool(0xcd), name: 'Doge Troll', symbol: 'DOLL', images: { logo: 'https://cdn.example/doll.png' }, socials: { website: 'https://doll.example/', twitter: 'https://x.com/doll', telegram: 'javascript:alert(1)' } }, now });

  it('links pairs into the terminal and carries holders, age, stage and the selected window', () => {
    const row = toRailPairRow(explore, { token_address: pool(0xcd), socials: { website: 'https://doll.example/', twitter: 'https://x.com/doll', telegram: 'javascript:alert(1)' } }, '5m', ['https://doll.example']);
    expect(row.href).toBe(`/terminal/${pool(0xab)}`);
    expect(terminalPoolPath(pool(0xab).toUpperCase().replace('0X', '0x'))).toBe(`/terminal/${pool(0xab)}`);
    expect(row).toMatchObject({ symbol: 'DOLL', name: 'Doge Troll', holders: 17, stage: 'new', mcap: 3120.5, createdAtMs: now - 42_000, change: { value: 3.25, reason: null } });
    expect(row.socials).toEqual([{ kind: 'website', url: 'https://doll.example/' }]);
  });

  it('drops socials off the approved origin list', () => {
    expect(railSocials({ socials: { website: 'https://doll.example/' } }, [])).toEqual([]);
    expect(railSocials(null, ['https://doll.example'])).toEqual([]);
  });

  it('formats the recent row spot price decimal with subscript zeros', () => {
    const recent = toRecentRow(explore, '0.0000031245');
    expect(recent).toMatchObject({ href: `/terminal/${pool(0xab)}`, price: '0.0000031245', mcap: 3120.5 });
    expect(formatRailPrice(recent.price)).toBe('$0.0₅3124');
    expect(formatRailPrice(null)).toBe('—');
    expect(toRecentRow(explore, null).price).toBeNull();
    expect(toRecentRow(explore, null, 'No trade yet, so there is no price.')).toMatchObject({ price: null, priceReason: 'No trade yet, so there is no price.' });
  });

  it('filters by name, symbol or address and pages recent rows by five', () => {
    expect(matchesRailFilter({ name: 'Doge Troll', symbol: 'DOLL', poolAddress: pool(0xab) }, 'troll')).toBe(true);
    expect(matchesRailFilter({ name: 'Doge Troll', symbol: 'DOLL', poolAddress: pool(0xab) }, 'abab')).toBe(true);
    expect(matchesRailFilter({ name: 'Doge Troll', symbol: 'DOLL', poolAddress: pool(0xab) }, 'pepe')).toBe(false);
    expect(pageCount(0)).toBe(1);
    expect(pageCount(5)).toBe(1);
    expect(pageCount(6)).toBe(2);
    expect(pageCount(20)).toBe(4);
  });
});
