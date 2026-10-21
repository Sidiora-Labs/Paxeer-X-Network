'use client';
import { useMemo, useState } from 'react';
import { Info, LayoutGrid } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, CoinCard, EmptyState, ErrorState, LoadMore, SegmentedControl, Tooltip, UnavailableState, type CoinCardStat } from '@/ui/primitives';
import { useExploreRows, type ExploreRow } from '@/widgets/explore/use-explore-rows';
import type { RankingCategory } from '@/core/market/use-rankings';
import { useWatchlist } from '@/hooks/ui/use-watchlist';
import { formatUsdl } from '@/utils/format';
import { compactCount } from '@/page-modules/token/market-presentation';
import { RiskRow, riskLegend } from './risk-row';
import { matchesFilter } from './trending-widget';
import type { WidgetProps } from './widget-registry';
import { columnToolbarClass, usePanelState } from './panel-state';

export type ScreenerCategory = Extract<RankingCategory, 'new' | 'movers' | 'volume' | 'breakout'>;

export const screenerCategories: { value: ScreenerCategory; label: string }[] = [
  { value: 'new', label: 'New' },
  { value: 'movers', label: 'Movers' },
  { value: 'volume', label: 'Volume' },
  { value: 'breakout', label: 'Breakout' },
];

export const feesUnavailableReason = 'Fees are not tracked yet';

export function screenerStats(row: ExploreRow): CoinCardStat[] {
  return [
    { label: 'V', value: row.volume24hRaw ? formatUsdl(row.volume24hRaw, { compact: true, unit: false }) : null },
    { label: 'TX', value: row.txns === null ? null : compactCount(row.txns) },
    { label: 'F', value: <span title={feesUnavailableReason} className="text-text-faint">—</span> },
  ];
}

/** Empty-snapshot copy per preset; an empty screener before the first launch is a normal state. */
export const screenerEmpty: Record<ScreenerCategory, { title: string; body: string }> = {
  new: { title: 'No coins launched yet', body: 'New coins show up here the moment they launch' },
  movers: { title: 'No movers yet', body: 'Coins show up here once their price starts moving' },
  volume: { title: 'No volume yet', body: 'Coins show up here as soon as they trade' },
  breakout: { title: 'No breakouts yet', body: 'Coins show up here when they break out' },
};

export interface ScreenerRisk {
  holders: number | null;
  devPct: number | null;
  top10Pct: number | null;
}

/** Holder count plus the developer and top-10 shares (PoolStats.creatorHoldingShareBps / top10HolderShareBps; null when not computed). */
export function screenerRisk(row: Pick<ExploreRow, 'holders' | 'devShare' | 'top10Share'>): ScreenerRisk {
  return { holders: row.holders, devPct: row.devShare, top10Pct: row.top10Share };
}

/**
 * Screener column (WP-09, features.csv 163-166): New / Movers / Volume /
 * Breakout rankings as screener cards with V, MC and TX from real stats, F
 * marked unavailable, and the risk row below each card. Sniper and bundle
 * shares are not served and render as dashes.
 */
export function ScreenerWidget({ filter, view }: WidgetProps) {
  const [category, setCategory] = useState<ScreenerCategory>('new');
  const explore = useExploreRows(category);
  const panel = usePanelState(explore.loading, explore.ranking, category);
  const watchlist = useWatchlist();
  const rows = useMemo(() => explore.rows.filter(row => matchesFilter(row, filter)), [explore.rows, filter]);
  const cards = view === 'cards';

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className={columnToolbarClass}>
        <SegmentedControl label="Screener list" options={screenerCategories} value={category} onChange={setCategory} size="sm" shape="sm" compact />
        <Tooltip content={<ul className="flex flex-col gap-1">{riskLegend.map(line => <li key={line}>{line}</li>)}</ul>} align="end" className="ml-auto">
          <button type="button" aria-label="What these numbers mean" className="inline-flex size-control-height-sm items-center justify-center rounded-sm text-text-faint hover:bg-surface-hover hover:text-text">
            <Info aria-hidden="true" className="size-icon-sm" />
          </button>
        </Tooltip>
      </div>
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-2 pb-2">
        {panel.state === 'loading' ? (
          <div aria-busy="true" className={cn('flex gap-2', cards ? 'grid grid-cols-2' : 'flex-col')}>
            <span className="sr-only">Loading screener</span>
            {Array.from({ length: 6 }, (_, index) => <CoinCard key={index} loading variant={cards ? 'grid' : 'screener'} surface="surface-1" image={null} name="" symbol="" mcapRaw={null} percentChange={null} createdAt={null} href="#" />)}
          </div>
        ) : panel.state === 'unsupported' ? (
          <UnavailableState variant="chip" plural="auto" surface={1} wrap className="self-start" title="Screener" operation={panel.operation} status={panel.status} message={panel.message} onRetry={explore.reload} />
        ) : panel.state === 'error' ? (
          <ErrorState variant="chip" className="self-start" message="Couldn't load coins" error={panel.error} onRetry={panel.retry} />
        ) : rows.length === 0 ? (
          filter.trim() ? (
            <EmptyState icon={LayoutGrid} title="No coins match this filter" body={`Nothing matches "${filter.trim()}"`} />
          ) : (
            <EmptyState icon={LayoutGrid} title={screenerEmpty[category].title} body={screenerEmpty[category].body} action={<Button variant="secondary" size="sm" href="/create">Create coin</Button>} />
          )
        ) : (
          <>
            {explore.stats.state === 'unsupported' ? <UnavailableState variant="chip" plural="auto" surface={1} wrap className="self-start" title="Live stats" operation={explore.stats.operation} status={explore.stats.status} /> : null}
            <ul className={cn('gap-2', cards ? 'grid grid-cols-2' : 'flex flex-col')}>
              {rows.map(row => {
                const risk = screenerRisk(row);
                return (
                  <li key={row.poolAddress} className="flex flex-col rounded-md bg-surface-2 pb-1.5">
                    <CoinCard
                      variant={cards ? 'grid' : 'screener'}
                      surface="surface-1"
                      image={row.image}
                      name={row.name}
                      symbol={row.symbol}
                      mcapRaw={row.mcapRaw}
                      percentChange={row.change24h}
                      createdAt={row.createdAtMs}
                      href={row.href}
                      stage={row.stage}
                      rank={row.rank}
                      footerStats={screenerStats(row)}
                      starred={watchlist.check(row.poolAddress)}
                      onToggleStar={() => watchlist.toggle(row.poolAddress)}
                    />
                    <RiskRow holders={risk.holders} sniperPct={null} bundlePct={null} devPct={risk.devPct} top10Pct={risk.top10Pct} loading={explore.enrichmentPending && explore.stats.state === 'loading'} />
                  </li>
                );
              })}
            </ul>
            <LoadMore onLoadMore={explore.loadMore} hasMore={explore.hasMore} loading={explore.loadingMore} error={explore.moreError !== null} auto={false} className="py-2" />
          </>
        )}
      </div>
    </div>
  );
}

export default ScreenerWidget;
