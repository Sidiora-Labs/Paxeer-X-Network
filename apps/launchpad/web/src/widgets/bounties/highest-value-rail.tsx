'use client';
import type { ReactNode } from 'react';
import Link from 'next/link';
import { BadgeCheck, Flame, Users } from 'lucide-react';
import { Button, CountdownProgress, EmptyState, ErrorState, RankBadge, Skeleton, UnavailableState } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { formatCount, formatUsdl } from '@/utils/format';
import { bountiesContract, type Bounty, type BountyValuation } from './bounties-contract';
import { useBountyValuations } from './bounty-reward';
import { BountyTokenChip } from './bounty-token-chip';
import { useBounties } from './use-bounties';

export interface HighestValueRailProps {
  /** False while the service probe has not answered or failed: no request is made. */
  enabled?: boolean;
  /** The service probe failed: the page explains the outage once, so the card shows a short muted line instead of loading forever. */
  serviceDown?: boolean;
  limit?: number;
  allHref?: string;
  now?: number;
  className?: string;
}

function bountyHref(bounty: Bounty): string {
  return `/bounties/${encodeURIComponent(bounty.bountyId)}`;
}

/** Card eyebrow in the reference density: 11px uppercase label with a small text link. */
function CardHeader({ id, icon, title, href, action }: { id: string; icon: ReactNode; title: string; href: string; action: string }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <h2 id={id} className="inline-flex min-w-0 items-center gap-1.5 text-2xs font-bold uppercase tracking-[.08em] text-text-muted">{icon}<span className="truncate">{title}</span></h2>
      <Button variant="link" size="sm" href={href} className="text-xs">{action}</Button>
    </div>
  );
}

function RewardValue({ valuation, size }: { valuation: BountyValuation | null; size: 'lg' | 'sm' }) {
  if (!valuation) return <span className="text-xs text-text-faint">Value not available yet</span>;
  return (
    <span data-numeric className={cn('font-bold tabular-nums text-success', size === 'lg' ? 'text-xl' : 'text-sm')}>
      {valuation.exact ? null : <span title="Estimated from the coin's market cap"><span aria-hidden="true">~</span><span className="sr-only">approximately </span></span>}
      {formatUsdl(valuation.usdlRaw, { compact: true })}
    </span>
  );
}

function Meta({ bounty, now }: { bounty: Bounty; now?: number }) {
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-text-faint">
      {bounty.poolAddress ? <span className="relative z-10"><BountyTokenChip poolAddress={bounty.poolAddress} /></span> : null}
      {bounty.deadlineAt ? <CountdownProgress endsAt={bounty.deadlineAt} variant="icon" now={now} /> : null}
      {bounty.submissionsCount !== undefined ? (
        <span className="inline-flex items-center gap-1 tabular-nums" data-numeric><Users aria-hidden="true" className="size-icon-sm" />{formatCount(bounty.submissionsCount)}<span className="sr-only"> submissions</span></span>
      ) : null}
    </span>
  );
}

/**
 * Open bounties ranked by USDL reward. Reads the same IF_BOUNTY_LIST page as
 * the Bounties tab (shared query key) and values rewards through the pool
 * valuation path; rewards without a real value rank last.
 */
