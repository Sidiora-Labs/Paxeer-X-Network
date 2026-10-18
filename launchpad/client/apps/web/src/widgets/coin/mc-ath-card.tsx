'use client';
import { UnavailableState } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import type { PoolStats } from '@/core/market/pool-resolver';
import { formatUsdl } from '@/utils/format';
import { statsCirculatingMarketCap, statsMarketCap } from '@/core/market/pool-stats-display';
import { StatDash } from '@/ui/shared/stat-value';

/**
 * Market cap versus all-time-high as one compact row (pump.fun: "MC $102K [bar] ATH $133K"). No
 * served contract carries an ATH valuation (IF_POOL_STATS and the legacy /stats row have none), so
 * the bar stays an empty track and the ATH reads as a dash; only the live market cap is a value.
 */
export default function McAthCard({ poolAddress, liveStats }: { poolAddress: string; liveStats: PoolStats | null }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const stats = liveStats ?? snapshot.data?.stats ?? null;
  // Market cap is price x total supply (PoolStats.totalSupplyValuationUsdlRaw), the pump.fun figure.
  const mcapStat = statsMarketCap(stats);
  const circulating = statsCirculatingMarketCap(stats).value;
  const mcap = mcapStat.value === null ? null : formatUsdl(mcapStat.value, { compact: true });
  const loading = snapshot.isPending && !stats;
  return (
    <section aria-labelledby="mc-ath-title" className="flex flex-col gap-1.5 rounded-md bg-surface-1 px-3 py-2.5">
      <h2 id="mc-ath-title" className="sr-only">Market cap vs ATH</h2>
      <dl className="flex items-center gap-3 text-xs">
        <div className="flex shrink-0 items-baseline gap-1">
          <dt className="text-text-faint">MC</dt>
          <dd data-numeric className={`text-sm font-bold tabular-nums ${mcap ? 'text-text' : 'text-text-faint'}`} aria-busy={loading || undefined}>
            {loading
              ? <span aria-hidden="true" className="sl-skeleton inline-block h-3.5 w-12 align-middle" />
              : mcap !== null
                ? <span title={circulating ? `Circulating market cap ${formatUsdl(circulating, { compact: true })}` : undefined}>{mcap}</span>
                : stats ? <StatDash reason={mcapStat.reason ?? 'Market cap not available yet'} subject="Market cap" align="start" /> : '—'}
          </dd>
        </div>
        <div aria-hidden="true" className="h-1 min-w-8 flex-1 rounded-pill bg-track" />
        <div className="flex shrink-0 items-baseline gap-1">
          <dt className="text-text-faint">ATH</dt>
          <dd data-numeric className="text-sm font-bold tabular-nums text-text-faint"><span aria-label="No data">—</span></dd>
        </div>
      </dl>
      <UnavailableState variant="inline" title="All-time high" operation="IF_POOL_STATS.athUsdlRaw" status={null} className="sr-only" />
    </section>
  );
}
