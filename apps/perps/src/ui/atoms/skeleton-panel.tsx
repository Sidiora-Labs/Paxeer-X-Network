import React from "react";
import { cn } from "@/ui/utils/cn";

// Fixed-seed bar heights so the chart skeleton doesn't jitter between renders.
const CHART_SKELETON_HEIGHTS = [
  45, 62, 38, 55, 70, 48, 33, 58, 65, 42, 77, 50,
  35, 68, 52, 40, 72, 46, 60, 38, 55, 44, 63, 48,
];

function Bone({ className, style }: { className?: string; style?: React.CSSProperties }) {
  return (
    <div className={cn("rounded bg-muted/50 animate-pulse", className)} style={style} />
  );
}

export function ChartSkeleton() {
  return (
    <div className="h-full w-full flex flex-col p-3 gap-2">
      <div className="flex items-center gap-2">
        <Bone className="h-3 w-16" />
        <Bone className="h-3 w-10" />
        <Bone className="h-3 w-10" />
      </div>
      <div className="flex-1 flex items-end gap-1 px-2 pb-2">
        {CHART_SKELETON_HEIGHTS.map((h, i) => (
          <Bone
            key={i}
            className="flex-1"
            style={{ height: `${h}%` }}
          />
        ))}
      </div>
      <Bone className="h-2 w-full" />
    </div>
  );
}

export function OrderbookSkeleton() {
  return (
    <div className="h-full w-full flex flex-col p-2 gap-1">
      <div className="flex items-center justify-between mb-1">
        <Bone className="h-2.5 w-14" />
        <Bone className="h-2.5 w-14" />
      </div>
      {Array.from({ length: 12 }).map((_, i) => (
        <div key={i} className="flex items-center gap-2">
          <Bone className="h-2 flex-1" />
          <Bone className="h-2 w-16" />
          <Bone className="h-2 w-12" />
        </div>
      ))}
      <Bone className="h-5 w-full my-1" />
      {Array.from({ length: 12 }).map((_, i) => (
        <div key={`b-${i}`} className="flex items-center gap-2">
          <Bone className="h-2 flex-1" />
          <Bone className="h-2 w-16" />
          <Bone className="h-2 w-12" />
        </div>
      ))}
    </div>
  );
}

export function PositionsSkeleton() {
  return (
    <div className="h-full w-full p-2">
      <div className="flex items-center gap-3 mb-3">
        <Bone className="h-3 w-20" />
        <Bone className="h-3 w-16" />
        <Bone className="h-3 w-16" />
      </div>
      <div className="flex items-center gap-3 mb-2 px-1">
        {Array.from({ length: 7 }).map((_, i) => (
          <Bone key={i} className="h-2 w-14" />
        ))}
      </div>
      {Array.from({ length: 3 }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 py-2 px-1 border-b border-border/30">
          {Array.from({ length: 7 }).map((_, j) => (
            <Bone key={j} className="h-3 w-14" />
          ))}
        </div>
      ))}
    </div>
  );
}

export function OrderFormSkeleton() {
  return (
    <div className="p-3 space-y-3">
      <div className="flex gap-1">
        <Bone className="h-8 flex-1" />
        <Bone className="h-8 flex-1" />
      </div>
      <Bone className="h-4 w-20" />
      <Bone className="h-9 w-full" />
      <Bone className="h-4 w-16" />
      <Bone className="h-9 w-full" />
      <Bone className="h-2 w-full" />
      <div className="flex justify-between">
        <Bone className="h-3 w-20" />
        <Bone className="h-3 w-16" />
      </div>
      <div className="flex justify-between">
        <Bone className="h-3 w-20" />
        <Bone className="h-3 w-16" />
      </div>
      <Bone className="h-10 w-full" />
    </div>
  );
}
