'use client';
import Link from 'next/link';
import { Video } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Avatar, Chip, Skeleton } from '@/ui/primitives';
import { poolPath } from '@/core/app-foundation/navigation';
import { shortAddress } from '@/utils/format';
import type { LiveDirectoryEntry } from './use-live-directory';
import { LiveBadge } from './live-badge';
import { ViewerCountChip } from './viewer-count-chip';
import { MarketCapChip } from './market-cap-chip';
import { StreamSparkline } from './stream-sparkline';
import { formatViewers } from './format';

export type StreamTile = { kind: 'thumbnail'; src: string } | { kind: 'coin'; src: string } | { kind: 'none' };

/** The provider thumbnail when the service publishes one; otherwise the coin image; otherwise our own glyph block. */
export function streamTile(entry: Pick<LiveDirectoryEntry, 'thumbnailUrl' | 'imageUrl'>): StreamTile {
  if (entry.thumbnailUrl) return { kind: 'thumbnail', src: entry.thumbnailUrl };
  if (entry.imageUrl) return { kind: 'coin', src: entry.imageUrl };
  return { kind: 'none' };
}

/** Coin name for display: metadata name, then symbol, then the short pool address. */
export function coinLabel(entry: Pick<LiveDirectoryEntry, 'name' | 'symbol' | 'poolAddress'>): string {
  return entry.name?.trim() || entry.symbol?.trim() || shortAddress(entry.poolAddress);
}

/** Accessible link name: "<title>, <coin>, live, 35 viewers". */
export function streamCardLabel(entry: LiveDirectoryEntry): string {
  return `${entry.stream.title}, ${coinLabel(entry)}, live, ${formatViewers(entry.viewerCount, entry.viewerQuality)}`;
}

export function streamHref(entry: Pick<LiveDirectoryEntry, 'poolAddress'>): string {
  return poolPath(125, entry.poolAddress);
}

export function StreamTileMedia({ entry, className }: { entry: Pick<LiveDirectoryEntry, 'thumbnailUrl' | 'imageUrl'>; className?: string }) {
  const tile = streamTile(entry);
  return (
    <div className={cn('relative aspect-video w-full overflow-hidden rounded-md bg-surface-2', className)}>
      {tile.kind === 'thumbnail' ? (
        <img src={tile.src} alt="" loading="lazy" className="size-full object-cover" />
      ) : tile.kind === 'coin' ? (
        <div className="flex size-full items-center justify-center">
          <img src={tile.src} alt="" loading="lazy" className="aspect-square h-3/5 rounded-lg object-cover" />
        </div>
      ) : (
        <div className="flex size-full items-center justify-center text-text-faint">
          <Video aria-hidden="true" className="size-10" />
        </div>
      )}
    </div>
  );
}

export interface StreamCardProps {
  entry: LiveDirectoryEntry;
  creatorLabel: string;
  className?: string;
}

export function StreamCard({ entry, creatorLabel, className }: StreamCardProps) {
  const name = coinLabel(entry);
  return (
    <Link
      href={streamHref(entry)}
      aria-label={streamCardLabel(entry)}
      data-stream-id={entry.stream.id}
      className={cn('group flex min-w-0 flex-col overflow-hidden rounded-lg bg-surface-1 transition-colors duration-fast hover:bg-surface-hover', className)}
    >
      <div className="relative">
        <StreamTileMedia entry={entry} className="rounded-b-none" />
        <LiveBadge className="absolute left-2 top-2" />
        <MarketCapChip raw={entry.mcapRaw} variant="media" className="absolute right-2 top-2" />
        <ViewerCountChip count={entry.viewerCount} quality={entry.viewerQuality} variant="media" className="absolute bottom-2 left-2" />
        <span aria-hidden="true" className="absolute bottom-2 right-2 rounded-sm bg-media-overlay px-1 py-0.5">
          <StreamSparkline poolAddress={entry.poolAddress} variant="overlay" />
        </span>
      </div>
      <div className="flex min-h-14 items-start gap-2 p-2 md:gap-3 md:p-3">
        <Avatar src={entry.imageUrl} name={entry.symbol ?? name} size="lg" decorative className="hidden sm:inline-flex" />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-bold text-text md:text-md">{name}</span>
            {entry.symbol ? <Chip variant="token" className="shrink-0">{entry.symbol}</Chip> : null}
          </div>
          <p className="truncate text-xs text-text-faint">{creatorLabel}</p>
          <p className="truncate text-sm text-text-muted">{entry.stream.title}</p>
        </div>
      </div>
    </Link>
  );
}

export function StreamCardSkeleton({ className }: { className?: string }) {
  return (
    <div aria-hidden="true" className={cn('flex flex-col overflow-hidden rounded-lg bg-surface-1', className)}>
      <Skeleton variant="block" className="aspect-video h-auto w-full rounded-none" />
      <div className="flex items-start gap-3 p-3">
        <Skeleton variant="circle" width={40} height={40} />
        <div className="flex-1 space-y-2">
          <Skeleton variant="text" width="60%" />
          <Skeleton variant="text" width="40%" />
        </div>
      </div>
    </div>
  );
}

/**
 * A card-shaped frame with no stream in it, for rows whose directory is
 * unavailable or empty. Static surfaces only: it never shimmers (nothing is
 * loading) and never carries stream text or numbers.
 */
export function StreamCardPlaceholder({ className }: { className?: string }) {
  return (
    <div aria-hidden="true" data-placeholder="" className={cn('flex flex-col overflow-hidden rounded-lg bg-surface-1', className)}>
      <span className="block aspect-video w-full bg-surface-2" />
      <span className="flex items-start gap-2 p-2 md:gap-3 md:p-3">
        <span className="hidden size-10 shrink-0 rounded-pill bg-surface-2 sm:block" />
        <span className="flex flex-1 flex-col gap-2 pt-1">
          <span className="h-3 w-3/5 rounded-xs bg-surface-2" />
          <span className="h-3 w-2/5 rounded-xs bg-surface-2" />
        </span>
      </span>
    </div>
  );
}
