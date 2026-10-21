'use client';

/**
 * Position card — only renders when the wallet holds this token.
 * Two metrics: USD value + token balance (with symbol).
 */

import { formatUsd, formatCompactNumber } from '@/lib/format';
import { SectionLabel, MetricCard } from './Atoms';

export interface PositionCardProps {
  balanceNum: number;
  valueUsd: number;
  tokenSymbol: string;
}

export function PositionCard({ balanceNum, valueUsd, tokenSymbol }: PositionCardProps) {
  if (balanceNum <= 0 && valueUsd <= 0) return null;

  return (
    <>
      <div className="col-span-2 px-1 pt-1">
        <SectionLabel text="Position" />
      </div>
      <MetricCard label="Value" value={valueUsd > 0 ? formatUsd(valueUsd) : '--'} />
      <MetricCard
        label="Balance"
        value={`${formatCompactNumber(balanceNum)} ${tokenSymbol}`}
      />
    </>
  );
}
