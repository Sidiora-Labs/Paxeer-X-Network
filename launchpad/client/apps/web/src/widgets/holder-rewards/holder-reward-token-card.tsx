'use client';

import type { MouseEvent } from 'react';
import Link from 'next/link';
import { AtSign, Coins, ExternalLink, Globe, Send } from 'lucide-react';
import { cn } from '@/lib/utils';
import { AgeChip, Avatar, RankBadge, StatusBadge } from '@/ui/primitives';
import CopyAddress from '@/ui/shared/copy-address';
import { explorerHref, holdersPaidText, mcapText, rewardsText, usdlAmountText, type SocialKind, type SocialLink } from './holder-rewards-presentation';

export interface HolderRewardTokenCardProps {
  name: string;
  symbol: string;
  image: string | null;
  tokenAddress: string | null;
  /** Coin page destination; null renders a static card (create preview). */
  href: string | null;
  createdAt: string | number | null;
  links?: SocialLink[];
  /** Cumulative USDL distributed to holders, raw 6 decimals; null when unknown. */
  distributedRaw: string | null;
  mcapRaw: string | null;
  holdersPaid: number | null;
  /** False when the pool's fee strategy is no longer Holders. */
  strategyActive?: boolean;
  rank?: number;
  surface?: 'canvas' | 'surface-1';
  loading?: boolean;
  className?: string;
}

const socialMeta: Record<SocialKind, { label: string; Icon: typeof Globe }> = {
  website: { label: 'Website', Icon: Globe },
  twitter: { label: 'X profile', Icon: AtSign },
  telegram: { label: 'Telegram', Icon: Send },
};

function stop(event: MouseEvent) {
  event.stopPropagation();
}

export function UsdlBadge({ size = 'sm', className }: { size?: 'sm' | 'md'; className?: string }) {
  return (
    <span className={cn('inline-flex shrink-0 items-center gap-1 rounded-pill bg-surface-3 font-bold text-text-muted', size === 'md' ? 'h-6 px-2 text-xs' : 'h-5 px-1.5 text-2xs', className)} title="Rewards are paid in USDL">
      <Coins aria-hidden="true" className={size === 'md' ? 'size-3.5' : 'size-3'} />
      <span>USDL</span>
    </span>
  );
}

export function TokenLinkIcons({ links, tokenAddress, className }: { links: SocialLink[]; tokenAddress: string | null; className?: string }) {
  if (links.length === 0 && !tokenAddress) return null;
  return (
    <span className={cn('relative z-10 inline-flex items-center gap-0.5', className)}>
      {links.map(link => {
        const meta = socialMeta[link.kind];
        return (
          <a
            key={link.kind}
            href={link.href}
            target="_blank"
            rel="noreferrer noopener"
            onClick={stop}
            aria-label={`${meta.label}, opens in new tab`}
            className="inline-flex size-5 items-center justify-center rounded-xs text-text-faint transition-colors hover:bg-surface-hover hover:text-text"
          >
            <meta.Icon aria-hidden="true" className="size-icon-sm" />
          </a>
        );
      })}
      {tokenAddress ? (
        <a
          href={explorerHref(tokenAddress)}
          target="_blank"
          rel="noreferrer noopener"
          onClick={stop}
          aria-label="View on explorer, opens in new tab"
          className="inline-flex size-5 items-center justify-center rounded-xs text-text-faint transition-colors hover:bg-surface-hover hover:text-text"
        >
          <ExternalLink aria-hidden="true" className="size-icon-sm" />
        </a>
      ) : null}
    </span>
  );
}

/**
 * Ranked holder-rewards coin card: top-paying rail, grid view and the create
 * form preview share it. Top-3 ranks tint the surface (no outline); the
 * whole card is reachable through the name link overlay while copy and
 * external links stay separately focusable.
 */
