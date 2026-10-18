'use client';

import { useCallback, useEffect, useRef, useState, useSyncExternalStore, type CSSProperties, type KeyboardEvent } from 'react';
import Link from 'next/link';
import { accountSession, subscribeAccount } from '@/widgets/profile/account-transport';
import { openAuthModal } from './auth-modal-store';
import { Avatar, Tooltip, UnavailableState } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import {
  isTickerMode,
  tickerModes,
  useEarnersTicker,
  useFollowingTicker,
  useHoldingsTicker,
  useRecentTicker,
  useWatchlistTicker,
  type ChipTone,
  type TickerChip,
  type TickerFeed,
  type TickerMode,
  type TickerModeConfig,
} from './use-ticker';

const MODE_KEY = 'sidiora-ticker-mode';
const modeListeners = new Set<() => void>();

function readMode(): TickerMode | null {
  try { const value = localStorage.getItem(MODE_KEY); return isTickerMode(value) ? value : null; } catch { return null; }
}
function writeMode(mode: TickerMode) {
  try { localStorage.setItem(MODE_KEY, mode); } catch {}
  for (const listener of modeListeners) listener();
}
function subscribeMode(listener: () => void) {
  modeListeners.add(listener);
  const onStorage = (event: StorageEvent) => { if (event.key === MODE_KEY) listener(); };
  window.addEventListener('storage', onStorage);
  return () => { modeListeners.delete(listener); window.removeEventListener('storage', onStorage); };
}
const noChoice = (): TickerMode | null => null;

/** md (768) and up, where the strip is shown; below it the strip is hidden and its feed is not mounted. */
const WIDE = '(min-width: 768px)';
function subscribeWide(listener: () => void) {
  if (typeof window.matchMedia !== 'function') return () => {};
  const query = window.matchMedia(WIDE);
  query.addEventListener('change', listener);
  return () => query.removeEventListener('change', listener);
}
const readWide = () => typeof window.matchMedia !== 'function' || window.matchMedia(WIDE).matches;
const serverWide = () => true;
const noSession = () => null;

const toneClass: Record<ChipTone, string> = {
  up: 'text-percent-up',
  down: 'text-percent-down',
  flat: 'text-percent-flat',
  neutral: 'text-text-muted',
};

function ChipLink({ chip, hidden }: { chip: TickerChip; hidden: boolean }) {
  return (
    <Link
      href={chip.href}
      tabIndex={hidden ? -1 : undefined}
      className="flex h-6 shrink-0 items-center gap-1.5 rounded-pill bg-surface-1 pl-0.5 pr-2 text-xs transition-colors duration-fast hover:bg-surface-hover"
    >
      <Avatar src={chip.image} name={chip.label} size="xs" shape={chip.avatarShape} decorative />
      <span className="max-w-48 truncate font-bold text-text">{chip.label}</span>
      {chip.value ? <span data-numeric className={cn('font-bold tabular-nums', toneClass[chip.tone])}>{chip.value}</span> : null}
    </Link>
  );
}

/**
 * How many copies of the chip list the strip renders. The list is shown once
 * when it fits (or holds a single entry): no animation, no repeated chips. Only
 * a list wider than the strip scrolls, and a seamless loop then needs exactly one
 * trailing copy (the keyframes translate by -50%), so no chip ever appears more
 * than twice and never to pad out empty width.
 */
export function marqueeCopies(chipCount: number, available: number, listWidth: number): 1 | 2 {
  if (chipCount <= 1) return 1;
  if (!(available > 0) || !(listWidth > 0)) return 1;
  return listWidth > available ? 2 : 1;
}

function Marquee({ chips, mode }: { chips: TickerChip[]; mode: TickerModeConfig }) {
  const container = useRef<HTMLDivElement | null>(null);
  const firstSet = useRef<HTMLUListElement | null>(null);
  const [copies, setCopies] = useState<1 | 2>(1);

  useEffect(() => {
    const outer = container.current;
    const set = firstSet.current;
    if (!outer || !set || typeof ResizeObserver === 'undefined') return;
    const measure = () => setCopies(marqueeCopies(chips.length, outer.clientWidth, set.scrollWidth));
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(outer);
    observer.observe(set);
    return () => observer.disconnect();
  }, [chips]);

  const scrolling = copies === 2;
  const duration = `${Math.max(24, chips.length * 5)}s`;
  const style = scrolling ? ({ '--sl-marquee-duration': duration } as CSSProperties) : undefined;

  return (
    <div
      ref={container}
      className="relative h-full overflow-hidden motion-reduce:overflow-x-auto"
    >
      <p className="sr-only">{mode.label}, {chips.length} {chips.length === 1 ? 'entry' : 'entries'}.</p>
      <Link href={mode.href} className="sr-only focus:not-sr-only focus:absolute focus:left-0 focus:top-1 focus:z-[var(--sl-z-raised)] focus:rounded-sm focus:bg-surface-3 focus:px-2 focus:text-xs focus:text-text">
        Open {mode.label.toLowerCase()}
      </Link>
      <div data-ticker-scrolling={scrolling ? '' : undefined} className={cn('flex h-full w-max items-center', scrolling && 'sl-marquee')} style={style} aria-live="off">
        {Array.from({ length: copies }).map((_, index) => (
          <ul key={index} ref={index === 0 ? firstSet : undefined} aria-hidden={index > 0 || undefined} className="flex items-center gap-1 pr-1">
            {chips.map(chip => <li key={chip.key}><ChipLink chip={chip} hidden={index > 0} /></li>)}
          </ul>
        ))}
      </div>
    </div>
  );
}

