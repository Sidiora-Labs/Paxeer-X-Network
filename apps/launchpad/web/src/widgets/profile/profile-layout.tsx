'use client';

import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';

export interface ProfileLayoutProps {
  /** Leaderboard rail; shown from 2xl only. */
  left?: ReactNode;
  center: ReactNode;
  /** Top trades / account rail; beside the centre from lg, stacked under it below lg. */
  right?: ReactNode;
  /** Accessible name of the right rail. */
  rightLabel?: string;
  className?: string;
}

/**
 * S16 grid, flush like the reference: `| left 360 | centre (max 960) | right 360 |`
 * with every column starting at the top of the content area and no gutters.
 * Columns are told apart by surface contrast only: the rails sit on surface-1,
 * the centre on the canvas. The left rail appears from 2xl (the reference
 * drops it at 1280 and 1440), the right rail sits beside the centre from lg
 * (320, 360 from xl) and stacks under it below lg. Breakpoint classes use
 * ranges (`xl:max-2xl:`) because the 2xl breakpoint (1440px) is emitted ahead of
 * the rem-based xl rules, so a bare `xl:` would win at 2xl. A missing slot collapses
 * its column so the centre never sits beside an empty gutter.
 */
export default function ProfileLayout({ left, center, right, rightLabel = 'Top trades', className }: ProfileLayoutProps) {
  const hasLeft = left !== undefined && left !== null;
  const hasRight = right !== undefined && right !== null;
  const columns = hasLeft && hasRight
    ? 'lg:max-xl:grid-cols-[minmax(0,1fr)_320px] xl:max-2xl:grid-cols-[minmax(0,1fr)_360px] 2xl:grid-cols-[minmax(300px,360px)_minmax(0,960px)_minmax(300px,360px)]'
    : hasRight
      ? 'lg:max-xl:grid-cols-[minmax(0,1fr)_320px] xl:max-2xl:grid-cols-[minmax(0,1fr)_360px] 2xl:grid-cols-[minmax(0,960px)_minmax(300px,360px)]'
      : hasLeft
        ? '2xl:grid-cols-[minmax(300px,360px)_minmax(0,960px)]'
        : 'lg:max-2xl:grid-cols-[minmax(0,960px)] 2xl:grid-cols-[minmax(0,960px)]';
  return (
    <div data-profile-layout className={cn('grid w-full grid-cols-1 items-stretch lg:min-h-[calc(100dvh-var(--sl-shell-top-total))] 2xl:justify-center', columns, className)}>
      {hasLeft ? (
        <aside aria-label="Leaderboard" className="hidden min-w-0 bg-surface-1 2xl:block">
          <div className="sticky top-0">{left}</div>
        </aside>
      ) : null}
      <div data-profile-col="center" className="flex w-full min-w-0 flex-col">{center}</div>
      {hasRight ? <aside aria-label={rightLabel} className="flex min-w-0 flex-col gap-2 bg-surface-1 max-lg:mt-2">{right}</aside> : null}
    </div>
  );
}
