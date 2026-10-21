import { describe, expect, it } from 'vitest';
import type { HoldersPage } from '@/core/market/use-holders';
import { coinTabFromSearch } from './coin-tabs';
import { buildHolderRows, matchesHolderSearch, sortHolderRows } from './holders-table';

const wallets = ['0x' + '1'.repeat(40), '0x' + '2'.repeat(40), '0x' + '3'.repeat(40)];
const position = { chainId: '125' as const, height: '20', transactionIndex: '1', logIndex: '0', transactionHash: '0x' + 'a'.repeat(64), blockHash: '0x' + 'b'.repeat(64) };
const watermark = { chainId: '125' as const, height: '20', blockHash: position.blockHash, projectionRevision: '1', integrity: 'healthy' as const, observedAt: '2026-01-01T00:00:00.000Z' };
const token = { decimals: 6, decimalsQuality: 'verified' as const, totalSupplyRaw: '1000000000000', supplyQuality: 'verified' as const };

function page(items: Array<{ wallet: string; balanceRaw: string; share: { numerator: string; denominator: string } | null }>, legacyShare: HoldersPage['legacyShare'] = null, nextCursor: string | null = null): HoldersPage {
  const response: HoldersPage['response'] = { data: { coverage: 'complete', excludedProtocolAddresses: [], exclusionPolicyVersion: '1', fromPosition: position, toPosition: position, items: items.map(item => ({ ...item, tokenAddress: '0x' + 'c'.repeat(40) })) }, generatedAt: watermark.observedAt, nextCursor, quality: 'complete', requestId: 'holders', revision: '1', watermark };
  return { response, rows: response!.data.items, canonical: null, legacyShare, legacySource: legacyShare ? 'stats-host' : null, nextCursor };
}

describe('coin tab selection from the query string', () => {
  it('accepts only the four known tabs and defaults to holders', () => {
    expect(coinTabFromSearch('?tab=trades')).toBe('trades');
    expect(coinTabFromSearch('?tab=about&side=buy')).toBe('about');
    expect(coinTabFromSearch('?tab=callouts')).toBe('callouts');
    expect(coinTabFromSearch('?tab=paired')).toBe('holders');
    expect(coinTabFromSearch('')).toBe('holders');
  });
});

describe('holder rows from real contract pages', () => {
  it('derives supply share from the response ratio, then the legacy basis points, then the verified total supply', () => {
    const rows = buildHolderRows([page([
      { wallet: wallets[0], balanceRaw: '250000000000', share: { numerator: '1', denominator: '4' } },
      { wallet: wallets[1], balanceRaw: '100000000000', share: null },
      { wallet: wallets[2], balanceRaw: '50000000000', share: null },
    ], { [wallets[1]]: { holderAddress: wallets[1], balance: '100000000000', pctOfSupply: '1000', rank: 2 } })], token, 2);
    const byWallet = Object.fromEntries(rows.map(row => [row.wallet, row]));
    expect(byWallet[wallets[0]].supplyPercent).toBe(25);
    expect(byWallet[wallets[1]].supplyPercent).toBe(10);
    expect(byWallet[wallets[2]].supplyPercent).toBe(5);
    expect(byWallet[wallets[0]].positionUsdlRaw).toBe(500000000000n);
  });
  it('leaves held and position unavailable without a verified token scale or a price', () => {
    const [row] = buildHolderRows([page([{ wallet: wallets[0], balanceRaw: '5', share: null }])], { ...token, decimalsQuality: 'unavailable', decimals: null, supplyQuality: 'unavailable', totalSupplyRaw: null }, 2);
    expect(row.supplyPercent).toBeNull();
    expect(row.positionUsdlRaw).toBeNull();
    const [priced] = buildHolderRows([page([{ wallet: wallets[0], balanceRaw: '5000000', share: null }])], token, null);
    expect(priced.positionUsdlRaw).toBeNull();
  });
  it('falls back to the legacy share rows when no transfer-coverage page answered', () => {
    const legacyOnly: HoldersPage = { response: null, rows: [], canonical: { state: 'unsupported', operation: 'IF_HOLDERS', status: 404, message: 'NOT_FOUND' }, legacyShare: { [wallets[2]]: { holderAddress: wallets[2], balance: '7000000', pctOfSupply: '50', rank: 1 } }, legacySource: 'stats-host', nextCursor: null };
    const rows = buildHolderRows([legacyOnly], token, 1);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ wallet: wallets[2], balanceRaw: '7000000', supplyPercent: 0.5, positionUsdlRaw: 7000000n });
  });
  it('deduplicates wallets across pages and sorts by held or supply share in either direction', () => {
    const rows = buildHolderRows([
      page([{ wallet: wallets[0], balanceRaw: '9', share: null }], null, 'next'),
      page([{ wallet: wallets[0], balanceRaw: '9', share: null }, { wallet: wallets[1], balanceRaw: '100', share: { numerator: '1', denominator: '2' } }]),
    ], token, null);
    expect(rows).toHaveLength(2);
    expect(sortHolderRows(rows, 'held', 'desc').map(row => row.balanceRaw)).toEqual(['100', '9']);
    expect(sortHolderRows(rows, 'held', 'asc').map(row => row.balanceRaw)).toEqual(['9', '100']);
    expect(sortHolderRows(rows, 'supply', 'desc')[0].wallet).toBe(wallets[1]);
    expect(sortHolderRows(rows, 'supply', 'asc')[0].wallet).toBe(wallets[0]);
  });
  it('matches wallet prefixes with or without 0x and display names case-insensitively', () => {
    const row = buildHolderRows([page([{ wallet: wallets[0], balanceRaw: '1', share: null }])], token, null)[0];
    expect(matchesHolderSearch(row, null, '0x11')).toBe(true);
    expect(matchesHolderSearch(row, null, '111')).toBe(true);
    expect(matchesHolderSearch(row, null, '0x22')).toBe(false);
    expect(matchesHolderSearch(row, 'Whale Watcher', 'whale')).toBe(true);
    expect(matchesHolderSearch(row, null, '   ')).toBe(true);
  });
});
