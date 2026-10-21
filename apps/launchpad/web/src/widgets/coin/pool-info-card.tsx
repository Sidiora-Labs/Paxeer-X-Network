'use client';
import { AgeChip, KeyValueRow, ShortAddress, UnavailableState } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { formatUsdl } from '@/utils/format';
import { statsCirculatingMarketCap } from '@/core/market/pool-stats-display';
import type { PoolStats } from '@/core/market/use-stats-batch';

function explorerAddress(address: string): string | null {
  return safeExternalDestination(`https://paxscan.paxeer.app/address/${address}`);
}

const rowClass = 'min-h-8 py-0.5';

/** Secondary valuation: price x circulating supply (PoolStats.circulatingMarketCapUsdlRaw); the headline market cap is the total-supply valuation. */
function circulatingRow(stats: PoolStats): string | null {
  const value = statsCirculatingMarketCap(stats).value;
  return value === null ? null : formatUsdl(value, { compact: true });
}

/**
 * Pool information: quote asset (header chip), on-pool USDL liquidity, indicative TVL when served, and
 * identity. Compact rows, one footnote at most. Values come from IF_POOL_STATS only.
 */
export default function PoolInfoCard({ poolAddress }: { poolAddress: string }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const data = snapshot.data ?? null;
  const stats = data?.stats ?? null;
  const loading = snapshot.isPending;
  const unavailable = data?.unavailable.stats ?? null;
  return (
    <section aria-labelledby="pool-info-title" className="flex flex-col gap-2 rounded-md bg-surface-1 p-3">
      <div className="flex items-center justify-between gap-2">
        <h2 id="pool-info-title" className="text-sm font-bold text-text">Pool information</h2>
        <span className="rounded-pill bg-surface-2 px-2 py-0.5 text-xs text-text-muted">Paired with <span className="font-bold text-text">USDL</span></span>
      </div>
      <dl className="flex flex-col rounded-md bg-surface-2 px-3 py-1">
        <KeyValueRow className={rowClass} label="Total liquidity" loading={loading} value={stats ? formatUsdl(stats.realUsdlBalanceRaw, { compact: true }) : null} />
        {stats ? <KeyValueRow className={rowClass} label="Circulating market cap" value={circulatingRow(stats)} /> : null}
        {stats?.indicativeTvlUsdlRaw ? <KeyValueRow className={rowClass} label="Total value locked" value={formatUsdl(stats.indicativeTvlUsdlRaw, { compact: true })} /> : null}
        <KeyValueRow className={rowClass} label="Pool" loading={loading} value={data ? <ShortAddress address={data.pool.poolAddress} explorerHref={explorerAddress(data.pool.poolAddress)} className="text-text" /> : null} />
        <KeyValueRow className={rowClass} label="Coin" loading={loading} value={data ? <ShortAddress address={data.pool.tokenAddress} explorerHref={explorerAddress(data.pool.tokenAddress)} className="text-text" /> : null} />
        {data?.createdAt || loading ? <KeyValueRow className={rowClass} label="Created" loading={loading} value={data?.createdAt ? <AgeChip iso={data.createdAt} compact={false} className="text-text" /> : null} /> : null}
      </dl>
      {snapshot.isError
        ? <UnavailableState variant="chip" wrap title="Pool information" plural="auto" operation="IF_POOL_STATS" status={null} onRetry={() => { void snapshot.refetch(); }} className="self-start" />
        : !loading && !stats && unavailable ? <UnavailableState variant="inline" title="Liquidity" operation={unavailable.operation} status={unavailable.status} className="text-xs" /> : null}
    </section>
  );
}
