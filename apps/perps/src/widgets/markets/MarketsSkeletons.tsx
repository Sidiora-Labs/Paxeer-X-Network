import React from "react";
import { cn } from "@/ui/utils/cn";

/**
 * Low-noise "bone" primitive — dark pulsing rectangle. Kept inline so
 * this file has no external deps beyond `cn`.
 */
function Bone({
  className,
  style,
}: {
  className?: string;
  style?: React.CSSProperties;
}) {
  return (
    <div
      className={cn(
        "rounded-[var(--radius-sm)] bg-white/[0.045] animate-pulse",
        className,
      )}
      style={style}
    />
  );
}

/**
 * Vertical stats column placeholder — matches the three-stat stacked
 * layout rendered by `MarketsOverviewStats` so the final layout doesn't
 * shift when the WS feed replaces it.
 */
export function OverviewStatsSkeleton() {
  return (
    <div className="flex flex-col gap-2">
      {[0, 1, 2].map((i) => (
        <div
          key={i}
          className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] px-4 py-3"
        >
          <Bone className="h-[10px] w-20 mb-2" />
          <Bone className="h-5 w-28" />
        </div>
      ))}
    </div>
  );
}

/**
 * Highlight panel placeholder — 5 rows of symbol / price / change
 * matching `MarketsHighlights` output.
 */
export function HighlightsPanelSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-0.5">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className="flex items-center justify-between px-3 py-2 rounded-[var(--radius-md)]"
        >
          <div className="flex items-center gap-2 min-w-0">
            <Bone className="h-7 w-7 rounded-full" />
            <div className="flex flex-col gap-1">
              <Bone className="h-3 w-14" />
              <Bone className="h-[10px] w-10" />
            </div>
          </div>
          <div className="flex flex-col items-end gap-1">
            <Bone className="h-3 w-16" />
            <Bone className="h-[10px] w-10" />
          </div>
        </div>
      ))}
    </div>
  );
}

/**
 * Markets table placeholder — keeps the same column layout as the live
 * table so scroll position and horizontal sizes stay stable.
 */
export function MarketsTableSkeleton({
  rows = 8,
  hiddenColsMd = false,
  hiddenColsLg = true,
}: {
  rows?: number;
  hiddenColsMd?: boolean;
  hiddenColsLg?: boolean;
}) {
  return (
    <div className="rounded-[var(--radius-lg)] bg-surface-1 border border-white/[0.04] overflow-hidden">
      <div className="hidden md:grid grid-cols-[minmax(180px,1.5fr)_1fr_1fr_1fr_1fr_1fr_80px] gap-3 px-4 py-3 border-b border-white/[0.04]">
        {Array.from({ length: 7 }).map((_, i) => (
          <Bone key={i} className="h-3" />
        ))}
      </div>
      <div className="divide-y divide-white/[0.04]">
        {Array.from({ length: rows }).map((_, i) => (
          <div
            key={i}
            className="flex md:grid md:grid-cols-[minmax(180px,1.5fr)_1fr_1fr_1fr_1fr_1fr_80px] items-center gap-3 px-4 py-3 justify-between"
          >
            <div className="flex items-center gap-3 min-w-0">
              <Bone className="h-8 w-8 rounded-full" />
              <div className="flex flex-col gap-1">
                <Bone className="h-3 w-16" />
                <Bone className="h-[10px] w-20" />
              </div>
            </div>
            <Bone className="hidden md:block h-3 w-16 justify-self-end" />
            <Bone className={cn("hidden md:block h-3 w-12 justify-self-end", hiddenColsMd && "md:hidden")} />
            <Bone className={cn("hidden md:block h-3 w-20 justify-self-end", hiddenColsLg && "lg:block")} />
            <Bone className={cn("hidden lg:block h-3 w-20 justify-self-end")} />
            <Bone className={cn("hidden lg:block h-3 w-16 justify-self-end")} />
            <div className="flex md:justify-end items-center gap-2">
              <Bone className="h-3 w-12 md:hidden" />
              <Bone className="h-7 w-14 rounded-[var(--radius-pill)] hidden md:block" />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
