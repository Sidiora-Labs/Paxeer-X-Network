'use client';
import { useQueries } from '@tanstack/react-query';
import { Chip, Skeleton } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { formatTokenAmount, formatUsdl, shortAddress } from '@/utils/format';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { useOperations } from '@/core/market/operations';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { resolvePoolSnapshot, type PoolSnapshot } from '@/core/market/pool-resolver';
import { bountiesContract, type Bounty, type BountyValuation, type ValuationContext } from './bounties-contract';

export type { BountyValuation, ValuationContext } from './bounties-contract';

function contextOf(snapshot: PoolSnapshot | undefined): ValuationContext {
  return { token: snapshot?.token ?? null, stats: snapshot?.stats ?? null };
}

export interface BountyValuations {
  valuationOf(bounty: Bounty): BountyValuation | null;
  contextOf(bounty: Bounty): ValuationContext;
  isPending: boolean;
}

/**
 * Valuations for a list of bounties. Rewards paid in a launched token are
 * valued through the linked pool's snapshot (shared cache key with
 * usePoolSnapshot, so cards and rails never request a pool twice).
 */
export function useBountyValuations(bounties: readonly Bounty[]): BountyValuations {
  const operations = useOperations();
  const pools = [...new Set(bounties.filter(bountiesContract.needsPoolValuation).map(bounty => (bounty.poolAddress as string).toLowerCase()))].sort();
  return useQueries({
    queries: pools.map(pool => ({
      queryKey: queryKeys.poolSnapshot(pool),
      queryFn: ({ signal }: { signal: AbortSignal }) => resolvePoolSnapshot(pool, { operations }, signal),
      ...marketCachePolicy.poolSnapshot,
    })),
    combine: results => {
      const snapshots = new Map<string, PoolSnapshot>();
      let isPending = false;
      pools.forEach((pool, index) => {
        const result = results[index];
        if (!result || result.status === 'pending') isPending = true;
        else if (result.status === 'success') snapshots.set(pool, result.data);
      });
      const context = (bounty: Bounty) => contextOf(bounty.poolAddress ? snapshots.get(bounty.poolAddress.toLowerCase()) : undefined);
      return { isPending, contextOf: context, valuationOf: (bounty: Bounty) => bountiesContract.usdlValue(bounty, context(bounty)) };
    },
  });
}

export interface BountyRewardProps {
  bounty: Bounty;
  /** Supply the pool context yourself (rails, tables); omitted, the component resolves the linked pool when it has to. */
  context?: ValuationContext;
  size?: 'md' | 'lg';
  className?: string;
}

/**
 * Reward value in USDL plus the escrowed asset amount. USDL rewards and
 * service-reported values are exact; a launched-token reward valued through
 * the pool's supply valuation is marked approximate. Nothing is estimated
 * beyond that: without a real valuation path the value cell says so.
 */
export function BountyReward({ bounty, context, size = 'md', className }: BountyRewardProps) {
  const resolveSelf = context === undefined && bountiesContract.needsPoolValuation(bounty);
  const snapshot = usePoolSnapshot(resolveSelf ? bounty.poolAddress : null);
  const resolved: ValuationContext = context ?? contextOf(snapshot.data);
  const valuation = bountiesContract.usdlValue(bounty, resolved);
  const pending = valuation === null && resolveSelf && snapshot.isPending;
  const kind = bountiesContract.rewardAssetKind(bounty.assetAddress);
  const token = resolved.token && resolved.token.address.toLowerCase() === bounty.assetAddress.toLowerCase() ? resolved.token : null;
  const symbol = kind === 'usdl' ? 'USDL' : token?.symbol ?? shortAddress(bounty.assetAddress);
  const amount = kind === 'usdl'
    ? formatUsdl(bounty.budgetRaw, { decimals: bountiesContract.usdlDecimals, unit: false, compact: true })
    : token && token.decimals !== null ? formatTokenAmount(bounty.budgetRaw, token.decimals) : null;

  return (
    <div className={cn('flex flex-wrap items-center gap-2', className)}>
      {valuation ? (
        <span data-numeric className={cn('font-bold tabular-nums text-success', size === 'lg' ? 'text-xl' : 'text-lg')}>
          {valuation.exact ? null : <span title="Estimated from the coin's market cap"><span aria-hidden="true">~</span><span className="sr-only">approximately </span></span>}
          {formatUsdl(valuation.usdlRaw, { compact: true })}
        </span>
      ) : pending ? (
        <Skeleton width={72} height={size === 'lg' ? 28 : 24} label="Loading reward value" />
      ) : (
        <span className="text-xs text-text-faint">Value not available yet</span>
      )}
      <Chip variant="token">
        <span className="sr-only">Reward </span>
        {amount !== null ? `${amount} ${symbol}` : symbol}
      </Chip>
    </div>
  );
}
