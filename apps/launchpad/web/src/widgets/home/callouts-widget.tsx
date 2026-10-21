'use client';
import { useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import { useQueryClient } from '@tanstack/react-query';
import { Megaphone, Pause, Play } from 'lucide-react';
import { cn } from '@/lib/utils';
import { EmptyState, ErrorState, UnavailableState } from '@/ui/primitives';
import { useFeed, type FeedOrder } from '@/core/market/use-feed';
import { queryKeys } from '@/core/query-keys';
import { CalloutCard, CalloutCardSkeleton } from '@/widgets/social/callout-card';
import { feedCardView, type CalloutView } from '@/widgets/social/callout-model';
import { useCalloutStats } from '@/widgets/social/hot-callouts-panel';
import type { WidgetProps } from './widget-registry';
import { columnToolbarClass } from './panel-state';
import { useColumnHeaderSlot } from './column-slot';

export type CalloutsFeed = 'latest' | 'hot';

export const calloutFeeds: { value: CalloutsFeed; label: string }[] = [
  { value: 'latest', label: 'Latest' },
  { value: 'hot', label: 'Hot' },
];

export function feedOrderFor(feed: CalloutsFeed): FeedOrder {
  return feed === 'latest' ? 'chronological' : 'relevance';
}

export const calloutsUnavailableMessage = 'Callouts are coming soon';

export const calloutsEmpty: Record<CalloutsFeed, { title: string; body: string }> = {
  latest: { title: 'No callouts yet', body: 'Calls from traders show up here as soon as they are posted' },
  hot: { title: 'No hot callouts yet', body: 'The most talked-about calls rise to the top here' },
};

export function matchesCallout(callout: CalloutView, filter: string): boolean {
  const needle = filter.trim().toLowerCase();
  if (!needle) return true;
  return callout.body.toLowerCase().includes(needle) || (callout.poolAddress ?? '').includes(needle) || (callout.authorWallet ?? '').includes(needle) || callout.authorId.toLowerCase().includes(needle);
}

export interface CalloutsFeedState {
  page: ReturnType<typeof useFeed>;
  visible: CalloutView[];
  statsFor: ReturnType<typeof useCalloutStats>;
  paused: boolean;
  pending: number;
  togglePause(): void;
  retry(): void;
}

/**
 * One connected discovery feed, filtered. Pausing freezes the list the viewer is reading while
 * the query keeps refreshing; `pending` counts what arrived meanwhile.
 */
export function useCalloutsFeed(feed: CalloutsFeed, filter: string): CalloutsFeedState {
  const [paused, setPaused] = useState(false);
  const order = feedOrderFor(feed);
  const page = useFeed(order);
  const queryClient = useQueryClient();
  const live = useMemo<CalloutView[]>(() => (page.state === 'ready' ? page.data.Entries.map(feedCardView) : []), [page]);
  const [frozen, setFrozen] = useState<CalloutView[] | null>(null);
  useEffect(() => { setPaused(false); setFrozen(null); }, [feed]);
  const callouts = paused && frozen ? frozen : live;
  const visible = useMemo(() => callouts.filter(callout => matchesCallout(callout, filter)), [callouts, filter]);
  const statsFor = useCalloutStats(visible);
  const pending = paused && frozen ? live.length - frozen.length : 0;
  return {
    page,
    visible,
    statsFor,
    paused,
    pending,
    togglePause() { setFrozen(paused ? null : live); setPaused(!paused); },
    retry() { void queryClient.invalidateQueries({ queryKey: queryKeys.feed(order, null, 50) }); },
  };
}

/** Live / paused toggle for a callouts feed. */
export function FeedPauseButton({ state, compact = false, className }: { state: Pick<CalloutsFeedState, 'paused' | 'pending' | 'togglePause'>; compact?: boolean; className?: string }) {
  const { paused, pending } = state;
  return (
    <button
      type="button"
      aria-pressed={paused}
      onClick={state.togglePause}
      className={cn('inline-flex h-control-height-sm shrink-0 items-center gap-1.5 whitespace-nowrap rounded-pill text-xs font-bold', compact ? 'px-2' : 'px-3', paused ? 'bg-warning-soft text-warning' : 'bg-surface-2 text-text-muted hover:bg-surface-hover hover:text-text', className)}
    >
      {paused ? <Play aria-hidden="true" className="size-3" /> : <Pause aria-hidden="true" className="size-3" />}
      <span className={cn(compact && 'sr-only @min-[340px]:not-sr-only')}>{paused ? (pending > 0 ? `Paused, ${pending} new` : 'Paused') : 'Live'}</span>
    </button>
  );
}

/** The feed body: skeleton, unavailable, error, empty or the callout cards. A polite live region only while live. */
export function CalloutsFeedBody({ feed, filter, state, className }: { feed: CalloutsFeed; filter: string; state: CalloutsFeedState; className?: string }) {
  const { page, visible, statsFor, paused } = state;
  return (
    <div aria-live={paused ? 'off' : 'polite'} aria-busy={page.state === 'loading'} className={cn('flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto', className)}>
      {page.state === 'loading' ? (
        <>
          <span className="sr-only">Loading callouts</span>
          <CalloutCardSkeleton /><CalloutCardSkeleton /><CalloutCardSkeleton />
        </>
      ) : page.state === 'unsupported' ? (
        <UnavailableState variant="chip" plural="auto" surface={1} wrap className="self-start" title="Callouts" operation={page.operation} status={page.status} message={page.status === 503 ? calloutsUnavailableMessage : page.message} onRetry={state.retry} />
      ) : page.state === 'error' ? (
        <ErrorState variant="chip" className="self-start" message="Couldn't load callouts" error={page.error} onRetry={page.retry} />
      ) : visible.length === 0 ? (
        <EmptyState icon={Megaphone} title={filter.trim() ? 'No callouts match this filter' : calloutsEmpty[feed].title} body={filter.trim() ? `Nothing matches "${filter.trim()}"` : calloutsEmpty[feed].body} />
      ) : (
        visible.map(callout => <CalloutCard key={`${callout.source}:${callout.id}`} callout={callout} stats={statsFor(callout.poolAddress)} />)
      )}
    </div>
  );
}

export interface CalloutsWidgetProps extends WidgetProps {
  /** Which feed this column reads. Each column is one feed, named in its header (Latest callouts, Hot callouts). */
  feed?: CalloutsFeed;
}

/**
 * Callouts column (WP-09, features.csv 151-152): one connected discovery feed
 * as callout cards, with a live / paused toggle in the column header.
 */
export function CalloutsWidget({ filter, feed = 'latest' }: CalloutsWidgetProps) {
  const state = useCalloutsFeed(feed, filter);
  const headerSlot = useColumnHeaderSlot();
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* In a board column the Live / Paused pill sits in the header's right cluster (no tab row, as on pump.fun). */}
      {headerSlot === undefined ? (
        <div className={columnToolbarClass}>
          <FeedPauseButton state={state} className="ml-auto" />
        </div>
      ) : headerSlot ? createPortal(<FeedPauseButton state={state} compact />, headerSlot) : null}
      <CalloutsFeedBody feed={feed} filter={filter} state={state} className="px-2 pb-2" />
    </div>
  );
}

/** Hot callouts column: the relevance-ordered feed (pump.fun's second default column). */
export function HotCalloutsWidget(props: WidgetProps) {
  return <CalloutsWidget {...props} feed="hot" />;
}

export default CalloutsWidget;
