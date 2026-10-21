'use client';

/**
 * Swap widget — orchestrator that routes between the four swap views and
 * wires the data hooks together.
 *
 * Composes:
 * - {@link SwapMainView}        — pay/receive cards + best-route summary
 * - {@link TokenSelectorView}   — full-page token picker (in/out)
 * - {@link SwapSettingsView}    — slippage tolerance editor
 * - {@link SwapConfirmDialog}   — pre-execute confirm drawer
 * - {@link TransferSuccess}     — post-execute success view
 *
 * Owns:
 * - From/to token state (initialized from `SWAP_TOKENS`)
 * - Amount input + slippage state
 * - Local view router (`main` / `selectFrom` / `selectTo` / `settings`)
 * - Confirm-drawer open state
 *
 * Data fetching is delegated to focused hooks:
 * - {@link useHeldTokens}      — input list (cached portfolio)
 * - {@link useOutputTokens}    — output list (paxscan + launchpad merge)
 * - {@link useSwapBalances}    — derived from/to balances
 * - {@link useSwapQuotes}      — debounced quote fetcher
 * - {@link useSwapExecution}   — execute swap state machine
 *
 * Replaces the legacy `SwapPage`. Same prop contract.
 */

import { useCallback, useState } from 'react';
import { useWalletState } from '@/providers/WalletProvider';
import { parseAmountToWei } from './util';
import { classifySwapError } from './prettifyError';
import {
    validateSlippageBps,
    validateSpendableBalance,
    validateSwapQuote,
    validateTokenAmount,
} from '@/lib/txValidation';
import { SwapSuccess } from '@/components/SwapSuccess';
import { SlippageDrawer } from '@/components/ui/SlippageDrawer';
import {
    InsufficientGasModal,
    TxTimeoutModal,
    RateLimitModal,
    ContractRevertModal,
} from '@/components/ui/TransactionModals';
import { SWAP_TOKENS, DEFAULT_SLIPPAGE_BPS, type SwapToken } from '@/lib/swap';
import { PaxSwapModal } from './PaxSwapModal';
import { SwapConfirmDialog } from './SwapConfirmDialog';
import { useSwapBalances } from './useSwapBalances';
import { useSwapQuotes } from './useSwapQuotes';
import { useSwapExecution } from './useSwapExecution';

export interface SwapWidgetProps {
    onPaxscan?: (path?: string) => void;
}

