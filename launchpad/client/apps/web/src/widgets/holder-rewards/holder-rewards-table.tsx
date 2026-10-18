'use client';

import Link from 'next/link';
import { ArrowDown, ArrowUp, ArrowUpDown, HandCoins, ListFilter } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { Unsupported } from '@/core/market/unsupported';
import { AgeChip, Avatar, Button, EmptyState, LoadMore, StatusBadge, Tooltip } from '@/ui/primitives';
import CopyAddress from '@/ui/shared/copy-address';
import { TokenLinkIcons, UsdlBadge } from './holder-reward-token-card';
import { launchHolderRewardsHref } from './holder-rewards-header';
import { airdropText, coinHref, holdersPaidText, mcapText, nextOrder, rewardsText, usdlAmountText, type HolderRewardRow, type HolderRewardsOrder, type HolderRewardsSortKey } from './holder-rewards-presentation';

/** Result of the single on-chain `getAirdropBalance` read for the row under the pointer. */
export type AirdropHover =
  | { pool: string; status: 'pending' }
  | { pool: string; status: 'ready'; balance: bigint }
  | { pool: string; status: 'error'; message: string };

export interface HolderRewardsTableProps {
  rows: readonly HolderRewardRow[];
  order: HolderRewardsOrder;
  onOrderChange(order: HolderRewardsOrder): void;
  loading?: boolean;
  /** Rows were loaded but the active filters hide all of them. */
  filtered?: boolean;
  hasMore: boolean;
  loadingMore?: boolean;
  loadMoreFailed?: boolean;
  onLoadMore(): void;
  pricesUnavailable?: Unsupported | null;
  hoveredPool?: string | null;
  onHoverPool?(pool: string | null): void;
  airdrop?: AirdropHover | null;
  className?: string;
}

const columns: ReadonlyArray<{ key: HolderRewardsSortKey; label: string; width: string }> = [
  { key: 'rewards', label: 'Holder rewards', width: 'w-[124px] md:w-[168px]' },
  { key: 'mcap', label: 'Market cap', width: 'hidden w-[132px] md:table-cell' },
  { key: 'holders', label: 'Holders paid', width: 'hidden w-[132px] md:table-cell' },
];

/** Sentence-case sortable column head (the shared SortHeader is uppercase by design). */
function ColumnHead({ label, sr, sort, onSort, className }: { label: string; sr?: string; sort: 'asc' | 'desc' | null; onSort(): void; className?: string }) {
  const Icon = sort === 'asc' ? ArrowUp : sort === 'desc' ? ArrowDown : ArrowUpDown;
  return (
    <th scope="col" aria-sort={sort === 'asc' ? 'ascending' : sort === 'desc' ? 'descending' : 'none'} className={cn('h-10 px-2 text-xs font-medium md:px-3', sort ? 'text-text' : 'text-text-faint', className)}>
      <button type="button" onClick={onSort} className="ml-auto flex h-full items-center justify-end gap-1 whitespace-nowrap rounded-xs transition-colors hover:text-text">
        <span>{label}{sr ? <span className="sr-only"> {sr}</span> : null}</span>
        <Icon aria-hidden="true" className={cn('size-3', sort ? 'text-text' : 'opacity-0 group-hover/head:opacity-60')} />
        <span className="sr-only">{sort === 'asc' ? ', sorted ascending' : sort === 'desc' ? ', sorted descending' : ', sortable'}</span>
      </button>
    </th>
  );
}

function AirdropLine({ airdrop }: { airdrop: AirdropHover }) {
  if (airdrop.status === 'pending') return <span role="status" className="text-2xs text-text-faint">Loading pending airdrop...</span>;
  if (airdrop.status === 'error') return <span role="status" className="text-2xs text-text-faint" >Pending airdrop not available yet</span>;
  return <span role="status" data-numeric className="text-2xs tabular-nums text-text-muted">Pending airdrop {airdropText(airdrop.balance)}</span>;
}

