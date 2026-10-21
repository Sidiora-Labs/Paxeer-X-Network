'use client';

import type { MouseEvent, ReactNode } from 'react';
import Link from 'next/link';
import { Star } from 'lucide-react';
import { cn } from '@/lib/utils';
import { formatUsdl } from '@/utils/format';
import { Avatar } from './avatar';
import { AgeChip, PercentPill, RankBadge, StageBadge, stageLabel, type Stage } from './badges';
import { Sparkline } from './data';

export interface CoinCardStat {
  label: string;
  value: ReactNode | null | undefined;
}

export interface CoinCardProps {
  image: string | null;
  name: string;
  symbol: string;
  /** Raw market cap in the quote asset scale; null when stats are unavailable. */
  mcapRaw: string | bigint | null;
  mcapDecimals?: number;
  /** 24h change in percent; null when unavailable. */
  percentChange: number | null;
  /** Creation time, ISO string or unix seconds; null when unknown. */
  createdAt: string | number | null;
  href: string;
  starred?: boolean;
  onToggleStar?: (next: boolean) => void;
  stage?: Stage | null;
  footerStats?: CoinCardStat[];
  sparkline?: number[] | null;
  rank?: number;
  variant?: 'grid' | 'screener';
  /** Surface the card sits on; the card takes the next step. */
  surface?: 'canvas' | 'surface-1';
  loading?: boolean;
  className?: string;
}

