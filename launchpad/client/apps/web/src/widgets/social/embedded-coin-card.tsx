'use client';
import Link from 'next/link';
import { ShieldCheck } from 'lucide-react';
import { cn } from '@/lib/utils';
import { technicalDetail } from '@/core/user-facing-error';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import type { Unsupported } from '@/core/market/unsupported';
import { Avatar, Skeleton, Tooltip } from '@/ui/primitives';
import { formatUsdl, shortAddress } from '@/utils/format';
import { marketCapRaw, type PoolStats } from './callout-model';

export type EmbeddedStatsState =
  | { state: 'loading' }
  | { state: 'ready'; stats: PoolStats | null }
  | Unsupported
  | { state: 'error'; error: Error };

export interface EmbeddedCoinCardProps {
  poolAddress: string;
  /** Live stats for this pool from the surrounding batch request. */
  stats: EmbeddedStatsState;
  positionDisclosure: string | null;
  positionVerified: boolean | null;
  className?: string;
}

/**
 * The coin embed inside a callout card (components.md 33): token identity,
 * the author's declared position and the live market cap chip. Sits on
 * surface-2 inside a surface-1 card; no strokes.
 */
export function EmbeddedCoinCard({ poolAddress, stats, positionDisclosure, positionVerified, className }: EmbeddedCoinCardProps) {
  const snapshot = usePoolSnapshot(poolAddress);
  const token = snapshot.data?.token ?? null;
  const metadata = snapshot.data?.metadata ?? null;
  const symbol = metadata?.displaySymbol ?? token?.symbol ?? null;
  const name = metadata?.displayName ?? token?.name ?? null;
  const pending = snapshot.isPending;
  const mcap = stats.state === 'ready' ? marketCapRaw(stats.stats) : null;

  return (
    <Link
      href={`/coin/${poolAddress}`}
      className={cn('flex items-center gap-3 rounded-sm bg-surface-2 p-3 transition-colors duration-fast hover:bg-surface-hover', className)}
      aria-label={`Open ${symbol ?? shortAddress(poolAddress)}`}
    >
      <Avatar src={null} name={symbol ?? shortAddress(poolAddress)} size="lg" shape="square" decorative loading={pending} />
      <span className="min-w-0 flex-1">
        {pending ? (
          <>
            <Skeleton width={72} className="h-3.5" />
            <Skeleton width={120} className="mt-1.5 h-3" />
          </>
        ) : (
          <>
            <span className="block truncate text-sm font-bold text-text">{symbol ?? shortAddress(poolAddress)}</span>
            <span className="block truncate text-xs text-text-muted">{name ?? 'Unknown coin'}</span>
          </>
        )}
      </span>
      <span className="flex shrink-0 flex-col items-end gap-0.5 text-right">
        <span className="inline-flex items-center gap-1 text-2xs font-semibold uppercase tracking-[.04em] text-text-faint">
          Position
          {positionVerified ? (
            <Tooltip content="Verified position">
              <span className="inline-flex text-success" aria-label="Position verified"><ShieldCheck aria-hidden="true" className="size-icon-sm" /></span>
            </Tooltip>
          ) : null}
        </span>
        <span className="max-w-32 truncate text-num-sm font-bold text-text" title={positionDisclosure ?? undefined}>{positionDisclosure ?? 'Not shared'}</span>
      </span>
      <span className="flex shrink-0 flex-col items-end gap-0.5 text-right">
        <span className="text-2xs font-semibold uppercase tracking-[.04em] text-text-faint">MC</span>
        {stats.state === 'loading' ? (
          <Skeleton width={56} className="h-3.5" />
        ) : (
          <span data-numeric className={cn('text-num-sm font-bold tabular-nums', mcap ? 'text-text' : 'text-text-faint')} title={stats.state === 'unsupported' ? 'Market cap is not available yet' : stats.state === 'error' ? "Can't load market cap right now" : undefined} data-operation={stats.state === 'unsupported' ? technicalDetail(stats.operation, stats.status) ?? undefined : undefined}>
            {mcap ? formatUsdl(mcap, { compact: true }) : '—'}
          </span>
        )}
      </span>
    </Link>
  );
}