function RowsSkeleton() {
  return (
    <>
      {Array.from({ length: 8 }).map((_, index) => (
        <tr key={index} aria-hidden="true" className="h-table-row-comfortable odd:bg-surface-2">
          <td className="px-3"><span className="sl-skeleton block h-3 w-4" /></td>
          <td className="px-3">
            <span className="flex items-center gap-3">
              <span className="sl-skeleton size-avatar-xl rounded-sm" />
              <span className="flex flex-1 flex-col gap-2"><span className="sl-skeleton h-3 w-32" /><span className="sl-skeleton h-3 w-20" /></span>
            </span>
          </td>
          <td className="px-3"><span className="sl-skeleton ml-auto block h-3 w-20" /></td>
          <td className="hidden px-3 md:table-cell"><span className="sl-skeleton ml-auto block h-3 w-16" /></td>
          <td className="hidden px-3 md:table-cell"><span className="sl-skeleton ml-auto block h-3 w-12" /></td>
        </tr>
      ))}
    </>
  );
}

/**
 * Ranked holder-rewards table. Rows alternate surface-1 / surface-2 so they
 * separate by tone alone; the coin name carries the row link overlay while
 * the copy and external-link buttons stay above it. Below md the market cap,
 * holders paid and age fold into the coin cell as a second line.
 */
