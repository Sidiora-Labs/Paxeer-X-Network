import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { decodeV2Contract } from '@kindle/sdk';
import { v2Reads } from '@/core/test-fixtures/market-production/v2-reads';
import { formatUsdl } from '@/utils/format';
import { formatShare, formatSpotPrice, marketCapRaw, noTradeMarketCapReason, noTradePriceReason, statsChange, statsChanges, statsCirculatingMarketCap, statsMarketCap, statsShare, statsSpotPrice, unverifiedSupplyReason } from './pool-stats-display';
import type { PoolStats } from './use-stats-batch';

/** The real chain-125 pool's 2.2.0 row as IF_STATS_BATCH served it (core-api-wire.json v2Reads.statsBatchReal). */
const real = decodeV2Contract('StatsBatchResponse', v2Reads.responses.statsBatchReal.body).data.items[0];
/** The same pool after the recorded follow-on buy, as the `market.stats.<pool>` live frame carried it (liveFrames). */
const wire = JSON.parse(readFileSync(new URL('../test-fixtures/market-production/core-api-wire.json', import.meta.url), 'utf8')) as { liveFrames: Array<{ topic?: string; payload?: unknown }> };
const live = decodeV2Contract('PoolStats', wire.liveFrames.find(frame => frame.topic?.startsWith('market.stats.'))?.payload);
/** The frozen pre-2.2.0 IF_STATS example row (no 2.2.0 fields at all). */
const legacy = decodeV2Contract('StatsBatchResponse', v2Reads.responses.statsBatch.body).data.items.find(row => row.poolAddress === v2Reads.pools.example) as PoolStats;

describe('PoolStats 2.2.0 display reads over the recorded bytes', () => {
  it('reads the real pool: price from spotPriceWad, total-supply market cap, circulating secondary, shares', () => {
    expect(statsSpotPrice(real)).toEqual({ value: '0.000010098226981692', reason: null });
    expect(formatSpotPrice(statsSpotPrice(real).value)).toBe('$0.0₄1009');
    expect(statsMarketCap(real)).toEqual({ value: '10098226981', reason: null });
    expect(formatUsdl(marketCapRaw(real), { compact: true })).toBe('10.1K USDL');
    expect(statsCirculatingMarketCap(real).value).toBe('48505893');
    expect(statsShare(real, 'top10')).toEqual({ value: 100, reason: null });
    expect(statsShare(real, 'creator')).toEqual({ value: 100, reason: null });
    expect(formatShare(100)).toBe('100%');
    expect(formatShare(0.3)).toBe('0.3%');
  });

  it('shows no change on an insufficient_history baseline and says why, for every window', () => {
    for (const [window, change] of Object.entries(statsChanges(real))) {
      expect(change.value, window).toBeNull();
      expect(change.reason, window).toMatch(/^Not enough trades yet for a \d+[mh] change$/);
    }
  });

  it('shows the live frame changes once their baselines are complete', () => {
    expect(statsChange(live, '5m')).toEqual({ value: 1.22, reason: null });
    expect(statsChange(live, '1h').value).toBe(1.22);
    expect(statsChange(live, '6h').value).toBe(1.22);
    expect(statsChange(live, '24h').value).toBe(1.22);
    expect(formatSpotPrice(statsSpotPrice(live).value)).toBe('$0.0₄1022');
    expect(statsMarketCap(live).value).toBe('10222435173');
  });

  it('keeps a pre-2.2.0 row explicit: not served, never zero', () => {
    expect(statsSpotPrice(legacy).reason).toBe('Price not available yet');
    expect(statsChange(legacy, '1h').reason).toBe('1h change not available yet');
    expect(statsChange(legacy, '24h').value).toBe(0.01);
    expect(statsShare(legacy, 'top10').reason).toBe('Holder shares not available yet');
  });

  it('reads a null spotPriceWad as no trade yet, and a null share as not computed', () => {
    const untraded: PoolStats = { ...real, spotPriceWad: null, totalSupplyValuationUsdlRaw: null, circulatingMarketCapUsdlRaw: null, supplyValuationQuality: 'unavailable', top10HolderShareBps: null };
    expect(statsSpotPrice(untraded)).toEqual({ value: null, reason: noTradePriceReason });
    expect(formatSpotPrice(statsSpotPrice(untraded).value)).toBe('—');
    expect(statsMarketCap(untraded)).toEqual({ value: null, reason: noTradeMarketCapReason });
    expect(statsMarketCap({ ...real, supplyValuationQuality: 'unavailable' }).reason).toBe(unverifiedSupplyReason);
    expect(statsShare(untraded, 'top10').reason).toBe('Holder shares not available yet');
    expect(statsChange({ ...live, changeBaselineQuality6h: 'unavailable' }, '6h').reason).toBe('6h change not available yet');
    expect(statsChange(null, '5m').reason).toBe('No stats for this coin yet');
  });
});
