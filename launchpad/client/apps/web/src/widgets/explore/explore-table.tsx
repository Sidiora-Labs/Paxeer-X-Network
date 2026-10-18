'use client';
import type { ReactNode } from 'react';
import { Info } from 'lucide-react';
import { cn } from '@/lib/utils';
import { LoadMore, SortHeader, Tooltip } from '@/ui/primitives';
import { columnSpecs, densityRowClass, type ColumnVisibility, type Density, type OptionalColumn } from './column-settings';
import { athUnavailableReason, ExploreRow } from './explore-row';
import type { SortKey, SortState } from './sort';
import type { ExploreRow as ExploreRowData } from './use-explore-rows';

export interface ExploreTableProps {
  rows: ExploreRowData[];
  sort: SortState;
  onSort: (key: SortKey) => void;
  columns: ColumnVisibility;
  density: Density;
  isStarred: (poolAddress: string) => boolean;
  onToggleStar: (poolAddress: string) => void;
  /** First page still loading. */
  loading: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  moreError: boolean;
  onLoadMore: () => void;
  /** Rendered inside the table when there are no rows and nothing is loading. */
  empty: ReactNode;
  /** Visible sort / filter disclosure under the header. */
  disclosure: string | null;
  caption: string;
}

/** The coin cell is w-36 (md:w-56, xl:min-w-56) plus px-3; the header holds that width when no rows render. */
const coinHeaderMinWidth = 'min-w-42 md:min-w-62';

function SkeletonRows({ count, columns, density }: { count: number; columns: ColumnVisibility; density: Density }) {
  return (
    <>
      {Array.from({ length: count }).map((_, index) => (
        <tr key={index} aria-hidden="true" className={cn(densityRowClass[density], index % 2 === 0 ? 'bg-surface-1' : 'bg-surface-2')}>
          {columnSpecs.map(spec => {
            if (spec.id in columns && !columns[spec.id as OptionalColumn]) return null;
            return (
              <td key={spec.id} className={cn('px-3 align-middle', spec.responsive)}>
                {spec.id === 'coin' ? (
                  <span className="flex w-36 items-center gap-2.5 md:w-56 xl:w-auto xl:min-w-56">
                    <span className="sl-skeleton h-3 w-5 shrink-0" />
                    <span className="sl-skeleton size-avatar-md shrink-0 rounded-pill" />
                    <span className="flex flex-1 flex-col gap-1.5"><span className="sl-skeleton h-3 w-2/3" /><span className="sl-skeleton h-2.5 w-1/3" /></span>
                  </span>
                ) : spec.id === 'graph' ? (
                  <span className="sl-skeleton block h-7 w-24" />
                ) : spec.id === 'star' ? null : (
                  <span className={cn('sl-skeleton inline-block h-3', spec.align === 'right' ? 'w-12' : 'w-16')} />
                )}
              </td>
            );
          })}
        </tr>
      ))}
    </>
  );
}

export function ExploreTable({ rows, sort, onSort, columns, density, isStarred, onToggleStar, loading, hasMore, loadingMore, moreError, onLoadMore, empty, disclosure, caption }: ExploreTableProps) {
  const footer = (
    <>
      {disclosure ? <p role="status" className="px-3 py-2 text-xs text-text-faint">{disclosure}</p> : null}
      {!loading && rows.length > 0 && (hasMore || moreError) ? (
        <LoadMore onLoadMore={onLoadMore} hasMore={hasMore || moreError} loading={loadingMore} error={moreError} label="Load more" />
      ) : null}
    </>
  );

  return (
    <div className="min-w-0 rounded-md bg-surface-1">
      {/* relative clips the sr-only header text; inline-size containment keeps the table's width out of the page's intrinsic size, so it scrolls here instead of widening the page */}
      <div className="relative overflow-x-auto [contain:inline-size]">
        <table className="w-full table-auto">
          <caption className="sr-only">{caption}</caption>
          <thead>
            <tr className="bg-surface-1">
              {columnSpecs.map(spec => {
                if (spec.id in columns && !columns[spec.id as OptionalColumn]) return null;
                const label = spec.id === 'star'
                  ? <span className="sr-only">{spec.label}</span>
                  : spec.id === 'ath'
                    ? (
                      <span className="inline-flex items-center gap-1">
                        <span>{spec.label}</span>
                        <Tooltip content={athUnavailableReason}>
                          <button type="button" aria-label="About all-time high" className="inline-flex size-4 items-center justify-center rounded-xs text-text-faint hover:text-text focus-visible:outline-2 focus-visible:outline-focus">
                            <Info aria-hidden="true" className="size-3" />
                          </button>
                        </Tooltip>
                      </span>
                    )
                    : (
                      <>
                        <span aria-hidden={spec.hint ? true : undefined}>{spec.label}</span>
                        {spec.hint ? <span className="sr-only">{spec.hint}</span> : null}
                      </>
                    );
                return (
                  <SortHeader
                    key={spec.id}
                    label={label}
                    align={spec.align}
                    width={spec.width === 'auto' ? undefined : spec.width}
                    sort={spec.sortKey && sort && sort.key === spec.sortKey ? sort.direction : null}
                    onSort={spec.sortKey ? () => onSort(spec.sortKey as SortKey) : undefined}
                    className={cn('whitespace-nowrap font-medium', spec.id === 'coin' && coinHeaderMinWidth, spec.responsive)}
                  />
                );
              })}
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <SkeletonRows count={8} columns={columns} density={density} />
            ) : rows.length === 0 ? null : (
              rows.map((row, index) => (
                <ExploreRow
                  key={row.poolAddress}
                  row={row}
                  columns={columns}
                  density={density}
                  starred={isStarred(row.poolAddress)}
                  onToggleStar={onToggleStar}
                  tone={index % 2 === 0 ? 'a' : 'b'}
                />
              ))
            )}
          </tbody>
        </table>
      </div>
      {/* Outside the horizontal scroller so the empty or unavailable notice stays centred in the visible card at every width. */}
      {!loading && rows.length === 0 ? <div data-explore-empty>{empty}</div> : null}
      {footer}
    </div>
  );
}