export default function HolderRewardsTable({
  rows,
  order,
  onOrderChange,
  loading = false,
  filtered = false,
  hasMore,
  loadingMore = false,
  loadMoreFailed = false,
  onLoadMore,
  pricesUnavailable = null,
  hoveredPool = null,
  onHoverPool,
  airdrop = null,
  className,
}: HolderRewardsTableProps) {
  const empty = !loading && rows.length === 0 && !filtered;
  return (
    <div className={cn('flex flex-col gap-2 rounded-lg bg-surface-1 p-2', className)}>
      <table className="w-full table-fixed">
        <caption className="sr-only">Holder rewards coins ranked by {order.key === 'age' ? 'creation time' : order.key === 'mcap' ? 'market cap' : order.key === 'holders' ? 'holders paid' : 'rewards distributed'}, {order.direction === 'desc' ? 'highest first' : 'lowest first'}</caption>
        <thead>
          <tr className="group/head">
            <th scope="col" className="h-10 w-8 px-1 text-center text-xs font-medium text-text-faint md:w-12 md:px-3">#</th>
            <th scope="col" className="h-10 px-2 text-left md:px-3 text-xs font-medium text-text-faint">Coin</th>
            {columns.map(column => (
              <ColumnHead
                key={column.key}
                label={column.label}
                sr={column.key === 'mcap' ? 'in USDL' : undefined}
                sort={order.key === column.key ? order.direction : null}
                onSort={() => onOrderChange(nextOrder(order, column.key))}
                className={column.width}
              />
            ))}
          </tr>
        </thead>
        <tbody onMouseLeave={onHoverPool ? () => onHoverPool(null) : undefined}>
          {loading && rows.length === 0 ? <RowsSkeleton /> : null}
          {empty ? (
            <EmptyState
              variant="table"
              colSpan={5}
              icon={HandCoins}
              title="No holder rewards paid yet"
              body="Coins that pay holders show up here after their first payout"
              action={<Button variant="secondary" size="sm" href={launchHolderRewardsHref}>Launch one</Button>}
            />
          ) : null}
          {filtered ? (
            <EmptyState
              variant="table"
              colSpan={5}
              icon={ListFilter}
              title="No coins match these filters"
              body="Clear a filter or load more coins"
            />
          ) : null}
          {rows.map((row, index) => {
            const hovered = hoveredPool === row.poolAddress;
            const href = coinHref(row.poolAddress);
            const mcap = row.mcapRaw === null ? null : mcapText(row.mcapRaw);
            return (
              <tr
                key={row.poolAddress}
                className={cn('group relative h-table-row-comfortable transition-colors duration-fast odd:bg-surface-2 hover:bg-surface-hover focus-within:bg-surface-hover', hovered && 'bg-surface-hover')}
                onMouseEnter={onHoverPool ? () => onHoverPool(row.poolAddress) : undefined}
                onFocus={onHoverPool ? () => onHoverPool(row.poolAddress) : undefined}
              >
                <td data-numeric className="px-1 text-center align-middle text-base font-bold md:px-3 md:text-md tabular-nums text-text"><span className="sr-only">Rank </span>{index + 1}</td>
                <td className="min-w-0 px-2 py-2 align-middle md:px-3">
                  <div className="flex min-w-0 items-center gap-2 md:gap-3">
                    <Avatar src={row.image} name={row.name} size="lg" shape="square" decorative className="md:size-avatar-xl" />
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 items-center gap-1">
                        <Link href={href} className="block truncate text-base font-semibold text-text after:absolute after:inset-0 after:content-['']" title={row.name}>
                          {row.name}
                          <span className="sr-only">
                            , {rewardsText(row.pool.distributedRaw)} distributed to holders
                            {mcap ? `, market cap ${mcap} USDL` : ', market cap not available yet'}
                            , {holdersPaidText(row.pool.holdersPaid)} holders paid
                          </span>
                        </Link>
                        <CopyAddress address={row.tokenAddress} variant="icon" className="relative z-10 shrink-0" />
                      </div>
                      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-text-faint">
                        {row.symbol ? <span className="truncate font-bold uppercase" title={row.symbol}>${row.symbol}</span> : null}
                        <AgeChip iso={row.pool.createdAt} className="font-bold text-warning" />
                        <UsdlBadge />
                        <TokenLinkIcons links={row.links} tokenAddress={row.tokenAddress} />
                        {!row.pool.strategyActive ? <StatusBadge status="neutral">Strategy changed</StatusBadge> : null}
                      </div>
                      <dl className="mt-1 flex flex-wrap gap-x-3 text-2xs text-text-faint md:hidden">
                        <div className="flex items-center gap-1"><dt>MC</dt><dd data-numeric className={cn('tabular-nums', mcap ? 'text-text' : 'text-text-faint')}>{mcap ? `${mcap} USDL` : '—'}</dd></div>
                        <div className="flex items-center gap-1"><dt>Paid</dt><dd data-numeric className="tabular-nums text-text">{holdersPaidText(row.pool.holdersPaid)}</dd></div>
                      </dl>
                    </div>
                  </div>
                </td>
                <td className="px-2 text-right align-middle md:px-3">
                  <div className="flex flex-col items-end gap-0.5">
                    <span data-numeric className="whitespace-nowrap text-md font-bold text-success md:text-lg [font-variant-numeric:proportional-nums]">{usdlAmountText(row.pool.distributedRaw)}<span className="ml-1 text-2xs font-semibold text-text-faint">USDL</span></span>
                    {hovered && airdrop && airdrop.pool === row.poolAddress ? <AirdropLine airdrop={airdrop} /> : null}
                  </div>
                </td>
                <td className="hidden px-3 text-right align-middle md:table-cell">
                  {mcap ? (
                    <span data-numeric className="whitespace-nowrap text-md font-bold tabular-nums text-text">{mcap} <span className="text-2xs font-semibold text-text-faint">USDL</span></span>
                  ) : pricesUnavailable ? (
                    <Tooltip content="Market cap not available yet">
                      <span className="text-text-faint" aria-label="Market cap not available yet">{'—'}</span>
                    </Tooltip>
                  ) : (
                    <span className="text-text-faint" aria-label="Loading market cap">{'—'}</span>
                  )}
                </td>
                <td className="hidden px-3 text-right align-middle md:table-cell">
                  <span data-numeric className="text-md font-bold tabular-nums text-text">{holdersPaidText(row.pool.holdersPaid)}</span>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {rows.length > 0 ? (
        <>
          {hasMore ? <p className="px-3 text-xs text-text-faint">Ranked across the {rows.length} coins shown</p> : null}
          <LoadMore hasMore={hasMore} loading={loadingMore} error={loadMoreFailed} onLoadMore={onLoadMore} auto={false} />
        </>
      ) : null}
    </div>
  );
}
