'use client';
import { useMemo, useState } from 'react';
import { Flame } from 'lucide-react';
import { Button, EmptyState, ErrorState, LoadMore, NoticeBar, SegmentedControl, SelectPill, UnavailableState, type SegmentOption, type SelectOption } from '@/ui/primitives';
import { useExploreRows, type ExploreRow } from '@/widgets/explore/use-explore-rows';
import type { RankingCategory } from '@/core/market/use-rankings';
import type { StatValue } from '@/core/market/pool-stats-display';
import type { TokenMetadata } from '@/widgets/home/types';
import type { Loadable } from '@/core/market/unsupported';
import { useWatchlist } from '@/hooks/ui/use-watchlist';
import { TrendingRow, TrendingRowSkeleton, safeSocials, type TrendingRowData } from './trending-row';
import type { WidgetProps } from './widget-registry';
import { columnNoticeClass, columnToolbarClass, usePanelState } from './panel-state';

export type TrendingCategory = Extract<RankingCategory, 'trending' | 'unusual' | 'movers' | 'volume'>;
export type Timeframe = '1h' | '4h' | '12h' | '24h';

/**
 * Ranking tabs. Labels are short so the four tabs and the change-window pill share one line at the
 * 288 px minimum column width; the full name is each tab's accessible name and hover title.
 */
export const trendingCategories: (SegmentOption<TrendingCategory> & { label: string; ariaLabel: string })[] = [
  { value: 'trending', label: 'Trend', ariaLabel: 'Trending' },
  { value: 'unusual', label: 'Unusual', ariaLabel: 'Unusual' },
  { value: 'movers', label: 'Movers', ariaLabel: 'Movers' },
  { value: 'volume', label: 'Vol', ariaLabel: 'Volume' },
];

export const timeframes: Timeframe[] = ['1h', '4h', '12h', '24h'];
/** Windows with a v2 PoolStats change field: 24h (priceChangeBps24h) and 1h (priceChangeBps1h). */
export const servedTimeframes: ReadonlySet<Timeframe> = new Set<Timeframe>(['1h', '24h']);
/** Change-window options for the compact selector; windows with no projection say so in the menu. */
export const timeframeOptions: SelectOption<Timeframe>[] = timeframes.map(value => ({
  value,
  text: value,
  label: servedTimeframes.has(value) ? value : <span className="inline-flex items-baseline gap-1.5">{value}<span className="text-xs font-semibold text-text-faint">soon</span></span>,
}));

/** Change-window segments for a column wide enough to show all four (pump.fun's 1h / 4h / 12h / 24h group). */
export const timeframeSegments: SegmentOption<Timeframe>[] = timeframes.map(value => ({
  value,
  label: value,
  ...(servedTimeframes.has(value) ? {} : { ariaLabel: `${value}, coming soon`, title: `${value} change coming soon` }),
}));

/**
 * Column width (px) from which the change window shows as the segmented 1h / 4h / 12h / 24h group
 * beside the ranking tabs; narrower columns keep the compact value pill (pump.fun shows a
 * dropdown at 1024 and the segmented group from 1280). Ranking tabs (~213 px) + gap (8) +
 * window segments (~130 px) + row padding (16) = 367 px, rounded up.
 */
export const timeframeSegmentsMinWidth = 376;

/** The row's change for the selected window: 1h and 24h from the v2 statistics (value or baseline reason); 4h and 12h have no v2 source. */
export function changeFor(row: Pick<ExploreRow, 'changes'>, timeframe: Timeframe): StatValue<number> {
  if (timeframe === '1h' || timeframe === '24h') return row.changes[timeframe];
  return { value: null, reason: `${timeframe} change coming soon, pick 1h or 24h` };
}

/** Empty-snapshot copy per ranking tab; a successful empty ranking is a normal state before coins trade. */
export const trendingEmptyTitles: Record<TrendingCategory, string> = {
  trending: 'No coins trending yet',
  unusual: 'No unusual activity yet',
  movers: 'No movers yet',
  volume: 'No volume yet',
};
export const trendingEmptyBody = 'Coins show up here once they start trading';

export function matchesFilter(row: Pick<ExploreRow, 'name' | 'symbol' | 'poolAddress' | 'tokenAddress'>, filter: string): boolean {
  const needle = filter.trim().toLowerCase();
  if (!needle) return true;
  return row.name.toLowerCase().includes(needle) || row.symbol.toLowerCase().includes(needle) || row.poolAddress.toLowerCase().includes(needle) || (row.tokenAddress ?? '').toLowerCase().includes(needle);
}

