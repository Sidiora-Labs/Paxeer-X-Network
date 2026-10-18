'use client';
import { Avatar, Chip, Skeleton } from '@/ui/primitives';
import { formatUsdl, shortAddress } from '@/utils/format';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import type { PoolStats } from './bounties-contract';
import { marketCapRaw } from '@/core/market/pool-stats-display';

export interface BountyTokenChipProps {
  poolAddress: string;
  /** Batched stats for the pool (IF_STATS_BATCH); the pool's own snapshot stats are the fallback. */
  stats?: PoolStats | null;
  className?: string;
}

/**
 * Linked-token chip: symbol from the pool's metadata, market cap from the
 * live stats (batched when the page has them, per-pool otherwise). Links to
 * the coin page. Shows the pool address when the resolver cannot identify it.
 */
export function BountyTokenChip({ poolAddress, stats, className }: BountyTokenChipProps) {
  const pool = poolAddress.toLowerCase();
  const snapshot = usePoolSnapshot(pool);
  const href = `/coin/${pool}`;
  if (snapshot.isPending) return <Skeleton variant="pill" width={112} height={22} label="Loading coin" className={className} />;
  const token = snapshot.data?.token ?? null;
  if (!token) return <Chip variant="token" href={href} className={className}>{shortAddress(pool)}</Chip>;
  const mcap = marketCapRaw(stats ?? snapshot.data?.stats ?? null);
  return (
    <Chip variant="token" href={href} icon={<Avatar src={null} name={token.symbol} size="xs" decorative />} className={className}>
      <span className="inline-flex items-baseline gap-1">
        <span className="text-text">${token.symbol}</span>
        <span className="font-normal text-text-faint"><span className="sr-only">market cap </span><span aria-hidden="true">MC </span>{mcap === null ? '—' : formatUsdl(mcap, { compact: true, unit: false })}</span>
      </span>
    </Chip>
  );
}
