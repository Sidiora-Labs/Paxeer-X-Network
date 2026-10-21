'use client';
import type { StreamState } from '@kindle/sdk';
import { CircleAlert, RefreshCw } from 'lucide-react';
import { plainCopy } from '@/core/user-facing-error';
import { cn } from '@/lib/utils';

export type MarketFreshness = 'current' | 'delayed' | 'reconnecting' | 'stale' | 'unavailable';

export interface FreshnessNoticeProps {
  freshness: MarketFreshness;
  stream: StreamState;
  error: string | null;
  onReconnect: () => void;
  className?: string;
}

/** `label` is the short chip text (fits a 390px header row); `title` and `body` are the full sentence for assistive tech and the hover title. */
const copy: Record<Exclude<MarketFreshness, 'current'>, { label: string; title: string; body: string }> = {
  delayed: { label: 'Delayed', title: 'Prices are a little behind', body: 'They catch up in a moment.' },
  reconnecting: { label: 'Reconnecting', title: 'Reconnecting...', body: 'Live updates are paused. You are seeing the latest prices we have.' },
  stale: { label: 'Out of date', title: 'Prices may be out of date', body: 'No new updates for a while.' },
  unavailable: { label: 'Updates off', title: 'Live updates are off', body: `${plainCopy.marketFeed} You are seeing the latest prices we have.` },
};
const problem = { label: 'Updates off', title: 'Live updates hit a problem', body: 'You are seeing the latest prices we have.' };

/**
 * Compact freshness chip for degraded market data, sized to sit inside the token header row: it never
 * spans the page or pushes the columns down. The full explanation is the chip's accessible text and
 * hover title. The transport state and its error code are developer detail: outside production they
 * sit in a data attribute, never in the visible text.
 */
export function FreshnessNotice({ freshness, stream, error, onReconnect, className }: FreshnessNoticeProps) {
  if (freshness === 'current' && !error) return null;
  const danger = freshness === 'unavailable' || stream === 'resync-required';
  const text = freshness === 'current' ? problem : copy[freshness];
  return (
    <div
      role="status"
      data-freshness-chip={freshness}
      title={`${text.title}. ${text.body}`}
      className={cn(
        'inline-flex h-7 max-w-full shrink-0 items-center gap-1 rounded-pill pl-2 pr-0.5 text-xs font-bold',
        danger ? 'bg-danger-soft text-danger' : 'bg-warning-soft text-warning',
        className,
      )}
    >
      <CircleAlert aria-hidden="true" className="size-3.5 shrink-0" />
      <span aria-hidden="true" className="whitespace-nowrap">{text.label}</span>
      <span className="sr-only">{text.title}. {text.body}</span>
      <button
        type="button"
        onClick={onReconnect}
        aria-label="Reconnect"
        className="inline-flex size-6 shrink-0 items-center justify-center rounded-pill text-text-muted transition-colors hover:bg-surface-hover hover:text-text focus-visible:outline-2 focus-visible:outline-focus"
      >
        <RefreshCw aria-hidden="true" className="size-3.5" />
      </button>
    </div>
  );
}

export default FreshnessNotice;
