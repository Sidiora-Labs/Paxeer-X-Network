// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { V2ContractTypes } from '@kindle/sdk';
import type { PoolListing } from '@/core/market/pool-listings';
import type { PortfolioPosition } from '@/widgets/profile/portfolio-transport';
import type { SearchResult } from '../../../../../../packages/production-contracts/src/types.gen';
import { applyPresetUpdate, defaultPresets, parsePresets, presetsStorageKey, useTradePresets } from '../presets-store';
import { positionFigures } from './position-summary';
import { validateDraft } from './preset-editor';
import { useInstantTrade } from './quick-trade-card';
import { instantTradeEvent, instantTradeStorageKey, parseInstantFlag, useInstantTrade as useInstantTradeToggle } from '../activity-tabs/instant-trade';
import { similarPools, similarRow, sortSimilar } from './similar-tokens';
import { parseAccordions } from './accordion';

declare global { var IS_REACT_ACT_ENVIRONMENT: boolean | undefined; }
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

type PoolStats = V2ContractTypes['PoolStats'];
const current = '0x' + 'aa'.repeat(20);
const poolB = '0x' + 'bb'.repeat(20);
const poolC = '0x' + 'cc'.repeat(20);
const tokenB = '0x' + 'b1'.repeat(20);
const wallet = '0x' + '77'.repeat(20);
const now = Date.parse('2026-10-13T12:00:00.000Z');

function mountHook<T>(hook: () => T): { read: () => T; unmount: () => void } {
  let value: T;
  function Probe() { value = hook(); return null; }
  const container = document.createElement('div');
  const root = createRoot(container);
  act(() => { root.render(React.createElement(Probe)); });
  return { read: () => value, unmount: () => act(() => root.unmount()) };
}

describe('trade presets store', () => {
  beforeEach(() => { window.localStorage.clear(); });

  it('parses persisted presets field by field and falls back to defaults', () => {
    expect(parsePresets(null)).toEqual(defaultPresets());
    expect(parsePresets('{bad')).toEqual(defaultPresets());
    const parsed = parsePresets(JSON.stringify({ active: 2, presets: { 2: { buyAmounts: ['1', '2', '3', '4'], sellPercents: [5, 500, 7, 8], slippageBps: 0 } } }));
    expect(parsed.active).toBe(2);
    expect(parsed.presets[2].buyAmounts).toEqual(['1', '2', '3', '4']);
    expect(parsed.presets[2].sellPercents).toEqual(defaultPresets().presets[2].sellPercents);
    expect(parsed.presets[2].slippageBps).toBe(defaultPresets().presets[2].slippageBps);
  });

  it('rejects invalid edits and keeps the previous state', () => {
    const base = defaultPresets();
    expect(applyPresetUpdate(base, 1, { slippageBps: 9000 })).toBe(base);
    expect(applyPresetUpdate(base, 1, { buyAmounts: ['1', '0', '2', '3'] })).toBe(base);
    expect(applyPresetUpdate(base, 3, { slippageBps: 250 }).presets[3].slippageBps).toBe(250);
  });

  it('persists the active preset and edits across hook instances', () => {
    const first = mountHook(() => useTradePresets());
    expect(first.read().active).toBe(1);
    act(() => { first.read().setActive(3); });
    act(() => { first.read().updatePreset(3, { buyAmounts: ['5', '10', '15', '20.5'] }); });
    expect(first.read().active).toBe(3);
    const stored = JSON.parse(window.localStorage.getItem(presetsStorageKey) ?? '{}');
    expect(stored.active).toBe(3);
    expect(stored.presets['3'].buyAmounts).toEqual(['5', '10', '15', '20.5']);
    first.unmount();
    const second = mountHook(() => useTradePresets());
    expect(second.read().presets[3].buyAmounts).toEqual(['5', '10', '15', '20.5']);
    second.unmount();
  });
});

