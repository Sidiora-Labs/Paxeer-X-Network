'use client';
import { Eye } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { StreamViewerQuality } from '@/core/clients/livestream-api';
import { formatViewers } from './format';

export interface ViewerCountChipProps {
  count: number | null | undefined;
  quality?: StreamViewerQuality;
  /** `chip`: surface-3 pill (hero meta). `media`: media-overlay pill for use on a thumbnail. */
  variant?: 'chip' | 'media';
  size?: 'sm' | 'md';
  icon?: boolean;
  className?: string;
}

export function ViewerCountChip({ count, quality, variant = 'chip', size = 'sm', icon = true, className }: ViewerCountChipProps) {
  const label = formatViewers(count, quality);
  const stale = quality === 'stale' && count !== null && count !== undefined;
  return (
    <span
      data-quality={quality}
      title={stale ? 'Estimate updating' : undefined}
      className={cn(
        'inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded-pill px-2 tabular-nums',
        size === 'md' ? 'h-6 text-sm' : 'h-5 text-xs',
        variant === 'media' ? 'bg-media-overlay text-text' : 'bg-surface-3 text-text-muted',
        className,
      )}
    >
      {icon ? <Eye aria-hidden="true" className={size === 'md' ? 'size-3.5' : 'size-3'} /> : null}
      <span>{label}</span>
      {stale ? <span className="sr-only">, estimate updating</span> : null}
    </span>
  );
}
