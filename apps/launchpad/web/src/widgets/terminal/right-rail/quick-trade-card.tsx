'use client';
import { useEffect, useRef, useState } from 'react';
import { Zap } from 'lucide-react';
import { formatUnits } from 'viem';
import TradeToast from '@/widgets/trading/trade-toast';
import { TradeRiskReview } from '@/widgets/trade/slippage-selector';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { openAuthModal } from '@/shell/auth-modal-store';
import { Button, NoticeBar } from '@/ui/primitives';
import { quoteFingerprint } from '@/widgets/trading/transaction-controls/domain';
import { TransactionStatus } from '@/widgets/trading/transaction-controls/transaction-status';
import { TradeSettingsPopover } from '@/widgets/trading/trade-settings-popover';
import { useTradePanel } from '@/widgets/trading/use-trade-panel';
import { plainTradeError } from '@/widgets/trading/trade-copy';
import { ActionButton, actionLabel, AmountInput, BalanceRow, FeeFooter, PresetChip, TradePreview, TradeTabs, ApprovalCheckbox, presetAmountText, type TradeActionState, type TradeSide } from '@/widgets/trading/trade-panel-parts';
import type { TradePreset } from '../presets-store';
import { instantTradeEvent, readInstantTrade } from '../activity-tabs/instant-trade';

const EXECUTION_DEADLINE_SECONDS = 120;
const ARM_TIMEOUT_MS = 15_000;
/** Follows the `sidiora:terminal-instant-trade` window event ({ enabled }) dispatched by the activity tabs. */
export function useInstantTrade(): boolean {
  const [enabled, setEnabled] = useState(false);
  useEffect(() => {
    setEnabled(readInstantTrade());
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<unknown>).detail;
      if (typeof detail === 'object' && detail !== null && typeof (detail as { enabled?: unknown }).enabled === 'boolean') setEnabled((detail as { enabled: boolean }).enabled);
    };
    window.addEventListener(instantTradeEvent, listener);
    return () => window.removeEventListener(instantTradeEvent, listener);
  }, []);
  return enabled;
}

/** Terminal quick button: the shared single-line preset chip at the card's tighter xs size. */
function QuickButton({ active, disabled, onClick, label, children }: { active: boolean; disabled?: boolean; onClick: () => void; label: string; children: string }) {
  return <PresetChip active={active} disabled={disabled} onClick={onClick} label={label} size="xs">{children}</PresetChip>;
}

interface Armed { side: TradeSide; amount: string; at: number }

/**
 * The terminal's buy / sell card. Every transaction step runs through
 * useTradePanel (the coin page TradePanel's own hook: approval, quote,
 * risk review, journey); this card only swaps the quick-amount row for the
 * active preset's values. With instant trade on, a quick button arms one
 * submission that fires through the same handleTrade call as the button
 * once the quote is executable; approvals and risk reviews still stop for
 * the user.
 */