describe('preset editor validation', () => {
  it('returns field errors and only yields a preset when everything is valid', () => {
    const bad = validateDraft({ buyAmounts: ['1', 'x', '2', '3'], sellPercents: ['10', '0', '50', '100'], slippageBps: '6000' });
    expect(bad.preset).toBeNull();
    expect(bad.errors.buy[1]).not.toBeNull();
    expect(bad.errors.sell[1]).not.toBeNull();
    expect(bad.errors.slippage).not.toBeNull();
    const good = validateDraft({ buyAmounts: ['1', '2.5', '10', '100'], sellPercents: ['10', '25', '50', '100'], slippageBps: '150' });
    expect(good.preset).toEqual({ buyAmounts: ['1', '2.5', '10', '100'], sellPercents: [10, 25, 50, 100], slippageBps: 150 });
  });
});

describe('instant trade event', () => {
  afterEach(() => { window.localStorage.clear(); });
  it('reads the persisted flag and follows the window event', () => {
    expect(parseInstantFlag('true')).toBe(true);
    expect(parseInstantFlag('false')).toBe(false);
    window.localStorage.setItem('sidiora-terminal-instant-trade', 'true');
    const probe = mountHook(() => useInstantTrade());
    expect(probe.read()).toBe(true);
    act(() => { window.dispatchEvent(new CustomEvent(instantTradeEvent, { detail: { enabled: false } })); });
    expect(probe.read()).toBe(false);
    act(() => { window.dispatchEvent(new CustomEvent(instantTradeEvent, { detail: 'garbage' })); });
    expect(probe.read()).toBe(false);
    probe.unmount();
  });

  it('agrees with the activity-tabs toggle on the stored strings and the event detail', () => {
    const toggle = mountHook(() => useInstantTradeToggle());
    const rail = mountHook(() => useInstantTrade());
    expect(rail.read()).toBe(false);
    act(() => { toggle.read()[1](true); });
    expect(window.localStorage.getItem(instantTradeStorageKey)).toBe('true');
    expect(rail.read()).toBe(true);
    act(() => { toggle.read()[1](false); });
    expect(window.localStorage.getItem(instantTradeStorageKey)).toBe('false');
    expect(rail.read()).toBe(false);
    act(() => { toggle.read()[1](true); });
    rail.unmount();
    const remounted = mountHook(() => useInstantTrade());
    expect(remounted.read()).toBe(true);
    remounted.unmount();
    toggle.unmount();
  });
});

describe('accordion state', () => {
  it('keeps defaults for missing or malformed fields', () => {
    expect(parseAccordions(null)).toEqual({ trade: true, security: false, similar: true });
    expect(parseAccordions(JSON.stringify({ security: true, similar: 'x' }))).toEqual({ trade: true, security: true, similar: true });
  });
});

describe('position figures', () => {
  // Shapes follow core/pnl-tracker/internal/production/l17/domain.go (Position, Price, Watermark) after Derive.
  const watermark = { blockHash: '0x' + '1'.repeat(64), chainId: '125', height: '10', integrity: 'healthy', observedAt: '2026-10-13T12:00:00.000Z', projectionRevision: '1' };
  const mark: NonNullable<PortfolioPosition['markPrice']> = {
    numerator: '3', denominator: '1',
    baseAsset: { kind: 'erc20', chainId: '125', address: tokenB, currencyCode: null, decimals: 0 },
    quoteAsset: { kind: 'erc20', chainId: '125', address: '0x85FcD13735F4309833A503EE804ea32395851479', currencyCode: 'USDL', decimals: 6 },
    source: { kind: 'post_swap_spot', amountBasis: 'effective_reserves', sourceEventId: 'price-1', observedAt: '2026-10-13T12:00:00.000Z', providerId: null, sourceReference: null, position: { chainId: '125', height: '10', transactionIndex: '0', logIndex: '1', blockHash: watermark.blockHash, transactionHash: '0x' + '2'.repeat(64) } },
  };
  const position: PortfolioPosition = {
    balanceRaw: '1000', basisCoverage: { numerator: '1000', denominator: '1000' }, basisQuality: 'complete', chainId: '125', knownQuantityRaw: '1000', knownRealizedSubtotalUsdlRaw: '1500000',
    knownRemainingCostUsdlRaw: '4000000', markPrice: mark, positionId: `125:${wallet}:${tokenB}`, pricingQuality: 'priced', realizedBasisQuality: 'complete', realizedPnlRaw: '1500000', remainingCostUsdlRaw: '4000000',
    sourceWatermark: watermark, tokenAddress: tokenB, unknownQuantityRaw: '0', unrealizedPnlRaw: '-500000', wallet,
  };

  it('maps served fields only', () => {
    const figures = positionFigures(position, 'no position');
    expect(figures.value).toMatchObject({ raw: 3_000_000_000n, partial: false });
    expect(figures.remaining).toMatchObject({ raw: 4_000_000n, partial: false });
    expect(figures.realized).toMatchObject({ raw: 1_500_000n, partial: false });
    expect(figures.totalPnl).toMatchObject({ raw: 1_000_000n, partial: false });
  });

  it('marks partial halves and dashes what is not served', () => {
    const unpriced = positionFigures({ ...position, markPrice: null, pricingQuality: 'unpriced', remainingCostUsdlRaw: null, basisQuality: 'partial', unrealizedPnlRaw: null }, 'no position');
    expect(unpriced.value).toMatchObject({ raw: null, note: 'No price yet' });
    expect(unpriced.remaining).toMatchObject({ raw: 4_000_000n, partial: true });
    expect(unpriced.totalPnl).toMatchObject({ raw: 1_500_000n, partial: true });
    const withheld = positionFigures({ ...position, realizedPnlRaw: null, realizedBasisQuality: 'partial', unrealizedPnlRaw: null }, 'no position');
    expect(withheld.realized).toMatchObject({ raw: 1_500_000n, partial: true });
    expect(withheld.totalPnl.raw).toBeNull();
    expect(positionFigures(null, 'no position').remaining).toMatchObject({ raw: null, note: 'no position' });
  });
});

