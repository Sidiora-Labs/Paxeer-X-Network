'use client';
import { useId, useRef, useState } from 'react';
import Link from 'next/link';
import { ArrowLeftRight, Link2, Pencil, TrendingDown, TrendingUp } from 'lucide-react';
import { toast } from 'sonner';
import { cn } from '@/lib/utils';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { AgeChip, Avatar, Chip, IconButton, Skeleton } from '@/ui/primitives';
import { shortAddress } from '@/utils/format';
import { EmbeddedCoinCard, type EmbeddedStatsState } from './embedded-coin-card';
import { FollowButton } from './follow-button';
import { ReportMenu } from './report-menu';
import type { CalloutView } from './callout-model';

export interface CalloutCardProps {
  callout: CalloutView;
  /** Live stats for the embedded pool, from the surrounding batch request. */
  stats: EmbeddedStatsState;
  /** in-feed cards sit in a surface-1 column; in-rail cards are surface-1 cards on canvas. */
  variant?: 'feed' | 'rail';
  /** Lines shown before "Show more". */
  clampLines?: 3 | 4;
  className?: string;
}

const clampClass = { 3: 'line-clamp-3', 4: 'line-clamp-4' } as const;

/**
 * Callout card (components.md 33 and 34). Author row with avatar, name,
 * relative time, follow and more menu; clamped body with "Show more"; the coin
 * embed with declared position and live MC; an action row limited to the
 * actions the backend serves (copy link, open trade). Like, repost, reply and
 * view counters are omitted until a counter contract exists.
 */
export function CalloutCard({ callout, stats, variant = 'feed', clampLines = 4, className }: CalloutCardProps) {
  const headingId = useId();
  const bodyRef = useRef<HTMLParagraphElement | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [clamped, setClamped] = useState(false);
  const wallets = callout.authorWallet ? [callout.authorWallet] : [];
  const { identities, isPending } = useWalletIdentities(wallets);
  const identity = callout.authorWallet ? identities[callout.authorWallet] : undefined;
  const authorLabel = identity?.displayName ?? (callout.authorWallet ? shortAddress(callout.authorWallet) : `${callout.authorId.slice(0, 8)}…`);
  const profileHref = callout.authorWallet ? `/profile/${callout.authorWallet}` : null;
  const removed = callout.removed;

  function measure(node: HTMLParagraphElement | null) {
    bodyRef.current = node;
    if (node && !expanded) setClamped(node.scrollHeight > node.clientHeight + 1);
  }

  function expand() {
    setExpanded(true);
    requestAnimationFrame(() => bodyRef.current?.focus());
  }

  async function copyLink() {
    const target = callout.poolAddress ? `${window.location.origin}/coin/${callout.poolAddress}?tab=callouts&callout=${encodeURIComponent(callout.id)}` : window.location.href;
    try {
      await navigator.clipboard.writeText(target);
      toast.success('Link copied');
    } catch {
      toast.error("Couldn't copy link");
    }
  }

  return (
    <article aria-labelledby={headingId} className={cn('flex flex-col gap-3 rounded-md bg-surface-1 p-3', variant === 'rail' && 'p-4', className)}>
      <header className="flex items-start gap-3">
        {profileHref ? (
          <Link href={profileHref} className="shrink-0 rounded-pill" aria-label={`Open profile of ${authorLabel}`}>
            <Avatar src={identity?.avatarUrl ?? null} name={authorLabel} size="lg" decorative loading={isPending} />
          </Link>
        ) : (
          <Avatar src={null} name={authorLabel} size="lg" decorative />
        )}
        <div className="min-w-0 flex-1">
          <h3 id={headingId} className="flex min-w-0 items-center gap-2 text-md font-bold text-text">
            {profileHref ? <Link href={profileHref} className="truncate hover:underline">{authorLabel}</Link> : <span className="truncate" title={callout.authorId}>{authorLabel}</span>}
            <AgeChip iso={callout.createdAt} className="shrink-0" />
            {callout.corrected ? <span className="inline-flex shrink-0 items-center gap-1 text-xs text-text-faint"><Pencil aria-hidden="true" className="size-icon-sm" />Edited</span> : null}
          </h3>
          <div className="mt-0.5 flex flex-wrap items-center gap-1.5">
            {callout.direction === 'bullish' ? <Chip variant="token" icon={<TrendingUp />} className="text-percent-up">Bullish</Chip> : null}
            {callout.direction === 'bearish' ? <Chip variant="token" icon={<TrendingDown />} className="text-percent-down">Bearish</Chip> : null}
            {callout.direction === 'neutral' ? <Chip variant="token">Neutral</Chip> : null}
            {callout.disclosure === 'sponsored' ? <Chip variant="token">Sponsored</Chip> : null}
            {callout.disclosure === 'creator' ? <Chip variant="token">Creator</Chip> : null}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {callout.authorWallet ? <FollowButton wallet={callout.authorWallet} handle={identity?.displayName ?? null} variant="icon" size="sm" /> : null}
          <ReportMenu subject={{ targetType: 'comment', targetId: callout.id, label: `callout by ${authorLabel}` }} />
        </div>
      </header>

      {removed ? (
        <p className="text-sm text-text-faint">This callout was removed</p>
      ) : (
        <div>
          <p
            ref={measure}
            tabIndex={expanded ? -1 : undefined}
            className={cn('whitespace-pre-wrap break-words text-base text-text outline-none', !expanded && clampClass[clampLines])}
          >
            {callout.body}
          </p>
          {!expanded && clamped ? (
            <button type="button" onClick={expand} className="mt-1 text-sm font-bold text-accent hover:underline">Show more</button>
          ) : null}
        </div>
      )}

      {callout.poolAddress && !removed ? (
        <EmbeddedCoinCard poolAddress={callout.poolAddress} stats={stats} positionDisclosure={callout.positionDisclosure} positionVerified={callout.positionVerified} />
      ) : callout.positionDisclosure && !removed ? (
        <p className="text-xs text-text-muted"><span className="font-semibold uppercase tracking-[.04em] text-text-faint">Position </span>{callout.positionDisclosure}</p>
      ) : null}

      <footer className="flex items-center gap-1">
        <IconButton label="Copy link" icon={<Link2 />} size="sm" onClick={() => { void copyLink(); }} />
        {callout.poolAddress ? (
          <Link href={`/coin/${callout.poolAddress}#trade`} aria-label="Trade this coin" title="Trade this coin" className="inline-flex size-control-height-sm items-center justify-center rounded-sm text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text [&_svg]:size-icon-sm">
            <ArrowLeftRight aria-hidden="true" />
          </Link>
        ) : null}
      </footer>
    </article>
  );
}

/** Loading placeholder matching the card anatomy (avatar, three lines, embed block). */
export function CalloutCardSkeleton({ className }: { className?: string }) {
  return (
    <div aria-hidden="true" className={cn('flex flex-col gap-3 rounded-md bg-surface-1 p-3', className)}>
      <div className="flex items-center gap-3">
        <Skeleton variant="circle" className="size-avatar-lg" />
        <div className="flex flex-1 flex-col gap-1.5"><Skeleton width="40%" /><Skeleton width="20%" className="h-3" /></div>
      </div>
      <Skeleton /><Skeleton width="92%" /><Skeleton width="60%" />
      <Skeleton variant="block" height={64} />
    </div>
  );
}
