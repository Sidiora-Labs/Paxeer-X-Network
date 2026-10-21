'use client';
import { ErrorState, UnavailableState } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { formatCount, formatUsdl } from '@/utils/format';
import type { Loadable } from '@/core/market/unsupported';
import type { BountiesStatsResponse } from './bounties-contract';

const EM_DASH = '—';

export interface BountiesStatsProps {
  stats: Loadable<BountiesStatsResponse>;
  /** Count of open bounties from a complete open-list page; used only while IF_BOUNTIES_STATS is not served. */
  openCount: number | null;
  /** The open list is still loading (keeps the live value in its loading state rather than showing a dash). */
  openLoading?: boolean;
  /**
   * The bounty service probe failed. The page explains the outage once in the
   * list panel, so the totals show dashes instead of loading forever or
   * repeating the error.
   */
  serviceDown?: boolean;
  /** `chips` is the compact header row (lg and up); `inline` is two lines of text for narrow headers. */
  variant?: 'chips' | 'inline';
  className?: string;
}

export interface BountyTotal { key: 'live' | 'pool' | 'submissions' | 'paid'; label: string; inlineLabel: string; value: string | null; loading: boolean; positive: boolean }

/** The four totals with only real values: null when no source answered. */
export function bountyTotals({ stats, openCount, openLoading = false, serviceDown = false }: Pick<BountiesStatsProps, 'stats' | 'openCount' | 'openLoading' | 'serviceDown'>): BountyTotal[] {
  const ready = stats.state === 'ready' ? stats.data.data : null;
  const loading = !serviceDown && stats.state === 'loading';
  const live = ready ? ready.openCount : openCount;
  return [
    { key: 'live', label: 'Live', inlineLabel: 'live', value: live === null ? null : formatCount(live), loading: ready === null && live === null && (loading || (!serviceDown && openLoading)), positive: false },
    { key: 'pool', label: 'Unclaimed rewards', inlineLabel: 'unclaimed rewards', value: ready ? formatUsdl(ready.unclaimedPoolUsdlRaw, { compact: true }) : null, loading, positive: true },
    { key: 'submissions', label: 'Submissions', inlineLabel: 'submissions across all', value: ready ? formatCount(ready.submissionsCount) : null, loading, positive: false },
    { key: 'paid', label: 'Paid out', inlineLabel: 'paid out', value: ready ? formatUsdl(ready.paidOutUsdlRaw, { compact: true }) : null, loading, positive: true },
  ];
}

function Value({ total, className }: { total: BountyTotal; className?: string }) {
  if (total.loading) return <span aria-hidden="true" className={cn('sl-skeleton inline-block h-[1em] w-10 align-middle', className)} />;
  if (total.value === null) return <span aria-label="No data" className={cn('text-text-faint', className)}>{EM_DASH}</span>;
  return <span data-numeric className={cn('tabular-nums', total.positive ? 'text-success' : 'text-text', className)}>{total.value}</span>;
}

/**
 * Header totals. Live comes from IF_BOUNTIES_STATS when served, else from a
 * complete open-list page. The money/submission totals exist only on
 * IF_BOUNTIES_STATS; until it is served they show a dash. A failed totals
 * request while the service itself answers gets one short inline note.
 */
export function BountiesStats({ stats, openCount, openLoading = false, serviceDown = false, variant = 'chips', className }: BountiesStatsProps) {
  const totals = bountyTotals({ stats, openCount, openLoading, serviceDown });
  const note = serviceDown ? null
    : stats.state === 'unsupported' ? <UnavailableState variant={variant === 'chips' ? 'chip' : 'inline'} title="Bounty totals" plural="auto" operation={stats.operation} status={stats.status} />
    : stats.state === 'error' ? <ErrorState variant="inline" title="Totals can't load right now" message="Totals can't load right now, try again" onRetry={stats.retry} />
    : null;

  if (variant === 'inline') {
    const [live, pool, submissions, paid] = totals;
    const item = (total: BountyTotal) => (
      <div key={total.key} className="inline-flex flex-row-reverse items-baseline gap-1">
        <dt className="text-text-muted">{total.inlineLabel}</dt>
        <dd className="font-bold"><Value total={total} /></dd>
      </div>
    );
    return (
      <div className={cn('flex flex-col gap-1', className)}>
        <dl aria-label="Bounty totals" className="flex flex-col gap-0.5 text-sm">
          <div className="flex flex-wrap items-baseline gap-x-2">{item(live)}<span aria-hidden="true" className="text-text-faint">·</span>{item(pool)}</div>
          <div className="flex flex-wrap items-baseline gap-x-2">{item(submissions)}<span aria-hidden="true" className="text-text-faint">·</span>{item(paid)}</div>
        </dl>
        {note}
      </div>
    );
  }

  return (
    <div className={cn('flex flex-col items-end gap-1', className)}>
      <dl aria-label="Bounty totals" className="flex flex-nowrap items-stretch gap-2">
        {totals.map(total => (
          <div key={total.key} data-stat={total.key} className="flex min-w-16 flex-col-reverse justify-center gap-0.5 rounded-md bg-surface-1 px-3 py-1.5">
            <dt className="whitespace-nowrap text-2xs font-bold uppercase tracking-[.06em] text-text-faint">{total.label}</dt>
            <dd className="text-base font-bold leading-5"><Value total={total} /></dd>
          </div>
        ))}
      </dl>
      {note}
    </div>
  );
}
