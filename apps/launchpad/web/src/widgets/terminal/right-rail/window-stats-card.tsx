'use client';
import { useEffect, useState, type ReactNode } from 'react';
import { ErrorState, NoticeBar, PercentPill, SegmentedControl, Skeleton, Tooltip, UnavailableState } from '@/ui/primitives';
import type { PoolStats } from '@/core/market/use-stats-batch';
import { formatCount, formatUsdl } from '@/utils/format';
import { cn } from '@/lib/utils';
import type { Trade } from '../../../../../../../sdk/launchpad/packages/production-contracts/src/types.gen';
import { deriveWindowStats, statsWindows, useWindowCandles, type StatsWindow, type WindowStats } from './window-stats';
import { userFacingMessage } from '@/core/user-facing-error';

const labels: Record<StatsWindow, string> = { '5m': '5M', '1h': '1H', '6h': '6H', '24h': '24H' };

function useHalfMinuteClock(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, []);
  return now;
}

function clockTime(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function money(raw: bigint, signed = false): string {
  return formatUsdl(raw, { compact: true, unit: false, signed });
}

function Figure({ label, children, tone }: { label: string; children: ReactNode; tone?: 'buy' | 'sell' }) {
  return (
    <div className="flex min-w-0 flex-col items-center gap-0.5 text-center">
      <dt className="text-2xs text-text-faint">{label}</dt>
      <dd className={cn('truncate text-xs font-bold tabular-nums', tone === 'buy' ? 'text-success' : tone === 'sell' ? 'text-danger' : 'text-text')}>{children}</dd>
    </div>
  );
}

function Dash({ reason }: { reason: string }) {
  return <Tooltip content={reason}><span tabIndex={0} aria-label={`Not available yet: ${reason}`}>-</span></Tooltip>;
}

function Figures({ stats }: { stats: WindowStats }) {
  const { counts } = stats;
  const countNote = counts.source === 'pool-stats' ? 'Buy and sell counts cover the last 24 hours' : undefined;
  return (
    <dl className="grid grid-cols-4 gap-1 rounded-sm bg-surface-2 px-1 py-2">
      <Figure label="Volume">{money(stats.volumeRaw)}</Figure>
      <Figure label="Buys" tone="buy"><span title={countNote}>{formatCount(counts.buys)}</span> / {money(stats.buyUsdlRaw)}</Figure>
      <Figure label="Sells" tone="sell"><span title={countNote}>{formatCount(counts.sells)}</span> / {money(stats.sellUsdlRaw)}</Figure>
      <Figure label="Net vol." tone={stats.netRaw > 0n ? 'buy' : stats.netRaw < 0n ? 'sell' : undefined}>{money(stats.netRaw, true)}</Figure>
    </dl>
  );
}

function Notes({ stats }: { stats: WindowStats }) {
  const { counts } = stats;
  return (
    <div className="flex flex-col gap-1.5">
      {counts.source === 'fills' && !counts.complete ? (
        <NoticeBar tone="warning" surface={1}>
          {counts.retained === 0
            ? `Buy and sell counts are still loading for this window. USDL totals are complete.`
            : `Buy and sell counts since ${counts.coveredSince ? clockTime(counts.coveredSince) : `block ${counts.oldestHeight ?? '-'}`} (${formatCount(counts.retained)} of ${formatCount(stats.trades)} trades). USDL totals are complete.`}
        </NoticeBar>
      ) : null}
      {stats.partialCandles > 0 ? (
        <NoticeBar tone="warning" surface={1}>Still updating, totals may rise as more trades come in.</NoticeBar>
      ) : null}
      <p className="text-2xs text-text-faint">
        Since {clockTime(stats.since)}
      </p>
    </div>
  );
}

/** Window stats (5M / 1H / 6H / 24H) for the open pool. */
export function WindowStatsCard({ poolAddress, fills, stats }: { poolAddress: string; fills: readonly Trade[]; stats: PoolStats | null }) {
  const [selected, setSelected] = useState<StatsWindow>('24h');
  const now = useHalfMinuteClock();
  const series = useWindowCandles(poolAddress, now);
  const derived = (window: StatsWindow): WindowStats | null => {
    const entry = series[window];
    return entry.candles.state === 'ready' ? deriveWindowStats(window, entry.candles.data, fills, stats, entry.from) : null;
  };
  const active = series[selected];
  const current = derived(selected);
  const marker = active.candles.state === 'unsupported' ? active.candles : null;
  const options = statsWindows.map(window => {
    const change = derived(window)?.changePct ?? null;
    return {
      value: window,
      label: (
        <span className="flex flex-col items-center leading-tight">
          <span>{labels[window]}</span>
          {change === null ? <span className="text-2xs font-semibold text-text-faint" aria-label="no price change in this window">-</span> : <PercentPill value={change} variant="text" size="sm" />}
        </span>
      ),
    };
  });

  return (
    <section aria-label="Window stats" className="flex flex-col gap-2 rounded-md bg-surface-1 p-2">
      <SegmentedControl<StatsWindow> options={options} value={selected} onChange={setSelected} label="Stats window" size="md" shape="sm" fullWidth />
      {current ? (
        <>
          <Figures stats={current} />
          <Notes stats={current} />
        </>
      ) : active.candles.state === 'loading' ? (
        <Skeleton variant="rows" rows={2} label="window stats" />
      ) : marker ? (
        <div className="flex flex-col gap-2">
          <dl className="grid grid-cols-4 gap-1 rounded-sm bg-surface-2 px-1 py-2">
            {['Volume', 'Buys', 'Sells', 'Net vol.'].map(label => <Figure key={label} label={label}><Dash reason="Not available yet" /></Figure>)}
          </dl>
          <UnavailableState variant="inline" title="These stats" operation={marker.operation} status={marker.status} />
        </div>
      ) : active.candles.state === 'error' ? (
        <ErrorState variant="inline" message={`Stats can't load right now. ${userFacingMessage(active.candles.error)}`} onRetry={active.candles.retry} />
      ) : null}
    </section>
  );
}
