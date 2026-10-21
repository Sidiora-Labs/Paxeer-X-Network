'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { formatUnits } from 'viem';
import TradeToast from './trade-toast';
import SharePnlModal from '@/widgets/pnl/share-pnl-modal';
import { TradeRiskReview } from '@/widgets/trade/slippage-selector';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { statsSpotPrice } from '@/core/market/pool-stats-display';
import { openAuthModal } from '@/shell/auth-modal-store';
import { Button, NoticeBar } from '@/ui/primitives';
import { shortAddress } from '@/utils/format';
import { quoteFingerprint } from './transaction-controls/domain';
import { TransactionStatus } from './transaction-controls/transaction-status';
import { TradeSettingsPopover } from './trade-settings-popover';
import { useTradePanel } from './use-trade-panel';
import { plainTradeError } from './trade-copy';
import {
  TradeTabs,
  AmountInput,
  BuyPresets,
  SellPcts,
  BalanceRow,
  TradeSteps,
  TradePreview,
  ApprovalCheckbox,
  ActionButton,
  FeeFooter,
  SharePnlButton,
  convertByPrice,
  type TradeActionState,
  type TradeSide,
} from './trade-panel-parts';

interface TradePanelProps { poolAddress: string; }

const EXECUTION_DEADLINE_SECONDS = 120;

