'use client';

import { useId, type ReactNode } from 'react';
import { formatUnits } from 'viem';
import { ArrowDownUp, Check, Share2 } from 'lucide-react';
import { Button, SegmentedControl, Toggle } from '@/ui/primitives';
import { formatTokenAmount, formatUsdl, safeFixed } from '@/utils/format';
import { cn } from '@/lib/utils';
import { plainTradeError } from './trade-copy';

export const BUY_PRESETS = ['25', '100', '250'] as const;
export const SELL_PCTS = [25, 50, 75, 100] as const;

export type TradeSide = 'buy' | 'sell';

const AMOUNT_ERRORS: Record<string, string> = {
  awaiting_asset_metadata: 'Loading...',
  invalid_decimals: "This coin can't be traded right now",
  too_long: 'Amount is too long',
  invalid_decimal: 'Enter a number, like 10 or 2.5',
  excess_precision: 'Too many decimal places',
  uint256_overflow: 'That amount is too big',
  insufficient_balance: 'Not enough balance',
};

export function amountErrorText(code: string, decimals: number): string {
  const base = AMOUNT_ERRORS[code] ?? 'Enter a valid amount';
  return code === 'excess_precision' ? `${base}, use up to ${decimals}` : base;
}

function pow10(n: number): bigint { return 10n ** BigInt(n); }

function decimalToScaled(text: string, scale: number): bigint | null {
  if (!/^\d+(?:\.\d+)?$/.test(text)) return null;
  const [whole, fraction = ''] = text.split('.');
  const digits = fraction.length > scale ? fraction.slice(0, scale) : fraction.padEnd(scale, '0');
  return BigInt(whole + digits);
}

function scaledToDecimal(value: bigint, scale: number): string {
  const text = value.toString().padStart(scale + 1, '0');
  const whole = text.slice(0, text.length - scale) || '0';
  const fraction = scale === 0 ? '' : text.slice(text.length - scale).replace(/0+$/, '');
  return fraction ? `${whole}.${fraction}` : whole;
}

/**
 * Convert between USDL and token amounts using a last-observed price (USDL per token,
 * decimal string). Exact integer arithmetic; floors to `outDecimals`. Returns null when
 * the inputs are not plain decimals or the price is zero.
 */
export function convertByPrice(value: string, price: string, from: 'usdl' | 'token', outDecimals: number): string | null {
  const priceDigits = (price.split('.')[1] ?? '').length;
  const priceScaled = decimalToScaled(price, priceDigits);
  if (priceScaled === null || priceScaled <= 0n) return null;
  const work = 36;
  if (!Number.isInteger(outDecimals) || outDecimals < 0 || outDecimals > work) return null;
  const scaled = decimalToScaled(value, work);
  if (scaled === null) return null;
  const converted = from === 'token' ? (scaled * priceScaled) / pow10(priceDigits) : (scaled * pow10(priceDigits)) / priceScaled;
  return scaledToDecimal(converted / pow10(work - outDecimals), outDecimals);
}

// Buy / Sell header

interface TradeTabsProps {
  side: TradeSide;
  onSwitch: (side: TradeSide) => void;
  trailing?: ReactNode;
}

export function TradeTabs({ side, onSwitch, trailing }: TradeTabsProps) {
  return (
    <div className="flex items-center gap-2">
      <SegmentedControl<TradeSide>
        label="Trade side"
        size="md"
        tone="trade"
        fullWidth
        value={side}
        onChange={onSwitch}
        options={[{ value: 'buy', label: 'Buy' }, { value: 'sell', label: 'Sell' }]}
        className="flex-1"
      />
      {trailing}
    </div>
  );
}

// Amount input

interface AmountInputProps {
  amount: string;
  onChange: (v: string) => void;
  inputDecimals: number;
  inputSymbol?: string;
  errorCode?: string;
  /** Unit shown to the right of the value; defaults to inputSymbol. */
  unitLabel?: string;
  /** When set, renders the unit as a toggle that cycles the entry unit. */
  onCycleUnit?: () => void;
  unitToggleDisabledReason?: string | null;
  /** Secondary line under the value (e.g. the converted estimate). */
  hint?: ReactNode;
  disabled?: boolean;
}

