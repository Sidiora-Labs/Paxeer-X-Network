'use client';

/**
 * Compact best-route summary rendered below the swap inputs.
 *
 * Shows the chosen protocol, fee, optional price impact (color-coded for
 * 1%+/3%+ impact), and the total number of routes considered.
 */

import type { SwapQuote } from '@/lib/swap';

export interface RouteInfoCardProps {
  bestQuote: SwapQuote;
  totalRoutes: number;
}

const impactColor = (pct: number): string => {
  if (pct > 3) return 'text-red-400';
  if (pct > 1) return 'text-amber-400';
  return '';
};

export function RouteInfoCard({ bestQuote, totalRoutes }: RouteInfoCardProps) {
  return (
    <div className="mt-3 glass-card p-3 space-y-1.5 text-xs animate-scale-in">
      <div className="flex justify-between">
        <span className="text-pax-muted">Best Route</span>
        <span className="text-pax-accent font-medium">{bestQuote.protocolLabel}</span>
      </div>
      <div className="flex justify-between">
        <span className="text-pax-muted">Fee</span>
        <span>{bestQuote.feeBps / 100}%</span>
      </div>
      {bestQuote.priceImpact > 0 && (
        <div className="flex justify-between">
          <span className="text-pax-muted">Price Impact</span>
          <span className={impactColor(bestQuote.priceImpact)}>
            {bestQuote.priceImpact.toFixed(2)}%
          </span>
        </div>
      )}
      {totalRoutes > 1 && (
        <div className="flex justify-between">
          <span className="text-pax-muted">Routes found</span>
          <span>{totalRoutes}</span>
        </div>
      )}
    </div>
  );
}
