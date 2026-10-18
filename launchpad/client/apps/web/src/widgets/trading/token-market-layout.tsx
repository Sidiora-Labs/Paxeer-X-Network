'use client';
import { Suspense, useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import PremiumErrorBoundary from '@/ui/common/error-boundary';
import { SearchX } from 'lucide-react';
import { Button, EmptyState, Skeleton } from '@/ui/primitives';
import type { PoolStats } from '@/core/market/pool-resolver';
import { formatCount } from '@/utils/format';
import { isNotFoundError } from '@/core/user-facing-error';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { useMarketLive } from '@/page-modules/token/market-session-context';
import { priceNumber } from '@/page-modules/token/market-presentation';
import { HotCalloutsPanel } from '@/widgets/social/hot-callouts-panel';
import TokenHeader from '@/widgets/coin/token-header';
import FreshnessNotice from '@/widgets/coin/freshness-notice';
import StatsAuditPanel from '@/widgets/coin/stats-audit-panel';
import PoolInfoCard from '@/widgets/coin/pool-info-card';
import CreatorCard from '@/widgets/coin/creator-card';
import McAthCard from '@/widgets/coin/mc-ath-card';
import CreatorRewardsCard from '@/widgets/coin/creator-rewards-card';
import CoinTabs from '@/widgets/coin/coin-tabs';
import TokenDescription from './token-description';

const TvChart = dynamic(() => import('./tv-chart'), { ssr: false });
const TradePanel = dynamic(() => import('./trade-panel'), { ssr: false });
const LiveStream = dynamic(() => import('./live-stream'), { ssr: false });
const MobileTradeDrawer = dynamic(() => import('./mobile-trade-drawer'), { ssr: false });

const gridClass = [
  'grid grid-cols-1 items-start gap-2',
  'lg:grid-cols-[minmax(0,1fr)_var(--spacing-right-rail-width)] lg:grid-rows-[auto_1fr]',
  'xl:grid-cols-[var(--spacing-callout-column-width)_minmax(0,1fr)_var(--spacing-right-rail-width)]',
  '2xl:grid-cols-[var(--spacing-callout-column-width-wide)_minmax(0,1fr)_var(--spacing-right-rail-width-wide)]',
].join(' ');
/**
 * The callouts feed scrolls inside a viewport-high column. It is deliberately not `position: sticky`:
 * the shell's `<main>` is the nearest scroll container, so a sticky offset there resolves against
 * main's top padding and pushes this column below the other two instead of keeping it flush.
 */
const calloutColumnClass = 'xl:flex xl:max-h-[calc(100vh-var(--spacing-shell-top-total))] xl:flex-col xl:overflow-y-auto';

/** Holder count row (rail position 2, after the trade panel); live stats first, then the snapshot rows. */
function RailHolders({ poolAddress, liveStats }: { poolAddress: string; liveStats: PoolStats | null }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const stats = liveStats ?? snapshot.data?.stats ?? null;
  const count = stats?.holderCount !== null && stats?.holderCount !== undefined ? formatCount(stats.holderCount) : null;
  return (
    <section aria-labelledby="rail-holders-title" className="flex h-10 items-center justify-between gap-2 rounded-md bg-surface-1 px-3">
      <h2 id="rail-holders-title" className="text-sm font-bold text-text">Holders</h2>
      {snapshot.isPending && count === null
        ? <span aria-hidden="true" className="sl-skeleton inline-block h-4 w-10" />
        : <span data-numeric className={`text-sm font-bold tabular-nums ${count ? 'text-text-muted' : 'text-text-faint'}`}>{count ?? '—'}</span>}
    </section>
  );
}

/** Description card for the rail, after market cap vs ATH (pump.fun order). */
function DescriptionCard({ poolAddress }: { poolAddress: string }) {
  return (
    <section aria-labelledby="description-card-title" className="flex flex-col gap-2 rounded-md bg-surface-1 p-3">
      <h2 id="description-card-title" className="text-sm font-bold text-text">Description</h2>
      <TokenDescription poolAddress={poolAddress} />
    </section>
  );
}

/**
 * Neutral state for an address no pool or token resolves to (the resolver answered 404). Not an
 * error: nothing failed, the coin simply does not exist, so there is no reconnect banner or retry.
 */
export function CoinNotFound({ poolAddress }: { poolAddress: string }) {
  return (
    <div data-market-pool={poolAddress.toLowerCase()} data-coin-state="not-found" className="flex w-full flex-col p-2 sm:p-3">
      <section aria-label="Coin not found" className="mx-auto w-full max-w-xl rounded-md bg-surface-1">
        <EmptyState
          icon={SearchX}
          headingLevel={2}
          title="Coin not found"
          body="No coin matches this address. Check the link, or browse the coins trading now."
          action={<Button variant="primary" size="md" href="/explore">Browse coins</Button>}
        />
      </section>
    </div>
  );
}

/** Coin page: hot callouts column, main column (header, chart, tabs) and the right rail; stacks to a single column with the trade drawer on small screens. */
export default function TokenMarketLayout({ poolAddress }: { poolAddress: string }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const data = snapshot.data ?? null;
  const pool = data?.pool.poolAddress ?? null;
  const notFound = snapshot.isError && isNotFoundError(snapshot.error);
  /**
   * One live session per coin page, keyed by the resolved pool. It starts only once resolution has
   * settled and never for a coin the resolver answered 404 for: an unknown address must not poll
   * `/v2/snapshots` for topics that cannot exist. A transient resolver failure (not a 404) still
   * starts the session on the URL address so live data can recover.
   */
  /**
   * Resolution has settled at least once for this address. Sticky on purpose: React Query puts a
   * failed query with no data back to `pending` whenever a new observer mounts and refetches it, and
   * the frames below mount such observers. Gating them on `isPending` alone would unmount them on
   * that refetch and remount them on the next failure, a tight refetch loop.
   */
  const [settledFor, setSettledFor] = useState<string | null>(null);
  useEffect(() => { if (!snapshot.isPending) setSettledFor(poolAddress); }, [snapshot.isPending, poolAddress]);
  const settled = !snapshot.isPending || settledFor === poolAddress;
  const live = useMarketLive(pool ?? poolAddress, { enabled: settled && !notFound });
  const price = priceNumber(live.state.price);
  const liveStats = live.state.stats;
  if (notFound) return <CoinNotFound poolAddress={poolAddress} />;
  /**
   * The pool the market frames render for. Once resolution settles without a 404 the URL already
   * names the pool, so a failed snapshot degrades values to dashes instead of removing the chart,
   * trade panel, tabs and trade drawer. While resolution is pending nothing that opens market
   * traffic mounts (an unknown address must not poll before its 404 arrives).
   */
  const marketPool = pool ?? (settled ? poolAddress : null);
  const notice = !settled ? null : <FreshnessNotice freshness={live.freshness} stream={live.state.stream} error={live.state.error} onReconnect={() => { void live.session.start(); }} />;
  return (
    <div
      data-market-pool={poolAddress.toLowerCase()}
      data-market-stream={live.state.stream}
      data-market-freshness={live.freshness}
      data-market-snapshot={pool ? 'ready' : settled ? 'failed' : 'pending'}
      className="flex w-full flex-col gap-2 p-2 pb-36 motion-reduce:[&_*]:animate-none motion-reduce:[&_*]:transition-none sm:p-3 lg:pb-3"
    >
      <div className={gridClass}>
        <aside aria-label="Hot callouts" className={`hidden min-w-0 xl:col-start-1 xl:row-start-1 xl:row-span-2 ${calloutColumnClass}`}>
          <PremiumErrorBoundary area="Callouts">
            <HotCalloutsPanel poolAddress={pool} initialFeed={pool ? 'coin' : 'hot'} />
          </PremiumErrorBoundary>
        </aside>
        <div className="flex min-w-0 flex-col gap-2 lg:col-start-1 lg:row-start-1 xl:col-start-2">
          <PremiumErrorBoundary area="Token header"><TokenHeader poolAddress={poolAddress} liveStats={liveStats} livePrice={price} now={live.now} notice={notice} /></PremiumErrorBoundary>
          <PremiumErrorBoundary area="TvChart">
            <Suspense fallback={<Skeleton variant="block" height={380} label="price chart" />}>
              {marketPool ? <TvChart poolAddress={marketPool} symbol={data?.token?.symbol} /> : <Skeleton variant="block" height={380} label="price chart" />}
            </Suspense>
          </PremiumErrorBoundary>
        </div>
        <aside aria-label="Trade and coin details" className="grid min-w-0 grid-cols-1 gap-2 md:grid-cols-2 lg:col-start-2 lg:row-start-1 lg:row-span-2 lg:grid-cols-1 xl:col-start-3">
          <div className="hidden min-w-0 lg:block">
            <PremiumErrorBoundary area="TradePanel">
              <Suspense fallback={<Skeleton variant="block" height={320} label="trade panel" />}>
                {marketPool ? <TradePanel poolAddress={marketPool} /> : <Skeleton variant="block" height={320} label="trade panel" />}
              </Suspense>
            </PremiumErrorBoundary>
          </div>
          {pool ? (
            <PremiumErrorBoundary area="LiveStream">
              <Suspense fallback={null}>
                <div className="md:col-span-2 lg:col-span-1 empty:hidden"><LiveStream poolAddress={pool} /></div>
              </Suspense>
            </PremiumErrorBoundary>
          ) : null}
          <div className="md:col-span-2 lg:col-span-1"><RailHolders poolAddress={poolAddress} liveStats={liveStats} /></div>
          <PremiumErrorBoundary area="Stats"><StatsAuditPanel poolAddress={poolAddress} liveStats={liveStats} /></PremiumErrorBoundary>
          <PremiumErrorBoundary area="Pool info"><PoolInfoCard poolAddress={poolAddress} /></PremiumErrorBoundary>
          <PremiumErrorBoundary area="Creator"><CreatorCard poolAddress={poolAddress} /></PremiumErrorBoundary>
          <PremiumErrorBoundary area="Market cap"><McAthCard poolAddress={poolAddress} liveStats={liveStats} /></PremiumErrorBoundary>
          <PremiumErrorBoundary area="Description"><DescriptionCard poolAddress={poolAddress} /></PremiumErrorBoundary>
          <PremiumErrorBoundary area="Creator rewards"><CreatorRewardsCard poolAddress={poolAddress} /></PremiumErrorBoundary>
        </aside>
        <div className="min-w-0 lg:col-start-1 lg:row-start-2 xl:col-start-2">
          {marketPool ? (
            <PremiumErrorBoundary area="Coin tabs"><CoinTabs poolAddress={marketPool} live={live} price={price} /></PremiumErrorBoundary>
          ) : <Skeleton variant="block" height={240} label="coin details" />}
        </div>
      </div>
      {marketPool ? <MobileTradeDrawer poolAddress={marketPool} /> : null}
    </div>
  );
}
