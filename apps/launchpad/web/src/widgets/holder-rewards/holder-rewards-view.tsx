'use client';

import { useMemo, useState } from 'react';
import { ErrorState, UnavailableState } from '@/ui/primitives';
import HolderRewardTokenCard from './holder-reward-token-card';
import HolderRewardsControls, { type HolderRewardsViewMode } from './holder-rewards-controls';
import type { HolderRewardsAvailability } from './holder-rewards-contract';
import HolderRewardsHeader from './holder-rewards-header';
import { coinHref, defaultFilters, defaultOrder, filterRows, rowsToCsv, sortRows, type HolderRewardsFilters, type HolderRewardsOrder } from './holder-rewards-presentation';
import HolderRewardsTable, { type AirdropHover } from './holder-rewards-table';
import StatsRow from './stats-row';
import TopPayingRail, { topPayingCount } from './top-paying-rail';
import { useAirdropBalance, useHolderRewardsRows, useHolderRewardsStats } from './use-holder-rewards';

export function HolderRewardsUnavailable({ availability, onRetry }: { availability: HolderRewardsAvailability; onRetry(): void }) {
  if (availability.kind === 'failed') {
    return <ErrorState title="Holder rewards can't load right now" message="Something went wrong, try again" onRetry={onRetry} className="rounded-lg bg-surface-1" />;
  }
  const { marker } = availability;
  return (
    <UnavailableState
      title="Holder rewards"
      plural="auto"
      operation={marker.operation}
      status={marker.status}
      message={availability.kind === 'unindexed'
        ? "Totals and rankings show up here once they're ready. Check back soon"
        : 'Try again in a moment'}
      onRetry={onRetry}
      className="rounded-lg bg-surface-1"
    />
  );
}

function downloadCsv(csv: string) {
  const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }));
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = 'holder-rewards.csv';
  anchor.click();
  URL.revokeObjectURL(url);
}

function GridView({ rows }: { rows: ReturnType<typeof sortRows> }) {
  return (
    <ol className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
      {rows.map((row, index) => (
        <li key={row.poolAddress} className="min-w-0">
          <HolderRewardTokenCard
            rank={index + 1}
            name={row.name}
            symbol={row.symbol}
            image={row.image}
            tokenAddress={row.tokenAddress}
            href={coinHref(row.poolAddress)}
            createdAt={row.pool.createdAt}
            links={row.links}
            distributedRaw={row.pool.distributedRaw}
            mcapRaw={row.mcapRaw}
            holdersPaid={row.pool.holdersPaid}
            strategyActive={row.pool.strategyActive}
          />
        </li>
      ))}
    </ol>
  );
}

/**
 * /holder-rewards. The ranked list drives the page: when its read model is
 * absent (real 404/501) or degraded (503), one panel replaces tiles, rail,
 * controls and table. The rail is its own five-row request ordered by
 * rewards so it stays correct while the table is sorted another way.
 */
export default function HolderRewardsView() {
  const [order, setOrder] = useState<HolderRewardsOrder>(defaultOrder);
  const [view, setView] = useState<HolderRewardsViewMode>('table');
  const [hoveredPool, setHoveredPool] = useState<string | null>(null);
  const [filters, setFilters] = useState<HolderRewardsFilters>(defaultFilters);

  const stats = useHolderRewardsStats();
  const list = useHolderRewardsRows(order.key);
  const top = useHolderRewardsRows('rewards', topPayingCount);
  const airdropQuery = useAirdropBalance(hoveredPool);

  const rows = useMemo(() => filterRows(sortRows(list.rows, order), filters), [list.rows, order, filters]);
  const topRows = useMemo(() => sortRows(top.rows, defaultOrder), [top.rows]);

  const airdrop: AirdropHover | null = hoveredPool === null
    ? null
    : airdropQuery.status === 'success'
      ? { pool: hoveredPool, status: 'ready', balance: airdropQuery.data }
      : airdropQuery.status === 'error'
        ? { pool: hoveredPool, status: 'error', message: airdropQuery.error.message }
        : { pool: hoveredPool, status: 'pending' };

  const retryAll = () => { list.retry(); top.retry(); };
  const exportRows = rows.length > 0 ? () => downloadCsv(rowsToCsv(rows)) : null;

  return (
    <div className="mx-auto flex w-full max-w-content-narrow-max-width flex-col gap-8 px-4 py-6">
      <HolderRewardsHeader>
        <StatsRow stats={stats} />
      </HolderRewardsHeader>
      {list.availability ? (
        <HolderRewardsUnavailable availability={list.availability} onRetry={retryAll} />
      ) : (
        <>
          <TopPayingRail rows={topRows} loading={top.loading || top.pricesPending || top.identityPending} />
          <section aria-labelledby="all-holder-rewards-title" className="flex flex-col gap-3">
            <h2 id="all-holder-rewards-title" className="sr-only">All holder rewards coins</h2>
            <HolderRewardsControls order={order} onSortChange={key => setOrder({ key, direction: 'desc' })} view={view} onViewChange={setView} filters={filters} onFiltersChange={setFilters} onExport={exportRows} />
            {view === 'grid' && rows.length > 0 ? (
              <GridView rows={rows} />
            ) : (
              <HolderRewardsTable
                rows={rows}
                order={order}
                onOrderChange={setOrder}
                loading={list.loading}
                filtered={rows.length === 0 && list.rows.length > 0}
                hasMore={list.hasMore}
                loadingMore={list.loadingMore}
                loadMoreFailed={list.loadMoreFailed}
                onLoadMore={list.loadMore}
                pricesUnavailable={list.pricesUnavailable}
                hoveredPool={hoveredPool}
                onHoverPool={setHoveredPool}
                airdrop={airdrop}
              />
            )}
          </section>
        </>
      )}
    </div>
  );
}
