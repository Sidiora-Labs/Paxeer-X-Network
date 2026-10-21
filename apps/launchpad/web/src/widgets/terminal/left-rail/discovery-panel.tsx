'use client';
import { useId, useMemo, useState } from 'react';
import { Filter } from 'lucide-react';
import { cn } from '@/lib/utils';
import { technicalDetail, userFacingMessage } from '@/core/user-facing-error';
import { useExploreRows } from '@/widgets/explore/use-explore-rows';
import { Button, EmptyState, ErrorState, IconButton, LoadMore, SegmentedControl, UnavailableState } from '@/ui/primitives';
import { TableRowsSkeleton } from '@/ui/shared/skeletons';
import { PairRow } from './pair-row';
import { toRailPairRow, type RailPairRow } from './rows';
import { railTimeframes, timeframeServed, unservedWindowReason, type RailTimeframe } from './timeframe';
import { useNow } from './use-now';

export type DiscoveryCategory = 'trending' | 'new';

export interface DiscoveryPanelProps {
  title: string;
  category: DiscoveryCategory;
  activePool: string | null;
  className?: string;
}

export function matchesRailFilter(row: Pick<RailPairRow, 'name' | 'symbol' | 'poolAddress'>, filter: string): boolean {
  const needle = filter.trim().toLowerCase();
  if (!needle) return true;
  return row.name.toLowerCase().includes(needle) || row.symbol.toLowerCase().includes(needle) || row.poolAddress.includes(needle);
}

export function DiscoveryPanel({ title, category, activePool, className }: DiscoveryPanelProps) {
  const headingId = useId();
  const filterId = useId();
  const [timeframe, setTimeframe] = useState<RailTimeframe>(category === 'new' ? '5m' : '1h');
  const [filterOpen, setFilterOpen] = useState(false);
  const [filter, setFilter] = useState('');
  const state = useExploreRows(category);
  const now = useNow(category === 'new' ? 1000 : 30_000);
  const active = activePool?.toLowerCase() ?? null;

  const metadataRows = state.metadata.state === 'ready' ? state.metadata.data : null;
  const rows = useMemo(() => state.rows.map(row => toRailPairRow(
    row,
    row.tokenAddress ? metadataRows?.[row.tokenAddress] ?? null : null,
    timeframe,
  )), [state.rows, metadataRows, timeframe]);
  const visible = useMemo(() => rows.filter(row => matchesRailFilter(row, filter)), [rows, filter]);

  const { ranking } = state;
  // The statistics read itself failed or is not deployed; per-row reasons cover everything else.
  const statsDetail = state.stats.state === 'unsupported' ? technicalDetail(state.stats.operation, state.stats.status) : null;
  let body;
  if (ranking.state === 'loading') {
    body = <div role="status" aria-label={`Loading ${title.toLowerCase()}`}><TableRowsSkeleton rows={6} columns={3} dense /></div>;
  } else if (ranking.state === 'unsupported') {
    body = <UnavailableState title={title} operation={ranking.operation} status={ranking.status} message={ranking.message} onRetry={state.reload} className="p-4" />;
  } else if (ranking.state === 'error') {
    body = <ErrorState title={`${title} can't load right now`} message={userFacingMessage(ranking.error)} onRetry={ranking.retry} className="p-4" />;
  } else if (rows.length === 0) {
    body = <EmptyState variant="inline" title={category === 'new' ? 'No new coins yet.' : 'No coins trending yet.'} body={category === 'new' ? 'New launches appear here as soon as they are created.' : 'Rankings appear as soon as coins trade.'} className="px-2 py-3" />;
  } else {
    body = (
      <>
        {visible.length === 0
          ? <EmptyState variant="inline" announce title="No coins match this filter" className="px-2 py-3" />
          : <ul aria-labelledby={headingId}>{visible.map(row => <PairRow key={row.poolAddress} row={row} active={row.poolAddress === active} now={now} />)}</ul>}
        {state.stats.state === 'unsupported' || state.stats.state === 'error'
          ? <p role="status" title={statsDetail ?? undefined} data-operation={statsDetail ?? undefined} className="px-2 py-1 text-2xs text-text-faint">{state.stats.state === 'error' ? "Changes can't load right now" : 'Changes are not available yet'}</p>
          : null}
        {state.hasMore || state.moreError
          ? <LoadMore onLoadMore={state.moreError ? state.retryMore : state.loadMore} hasMore={state.hasMore} loading={state.loadingMore} error={state.moreError !== null} auto={false} label="More pairs" />
          : null}
      </>
    );
  }

  return (
    <section aria-labelledby={headingId} className={cn('flex min-h-0 flex-col bg-surface-1', className)}>
      <header className="flex h-7 shrink-0 items-center gap-1 px-2">
        <h2 id={headingId} className="mr-auto truncate text-sm font-bold text-text">{title}</h2>
        <SegmentedControl
          label={`${title} change window`}
          size="sm"
          shape="sm"
          options={railTimeframes.map(option => ({ value: option.value, label: option.label, ariaLabel: timeframeServed(option.value) ? `${option.label} change` : `${option.label} change, coming soon` }))}
          value={timeframe}
          onChange={setTimeframe}
        />
        <IconButton
          label={filterOpen ? `Hide ${title.toLowerCase()} filter` : `Filter ${title.toLowerCase()}`}
          icon={<Filter />}
          size="sm"
          pressed={filterOpen || filter !== ''}
          aria-controls={filterOpen ? filterId : undefined}
          onClick={() => setFilterOpen(open => !open)}
        />
      </header>
      <p className="sr-only">
        {timeframeServed(timeframe)
          ? `Price change over the last ${timeframe}.`
          : unservedWindowReason(timeframe)}
      </p>
      {filterOpen
        ? (
          <div className="flex shrink-0 items-center gap-1 px-2 pb-1">
            <label htmlFor={filterId} className="sr-only">{`Filter ${title.toLowerCase()} by name, symbol or address`}</label>
            <input
              id={filterId}
              type="search"
              value={filter}
              autoFocus
              onChange={event => setFilter(event.target.value)}
              onKeyDown={event => { if (event.key === 'Escape') { setFilter(''); setFilterOpen(false); } }}
              placeholder="Name, symbol or address"
              className="h-7 min-w-0 flex-1 rounded-sm bg-surface-2 px-2 text-xs text-text placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-focus"
            />
            {filter !== '' ? <Button variant="ghost" size="sm" onClick={() => setFilter('')}>Clear</Button> : null}
          </div>
        )
        : null}
      <div aria-hidden="true" className="flex h-6 shrink-0 items-center justify-between px-2 text-2xs uppercase text-text-faint">
        <span>Coin</span>
        <span>Market cap</span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">{body}</div>
    </section>
  );
}
