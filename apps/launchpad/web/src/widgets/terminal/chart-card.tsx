'use client';
import { Suspense, useCallback, useEffect, useRef, useState } from 'react';
import dynamic from 'next/dynamic';
import { Maximize2, Minimize2 } from 'lucide-react';
import PremiumErrorBoundary from '@/ui/common/error-boundary';
import { IconButton, Skeleton } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { useMarketClock } from '@/page-modules/token/market-session-context';
import { CHART_MAX_HEIGHT, useChartSettings, type ChartInterval } from '@/widgets/coin/use-chart-settings';

const TvChart = dynamic(() => import('@/widgets/trading/tv-chart'), { ssr: false, loading: () => <Skeleton variant="block" height={380} label="price chart" /> });

/** Quick picks set the candle interval only: TvChart exposes no visible-range API to its host. */
const QUICK_INTERVALS: readonly { value: ChartInterval; label: string; description: string }[] = [
  { value: '60', label: '1h', description: '1 hour candles' },
  { value: '1D', label: '1d', description: '1 day candles' },
  { value: '1W', label: '1w', description: '1 week candles' },
];

/** Chrome reserved around the plot in fullscreen: chart toolbar, resize handle, data toggle row and this footer. */
const FULLSCREEN_CHROME = 140;

export function clockLabel(now: number): string {
  const date = new Date(now);
  const pad = (value: number) => String(value).padStart(2, '0');
  const offset = -date.getTimezoneOffset();
  const sign = offset < 0 ? '-' : '+';
  const hours = Math.floor(Math.abs(offset) / 60);
  const minutes = Math.abs(offset) % 60;
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())} (UTC${sign}${hours}${minutes ? `:${pad(minutes)}` : ''})`;
}

/** Terminal centre chart: the shared TradingView chart (toolbar, plot, resize) with fullscreen, interval quick picks and a local clock. */
export default function TerminalChartCard({ poolAddress }: { poolAddress: string }) {
  const cardRef = useRef<HTMLElement>(null);
  const snapshot = usePoolSnapshot(poolAddress);
  const { settings, update } = useChartSettings();
  const now = useMarketClock();
  const [fullscreen, setFullscreen] = useState(false);
  const [viewportHeight, setViewportHeight] = useState<number | null>(null);
  const pool = snapshot.data?.pool.poolAddress ?? null;
  const symbol = snapshot.data?.metadata?.displaySymbol ?? snapshot.data?.token?.symbol;

  useEffect(() => {
    const sync = () => {
      const active = document.fullscreenElement !== null && document.fullscreenElement === cardRef.current;
      setFullscreen(active);
      setViewportHeight(active ? window.innerHeight : null);
    };
    document.addEventListener('fullscreenchange', sync);
    window.addEventListener('resize', sync);
    return () => { document.removeEventListener('fullscreenchange', sync); window.removeEventListener('resize', sync); };
  }, []);

  const toggleFullscreen = useCallback(() => {
    const card = cardRef.current;
    if (!card) return;
    if (document.fullscreenElement === card) void document.exitFullscreen().catch(() => undefined);
    else if (typeof card.requestFullscreen === 'function') void card.requestFullscreen().catch(() => undefined);
  }, []);

  const height = fullscreen && viewportHeight !== null ? Math.min(CHART_MAX_HEIGHT, Math.max(0, viewportHeight - FULLSCREEN_CHROME)) : undefined;

  return (
    <section ref={cardRef} aria-label="Chart" className={cn('flex min-w-0 flex-col overflow-hidden rounded-md bg-surface-1', fullscreen && 'h-screen w-screen rounded-none')}>
      <div className="min-w-0 flex-1">
        <PremiumErrorBoundary area="Terminal chart">
          <Suspense fallback={<Skeleton variant="block" height={380} label="price chart" />}>
            {pool ? (
              <TvChart poolAddress={pool} symbol={symbol} height={height} />
            ) : snapshot.isPending ? (
              <Skeleton variant="block" height={380} label="price chart" />
            ) : (
              <p role="status" className="p-4 text-sm text-text-muted">The chart can&apos;t load yet. Try again in a moment.</p>
            )}
          </Suspense>
        </PremiumErrorBoundary>
      </div>
      <div className="flex h-8 shrink-0 items-center gap-1 bg-surface-1 px-2 text-xs">
        <div role="group" aria-label="Quick candle interval" className="flex items-center gap-0.5">
          {QUICK_INTERVALS.map(option => {
            const active = settings.interval === option.value;
            return (
              <button
                key={option.value}
                type="button"
                aria-pressed={active}
                aria-label={option.description}
                onClick={() => update({ interval: option.value })}
                className={cn('h-6 rounded-sm px-2 font-bold tabular-nums transition-colors hover:bg-surface-hover hover:text-text', active ? 'bg-surface-3 text-text' : 'text-text-muted')}
              >
                {option.label}
              </button>
            );
          })}
        </div>
        <time suppressHydrationWarning dateTime={new Date(now).toISOString()} aria-label="Local time" className="ml-auto tabular-nums text-text-faint">{clockLabel(now)}</time>
        <IconButton
          label={fullscreen ? 'Exit fullscreen chart' : 'Fullscreen chart'}
          pressed={fullscreen}
          icon={fullscreen ? <Minimize2 /> : <Maximize2 />}
          size="sm"
          className="size-6"
          onClick={toggleFullscreen}
        />
      </div>
    </section>
  );
}
