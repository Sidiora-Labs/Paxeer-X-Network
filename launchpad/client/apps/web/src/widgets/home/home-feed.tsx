'use client';
import { useId, useRef, useState, type KeyboardEvent } from 'react';
import { ChevronUp } from 'lucide-react';
import { cn } from '@/lib/utils';
import { CalloutsFeedBody, FeedPauseButton, useCalloutsFeed, type CalloutsFeed } from './callouts-widget';

/**
 * Feed tabs on the compact home. pump.fun shows Callouts / Friends / Top; the connected feed
 * serves chronological and relevance orders only (IF_CONNECTED_FEED), so there is no Friends tab
 * until a following feed exists.
 */
export const homeFeedTabs: { value: CalloutsFeed; label: string }[] = [
  { value: 'latest', label: 'Callouts' },
  { value: 'hot', label: 'Top' },
];

/**
 * Home below lg (768 and 390): a single centred callouts feed, as on pump.fun, instead of the
 * column board. Large tab titles, a live / paused toggle, a "new callouts" pill while paused,
 * then full-width callout cards. No board customisation on touch.
 */
export function HomeFeed({ className }: { className?: string }) {
  const [feed, setFeed] = useState<CalloutsFeed>('latest');
  const state = useCalloutsFeed(feed, '');
  const id = useId();
  const refs = useRef<Array<HTMLButtonElement | null>>([]);

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const delta = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
    if (!delta) return;
    event.preventDefault();
    const next = (index + delta + homeFeedTabs.length) % homeFeedTabs.length;
    setFeed(homeFeedTabs[next].value);
    refs.current[next]?.focus();
  }

  return (
    <div data-home-feed="" className={cn('mx-auto flex w-full max-w-[480px] flex-col gap-3 px-4 pb-6 pt-4', className)}>
      <div className="flex items-center gap-4">
        <div role="tablist" aria-label="Callout feed" className="flex min-w-0 items-baseline gap-5">
          {homeFeedTabs.map((tab, index) => {
            const selected = tab.value === feed;
            return (
              <button
                key={tab.value}
                ref={element => { refs.current[index] = element; }}
                type="button"
                role="tab"
                id={`${id}-tab-${tab.value}`}
                aria-selected={selected}
                aria-controls={`${id}-panel`}
                tabIndex={selected ? 0 : -1}
                onClick={() => setFeed(tab.value)}
                onKeyDown={event => onKeyDown(event, index)}
                className={cn('whitespace-nowrap text-lg font-bold transition-colors duration-base', selected ? 'text-text' : 'text-text-faint hover:text-text-muted')}
              >
                {tab.label}
              </button>
            );
          })}
        </div>
        <FeedPauseButton state={state} className="ml-auto" />
      </div>
      {state.paused && state.pending > 0 ? (
        <button
          type="button"
          onClick={state.togglePause}
          className="inline-flex h-control-height-sm w-full items-center justify-center gap-1.5 rounded-sm bg-surface-2 text-xs font-bold text-text hover:bg-surface-hover"
        >
          <ChevronUp aria-hidden="true" className="size-icon-sm" />
          {state.pending} new {state.pending === 1 ? 'callout' : 'callouts'}
        </button>
      ) : null}
      <div role="tabpanel" id={`${id}-panel`} aria-labelledby={`${id}-tab-${feed}`} className="flex flex-col">
        <CalloutsFeedBody feed={feed} filter="" state={state} />
      </div>
    </div>
  );
}

export default HomeFeed;
