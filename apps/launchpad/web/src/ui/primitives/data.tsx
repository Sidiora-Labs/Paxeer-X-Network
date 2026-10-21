'use client';

import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Clock } from 'lucide-react';
import { cn } from '@/lib/utils';
import { formatCompactUsd, formatCount, formatPercent, formatUsdl, type FormatUsdlOptions } from '@/utils/format';

const EM_DASH = '—';
const MASK = '••••••';

/* ------------------------------------------------------------ StatTile */

export type StatTone = 'neutral' | 'positive' | 'negative' | 'accent';

export interface StatTileProps {
  label: string;
  /** Rendered value; null/undefined shows an em dash. */
  value: ReactNode | null | undefined;
  /** Unit spoken with the value when the visible value omits it. */
  unit?: string;
  tone?: StatTone;
  size?: 'bar' | 'inline' | 'row';
  loading?: boolean;
  masked?: boolean;
  /** Surface the tile sits on; the tile insets one step. */
  surface?: 1 | 2;
  className?: string;
}

const toneClass: Record<StatTone, string> = {
  neutral: 'text-text',
  positive: 'text-success',
  negative: 'text-danger',
  accent: 'text-accent',
};

/** One stat. Wrap a row of tiles in a `<dl>`; each tile renders its own dt/dd pair. */
export function StatTile({ label, value, unit, tone = 'neutral', size = 'bar', loading = false, masked = false, surface = 1, className }: StatTileProps) {
  const empty = value === null || value === undefined;
  const valueClass = size === 'bar' ? 'text-2xl font-extrabold' : size === 'inline' ? 'text-lg font-bold' : 'text-md font-bold';
  return (
    <div
      className={cn(
        'flex min-w-0 flex-col gap-1 rounded-md',
        size === 'row' ? 'p-0' : 'p-4',
        size !== 'row' && (surface === 1 ? 'bg-surface-2' : 'bg-surface-3'),
        size === 'inline' && 'min-h-16 min-w-24 p-3',
        className,
      )}
    >
      <dt className="text-2xs font-bold uppercase tracking-[.04em] text-text-faint">{label}</dt>
      <dd data-numeric className={cn('truncate tabular-nums', valueClass, empty ? 'text-text-faint' : toneClass[tone])} aria-busy={loading || undefined}>
        {loading ? (
          <span aria-hidden="true" className="sl-skeleton inline-block h-[1em] w-20 align-middle" />
        ) : masked ? (
          <span aria-label="Hidden">{MASK}</span>
        ) : empty ? (
          <span aria-label="No data">{EM_DASH}</span>
        ) : (
          <>
            {value}
            {unit ? <span className="sr-only"> {unit}</span> : null}
          </>
        )}
      </dd>
    </div>
  );
}

/* ------------------------------------------------------- CompactNumber */

export interface CompactNumberProps {
  value: number | string | null | undefined;
  kind?: 'usd' | 'count' | 'percent';
  decimals?: number;
  className?: string;
}

export function CompactNumber({ value, kind = 'count', decimals = 2, className }: CompactNumberProps) {
  if (value === null || value === undefined || value === '') return <span className={cn('text-text-faint', className)}>{EM_DASH}</span>;
  const numeric = typeof value === 'number' ? value : Number(value);
  if (!Number.isFinite(numeric)) return <span className={cn('text-text-faint', className)}>{EM_DASH}</span>;
  const text = kind === 'usd' ? formatCompactUsd(numeric) : kind === 'percent' ? formatPercent(numeric, { decimals, sign: 'negative' }) : formatCount(numeric);
  return <span data-numeric title={numeric.toLocaleString()} className={cn('tabular-nums', className)}>{text}</span>;
}

/* ---------------------------------------------------------- MoneyValue */

export interface MoneyValueProps extends FormatUsdlOptions {
  raw: string | bigint | null | undefined;
  /** auto colours positive success / negative danger; neutral keeps text. */
  tone?: 'auto' | 'neutral' | 'accent';
  size?: 'sm' | 'md' | 'lg' | 'xl';
  masked?: boolean;
  className?: string;
}

const moneySize = { sm: 'text-num-sm', md: 'text-num-md', lg: 'text-lg', xl: 'text-2xl' } as const;

export function MoneyValue({ raw, tone = 'neutral', size = 'md', masked = false, className, ...opts }: MoneyValueProps) {
  const text = formatUsdl(raw, opts);
  const negative = typeof raw === 'bigint' ? raw < 0n : typeof raw === 'string' && raw.trim().startsWith('-');
  const zero = typeof raw === 'bigint' ? raw === 0n : typeof raw === 'string' && /^[+-]?0+$/.test(raw.trim());
  const color = text === EM_DASH
    ? 'text-text-faint'
    : tone === 'accent'
      ? 'text-accent'
      : tone === 'auto'
        ? negative ? 'text-danger' : zero ? 'text-text' : 'text-success'
        : 'text-text';
  return (
    <span data-numeric className={cn('font-bold tabular-nums', moneySize[size], color, className)}>
      {masked ? <span aria-label="Hidden">{MASK}</span> : text}
    </span>
  );
}

