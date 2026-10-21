import { describe, it, expect } from 'vitest';
import type { V2ContractTypes } from '@kindle/sdk';
import { sparklineFromCandles, sparklineHours, sparklineWindow } from './use-sparkline';
import { canonicalBound, validateCandles } from './use-candles';

type Candle = V2ContractTypes['Candle'];
type Price = V2ContractTypes['Price'];
const pool = '0x' + 'a1'.repeat(20);
const position = { chainId: '125' as const, height: '10', transactionIndex: '0', logIndex: '0', transactionHash: '0x' + '1'.repeat(64), blockHash: '0x' + '2'.repeat(64) };
const asset = { address: null, chainId: '125', currencyCode: null, decimals: 6, kind: 'erc20' as const };
const price = (numerator: string): Price => ({ numerator, denominator: '1000000', baseAsset: asset, quoteAsset: asset, source: { amountBasis: 'effective_reserves', kind: 'post_swap_spot', observedAt: '2026-10-12T15:00:00.000Z', position: null, providerId: null, sourceEventId: null, sourceReference: null } });
const candle = (start: string, close: string, coverage: Candle['coverage'] = 'complete'): Candle => ({ poolAddress: pool, interval: '1h', start, open: price(close), high: price(close), low: price(close), close: price(close), coverage, firstPosition: position, lastPosition: position, buyVolumeUsdlRaw: '1', sellVolumeUsdlRaw: '1', volumeUsdlRaw: '2', volumeTokenRaw: '2', trades: '1', largeTradeCount: null, largeTradePolicyId: null, largeTradeThresholdUsdlRaw: null, revision: '1', seriesModelVersion: 'candles-v1', snapshotId: '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01', synthetic: false, uniqueTraderCount: null });

describe('sparkline window and closes', () => {
  it('aligns the 24h window to whole hours in canonical UTC millisecond form', () => {
    const window = sparklineWindow(Date.parse('2026-10-12T15:17:42.123Z'));
    expect(window).toEqual({ from: '2026-10-11T16:00:00.000Z', to: '2026-10-12T16:00:00.000Z' });
    expect((Date.parse(window.to) - Date.parse(window.from)) / 3_600_000).toBe(sparklineHours);
    expect(canonicalBound(window.from)).toBe(window.from);
    expect(() => canonicalBound('not a date')).toThrow(RangeError);
  });
  it('orders closes by bucket start and reports coverage honestly', () => {
    const items = [candle('2026-10-12T14:00:00.000Z', '2500000'), candle('2026-10-12T12:00:00.000Z', '1000000'), candle('2026-10-12T13:00:00.000Z', '1500000', 'partial')];
    const line = sparklineFromCandles(items, '2026-10-12T15:00:00.000Z');
    expect(line.closes).toEqual([1, 1.5, 2.5]);
    expect(line.starts).toEqual(['2026-10-12T12:00:00.000Z', '2026-10-12T13:00:00.000Z', '2026-10-12T14:00:00.000Z']);
    expect(line.coverage).toBe('partial');
    expect(sparklineFromCandles([], 'x').coverage).toBe('empty');
  });
  it('rejects candle pages that disagree with the requested pool, interval or series', () => {
    const base = { requestId: '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01', generatedAt: '2026-10-12T15:00:00.000Z', nextCursor: null, quality: 'partial' as const, revision: '1', watermark: null };
    const ok = { ...base, data: { items: [candle('2026-10-12T12:00:00.000Z', '1000000')], seriesModelVersion: 'candles-v1', snapshotId: 'x' } };
    expect(validateCandles(ok, pool, '1h')).toBe(ok);
    expect(() => validateCandles(ok, pool, '5m')).toThrow('Candle identity disagreement');
    expect(() => validateCandles({ ...ok, data: { ...ok.data, items: [...ok.data.items, ...ok.data.items] } }, pool, '1h')).toThrow('Duplicate candle bucket');
    expect(() => validateCandles({ ...ok, quality: 'quarantined' }, pool, '1h')).toThrow('Candle integrity quarantined');
  });
});
