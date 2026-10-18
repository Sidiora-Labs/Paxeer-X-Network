'use client';
import type { ReactNode } from 'react';
import { ResizeHandle } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { CHART_DEFAULT_HEIGHT, CHART_MAX_HEIGHT, CHART_MIN_HEIGHT, clampChartHeight } from './use-chart-settings';

export interface ChartSplitProps {
  height: number;
  onHeightChange: (height: number) => void;
  min?: number;
  max?: number;
  defaultHeight?: number;
  label?: string;
  children: ReactNode;
  className?: string;
}

/** Chart body with the drag handle that resizes the split between chart and the tabs below it. */
export function ChartSplit({ height, onHeightChange, min = CHART_MIN_HEIGHT, max = CHART_MAX_HEIGHT, defaultHeight = CHART_DEFAULT_HEIGHT, label = 'Resize chart', children, className }: ChartSplitProps) {
  const current = clampChartHeight(height);
  return (
    <div className={cn('flex w-full flex-col', className)}>
      <div style={{ height: current }} className="relative w-full overflow-hidden bg-canvas">{children}</div>
      <ResizeHandle value={current} min={min} max={max} defaultValue={defaultHeight} onChange={onHeightChange} label={label} className="bg-surface-1" />
    </div>
  );
}
