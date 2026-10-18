'use client';
import { StatusBadge } from '@/ui/primitives';

export interface LiveBadgeProps {
  /** `pill`: filled LIVE pill for media overlays and the hero. `dot`: accent dot plus "Live" text for inline rows. */
  variant?: 'pill' | 'dot';
  size?: 'sm' | 'md';
  className?: string;
}

export function LiveBadge({ variant = 'pill', size = 'sm', className }: LiveBadgeProps) {
  return (
    <StatusBadge status="live" dot={variant === 'dot'} size={size} className={className}>
      {variant === 'dot' ? 'Live' : 'LIVE'}
    </StatusBadge>
  );
}
