'use client';

import { useId, useMemo, useState, type PointerEvent } from 'react';
import { cn } from '@/lib/utils';
import { formatUsdl, rawToDecimalString } from '@/utils/format';
import { useBalanceVisibility } from './balance-visibility';
import type { DeltaTone, SeriesPoint } from './portfolio-transport';

export interface PortfolioChartProps {
  points: readonly SeriesPoint[];
  tone: DeltaTone;
  /** Chart height in CSS pixels; the SVG scales to the container width. */
  height?: number;
  className?: string;
}

const WIDTH = 600;
const PAD_X = 6;
const PAD_Y = 10;

export interface ChartGeometry { line: string; area: string; coords: { x: number; y: number }[] }

/**
 * Line and area paths in a fixed viewBox. A single sample becomes a horizontal
 * line across the full width (not an empty chart); equal values sit mid-height.
 */
export function chartGeometry(points: readonly SeriesPoint[], height: number): ChartGeometry {
  if (points.length === 0) return { line: '', area: '', coords: [] };
  const values = points.map(point => Number(rawToDecimalString(point.valueRaw, 6)));
  const min = Math.min(...values), max = Math.max(...values);
  const span = max - min;
  const innerW = WIDTH - PAD_X * 2, innerH = height - PAD_Y * 2;
  const y = (value: number) => (span === 0 ? PAD_Y + innerH / 2 : PAD_Y + innerH - ((value - min) / span) * innerH);
  const coords = values.length === 1
    ? [{ x: PAD_X, y: y(values[0]) }, { x: WIDTH - PAD_X, y: y(values[0]) }]
    : values.map((value, index) => ({ x: PAD_X + (index / (values.length - 1)) * innerW, y: y(value) }));
  const line = coords.map((c, index) => `${index === 0 ? 'M' : 'L'}${c.x.toFixed(2)} ${c.y.toFixed(2)}`).join(' ');
  const base = height - PAD_Y;
  const area = `${line} L${coords[coords.length - 1].x.toFixed(2)} ${base} L${coords[0].x.toFixed(2)} ${base} Z`;
  return { line, area, coords };
}

const strokeClass: Record<DeltaTone, string> = { gain: 'stroke-candle-up', loss: 'stroke-candle-down', flat: 'stroke-text-muted' };
const dotClass: Record<DeltaTone, string> = { gain: 'fill-candle-up', loss: 'fill-candle-down', flat: 'fill-text-muted' };
const fillClass: Record<DeltaTone, string> = {
  gain: 'fill-[var(--sl-tint-accent-fill-chart)]',
  loss: 'fill-[var(--sl-tint-danger-fill-chart)]',
  flat: 'fill-surface-2',
};

function bucketLabel(iso: string): string {
  return new Date(iso).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit', timeZone: 'UTC', hour12: false }) + ' UTC';
}

/** Inline SVG area chart of portfolio value: flat alpha fill under a sign-coloured line, hover readout, no axes. */
export function PortfolioChart({ points, tone, height = 200, className }: PortfolioChartProps) {
  const { hidden } = useBalanceVisibility();
  const geometry = useMemo(() => chartGeometry(points, height), [points, height]);
  const [hover, setHover] = useState<number | null>(null);
  const readoutId = useId();

  const summary = points.length === 0
    ? 'No history yet.'
    : points.length === 1
      ? `${formatUsdl(points[0].valueRaw)} at ${bucketLabel(points[0].time)}.`
      : `From ${formatUsdl(points[0].valueRaw)} to ${formatUsdl(points[points.length - 1].valueRaw)}; ${tone === 'gain' ? 'gaining' : tone === 'loss' ? 'losing' : 'flat'} over this period.`;

  if (hidden) {
    return (
      <div role="img" aria-label="Portfolio chart hidden while balances are hidden" style={{ height }} className={cn('flex items-center justify-center rounded-md bg-surface-2 text-sm font-semibold text-text-faint', className)}>
        Hidden
      </div>
    );
  }

  function onMove(event: PointerEvent<SVGSVGElement>) {
    if (points.length < 2) return;
    const rect = event.currentTarget.getBoundingClientRect();
    const ratio = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
    setHover(Math.round(ratio * (points.length - 1)));
  }

  const active = hover !== null && points.length > 1 ? hover : null;
  const activeCoord = active !== null ? geometry.coords[active] : null;
  const end = geometry.coords[geometry.coords.length - 1];

  return (
    <div className={cn('relative', className)}>
      <svg
        role="img"
        aria-label={`Portfolio value chart. ${summary}`}
        aria-describedby={active !== null ? readoutId : undefined}
        viewBox={`0 0 ${WIDTH} ${height}`}
        preserveAspectRatio="none"
        style={{ height }}
        className="block w-full touch-none select-none"
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
      >
        {geometry.area ? <path d={geometry.area} className={fillClass[tone]} /> : null}
        {geometry.line ? <path d={geometry.line} fill="none" strokeWidth={2} vectorEffect="non-scaling-stroke" strokeLinejoin="round" strokeLinecap="round" className={strokeClass[tone]} /> : null}
        {end ? <circle cx={end.x} cy={end.y} r={4} className={dotClass[tone]} /> : null}
        {activeCoord ? (
          <>
            <line x1={activeCoord.x} x2={activeCoord.x} y1={PAD_Y} y2={height - PAD_Y} strokeWidth={1} vectorEffect="non-scaling-stroke" className="stroke-chart-crosshair" />
            <circle cx={activeCoord.x} cy={activeCoord.y} r={5} className={dotClass[tone]} />
          </>
        ) : null}
      </svg>
      {active !== null ? (
        <p id={readoutId} role="status" className="pointer-events-none absolute left-2 top-2 rounded-sm bg-surface-3 px-2 py-1 text-xs font-semibold text-text">
          <span data-numeric className="tabular-nums">{formatUsdl(points[active].valueRaw)}</span>
          <span className="ml-2 text-text-faint">{bucketLabel(points[active].time)}</span>
          {!points[active].complete ? <span className="ml-2 text-warning">some prices missing</span> : null}
        </p>
      ) : null}
    </div>
  );
}
