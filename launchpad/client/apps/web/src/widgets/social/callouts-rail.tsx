'use client';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { ArrowUp, Megaphone } from 'lucide-react';
import { cn } from '@/lib/utils';
import { queryKeys } from '@/core/query-keys';
import { useFeed } from '@/core/market/use-feed';
import { Button, EmptyState, ErrorState, IconButton, SectionHeader, SegmentedControl, UnavailableState } from '@/ui/primitives';
import { CalloutCard, CalloutCardSkeleton } from './callout-card';
import { calloutWindows, feedCardView, withinWindow, type CalloutWindow } from './callout-model';
import { useCalloutStats } from './hot-callouts-panel';

export interface CalloutsRailProps {
  title?: string;
  /** Initial window; 1D keeps the last 24 hours of the relevance feed, 1W the last 7 days. */
  initialWindow?: CalloutWindow;
  /** Height of the scrolling list; defaults to filling the rail. */
  maxHeight?: number | string;
  className?: string;
}

const windowOptions = [{ value: '1d' as const, label: '1D' }, { value: '1w' as const, label: '1W' }];

/**
 * Best callouts rail (GAP_BD WP-BD-6): header with a 1D/1W segmented control,
 * the relevance-ordered discovery feed filtered to the chosen window, and a
 * floating scroll-to-top button that appears once the rail has scrolled.
 * The feed's real 503 today renders as an unavailable state.
 */
export function CalloutsRail({ title = 'Best callouts', initialWindow = '1d', maxHeight, className }: CalloutsRailProps) {
  const [range, setRange] = useState<CalloutWindow>(initialWindow);
  const [scrolled, setScrolled] = useState(false);
  const listRef = useRef<HTMLDivElement | null>(null);
  const queryClient = useQueryClient();
  const feed = useFeed('relevance');
  const all = useMemo(() => (feed.state === 'ready' ? (feed.data.Entries ?? []).map(feedCardView) : []), [feed]);
  const callouts = useMemo(() => withinWindow(all, calloutWindows[range]), [all, range]);
  const statsFor = useCalloutStats(callouts);

  useEffect(() => {
    const node = listRef.current;
    if (!node) return;
    const onScroll = () => setScrolled(node.scrollTop > 240);
    node.addEventListener('scroll', onScroll, { passive: true });
    return () => node.removeEventListener('scroll', onScroll);
  }, [feed.state]);

  function scrollToTop() {
    listRef.current?.scrollTo({ top: 0, behavior: 'smooth' });
    listRef.current?.focus();
  }

  return (
    <aside aria-label={title} className={cn('relative flex min-h-0 flex-col gap-3', className)}>
      <SectionHeader
        title={title}
        count={feed.state === 'ready' ? callouts.length : undefined}
        loading={feed.state === 'loading'}
        description={feed.state === 'ready' ? `Top calls from the last ${range === '1d' ? 'day' : 'week'}` : undefined}
        action={<SegmentedControl label="Time range" options={windowOptions} value={range} onChange={setRange} />}
      />
      <div ref={listRef} tabIndex={-1} style={maxHeight !== undefined ? { maxHeight } : undefined} className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto outline-none">
        {feed.state === 'loading' ? (
          <div aria-busy="true" className="flex flex-col gap-2">
            <span className="sr-only">Loading callouts</span>
            <CalloutCardSkeleton /><CalloutCardSkeleton />
          </div>
        ) : feed.state === 'unsupported' ? (
          <div className="rounded-md bg-surface-1">
            <UnavailableState title="Callouts" plural="auto" operation={feed.operation} status={feed.status} message={feed.status === 503 ? 'Check back in a bit' : undefined} onRetry={() => { void queryClient.invalidateQueries({ queryKey: queryKeys.feed('relevance', null, 50) }); }} />
          </div>
        ) : feed.state === 'error' ? (
          <div className="rounded-md bg-surface-1"><ErrorState title="Can't load callouts right now" message="Check your connection and try again" onRetry={feed.retry} /></div>
        ) : callouts.length === 0 ? (
          <div className="rounded-md bg-surface-1">
            <EmptyState icon={Megaphone} title={all.length === 0 ? 'No callouts yet' : `No callouts in the last ${range === '1d' ? 'day' : 'week'}`} body={all.length === 0 ? 'Callouts from traders show up here' : range === '1d' ? 'Try the last week' : 'Check back soon'} action={all.length === 0 ? <Button variant="secondary" size="sm" href="/explore">Browse coins</Button> : range === '1d' ? <Button variant="secondary" size="sm" onClick={() => setRange('1w')}>Show the last week</Button> : null} announce />
          </div>
        ) : (
          callouts.map(callout => <CalloutCard key={callout.id} callout={callout} stats={statsFor(callout.poolAddress)} variant="rail" />)
        )}
      </div>
      {scrolled ? (
        <IconButton label="Back to top" icon={<ArrowUp />} variant="round" size="md" onClick={scrollToTop} className="absolute bottom-3 right-3 shadow-pop" />
      ) : null}
    </aside>
  );
}
