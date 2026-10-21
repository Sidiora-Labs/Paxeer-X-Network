'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { Check, Share2, X } from 'lucide-react';
import { Button, IconButton } from '@/ui/primitives';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { cn } from '@/lib/utils';
import { emitBrowserObservation } from './transaction-controls/measurement';

export interface TradeToastData {
  type: 'buy' | 'sell';
  inputAmount: string;
  inputSymbol: string;
  outputAmount: string;
  outputSymbol: string;
  newUsdlBalance: string | null;
  newTokenBalance: string | null;
  tokenName: string;
  txHash: string;
  feeRaw?: string;
  receiptHeight?: string;
  historyUpdating?: boolean;
  balanceObservedAt?: number;
}

interface TradeToastProps {
  data: TradeToastData;
  onDismiss: () => void;
  /** If provided, renders a Share PnL button at the bottom of the toast. */
  onShare?: () => void;
  /** When true, auto-dismiss and the progress line pause (mint in flight, modal imminent). */
  sharing?: boolean;
  /** Optional error text (for example from a failed mint) shown under the share button. */
  shareError?: string | null;
}

const AUTO_DISMISS_MS = 6000;

export default function TradeToast({ data, onDismiss, onShare, sharing = false, shareError }: TradeToastProps) {
  const [visible, setVisible] = useState(false);
  const [exiting, setExiting] = useState(false);
  const [paused, setPaused] = useState(false);

  const dismiss = useCallback(() => {
    setExiting(true);
    setTimeout(onDismiss, 300);
  }, [onDismiss]);

  useEffect(() => {
    const frame = requestAnimationFrame(() => setVisible(true));
    return () => cancelAnimationFrame(frame);
  }, []);

  const hold = sharing || !!shareError || paused;
  useEffect(() => {
    if (hold) return;
    const timer = setTimeout(() => dismiss(), AUTO_DISMISS_MS);
    return () => clearTimeout(timer);
  }, [dismiss, hold]);

  const observedToast = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (observedToast.current === data.txHash) return;
    observedToast.current = data.txHash;
    emitBrowserObservation({ kind: 'toast_confirmed', txHash: data.txHash, receiptHeight: data.receiptHeight });
  }, [data.txHash, data.receiptHeight]);

  const isBuy = data.type === 'buy';
  const truncatedHash = data.txHash ? `${data.txHash.slice(0, 6)}...${data.txHash.slice(-4)}` : '';
  const explorerHref = data.txHash ? safeExternalDestination(`https://paxscan.paxeer.app/tx/${data.txHash}`) : null;
  const tone = isBuy ? 'text-accent' : 'text-danger';

  return (
    <div
      role="status"
      aria-live="polite"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
      className={cn(
        'fixed bottom-24 left-3 right-3 z-[9999] w-auto transition-all duration-slow ease-out sm:bottom-5 sm:left-auto sm:right-5 sm:w-[360px]',
        visible && !exiting ? 'translate-y-0 opacity-100' : 'translate-y-4 opacity-0',
      )}
    >
      <div className="overflow-hidden rounded-md bg-surface-3 shadow-pop">
        <div className={cn('flex items-center gap-2 px-4 py-3', isBuy ? 'bg-accent-soft' : 'bg-danger-soft')}>
          <span className={cn('inline-flex size-5 items-center justify-center rounded-pill', isBuy ? 'bg-accent text-on-accent' : 'bg-danger text-on-danger')}>
            <Check aria-hidden="true" className="size-3" />
          </span>
          <span className={cn('text-sm font-bold', tone)}>{isBuy ? 'Bought!' : 'Sold!'}</span>
          <IconButton label="Dismiss notification" icon={<X />} variant="ghost" size="sm" className="ml-auto" onClick={dismiss} />
        </div>

        <div className="space-y-3 px-4 py-3">
          <dl className="space-y-1.5 text-sm">
            <div className="flex items-center justify-between gap-3">
              <dt className="text-text-muted">{isBuy ? 'Spent' : 'Sold'}</dt>
              <dd className="font-bold text-text tabular-nums">{data.inputAmount} {data.inputSymbol}</dd>
            </div>
            <div className="flex items-center justify-between gap-3">
              <dt className="text-text-muted">{isBuy ? 'Received' : 'Returned'}</dt>
              <dd className={cn('font-bold tabular-nums', tone)}>{data.outputAmount} {data.outputSymbol}</dd>
            </div>
            {data.feeRaw ? (
              <div className="flex items-center justify-between gap-3 text-xs">
                <dt className="text-text-faint">Fee paid</dt>
                <dd className="text-text-muted tabular-nums">{data.feeRaw} raw</dd>
              </div>
            ) : null}
          </dl>

          {data.historyUpdating ? (
            <p className="text-xs text-text-muted tabular-nums">Updating your trades...</p>
          ) : null}

          <div className="space-y-1 rounded-sm bg-surface-2 p-2.5">
            <p className="text-2xs uppercase tracking-wider text-text-faint">
              {data.balanceObservedAt ? `Balance as of ${new Date(data.balanceObservedAt).toLocaleTimeString()}` : 'Updating balance...'}
            </p>
            <div className="flex items-center justify-between gap-3 text-xs">
              <span className="text-text-muted">USDL</span>
              <span className="font-bold text-text tabular-nums">{data.newUsdlBalance ?? '...'}</span>
            </div>
            <div className="flex items-center justify-between gap-3 text-xs">
              <span className="text-text-muted">{data.tokenName || 'Coin'}</span>
              <span className="font-bold text-text tabular-nums">{data.newTokenBalance ?? '...'}</span>
            </div>
          </div>

          {truncatedHash ? (
            <div className="flex items-center justify-between gap-3 text-xs">
              <span className="text-text-faint">Transaction</span>
              {explorerHref ? (
                <a className="font-mono-sl text-text-muted hover:text-text hover:underline" target="_blank" rel="noreferrer" href={explorerHref}>{truncatedHash}</a>
              ) : (
                <span className="font-mono-sl text-text-muted">{truncatedHash}</span>
              )}
            </div>
          ) : null}

          {onShare ? (
            <div className="space-y-1">
              <Button variant="primary" size="sm" fullWidth loading={sharing} disabled={sharing} icon={<Share2 />} onClick={onShare}>
                {sharing ? 'Creating card...' : 'Share PnL'}
              </Button>
              {shareError ? <p role="alert" className="text-center text-xs text-danger">{shareError}</p> : null}
            </div>
          ) : null}
        </div>

        <div className="h-[2px] bg-track" aria-hidden="true">
          <div
            className={isBuy ? 'h-full bg-accent' : 'h-full bg-danger'}
            style={{ animation: `toast-progress ${AUTO_DISMISS_MS}ms linear forwards`, animationPlayState: hold ? 'paused' : 'running' }}
          />
        </div>
      </div>
    </div>
  );
}
