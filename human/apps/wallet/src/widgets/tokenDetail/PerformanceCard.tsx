'use client';

/**
 * 24h performance card — Sidiora-only. Shows volume, trades, unique traders,
 * 24h high/low. Hidden when no pool stats are available.
 */

import { formatUsd, formatPrice, formatCompactNumber, formatCompactUsd } from '@/lib/format';
import { SectionLabel, InfoRow } from './Atoms';
import type { SidioraPoolStats } from '@/lib/api';

export interface PerformanceCardProps {
  stats: SidioraPoolStats | null;
}

export function PerformanceCard({ stats }: PerformanceCardProps) {
  if (!stats) return null;

  const volume24h = stats.volume24h ? parseFloat(stats.volume24h) / 1e6 : 0;
  const traders24h = (stats.buyCount24h || 0) + (stats.sellCount24h || 0);
  const uniqueTraders = stats.uniqueTraders24h || 0;

  return (
    <>
      <div className="col-span-2 px-1 pt-1">
        <SectionLabel text="24h Performance" />
      </div>
      <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
        <InfoRow
          label="Volume"
          value={
            <span className="font-semibold">
              {volume24h >= 1000 ? formatCompactUsd(volume24h) : formatUsd(volume24h)}
            </span>
          }
        />
        <InfoRow
          label="Trades"
          value={<span className="font-semibold">{traders24h.toLocaleString()}</span>}
        />
        {uniqueTraders > 0 && (
          <InfoRow
            label="Unique Traders"
            value={<span className="font-semibold">{uniqueTraders.toLocaleString()}</span>}
          />
        )}
        {stats.high24h && (
          <InfoRow
            label="24h High"
            value={
              <span className="font-semibold">
                {formatPrice(parseFloat(stats.high24h) / 1e18)}
              </span>
            }
          />
        )}
        {stats.low24h && (
          <InfoRow
            label="24h Low"
            value={
              <span className="font-semibold">
                {formatPrice(parseFloat(stats.low24h) / 1e18)}
              </span>
            }
          />
        )}
      </div>
    </>
  );
}
