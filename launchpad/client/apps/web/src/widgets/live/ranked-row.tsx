'use client';
import Link from 'next/link';
import { ChevronRight } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { SectionHeader } from '@/ui/primitives';
import type { LiveDirectoryEntry } from './use-live-directory';
import { StreamCard, StreamCardPlaceholder, StreamCardSkeleton } from './stream-card';

export type LiveSort = 'viewers' | 'mcap';

export const liveSortLabels: Record<LiveSort, string> = { viewers: 'Most viewers', mcap: 'Highest market cap' };

/** `?sort=` value to a known ranking; anything else falls back to viewers. */
export function parseLiveSort(raw: string | null | undefined): LiveSort {
  return raw === 'mcap' ? 'mcap' : 'viewers';
}

export function showAllHref(sort: LiveSort): string {
  return `/live/all?sort=${sort}`;
}

export const rankedRowSize = 4;

/** Grid columns for stream cards: two per row on phones, three from md, four from xl (the reference density). */
export const streamGridClass = 'grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-4';

/** A ranked row shows exactly one grid row: card 3 from md, card 4 from xl. */
export function rankedCardVisibility(index: number): string | undefined {
  if (index === 2) return 'hidden md:block';
  if (index === 3) return 'hidden xl:block';
  return undefined;
}

export interface RankedRowProps {
  sort: LiveSort;
  entries: readonly LiveDirectoryEntry[];
  creatorLabel(wallet: string): string;
  loading?: boolean;
  /** Keep the row's frame with static card placeholders (directory unavailable or nobody live). */
  placeholder?: boolean;
  /** Compact state pill centred over the placeholder cards (unavailable or nobody live); only shown with `placeholder`. */
  notice?: ReactNode;
  className?: string;
}

export function RankedRow({ sort, entries, creatorLabel, loading = false, placeholder = false, notice, className }: RankedRowProps) {
  const visible = entries.slice(0, rankedRowSize);
  const frames = loading || placeholder;
  return (
    <section data-live-row={sort} aria-label={liveSortLabels[sort]} className={cn('flex flex-col gap-4', className)}>
      <SectionHeader
        title={<span className="text-lg">{liveSortLabels[sort]}</span>}
        level={2}
        loading={loading}
        action={
          <Link href={showAllHref(sort)} className="inline-flex h-8 items-center gap-1 rounded-sm px-2 text-sm font-bold text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text">
            Show all
            <span className="sr-only"> {liveSortLabels[sort].toLowerCase()} streams</span>
            <ChevronRight aria-hidden="true" className="size-4" />
          </Link>
        }
      />
      <div className="relative">
        <ol className={streamGridClass} aria-label={placeholder ? `${liveSortLabels[sort]}: no streams to show` : undefined}>
          {frames
            ? Array.from({ length: rankedRowSize }, (_, index) => (
              <li key={index} aria-hidden="true" className={cn('min-w-0', rankedCardVisibility(index))}>{loading ? <StreamCardSkeleton /> : <StreamCardPlaceholder />}</li>
            ))
            : visible.map((entry, index) => (
              <li key={entry.stream.id} className={cn('min-w-0', rankedCardVisibility(index))}>
                <StreamCard entry={entry} creatorLabel={creatorLabel(entry.creatorWallet)} />
              </li>
            ))}
        </ol>
        {placeholder && notice ? (
          <div data-live-row-notice className="pointer-events-none absolute inset-0 flex items-center justify-center p-4">
            <div className="pointer-events-auto flex max-w-full justify-center">{notice}</div>
          </div>
        ) : null}
      </div>
    </section>
  );
}
