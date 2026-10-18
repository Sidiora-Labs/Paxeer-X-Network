'use client';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { cn } from '@/lib/utils';
import { IconButton, Skeleton } from '@/ui/primitives';
import { StreamPlayer } from './stream-player';
import type { LiveDirectoryEntry } from './use-live-directory';
import { HeroMetaPanel } from './hero-meta-panel';
import { StreamTileMedia, coinLabel, streamHref } from './stream-card';

export const heroAutoAdvanceMs = 15_000;
const swipeThresholdPx = 40;

/** Stage around the hero: side gutters hold the arrows and the next-slide peek from md up. */
const heroStageClass = 'relative md:px-16 lg:px-24';
/** One surface for player and meta: stacked below lg, side by side (about 65/35) from lg. */
const heroFrameClass = 'grid w-full grid-cols-1 overflow-hidden rounded-lg bg-surface-1 lg:grid-cols-[minmax(0,1.9fr)_minmax(0,1fr)]';
/** Still tile of the next slide, in the right gutter beside the frame. */
const heroPeekClass = 'absolute inset-y-4 left-[calc(100%+16px)] hidden w-12 overflow-hidden rounded-lg bg-surface-1 md:block';

/** Wrap-around step through `length` slides. */
export function stepIndex(current: number, delta: number, length: number): number {
  if (length <= 0) return 0;
  return (((current + delta) % length) + length) % length;
}

/** Index of the slide the viewer was on, kept across directory refreshes by stream id; first slide when it left the set. */
export function activeSlideIndex(entries: readonly Pick<LiveDirectoryEntry, 'stream'>[], activeId: string | null): number {
  if (activeId === null) return 0;
  const index = entries.findIndex(entry => entry.stream.id === activeId);
  return index < 0 ? 0 : index;
}

/** Auto-advance only runs before any interaction, with more than one slide, without reduced motion and while the pointer is away. */
export function shouldAutoAdvance(input: { slides: number; interacted: boolean; reducedMotion: boolean; hovered: boolean }): boolean {
  return input.slides > 1 && !input.interacted && !input.reducedMotion && !input.hovered;
}

function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(true);
  useEffect(() => {
    const query = window.matchMedia('(prefers-reduced-motion: reduce)');
    setReduced(query.matches);
    const listener = (event: MediaQueryListEvent) => setReduced(event.matches);
    query.addEventListener('change', listener);
    return () => query.removeEventListener('change', listener);
  }, []);
  return reduced;
}

export interface HeroCarouselProps {
  entries: readonly LiveDirectoryEntry[];
  creatorLabel(wallet: string): string;
  className?: string;
}

/**
 * Featured streams. Exactly one StreamPlayer is mounted, for the active slide;
 * the neighbour peek is a still tile. Auto-advance stops for good after the
 * first pointer or keyboard interaction (which includes unmuting).
 */