/* ----------------------------------------------------------- Sparkline */

export interface SparklineProps {
  data: number[];
  width?: number;
  height?: number;
  direction?: 'up' | 'down' | 'auto';
  /** Accessible description, e.g. "24h price". Decorative sparklines pass `decorative`. */
  label?: string;
  decorative?: boolean;
  loading?: boolean;
  className?: string;
}

export function Sparkline({ data, width = 96, height = 28, direction = 'auto', label = 'Price trend', decorative = false, loading = false, className }: SparklineProps) {
  const geometry = useMemo(() => {
    if (data.length < 2) return null;
    const min = Math.min(...data);
    const max = Math.max(...data);
    const range = max - min || 1;
    const step = width / (data.length - 1);
    const points = data.map((v, i) => `${(i * step).toFixed(2)},${(height - 1.5 - ((v - min) / range) * (height - 3)).toFixed(2)}`);
    const line = `M${points.join(' L')}`;
    return { line, area: `${line} L${width},${height} L0,${height} Z`, first: data[0], last: data[data.length - 1] };
  }, [data, width, height]);

  if (loading) return <span aria-hidden="true" style={{ width, height }} className={cn('sl-skeleton inline-block', className)} />;

  const insufficient = geometry === null;
  const resolved = direction === 'auto'
    ? geometry && geometry.last > geometry.first ? 'up' : geometry && geometry.last < geometry.first ? 'down' : 'flat'
    : direction;
  const pct = geometry && geometry.first !== 0 ? ((geometry.last - geometry.first) / Math.abs(geometry.first)) * 100 : null;
  const description = insufficient
    ? `${label}, not enough data`
    : `${label}, ${resolved === 'flat' ? 'flat' : resolved}${pct !== null ? ` ${Math.abs(pct).toFixed(1)} percent` : ''}`;
  const stroke = insufficient || resolved === 'flat' ? 'stroke-text-faint' : resolved === 'up' ? 'stroke-candle-up' : 'stroke-candle-down';
  const fill = resolved === 'up' ? 'var(--sl-tint-accent-fill-chart)' : resolved === 'down' ? 'var(--sl-tint-danger-fill-chart)' : 'transparent';

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role={decorative ? undefined : 'img'}
      aria-label={decorative ? undefined : description}
      aria-hidden={decorative || undefined}
      preserveAspectRatio="none"
      className={cn('shrink-0 overflow-visible', className)}
    >
      {geometry ? (
        <>
          <path d={geometry.area} style={{ fill }} />
          <path d={geometry.line} fill="none" className={stroke} strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
        </>
      ) : (
        <line x1={0} x2={width} y1={height / 2} y2={height / 2} className={stroke} strokeWidth={1.5} strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />
      )}
    </svg>
  );
}

/* --------------------------------------------------------- ProgressBar */

export interface ProgressBarProps {
  /** 0-100; null renders the track only with an em dash. */
  value: number | null | undefined;
  /** Spoken description, e.g. "56 percent of all-time high market cap". */
  label: string;
  size?: 4 | 6;
  tone?: 'auto' | 'accent' | 'warning' | 'danger';
  showValue?: boolean;
  /** Text shown at the end when value reaches 100 in auto tone. */
  completeLabel?: string;
  flash?: boolean;
  width?: number | string;
  className?: string;
}

export function ProgressBar({ value, label, size = 6, tone = 'auto', showValue = false, completeLabel, flash = false, width, className }: ProgressBarProps) {
  const known = typeof value === 'number' && Number.isFinite(value);
  const pct = known ? Math.max(0, Math.min(100, value)) : 0;
  const fillTone = tone === 'warning' || (tone === 'auto' && pct >= 95) ? 'bg-warning' : tone === 'danger' ? 'bg-danger' : 'bg-track-fill';
  return (
    <span className={cn('inline-flex items-center gap-2', className)} style={width !== undefined ? { width } : undefined}>
      <span
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={known ? Math.round(pct) : undefined}
        aria-valuetext={known ? `${Math.round(pct)} percent, ${label}` : `unknown, ${label}`}
        className={cn('relative block min-w-10 flex-1 overflow-hidden rounded-pill bg-track', size === 6 ? 'h-1.5' : 'h-1', flash && 'sl-flash-up')}
      >
        <span className={cn('absolute inset-y-0 left-0 rounded-pill transition-[width] duration-base ease-sl', fillTone)} style={{ width: `${pct}%` }} />
      </span>
      {showValue ? (
        <span data-numeric className={cn('text-num-sm tabular-nums', known ? 'text-text' : 'text-text-faint')}>
          {known ? (pct >= 100 && completeLabel ? completeLabel : `${Math.round(pct)}%`) : EM_DASH}
        </span>
      ) : null}
    </span>
  );
}

/* ----------------------------------------------------------- PairedBar */

export interface PairedBarProps {
  left: number;
  right: number;
  leftLabel: string;
  rightLabel: string;
  /** Formats the raw counts for display and speech. */
  format?: (n: number) => string;
  className?: string;
}