export function CoinCard({
  image,
  name,
  symbol,
  mcapRaw,
  mcapDecimals = 6,
  percentChange,
  createdAt,
  href,
  starred = false,
  onToggleStar,
  stage = null,
  footerStats,
  sparkline = null,
  rank,
  variant = 'grid',
  surface = 'canvas',
  loading = false,
  className,
}: CoinCardProps) {
  const cardBg = surface === 'canvas' ? 'bg-surface-1' : 'bg-surface-2';
  const top3 = rank !== undefined && rank >= 1 && rank <= 3;
  const mcap = mcapRaw === null ? null : formatUsdl(mcapRaw, { decimals: mcapDecimals, compact: true, unit: false });
  const parentSurface = surface === 'canvas' ? 1 : 2;

  function onStar(event: MouseEvent<HTMLButtonElement>) {
    event.preventDefault();
    event.stopPropagation();
    onToggleStar?.(!starred);
  }

  if (loading) {
    return (
      <div aria-busy="true" className={cn('rounded-md p-4', cardBg, variant === 'screener' ? 'h-screener-card' : 'min-w-token-card-min-width', className)}>
        <span className="sr-only">Loading coin</span>
        <div aria-hidden="true" className="flex items-center gap-3">
          <span className={cn('sl-skeleton rounded-sm', variant === 'screener' ? 'size-avatar-lg' : 'size-avatar-xl')} />
          <div className="flex flex-1 flex-col gap-2">
            <span className="sl-skeleton h-3 w-2/3" />
            <span className="sl-skeleton h-3 w-1/3" />
          </div>
        </div>
      </div>
    );
  }

  const star = onToggleStar ? (
    <button
      type="button"
      onClick={onStar}
      aria-pressed={starred}
      aria-label={starred ? `Remove ${name} from watchlist` : `Add ${name} to watchlist`}
      className={cn(
        'relative z-10 inline-flex size-control-height-sm shrink-0 items-center justify-center rounded-sm transition-colors duration-fast hover:bg-surface-hover',
        starred ? 'text-accent' : 'text-text-faint hover:text-text',
      )}
    >
      <Star aria-hidden="true" className={cn('size-icon-sm', starred && 'fill-current')} />
    </button>
  ) : null;

  const avatar = (
    <Avatar
      src={image}
      name={name}
      size={variant === 'screener' ? 'lg' : 'xl'}
      shape="square"
      decorative
      badge={stage ? <StageBadge stage={stage} decorative surface={parentSurface} /> : undefined}
    />
  );

  const hiddenSummary = (
    <span className="sr-only">
      {stage ? `, ${stageLabel(stage)}` : ''}
      {mcap ? `, market cap ${mcap} USDL` : ', no market cap yet'}
    </span>
  );

  if (variant === 'screener') {
    return (
      <article className={cn('group relative flex h-screener-card items-center gap-3 rounded-md px-3 transition-colors duration-fast hover:bg-surface-hover', cardBg, top3 && 'bg-rank-1-soft', className)}>
        {rank !== undefined ? <RankBadge rank={rank} /> : null}
        {avatar}
        <div className="min-w-0 flex-1">
          <Link href={href} className="block truncate text-md font-bold text-text after:absolute after:inset-0 after:rounded-md after:content-['']">
            <span className="truncate" title={name}>{name}</span>
            {hiddenSummary}
          </Link>
          <div className="flex items-center gap-2 text-xs text-text-faint">
            <span className="truncate font-bold uppercase" title={symbol}>${symbol}</span>
            <AgeChip iso={createdAt} />
          </div>
          {footerStats && footerStats.length > 0 ? (
            <dl className="mt-1 flex flex-wrap gap-x-3 text-2xs text-text-faint">
              {footerStats.map(stat => (
                <div key={stat.label} className="flex items-center gap-1">
                  <dt>{stat.label}</dt>
                  <dd data-numeric className={cn('tabular-nums', stat.value === null || stat.value === undefined ? 'text-text-faint' : 'text-text')}>{stat.value ?? '—'}</dd>
                </div>
              ))}
            </dl>
          ) : null}
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1">
          <span data-numeric className={cn('text-num-sm font-bold tabular-nums', mcap ? 'text-text' : 'text-text-faint')}>{mcap ? `${mcap} MC` : '—'}</span>
          <PercentPill value={percentChange} variant="text" />
        </div>
        {star}
      </article>
    );
  }

  return (
    <article className={cn('group relative flex min-w-token-card-min-width flex-col gap-3 rounded-md p-4 transition-colors duration-fast hover:bg-surface-hover', cardBg, top3 && 'bg-rank-1-soft', className)}>
      <div className="flex items-start gap-3">
        {avatar}
        <div className="min-w-0 flex-1">
          <Link href={href} className="block truncate text-md font-bold text-text after:absolute after:inset-0 after:rounded-md after:content-['']" title={name}>
            {name}
            {hiddenSummary}
          </Link>
          <div className="flex items-center gap-2 text-xs text-text-faint">
            <span className="truncate font-bold uppercase" title={symbol}>${symbol}</span>
            <AgeChip iso={createdAt} />
          </div>
        </div>
        {rank !== undefined ? <RankBadge rank={rank} /> : null}
        {star}
      </div>
      <div className="flex items-end justify-between gap-3">
        <div className="flex flex-col">
          <span className="text-2xs font-bold uppercase tracking-[.04em] text-text-faint">Market cap</span>
          <span data-numeric className={cn('text-num-md font-bold tabular-nums', mcap ? 'text-text' : 'text-text-faint')}>{mcap ? `${mcap} USDL` : '—'}</span>
        </div>
        <div className="flex items-center gap-2">
          {sparkline && sparkline.length > 1 ? <Sparkline data={sparkline} width={64} height={24} decorative /> : null}
          <PercentPill value={percentChange} arrow />
        </div>
      </div>
      {footerStats && footerStats.length > 0 ? (
        <dl className="grid grid-cols-3 gap-2">
          {footerStats.map(stat => (
            <div key={stat.label} className="min-w-0">
              <dt className="truncate text-2xs text-text-faint">{stat.label}</dt>
              <dd data-numeric className={cn('truncate text-num-sm tabular-nums', stat.value === null || stat.value === undefined ? 'text-text-faint' : 'text-text')}>{stat.value ?? '—'}</dd>
            </div>
          ))}
        </dl>
      ) : null}
    </article>
  );
}
