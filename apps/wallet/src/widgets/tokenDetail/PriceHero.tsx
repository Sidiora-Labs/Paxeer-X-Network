'use client';

/**
 * Price hero — displays current price + 24h change pill, swaps to a scrubbed
 * value when the user drags across the chart below.
 *
 * Stateless: scrub state is owned by `TokenDetailWidget` and passed in.
 */

import { formatPrice } from '@/lib/format';

const POS_COLOR = 'var(--color-status-success)';
const NEG_COLOR = 'var(--color-status-danger)';

export interface PriceChangeInfo {
  pct: number;
  dollar: number;
  positive: boolean;
}

export interface PriceHeroProps {
  loading: boolean;
  displayPrice: number;
  /** Set when the user is hovering / dragging a point on the chart. */
  scrubTime: number | null;
  /**
   * Either the active scrub change (when user is dragging) or the period
   * change (when at rest). Null when not enough data points.
   */
  change: PriceChangeInfo | null;
  isScrubbing: boolean;
}

const formatDollarChange = (dollar: number, positive: boolean): string => {
  const sign = positive ? '+' : '-';
  const abs = Math.abs(dollar);
  if (abs === 0) return formatPrice(0);
  return `${sign}${formatPrice(abs)}`;
};

export function PriceHero({
  loading,
  displayPrice,
  scrubTime,
  change,
  isScrubbing,
}: PriceHeroProps) {
  if (loading) {
    return (
      <div className="px-5 pt-3 pb-1 space-y-2">
        <div className="h-10 w-52 shimmer rounded-lg" />
        <div className="h-5 w-36 shimmer rounded-lg" />
      </div>
    );
  }

  if (displayPrice <= 0) {
    return (
      <div className="px-5 pt-3 pb-1">
        <p className="text-sm text-pax-muted">No price data</p>
      </div>
    );
  }

  const colorClass = change?.positive ? `text-[${POS_COLOR}]` : `text-[${NEG_COLOR}]`;
  const bgColorClass = change?.positive
    ? `bg-[${POS_COLOR}]/15 text-[${POS_COLOR}]`
    : `bg-[${NEG_COLOR}]/15 text-[${NEG_COLOR}]`;

  return (
    <div className="px-5 pt-3 pb-1">
      {isScrubbing && scrubTime && (
        <p className="text-[11px] text-pax-muted font-medium mb-0.5">
          {new Date(scrubTime * 1000).toLocaleString('en-US', {
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
          })}
        </p>
      )}
      <p
        className={`text-[32px] font-bold leading-tight tracking-tight transition-opacity duration-100 ${
          isScrubbing ? 'text-white' : ''
        }`}
      >
        {formatPrice(displayPrice)}
      </p>
      {change && (
        <div className="flex items-center gap-2 mt-0.5">
          {change.dollar !== 0 && (
            <span className={`text-sm font-medium ${colorClass}`}>
              {formatDollarChange(change.dollar, change.positive)}
            </span>
          )}
          <span className={`text-xs font-semibold px-1.5 py-0.5 rounded ${bgColorClass}`}>
            {change.positive ? '+' : ''}
            {change.pct.toFixed(2)}%
          </span>
        </div>
      )}
    </div>
  );
}