export function HeroCarousel({ entries, creatorLabel, className }: HeroCarouselProps) {
  const [activeId, setActiveId] = useState<string | null>(null);
  const [interacted, setInteracted] = useState(false);
  const [hovered, setHovered] = useState(false);
  const reducedMotion = usePrefersReducedMotion();
  const swipeStart = useRef<number | null>(null);

  const count = entries.length;
  const index = activeSlideIndex(entries, activeId);
  const active = entries[index];
  const next = count > 1 ? entries[stepIndex(index, 1, count)] : undefined;
  const autoAdvance = shouldAutoAdvance({ slides: count, interacted, reducedMotion, hovered });

  const goTo = (target: number) => {
    const entry = entries[stepIndex(target, 0, count)];
    if (entry) setActiveId(entry.stream.id);
  };
  const step = (delta: number) => goTo(index + delta);

  useEffect(() => {
    if (!autoAdvance || !active) return;
    const timer = setTimeout(() => {
      if (document.visibilityState !== 'visible') return;
      const following = entries[stepIndex(index, 1, entries.length)];
      if (following) setActiveId(following.stream.id);
    }, heroAutoAdvanceMs);
    return () => clearTimeout(timer);
  }, [autoAdvance, active, entries, index]);

  if (!active) return null;

  return (
    <section
      data-live-hero
      aria-roledescription="carousel"
      aria-label="Featured live streams"
      className={cn('flex flex-col gap-4', className)}
      onPointerDownCapture={() => setInteracted(true)}
      onKeyDownCapture={() => setInteracted(true)}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <div className={heroStageClass}>
        <div className="relative mx-auto w-full max-w-[1000px]">
          <div
            data-live-hero-frame
            role="group"
            aria-roledescription="slide"
            aria-label={`${index + 1} of ${count}: ${active.stream.title}`}
            className={heroFrameClass}
            onPointerDown={event => { swipeStart.current = event.pointerType === 'touch' ? event.clientX : null; }}
            onPointerUp={event => {
              const start = swipeStart.current;
              swipeStart.current = null;
              if (start === null) return;
              const dx = event.clientX - start;
              if (Math.abs(dx) >= swipeThresholdPx) step(dx < 0 ? 1 : -1);
            }}
          >
            <div className="min-w-0">
              <StreamPlayer key={active.stream.id} stream={active.stream} poster={active.thumbnailUrl} muted autoplay className="rounded-none">
                <Link href={streamHref(active)} className="text-sm font-bold text-accent hover:underline">Go to {coinLabel(active)}</Link>
              </StreamPlayer>
            </div>
            <HeroMetaPanel entry={active} creatorLabel={creatorLabel(active.creatorWallet)} />
          </div>

          {next ? (
            <button
              type="button"
              onClick={() => step(1)}
              className={cn(heroPeekClass, 'text-left')}
            >
              <span className="sr-only">Next stream: {next.stream.title}</span>
              <StreamTileMedia entry={next} className="aspect-auto h-full rounded-none" />
            </button>
          ) : null}
        </div>

        {count > 1 ? (
          <>
            <IconButton
              label="Previous stream"
              icon={<ChevronLeft />}
              variant="round"
              size="md"
              onClick={() => step(-1)}
              className="absolute left-0 top-1/2 hidden -translate-y-1/2 md:inline-flex"
            />
            <IconButton
              label="Next stream"
              icon={<ChevronRight />}
              variant="round"
              size="md"
              onClick={() => step(1)}
              className="absolute right-0 top-1/2 hidden -translate-y-1/2 md:inline-flex"
            />
          </>
        ) : null}
      </div>

      {count > 1 ? (
        <div className="flex items-center justify-center gap-1" aria-label="Choose a featured stream" role="group">
          {entries.map((entry, slide) => (
            <button
              key={entry.stream.id}
              type="button"
              aria-label={`Show stream ${slide + 1} of ${count}: ${entry.stream.title}`}
              aria-current={slide === index ? 'true' : undefined}
              onClick={() => goTo(slide)}
              className="flex h-6 items-center px-1"
            >
              <span aria-hidden="true" className={cn('block h-1.5 rounded-pill transition-all duration-base', slide === index ? 'w-6 bg-text' : 'w-1.5 bg-surface-3')} />
            </button>
          ))}
        </div>
      ) : null}
    </section>
  );
}

export function HeroCarouselSkeleton() {
  return (
    <div aria-hidden="true" className={heroStageClass}>
      <div className={cn(heroFrameClass, 'mx-auto max-w-[1000px]')}>
        <Skeleton variant="block" className="aspect-video h-auto w-full rounded-none" />
        <div className="flex flex-col gap-4 p-4 lg:p-5">
          <Skeleton variant="pill" />
          <Skeleton variant="text" width="80%" />
          <Skeleton variant="block" height={48} />
          <Skeleton variant="block" height={72} />
        </div>
      </div>
    </div>
  );
}

export interface HeroFramePlaceholderProps {
  /** The message shown inside the player area (unavailable or nobody live). */
  children: ReactNode;
  className?: string;
}

/**
 * The hero frame with no stream in it: the player area carries the page's
 * unavailable or empty message, and the meta column keeps its shape with
 * static (non-shimmering) blocks. Nothing here stands in for a stream.
 */
export function HeroFramePlaceholder({ children, className }: HeroFramePlaceholderProps) {
  return (
    <section data-live-hero aria-label="Featured live streams" className={cn(heroStageClass, className)}>
      <div data-live-hero-frame className={cn(heroFrameClass, 'mx-auto max-w-[1000px]')}>
        <div className="flex aspect-video min-h-56 min-w-0 items-center justify-center bg-surface-2 p-4">
          {children}
        </div>
        <div aria-hidden="true" className="hidden flex-col gap-4 p-5 lg:flex">
          <span className="flex gap-2">
            <span className="h-6 w-12 rounded-sm bg-surface-2" />
            <span className="h-6 w-20 rounded-sm bg-surface-2" />
          </span>
          <span className="h-6 w-4/5 rounded-xs bg-surface-2" />
          <span className="flex items-center gap-3">
            <span className="size-10 shrink-0 rounded-pill bg-surface-2" />
            <span className="flex flex-1 flex-col gap-2">
              <span className="h-3 w-3/5 rounded-xs bg-surface-2" />
              <span className="h-3 w-2/5 rounded-xs bg-surface-2" />
            </span>
          </span>
          <span className="mt-auto h-16 w-full rounded-md bg-surface-2" />
        </div>
      </div>
    </section>
  );
}
