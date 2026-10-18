import { describe, expect, it } from 'vitest';
import type { PoolStats } from '@/core/market/use-stats-batch';
import { poolPath } from '@/core/app-foundation/navigation';
import { decodeV2Contract } from '@kindle/sdk';
import { v2Reads } from '@/core/test-fixtures/market-production/v2-reads';
import { buildExploreRow, stageFor } from './use-explore-rows';
import { changeReason } from '@/core/market/pool-stats-display';

const pool = '0x' + 'ab'.repeat(20);
const token = '0x' + 'cd'.repeat(20);
const now = Date.parse('2026-10-12T15:00:00.000Z');

const watermark: PoolStats['watermark'] = { blockHash: '0x' + '1'.repeat(64), chainId: '125', height: '100', integrity: 'healthy', observedAt: '2026-10-12T14:59:00.000Z', projectionRevision: '7' };
const stats: PoolStats = {
  poolAddress: pool,
  buys24h: '120',
  sells24h: '80',
  changeBaselineQuality: 'complete',
  circulatingMarketCapUsdlRaw: '602500000000',
  totalSupplyValuationUsdlRaw: '700000000000',
  holderCount: '415',
  indicativeTvlUsdlRaw: null,
  priceChangeBps24h: '1234',
  realUsdlBalanceRaw: '1000000',
  sourceUpperPosition: { blockHash: '0x' + '2'.repeat(64), chainId: '125', height: '100', logIndex: '0', transactionHash: '0x' + '3'.repeat(64), transactionIndex: '0' },
  supplyValuationQuality: 'verified',
  valuationChangeUsdlRaw: null,
  valuationMethod: null,
  volume24hRaw: '1300000000000',
  watermark,
  windowEnd: '2026-10-12T15:00:00.000Z',
  windowStart: '2026-10-11T15:00:00.000Z',
};
/** The same row with the interface 2.2.0 fields: complete 5m / 1h baselines, an insufficient 6h one. */
const served: PoolStats = {
  ...stats,
  priceChangeBps5m: '-40', priceChangeBps1h: '250', priceChangeBps6h: null,
  changeBaselineQuality5m: 'complete', changeBaselineQuality1h: 'complete', changeBaselineQuality6h: 'insufficient_history',
  spotPriceWad: '10000000000000', spotPriceUsdlRaw: '10',
  uniqueTraders24h: '7833', txCount24h: '205', top10HolderShareBps: 2500, creatorHoldingShareBps: 100,
};
const listing = { poolAddress: pool, tokenAddress: token, creator: '0x' + 'ef'.repeat(20), createdAtMs: now - 20 * 60 * 1000 };

describe('buildExploreRow', () => {
  it('maps the v2 stats and listing, derives the stage from the real age and leaves unserved fields null', () => {
    const row = buildExploreRow({ poolAddress: pool.toUpperCase().replace('0X', '0x'), rank: 4, stats, listing, metadata: { token_address: token, name: 'Science Inu', symbol: 'SCINU', images: { logo: 'https://cdn.example/logo.png' } }, now });
    expect(row.poolAddress).toBe(pool);
    expect(row.tokenAddress).toBe(token);
    expect(row.name).toBe('Science Inu');
    expect(row.symbol).toBe('SCINU');
    expect(row.image).toBe('https://cdn.example/logo.png');
    expect(row.href).toBe(poolPath(125, pool));
    expect(row.href).toMatch(new RegExp(`/${pool}$`));
    // Market cap is the total-supply valuation (pump.fun's figure); the circulating one is the secondary value.
    expect(row.mcapRaw).toBe('700000000000');
    expect(row.mcap).toBe(700000);
    expect(row.circulatingMcapRaw).toBe('602500000000');
    expect(row.volume24hRaw).toBe('1300000000000');
    expect(row.volume24h).toBe(1300000);
    expect(row.txns).toBe(200);
    expect(row.holders).toBe(415);
    expect(row.change24h).toBe(12.34);
    expect(row.ageMs).toBe(20 * 60 * 1000);
    expect(row.stage).toBe('new');
    expect(row).toMatchObject({ traders: null, price: null, change5m: null, change1h: null, change6h: null, top10Share: null, devShare: null });
    expect(row.priceReason).toBe('Price not available yet');
    expect(row.changes['1h'].reason).toBe(changeReason('1h', undefined));
  });
  it('reads the 2.2.0 PoolStats fields, keeping a change without a complete baseline empty with its reason', () => {
    const row = buildExploreRow({ poolAddress: pool, rank: 2, stats: served, listing, metadata: null, now });
    expect(row).toMatchObject({ traders: 7833, txns: 205, price: '0.00001', change5m: -0.4, change1h: 2.5, change6h: null, top10Share: 25, devShare: 1 });
    expect(row.changes['6h']).toEqual({ value: null, reason: changeReason('6h', 'insufficient_history') });
  });
  it('builds the real pool row from the recorded IF_STATS_BATCH bytes', () => {
    const real = decodeV2Contract('StatsBatchResponse', v2Reads.responses.statsBatchReal.body).data.items[0];
    const row = buildExploreRow({ poolAddress: real.poolAddress, rank: 1, stats: real, metadata: null, now });
    expect(row).toMatchObject({ mcapRaw: '10098226981', mcap: 10098.226981, circulatingMcapRaw: '48505893', price: '0.000010098226981692', priceReason: null, traders: 1, txns: 2, holders: 2, top10Share: 100, devShare: 100, change5m: null, change1h: null, change6h: null, change24h: null });
    expect(row.changes['24h'].reason).toBe(changeReason('24h', 'insufficient_history'));
  });
  it('never fills an unverified market cap or an incomplete 24h baseline from another source', () => {
    const row = buildExploreRow({ poolAddress: pool, rank: 1, stats: { ...stats, changeBaselineQuality: 'insufficient_history', circulatingMarketCapUsdlRaw: null, totalSupplyValuationUsdlRaw: null }, listing, metadata: null, now });
    expect(row.change24h).toBeNull();
    expect(row.mcapRaw).toBeNull();
    expect(row.mcap).toBeNull();
    expect(row.name).toBe('0xababab...abab');
    expect(row.symbol).toBe('0xcdcd...cdcd');
    expect(row.image).toBeNull();
  });
  it('leaves every enrichment null when no source has it', () => {
    const row = buildExploreRow({ poolAddress: pool, rank: 9, stats: null, metadata: null, now });
    expect(row).toMatchObject({ rank: 9, tokenAddress: null, mcapRaw: null, mcap: null, volume24h: null, createdAtMs: null, ageMs: null, txns: null, traders: null, holders: null, price: null, change1h: null, change24h: null, stage: null });
  });
});

describe('stageFor', () => {
  it('only claims the new stage for pools under a day old', () => {
    expect(stageFor(23 * 3_600_000)).toBe('new');
    expect(stageFor(25 * 3_600_000)).toBeNull();
    expect(stageFor(null)).toBeNull();
  });
});
