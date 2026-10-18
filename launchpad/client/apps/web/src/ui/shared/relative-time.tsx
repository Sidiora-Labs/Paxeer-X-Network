import { relativeAge } from '@/utils/format';

interface RelativeTimeProps {
  /** Unix seconds (or ms, detected by magnitude) or an ISO string. */
  timestamp: number | string;
  /** "5m" instead of "5m ago". */
  compact?: boolean;
  now?: number;
  className?: string;
}

export default function RelativeTime({ timestamp, compact = false, now, className = '' }: RelativeTimeProps) {
  const ms = typeof timestamp === 'number' ? (timestamp > 1e12 ? timestamp : timestamp * 1000) : Date.parse(timestamp);
  const date = Number.isNaN(ms) ? null : new Date(ms);
  const age = relativeAge(timestamp, now);
  return (
    <time
      dateTime={date ? date.toISOString() : undefined}
      title={date ? date.toLocaleString() : undefined}
      className={`text-text-faint tabular-nums ${className}`}
    >
      {compact || age === '—' ? age : `${age} ago`}
    </time>
  );
}
