'use client';
import { cn } from '@/lib/utils';
import { formatMarketCap, marketCapUnavailableLabel } from './format';

export interface MarketCapChipProps {
  /** Raw USDL (6 decimals) circulating market cap; null when the stats projection has none. */
  raw: string | null | undefined;
  /** `chip`: surface-2 pill with an "MC" label (hero meta, footers). `media`: media-overlay pill with the value in success colour. */
  variant?: 'chip' | 'media';
  size?: 'sm' | 'md';
  className?: string;
}

export function MarketCapChip({ raw, variant = 'chip', size = 'sm', className }: MarketCapChipProps) {
  const value = formatMarketCap(raw);
  return (
    <span
      title={value === null ? marketCapUnavailableLabel : undefined}
      className={cn(
        'inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded-pill px-2 tabular-nums',
        size === 'md' ? 'h-6 text-sm' : 'h-5 text-xs',
        variant === 'media' ? 'bg-media-overlay' : 'bg-surface-2',
        className,
      )}
    >
      <span className="sr-only">Market cap </span>
      {variant === 'chip' ? <span aria-hidden="true" className="text-text-faint">MC</span> : null}
      {value === null ? (
        <>
          <span aria-hidden="true" className="text-text-faint">&mdash;</span>
          <span className="sr-only">not available yet</span>
        </>
      ) : (
        <span className={cn('font-bold', variant === 'media' ? 'text-success' : 'text-text')}>{value}</span>
      )}
    </span>
  );
}
