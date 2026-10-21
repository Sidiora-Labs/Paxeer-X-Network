// @vitest-environment jsdom
import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { CapabilityUnavailableError, HttpError } from '@kindle/sdk';
import { toUnsupported, type Unsupported } from '@/core/market/unsupported';
import type { HolderRewardPool, HolderRewardsStatsResponse } from './holder-rewards-contract';
import { classifyHolderRewardsFailure } from './holder-rewards-contract';
import { buildRows, defaultFilters, defaultOrder, type HolderRewardsFilters } from './holder-rewards-presentation';
import HolderRewardTokenCard from './holder-reward-token-card';
import HolderRewardsControls, { filterMenuItems } from './holder-rewards-controls';
import HolderRewardsHeader, { launchHolderRewardsHref } from './holder-rewards-header';
import HolderRewardsTable from './holder-rewards-table';
import { HolderRewardsUnavailable } from './holder-rewards-view';
import StatsRow from './stats-row';
import TopPayingRail from './top-paying-rail';

const html = (node: React.ReactElement) => renderToStaticMarkup(node);
const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const noop = () => {};

const poolA = '0x' + 'a1'.repeat(20);
const poolB = '0x' + 'b2'.repeat(20);
const tokenA = '0x' + 'c3'.repeat(20);
const tokenB = '0x' + 'd4'.repeat(20);
const items: HolderRewardPool[] = [
  { poolAddress: poolA, tokenAddress: tokenA, distributedRaw: '504967000000', holdersPaid: 14648, strategyActive: true, createdAt: '2026-09-14T10:00:00.000Z' },
  { poolAddress: poolB, tokenAddress: tokenB, distributedRaw: '498048000000', holdersPaid: 26093, strategyActive: false, createdAt: '2026-09-01T10:00:00.000Z' },
];
const rows = buildRows(items, new Map(), { [tokenA]: { token_address: tokenA, name: 'Pill', symbol: 'PILL' }, [tokenB]: null });

const unsupportedStats = toUnsupported('IF_HOLDER_REWARDS_STATS', new CapabilityUnavailableError('Unavailable endpoint /v2/holder-rewards/stats', 404)) as Unsupported;
const readyStats: HolderRewardsStatsResponse = {
  requestId: '0f0e2a8e-5b3c-4d8a-9a1e-2c6d7b8f9a01', generatedAt: '2026-10-12T15:00:01.000Z', nextCursor: null, quality: 'complete', revision: '7', watermark: null,
  data: { cumulativeDistributedRaw: '18627546000000', walletsRewarded: 445691, generatedAt: '2026-10-12T15:00:01.000Z', watermark: null },
};

