'use client';
import { Trophy } from 'lucide-react';
import type { LeaderboardWindow } from '@/core/market/use-leaderboard';
import { SegmentedControl } from '@/ui/primitives';

export type LeaderboardPeriod = 'cup' | '1d' | '1w' | '1m';
export type RollingPeriod = Exclude<LeaderboardPeriod, 'cup'>;

/** Hero control segments map onto the IF_LEADERBOARDS windows. */
export const periodWindows: Record<RollingPeriod, LeaderboardWindow> = { '1d': '24h', '1w': '7d', '1m': '30d' };

export const periodLabels: Record<LeaderboardPeriod, string> = { cup: 'Competition', '1d': 'Last 24 hours', '1w': 'Last 7 days', '1m': 'Last 30 days' };

export function isPeriod(value: string | null | undefined): value is LeaderboardPeriod {
  return value === 'cup' || value === '1d' || value === '1w' || value === '1m';
}

export function parsePeriod(value: string | null | undefined, fallback: LeaderboardPeriod = '1d'): LeaderboardPeriod {
  return isPeriod(value) ? value : fallback;
}

function periodOptions(cupDisabled: boolean) {
  return [
    { value: 'cup' as const, label: null, icon: <Trophy />, ariaLabel: periodLabels.cup, disabled: cupDisabled, title: cupDisabled ? 'No competition to rank right now' : undefined },
    { value: '1d' as const, label: '1D', ariaLabel: periodLabels['1d'] },
    { value: '1w' as const, label: '1W', ariaLabel: periodLabels['1w'] },
    { value: '1m' as const, label: '1M', ariaLabel: periodLabels['1m'] },
  ];
}

export interface PeriodControlProps {
  value: LeaderboardPeriod;
  onChange: (value: LeaderboardPeriod) => void;
  size?: 'sm' | 'md';
  /** Disables the trophy segment when no cup record is available to rank. */
  cupDisabled?: boolean;
  className?: string;
}

/** Trophy / 1D / 1W / 1M segmented control (components.md 11, 4-up with an icon segment); it sits inside the cup hero. */
export function PeriodControl({ value, onChange, size = 'md', cupDisabled = false, className }: PeriodControlProps) {
  return <SegmentedControl options={periodOptions(cupDisabled)} value={value} onChange={onChange} label="Ranking period" size={size} className={className} />;
}
