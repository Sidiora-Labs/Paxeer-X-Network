'use client';
import TabUnavailable from './tab-unavailable';
import { useEffect, useMemo, useState } from 'react';
import { Megaphone } from 'lucide-react';
import { EmptyState, ErrorState, Skeleton, TabStrip, UnavailableState } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { usePoolCallouts } from '@/core/market/use-feed';
import { CalloutCard, CalloutCardSkeleton } from '@/widgets/social/callout-card';
import { useCalloutStats } from '@/widgets/social/hot-callouts-panel';
import { poolCalloutView } from '@/widgets/social/callout-model';
import HoldersTable from './holders-table';
import TradesTable, { type MarketLive } from './trades-table';
import AboutTab from './about-tab';
import { userFacingMessage } from '@/core/user-facing-error';

export const coinTabIds = ['holders', 'trades', 'about', 'callouts'] as const;
export type CoinTab = typeof coinTabIds[number];
const idPrefix = 'coin-tabs';

export function coinTabFromSearch(search: string): CoinTab {
  const value = new URLSearchParams(search).get('tab');
  return (coinTabIds as readonly string[]).includes(value ?? '') ? value as CoinTab : 'holders';
}

function CalloutsTab({ pool }: { pool: string }) {
  const callouts = usePoolCallouts(pool);
  const views = useMemo(() => (callouts.state === 'ready' ? callouts.data.data.items.map(poolCalloutView) : []), [callouts]);
  const statsFor = useCalloutStats(views);
  if (callouts.state === 'loading') return <div aria-busy="true" className="flex flex-col gap-2"><span className="sr-only">Loading callouts</span><CalloutCardSkeleton /><CalloutCardSkeleton /></div>;
  if (callouts.state === 'unsupported') return <UnavailableState title="Callouts for this coin" plural operation={callouts.operation} status={callouts.status} message="Callouts are coming soon. Check back later." />;
  if (callouts.state === 'error') return <ErrorState title="Callouts can't load right now" message={userFacingMessage(callouts.error)} onRetry={callouts.retry} />;
  if (views.length === 0) return <EmptyState icon={Megaphone} title="No callouts yet" body="Be the first to post a callout for this coin." />;
  return <section className="flex flex-col gap-2" aria-label="Callouts">
    {views.map(view => <CalloutCard key={`${view.source}:${view.id}`} callout={view} stats={statsFor(view.poolAddress)} />)}
  </section>;
}

export default function CoinTabs({ poolAddress, live, price = null }: { poolAddress: string; live: MarketLive; price?: number | null }) {
  const [tab, setTab] = useState<CoinTab>('holders');
  const snapshot = usePoolSnapshot(poolAddress);
  useEffect(() => {
    const restore = () => setTab(coinTabFromSearch(window.location.search));
    restore();
    window.addEventListener('popstate', restore);
    window.addEventListener('kindle:restore-navigation', restore);
    return () => { window.removeEventListener('popstate', restore); window.removeEventListener('kindle:restore-navigation', restore); };
  }, []);
  function select(next: CoinTab) {
    setTab(next);
    const url = new URL(window.location.href);
    url.searchParams.set('tab', next);
    window.history.replaceState(window.history.state, '', url);
  }
  const stats = snapshot.data?.stats ?? null;
  const holderCount = stats?.holderCount !== null && stats?.holderCount !== undefined ? Number(stats.holderCount) : null;
  const pool = snapshot.data?.pool.poolAddress ?? null;
  const tabs = [
    { id: 'holders' as const, label: 'Holders', count: holderCount !== null && Number.isSafeInteger(holderCount) ? holderCount : undefined },
    { id: 'trades' as const, label: 'Trades' },
    { id: 'about' as const, label: 'About' },
    { id: 'callouts' as const, label: 'Callouts' },
  ];
  return <section aria-label="Coin details" className="flex flex-col gap-4 rounded-md bg-surface-1 p-3 sm:p-4">
    <TabStrip tabs={tabs} active={tab} onChange={select} label="Coin details" manual idPrefix={idPrefix} />
    <div role="tabpanel" id={`${idPrefix}-panel-${tab}`} aria-labelledby={`${idPrefix}-tab-${tab}`} tabIndex={0} className="min-w-0 outline-none focus-visible:outline-2 focus-visible:outline-focus">
      {tab === 'holders' ? <HoldersTable poolAddress={poolAddress} price={price} /> : null}
      {tab === 'trades' ? <TradesTable poolAddress={poolAddress} live={live} /> : null}
      {tab === 'about' ? <AboutTab poolAddress={poolAddress} /> : null}
      {tab === 'callouts' ? (pool ? <CalloutsTab pool={pool} /> : snapshot.isError ? <TabUnavailable title="Callouts for this coin" error={snapshot.error} onRetry={() => { void snapshot.refetch(); }} /> : <Skeleton variant="rows" rows={4} label="callouts" />) : null}
    </div>
  </section>;
}
