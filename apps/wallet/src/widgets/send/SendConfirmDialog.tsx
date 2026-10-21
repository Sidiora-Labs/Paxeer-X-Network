'use client';

/**
 * Send confirmation drawer body — token / amount / recipient summary.
 *
 * Wraps the shared {@link ConfirmDrawer} with send-specific copy.
 */

import { ConfirmDrawer } from '@/components/ConfirmDrawer';
import { TokenIcon } from './TokenIcon';
import type { SendableToken } from './useSendableTokens';

export interface SendConfirmDialogProps {
  open: boolean;
  loading: boolean;
  error: string;
  selectedToken: SendableToken | null;
  amount: string;
  to: string;
  onClose: () => void;
  onConfirm: () => void;
}

export function SendConfirmDialog({
  open,
  loading,
  error,
  selectedToken,
  amount,
  to,
  onClose,
  onConfirm,
}: SendConfirmDialogProps) {
  return (
    <ConfirmDrawer
      open={open}
      onClose={onClose}
      onConfirm={onConfirm}
      title="Confirm Send"
      confirmLabel={`Send ${selectedToken?.symbol || ''}`}
      cancelLabel="Back"
      loading={loading}
      error={error}
    >
      <div className="glass-card p-4 space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-xs text-pax-muted">Token</span>
          <div className="flex items-center gap-2">
            {selectedToken && <TokenIcon token={selectedToken} />}
            <span className="text-sm font-medium">{selectedToken?.symbol}</span>
          </div>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-xs text-pax-muted">Amount</span>
          <span className="text-sm font-bold">
            {amount} {selectedToken?.symbol}
          </span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-xs text-pax-muted">Recipient</span>
          <span className="text-[11px] font-mono text-pax-muted truncate max-w-[180px]">
            {to}
          </span>
        </div>
      </div>
    </ConfirmDrawer>
  );
}
