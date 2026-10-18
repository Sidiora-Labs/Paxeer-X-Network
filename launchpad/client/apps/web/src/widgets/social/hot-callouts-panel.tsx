'use client';
import { useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Megaphone } from 'lucide-react';
import { cn } from '@/lib/utils';
import { queryKeys } from '@/core/query-keys';
import { toUnsupported, type Loadable, type Unsupported } from '@/core/market/unsupported';
import { useFeed, usePoolCallouts, type CalloutListResponse, type FeedPage } from '@/core/market/use-feed';
import { statsByPool, useStatsBatch } from '@/core/market/use-stats-batch';
import { Button, EmptyState, ErrorState, UnavailableState } from '@/ui/primitives';
import { CalloutCard, CalloutCardSkeleton } from './callout-card';
import { CalloutComposer } from './callout-composer';
import { calloutPools, feedCardView, poolCalloutView, type CalloutView } from './callout-model';
import type { EmbeddedStatsState } from './embedded-coin-card';
import { FeedSelector, feedTitles, type CalloutFeedKind } from './feed-selector';
import { useViewerWallet } from './use-follow';

const walletPattern = /^0x[0-9a-fA-F]{40}$/;

/**
 * Frame for a degraded-state chip in the list area: the surface-1 card keeps its place and
 * height, and the compact pill (surface-2) sits at its top-left, as in the home board columns.
 */
const noticeFrameClass = 'flex min-h-24 flex-col rounded-md bg-surface-1 p-2';

export interface HotCalloutsPanelProps {
  /** Pool whose "This coin" feed and composer are offered; omitted on the dashboard. */
  poolAddress?: string | null;
  /** Initial feed; "coin" requires poolAddress. */
  initialFeed?: CalloutFeedKind;
  className?: string;
}

function feedViews(page: Loadable<FeedPage>): Loadable<CalloutView[]> {
  return page.state === 'ready' ? { state: 'ready', data: (page.data.Entries ?? []).map(feedCardView) } : page;
}

function poolViews(page: Loadable<CalloutListResponse>): Loadable<CalloutView[]> {
  return page.state === 'ready' ? { state: 'ready', data: page.data.data.items.map(poolCalloutView) } : page;
}

/** Per-pool live stats resolved from one IF_STATS_BATCH request for every pool the cards reference. */
export function useCalloutStats(callouts: readonly CalloutView[]): (pool: string | null) => EmbeddedStatsState {
  const pools = useMemo(() => calloutPools(callouts), [callouts]);
  const batch = useStatsBatch(pools);
  const byPool = useMemo(() => statsByPool(batch.data), [batch.data]);
  return pool => {
    if (!pool || pools.length === 0) return { state: 'ready', stats: null };
    if (batch.status === 'pending') return { state: 'loading' };
    if (batch.status === 'error') {
      const unsupported: Unsupported | null = toUnsupported('IF_STATS_BATCH', batch.error);
      return unsupported ?? { state: 'error', error: batch.error instanceof Error ? batch.error : new Error(String(batch.error)) };
    }
    return { state: 'ready', stats: byPool.get(pool) ?? null };
  };
}

/**
 * Callouts panel for the coin page and dashboard (GAP_AC WP-08). Hot and
 * Latest read IF_CONNECTED_FEED (relevance / chronological); This coin reads
 * IF_CALLOUT_LIST for the pool. The feed's real 503 or 404 renders as an
 * unavailable state with a retry; rows are never synthesised.
 */
export function HotCalloutsPanel({ poolAddress = null, initialFeed, className }: HotCalloutsPanelProps) {
  const pool = poolAddress && walletPattern.test(poolAddress) ? poolAddress.toLowerCase() : null;
  const [feed, setFeed] = useState<CalloutFeedKind>(initialFeed && (initialFeed !== 'coin' || pool) ? initialFeed : 'hot');
  const [composing, setComposing] = useState(false);
  const queryClient = useQueryClient();
  const viewer = useViewerWallet();
  const order = feed === 'latest' ? 'chronological' : 'relevance';
  const page = useFeed(order);
  const coin = usePoolCallouts(feed === 'coin' ? pool : null);
  const source = feed === 'coin' && pool ? poolViews(coin) : feedViews(page);
  const callouts = useMemo(() => (source.state === 'ready' ? source.data : []), [source]);
  const statsFor = useCalloutStats(callouts);

  function retry() {
    void queryClient.invalidateQueries({ queryKey: feed === 'coin' && pool ? queryKeys.poolCallouts(pool, null) : queryKeys.feed(order, null, 50) });
  }

  return (
    <section aria-label={feedTitles[feed]} className={cn('flex min-h-0 flex-col gap-2', className)}>
      <header className="flex h-11 items-center gap-2 px-1">
        <Megaphone aria-hidden="true" className="size-icon-md shrink-0 text-text-muted" />
        <FeedSelector value={feed} onChange={setFeed} coinAvailable={pool !== null} />
        <div className="ml-auto flex items-center gap-1">
          {pool ? (
            <Button variant="ghost" size="sm" icon={<Megaphone />} aria-expanded={composing} onClick={() => setComposing(value => !value)}>
              {composing ? 'Close' : 'Post'}
            </Button>
          ) : null}
        </div>
      </header>

      {pool && composing ? (
        <CalloutComposer poolAddress={pool} onPosted={() => { setComposing(false); setFeed('coin'); }} />
      ) : null}

      <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
        {source.state === 'loading' ? (
          <div aria-busy="true" className="flex flex-col gap-2">
            <span className="sr-only">Loading callouts</span>
            <CalloutCardSkeleton /><CalloutCardSkeleton /><CalloutCardSkeleton />
          </div>
        ) : source.state === 'unsupported' ? (
          <div className={noticeFrameClass}>
            <UnavailableState variant="chip" plural="auto" surface={1} wrap className="self-start" title="Callouts" operation={source.operation} status={source.status} message={source.status === 503 ? 'Check back in a bit' : undefined} onRetry={retry} />
          </div>
        ) : source.state === 'error' ? (
          <div className={noticeFrameClass}>
            <ErrorState variant="chip" wrap className="self-start" message="Can't load callouts right now" error={source.error} onRetry={source.retry} />
          </div>
        ) : callouts.length === 0 ? (
          <div className="rounded-md bg-surface-1">
            <EmptyState
              icon={Megaphone}
              title="No callouts yet"
              body={feed === 'coin' ? (viewer ? 'Be the first to call this coin' : 'Sign in to post the first callout') : 'Callouts from traders show up here'}
              action={pool && viewer && !composing ? <Button variant="secondary" size="sm" icon={<Megaphone />} onClick={() => setComposing(true)}>Post a callout</Button> : null}
              announce
            />
          </div>
        ) : (
          callouts.map(callout => <CalloutCard key={`${callout.source}:${callout.id}`} callout={callout} stats={statsFor(callout.poolAddress)} />)
        )}
      </div>
    </section>
  );
}
