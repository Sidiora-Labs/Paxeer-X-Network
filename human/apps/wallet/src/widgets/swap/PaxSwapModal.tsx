"use client";

import NumberFlow from "@number-flow/react";
import { AnimatePresence, motion, MotionConfig } from "framer-motion";
import { ArrowDownUp, ChevronDown, Equal, Loader2, X } from "lucide-react";
import React, { useMemo, useRef, useState } from "react";
import useMeasure from "react-use-measure";
import Image from "next/image";
import { cn } from "@/lib/cn";
import { useWalletState } from "@/providers/WalletProvider";
import { usePaxPriceQuery } from "@/lib/queries";
import { SWAP_TOKENS, DEFAULT_SLIPPAGE_BPS, type SwapToken } from "@/lib/swap";
import { useHeldTokens } from "./useHeldTokens";
import { useOutputTokens } from "./useOutputTokens";
import { useSwapBalances } from "./useSwapBalances";
import { useSwapQuotes } from "./useSwapQuotes";
import { formatBalanceFull, parseAmountToWei } from "./util";
import { formatUsd } from '@/lib/format';

function TokenIcon({ token }: { token: SwapToken }) {
    return (
        <div className="relative size-8 rounded-full bg-[var(--color-surface-control)] flex items-center justify-center overflow-hidden shrink-0">
            <Image src={token.iconUrl || '/default_icon.webp'} alt={token.symbol} className="w-full h-full object-cover rounded-full" fill sizes="32px" />
        </div>
    );
}

function TokenSelectorSheet({ title, tokens, selected, onSelect, onClose, enableSearch = false }: {
    title: string; tokens: SwapToken[]; selected: SwapToken;
    onSelect: (t: SwapToken) => void; onClose: () => void; enableSearch?: boolean;
}) {
    const [query, setQuery] = useState('');
    const list = useMemo(() => {
        const f = !query.trim() ? tokens : tokens.filter(t =>
            t.symbol.toLowerCase().includes(query.toLowerCase()) ||
            t.name.toLowerCase().includes(query.toLowerCase()));
        return f.filter(t => !(t.symbol === selected.symbol && t.address === selected.address));
    }, [tokens, query, selected]);
    return (
        <div className="fixed inset-0 z-50 flex items-end justify-center">
            <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" onClick={onClose} />
            <div className="relative w-full max-w-[430px] bg-[var(--color-surface-raised)] rounded-t-3xl flex flex-col" style={{ maxHeight: '70vh' }}>
                <div className="flex items-center justify-between px-5 pt-5 pb-3  ">
                    <span className="text-sm font-semibold">{title}</span>
                    <button onClick={onClose} className="p-1.5 rounded-full bg-[var(--color-surface-control)] hover:bg-[var(--color-surface-overlay)]">
                        <X className="size-4 opacity-60" />
                    </button>
                </div>
                {enableSearch && (
                    <div className="px-4 pt-3">
                        <input type="text" value={query} onChange={e => setQuery(e.target.value)}
                            placeholder="Search..." autoFocus
                            className="w-full bg-[var(--color-surface-control)] rounded-xl px-3 py-2.5 text-sm outline-none placeholder:opacity-40" />
                    </div>
                )}
                <div className="flex-1 overflow-y-auto p-3 space-y-1">
                    {list.length === 0 && <p className="text-center text-sm text-pax-muted py-8">No tokens found</p>}
                    {list.map(t => (
                        <button key={t.address || 'native'} onClick={() => onSelect(t)}
                            className="w-full flex items-center gap-3 px-3 py-3 rounded-xl bg-[var(--color-surface-card)] hover:bg-[var(--color-surface-control)] transition-colors">
                            <TokenIcon token={t} />
                            <div className="flex-1 text-left min-w-0">
                                <p className="text-sm font-semibold">{t.symbol}</p>
                                <p className="text-xs text-pax-muted truncate">{t.name}</p>
                            </div>
                            {t.isStablecoin && <span className="text-[10px] px-1.5 py-0.5 rounded bg-blue-500/10 text-blue-400">Stable</span>}
                            {t.isNative && <span className="text-[10px] px-1.5 py-0.5 rounded bg-pax-accent/10 text-pax-accent">Native</span>}
                            {t.isLaunchpad && <span className="text-[10px] px-1.5 py-0.5 rounded bg-pax-accent/10 text-pax-accent">Launchpad</span>}
                        </button>
                    ))}
                </div>
            </div>
        </div>
    );
}

