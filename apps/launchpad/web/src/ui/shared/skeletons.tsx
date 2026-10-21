'use client';

import { cn } from '@/lib/utils';

function Shimmer({ className, style }: { className?: string; style?: React.CSSProperties }) {
  return <div aria-hidden="true" style={style} className={cn('sl-skeleton bg-skeleton rounded-md', className)} />;
}

export function TokenCardSkeleton() {
  return (
    <div className="flex flex-col overflow-hidden rounded-md bg-surface-1">
      <Shimmer className="aspect-square w-full rounded-none" />
      <div className="flex flex-col gap-1.5 p-2.5">
        <Shimmer className="h-3 w-24" />
        <div className="flex items-center justify-between">
          <Shimmer className="h-3 w-16" />
          <Shimmer className="h-3 w-10" />
        </div>
        <Shimmer className="h-2 w-20" />
      </div>
    </div>
  );
}

export function TokenGridSkeleton({ count = 12 }: { count?: number }) {
  return (
    <div className="grid grid-cols-2 gap-3 px-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
      {Array.from({ length: count }).map((_, i) => (
        <TokenCardSkeleton key={i} />
      ))}
    </div>
  );
}

export function ProfileSkeleton() {
  return (
    <div className="mx-auto max-w-4xl space-y-6 px-4 pt-8">
      <div className="flex items-center gap-4">
        <Shimmer className="size-20 rounded-pill" />
        <div className="flex-1 space-y-2">
          <Shimmer className="h-5 w-40" />
          <Shimmer className="h-3 w-28" />
          <Shimmer className="h-3 w-64" />
        </div>
      </div>
      <div className="grid grid-cols-3 gap-3">
        <Shimmer className="h-20" />
        <Shimmer className="h-20" />
        <Shimmer className="h-20" />
      </div>
      <Shimmer className="h-[200px]" />
      <div className="space-y-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Shimmer key={i} className="h-16" />
        ))}
      </div>
    </div>
  );
}

export function TerminalSkeleton() {
  return (
    <div className="flex h-full gap-1 p-1">
      <div className="w-[240px] flex-shrink-0 space-y-1">
        {Array.from({ length: 12 }).map((_, i) => (
          <Shimmer key={i} className="h-10" />
        ))}
      </div>
      <div className="flex-1 space-y-1">
        <Shimmer className="h-10" />
        <Shimmer className="h-[400px]" />
        <Shimmer className="h-[200px]" />
      </div>
      <div className="w-[300px] flex-shrink-0 space-y-1">
        <Shimmer className="h-[200px]" />
        <Shimmer className="h-[120px]" />
        <Shimmer className="h-[200px]" />
      </div>
    </div>
  );
}

export function SearchResultSkeleton({ count = 4 }: { count?: number }) {
  return (
    <div className="flex flex-col gap-0.5">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="flex items-center gap-2.5 bg-surface-2 px-3 py-2.5">
          <Shimmer className="size-8 flex-shrink-0 rounded-pill" />
          <div className="flex-1 space-y-1.5">
            <Shimmer className="h-3 w-24" />
            <Shimmer className="h-2 w-16" />
          </div>
          <Shimmer className="h-3 w-12" />
        </div>
      ))}
    </div>
  );
}

export function ChartSkeleton() {
  return (
    <div className="relative overflow-hidden rounded-md bg-surface-1 p-4">
      <div className="flex h-[180px] items-end gap-1">
        {[45,72,38,85,55,68,42,90,60,35,75,50,82,40,65,48,78,55,88,43,70,58,80,52].map((h, i) => (
          <Shimmer
            key={i}
            className="flex-1 rounded-t-xs rounded-b-none"
            style={{ height: `${h}%` }}
          />
        ))}
      </div>
      <div className="mt-2 flex justify-between">
        <Shimmer className="h-2 w-8" />
        <Shimmer className="h-2 w-8" />
        <Shimmer className="h-2 w-8" />
        <Shimmer className="h-2 w-8" />
      </div>
    </div>
  );
}

export function StatTileSkeleton({ count = 1, size = 'bar' }: { count?: number; size?: 'bar' | 'inline' }) {
  return (
    <>
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} aria-hidden="true" className={cn('flex flex-col gap-2 rounded-md bg-surface-2', size === 'bar' ? 'p-4' : 'min-h-16 min-w-24 p-3')}>
          <span className="sl-skeleton h-2.5 w-16" />
          <span className={cn('sl-skeleton', size === 'bar' ? 'h-8 w-28' : 'h-6 w-20')} />
        </div>
      ))}
    </>
  );
}

export function RankRowSkeleton({ count = 5, size = 'md' }: { count?: number; size?: 'md' | 'lg' }) {
  return (
    <div aria-hidden="true" className="flex flex-col gap-0.5">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className={cn('flex items-center gap-3 px-3', size === 'lg' ? 'h-list-row-lg' : 'h-list-row')}>
          <span className="sl-skeleton h-3 w-5" />
          <span className={cn('sl-skeleton rounded-pill', size === 'lg' ? 'size-avatar-xl' : 'size-avatar-md')} />
          <span className="sl-skeleton h-3 flex-1" />
          <span className="sl-skeleton h-3 w-16" />
        </div>
      ))}
    </div>
  );
}

export function CardRailSkeleton({ count = 4, width = 240, height = 160 }: { count?: number; width?: number; height?: number }) {
  return (
    <div aria-hidden="true" className="flex gap-3 overflow-hidden">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} style={{ width, height }} className="flex shrink-0 flex-col gap-3 rounded-md bg-surface-1 p-4">
          <div className="flex items-center gap-3">
            <span className="sl-skeleton size-avatar-xl rounded-sm" />
            <div className="flex flex-1 flex-col gap-2">
              <span className="sl-skeleton h-3 w-2/3" />
              <span className="sl-skeleton h-3 w-1/3" />
            </div>
          </div>
          <span className="sl-skeleton h-3 w-1/2" />
          <span className="sl-skeleton h-6 w-full" />
        </div>
      ))}
    </div>
  );
}

export function TableRowsSkeleton({ rows = 5, columns = 5, dense = false }: { rows?: number; columns?: number; dense?: boolean }) {
  return (
    <div aria-hidden="true" className="flex flex-col gap-0.5">
      {Array.from({ length: rows }).map((_, r) => (
        <div key={r} className={cn('flex items-center gap-3 bg-surface-1 px-3', dense ? 'h-table-row-dense' : 'h-table-row')}>
          <span className="sl-skeleton size-avatar-sm rounded-pill" />
          <span className="sl-skeleton h-3 flex-1" />
          {Array.from({ length: Math.max(0, columns - 2) }).map((_, c) => (
            <span key={c} className="sl-skeleton h-3 w-16" />
          ))}
        </div>
      ))}
    </div>
  );
}

export { Shimmer };