export function HighestValueRail({ enabled = true, serviceDown = false, limit = 5, allHref = '/bounties?tab=open', now, className }: HighestValueRailProps) {
  const list = useBounties({ state: 'open' }, { enabled });
  const valuations = useBountyValuations(list.items);
  const rows = bountiesContract.rankByValue(list.items.map(bounty => ({ bounty, valuation: valuations.valuationOf(bounty) }))).slice(0, limit);
  const [featured, ...rest] = rows;

  return (
    <section aria-labelledby="highest-value-title" className={cn('flex flex-col gap-3 rounded-lg bg-surface-1 p-4', className)}>
      <CardHeader id="highest-value-title" icon={<Flame aria-hidden="true" className="size-icon-sm text-accent" />} title="Highest value · open" href={allHref} action="All open" />
      {serviceDown ? (
        <p className="text-sm text-text-faint">Bounties can't load right now</p>
      ) : list.loadable.state === 'loading' ? (
        <div className="flex flex-col gap-2" aria-busy="true"><span className="sr-only">Loading open bounties</span><Skeleton variant="block" height={96} /><Skeleton height={40} /><Skeleton height={40} /></div>
      ) : list.loadable.state === 'unsupported' ? (
        <UnavailableState variant="inline" title="Open bounties" plural="auto" operation={list.loadable.operation} status={list.loadable.status} />
      ) : list.loadable.state === 'error' ? (
        <ErrorState variant="inline" title="Open bounties can't load right now" message="Open bounties can't load right now, try again" onRetry={list.loadable.retry} />
      ) : !featured ? (
        <EmptyState variant="inline" title="No open bounties yet" body="The biggest rewards show up here first" />
      ) : (
        <ol className="flex flex-col gap-2">
          <li className="relative flex flex-col gap-1 rounded-md bg-accent-soft p-3 transition-colors duration-fast hover:bg-accent-soft-strong">
            <div className="flex items-center gap-2">
              <RankBadge rank={1} variant="disc" />
              <span className="text-2xs font-bold uppercase tracking-[.04em] text-text-faint">Top open bounty</span>
            </div>
            <RewardValue valuation={featured.valuation} size="lg" />
            <Link href={bountyHref(featured.bounty)} className="line-clamp-2 text-sm font-bold text-text after:absolute after:inset-0 after:rounded-md after:content-['']">{featured.bounty.title}</Link>
            <Meta bounty={featured.bounty} now={now} />
          </li>
          {rest.map((row, index) => (
            <li key={row.bounty.bountyId} className="relative flex items-center gap-2 rounded-md p-2 transition-colors duration-fast hover:bg-surface-hover">
              <RankBadge rank={index + 2} variant="disc" />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <Link href={bountyHref(row.bounty)} className="truncate text-sm font-bold text-text after:absolute after:inset-0 after:rounded-md after:content-['']">{row.bounty.title}</Link>
                <Meta bounty={row.bounty} now={now} />
              </span>
              <RewardValue valuation={row.valuation} size="sm" />
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

export interface RecentlyPaidCardProps {
  enabled?: boolean;
  serviceDown?: boolean;
  limit?: number;
  allHref?: string;
  className?: string;
}

/**
 * Bounties whose winners were settled from escrow, in the order the
 * IF_BOUNTY_LIST `state=paid` page returns them. Each row is a real paid
 * bounty with its reward value; nothing is shown that the service did not list.
 */
export function RecentlyPaidCard({ enabled = true, serviceDown = false, limit = 6, allHref = '/bounties?tab=paid', className }: RecentlyPaidCardProps) {
  const list = useBounties({ state: 'paid' }, { enabled });
  const rows = list.items.slice(0, limit);
  const valuations = useBountyValuations(rows);
  return (
    <section aria-labelledby="recently-paid-title" className={cn('flex flex-col gap-3 rounded-lg bg-surface-1 p-4', className)}>
      <CardHeader id="recently-paid-title" icon={<BadgeCheck aria-hidden="true" className="size-icon-sm text-success" />} title="Recently paid" href={allHref} action="All paid" />
      {serviceDown ? (
        <p className="text-sm text-text-faint">Payouts show up here once bounties load</p>
      ) : list.loadable.state === 'loading' ? (
        <div className="flex flex-col gap-2" aria-busy="true"><span className="sr-only">Loading paid bounties</span><Skeleton height={32} /><Skeleton height={32} /><Skeleton height={32} /></div>
      ) : list.loadable.state === 'unsupported' ? (
        <UnavailableState variant="inline" title="Paid bounties" plural="auto" operation={list.loadable.operation} status={list.loadable.status} />
      ) : list.loadable.state === 'error' ? (
        <ErrorState variant="inline" title="Paid bounties can't load right now" message="Paid bounties can't load right now, try again" onRetry={list.loadable.retry} />
      ) : rows.length === 0 ? (
        <EmptyState variant="inline" title="No bounties paid out yet" body="Paid rewards show up here" />
      ) : (
        <ul className="flex flex-col gap-1">
          {rows.map(bounty => (
            <li key={bounty.bountyId} className="relative flex items-center gap-2 rounded-md p-2 transition-colors duration-fast hover:bg-surface-hover">
              <Link href={bountyHref(bounty)} className="min-w-0 flex-1 truncate text-sm font-bold text-text after:absolute after:inset-0 after:rounded-md after:content-['']">{bounty.title}</Link>
              {bounty.winnersCount !== undefined ? <span className="shrink-0 text-xs tabular-nums text-text-faint" data-numeric>{formatCount(bounty.winnersCount)} {bounty.winnersCount === 1 ? 'winner' : 'winners'}</span> : null}
              <RewardValue valuation={valuations.valuationOf(bounty)} size="sm" />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/**
 * Highest value and recently paid cards. Beside the list from xl; stacked
 * full width below the list from md to xl (the page orders the regions); a horizontal scroller with the two
 * cards side by side on phones.
 */
export function BountyHighlights({ enabled = true, serviceDown = false, className }: { enabled?: boolean; serviceDown?: boolean; className?: string }) {
  const card = 'w-[85%] shrink-0 snap-start md:w-auto';
  return (
    <div className={cn('-mx-4 flex snap-x snap-mandatory gap-3 overflow-x-auto scroll-px-4 px-4 [scrollbar-width:none] md:mx-0 md:scroll-px-0 md:snap-none md:flex-col md:overflow-visible md:px-0', className)}>
      <HighestValueRail enabled={enabled} serviceDown={serviceDown} className={card} />
      <RecentlyPaidCard enabled={enabled} serviceDown={serviceDown} className={card} />
    </div>
  );
}
