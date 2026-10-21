'use client';
import Link from 'next/link';
import { Globe, MessageCircle, Star, type LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Avatar, StageBadge, type Stage } from '@/ui/primitives';
import { StatChange, StatDash } from '@/ui/shared/stat-value';
import { formatSpotPrice } from '@/core/market/pool-stats-display';
import { formatUsdl } from '@/utils/format';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import type { ColumnDensity } from './layout-store';

export interface SocialLink {
  kind: 'website' | 'twitter' | 'telegram';
  url: string;
}

export interface TrendingRowData {
  poolAddress: string;
  href: string;
  rank: number;
  name: string;
  symbol: string;
  image: string | null;
  /** Exact decimal USDL price per whole token (PoolStats.spotPriceWad); null before the first trade. */
  price: string | null;
  priceReason: string | null;
  /** Market cap (PoolStats.totalSupplyValuationUsdlRaw), raw USDL. */
  mcapRaw: string | null;
  mcapReason: string | null;
  /** Percent change for the selected timeframe, only on a complete baseline. */
  change: number | null;
  /** Why `change` is null: no v2 window, not enough history, no stats. */
  changeReason: string | null;
  socials: SocialLink[];
  stage: Stage | null;
}

export interface TrendingRowProps {
  row: TrendingRowData;
  density?: ColumnDensity;
  starred?: boolean;
  onToggleStar?: (next: boolean) => void;
  flash?: 'up' | 'down' | null;
  className?: string;
}

const socialIcon: Record<SocialLink['kind'], { Icon: LucideIcon; label: string }> = {
  website: { Icon: Globe, label: 'Website' },
  twitter: { Icon: MessageCircle, label: 'X profile' },
  telegram: { Icon: MessageCircle, label: 'Telegram' },
};

/** Keep only socials that pass the app's external destination allowlist. */
export function safeSocials(socials: { website?: string | null; twitter?: string | null; telegram?: string | null } | null | undefined): SocialLink[] {
  if (!socials) return [];
  const out: SocialLink[] = [];
  for (const kind of ['website', 'twitter', 'telegram'] as const) {
    const raw = socials[kind];
    if (!raw) continue;
    const safe = safeExternalDestination(raw);
    if (safe) out.push({ kind, url: safe });
  }
  return out;
}

export function changeWash(change: number | null): string {
  if (change === null || !Number.isFinite(change) || change === 0) return '';
  return change > 0 ? 'bg-[var(--sl-tint-accent-row-wash)]' : 'bg-[var(--sl-tint-danger-row-wash)]';
}

/**
 * Trending coin row (components.md coin row): image with stage badge, name and
 * symbol, adaptive-precision price, socials, market cap and the selected
 * timeframe change. The whole row links to the coin page; the direction wash
 * is a flat alpha tint.
 */
export function TrendingRow({ row, density = 'comfortable', starred = false, onToggleStar, flash = null, className }: TrendingRowProps) {
  const compact = density === 'compact';
  return (
    <li
      className={cn(
        'relative flex items-center gap-3 px-3 hover:bg-surface-hover',
        compact ? 'h-list-row' : 'h-list-row-lg',
        changeWash(row.change),
        className,
      )}
      data-pool={row.poolAddress}
    >
      <Avatar src={row.image} name={row.name} size={compact ? 'sm' : 'lg'} shape="square" decorative badge={row.stage && !compact ? <StageBadge stage={row.stage} size={14} surface={1} /> : undefined} />
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex min-w-0 items-baseline gap-1.5">
          <Link href={row.href} className="truncate text-sm font-bold text-text after:absolute after:inset-0 after:content-['']">
            {row.name}
          </Link>
          <span className="truncate text-xs text-text-muted">{row.symbol}</span>
        </div>
        <div className="flex items-center gap-2 text-xs text-text-muted">
          {row.price === null
            ? <StatDash reason={row.priceReason ?? 'No price yet'} subject="Price" align="start" />
            : <span data-numeric className="tabular-nums"><span className="sr-only">Price </span>{formatSpotPrice(row.price)}</span>}
          {row.socials.length > 0 ? (
            <span className="relative z-10 inline-flex items-center gap-1">
              {row.socials.map(({ kind, url }) => {
                const { Icon, label } = socialIcon[kind];
                return (
                  <a key={kind} href={url} target="_blank" rel="noopener noreferrer" aria-label={`${row.name} ${label}`} className="inline-flex size-4 items-center justify-center rounded-xs text-text-faint hover:bg-surface-hover hover:text-text">
                    <Icon aria-hidden="true" className="size-3" />
                  </a>
                );
              })}
            </span>
          ) : null}
        </div>
      </div>
      <div className="flex shrink-0 flex-col items-end">
        {row.mcapRaw
          ? <span data-numeric className="text-sm font-bold tabular-nums text-text">{`${formatUsdl(row.mcapRaw, { compact: true, unit: false })} MC`}</span>
          : <StatDash reason={row.mcapReason ?? 'No market cap yet'} subject="Market cap" className="text-sm" />}
        <StatChange change={{ value: row.change, reason: row.changeReason }} variant="text" arrow flash={flash} />
      </div>
      {onToggleStar ? (
        <button
          type="button"
          aria-label={starred ? `Remove ${row.name} from watchlist` : `Add ${row.name} to watchlist`}
          aria-pressed={starred}
          onClick={() => onToggleStar(!starred)}
          className={cn('relative z-10 inline-flex size-7 shrink-0 items-center justify-center rounded-sm hover:bg-surface-hover', starred ? 'text-warning' : 'text-text-faint hover:text-text')}
        >
          <Star aria-hidden="true" className="size-icon-sm" fill={starred ? 'currentColor' : 'none'} />
        </button>
      ) : null}
    </li>
  );
}

export function TrendingRowSkeleton({ density = 'comfortable', count = 8 }: { density?: ColumnDensity; count?: number }) {
  const compact = density === 'compact';
  return (
    <ul aria-busy="true" className="flex flex-col">
      <li className="sr-only">Loading coins</li>
      {Array.from({ length: count }, (_, index) => (
        <li key={index} aria-hidden="true" className={cn('flex items-center gap-3 px-3', compact ? 'h-list-row' : 'h-list-row-lg')}>
          <span className={cn('sl-skeleton shrink-0 rounded-sm', compact ? 'size-avatar-sm' : 'size-avatar-lg')} />
          <span className="flex flex-1 flex-col gap-1.5">
            <span className="sl-skeleton h-3 w-28" />
            <span className="sl-skeleton h-2.5 w-16" />
          </span>
          <span className="flex flex-col items-end gap-1.5">
            <span className="sl-skeleton h-3 w-14" />
            <span className="sl-skeleton h-2.5 w-10" />
          </span>
        </li>
      ))}
    </ul>
  );
}
