'use client';
import Link from 'next/link';
import { AtSign, Globe, MessageCircle, Send, Users, type LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Avatar, PercentPill, StageBadge, Tooltip } from '@/ui/primitives';
import { formatCompactUsd, formatCount, relativeAge } from '@/utils/format';
import type { RailPairRow, SocialKind } from './rows';

const socialIcon: Record<SocialKind, LucideIcon> = { website: Globe, twitter: AtSign, telegram: Send, discord: MessageCircle };
const socialLabel: Record<SocialKind, string> = { website: 'Website', twitter: 'X profile', telegram: 'Telegram', discord: 'Discord' };

export interface PairRowProps {
  row: RailPairRow;
  active: boolean;
  now: number;
}

/** Dense 44px pair row: the symbol link covers the row; social links sit above it. */
export function PairRow({ row, active, now }: PairRowProps) {
  const { change } = row;
  return (
    <li
      data-pool={row.poolAddress}
      className={cn('relative flex h-11 items-center gap-2 px-2 transition-colors duration-fast', active ? 'bg-surface-3' : 'bg-surface-1 hover:bg-surface-hover')}
    >
      <Avatar src={row.image} name={row.name} size="md" decorative badge={row.stage ? <StageBadge stage={row.stage} size={14} decorative surface={active ? 3 : 1} /> : undefined} />
      <div className="flex min-w-0 flex-1 flex-col justify-center gap-0.5">
        <div className="flex min-w-0 items-baseline gap-1.5">
          <Link
            href={row.href}
            aria-current={active ? 'page' : undefined}
            title={row.name}
            className="shrink-0 truncate text-sm font-bold text-text after:absolute after:inset-0 after:content-[''] focus-visible:outline-2 focus-visible:outline-focus"
          >
            {row.symbol}
            <span className="sr-only">{`, ${row.name}, market cap ${row.mcap === null ? 'not available yet' : formatCompactUsd(row.mcap)}`}</span>
          </Link>
          <span aria-hidden="true" className="min-w-0 truncate text-xs text-text-muted">{row.name}</span>
        </div>
        <div className="flex min-w-0 items-center gap-1.5 text-2xs text-text-faint">
          {row.createdAtMs === null
            ? <span>{'—'}</span>
            : <time dateTime={new Date(row.createdAtMs).toISOString()} className="font-bold tabular-nums text-accent"><span className="sr-only">Age </span>{relativeAge(row.createdAtMs, now)}</time>}
          <span className="inline-flex items-center gap-0.5 tabular-nums" title="Holders">
            <Users aria-hidden="true" className="size-3" />
            <span className="sr-only">Holders </span>{formatCount(row.holders)}
          </span>
          {row.socials.map(social => {
            const Icon = socialIcon[social.kind];
            return (
              <a
                key={social.kind}
                href={social.url}
                target="_blank"
                rel="noopener noreferrer nofollow"
                className="relative z-10 inline-flex size-4 items-center justify-center rounded-xs text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-focus"
              >
                <Icon aria-hidden="true" className="size-3" />
                <span className="sr-only">{`${socialLabel[social.kind]} of ${row.symbol}, opens in a new tab`}</span>
              </a>
            );
          })}
        </div>
      </div>
      <div className="flex shrink-0 flex-col items-end justify-center gap-0.5">
        <span data-numeric className={cn('text-sm font-bold tabular-nums', row.mcap === null ? 'text-text-faint' : 'text-text')}>{formatCompactUsd(row.mcap)}</span>
        {change.reason === null
          ? <PercentPill value={change.value} variant="text" decimals={1} />
          : (
            <Tooltip content={change.reason} align="end" className="relative z-10">
              <span data-numeric className="text-xs text-text-faint" aria-label={`Change not available yet: ${change.reason}`}>-</span>
            </Tooltip>
          )}
      </div>
    </li>
  );
}
