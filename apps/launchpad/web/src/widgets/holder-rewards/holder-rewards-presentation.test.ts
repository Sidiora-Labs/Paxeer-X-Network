import { describe, expect, it } from 'vitest';
import type { TokenMetadata } from '@/widgets/home/types';
import type { HolderRewardPool, PoolStats } from './holder-rewards-contract';
import { activeFilterCount, approvedSocialLinks, buildRows, coinHref, defaultFilters, defaultOrder, explorerHref, filterRows, holdersPaidText, mcapText, nextOrder, rewardsText, rowsToCsv, sortRows, usdlAmountText } from './holder-rewards-presentation';

const poolA = '0x' + 'a1'.repeat(20);
const poolB = '0x' + 'b2'.repeat(20);
const poolC = '0x' + 'c3'.repeat(20);
const tokenA = '0x' + 'd4'.repeat(20);
const tokenB = '0x' + 'e5'.repeat(20);
const tokenC = '0x' + 'f6'.repeat(20);

const items: HolderRewardPool[] = [
  { poolAddress: poolA.toUpperCase().replace('0X', '0x'), tokenAddress: tokenA, distributedRaw: '504967000000', holdersPaid: 14648, strategyActive: true, createdAt: '2026-09-14T10:00:00.000Z' },
  { poolAddress: poolB, tokenAddress: tokenB, distributedRaw: '498048000000', holdersPaid: 26093, strategyActive: true, createdAt: '2026-09-01T10:00:00.000Z' },
  { poolAddress: poolC, tokenAddress: tokenC, distributedRaw: '229470000000', holdersPaid: 671, strategyActive: false, createdAt: '2026-10-01T10:00:00.000Z' },
];

const position = { chainId: '125' as const, height: '4200', transactionIndex: '1', logIndex: '0', transactionHash: '0x' + '5'.repeat(64), blockHash: '0x' + '6'.repeat(64) };
const watermark = { chainId: '125' as const, height: '4200', blockHash: position.blockHash, projectionRevision: '7', integrity: 'healthy' as const, observedAt: '2026-10-12T15:00:00.000Z' };
const statsFor = (poolAddress: string, mcap: string | null): PoolStats => ({
  poolAddress, buys24h: '40', sells24h: '15', changeBaselineQuality: 'complete', circulatingMarketCapUsdlRaw: mcap, holderCount: '310', indicativeTvlUsdlRaw: '500000000000', priceChangeBps24h: '-450',
  realUsdlBalanceRaw: '250000000000', sourceUpperPosition: position, supplyValuationQuality: 'verified', totalSupplyValuationUsdlRaw: mcap, valuationChangeUsdlRaw: '0', valuationMethod: 'reserves', volume24hRaw: '12345670000', watermark, windowEnd: '2026-10-12T15:00:00.000Z', windowStart: '2026-10-11T15:00:00.000Z',
});

const stats = new Map<string, PoolStats>([[poolA, statsFor(poolA, '1970000000000')], [poolB, statsFor(poolB, '16400000000000')]]);
const metadata: Record<string, TokenMetadata | null> = {
  [tokenA]: { token_address: tokenA, name: 'Pill', symbol: 'PILL', images: { logo: 'https://ipfs.io/ipfs/abc' }, socials: { website: 'https://example.com', twitter: 'https://x.com/pill' } },
  [tokenB]: { token_address: tokenB, name: '  ', symbol: 'BATON', images: {} },
  [tokenC]: null,
};