describe('holder rewards components', () => {
  it('renders the header on a tinted panel with two-line copy, display H1 and the airdrop-strategy create deep link', () => {
    const out = html(<HolderRewardsHeader><p>stats slot</p></HolderRewardsHeader>);
    expect(out).toContain('Holder rewards coins');
    expect(out).toMatch(/<header class="[^"]*bg-surface-1/);
    expect(out).toContain('sm:text-[40px]');
    expect(out).toContain('text-[28px]');
    expect(out).not.toMatch(/font-(extrabold|black)/);
    expect(out).toContain('font-medium');
    expect(out).toContain('stats slot');
    expect(out).toContain('paid in USDL');
    const copy = /<p class="[^"]*">([^<]+)<\/p>/.exec(out)?.[1] ?? '';
    expect(copy.length).toBeLessThanOrEqual(140);
    expect(out).toContain(`href="${launchHolderRewardsHref}"`);
    expect(launchHolderRewardsHref).toBe('/create?strategy=airdrop');
    expect(out).not.toMatch(BORDER);
  });

  it('shows three display tiles, real totals only, today\'s value derived from the served USDL total', () => {
    const ready = html(<StatsRow stats={{ state: 'ready', data: readyStats }} />);
    expect(ready.match(/<dt /g)).toHaveLength(3);
    expect(ready).toContain('Holder rewards');
    expect(ready).toContain('At today&#x27;s prices');
    expect(ready).toContain('Wallets rewarded');
    expect(ready.match(/>18,627,546<\/span><span[^>]*>USDL</g)).toHaveLength(2);
    expect(ready).toContain('Valued in USDL');
    expect(ready).toContain('445,691');
    expect(ready).toContain('sm:text-[44px]');
    expect(ready).toContain('lg:grid-cols-3');
    expect(ready).not.toContain('aria-label="No data"');
    expect(ready).not.toContain('not available yet');
    const loading = html(<StatsRow stats={{ state: 'loading' }} />);
    expect(loading.match(/aria-busy="true"/g)).toHaveLength(3);
    expect(loading).not.toContain('USDL');
    const unavailable = html(<StatsRow stats={unsupportedStats} />);
    expect(unavailable.match(/<dt /g)).toHaveLength(3);
    expect(unavailable).toContain('Holder rewards are not available yet<');
    expect(unavailable).toContain('Today&#x27;s value is not available yet<');
    expect(unavailable).toContain('Wallet count is not available yet<');
    expect(unavailable.match(/role="status"[^>]*data-state="unavailable"/g)).toHaveLength(3);
    expect(unavailable).toContain('bg-surface-3');
    expect(unavailable).not.toContain('IF_HOLDER_REWARDS_STATS');
    expect(unavailable).not.toMatch(/\d{2,} USDL/);
    expect(unavailable).not.toMatch(/>\d[\d,]*</);
    expect(unavailable.match(/aria-label="Not available yet"/g)).toHaveLength(3);
    expect(unavailable).toContain('sm:text-[44px]');
    expect(unavailable).not.toContain('Failed to fetch');
    expect(unavailable).not.toMatch(BORDER);
    const failed = html(<StatsRow stats={{ state: 'error', error: new TypeError('Failed to fetch'), retry: noop }} />);
    expect(failed.match(/<dt /g)).toHaveLength(3);
    expect(failed).not.toContain('Failed to fetch');
    expect(failed).toContain("Totals can&#x27;t load right now");
    expect(failed).toMatch(/Try again|Retry/);
  });

  it('renders the page-level panel for the real 404 and a retry-worthy variant for 503', () => {
    const unindexed = html(<HolderRewardsUnavailable availability={classifyHolderRewardsFailure('IF_HOLDER_REWARDS_POOLS', new CapabilityUnavailableError('Unavailable endpoint /v2/holder-rewards/pools', 404))} onRetry={noop} />);
    expect(unindexed).toContain('Holder rewards are not available yet');
    expect(unindexed).not.toContain('IF_HOLDER_REWARDS_POOLS');
    expect(unindexed).toContain('Try again');
    const degraded = html(<HolderRewardsUnavailable availability={classifyHolderRewardsFailure('IF_HOLDER_REWARDS_POOLS', new HttpError(503, { code: 'DEPENDENCY_UNAVAILABLE', message: 'x', requestId: 'r', retryable: true, retryAfterMs: null, fieldErrors: [], details: null }))} onRetry={noop} />);
    expect(degraded).not.toContain('IF_HOLDER_REWARDS_POOLS');
    expect(degraded).toContain('Try again in a moment');
    expect(degraded).not.toContain('DEPENDENCY_UNAVAILABLE');
    const failed = html(<HolderRewardsUnavailable availability={classifyHolderRewardsFailure('IF_HOLDER_REWARDS_POOLS', new Error('boom'))} onRetry={noop} />);
    expect(failed).toContain('role="alert"');
    expect(failed).not.toContain('boom');
    expect(failed).toContain('Something went wrong, try again');
  });

  it('renders the top paying rail as a ranked list, a skeleton rail or an honest empty state', () => {
    const ready = html(<TopPayingRail rows={rows} />);
    expect(ready).toContain('<ol');
    expect(ready).toContain('Pill');
    expect(ready).toContain('bg-rank-1-soft');
    expect(ready).toContain(`href="/coin/${poolA}"`);
    expect(ready).toContain('Strategy changed');
    expect(ready).not.toMatch(BORDER);
    const loading = html(<TopPayingRail rows={[]} loading />);
    expect(loading).toContain('aria-busy="true"');
    const empty = html(<TopPayingRail rows={[]} />);
    expect(empty).toContain('No holder rewards paid yet');
    expect(empty).toContain(`href="${launchHolderRewardsHref}"`);
  });

  it('renders the table with sortable headers, row links, copy buttons, the USDL badge and the loaded-rows disclosure', () => {
    const out = html(<HolderRewardsTable rows={rows} order={defaultOrder} onOrderChange={noop} hasMore onLoadMore={noop} hoveredPool={poolA} airdrop={{ pool: poolA, status: 'ready', balance: 1250000n }} />);
    expect(out).toContain('aria-sort="descending"');
    expect(out).toContain(`href="/coin/${poolA}"`);
    expect(out).toContain(`href="https://paxscan.paxeer.app/address/${tokenA}"`);
    expect(out).toContain('Copy address');
    expect(out).toMatch(/>504,967<span[^>]*>USDL</);
    expect(out).toContain('Pending airdrop 1.25 USDL');
    expect(out).toContain('>Holder rewards');
    expect(out).toContain('>Holders paid');
    expect(out).not.toContain('>Age');
    expect(/<thead>.*<\/thead>/.exec(out)?.[0]).not.toContain('uppercase');
    expect(out).toContain('whitespace-nowrap');
    expect(out).toContain('>14,648<');
    expect(out).toContain('>USDL<');
    expect(out).toContain('Ranked across the 2 coins shown');
    expect(out).toContain('Load more');
    expect(out).toContain('odd:bg-surface-2');
    expect(out).not.toMatch(BORDER);
    expect(out).not.toMatch(/\$\s?\d/);
  });

  it('renders table loading and empty states without numbers', () => {
    const loading = html(<HolderRewardsTable rows={[]} order={defaultOrder} onOrderChange={noop} loading hasMore={false} onLoadMore={noop} />);
    expect(loading).toContain('sl-skeleton');
    expect(loading).not.toContain('text-success');
    expect(loading).not.toMatch(/\d USDL/);
    const empty = html(<HolderRewardsTable rows={[]} order={defaultOrder} onOrderChange={noop} hasMore={false} onLoadMore={noop} />);
    expect(empty).toContain('No holder rewards paid yet');
    expect(empty).not.toContain('Load more');
    const filtered = html(<HolderRewardsTable rows={[]} filtered order={defaultOrder} onOrderChange={noop} hasMore={false} onLoadMore={noop} />);
    expect(filtered).toContain('No coins match these filters');
    expect(filtered).not.toContain('No holder rewards paid yet');
  });

  it('renders the shared card as a static preview when it has no destination', () => {
    const preview = html(<HolderRewardTokenCard name="My coin" symbol="MINE" image={null} tokenAddress={null} href={null} createdAt={null} distributedRaw={null} mcapRaw={null} holdersPaid={null} />);
    expect(preview).toContain('<h3');
    expect(preview).not.toContain('<a ');
    expect(preview).toContain('$MINE');
    expect(preview).not.toMatch(BORDER);
    const ranked = html(<HolderRewardTokenCard rank={4} name="Tulip" symbol="TULIP" image={null} tokenAddress={tokenA} href={`/coin/${poolA}`} createdAt="2026-09-14T10:00:00.000Z" distributedRaw="226147000000" mcapRaw="61000000000" holdersPaid={875} />);
    expect(ranked).not.toContain('bg-rank-1-soft');
    expect(ranked).toMatch(/>226,147<span[^>]*>USDL</);
    expect(ranked).toContain('61K USDL');
    expect(ranked).toContain('Holders paid');
    expect(ranked).toContain('875');
    expect(ranked).toContain('min-h-[188px]');
    expect(ranked).toContain('title="Rewards are paid in USDL"');
  });

  it('renders the controls with sort, fixed USDL pairing, view toggle, filters and export', () => {
    const out = html(<HolderRewardsControls order={defaultOrder} onSortChange={noop} view="table" onViewChange={noop} filters={defaultFilters} onFiltersChange={noop} onExport={noop} />);
    expect(out).toContain('Sort by');
    expect(out).toContain('Paired with');
    expect(out).not.toContain('Paired with:');
    expect(out).toContain('aria-label="Table view"');
    expect(out).toContain('aria-checked="true"');
    expect(out).toContain('Filters');
    expect(out).toContain('aria-label="Download as CSV"');
    expect(out).not.toMatch(/aria-label="Download as CSV"[^>]*disabled=""/);
    expect(out).not.toMatch(BORDER);
    const disabled = html(<HolderRewardsControls order={defaultOrder} onSortChange={noop} view="table" onViewChange={noop} filters={{ activeOnly: true, pricedOnly: true }} onFiltersChange={noop} onExport={null} />);
    expect(disabled).toMatch(/<button[^>]*disabled=""[^>]*aria-label="Download as CSV"|aria-label="Download as CSV"[^>]*disabled=""/);
    expect(disabled).toContain('aria-label="Filters, 2 active"');
  });

  it('builds filter menu items as radio pairs that report the next filter state', () => {
    const seen: HolderRewardsFilters[] = [];
    const items = filterMenuItems(defaultFilters, next => seen.push(next));
    expect(items.map(item => [item.group, item.selected])).toEqual([['Fee strategy', true], ['Fee strategy', false], ['Market cap', true], ['Market cap', false]]);
    items[1].onSelect?.();
    items[3].onSelect?.();
    expect(seen).toEqual([{ activeOnly: true, pricedOnly: false }, { activeOnly: false, pricedOnly: true }]);
  });
});
