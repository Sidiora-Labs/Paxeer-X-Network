import { ChefHat, Crosshair, Crown, Info, Layers, Users } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Tooltip } from '@/ui/primitives';

export type RiskLevel = 'ok' | 'warn' | 'bad' | 'unknown';

export interface RiskRowProps {
  holders: number | null;
  /** Sniper share in percent; the backend serves no sniper detection today, so this is null. */
  sniperPct: number | null;
  /** Bundle share in percent; the backend serves no bundle detection today, so this is null. */
  bundlePct: number | null;
  devPct: number | null;
  top10Pct: number | null;
  loading?: boolean;
  legend?: boolean;
  className?: string;
}

const finite = (value: number | null): value is number => value !== null && Number.isFinite(value);

export function holdersLevel(holders: number | null): RiskLevel {
  if (!finite(holders)) return 'unknown';
  return holders < 50 ? 'warn' : 'ok';
}

export function top10Level(pct: number | null): RiskLevel {
  if (!finite(pct)) return 'unknown';
  if (pct > 20) return 'bad';
  return pct >= 12 ? 'warn' : 'ok';
}

export function devLevel(pct: number | null): RiskLevel {
  if (!finite(pct)) return 'unknown';
  if (pct > 10) return 'bad';
  return pct >= 5 ? 'warn' : 'ok';
}

export function sniperLevel(pct: number | null): RiskLevel {
  if (!finite(pct)) return 'unknown';
  return pct > 25 ? 'bad' : 'ok';
}

export function bundleLevel(pct: number | null): RiskLevel {
  if (!finite(pct)) return 'unknown';
  return pct > 15 ? 'bad' : 'ok';
}

const levelClass: Record<RiskLevel, string> = {
  ok: 'text-risk-ok',
  warn: 'text-risk-warn',
  bad: 'text-risk-bad',
  unknown: 'text-risk-unknown',
};

const levelText: Record<RiskLevel, string> = { ok: 'ok', warn: 'warning', bad: 'high risk', unknown: 'not tracked yet' };

export function formatRiskPercent(pct: number | null): string {
  if (!finite(pct)) return '—';
  return `${pct >= 10 ? Math.round(pct) : pct.toFixed(1).replace(/\.0$/, '')}%`;
}

export function formatHolders(holders: number | null): string {
  if (!finite(holders)) return '—';
  if (holders >= 1_000_000) return `${(holders / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`;
  if (holders >= 10_000) return `${(holders / 1_000).toFixed(0)}K`;
  if (holders >= 1_000) return `${(holders / 1_000).toFixed(1).replace(/\.0$/, '')}K`;
  return String(holders);
}

interface Metric {
  key: string;
  label: string;
  Icon: typeof Users;
  value: string;
  level: RiskLevel;
  reason?: string;
}

export function riskMetrics({ holders, sniperPct, bundlePct, devPct, top10Pct }: Pick<RiskRowProps, 'holders' | 'sniperPct' | 'bundlePct' | 'devPct' | 'top10Pct'>): Metric[] {
  return [
    { key: 'holders', label: 'Holders', Icon: Users, value: formatHolders(holders), level: holdersLevel(holders) },
    { key: 'sniper', label: 'Sniper share', Icon: Crosshair, value: formatRiskPercent(sniperPct), level: sniperLevel(sniperPct), reason: sniperPct === null ? 'Sniper share is not tracked yet' : undefined },
    { key: 'bundle', label: 'Bundle share', Icon: Layers, value: formatRiskPercent(bundlePct), level: bundleLevel(bundlePct), reason: bundlePct === null ? 'Bundle share is not tracked yet' : undefined },
    { key: 'dev', label: 'Creator holding', Icon: ChefHat, value: formatRiskPercent(devPct), level: devLevel(devPct) },
    { key: 'top10', label: 'Top 10 holders', Icon: Crown, value: formatRiskPercent(top10Pct), level: top10Level(top10Pct) },
  ];
}

export const riskLegend = [
  'Holders: under 50 is a warning',
  'Sniper share: over 25% is high risk (not tracked yet)',
  'Bundle share: over 15% is high risk (not tracked yet)',
  'Creator holding: 5 to 10% is a warning, over 10% is high risk',
  'Top 10 holders: 12 to 20% is a warning, over 20% is high risk',
];

/**
 * Risk indicator row (components.md 25): holders, sniper %, bundle %, dev %,
 * top-10 % coloured by threshold. Metrics the backend does not serve render
 * as an em dash in the unknown tone with the reason on hover.
 */
export function RiskRow({ holders, sniperPct, bundlePct, devPct, top10Pct, loading = false, legend = false, className }: RiskRowProps) {
  if (loading) {
    return (
      <div aria-busy="true" className={cn('flex h-5 items-center gap-2.5 px-3', className)}>
        <span className="sr-only">Loading risk indicators</span>
        {Array.from({ length: 5 }, (_, index) => <span key={index} aria-hidden="true" className="sl-skeleton h-3 w-10" />)}
      </div>
    );
  }
  const metrics = riskMetrics({ holders, sniperPct, bundlePct, devPct, top10Pct });
  return (
    <ul aria-label="Risk indicators" className={cn('flex h-5 items-center gap-2.5 px-3 text-xs tabular-nums', className)}>
      {metrics.map(({ key, label, Icon, value, level, reason }) => (
        <li key={key} className={cn('inline-flex items-center gap-1', levelClass[level])} title={reason ?? `${label}: ${value}`} data-level={level}>
          <Icon aria-hidden="true" className="size-3 shrink-0" />
          <span aria-hidden="true">{value}</span>
          <span className="sr-only">{value === '—' ? `${label} not tracked yet` : `${label} ${value}, ${levelText[level]}`}</span>
        </li>
      ))}
      {legend ? (
        <li className="ml-auto inline-flex">
          <Tooltip content={<ul className="flex flex-col gap-1">{riskLegend.map(line => <li key={line}>{line}</li>)}</ul>} align="end">
            <button type="button" aria-label="What these numbers mean" className="inline-flex size-5 items-center justify-center rounded-xs text-text-faint hover:bg-surface-hover hover:text-text">
              <Info aria-hidden="true" className="size-3" />
            </button>
          </Tooltip>
        </li>
      ) : null}
    </ul>
  );
}