export function AmountInput({ amount, onChange, inputDecimals, inputSymbol = 'USDL', errorCode, unitLabel, onCycleUnit, unitToggleDisabledReason, hint, disabled = false }: AmountInputProps) {
  const inputId = useId();
  const errorId = useId();
  const hintId = useId();
  const invalid = !!errorCode;
  const unit = unitLabel ?? inputSymbol;
  const describedBy = [invalid ? errorId : null, hint ? hintId : null].filter(Boolean).join(' ') || undefined;
  return (
    <div className="space-y-1.5">
      <div className={cn('flex h-control-height-lg items-center gap-2 rounded-sm px-3 transition-colors duration-fast', invalid ? 'bg-danger-soft' : 'bg-surface-2', disabled && 'text-text-disabled')}>
        <label htmlFor={inputId} className="sr-only">Trade amount in {unit}</label>
        <input
          id={inputId}
          type="text"
          inputMode="decimal"
          autoComplete="off"
          aria-invalid={invalid || undefined}
          aria-describedby={describedBy}
          maxLength={80}
          value={amount}
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          placeholder="0"
          className="min-w-0 flex-1 bg-transparent text-lg font-bold text-text tabular-nums placeholder:text-text-faint outline-none focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-focus disabled:text-text-disabled"
        />
        {onCycleUnit ? (
          <button
            type="button"
            onClick={onCycleUnit}
            disabled={!!unitToggleDisabledReason}
            aria-label={`Switch unit, currently ${unit}`}
            title={unitToggleDisabledReason ?? `Switch unit, currently ${unit}`}
            className="inline-flex h-7 shrink-0 items-center gap-1 rounded-pill bg-surface-3 px-2 text-xs font-bold text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text disabled:cursor-not-allowed disabled:text-text-disabled disabled:hover:bg-surface-3"
          >
            <span>{unit}</span>
            <ArrowDownUp aria-hidden="true" className="size-icon-sm" />
          </button>
        ) : (
          <span className="shrink-0 text-xs font-bold text-text-muted">{unit}</span>
        )}
      </div>
      {hint ? <p id={hintId} className="px-1 text-xs text-text-faint tabular-nums">{hint}</p> : null}
      {invalid ? <p id={errorId} role="alert" className="px-1 text-xs text-danger">{amountErrorText(errorCode, inputDecimals)}</p> : null}
    </div>
  );
}

// Presets

/** Longest chip text that fits a quarter of the 1440 right rail at text-sm bold without wrapping. */
const PRESET_TEXT_MAX = 6;
const compactAmount = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 2 });

/**
 * Chip text for a USDL preset amount: the plain amount when short, otherwise a compact form
 * ("1.5K", "2.25M"), so a user preset of up to 12 integer and 6 fraction digits stays on one line.
 * The chip's accessible name and title carry the exact amount.
 */
export function presetAmountText(amount: string): string {
  if (amount.length <= PRESET_TEXT_MAX) return amount;
  const value = Number(amount);
  if (!Number.isFinite(value)) return amount;
  if (value < 0.01) return '<0.01';
  if (value >= 1000) return compactAmount.format(value);
  const fixed = safeFixed(value, Math.max(0, PRESET_TEXT_MAX - 1 - String(Math.trunc(value)).length));
  return fixed.includes('.') ? fixed.replace(/0+$/, '').replace(/\.$/, '') : fixed;
}

interface PresetChipProps {
  active?: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
  label?: string;
  /** Tighter type for the terminal quick-trade card's narrower grid. */
  size?: 'sm' | 'xs';
}

/**
 * Quick-amount chip shared by the coin page Buy panel and the terminal quick-trade card.
 * Single line by construction: no wrapping, no clipping; callers keep the text short (amount only,
 * the unit is shown once by the row or the input adornment).
 */