describe('similar tokens mapping', () => {
  const result = (pool: string | null, kind: SearchResult['kind'] = 'pool', token: string | null = null, label = 'Doll'): SearchResult => ({ id: `${kind}:${pool}`, kind, label, poolAddress: pool, score: '1', tokenAddress: token, wallet: null });

  it('drops the open pool, non-pool hits and duplicates, keeping served order', () => {
    const hits = similarPools([result(current), result(poolB.toUpperCase().replace('0X', '0x'), 'pool', tokenB), result(null, 'token'), result(poolB), result(poolC)], current);
    expect(hits.map(hit => hit.poolAddress)).toEqual([poolB, poolC]);
    expect(hits[0].tokenAddress).toBe(tokenB);
  });

  it('maps stats, the pool listing and metadata onto a row with terminal links', () => {
    const stats = { poolAddress: poolB, realUsdlBalanceRaw: '62000000000', volume24hRaw: '5000000', circulatingMarketCapUsdlRaw: '14000000000', totalSupplyValuationUsdlRaw: '28000000000', supplyValuationQuality: 'verified', buys24h: '1', sells24h: '1', holderCount: null, changeBaselineQuality: 'unavailable', priceChangeBps24h: null } as unknown as PoolStats;
    const listing: PoolListing = { poolAddress: poolB, tokenAddress: tokenB, creator: current, createdAtMs: now - 3_600_000 };
    const row = similarRow({ poolAddress: poolB, tokenAddress: tokenB, label: 'Miffy Doll' }, stats, listing, { symbol: 'MIFFY', name: 'Miffy Doll', images: { logo: 'https://cdn.example/m.png' } }, now);
    expect(row).toMatchObject({ poolAddress: poolB, symbol: 'MIFFY', name: 'Miffy Doll', image: 'https://cdn.example/m.png', href: `/terminal/${poolB}`, liquidityRaw: '62000000000', volumeRaw: '5000000', mcapRaw: '28000000000', createdAtMs: now - 3_600_000 });
    const bare = similarRow({ poolAddress: poolC, tokenAddress: null, label: 'Pink Doll' }, null, null, null, now);
    expect(bare).toMatchObject({ name: 'Pink Doll', liquidityRaw: null, volumeRaw: null, mcapRaw: null, createdAtMs: null });
    expect(sortSimilar([bare, row], 'liq').map(item => item.poolAddress)).toEqual([poolB, poolC]);
    expect(sortSimilar([row, { ...row, poolAddress: poolC, mcapRaw: '99000000000' }], 'mc').map(item => item.poolAddress)).toEqual([poolC, poolB]);
  });
});