describe('holder rewards rows', () => {
  const rows = buildRows(items, stats, metadata);

  it('joins contract rows with live market cap and identity, falling back honestly', () => {
    expect(rows.map(row => row.poolAddress)).toEqual([poolA, poolB, poolC]);
    expect(rows[0]).toMatchObject({ name: 'Pill', symbol: 'PILL', image: 'https://ipfs.io/ipfs/abc', mcapRaw: '1970000000000' });
    expect(rows[1]).toMatchObject({ name: 'BATON', symbol: 'BATON', image: null, mcapRaw: '16400000000000' });
    expect(rows[2]).toMatchObject({ name: '0xf6f6...f6f6', symbol: '', image: null, mcapRaw: null, stats: null });
  });

  it('orders by every key in both directions with unknown market caps last', () => {
    expect(sortRows(rows, defaultOrder).map(row => row.poolAddress)).toEqual([poolA, poolB, poolC]);
    expect(sortRows(rows, { key: 'rewards', direction: 'asc' }).map(row => row.poolAddress)).toEqual([poolC, poolB, poolA]);
    expect(sortRows(rows, { key: 'mcap', direction: 'desc' }).map(row => row.poolAddress)).toEqual([poolB, poolA, poolC]);
    expect(sortRows(rows, { key: 'mcap', direction: 'asc' }).map(row => row.poolAddress)).toEqual([poolA, poolB, poolC]);
    expect(sortRows(rows, { key: 'holders', direction: 'desc' }).map(row => row.poolAddress)).toEqual([poolB, poolA, poolC]);
    expect(sortRows(rows, { key: 'age', direction: 'desc' }).map(row => row.poolAddress)).toEqual([poolC, poolA, poolB]);
    expect(sortRows(rows, { key: 'age', direction: 'asc' }).map(row => row.poolAddress)).toEqual([poolB, poolA, poolC]);
  });

  it('cycles a column header desc, asc, then back to the default order', () => {
    expect(nextOrder(defaultOrder, 'mcap')).toEqual({ key: 'mcap', direction: 'desc' });
    expect(nextOrder({ key: 'mcap', direction: 'desc' }, 'mcap')).toEqual({ key: 'mcap', direction: 'asc' });
    expect(nextOrder({ key: 'mcap', direction: 'asc' }, 'mcap')).toEqual(defaultOrder);
    expect(nextOrder(defaultOrder, 'rewards')).toEqual({ key: 'rewards', direction: 'asc' });
  });

  it('formats amounts in whole USDL, compact market caps and routes rows to the coin page', () => {
    expect(rewardsText('504967000000')).toBe('504,967 USDL');
    expect(rewardsText('120000')).toBe('< 1 USDL');
    expect(rewardsText('999999')).toBe('< 1 USDL');
    expect(rewardsText('1000000')).toBe('1 USDL');
    expect(rewardsText('0')).toBe('0 USDL');
    expect(rewardsText(null)).toBe('—');
    expect(mcapText('1970000000000')).toBe('1.97M');
    expect(mcapText('16400000000000')).toBe('16.4M');
    expect(coinHref(poolA.toUpperCase().replace('0X', '0x'))).toBe(`/coin/${poolA}`);
    expect(explorerHref(tokenA)).toBe(`https://paxscan.paxeer.app/address/${tokenA}`);
  });

  it('exposes only social destinations the shell link guard admits', () => {
    expect(approvedSocialLinks(metadata[tokenA])).toEqual([{ kind: 'twitter', href: 'https://x.com/pill' }]);
    expect(approvedSocialLinks({ socials: { telegram: 'https://t.me/Sidiorafunsupport' } })).toEqual([{ kind: 'telegram', href: 'https://t.me/Sidiorafunsupport' }]);
    expect(approvedSocialLinks(null)).toEqual([]);
  });
});

describe('holder rewards filters and export', () => {
  const rows = buildRows(items, stats, metadata);

  it('filters loaded rows without inventing any', () => {
    expect(filterRows(rows, defaultFilters)).toHaveLength(3);
    expect(filterRows(rows, { activeOnly: true, pricedOnly: false }).map(row => row.poolAddress)).toEqual([poolA, poolB]);
    expect(filterRows(rows, { activeOnly: false, pricedOnly: true }).map(row => row.poolAddress)).toEqual([poolA, poolB]);
    expect(activeFilterCount({ activeOnly: true, pricedOnly: true })).toBe(2);
  });

  it('exports exactly the shown rows as CSV with exact USDL decimals and neutralised formulas', () => {
    const tricky = buildRows([{ ...items[2] }], new Map(), { [tokenC]: { token_address: tokenC, name: '=HYPERLINK("x")', symbol: 'A,B' } });
    const csv = rowsToCsv([rows[0], ...tricky]);
    const lines = csv.trimEnd().split('\n');
    expect(lines[0]).toBe('rank,name,symbol,token_address,pool_address,holder_rewards_usdl,market_cap_usdl,holders_paid,strategy_active,created_at');
    expect(lines[1]).toBe(`1,Pill,PILL,${tokenA},${poolA},504967.000000,1970000.000000,14648,true,2026-09-14T10:00:00.000Z`);
    expect(lines[2]).toBe(`2,"'=HYPERLINK(""x"")","A,B",${tokenC},${poolC},229470.000000,,671,false,2026-10-01T10:00:00.000Z`);
    expect(rowsToCsv([])).toBe('rank,name,symbol,token_address,pool_address,holder_rewards_usdl,market_cap_usdl,holders_paid,strategy_active,created_at\n');
  });

  it('splits the unit off display amounts', () => {
    expect(usdlAmountText('18627546000000')).toBe('18,627,546');
    expect(usdlAmountText('5')).toBe('< 1');
    expect(usdlAmountText(null)).toBe('—');
    expect(holdersPaidText(14726)).toBe('14,726');
    expect(holdersPaidText(null)).toBe('—');
  });
});
