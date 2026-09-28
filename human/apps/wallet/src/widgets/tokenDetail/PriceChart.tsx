'use client';

/**
 * Recharts-powered area chart with touch/mouse scrub + period selector.
 *
 * Color flips green/red based on the period change sign. Scrub events
 * propagate up via `onScrubMove` / `onScrubLeave` so the price hero above
 * can mirror the dragged value.
 */

import { AreaChart, Area, ResponsiveContainer, YAxis, Tooltip } from 'recharts';
import { TIME_PERIODS, type TimePeriod } from '@/lib/queries';
import { Loader2 } from 'lucide-react';

export interface ChartPoint {
  time: number;
  price: number;
}

export interface PriceChartProps {
  points: ChartPoint[];
  loading: boolean;
  chartLoading: boolean;
  positive: boolean;
  period: TimePeriod;
  onPeriodChange: (period: TimePeriod) => void;
  onScrubMove?: (point: ChartPoint) => void;
  onScrubLeave?: () => void;
}

export function PriceChart({
  points,
  loading,
  chartLoading,
  positive,
  period,
  onPeriodChange,
  onScrubMove,
  onScrubLeave,
}: PriceChartProps) {
  const chartColor = positive
    ? 'var(--color-status-success)'
    : 'var(--color-status-danger)';

  const handleMove = (state: any) => {
    if (state?.activePayload?.[0]) {
      onScrubMove?.(state.activePayload[0].payload as ChartPoint);
    }
  };

  return (
    <div className="relative mt-2">
      {chartLoading && (
        <div className="absolute inset-0 flex items-center justify-center z-10">
          <Loader2
            aria-label="Loading chart"
            className="h-5 w-5 animate-spin text-pax-accent"
          />
        </div>
      )}
      <div
        className={`h-56 transition-opacity duration-200 touch-none select-none ${
          chartLoading ? 'opacity-30' : 'opacity-100'
        }`}
        onTouchEnd={onScrubLeave}
        onPointerUp={onScrubLeave}
      >
        {points.length > 1 ? (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart
              data={points}
              margin={{ top: 8, right: 0, bottom: 0, left: 0 }}
              onMouseMove={handleMove}
              onMouseLeave={onScrubLeave}
            >
              <defs>
                <linearGradient id="premiumGrad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={chartColor} stopOpacity={0.2} />
                  <stop offset="60%" stopColor={chartColor} stopOpacity={0.05} />
                  <stop offset="100%" stopColor={chartColor} stopOpacity={0} />
                </linearGradient>
              </defs>
              <YAxis domain={['dataMin', 'dataMax']} hide />
              <Tooltip
                content={() => null}
                cursor={{ stroke: 'rgba(255,255,255,0.15)', strokeWidth: 1 }}
              />
              <Area
                type="monotone"
                dataKey="price"
                stroke={chartColor}
                strokeWidth={2}
                fill="url(#premiumGrad)"
                animationDuration={400}
                dot={false}
                activeDot={{ r: 4, fill: chartColor, stroke: 'var(--color-surface-base)', strokeWidth: 2 }}
              />
            </AreaChart>
          </ResponsiveContainer>
        ) : (
          !loading && (
            <div className="h-full flex items-center justify-center">
              <p className="text-xs text-pax-muted">No chart data available</p>
            </div>
          )
        )}
      </div>

      <div className="flex items-center justify-center gap-1 px-5 mt-3">
        {TIME_PERIODS.map((tp) => (
          <button
            key={tp.id}
            onClick={() => onPeriodChange(tp.id)}
            className={`px-3.5 py-1.5 rounded-lg text-xs font-semibold transition-all press-scale ${
              period === tp.id ? 'bg-white/10 text-white' : 'text-pax-muted hover:text-white/60'
            }`}
          >
            {tp.label}
          </button>
        ))}
      </div>
    </div>
  );
}
