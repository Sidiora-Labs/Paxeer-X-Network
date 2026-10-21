'use client';

/**
 * Security card — top-10 holder concentration + risk score.
 * Hidden when neither metric is available.
 */

import { Shield } from 'lucide-react';
import { SectionLabel, InfoRow } from './Atoms';

const RISK_GREEN = 30;
const RISK_AMBER = 60;

export interface SecurityCardProps {
  top10Pct: number;
  riskRating: number | null;
}

export function SecurityCard({ top10Pct, riskRating }: SecurityCardProps) {
  if (top10Pct <= 0 && riskRating === null) return null;

  const riskColor =
    riskRating === null
      ? ''
      : riskRating <= RISK_GREEN
        ? 'text-pax-success'
        : riskRating <= RISK_AMBER
          ? 'text-pax-warning'
          : 'text-pax-error';

  return (
    <>
      <div className="col-span-2 px-1 pt-1">
        <SectionLabel text="Security" />
      </div>
      <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
        {top10Pct > 0 && (
          <InfoRow
            label={
              <span className="flex items-center gap-1">
                Top 10 Holders <Shield className="w-3 h-3 text-pax-muted" />
              </span>
            }
            value={<span className="font-semibold">{top10Pct.toFixed(2)}%</span>}
          />
        )}
        {riskRating !== null && (
          <InfoRow
            label="Risk Score"
            value={<span className={`font-semibold ${riskColor}`}>{riskRating}/100</span>}
          />
        )}
      </div>
    </>
  );
}
