'use client';

import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { Settings2 } from 'lucide-react';
import { Button, IconButton, SegmentedControl } from '@/ui/primitives';
import { safeFixed, shortAddress } from '@/utils/format';
import { parseAmount } from '@/widgets/trading/transaction-controls/domain';
import { cn } from '@/lib/utils';

export const SLIPPAGE_PRESETS = [50, 100, 300] as const;
export const MAX_SLIPPAGE_BPS = 5000;
export const HIGH_SLIPPAGE_BPS = 500;

type PresetValue = '50' | '100' | '300' | 'custom';

export interface TradeSettingsPopoverProps {
  slippageBps: number;
  onSlippageChange: (bps: number) => void;
  /** Execution deadline enforced by the reviewed router flow, in seconds after review. */
  deadlineSeconds?: number;
  /** Verified spender the approval targets. */
  spender?: string | null;
  size?: 'sm' | 'md';
  className?: string;
}

export function formatSlippage(bps: number): string {
  return `${safeFixed(bps / 100, bps % 100 === 0 ? 1 : 2)}%`;
}

export function TradeSettingsPopover({ slippageBps, onSlippageChange, deadlineSeconds, spender, size = 'sm', className }: TradeSettingsPopoverProps) {
  const [open, setOpen] = useState(false);
  const [custom, setCustom] = useState('');
  const [customError, setCustomError] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const titleId = useId();
  const customId = useId();
  const customErrorId = useId();

  const preset: PresetValue = SLIPPAGE_PRESETS.includes(slippageBps as (typeof SLIPPAGE_PRESETS)[number]) ? (String(slippageBps) as PresetValue) : 'custom';
  const highSlippage = slippageBps > HIGH_SLIPPAGE_BPS;

  useEffect(() => {
    if (!open) return;
    opener.current = document.activeElement as HTMLElement | null;
    panel.current?.querySelector<HTMLElement>('[data-autofocus]')?.focus();
    const onPointer = (event: PointerEvent) => {
      if (root.current && !root.current.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', onPointer);
    return () => {
      document.removeEventListener('pointerdown', onPointer);
      opener.current?.focus();
    };
  }, [open]);

  useEffect(() => {
    if (!open) { setCustom(''); setCustomError(null); }
  }, [open]);

  const applyCustom = () => {
    const parsed = parseAmount(custom.trim(), 2);
    if (parsed.errorCode || parsed.parsedRaw === undefined || parsed.parsedRaw < 1n || parsed.parsedRaw > BigInt(MAX_SLIPPAGE_BPS)) {
      setCustomError('Enter a slippage between 0.01% and 50%');
      return;
    }
    setCustomError(null);
    onSlippageChange(Number(parsed.parsedRaw));
    setCustom('');
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') { event.preventDefault(); setOpen(false); return; }
    if (event.key !== 'Tab') return;
    const controls = Array.from(panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled)') ?? []);
    const first = controls[0];
    const last = controls[controls.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  };

  return (
    <div ref={root} className={cn('relative', className)}>
      <IconButton
        label={`Trade settings, slippage ${formatSlippage(slippageBps)}`}
        icon={<Settings2 />}
        variant="raised"
        size={size}
        pressed={open}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => setOpen(value => !value)}
      />
      {open ? (
        <div
          ref={panel}
          role="dialog"
          aria-labelledby={titleId}
          onKeyDown={onKeyDown}
          className="absolute right-0 top-full z-30 mt-2 w-[280px] max-w-[calc(100vw-32px)] rounded-md bg-surface-3 p-3 shadow-pop"
        >
          <div className="flex items-center justify-between">
            <h3 id={titleId} className="text-sm font-bold text-text">Settings</h3>
            <span className="text-xs text-text-muted tabular-nums">{formatSlippage(slippageBps)}</span>
          </div>

          <div className="mt-3 space-y-2">
            <p className="text-xs text-text-muted">Max slippage</p>
            <SegmentedControl<PresetValue>
              label="Slippage preset"
              size="sm"
              shape="sm"
              fullWidth
              value={preset}
              onChange={value => { if (value !== 'custom') onSlippageChange(Number(value)); }}
              options={[
                { value: '50', label: '0.5%' },
                { value: '100', label: '1%' },
                { value: '300', label: '3%' },
                { value: 'custom', label: preset === 'custom' ? formatSlippage(slippageBps) : 'Custom', disabled: preset !== 'custom' },
              ]}
            />
            <div className="flex items-center gap-2">
              <label htmlFor={customId} className="sr-only">Custom slippage percent</label>
              <input
                id={customId}
                data-autofocus
                type="text"
                inputMode="decimal"
                maxLength={80}
                value={custom}
                aria-invalid={customError ? true : undefined}
                aria-describedby={customError ? customErrorId : undefined}
                onChange={event => { setCustom(event.target.value); if (customError) setCustomError(null); }}
                onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); applyCustom(); } }}
                placeholder="Custom %"
                className={cn('h-control-height-sm min-w-0 flex-1 rounded-sm px-2 text-sm text-text tabular-nums placeholder:text-text-faint outline-none focus-visible:outline-2 focus-visible:outline-focus', customError ? 'bg-danger-soft' : 'bg-surface-2')}
              />
              <Button size="sm" variant="secondary" onClick={applyCustom} disabled={custom.trim() === ''}>Apply</Button>
            </div>
            {customError ? <p id={customErrorId} role="alert" className="text-xs text-danger">{customError}</p> : null}
            {highSlippage ? <p role="status" className="rounded-sm bg-warning-soft px-2 py-1.5 text-xs text-warning">High slippage. You&apos;ll be asked to confirm before each trade.</p> : null}
          </div>

          <dl className="mt-3 space-y-1.5 rounded-sm bg-surface-2 p-2 text-xs">
            {deadlineSeconds !== undefined ? (
              <div className="flex items-center justify-between gap-3">
                <dt className="text-text-muted">Trade expires</dt>
                <dd className="text-text tabular-nums">{deadlineSeconds}s after you confirm</dd>
              </div>
            ) : null}
            <div className="flex items-center justify-between gap-3">
              <dt className="text-text-muted">Network fee</dt>
              <dd className="text-text">Set in your wallet</dd>
            </div>
            {spender ? (
              <div className="flex items-center justify-between gap-3">
                <dt className="text-text-muted">Approved contract</dt>
                <dd className="font-mono-sl text-text" title={spender}>{shortAddress(spender)}</dd>
              </div>
            ) : null}
          </dl>
          <p className="mt-2 text-2xs text-text-faint">Saved on this device.</p>
        </div>
      ) : null}
    </div>
  );
}