export default function HolderRewardTokenCard({
  name,
  symbol,
  image,
  tokenAddress,
  href,
  createdAt,
  links = [],
  distributedRaw,
  mcapRaw,
  holdersPaid,
  strategyActive = true,
  rank,
  surface = 'canvas',
  loading = false,
  className,
}: HolderRewardTokenCardProps) {
  const cardBg = surface === 'canvas' ? 'bg-surface-1' : 'bg-surface-2';
  const top3 = rank !== undefined && rank >= 1 && rank <= 3;

  if (loading) {
    return (
      <div aria-busy="true" className={cn('flex h-full min-h-[188px] min-w-0 flex-col gap-3 rounded-md p-4', cardBg, className)}>
        <span className="sr-only">Loading coin</span>
        <div aria-hidden="true" className="flex items-center gap-3">
          <span className="sl-skeleton size-avatar-lg rounded-sm" />
          <div className="flex flex-1 flex-col gap-2">
            <span className="sl-skeleton h-3 w-2/3" />
            <span className="sl-skeleton h-3 w-1/3" />
          </div>
        </div>
        <div aria-hidden="true" className="flex flex-col gap-2">
          <span className="sl-skeleton h-2.5 w-1/3" />
          <span className="sl-skeleton h-6 w-1/2" />
          <span className="sl-skeleton h-3 w-2/5" />
        </div>
        <div aria-hidden="true" className="mt-auto flex items-center justify-between">
          <span className="sl-skeleton h-3 w-1/4" />
          <span className="sl-skeleton h-6 w-16 rounded-pill" />
        </div>
      </div>
    );
  }

  const title = (
    <>
      <span className="truncate" title={name}>{name}</span>
      <span className="sr-only">
        {distributedRaw ? `, ${rewardsText(distributedRaw)} distributed to holders` : ', rewards not available yet'}
        {mcapRaw ? `, market cap ${mcapText(mcapRaw)} USDL` : ', market cap not available yet'}
      </span>
    </>
  );

  return (
    <article className={cn('group relative flex h-full min-h-[188px] min-w-0 flex-col gap-3 rounded-md p-4 transition-colors duration-fast', cardBg, href && 'hover:bg-surface-hover', top3 && 'bg-rank-1-soft', className)}>
      <div className="flex items-start gap-3">
        <Avatar src={image} name={name} size="lg" shape="square" decorative />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1">
            {href ? (
              <Link href={href} className="flex min-w-0 items-center text-base font-semibold text-text after:absolute after:inset-0 after:rounded-md after:content-['']">
                {title}
              </Link>
            ) : (
              <h3 className="flex min-w-0 items-center text-base font-semibold text-text">{title}</h3>
            )}
            {tokenAddress ? <CopyAddress address={tokenAddress} variant="icon" className="relative z-10 shrink-0" /> : null}
          </div>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-text-faint">
            {symbol ? <span className="truncate font-semibold uppercase" title={symbol}>${symbol}</span> : null}
            <AgeChip iso={createdAt} className="font-bold text-warning" />
            <TokenLinkIcons links={links} tokenAddress={tokenAddress} />
            {!strategyActive ? <StatusBadge status="neutral">Strategy changed</StatusBadge> : null}
          </div>
        </div>
        {rank !== undefined ? <RankBadge rank={rank} className="shrink-0" /> : null}
      </div>
      <dl className="flex flex-col gap-1">
        <div className="flex min-w-0 flex-col gap-1">
          <dt className="text-2xs font-semibold uppercase tracking-[.1em] text-text-faint">Holder rewards</dt>
          <dd data-numeric className={cn('truncate text-xl font-bold [font-variant-numeric:proportional-nums]', distributedRaw === null ? 'text-text-faint' : 'text-success')}>
            {distributedRaw === null ? <span aria-label="Not available yet">{'—'}</span> : <>{usdlAmountText(distributedRaw)}<span className="ml-1 text-xs font-semibold text-text-faint">USDL</span></>}
          </dd>
        </div>
        <div className="flex min-w-0 items-baseline gap-1.5 text-xs">
          <dt className="text-text-faint">Market cap</dt>
          <dd data-numeric className={cn('truncate text-sm font-bold tabular-nums', mcapRaw === null ? 'text-text-faint' : 'text-text')}>
            {mcapRaw === null ? <span aria-label="Not available yet">{'—'}</span> : `${mcapText(mcapRaw)} USDL`}
          </dd>
        </div>
      </dl>
      <div className="mt-auto flex items-center justify-between gap-2">
        <dl className="flex min-w-0 items-baseline gap-1.5 whitespace-nowrap text-xs">
          <dt className="text-text-faint">Holders paid</dt>
          <dd data-numeric className={cn('truncate font-bold tabular-nums', holdersPaid === null ? 'text-text-faint' : 'text-text')}>
            {holdersPaid === null ? <span aria-label="Not available yet">{'—'}</span> : holdersPaidText(holdersPaid)}
          </dd>
        </dl>
        <UsdlBadge size="md" />
      </div>
    </article>
  );
}