export function PresetChip({ active = false, disabled = false, onClick, children, label, size = 'sm' }: PresetChipProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={active}
      aria-label={label}
      title={label}
      className={cn(
        'inline-flex h-control-height-sm min-w-0 items-center justify-center whitespace-nowrap rounded-sm font-bold tabular-nums transition-colors duration-fast',
        size === 'sm' ? 'px-2 text-sm' : 'px-1 text-xs',
        active ? 'bg-surface-active text-text' : 'bg-surface-2 text-text-muted hover:bg-surface-hover hover:text-text',
        disabled && 'cursor-not-allowed text-text-disabled hover:bg-surface-2 hover:text-text-disabled',
      )}
    >
      {children}
    </button>
  );
}

interface BuyPresetsProps {
  amount: string;
  onSelect: (preset: string) => void;
  onMax?: () => void;
  maxDisabled?: boolean;
}

/** USDL quick amounts: the unit once at the start of the row, the chips carry the amount only. */
export function BuyPresets({ amount, onSelect, onMax, maxDisabled = false }: BuyPresetsProps) {
  return (
    <div role="group" aria-label="Quick USDL amounts" className="flex items-center gap-2">
      <span aria-hidden="true" className="shrink-0 text-xs font-bold text-text-muted">USDL</span>
      <div className="grid min-w-0 flex-1 grid-cols-4 gap-2">
        {BUY_PRESETS.map((preset) => (
          <PresetChip key={preset} active={amount === preset} onClick={() => onSelect(preset)} label={`${preset} USDL`}>
            {presetAmountText(preset)}
          </PresetChip>
        ))}
        <PresetChip onClick={() => onMax?.()} disabled={!onMax || maxDisabled} label="Use full USDL balance">
          Max
        </PresetChip>
      </div>
    </div>
  );
}

interface SellPctsProps {
  tokenBalance: bigint | null;
  tokenDecimals: number;
  amount: string;
  onSelect: (pct: number) => void;
}

export function SellPcts({ tokenBalance, tokenDecimals, amount, onSelect }: SellPctsProps) {
  const disabled = tokenBalance === null || tokenBalance === 0n;
  return (
    <div role="group" aria-label="Portion of holdings to sell" className="grid grid-cols-4 gap-2">
      {SELL_PCTS.map((pct) => {
        const portion = tokenBalance !== null ? (tokenBalance * BigInt(pct)) / 100n : 0n;
        const isActive = !disabled && amount !== '' && amount === formatUnits(portion, tokenDecimals);
        return (
          <PresetChip key={pct} active={isActive} disabled={disabled} onClick={() => onSelect(pct)} label={`Sell ${pct} percent of holdings`}>
            {pct}%
          </PresetChip>
        );
      })}
    </div>
  );
}

// Balance row

interface BalanceRowProps {
  side: TradeSide;
  usdlBalance: bigint | null;
  tokenBalance: bigint | null;
  tokenDecimals: number;
  tokenSymbol: string;
  /** Reason the latest live chain read failed; the only case the row is marked stale. */
  error?: string;
  onMax: () => void;
}

/** Live balance of the signed-in wallet. Rendered only for an authenticated session (see trade-panel). */
export function BalanceRow({ side, usdlBalance, tokenBalance, tokenDecimals, tokenSymbol, error, onMax }: BalanceRowProps) {
  const held = side === 'buy' ? usdlBalance : tokenBalance;
  const value = held === null
    ? (error ? '—' : 'Loading...')
    : side === 'buy'
      ? formatUsdl(held, { fractionDigits: 2 })
      : `${formatTokenAmount(held, tokenDecimals, { compact: false, maxFraction: 6 })} ${tokenSymbol}`;
  return (
    <div className="flex items-center justify-between gap-2 text-xs">
      <div className="flex min-w-0 flex-wrap items-center gap-x-1.5">
        <span className="text-text-muted">Balance</span>
        <span data-balance-value="" className="font-bold text-text tabular-nums">{value}</span>
        {error && held !== null ? <span data-balance-stale="" className="text-warning">may be out of date</span> : null}
      </div>
      <Button variant="link" size="sm" onClick={onMax} disabled={held === null}>Max</Button>
    </div>
  );
}

// Two-step indicator

interface TradeStepsProps {
  side: TradeSide;
  assetSymbol: string;
  needsApproval: boolean;
}