function FeedView({ feed, mode }: { feed: TickerFeed; mode: TickerModeConfig }) {
  switch (feed.state) {
    case 'loading':
      return (
        // No placeholder pills: a feed that never answers must not read as a
        // permanent skeleton. The strip stays an empty canvas row (the mode
        // switcher still shows which feed is selected) until chips arrive.
        <div aria-busy="true" data-ticker-loading="" className="h-full">
          <span className="sr-only">Loading {mode.label.toLowerCase()}</span>
        </div>
      );
    case 'unsupported':
      return <UnavailableState variant="inline" title={mode.label} operation={feed.operation} status={feed.status} className="truncate text-xs" />;
    case 'error':
      // Transient failures were already retried quietly (core/transient-retry);
      // what is left reads as a status line like the empty state, never an alert.
      return (
        <p role="status" className="truncate text-xs text-text-faint">
          Couldn't load {mode.label.toLowerCase()}.{' '}
          <button type="button" onClick={feed.retry} className="font-bold text-text-muted transition-colors duration-fast hover:text-text">Retry</button>
        </p>
      );
    case 'signed-out':
      return (
        <button
          type="button"
          onClick={() => openAuthModal('ticker-holdings')}
          className="flex h-6 items-center rounded-pill bg-surface-1 px-3 text-xs font-bold text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text"
        >
          {feed.prompt}
        </button>
      );
    case 'ready':
      if (feed.chips.length === 0) {
        return (
          // Only the message may truncate; the open link never shrinks. Narrow screens get the short message.
          <p data-ticker-empty="" className="flex h-full min-w-0 items-center gap-1 text-xs text-text-faint">
            <span className="min-w-0 truncate">
              <span className="sm:hidden">{feed.emptyShort ?? feed.emptyText}</span>
              <span className="hidden sm:inline">{feed.emptyText}</span>
            </span>
            <Link href={mode.href} className="shrink-0 whitespace-nowrap font-bold text-text-muted hover:text-text">Open {mode.label.toLowerCase()}</Link>
          </p>
        );
      }
      return <Marquee chips={feed.chips} mode={mode} />;
  }
}

function WatchlistFeed({ mode }: { mode: TickerModeConfig }) { return <FeedView feed={useWatchlistTicker()} mode={mode} />; }
function HoldingsFeed({ mode }: { mode: TickerModeConfig }) { return <FeedView feed={useHoldingsTicker()} mode={mode} />; }
function RecentFeed({ mode }: { mode: TickerModeConfig }) { return <FeedView feed={useRecentTicker()} mode={mode} />; }
function EarnersFeed({ mode }: { mode: TickerModeConfig }) { return <FeedView feed={useEarnersTicker()} mode={mode} />; }
function FollowingFeed({ mode }: { mode: TickerModeConfig }) { return <FeedView feed={useFollowingTicker()} mode={mode} />; }

const feeds: Record<TickerMode, (props: { mode: TickerModeConfig }) => React.JSX.Element> = {
  watchlist: WatchlistFeed,
  holdings: HoldingsFeed,
  recent: RecentFeed,
  earners: EarnersFeed,
  following: FollowingFeed,
};

export default function TickerStrip() {
  const choice = useSyncExternalStore(subscribeMode, readMode, noChoice);
  const session = useSyncExternalStore(subscribeAccount, accountSession, noSession);
  const active: TickerMode = choice ?? (session ? 'watchlist' : 'earners');
  const mode = tickerModes.find(entry => entry.id === active) ?? tickerModes[0];
  const Feed = feeds[mode.id];
  const wide = useSyncExternalStore(subscribeWide, readWide, serverWide);
  const buttons = useRef<Array<HTMLButtonElement | null>>([]);

  const select = useCallback((next: TickerMode) => writeMode(next), []);

  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0;
    if (step === 0) return;
    event.preventDefault();
    const next = (index + step + tickerModes.length) % tickerModes.length;
    select(tickerModes[next].id);
    buttons.current[next]?.focus();
  };


  return (
    // Hidden below md, as on the reference: phones start content right under the top bar.
    <section aria-label="Market ticker" className="hidden h-ticker-height items-center gap-2 bg-canvas px-3 sm:px-4 md:flex">
      <div role="radiogroup" aria-label="Ticker mode" className="flex shrink-0 items-center gap-0.5 rounded-sm bg-surface-1 p-0.5">
        {tickerModes.map((entry, index) => {
          const Icon = entry.icon;
          const selected = entry.id === mode.id;
          return (
            <Tooltip key={entry.id} content={entry.label}>
              <button
                ref={element => { buttons.current[index] = element; }}
                type="button"
                role="radio"
                aria-checked={selected}
                aria-label={entry.label}
                tabIndex={selected ? 0 : -1}
                onClick={() => select(entry.id)}
                onKeyDown={event => onKeyDown(event, index)}
                className={cn(
                  'flex size-6 items-center justify-center rounded-sm text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text',
                  selected && 'bg-surface-3 text-text',
                )}
              >
                <Icon aria-hidden="true" className="size-icon-sm" />
              </button>
            </Tooltip>
          );
        })}
      </div>
      <div className="relative h-full min-w-0 flex-1 overflow-hidden py-1">
        {wide ? <Feed mode={mode} /> : null}
      </div>
    </section>
  );
}
