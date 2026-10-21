'use client';
import { useRef, type ReactNode } from 'react';
import { useSparkline } from '@/core/market/use-sparkline';
import { CoinCard, LoadMore } from '@/ui/primitives';
import { formatCompactUsd, formatCount } from '@/utils/format';
import { formatShare, formatSpotPrice } from '@/core/market/pool-stats-display';
import type { ExploreRow } from './use-explore-rows';
import { useVisible } from './use-visible';

interface GridCardProps {
  row: ExploreRow;
  starred: boolean;
  onToggleStar: (poolAddress: string) => void;
}

function GridCard({ row, starred, onToggleStar }: GridCardProps) {
  const ref = useRef<HTMLLIElement | null>(null);
  const visible = useVisible(ref);
  const sparkline = useSparkline(visible ? row.poolAddress : null);
  return (
    <li ref={ref} className="flex">
      <CoinCard
        variant="grid"
        surface="canvas"
        image={row.image}
        name={row.name}
        symbol={row.symbol}
        mcapRaw={row.mcapRaw}
        percentChange={row.change24h}
        createdAt={row.createdAtMs}
        href={row.href}
        rank={row.rank}
        stage={row.stage}
        starred={starred}
        onToggleStar={() => onToggleStar(row.poolAddress)}
        sparkline={sparkline.data?.closes ?? null}
        footerStats={[
          { label: 'Vol 24h', value: row.volume24h === null ? null : formatCompactUsd(row.volume24h) },
          { label: 'Txns 24h', value: row.txns === null ? null : formatCount(row.txns) },
          { label: 'Traders 24h', value: row.traders === null ? null : formatCount(row.traders) },
          { label: 'Price', value: row.price === null ? null : formatSpotPrice(row.price) },
          { label: 'Top 10', value: row.top10Share === null ? null : formatShare(row.top10Share) },
          { label: 'Creator', value: row.devShare === null ? null : formatShare(row.devShare) },
        ]}
        className="w-full"
      />
    </li>
  );
}

export interface ExploreGridProps {
  rows: ExploreRow[];
  isStarred: (poolAddress: string) => boolean;
  onToggleStar: (poolAddress: string) => void;
  loading: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  moreError: boolean;
  onLoadMore: () => void;
  empty: ReactNode;
  disclosure: string | null;
  label: string;
}

export function ExploreGrid({ rows, isStarred, onToggleStar, loading, hasMore, loadingMore, moreError, onLoadMore, empty, disclosure, label }: ExploreGridProps) {
  if (loading) {
    return (
      <div aria-busy="true" className="grid grid-cols-[repeat(auto-fill,minmax(var(--spacing-token-card-min-width),1fr))] gap-4">
        <span className="sr-only">Loading coins</span>
        {Array.from({ length: 8 }).map((_, index) => <CoinCard key={index} loading image={null} name="" symbol="" mcapRaw={null} percentChange={null} createdAt={null} href="/explore" />)}
      </div>
    );
  }
  if (rows.length === 0) return <div className="rounded-md bg-surface-1">{empty}</div>;
  return (
    <section aria-label={label} className="flex flex-col gap-2">
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(var(--spacing-token-card-min-width),1fr))] gap-4">
        {rows.map(row => <GridCard key={row.poolAddress} row={row} starred={isStarred(row.poolAddress)} onToggleStar={onToggleStar} />)}
      </ul>
      {disclosure ? <p role="status" className="px-1 text-xs text-text-faint">{disclosure}</p> : null}
      {hasMore || moreError ? <LoadMore onLoadMore={onLoadMore} hasMore={hasMore || moreError} loading={loadingMore} error={moreError} label="Load more coins" /> : null}
    </section>
  );
}