export interface PaxSwapModalProps {
    fromToken?: SwapToken;
    toToken?: SwapToken;
    fromAmount?: string;
    slippageBps?: number;
    onFromTokenChange?: (t: SwapToken) => void;
    onToTokenChange?: (t: SwapToken) => void;
    onFromAmountChange?: (v: string) => void;
    onFlip?: () => void;
}

export const PaxSwapModal = (props: PaxSwapModalProps = {}) => {
    const { activeAccount } = useWalletState();
    const recipient = activeAccount?.address;

    // Uncontrolled fallbacks
    const [fromTokenLocal, setFromTokenLocal] = useState<SwapToken>(SWAP_TOKENS[0]);
    const [toTokenLocal, setToTokenLocal] = useState<SwapToken>(SWAP_TOKENS[2]);
    const [fromAmountLocal, setFromAmountLocal] = useState('');
    const [slippageBpsLocal] = useState(DEFAULT_SLIPPAGE_BPS);

    const fromToken = props.fromToken ?? fromTokenLocal;
    const toToken = props.toToken ?? toTokenLocal;
    const inputValue = props.fromAmount ?? fromAmountLocal;
    const slippageBps = props.slippageBps ?? slippageBpsLocal;
    const setFromToken = (t: SwapToken) => (props.onFromTokenChange ?? setFromTokenLocal)(t);
    const setToToken = (t: SwapToken) => (props.onToTokenChange ?? setToTokenLocal)(t);
    const setInputValue = (v: string) => (props.onFromAmountChange ?? setFromAmountLocal)(v);
    const value = parseFloat(inputValue || '0') || 0;

    const [selectorTarget, setSelectorTarget] = useState<'from' | 'to' | null>(null);
    const inputRef = useRef<HTMLInputElement>(null);
    const [ref, bounds] = useMeasure();
    const [ref2, bounds2] = useMeasure();
    const heldTokens = useHeldTokens(recipient);
    const outputTokens = useOutputTokens();
    const balances = useSwapBalances(recipient, fromToken, toToken);
    const { data: paxPriceData } = usePaxPriceQuery();
    const quotes = useSwapQuotes({ fromToken, toToken, fromAmount: inputValue, slippageBps, recipient });
    const fromBalanceNum = parseFloat(balances.fromBalance || '0') || 0;
    const isInsufficient = (() => {
        if (value <= 0 || balances.fromBalanceRaw === null || balances.fromBalanceRaw === undefined) return false;
        const inputWei = parseAmountToWei(inputValue, fromToken.decimals);
        if (!inputWei) return false;
        return BigInt(inputWei) > balances.fromBalanceRaw;
    })();
    const usdValue = useMemo(() => {
        if (!value) return 0;
        if (fromToken.isNative && paxPriceData?.latest) return value * paxPriceData.latest;
        if (toToken.isStablecoin && quotes.bestQuote) return parseFloat(quotes.bestQuote.amountOutDisplay.replace(/,/g, '')) || 0;
        return 0;
    }, [value, fromToken.isNative, paxPriceData, toToken.isStablecoin, quotes.bestQuote]);
    const showUsdValue = usdValue > 0 && !isInsufficient && !quotes.quoting;
    const ETH_BALANCE = fromBalanceNum; // alias for max-button logic

    const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const v = e.target.value;
        if (!/^[0-9]*\.?[0-9]*$/.test(v) && v !== '') return;
        setInputValue(v);
    };
    const handleUseMax = () => {
        if (!balances.fromBalanceRaw) return;
        setInputValue(formatBalanceFull(balances.fromBalanceRaw, fromToken.decimals));
    };
    const handleClear = () => { setInputValue(''); };
    const handleFlip = () => {
        if (props.onFlip) { props.onFlip(); return; }
        const a = fromToken, b = toToken;
        setFromToken(b); setToToken(a); setInputValue('');
    };
    const digits = (inputValue || '0').split('');
    const receiveDisplay = quotes.quoting ? null : quotes.bestQuote ? quotes.bestQuote.amountOutDisplay.replace(/,/g, '') : null;
    const errorMessage = isInsufficient ? `Not Enough ${fromToken.symbol}`
        : (quotes.quoteError && !quotes.quoting && value > 0) ? 'No Route Found' : null;

    return (
        <MotionConfig transition={{ type: 'spring', stiffness: 400, damping: 35 }}>
            {selectorTarget && (
                <TokenSelectorSheet
                    title={selectorTarget === 'from' ? 'You Pay' : 'You Receive'}
                    tokens={selectorTarget === 'from' ? heldTokens : outputTokens}
                    selected={selectorTarget === 'from' ? fromToken : toToken}
                    enableSearch={selectorTarget === 'to'}
                    onSelect={(t) => { if (selectorTarget === 'from') setFromToken(t); else setToToken(t); setSelectorTarget(null); handleClear(); }}
                    onClose={() => setSelectorTarget(null)}
                />
            )}
            <div className="font-open-runde w-full text-white">
                <div className="flex flex-col gap-3">
                    <div className="relative flex w-full flex-col items-center justify-center gap-5 rounded-3xl bg-[var(--color-surface-card)] px-3 py-3">
                        <div className="flex w-full items-center justify-between">
                            <button onClick={() => setSelectorTarget('from')} className="flex items-center gap-2.5 hover:opacity-80 transition-opacity">
                                <TokenIcon token={fromToken} />
                                <div className="text-left">
                                    <div className="flex items-center gap-1">
                                        <h2 className="text-base font-semibold">{fromToken.symbol}</h2>
                                        <ChevronDown className="size-3.5 opacity-50" />
                                    </div>
                                    <p className="text-xs text-pax-muted">{balances.loading ? '…' : (balances.fromBalance ?? '—')}</p>
                                </div>
                            </button>
                            <button
                                onClick={handleUseMax}
                                disabled={fromBalanceNum === 0}
                                className="flex items-center gap-1 rounded-full bg-[var(--color-surface-control)] px-3 py-1 text-sm font-semibold transition-colors hover:bg-[var(--color-surface-overlay)] disabled:opacity-30"
                            >
                                <motion.span animate={{ width: bounds.width > 0 ? bounds.width : 'auto' }}>
                                    <span ref={ref} className="inline-flex overflow-hidden">
                                        <AnimatePresence mode="popLayout">
                                            {value > 0 && value >= fromBalanceNum && fromBalanceNum > 0 ? (
                                                <motion.span key="using" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>Using{' '}</motion.span>
                                            ) : (
                                                <motion.span key="use" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>Use{' '}</motion.span>
                                            )}
                                        </AnimatePresence>
                                    </span>
                                </motion.span>
                                Max
                            </button>
                        </div>

                        <div className="w-full" />

                        <div className="mb-6 flex w-full flex-col items-center justify-center gap-4">
                            <div className="relative w-full overflow-hidden text-center">
                                <input
                                    ref={inputRef}
                                    type="text"
                                    autoComplete="off"
                                    className={cn(
                                        "inset-0 w-full cursor-pointer bg-transparent text-center text-[45px] font-semibold tracking-tight text-transparent caret-white outline-none",
                                        inputValue === "" && "caret-transparent",
                                    )}
                                    placeholder="0"
                                    value={inputValue}
                                    onChange={handleInputChange}
                                />
                                <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
                                    <AnimatePresence initial={false} mode="popLayout">
                                        {digits.map((digit, index) => (
                                            <motion.span
                                                key={`${digit}-${index}`}
                                                className="text-[45px] font-semibold tracking-tight"
                                                initial={{ y: "100%", opacity: 0 }}
                                                animate={{ y: "0%", opacity: 1 }}
                                                exit={{ y: "100%", opacity: 0 }}
                                            >
                                                {digit}
                                            </motion.span>
                                        ))}
                                    </AnimatePresence>
                                </div>
                            </div>

                            <div className="flex w-full items-center justify-center gap-2">
                                <AnimatePresence initial={false} mode="popLayout">
                                    {errorMessage ? (
                                        <motion.p key="err" initial={{ opacity: 0, scale: 0 }}
                                            animate={{ opacity: 1, scale: 1, x: [0, -5, 5, -3, 3, 0], transition: { x: { delay: 0.2, times: [0, 0.2, 0.4, 0.6, 0.8, 1] } } }}
                                            exit={{ opacity: 0, scale: 0 }} style={{ transformOrigin: 'bottom center' }}
                                            className="font-lg w-max text-center font-semibold tracking-tight text-red-500">
                                            {errorMessage}
                                        </motion.p>
                                    ) : quotes.quoting ? (
                                        <motion.div key="loading" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
                                            <Loader2 className="size-5 animate-spin text-pax-muted" />
                                        </motion.div>
                                    ) : showUsdValue ? (
                                        <motion.div key="usd-value" style={{ transformOrigin: 'top center' }}
                                            initial={{ opacity: 0, y: '100%', scale: 0 }} animate={{ opacity: 1, scale: 1, y: 0 }} exit={{ opacity: 0, scale: 0 }}
                                            className="flex items-center justify-center gap-2">
                                            <div className="rounded-full bg-[var(--color-surface-control)] p-1"><Equal className="size-5" /></div>
                                            <motion.div animate={{ width: bounds2.width > 0 ? bounds2.width : 'auto' }} className="flex items-center gap-2 overflow-hidden">
                                                <div ref={ref2} className="font-lg flex justify-between font-semibold tracking-tight">
                                                    {formatUsd(usdValue)}
                                                </div>
                                            </motion.div>
                                            <ArrowDownUp className="size-5" />
                                        </motion.div>
                                    ) : value > 0 ? (
                                        <motion.p key="hint" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="text-sm text-pax-muted font-medium">
                                            {inputValue} {fromToken.symbol}
                                        </motion.p>
                                    ) : null}
                                </AnimatePresence>
                            </div>
                        </div>
                        <button
                            type="button"
                            onClick={handleFlip}
                            className="absolute -bottom-6 rounded-full bg-[var(--color-surface-raised)] p-1.5 hover:bg-[var(--color-surface-control)] transition-colors"
                            aria-label="Flip tokens"
                        >
                            <ChevronDown className="size-5 opacity-50" />
                        </button>
                    </div>

                    <div className="flex w-full flex-col items-start justify-start gap-5 rounded-3xl bg-[var(--color-surface-card)] px-3 py-3">
                        <div className="flex w-full items-center justify-between">
                            <button onClick={() => setSelectorTarget('to')} className="flex items-center gap-2.5 hover:opacity-80 transition-opacity">
                                <TokenIcon token={toToken} />
                                <div className="text-left">
                                    <div className="flex items-center gap-1">
                                        <h2 className="text-base font-semibold">{toToken.symbol}</h2>
                                        <ChevronDown className="size-3.5 opacity-50" />
                                    </div>
                                    <p className="text-sm text-pax-muted">Receive {toToken.name}</p>
                                </div>
                            </button>
                            <p className="rounded-full pr-2 text-lg font-semibold">
                                {quotes.quoting ? <Loader2 className="size-5 animate-spin text-pax-muted" />
                                    : receiveDisplay ? <NumberFlow value={parseFloat(receiveDisplay) || 0} format={{ maximumFractionDigits: 8 }} />
                                        : <span className="text-pax-muted">—</span>}
                            </p>
                        </div>
                    </div>
                    {inputValue && (
                        <button
                            onClick={handleClear}
                            className="w-full rounded-full bg-white/5 py-2 text-white/40 text-sm transition-colors hover:bg-white/10"
                        >
                            Clear
                        </button>
                    )}
                </div>
            </div>
        </MotionConfig>
    );
};
