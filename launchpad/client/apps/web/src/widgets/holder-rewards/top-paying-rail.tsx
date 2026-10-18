'use client';

import { HandCoins } from 'lucide-react';
import { Button, EmptyState } from '@/ui/primitives';
import HolderRewardTokenCard from './holder-reward-token-card';
import { launchHolderRewardsHref } from './holder-rewards-header';
import { coinHref, type HolderRewardRow } from './holder-rewards-presentation';

export const topPayingCount = 5;

export interface TopPayingRailProps {
  /** Rows already ordered by cumulative rewards, highest first. */
  rows: readonly HolderRewardRow[];
  loading?: boolean;
}

export default function TopPayingRail({ rows, loading = false }: TopPayingRailProps) {
  const top = rows.slice(0, topPayingCount);
  return (
    <section aria-labelledby="top-paying-title" className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5 px-2">
        <h2 id="top-paying-title" className="text-lg font-medium text-text">Top paying coins</h2>
        <p className="text-sm text-text-muted">The coins that have paid their holders the most USDL</p>
      </div>
      {loading && top.length === 0 ? (
        <ul aria-busy="true" aria-label="Loading top paying coins" className="grid gap-3 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
          {Array.from({ length: topPayingCount }).map((_, index) => (
            <li key={index}><HolderRewardTokenCard loading name="" symbol="" image={null} tokenAddress={null} href={null} createdAt={null} distributedRaw={null} mcapRaw={null} holdersPaid={null} /></li>
          ))}
        </ul>
      ) : top.length === 0 ? (
        <EmptyState
          icon={HandCoins}
          title="No holder rewards paid yet"
          body="Coins that pay holders show up here after their first payout"
          action={<Button variant="secondary" size="sm" href={launchHolderRewardsHref}>Launch one</Button>}
          className="rounded-lg bg-surface-1"
        />
      ) : (
        <ol className="grid gap-3 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
          {top.map((row, index) => (
            <li key={row.poolAddress} className="min-w-0">
              <HolderRewardTokenCard
                rank={index + 1}
                name={row.name}
                symbol={row.symbol}
                image={row.image}
                tokenAddress={row.tokenAddress}
                href={coinHref(row.poolAddress)}
                createdAt={row.pool.createdAt}
                links={row.links}
                distributedRaw={row.pool.distributedRaw}
                mcapRaw={row.mcapRaw}
                holdersPaid={row.pool.holdersPaid}
                strategyActive={row.pool.strategyActive}
              />
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