export function toTrendingRow(row: ExploreRow, timeframe: Timeframe, metadata: TokenMetadata | null): TrendingRowData {
  const change = changeFor(row, timeframe);
  return {
    poolAddress: row.poolAddress,
    href: row.href,
    rank: row.rank,
    name: row.name,
    symbol: row.symbol,
    image: row.image,
    price: row.price,
    priceReason: row.priceReason,
    mcapRaw: row.mcapRaw,
    mcapReason: row.mcapReason,
    change: change.value,
    changeReason: change.reason,
    socials: safeSocials(metadata?.socials),
    stage: row.stage,
  };
}

function readyRecord<T>(source: Loadable<Record<string, T>>): Record<string, T> | null {
  return source.state === 'ready' ? source.data : null;
}

/**
 * Trending column (WP-09, features.csv 135-148): ranking categories as tabs,
 * 1h/4h/12h/24h selector where only the served timeframes carry data, rows
 * with image, name, price to 7 decimals, socials, market cap and change tint.
 */
export function TrendingWidget({ filter, density }: WidgetProps) {
  const [category, setCategory] = useState<TrendingCategory>('trending');
  const [timeframe, setTimeframe] = useState<Timeframe>('24h');
  const explore = useExploreRows(category);
  const panel = usePanelState(explore.loading, explore.ranking, category);
  const watchlist = useWatchlist();
  const metadata = readyRecord(explore.metadata);
  const rows = useMemo(
    () => explore.rows.filter(row => matchesFilter(row, filter)).map(row => toTrendingRow(row, timeframe, metadata?.[row.poolAddress] ?? null)),
    [explore.rows, filter, timeframe, metadata],
  );
  const quality = explore.snapshot?.quality ?? null;
  const timeframeServed = servedTimeframes.has(timeframe);

  return (
    <div className="@container flex min-h-0 flex-1 flex-col">
      <div className={columnToolbarClass}>
        <SegmentedControl label="Ranking" options={trendingCategories} value={category} onChange={setCategory} size="sm" shape="sm" compact />
        {/* One of the two is displayed, by the column's own width (container query), never both. */}
        <div data-timeframe="segments" className="ml-auto hidden @min-[376px]:flex">
          <SegmentedControl label="Change window" options={timeframeSegments} value={timeframe} onChange={setTimeframe} size="sm" shape="sm" compact />
        </div>
        <SelectPill label="Change window" className="ml-auto @min-[376px]:hidden" options={timeframeOptions} value={timeframe} onChange={setTimeframe} size="sm" compact />
      </div>
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {!timeframeServed ? (
          <UnavailableState variant="chip" plural="auto" surface={1} wrap className={columnNoticeClass} title={`${timeframe} change`} operation={`IF_STATS_BATCH (priceChangeBps${timeframe})`} status={null} />
        ) : null}
        {rows.length > 0 && (quality === 'stale' || quality === 'partial' || quality === 'quarantined') ? (
          <NoticeBar tone="warning" className="mx-2 mb-2" surface={1}>Rankings may be a little behind right now</NoticeBar>
        ) : null}
        {panel.state === 'loading' ? (
          <TrendingRowSkeleton density={density} />
        ) : panel.state === 'unsupported' ? (
          <UnavailableState variant="chip" plural="auto" surface={1} wrap className={columnNoticeClass} title="Trending" operation={panel.operation} status={panel.status} message={panel.message} onRetry={explore.reload} />
        ) : panel.state === 'error' ? (
          <ErrorState variant="chip" className={columnNoticeClass} message="Couldn't load trending coins" error={panel.error} onRetry={panel.retry} />
        ) : rows.length === 0 ? (
          filter.trim() ? (
            <EmptyState icon={Flame} title="No coins match this filter" body={`Nothing matches "${filter.trim()}"`} />
          ) : (
            <EmptyState icon={Flame} title={trendingEmptyTitles[category]} body={trendingEmptyBody} action={<Button variant="secondary" size="sm" href="/create">Create coin</Button>} />
          )
        ) : (
          <>
            {explore.stats.state === 'unsupported' ? <UnavailableState variant="chip" plural="auto" surface={1} wrap className={columnNoticeClass} title="Live stats" operation={explore.stats.operation} status={explore.stats.status} /> : null}
            <ol className="flex flex-col">
              {rows.map(row => (
                <TrendingRow key={row.poolAddress} row={row} density={density} starred={watchlist.check(row.poolAddress)} onToggleStar={() => watchlist.toggle(row.poolAddress)} />
              ))}
            </ol>
            <LoadMore onLoadMore={explore.loadMore} hasMore={explore.hasMore} loading={explore.loadingMore} error={explore.moreError !== null} auto={false} className="px-3 py-2" />
          </>
        )}
      </div>
    </div>
  );
}

export default TrendingWidget;