export function PairedBar({ left, right, leftLabel, rightLabel, format = formatCount, className }: PairedBarProps) {
  const total = left + right;
  const leftPct = total > 0 ? (left / total) * 100 : 50;
  const description = total > 0
    ? `${format(left)} ${leftLabel} versus ${format(right)} ${rightLabel}, ${Math.round(leftPct)} percent ${leftLabel}`
    : `No ${leftLabel} or ${rightLabel} yet`;
  return (
    <div className={cn('flex flex-col gap-1', className)}>
      <div className="flex items-center justify-between text-xs">
        <span className="text-text-faint">{leftLabel} <span data-numeric className="font-bold tabular-nums text-success">{format(left)}</span></span>
        <span className="text-text-faint"><span data-numeric className="font-bold tabular-nums text-danger">{format(right)}</span> {rightLabel}</span>
      </div>
      <div role="img" aria-label={description} className="flex h-1 w-full overflow-hidden rounded-pill bg-track">
        <span className={cn('h-full', total > 0 ? 'bg-candle-up' : 'bg-transparent')} style={{ width: `${leftPct}%` }} />
        <span className={cn('h-full flex-1', total > 0 ? 'bg-candle-down' : 'bg-transparent')} />
      </div>
    </div>
  );
}

/* --------------------------------------------------- CountdownProgress */

export interface CountdownProgressProps {
  endsAt: string | number;
  startsAt?: string | number;
  variant?: 'text' | 'icon' | 'bar';
  /** Injectable clock for deterministic rendering. */
  now?: number;
  /** Seconds between refreshes. Default 60. */
  tick?: number;
  className?: string;
}

function toMs(value: string | number): number {
  if (typeof value === 'number') return value > 1e12 ? value : value * 1000;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? Number.NaN : parsed;
}

export function remainingLabel(ms: number): string {
  if (ms <= 0) return 'Ended';
  const totalMinutes = Math.floor(ms / 60_000);
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return `${days}d ${hours}h left`;
  if (hours > 0) return `${hours}h ${minutes}m left`;
  if (minutes > 0) return `${minutes}m left`;
  return 'Under a minute left';
}

export function CountdownProgress({ endsAt, startsAt, variant = 'text', now, tick = 60, className }: CountdownProgressProps) {
  const [clock, setClock] = useState(() => now ?? Date.now());
  useEffect(() => {
    if (now !== undefined) {
      setClock(now);
      return;
    }
    const id = window.setInterval(() => setClock(Date.now()), tick * 1000);
    return () => window.clearInterval(id);
  }, [now, tick]);

  const end = toMs(endsAt);
  if (Number.isNaN(end)) return <span className={cn('text-xs text-text-faint', className)}>{EM_DASH}</span>;
  const remaining = end - clock;
  const ended = remaining <= 0;
  const endingSoon = !ended && remaining < 48 * 3600_000;
  const start = startsAt !== undefined ? toMs(startsAt) : Number.NaN;
  const pct = !Number.isNaN(start) && end > start ? Math.max(0, Math.min(100, (remaining / (end - start)) * 100)) : null;
  const textTone = ended ? 'text-text-faint' : endingSoon ? 'text-warning' : 'text-text-muted';

  return (
    <span className={cn('inline-flex flex-col gap-1', className)}>
      <span className={cn('inline-flex items-center gap-1 text-xs tabular-nums', textTone)}>
        {variant !== 'text' ? <Clock aria-hidden="true" className="size-icon-sm" /> : null}
        <time dateTime={new Date(end).toISOString()} title={new Date(end).toLocaleString()}>{remainingLabel(remaining)}</time>
      </span>
      {variant === 'bar' ? (
        <span aria-hidden="true" className="block h-1 w-full overflow-hidden rounded-pill bg-track">
          <span className={cn('block h-full rounded-pill', ended ? 'bg-transparent' : endingSoon ? 'bg-warning-strong' : 'bg-track-fill')} style={{ width: `${ended ? 0 : pct ?? 100}%` }} />
        </span>
      ) : null}
    </span>
  );
}

/* --------------------------------------------------------- KeyValueRow */

export interface KeyValueRowProps {
  label: ReactNode;
  value: ReactNode | null | undefined;
  loading?: boolean;
  masked?: boolean;
  trailing?: ReactNode;
  className?: string;
}

/** Wrap rows in a `<dl>`; each row renders its own dt/dd pair. */
export function KeyValueRow({ label, value, loading = false, masked = false, trailing, className }: KeyValueRowProps) {
  const empty = value === null || value === undefined;
  return (
    <div className={cn('flex min-h-10 items-center justify-between gap-3 py-1', className)}>
      <dt className="shrink-0 text-sm text-text-faint">{label}</dt>
      <dd data-numeric className={cn('flex min-w-0 items-center justify-end gap-2 text-right text-num-md font-bold tabular-nums', empty ? 'text-text-faint' : 'text-text')} aria-busy={loading || undefined}>
        {loading ? <span aria-hidden="true" className="sl-skeleton inline-block h-4 w-16" /> : masked ? <span aria-label="Hidden" className="text-text-muted">{MASK}</span> : empty ? EM_DASH : <span className="min-w-0 break-words">{value}</span>}
        {trailing}
      </dd>
    </div>
  );
}