export default function TradePanel({ poolAddress }: TradePanelProps) {
  const t = useTradePanel({ poolAddress });
  const snapshot = usePoolSnapshot(poolAddress);
  const pnlSnapshotId = t.pnlSnapshotId;
  const side: TradeSide = t.isBuy ? 'buy' : 'sell';

  const tokenSymbol = snapshot.data?.token?.symbol?.trim() || t.tokenName;
  const spendSymbol = t.isBuy ? 'USDL' : tokenSymbol;
  const nativeUnit = t.isBuy ? 'USDL' : tokenSymbol;
  const altUnit = t.isBuy ? tokenSymbol : 'USDL';

  // PoolStats.spotPriceWad as an exact decimal USDL price; the unit toggle stays disabled before the first trade (null price).
  const lastPrice = statsSpotPrice(snapshot.data?.stats ?? null).value;
  const priceUsable = typeof lastPrice === 'string' && /^\d+(?:\.\d+)?$/.test(lastPrice) && /[1-9]/.test(lastPrice);
  const chainMetadataReady = t.tokenAddress !== null;
  const unitToggleDisabledReason = !chainMetadataReady
    ? 'Loading...'
    : !priceUsable
      ? 'Available after the first trade'
      : null;

  const [unit, setUnit] = useState<'native' | 'alt'>('native');
  const [altText, setAltText] = useState('');

  const toNative = useCallback((text: string) => {
    if (!priceUsable || !lastPrice) return null;
    return convertByPrice(text, lastPrice, t.isBuy ? 'token' : 'usdl', t.inputDecimals);
  }, [priceUsable, lastPrice, t.isBuy, t.inputDecimals]);
  const toAlt = useCallback((text: string) => {
    if (!priceUsable || !lastPrice) return null;
    return convertByPrice(text, lastPrice, t.isBuy ? 'usdl' : 'token', t.isBuy ? t.tokenDecimals : 6);
  }, [priceUsable, lastPrice, t.isBuy, t.tokenDecimals]);

  useEffect(() => { setUnit('native'); setAltText(''); }, [t.isBuy, poolAddress]);
  useEffect(() => { if (unit === 'alt' && t.amount === '' && altText !== '') setAltText(''); }, [unit, t.amount, altText]);
  useEffect(() => { if (unit === 'alt' && unitToggleDisabledReason) { setUnit('native'); setAltText(''); } }, [unit, unitToggleDisabledReason]);

  const onAmountChange = (text: string) => {
    if (unit === 'native') { t.setAmount(text); return; }
    setAltText(text);
    t.setAmount(text.trim() === '' ? '' : (toNative(text.trim()) ?? text));
  };
  const syncAltFromNative = (native: string) => { if (unit === 'alt') setAltText(native === '' ? '' : (toAlt(native) ?? '')); };
  const onPreset = (preset: string) => { t.setAmount(preset); syncAltFromNative(preset); };
  const onMax = () => {
    t.handleMax();
    const balance = t.isBuy ? t.usdlBalance : t.tokenBalance;
    if (balance !== null) syncAltFromNative(formatUnits(balance, t.inputDecimals));
  };
  const onSellPct = (pct: number) => {
    t.handleSellPct(pct);
    if (t.tokenBalance !== null) syncAltFromNative(formatUnits((t.tokenBalance * BigInt(pct)) / 100n, t.tokenDecimals));
  };
  const cycleUnit = () => {
    if (unit === 'native') { setAltText(t.amount === '' ? '' : (toAlt(t.amount) ?? '')); setUnit('alt'); }
    else { setUnit('native'); setAltText(''); }
  };

  const displayValue = unit === 'native' ? t.amount : altText;
  const displayUnit = unit === 'native' ? nativeUnit : altUnit;
  const hint = useMemo(() => {
    if (displayValue.trim() === '' || !priceUsable) return null;
    if (unit === 'alt') return t.amount ? `≈ ${t.amount} ${nativeUnit}` : null;
    const alt = toAlt(t.amount);
    return alt ? `≈ ${alt} ${altUnit}` : null;
  }, [displayValue, priceUsable, unit, t.amount, nativeUnit, altUnit, toAlt]);

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

  const tradeReady = t.isConnected && t.signedIn;
  const showSteps = tradeReady && t.hasAmount;
  const minOutRaw = t.minOut ? BigInt(t.minOut) : null;
  const quoteLoading = t.hasAmount && !t.quote && !t.staleQuote && tradeReady;

  return (
    <>
      <form
        aria-label="Trade"
        aria-busy={t.isPending || undefined}
        onSubmit={event => event.preventDefault()}
        className="space-y-3 rounded-lg bg-surface-1 p-3"
      >
        <TradeTabs
          side={side}
          onSwitch={next => t.switchMode(next === 'buy')}
          trailing={<TradeSettingsPopover slippageBps={t.slippageBps} onSlippageChange={t.setSlippageBps} deadlineSeconds={EXECUTION_DEADLINE_SECONDS} spender={t.deployment?.router ?? null} size="md" />}
        />

        <fieldset className="m-0 min-w-0 space-y-3 p-0 border-0">
          <legend className="sr-only">{t.isBuy ? 'Buy' : 'Sell'} {tokenSymbol}</legend>

          <AmountInput
            amount={displayValue}
            onChange={onAmountChange}
            inputDecimals={t.inputDecimals}
            inputSymbol={nativeUnit}
            unitLabel={displayUnit}
            onCycleUnit={cycleUnit}
            unitToggleDisabledReason={unitToggleDisabledReason}
            errorCode={t.amountError}
            hint={hint}
          />

          {t.isBuy ? (
            <BuyPresets amount={t.amount} onSelect={onPreset} onMax={tradeReady ? onMax : undefined} maxDisabled={t.usdlBalance === null} />
          ) : (
            <SellPcts tokenBalance={t.tokenBalance} tokenDecimals={t.tokenDecimals} amount={t.amount} onSelect={onSellPct} />
          )}

          {tradeReady ? (
            <BalanceRow
              side={side}
              usdlBalance={t.usdlBalance}
              tokenBalance={t.tokenBalance}
              tokenDecimals={t.tokenDecimals}
              tokenSymbol={tokenSymbol}
              error={t.balanceError}
              onMax={onMax}
            />
          ) : null}
        </fieldset>

        {showSteps ? <TradeSteps side={side} assetSymbol={spendSymbol} needsApproval={t.needsApproval} /> : null}

        {tradeReady && t.hasAmount ? (
          <TradePreview
            side={side}
            tokenSymbol={tokenSymbol}
            tokenDecimals={t.tokenDecimals}
            outputRaw={t.quote?.outputRaw ?? null}
            minOutRaw={t.quote ? minOutRaw : null}
            impactBps={t.quote?.impactBps ?? null}
            quoteAge={t.quoteAge}
            loading={quoteLoading}
          />
        ) : null}

        {t.needsApproval && t.hasAmount && tradeReady ? (
          <ApprovalCheckbox unlimited={t.approveUnlimited} onChange={t.setApproveUnlimited} assetSymbol={spendSymbol} />
        ) : null}

        <div role="status" aria-live="polite" className="space-y-2 empty:hidden">
          {t.switchError ? <NoticeBar tone="danger" surface={1} wrap>{plainTradeError(t.switchError)}</NoticeBar> : null}
          {tradeReady && t.balanceError ? (
            <NoticeBar tone="warning" surface={1} wrap action={<Button size="sm" variant="secondary" onClick={t.refreshBalances}>Retry</Button>}>
              Your balance can&apos;t load right now.{t.balanceTimestamp !== undefined ? ` Showing your balance from ${new Date(t.balanceTimestamp).toLocaleTimeString()}.` : ''}
            </NoticeBar>
          ) : null}
          {t.staleQuote && !t.quote ? (
            <NoticeBar tone="warning" surface={1} wrap action={<Button size="sm" variant="secondary" onClick={t.retryQuote}>Refresh</Button>}>
              The price moved. Refresh to see the new price.
            </NoticeBar>
          ) : null}
          {/* A missing trading deployment is already the action button's state once the viewer can trade; before that it is noise in the rail. */}
          {t.approvalError && (tradeReady || !t.deploymentUnavailable) ? (
            <NoticeBar tone="danger" surface={1} wrap action={<Button size="sm" variant="secondary" onClick={t.retryQuote} aria-label="Refresh">Refresh</Button>}>
              {plainTradeError(t.approvalError)}
            </NoticeBar>
          ) : null}
        </div>

        {tradeReady && t.hasAmount && !t.quote && !t.isPending && !t.staleQuote ? (
          <div className="flex justify-end">
            <Button size="sm" variant="link" onClick={t.retryQuote}>Refresh price</Button>
          </div>
        ) : null}

        <ActionButton state={action} side={side} onClick={onAction} />

        <FeeFooter feePercent={t.feePercent} slippageBps={t.slippageBps} feeComponentsRaw={t.feeAmt} />

        {tradeReady && t.hasAmount ? (
          <details className="group rounded-sm bg-surface-2 text-xs text-text-muted">
            <summary className="cursor-pointer select-none px-2.5 py-2 font-bold text-text-muted transition-colors duration-fast hover:text-text">Details</summary>
            <div className="space-y-1.5 px-2.5 pb-2.5">
              {t.quote ? (
                <>
                  <p className="tabular-nums">Price from {t.quoteAge}s ago, good until {new Date(t.quote.expiresAt).toLocaleTimeString()} (block {t.quote.blockHeight.toString()})</p>
                  <p className="tabular-nums">Raw amounts: receive {t.quote.outputRaw.toString()}, fees {t.feeAmt ?? '—'}, minimum {t.minOut ?? '—'}</p>
                </>
              ) : (
                <p>No price yet</p>
              )}
              <p>Trade expires {EXECUTION_DEADLINE_SECONDS} seconds after you confirm. Approved contract {t.deployment?.router ? shortAddress(t.deployment.router) : '—'}.</p>
              <ul aria-label="Trading limits" className="list-none space-y-0.5 p-0">
                {t.restrictions.map(restriction => <li key={restriction}>{restriction}</li>)}
              </ul>
            </div>
          </details>
        ) : null}

        <TransactionStatus journey={t.journey} state={t.journeyState} />

        {tradeReady && t.address && t.canShare ? (
          <SharePnlButton
            tokenName={tokenSymbol}
            state={t.mintState.kind}
            errorMessage={t.mintState.kind === 'error' ? t.mintState.message : undefined}
            onClick={t.handleSharePnl}
          />
        ) : null}
      </form>

      {t.toastData ? (
        <TradeToast
          data={{
            ...t.toastData,
            tokenName: tokenSymbol,
            inputSymbol: t.toastData.type === 'buy' ? 'USDL' : tokenSymbol,
            outputSymbol: t.toastData.type === 'buy' ? tokenSymbol : 'USDL',
          }}
          onDismiss={() => t.setToastData(null)}
          onShare={t.address && tradeReady && t.canShare ? t.handleSharePnl : undefined}
          sharing={t.mintState.kind === 'minting'}
          shareError={t.mintState.kind === 'error' ? plainTradeError(t.mintState.message, "Couldn't create your card, try again") : null}
        />
      ) : null}

      {t.showHighImpact && t.quote ? (
        <TradeRiskReview
          key={`${quoteFingerprint(t.quote)}:${t.slippageBps}`}
          slippageBps={t.slippageBps}
          impactBps={t.quote.impactBps}
          onConfirm={t.confirmRisk}
          onCancel={() => t.setShowHighImpact(false)}
        />
      ) : null}

      {t.mintState.kind === 'ready' && pnlSnapshotId ? (
        <SharePnlModal
          snapshotId={pnlSnapshotId}
          preview={t.mintState.preview}
          owner={t.mintState.owner}
          error={t.mintState.error}
          onClose={t.resetPnlMint}
          onSelection={(fields, includeWallet, expiresAt) => t.selectPnlFields(pnlSnapshotId, fields, includeWallet, expiresAt)}
          onPublish={t.publishPnlCard}
          onRefresh={t.refreshPnlCard}
          onRevoke={t.revokePnlCard}
        />
      ) : null}
    </>
  );
}