export function TradeSteps({ side, assetSymbol, needsApproval }: TradeStepsProps) {
  const steps = [`Approve ${assetSymbol}`, side === 'buy' ? 'Buy' : 'Sell'];
  const activeIndex = needsApproval ? 0 : 1;
  return (
    <ol aria-label="Trade steps" className="grid grid-cols-2 gap-2 text-xs">
      {steps.map((step, index) => {
        const done = index < activeIndex;
        const active = index === activeIndex;
        return (
          <li
            key={step}
            aria-current={active ? 'step' : undefined}
            className={cn('flex items-center gap-1.5 rounded-sm px-2 py-1.5', active ? 'bg-accent-soft text-accent' : 'bg-surface-2 text-text-muted')}
          >
            <span className={cn('inline-flex size-4 shrink-0 items-center justify-center rounded-pill text-2xs font-bold', active ? 'bg-accent text-on-accent' : done ? 'bg-success-soft text-success' : 'bg-surface-3 text-text-muted')}>
              {done ? <Check aria-hidden="true" className="size-3" /> : index + 1}
            </span>
            <span className="truncate font-bold">{step}</span>
          </li>
        );
      })}
    </ol>
  );
}

// Quote summary

interface TradePreviewProps {
  side: TradeSide;
  tokenSymbol: string;
  tokenDecimals: number;
  outputRaw: bigint | null;
  minOutRaw: bigint | null;
  impactBps: number | null;
  quoteAge: number | null;
  loading: boolean;
}

function impactTone(bps: number): string {
  if (bps > 1000) return 'text-danger';
  if (bps > 300) return 'text-warning';
  return 'text-success';
}

export function TradePreview({ side, tokenSymbol, tokenDecimals, outputRaw, minOutRaw, impactBps, quoteAge, loading }: TradePreviewProps) {
  const fmt = (raw: bigint | null) => raw === null ? null : side === 'buy' ? `${formatTokenAmount(raw, tokenDecimals, { compact: false, maxFraction: 6 })} ${tokenSymbol}` : formatUsdl(raw, { fractionDigits: 4 });
  const pending = <span className="inline-block h-3 w-16 rounded-xs bg-skeleton" aria-hidden="true" />;
  const rows: Array<{ label: string; value: ReactNode; className?: string }> = [
    { label: 'You receive', value: fmt(outputRaw) ?? (loading ? pending : '—') },
    { label: 'Minimum received', value: fmt(minOutRaw) ?? (loading ? pending : '—') },
    { label: 'Price impact', value: impactBps === null ? (loading ? pending : '—') : `${safeFixed(impactBps / 100, 2)}%`, className: impactBps === null ? undefined : impactTone(impactBps) },
  ];
  return (
    <dl aria-live="polite" aria-busy={loading || undefined} className="space-y-1.5 rounded-sm bg-surface-2 p-2.5 text-xs">
      {rows.map(row => (
        <div key={row.label} className="flex items-center justify-between gap-3">
          <dt className="text-text-muted">{row.label}</dt>
          <dd className={cn('text-right font-bold text-text tabular-nums', row.className)}>{row.value}</dd>
        </div>
      ))}
      {quoteAge !== null ? (
        <div className="flex items-center justify-between gap-3">
          <dt className="text-text-faint">Price updated</dt>
          <dd className="text-text-faint tabular-nums">{quoteAge}s ago</dd>
        </div>
      ) : null}
    </dl>
  );
}

// Approval amount choice

interface ApprovalCheckboxProps {
  unlimited: boolean;
  onChange: (v: boolean) => void;
  assetSymbol: string;
}

export function ApprovalCheckbox({ unlimited, onChange, assetSymbol }: ApprovalCheckboxProps) {
  const labelId = useId();
  const helpId = useId();
  return (
    <div className="flex items-start justify-between gap-3 rounded-sm bg-surface-2 p-2.5">
      <div className="min-w-0 space-y-0.5">
        <p id={labelId} className="text-xs font-bold text-text">Approve unlimited {assetSymbol}</p>
        <p id={helpId} className="text-2xs text-text-faint">Skip the approval step on future trades. Approving only this amount is safer.</p>
      </div>
      <Toggle size="sm" checked={unlimited} onChange={onChange} labelledBy={labelId} describedBy={helpId} />
    </div>
  );
}

