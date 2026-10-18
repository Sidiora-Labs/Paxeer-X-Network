'use client';
import { useId } from 'react';
import Link from 'next/link';
import { Heart, Trophy, Users } from 'lucide-react';
import { Avatar, CountdownProgress, Skeleton, StatusBadge } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { formatCount, shortAddress } from '@/utils/format';
import { BountyReward } from './bounty-reward';
import { BountyTokenChip } from './bounty-token-chip';
import { bountiesContract, type Bounty, type PoolStats, type ValuationContext } from './bounties-contract';

export interface BountyCardCreator { label: string; avatarUrl: string | null }

export interface BountyCardProps {
  bounty: Bounty;
  creator?: BountyCardCreator;
  /** Batched pool stats for the linked token chip. */
  stats?: PoolStats | null;
  /** Valuation context for the reward (rails and tables that already resolved the pool). */
  valuationContext?: ValuationContext;
  href?: string;
  now?: number;
  className?: string;
}

/**
 * Bounty card built from the real contract fields only. Cover art, the
 * countdown, the winner count and the like count render when the service
 * sends them and are omitted, not stubbed, when it does not.
 */
export function BountyCard({ bounty, creator, stats, valuationContext, href, now, className }: BountyCardProps) {
  const titleId = useId();
  const badge = bountiesContract.badgeFor(bounty, now);
  const target = href ?? `/bounties/${encodeURIComponent(bounty.bountyId)}`;
  const creatorLabel = creator?.label ?? shortAddress(bounty.creatorWallet);
  const counts = [
    bounty.submissionsCount !== undefined ? { icon: Users, value: bounty.submissionsCount, label: 'submissions' } : null,
    bounty.winnersCount !== undefined ? { icon: Trophy, value: bounty.winnersCount, label: 'winners' } : null,
    bounty.likesCount !== undefined ? { icon: Heart, value: bounty.likesCount, label: 'likes' } : null,
  ].filter((entry): entry is { icon: typeof Users; value: number; label: string } => entry !== null);

  return (
    <article
      data-state={bounty.state}
      aria-labelledby={titleId}
      className={cn('group relative flex min-w-0 flex-col gap-3 rounded-lg bg-surface-1 p-3 transition-colors duration-fast hover:bg-surface-hover focus-within:bg-surface-hover', className)}
    >
      <div className="flex items-start justify-between gap-2">
        <StatusBadge status={badge.status}>{badge.label}</StatusBadge>
        {bounty.poolAddress ? <span className="relative z-10 min-w-0"><BountyTokenChip poolAddress={bounty.poolAddress} stats={stats} /></span> : null}
      </div>
      <h3 id={titleId} className="line-clamp-2 text-md font-bold text-text">
        <Link href={target} className="after:absolute after:inset-0 after:rounded-lg after:content-['']">
          {bounty.title}
        </Link>
      </h3>
      <div className="flex items-center gap-2 text-sm text-text-muted">
        <Avatar src={creator?.avatarUrl ?? null} name={creatorLabel} size="sm" decorative />
        <span className="truncate">{creatorLabel}</span>
      </div>
      <BountyReward bounty={bounty} context={valuationContext} />
      {bounty.deadlineAt ? <CountdownProgress endsAt={bounty.deadlineAt} variant="bar" now={now} /> : null}
      {counts.length > 0 ? (
        <footer className="flex flex-wrap items-center gap-3 text-xs text-text-faint">
          {counts.map(entry => (
            <span key={entry.label} className="inline-flex items-center gap-1 tabular-nums" data-numeric>
              <entry.icon aria-hidden="true" className="size-icon-sm" />
              <span>{formatCount(entry.value)} {entry.label}</span>
            </span>
          ))}
        </footer>
      ) : null}
      <p className="sr-only">Status {badge.label}. Created by {creatorLabel}.</p>
    </article>
  );
}

export function BountyCardSkeleton({ className }: { className?: string }) {
  return (
    <div aria-hidden="true" className={cn('flex flex-col gap-3 rounded-lg bg-surface-1 p-3', className)}>
      <div className="flex items-center justify-between"><Skeleton variant="pill" width={56} height={20} /><Skeleton variant="pill" width={112} height={22} /></div>
      <Skeleton width="80%" height={20} />
      <Skeleton width="55%" height={20} />
      <div className="flex items-center gap-2"><Skeleton variant="circle" width={24} height={24} /><Skeleton width={96} height={14} /></div>
      <div className="flex items-center gap-2"><Skeleton width={72} height={24} /><Skeleton variant="pill" width={96} height={22} /></div>
    </div>
  );
}
