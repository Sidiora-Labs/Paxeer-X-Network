'use client';
import type { ReactNode } from 'react';
import Link from 'next/link';
import { cn } from '@/lib/utils';
import type { WalletIdentity } from '@/core/market/use-wallet-identities';
import { Avatar, RankBadge } from '@/ui/primitives';
import { formatUsdl, shortAddress } from '@/utils/format';
import { metricTone } from './cups/cups-contract';

export interface RankRowProps {
  rank: number;
  wallet: string;
  identity?: WalletIdentity;
  /** Signed raw USDL (6 decimals) metric; null renders an em dash with the quality note. */
  metricSigned: string | null;
  /** Spoken label for the metric column, e.g. "Profit". */
  metricLabel?: string;
  /** Raw USDL prize for the rank (cup results); omitted on rolling boards. */
  prizeRaw?: string | null;
  /** Row belongs to the signed-in viewer. */
  self?: boolean;
  size?: 'md' | 'lg';
  /** Secondary controls (Follow) rendered outside the row link. */
  actions?: ReactNode;
  qualityNote?: string | null;
  className?: string;
}

export function profileHref(wallet: string): string {
  return `/profile/${wallet.toLowerCase()}`;
}

const toneClass = { success: 'text-success', danger: 'text-danger', neutral: 'text-text' } as const;

/**
 * Leaderboard row (components.md 36): rank with podium tone and crown for
 * 1-3, avatar or initials, handle (or short address), short wallet, signed
 * PnL and an optional prize pill. The handle link covers the whole row; the
 * actions slot sits above it so Follow stays a separate control.
 */
export function RankRow({ rank, wallet, identity, metricSigned, metricLabel = 'Profit', prizeRaw = null, self = false, size = 'md', actions, qualityNote = null, className }: RankRowProps) {
  const address = wallet.toLowerCase();
  const handle = identity?.displayName?.trim() || null;
  const short = identity?.shortAddress ?? shortAddress(address, 6, 4);
  const tone = metricSigned === null ? 'neutral' : metricTone(metricSigned);
  const large = size === 'lg';
  return (
    <li
      className={cn(
        'relative flex items-center gap-3 rounded-sm px-3 transition-colors duration-fast hover:bg-surface-hover',
        // components.md 36 / layout.md: leaderboard rows are 48 (board) and 64 (cup results).
        large ? 'min-h-list-row-lg py-2' : 'min-h-12 py-1',
        self && 'bg-accent-soft',
        className,
      )}
      data-rank={rank}
      data-self={self || undefined}
    >
      <RankBadge rank={rank} className={cn('shrink-0 justify-center', large ? 'w-10' : 'w-8')} />
      <Avatar src={identity?.avatarUrl ?? null} name={handle ?? short} size={large ? 'xl' : 'md'} decorative />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <Link
            href={profileHref(address)}
            className="truncate text-md font-bold leading-5 text-text after:absolute after:inset-0 after:content-['']"
          >
            {handle ?? short}
          </Link>
          {self ? <span className="inline-flex h-5 shrink-0 items-center rounded-xs bg-surface-3 px-1.5 text-2xs font-bold uppercase tracking-wide text-text-muted">You</span> : null}
        </div>
        {handle ? <p className="truncate font-mono text-xs leading-4 text-text-muted">{short}</p> : null}
      </div>
      <div className="flex shrink-0 flex-col items-end gap-1 text-right">
        <span className={cn('text-md font-bold leading-5 tabular-nums', toneClass[tone])} data-numeric>
          <span className="sr-only">{metricLabel} </span>
          {metricSigned === null ? <span aria-label="no data yet">&mdash;</span> : formatUsdl(metricSigned, { signed: true })}
        </span>
        {qualityNote ? <span className="text-2xs text-text-faint">{qualityNote}</span> : null}
        {prizeRaw !== null ? (
          <span className="inline-flex h-6 items-center rounded-pill bg-surface-3 px-2 text-xs font-bold text-text" data-numeric>
            <span className="sr-only">Prize </span>
            {formatUsdl(prizeRaw, { fractionDigits: 0 })}
          </span>
        ) : null}
      </div>
      {actions ? <div className="relative z-10 flex shrink-0 items-center">{actions}</div> : null}
    </li>
  );
}
