'use client';
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';
import { PercentPill, Tooltip } from '@/ui/primitives';
import type { StatValue } from '@/core/market/pool-stats-display';

export interface StatDashProps {
  /** Why there is no value; spoken with `subject` and shown on hover or focus. */
  reason: string;
  subject: string;
  align?: 'start' | 'center' | 'end';
  className?: string;
}

/** The explicit unavailable treatment for one cell: a faint em dash whose reason is a tooltip and the accessible name. */
export function StatDash({ reason, subject, align = 'end', className }: StatDashProps) {
  return (
    <Tooltip content={reason} align={align} className="relative z-10">
      <span tabIndex={0} data-numeric data-unavailable="" aria-label={`${subject}: ${reason}`} className={cn('text-xs text-text-faint', className)}>—</span>
    </Tooltip>
  );
}

export interface StatChangeProps {
  change: StatValue<number>;
  variant?: 'pill' | 'text';
  size?: 'sm' | 'md';
  arrow?: boolean;
  decimals?: number;
  align?: 'start' | 'center' | 'end';
  flash?: 'up' | 'down' | null;
  className?: string;
}

/** A window change from the v2 statistics: the percent pill on a complete baseline, otherwise the dash with its reason. */
export function StatChange({ change, variant = 'pill', size = 'sm', arrow = false, decimals = 1, align = 'end', flash = null, className }: StatChangeProps) {
  if (change.value !== null) return <PercentPill value={change.value} variant={variant} size={size} arrow={arrow} decimals={decimals} flash={flash} className={className} />;
  return <StatDash reason={change.reason ?? 'No change yet'} subject="Change" align={align} className={className} />;
}

/** `children` when the value is present, else the dash with the reason. */
export function StatOr<T>({ stat, subject, align, children }: { stat: StatValue<T>; subject: string; align?: 'start' | 'center' | 'end'; children: (value: T) => ReactNode }) {
  return stat.value !== null ? <>{children(stat.value)}</> : <StatDash reason={stat.reason ?? `${subject} not available yet`} subject={subject} align={align} />;
}
