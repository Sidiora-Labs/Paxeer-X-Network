'use client';

/**
 * Bottom-sheet token picker.
 *
 * Shows a spinner while the underlying tokens query is pending, an empty
 * placeholder if there's nothing to send, otherwise a tappable list with the
 * current selection highlighted.
 */

import { SvgIcon } from '@/components/ui/SvgIcon';
import { TokenIcon } from './TokenIcon';
import type { SendableToken } from './useSendableTokens';
import { Loader2 } from 'lucide-react';

const ACCENT_FILTER =
  'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)';

export interface TokenSelectorSheetProps {
  open: boolean;
  onClose: () => void;
  tokens: SendableToken[];
  loading: boolean;
  selected: SendableToken | null;
  onSelect: (token: SendableToken) => void;
}

const isSameToken = (a: SendableToken | null, b: SendableToken): boolean =>
  a !== null && a.symbol === b.symbol && a.address === b.address;

export function TokenSelectorSheet({
  open,
  onClose,
  tokens,
  loading,
  selected,
  onSelect,
}: TokenSelectorSheetProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center">
      <div
        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
        onClick={onClose}
      />
      <div className="relative w-full max-w-md bg-pax-card rounded-t-3xl p-5 pb-8 animate-slide-up">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-base font-bold">Select Token</h3>
          <button onClick={onClose} className="p-1.5 rounded-full bg-white/5 press-scale">
            <SvgIcon name="x" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.6)' }} />
          </button>
        </div>
        <div className="space-y-1 max-h-72 overflow-y-auto">
          {loading ? (
            <div className="py-8 text-center">
              <Loader2
                aria-label="Loading tokens"
                className="mx-auto mb-2 h-5 w-5 animate-spin text-pax-accent"
              />
              <p className="text-xs text-pax-muted">Loading tokens...</p>
            </div>
          ) : tokens.length === 0 ? (
            <p className="text-xs text-pax-muted text-center py-8">No tokens found</p>
          ) : (
            tokens.map((t) => {
              const sel = isSameToken(selected, t);
              return (
                <button
                  key={t.address || 'native'}
                  onClick={() => {
                    onSelect(t);
                    onClose();
                  }}
                  className={`w-full flex items-center gap-3 px-3 py-3 rounded-xl transition-all press-scale ${
                    sel ? 'bg-pax-accent/10' : 'bg-white/5 hover:bg-white/8'
                  }`}
                >
                  <TokenIcon token={t} />
                  <div className="flex-1 text-left min-w-0">
                    <p className="text-sm font-medium">{t.symbol}</p>
                    <p className="text-[11px] text-pax-muted truncate">{t.name}</p>
                  </div>
                  <div className="text-right shrink-0">
                    <p className="text-xs font-medium">{t.balance}</p>
                  </div>
                  {sel && (
                    <SvgIcon
                      name="check"
                      className="w-4 h-4"
                      style={{ filter: ACCENT_FILTER }}
                    />
                  )}
                </button>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
}
