'use client';

/**
 * Confirm-swap drawer body.
 *
 * Shows pay/receive amounts with badges, route metadata, and the minimum
 * received after slippage. Wraps the shared {@link ConfirmDrawer}.
 */

import { ConfirmDrawer } from '@/components/ConfirmDrawer';
import { SvgIcon } from '@/components/ui/SvgIcon';
import type { SwapToken, SwapQuote } from '@/lib/swap';
import { TokenBadge } from './TokenBadge';
import { SwapErrorCard } from './SwapErrorCard';
import { minReceived } from './util';

export interface SwapConfirmDialogProps {
  open: boolean;
  loading: boolean;
  execError: string;
  bestQuote: SwapQuote;
  fromToken: SwapToken;
  toToken: SwapToken;
  fromAmount: string;
  slippageBps: number;
  onClose: () => void;
  onConfirm: () => void;
}

const impactClass = (pct: number): string => {
  if (pct > 3) return 'text-red-400 font-medium';
  if (pct > 1) return 'text-amber-400';
  return '';
};

export function SwapConfirmDialog({
  open,
  loading,
  execError,
  bestQuote,
  fromToken,
  toToken,
  fromAmount,
  slippageBps,
  onClose,
  onConfirm,
}: SwapConfirmDialogProps) {
  return (
    <ConfirmDrawer
      open={open}
      onClose={onClose}
      onConfirm={onConfirm}
      title="Confirm Swap"
      confirmLabel="Confirm Swap"
      cancelLabel="Back"
      loading={loading}
      error={undefined}
    >
      {execError && <SwapErrorCard error={execError} />}
      <div className="glass-card p-4 space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs text-pax-muted mb-1">You pay</p>
            <p className="text-lg font-bold">
              {fromAmount} <span className="text-sm text-pax-muted">{fromToken.symbol}</span>
            </p>
          </div>
          <TokenBadge token={fromToken} />
        </div>
        <div className="flex justify-center">
          <SvgIcon
            name="swap"
            className="w-4 h-4"
            style={{ filter: 'brightness(0) invert(0.6)' }}
          />
        </div>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs text-pax-muted mb-1">You receive</p>
            <p className="text-lg font-bold">
              {bestQuote.amountOutDisplay}{' '}
              <span className="text-sm text-pax-muted">{toToken.symbol}</span>
            </p>
          </div>
          <TokenBadge token={toToken} />
        </div>
      </div>
      <div className="glass-card p-3 space-y-2 text-xs">
        <div className="flex justify-between">
          <span className="text-pax-muted">Route</span>
          <span className="font-medium text-pax-accent">{bestQuote.protocolLabel}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-pax-muted">Fee</span>
          <span>{bestQuote.feeBps / 100}%</span>
        </div>
        <div className="flex justify-between">
          <span className="text-pax-muted">Slippage</span>
          <span>{slippageBps / 100}%</span>
        </div>
        {bestQuote.priceImpact > 0 && (
          <div className="flex justify-between">
            <span className="text-pax-muted">Price Impact</span>
            <span className={impactClass(bestQuote.priceImpact)}>
              {bestQuote.priceImpact.toFixed(2)}%
            </span>
          </div>
        )}
        <div className="flex justify-between">
          <span className="text-pax-muted">Min. received</span>
          <span>
            {minReceived(bestQuote, slippageBps, toToken.decimals)} {toToken.symbol}
          </span>
        </div>
      </div>
    </ConfirmDrawer>
  );
}