export function SwapWidget({ onPaxscan }: SwapWidgetProps) {
    const { activeAccount } = useWalletState();
    const recipient = activeAccount?.address;

    const [fromToken, setFromToken] = useState<SwapToken>(SWAP_TOKENS[0]);
    const [toToken, setToToken] = useState<SwapToken>(SWAP_TOKENS[2]); // USDC default
    const [fromAmount, setFromAmount] = useState('');
    const [slippageBps, setSlippageBps] = useState(DEFAULT_SLIPPAGE_BPS);
    const [preflightError, setPreflightError] = useState('');
    const [settingsOpen, setSettingsOpen] = useState(false);
    const [confirmOpen, setConfirmOpen] = useState(false);

    const balances = useSwapBalances(recipient, fromToken, toToken);
    const quotes = useSwapQuotes({
        fromToken,
        toToken,
        fromAmount,
        slippageBps,
        recipient,
    });
    const execution = useSwapExecution();

    // ── Error modal routing ──────────────────────────────────────────────
    // Classify the raw exec error into the right modal kind; null = inline only.
    const errorKind = execution.execError ? classifySwapError(execution.execError) : null;

    const handleFlip = useCallback(() => {
        setFromToken(toToken);
        setToToken(fromToken);
        setFromAmount('');
    }, [fromToken, toToken]);

    const handleConfirmSwap = useCallback(async () => {
        if (!quotes.bestQuote || !recipient) return;
        await execution.execute(
            quotes.bestQuote,
            fromToken,
            toToken,
            slippageBps,
            recipient,
            balances.fromBalanceRaw,
        );
    }, [quotes.bestQuote, recipient, fromToken, toToken, slippageBps, balances.fromBalanceRaw, execution]);

    const runPreflight = useCallback(() => {
        try {
            if (!quotes.bestQuote) throw new Error('No route found.');
            validateTokenAmount(fromAmount, fromToken.decimals);
            validateSpendableBalance(BigInt(quotes.bestQuote.amountIn), balances.fromBalanceRaw);
            validateSlippageBps(slippageBps);
            validateSwapQuote(quotes.bestQuote);
            setPreflightError('');
            return true;
        } catch (e) {
            setPreflightError(e instanceof Error ? e.message : 'Swap preflight failed');
            return false;
        }
    }, [quotes.bestQuote, fromAmount, fromToken.decimals, balances.fromBalanceRaw, slippageBps]);

    const handleResetAfterSuccess = useCallback(() => {
        setFromAmount('');
        setConfirmOpen(false);
        quotes.clear();
        execution.reset();
    }, [quotes, execution]);

    // ── Main view ───────────────────────────────────────────────────────
    const isOverBalance = (() => {
        if (balances.fromBalanceRaw === null || balances.fromBalanceRaw === undefined) return false;
        const inputWei = parseAmountToWei(fromAmount, fromToken.decimals);
        if (!inputWei) return false;
        return BigInt(inputWei) > balances.fromBalanceRaw;
    })();

    const submitDisabled =
        !recipient ||
        !quotes.bestQuote ||
        quotes.quoting ||
        !!quotes.quoteError ||
        !fromAmount ||
        parseFloat(fromAmount) <= 0 ||
        isOverBalance;

    return (
        <>
            <div className="flex flex-col gap-3 px-3 pt-2 pb-24 w-full">
                <PaxSwapModal
                    fromToken={fromToken}
                    toToken={toToken}
                    fromAmount={fromAmount}
                    slippageBps={slippageBps}
                    onFromTokenChange={setFromToken}
                    onToTokenChange={setToToken}
                    onFromAmountChange={setFromAmount}
                    onFlip={handleFlip}
                />

                <button
                    onClick={() => setSettingsOpen(true)}
                    className="flex items-center justify-between rounded-2xl   bg-white/5 px-4 py-2.5 text-sm hover:bg-white/[0.08] transition-colors w-full"
                >
                    <span className="text-pax-muted">Slippage</span>
                    <span className="font-semibold">{(slippageBps / 100).toFixed(2)}%</span>
                </button>

                <button
                    onClick={() => {
                        execution.clearError();
                        if (runPreflight()) setConfirmOpen(true);
                    }}
                    disabled={submitDisabled}
                    className="w-full rounded-full bg-pax-accent text-black font-bold py-3.5 text-sm press-scale disabled:opacity-40 transition-all"
                >
                    {!recipient ? 'Connect wallet'
                        : quotes.quoting ? 'Finding best route…'
                            : !fromAmount || parseFloat(fromAmount) <= 0 ? 'Enter an amount'
                                : quotes.quoteError ? 'No route found'
                                    : quotes.quoteSecondsLeft > 0
                                        ? `Swap ${fromToken.symbol} → ${toToken.symbol} · ${quotes.quoteSecondsLeft}s`
                                        : `Swap ${fromToken.symbol} → ${toToken.symbol}`}
                </button>

                {quotes.quoteSecondsLeft > 0 && quotes.quoteSecondsLeft <= 10 && (
                    <button
                        onClick={quotes.refresh}
                        className="w-full text-xs text-amber-400 text-center py-1 press-scale"
                    >
                        Quote expires in {quotes.quoteSecondsLeft}s — tap to refresh
                    </button>
                )}
                {preflightError && <p className="text-xs text-red-400 text-center">{preflightError}</p>}
            </div>

            {execution.txHash && quotes.bestQuote && (
                <SwapSuccess
                    fromAmount={fromAmount}
                    fromSymbol={fromToken.symbol}
                    fromIconUrl={fromToken.iconUrl}
                    toAmount={quotes.bestQuote.amountOutDisplay}
                    toSymbol={toToken.symbol}
                    toIconUrl={toToken.iconUrl}
                    txHash={execution.txHash}
                    onExplorerView={() => onPaxscan?.(`/tx/${execution.txHash}`)}
                    onDone={handleResetAfterSuccess}
                />
            )}

            <SlippageDrawer
                open={settingsOpen}
                onClose={() => setSettingsOpen(false)}
                slippageBps={slippageBps}
                onChange={setSlippageBps}
            />

            {quotes.bestQuote && (
                <SwapConfirmDialog
                    open={confirmOpen && !execution.txHash}
                    loading={execution.executing}
                    execError={errorKind ? '' : execution.execError}
                    bestQuote={quotes.bestQuote}
                    fromToken={fromToken}
                    toToken={toToken}
                    fromAmount={fromAmount}
                    slippageBps={slippageBps}
                    onClose={() => {
                        setConfirmOpen(false);
                        execution.clearError();
                    }}
                    onConfirm={handleConfirmSwap}
                />
            )}

            {/* ── Transaction error modals ─────────────────────────────── */}
            <InsufficientGasModal
                open={errorKind === 'gas'}
                onClose={execution.clearError}
                onBuyPax={() => { execution.clearError(); setConfirmOpen(false); onPaxscan?.(); }}
            />
            <ContractRevertModal
                open={errorKind === 'revert'}
                onClose={execution.clearError}
                revertReason={execution.execError}
            />
            <TxTimeoutModal
                open={errorKind === 'timeout'}
                onClose={execution.clearError}
                onSpeedUp={() => { execution.clearError(); handleConfirmSwap(); }}
            />
            <RateLimitModal
                open={errorKind === 'ratelimit'}
                onClose={execution.clearError}
                onRetry={() => { execution.clearError(); handleConfirmSwap(); }}
            />
        </>
    );
}
