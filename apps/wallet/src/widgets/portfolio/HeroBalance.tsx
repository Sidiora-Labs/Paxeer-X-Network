'use client';

/**
 * Hero balance card — total portfolio USD, daily PnL pill, PAX spot.
 *
 * Stateless except for the local hide toggle. All numeric work is done by the
 * orchestrator before this widget is rendered.
 */

import { useState } from 'react';
import { formatUsd } from '@/lib/format';
import { SvgIcon } from '@/components/ui/SvgIcon';

export interface HeroBalanceProps {
  totalUsd: number;
  loading: boolean;
  paxPrice: number;
  dailyPnlUsd: string | null;
  dailyPnlPercent: string | null;
}

export function HeroBalance({
  totalUsd,
  loading,
  paxPrice,
  dailyPnlUsd,
  dailyPnlPercent,
}: HeroBalanceProps) {
  const [hidden, setHidden] = useState(false);

  const pnlVal = dailyPnlUsd && dailyPnlUsd !== '0' ? parseFloat(dailyPnlUsd) : null;
  const pnlPct = dailyPnlPercent ? parseFloat(dailyPnlPercent) : 0;
  const pos = pnlVal !== null && pnlVal >= 0;

  return (
    <div className="col-span-2 bg-pax-surface rounded-[20px] px-5 pt-6 pb-5 relative">
      <button
        onClick={() => setHidden(!hidden)}
        className="absolute top-5 right-4 w-8 h-8 flex items-center justify-center rounded-[10px] bg-white/[0.04] press-scale"
      >
        <SvgIcon name="lock" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.5)' }} />
      </button>
      <p className="text-[11px] font-semibold text-pax-muted tracking-[0.08em] uppercase mb-2">
        Total Balance
      </p>
      {loading ? (
        <div className="h-12 w-48 shimmer rounded-lg mb-2" />
      ) : (
        <h1 className="text-[42px] font-extrabold tracking-[-0.04em] leading-none mb-3">
          {hidden ? '••••••' : formatUsd(totalUsd)}
        </h1>
      )}
      {!loading && (
        <div className="flex items-center gap-2 flex-wrap">
          {!hidden && pnlVal !== null && (
            <>
              <span className={`text-[13px] font-semibold ${pos ? 'text-pax-success' : 'text-pax-error'}`}>
                {pos ? '+' : ''}{formatUsd(pnlVal)}
              </span>
              <span className={`text-[11px] font-bold px-2 py-0.5 rounded-md ${pos ? 'bg-pax-success/[0.12] text-pax-success' : 'bg-pax-error/[0.12] text-pax-error'}`}>
                {pos ? '+' : ''}{pnlPct.toFixed(2)}%
              </span>
            </>
          )}
          {paxPrice > 0 && (
            <span className="text-[11px] text-pax-muted ml-auto">
              PAX {hidden ? '•••' : formatUsd(paxPrice)}
            </span>
          )}
        </div>
      )}
    </div>
  );
}
