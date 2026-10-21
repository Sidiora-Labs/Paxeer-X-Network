'use client';
import { useMemo } from 'react';
import { Star } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, EmptyState, UnavailableState } from '@/ui/primitives';
import { useWatchlistRows, type WatchlistRow } from '@/core/market/use-watchlist-rows';
import { buildExploreRow } from '@/widgets/explore/use-explore-rows';
import { openAuthModal } from '@/shell/auth-modal-store';
import { TrendingRow, TrendingRowSkeleton, type TrendingRowData } from './trending-row';
import { matchesFilter, toTrendingRow } from './trending-widget';
import type { ColumnDensity } from './layout-store';
import { columnNoticeClass } from './panel-state';

export interface WatchlistWidgetProps {
  filter?: string;
  density?: ColumnDensity;
  /** Standalone pages (the watchlist route, the profile More tab) render on canvas with their own card surface. */
  standalone?: boolean;
  className?: string;
}

export function watchlistRowView(row: WatchlistRow, index: number, now: number): TrendingRowData {
  const explore = buildExploreRow({ poolAddress: row.poolAddress, rank: index + 1, stats: row.stats, listing: row.listing, metadata: row.metadata, now });
  return toTrendingRow(explore, '24h', row.metadata);
}

/**
 * Watchlist column (WP-09): the account or local watchlist joined with live
 * stats through use-watchlist-rows. Each source carries its own unavailable
 * state so a 503 from one projection never blanks the list.
 */
export function WatchlistWidget({ filter = '', density = 'comfortable', standalone = false, className }: WatchlistWidgetProps) {
  const watchlist = useWatchlistRows();
  const rows = useMemo(() => {
    const now = Date.now();
    return watchlist.rows.map((row, index) => watchlistRowView(row, index, now)).filter(row => matchesFilter({ name: row.name, symbol: row.symbol, poolAddress: row.poolAddress, tokenAddress: null }, filter));
  }, [watchlist.rows, filter]);

  const body = watchlist.loading ? (
    <TrendingRowSkeleton density={density} count={4} />
  ) : watchlist.pools.length === 0 ? (
    <EmptyState
      icon={Star}
      title="Your watchlist is empty"
      body={watchlist.isLocal ? 'Star coins to follow them here, sign in to save them' : 'Star coins to follow them here'}
      action={<Button variant="secondary" size="sm" href="/explore">Browse coins</Button>}
      secondaryAction={watchlist.isLocal ? <Button variant="ghost" size="sm" onClick={() => openAuthModal('watchlist')}>Sign in</Button> : undefined}
    />
  ) : rows.length === 0 ? (
    <EmptyState icon={Star} title="No coins match this filter" body={`Nothing matches "${filter.trim()}"`} />
  ) : (
    <>
      {watchlist.stats.state === 'unsupported' ? <UnavailableState variant="chip" plural="auto" surface={1} wrap className={columnNoticeClass} title="Live stats" operation={watchlist.stats.operation} status={watchlist.stats.status} /> : null}
      {watchlist.metadata.state === 'unsupported' ? <UnavailableState variant="chip" plural="auto" surface={1} wrap className={columnNoticeClass} title="Coin details" operation={watchlist.metadata.operation} status={watchlist.metadata.status} /> : null}
      <ol className="flex flex-col">
        {rows.map(row => (
          <TrendingRow key={row.poolAddress} row={row} density={density} starred={watchlist.check(row.poolAddress)} onToggleStar={() => watchlist.toggle(row.poolAddress)} />
        ))}
      </ol>
    </>
  );

  if (standalone) {
    return (
      <section aria-label="Watchlist" className={cn('flex flex-col rounded-md bg-surface-1 py-2', className)}>
        {watchlist.isLocal && watchlist.pools.length > 0 ? <p className="px-3 pb-2 text-xs text-text-muted">Saved on this device. <button type="button" className="text-accent hover:underline" onClick={() => openAuthModal('watchlist')}>Sign in</button> to keep it on your account.</p> : null}
        {body}
      </section>
    );
  }

  return (
    <div className={cn('flex min-h-0 flex-1 flex-col', className)}>
      {watchlist.isLocal && watchlist.pools.length > 0 ? <p className="flex h-10 shrink-0 items-center px-3 text-xs text-text-muted">Saved on this device. <button type="button" className="ml-1 text-accent hover:underline" onClick={() => openAuthModal('watchlist')}>Sign in</button></p> : null}
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">{body}</div>
    </div>
  );
}

export default WatchlistWidget;