export function QuickTradeCard({ poolAddress, preset, onSlippageChange }: { poolAddress: string; preset: TradePreset; onSlippageChange: (bps: number) => void }) {
  const t = useTradePanel({ poolAddress });
  const snapshot = usePoolSnapshot(poolAddress);
  const instant = useInstantTrade();
  const side: TradeSide = t.isBuy ? 'buy' : 'sell';
  const tokenSymbol = snapshot.data?.token?.symbol?.trim() || t.tokenName;
  const spendSymbol = t.isBuy ? 'USDL' : tokenSymbol;
  const [armed, setArmed] = useState<Armed | null>(null);
  const [instantNote, setInstantNote] = useState<string | null>(null);

  const { setSlippageBps } = t;
  useEffect(() => { setSlippageBps(preset.slippageBps); }, [preset.slippageBps, setSlippageBps]);

  const action: TradeActionState = !t.walletConnected
    ? { kind: 'connect' }
    : t.wrongNetwork
      ? { kind: 'switch-network', busy: t.switching }
      : !t.signedIn
        ? { kind: 'sign-in' }
        : t.deploymentUnavailable
          ? { kind: 'deployment-unavailable' }
          : t.insufficientBalance
            ? { kind: 'insufficient', assetSymbol: spendSymbol }
            : !t.hasAmount
              ? { kind: 'enter-amount' }
              : t.approveConfirming
                ? { kind: 'approving', assetSymbol: spendSymbol }
                : t.isPending
                  ? { kind: 'confirming' }
                  : t.needsApproval
                    ? { kind: 'approve', assetSymbol: spendSymbol }
                    : t.quoteUnavailable
                      ? { kind: 'quote-pending' }
                      : { kind: 'ready', side, tokenSymbol };

  const onAction = () => {
    if (action.kind === 'connect' || action.kind === 'sign-in') { openAuthModal('trade'); return; }
    if (action.kind === 'switch-network') { void t.switchToPaxeer(); return; }
    void t.handleTrade();
  };

  const handleTrade = t.handleTrade;
  const fired = useRef<string | null>(null);
  useEffect(() => {
    if (!armed) return;
    if (armed.side !== side || t.amount !== armed.amount) { setArmed(null); return; }
    if (action.kind === 'ready') {
      const key = `${armed.at}`;
      if (fired.current === key) return;
      fired.current = key;
      setArmed(null);
      setInstantNote(null);
      void handleTrade();
      return;
    }
    if (action.kind === 'quote-pending' || action.kind === 'enter-amount') return;
    setArmed(null);
    setInstantNote(action.kind === 'approve' ? `Instant trade paused: approve ${spendSymbol} first, then tap the button again.` : `Instant trade paused: ${actionLabel(action).toLowerCase()}.`);
  }, [armed, side, t.amount, action.kind, handleTrade, spendSymbol]);
  useEffect(() => {
    if (!armed) return;
    const timer = window.setTimeout(() => { setArmed(null); setInstantNote('Instant trade cancelled: no price within 15 seconds. Try again.'); }, ARM_TIMEOUT_MS);
    return () => window.clearTimeout(timer);
  }, [armed]);
  useEffect(() => { if (!instant) setArmed(null); }, [instant]);

  const arm = (amount: string) => {
    setInstantNote(null);
    if (instant) setArmed({ side, amount, at: Date.now() });
  };
  const onBuyPreset = (amount: string) => { t.setAmount(amount); arm(amount); };
  const onSellPreset = (pct: number) => {
    t.handleSellPct(pct);
    if (instant && t.tokenBalance !== null && t.tokenAddress !== null) arm(formatUnits((t.tokenBalance * BigInt(pct)) / 100n, t.tokenDecimals));
  };
  const changeSlippage = (bps: number) => { t.setSlippageBps(bps); onSlippageChange(bps); };

  const tradeReady = t.isConnected && t.signedIn;
  const minOutRaw = t.minOut ? BigInt(t.minOut) : null;
  const quoteLoading = t.hasAmount && !t.quote && !t.staleQuote && tradeReady;
  const sellDisabled = t.tokenBalance === null || t.tokenBalance === 0n;

  return (
    <>
      <form aria-label="Quick trade" aria-busy={t.isPending || undefined} onSubmit={event => event.preventDefault()} className="flex flex-col gap-2 rounded-md bg-surface-1 p-2">
        <TradeTabs
          side={side}
          onSwitch={next => { setArmed(null); t.switchMode(next === 'buy'); }}
          trailing={<TradeSettingsPopover slippageBps={t.slippageBps} onSlippageChange={changeSlippage} deadlineSeconds={EXECUTION_DEADLINE_SECONDS} spender={t.deployment?.router ?? null} size="sm" />}
        />
        <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
          <legend className="sr-only">{t.isBuy ? 'Buy' : 'Sell'} {tokenSymbol}</legend>
          <AmountInput amount={t.amount} onChange={value => { setArmed(null); t.setAmount(value); }} inputDecimals={t.inputDecimals} inputSymbol={t.isBuy ? 'USDL' : tokenSymbol} errorCode={t.amountError} />
          {t.isBuy ? (
            <div role="group" aria-label="Preset USDL amounts" className="grid grid-cols-4 gap-1">
              {preset.buyAmounts.map((amount, index) => (
                <QuickButton key={`${index}-${amount}`} active={t.amount === amount} onClick={() => onBuyPreset(amount)} label={`${instant ? 'Instant buy' : 'Set'} ${amount} USDL`}>{presetAmountText(amount)}</QuickButton>
              ))}
            </div>
          ) : (
            <div role="group" aria-label="Preset sell portions" className="grid grid-cols-4 gap-1">
              {preset.sellPercents.map((pct, index) => (
                <QuickButton key={`${index}-${pct}`} active={false} disabled={sellDisabled} onClick={() => onSellPreset(pct)} label={`${instant ? 'Instant sell' : 'Set'} ${pct}% of your coins`}>{`${pct}%`}</QuickButton>
              ))}
            </div>
          )}
          {instant ? (
            <p className="flex items-center gap-1 text-2xs text-warning"><Zap aria-hidden="true" className="size-3" />Instant trade is on: quick buttons trade right away.</p>
          ) : null}
          {tradeReady ? (
            <BalanceRow side={side} usdlBalance={t.usdlBalance} tokenBalance={t.tokenBalance} tokenDecimals={t.tokenDecimals} tokenSymbol={tokenSymbol} error={t.balanceError} onMax={t.handleMax} />
          ) : null}
        </fieldset>

        {tradeReady && t.hasAmount ? (
          <TradePreview side={side} tokenSymbol={tokenSymbol} tokenDecimals={t.tokenDecimals} outputRaw={t.quote?.outputRaw ?? null} minOutRaw={t.quote ? minOutRaw : null} impactBps={t.quote?.impactBps ?? null} quoteAge={t.quoteAge} loading={quoteLoading} />
        ) : null}
        {t.needsApproval && t.hasAmount && tradeReady ? <ApprovalCheckbox unlimited={t.approveUnlimited} onChange={t.setApproveUnlimited} assetSymbol={spendSymbol} /> : null}

        <div role="status" aria-live="polite" className="flex flex-col gap-2 empty:hidden">
          {armed ? <NoticeBar tone="info" surface={1}>Getting a price for {armed.amount} {armed.side === 'buy' ? 'USDL' : tokenSymbol}...</NoticeBar> : null}
          {instantNote ? <NoticeBar tone="warning" surface={1} onDismiss={() => setInstantNote(null)}>{instantNote}</NoticeBar> : null}
          {t.switchError ? <NoticeBar tone="danger" surface={1} wrap>{plainTradeError(t.switchError)}</NoticeBar> : null}
          {tradeReady && t.balanceError ? (
            <NoticeBar tone="warning" surface={1} wrap action={<Button size="sm" variant="secondary" onClick={t.refreshBalances}>Retry</Button>}>Your balance can&apos;t load right now.{t.balanceTimestamp !== undefined ? ` Showing your balance from ${new Date(t.balanceTimestamp).toLocaleTimeString()}.` : ''}</NoticeBar>
          ) : null}
          {t.staleQuote && !t.quote ? (
            <NoticeBar tone="warning" surface={1} wrap action={<Button size="sm" variant="secondary" onClick={t.retryQuote}>Refresh</Button>}>The price moved. Refresh to see the new price.</NoticeBar>
          ) : null}
          {t.approvalError ? (
            <NoticeBar tone="danger" surface={1} wrap action={<Button size="sm" variant="secondary" onClick={t.retryQuote}>Refresh</Button>}>{plainTradeError(t.approvalError)}</NoticeBar>
          ) : null}
        </div>

        <ActionButton state={action} side={side} onClick={onAction} />
        <FeeFooter feePercent={t.feePercent} slippageBps={t.slippageBps} feeComponentsRaw={t.feeAmt} />
        <TransactionStatus journey={t.journey} state={t.journeyState} />
      </form>

      {t.toastData ? (
        <TradeToast
          data={{ ...t.toastData, tokenName: tokenSymbol, inputSymbol: t.toastData.type === 'buy' ? 'USDL' : tokenSymbol, outputSymbol: t.toastData.type === 'buy' ? tokenSymbol : 'USDL' }}
          onDismiss={() => t.setToastData(null)}
        />
      ) : null}
      {t.showHighImpact && t.quote ? (
        <TradeRiskReview key={`${quoteFingerprint(t.quote)}:${t.slippageBps}`} slippageBps={t.slippageBps} impactBps={t.quote.impactBps} onConfirm={t.confirmRisk} onCancel={() => t.setShowHighImpact(false)} />
      ) : null}
    </>
  );
}
