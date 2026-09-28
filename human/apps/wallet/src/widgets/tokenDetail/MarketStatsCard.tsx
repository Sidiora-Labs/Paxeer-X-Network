'use client';

/**
 * Market stats — PAX / WPAX only. Shows market cap, circulating supply, and
 * 24h high/low derived from the OHLC dataset.
 */

import { formatUsd } from '@/lib/format';
import type { OhlcCandle } from '@/lib/api';
import { SectionLabel, MetricCard } from './Atoms';

const PAX_CIRCULATING = 500_000_000;

export interface MarketStatsCardProps {
  paxPrice: number;
  ohlcData: OhlcCandle[];
}

export function MarketStatsCard({ paxPrice, ohlcData }: MarketStatsCardProps) {
  if (paxPrice <= 0) return null;

  // 288 ≈ 24h of 5-minute candles; if shorter dataset, slice still works.
  const last24h = ohlcData.slice(-288);
  const has24hData = last24h.length > 0;

  return (
    <>
      <div className="col-span-2 px-1 pt-1">
        <SectionLabel text="Market" />
      </div>
      <MetricCard label="Market Cap" value={formatUsd(paxPrice * PAX_CIRCULATING)} />
      <MetricCard label="Circulating" value="500M PAX" />
      {has24hData && (
        <>
          <MetricCard
            label="24h High"
            value={formatUsd(Math.max(...last24h.map((c) => c.high)))}
          />
          <MetricCard
            label="24h Low"
            value={formatUsd(Math.min(...last24h.map((c) => c.low)))}
          />
        </>
      )}
    </>
  );
}