// Primary action

export type TradeActionState =
  | { kind: 'connect' }
  | { kind: 'sign-in' }
  | { kind: 'switch-network'; busy: boolean }
  | { kind: 'deployment-unavailable' }
  | { kind: 'enter-amount' }
  | { kind: 'insufficient'; assetSymbol: string }
  | { kind: 'approving'; assetSymbol: string }
  | { kind: 'confirming' }
  | { kind: 'approve'; assetSymbol: string }
  | { kind: 'quote-pending' }
  | { kind: 'ready'; side: TradeSide; tokenSymbol: string };

interface ActionButtonProps {
  state: TradeActionState;
  side: TradeSide;
  onClick: () => void;
}

export function actionLabel(state: TradeActionState): string {
  switch (state.kind) {
    case 'connect': return 'Connect wallet to trade';
    case 'sign-in': return 'Sign in to trade';
    case 'switch-network': return state.busy ? 'Switching...' : 'Switch to Paxeer';
    case 'deployment-unavailable': return "Trading isn't available yet";
    case 'enter-amount': return 'Enter an amount';
    case 'insufficient': return `Not enough ${state.assetSymbol}`;
    case 'approving': return 'Approving...';
    case 'confirming': return 'Confirming...';
    case 'approve': return `Approve ${state.assetSymbol}`;
    case 'quote-pending': return 'Getting price...';
    case 'ready': return 'Place trade';
  }
}

export function ActionButton({ state, side, onClick }: ActionButtonProps) {
  const label = actionLabel(state);
  const loading = state.kind === 'approving' || state.kind === 'confirming' || (state.kind === 'switch-network' && state.busy);
  const disabled = state.kind === 'deployment-unavailable' || state.kind === 'enter-amount' || state.kind === 'insufficient' || state.kind === 'quote-pending' || loading;
  if (state.kind === 'insufficient') {
    return (
      <button type="button" disabled aria-disabled="true" className="h-control-height-lg w-full cursor-not-allowed rounded-sm bg-danger-soft px-5 text-base font-bold text-danger">
        {label}
      </button>
    );
  }
  const variant = state.kind === 'ready' && side === 'sell' ? 'danger' : 'primary';
  return (
    <Button variant={variant} size="lg" fullWidth loading={loading} disabled={disabled} onClick={onClick}>
      {label}
    </Button>
  );
}

// Footer

interface FeeFooterProps {
  feePercent: number | null;
  slippageBps: number;
  feeComponentsRaw: string | null;
}

export function FeeFooter({ feePercent, slippageBps, feeComponentsRaw }: FeeFooterProps) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-2xs text-text-faint">
      <span className="tabular-nums">Slippage {safeFixed(slippageBps / 100, slippageBps % 100 === 0 ? 1 : 2)}%</span>
      <span className="tabular-nums">{feePercent !== null ? `Fee ${feePercent}%` : feeComponentsRaw ? 'Fee included in price' : 'Fee included in price'}</span>
    </div>
  );
}

// Share PnL

interface SharePnlButtonProps {
  tokenName: string;
  state: 'idle' | 'minting' | 'ready' | 'error';
  errorMessage?: string;
  onClick: () => void;
}

export function SharePnlButton({ tokenName, state, errorMessage, onClick }: SharePnlButtonProps) {
  return (
    <div className="space-y-1">
      <Button
        variant="secondary"
        size="sm"
        fullWidth
        loading={state === 'minting'}
        disabled={state === 'minting'}
        icon={<Share2 />}
        onClick={onClick}
        title="Make a card to share your profit or loss"
      >
        {state === 'minting' ? 'Creating card...' : `Share my ${tokenName} PnL`}
      </Button>
      {state === 'error' && errorMessage ? <p role="alert" className="text-xs text-danger">{plainTradeError(errorMessage, "Couldn't create your card, try again")}</p> : null}
    </div>
  );
}
