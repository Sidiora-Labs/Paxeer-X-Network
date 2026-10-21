import { formatPercent } from '@/utils/format';

interface PriceChangeProps {
  value: number | null | undefined;
  decimals?: number;
  className?: string;
}

export default function PriceChange({ value, decimals = 2, className = '' }: PriceChangeProps) {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return <span className={`text-text-faint ${className}`} aria-label="No change yet">{'—'}</span>;
  }
  const tone = value > 0 ? 'text-percent-up' : value < 0 ? 'text-percent-down' : 'text-percent-flat';
  return (
    <span data-numeric className={`font-bold tabular-nums ${tone} ${className}`}>
      {value !== 0 ? <span className="sr-only">{value > 0 ? 'up ' : 'down '}</span> : null}
      {formatPercent(value, { decimals })}
    </span>
  );
}
