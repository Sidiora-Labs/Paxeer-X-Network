'use client';

/**
 * Send-form input fields: token selector, recipient, amount, percentage.
 *
 * Stateless — all values flow in/out via props so the orchestrator owns the
 * form state machine.
 */

import { ScanLine } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { TokenIcon } from './TokenIcon';
import type { SendableToken } from './useSendableTokens';
import type { Contact } from '@/lib/contacts';

const ACCENT_FILTER =
  'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)';

export interface SendFormProps {
  selectedToken: SendableToken | null;
  to: string;
  amount: string;
  resolvedContact?: Contact;
  hasContacts: boolean;
  onOpenTokenSelector: () => void;
  onOpenContactPicker: () => void;
  onOpenScanner: () => void;
  onChangeTo: (value: string) => void;
  onChangeAmount: (value: string) => void;
  onApplyPercentage: (pct: number) => void;
}

const PERCENTAGES = [25, 50, 75, 100] as const;

export function SendForm({
  selectedToken,
  to,
  amount,
  resolvedContact,
  hasContacts,
  onOpenTokenSelector,
  onOpenContactPicker,
  onOpenScanner,
  onChangeTo,
  onChangeAmount,
  onApplyPercentage,
}: SendFormProps) {
  return (
    <div className="flex-1 flex flex-col gap-4">
      {/* Token Selector */}
      <div>
        <label className="text-xs text-pax-muted mb-1.5 block">Token</label>
        <button
          onClick={onOpenTokenSelector}
          className="w-full flex items-center gap-3 px-4 py-3.5 rounded-xl bg-white/5 press-scale transition-colors hover:bg-white/8"
        >
          {selectedToken ? (
            <>
              <TokenIcon token={selectedToken} />
              <div className="flex-1 text-left min-w-0">
                <p className="text-sm font-medium">{selectedToken.symbol}</p>
                <p className="text-[11px] text-pax-muted truncate">{selectedToken.name}</p>
              </div>
              <div className="text-right mr-1">
                <p className="text-xs text-pax-muted">{selectedToken.balance}</p>
              </div>
              <SvgIcon
                name="chevron-down"
                className="w-4 h-4"
                style={{ filter: 'brightness(0) invert(0.6)' }}
              />
            </>
          ) : (
            <span className="text-sm text-white/20">Select token...</span>
          )}
        </button>
      </div>

      {/* Recipient */}
      <div>
        <div className="flex items-center justify-between mb-1.5">
          <label className="text-xs text-pax-muted">Recipient Address</label>
          <div className="flex items-center gap-2">
            <button
              onClick={onOpenScanner}
              className="flex items-center gap-1 text-[11px] text-pax-accent press-scale"
            >
              <ScanLine className="w-3 h-3" />
              Scan QR
            </button>
            {hasContacts && (
              <button
                onClick={onOpenContactPicker}
                className="flex items-center gap-1 text-[11px] text-pax-accent press-scale"
              >
                <SvgIcon name="user" className="w-3 h-3" style={{ filter: ACCENT_FILTER }} />
                Contacts
              </button>
            )}
          </div>
        </div>
        <input
          type="text"
          value={to}
          onChange={(e) => onChangeTo(e.target.value)}
          placeholder="0x..."
          className="w-full px-4 py-3.5 rounded-xl bg-white/5   text-sm outline-none  transition-colors placeholder:text-white/20"
        />
        {resolvedContact && (
          <p className="text-[11px] text-pax-accent mt-1 px-1">
            Sending to: {resolvedContact.name}
          </p>
        )}
      </div>

      {/* Amount */}
      <div>
        <label className="text-xs text-pax-muted mb-1.5 block">Amount</label>
        <div className="relative">
          <input
            type="text"
            inputMode="decimal"
            value={amount}
            onChange={(e) => onChangeAmount(e.target.value)}
            placeholder="0.00"
            className="w-full px-4 py-3.5 pr-16 rounded-xl bg-white/5   text-sm outline-none  transition-colors placeholder:text-white/20"
          />
          <span className="absolute right-4 top-1/2 -translate-y-1/2 text-xs text-pax-muted font-medium">
            {selectedToken?.symbol || 'PAX'}
          </span>
        </div>
        <div className="flex gap-2 mt-2">
          {PERCENTAGES.map((pct) => (
            <button
              key={pct}
              onClick={() => onApplyPercentage(pct)}
              className="flex-1 py-1.5 rounded-lg bg-white/5 text-[11px] font-medium text-pax-muted hover:bg-white/10 hover:text-white transition-all press-scale"
            >
              {pct === 100 ? 'MAX' : `${pct}%`}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
