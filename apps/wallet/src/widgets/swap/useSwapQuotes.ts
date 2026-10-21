'use client';

/**
 * Debounced swap-quote fetcher.
 *
 * Re-runs when any of the inputs change (token pair, amount, slippage,
 * recipient). 600ms debounce keeps the quote API quiet while the user types.
 *
 * Returns `quotes`, the highest-output `bestQuote`, plus `quoting` and
 * `quoteError` flags so the orchestrator can render busy / error states.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import {
    getSwapQuotes,
    getBestQuote,
    type SwapToken,
    type SwapQuote,
    type SwapParams,
} from '@/lib/swap';
import { parseAmountToWei } from './util';

/** How long a quote stays valid before an automatic re-fetch is triggered. */
const QUOTE_TTL_MS = 30_000;

export interface UseSwapQuotesResult {
    quotes: SwapQuote[];
    bestQuote: SwapQuote | null;
    quoting: boolean;
    quoteError: string;
    /** Seconds until the current quote auto-refreshes (0 when no valid quote). */
    quoteSecondsLeft: number;
    /** Reset the quote state — useful after a successful execution. */
    clear: () => void;
    /** Force an immediate re-fetch of quotes. */
    refresh: () => void;
}

export interface UseSwapQuotesOptions {
    fromToken: SwapToken;
    toToken: SwapToken;
    fromAmount: string;
    slippageBps: number;
    recipient: string | undefined;
    /** Debounce delay in ms (default 600ms). */
    debounceMs?: number;
}

export function useSwapQuotes({
    fromToken,
    toToken,
    fromAmount,
    slippageBps,
    recipient,
    debounceMs = 600,
}: UseSwapQuotesOptions): UseSwapQuotesResult {
    const [quotes, setQuotes] = useState<SwapQuote[]>([]);
    const [bestQuote, setBestQuote] = useState<SwapQuote | null>(null);
    const [quoting, setQuoting] = useState(false);
    const [quoteError, setQuoteError] = useState('');
    const [quoteSecondsLeft, setQuoteSecondsLeft] = useState(0);

    const debounceRef = useRef<ReturnType<typeof setTimeout>>();
    const ttlIntervalRef = useRef<ReturnType<typeof setInterval>>();
    const requestSeqRef = useRef(0);
    const quoteAtRef = useRef<number>(0);
    // Stable ref to the fetch function — filled in below, avoids dependency loops.
    const fetchQuotesRef = useRef<() => void>(() => { });

    const clear = useCallback(() => {
        setQuotes([]);
        setBestQuote(null);
        setQuoteError('');
        setQuoting(false);
        setQuoteSecondsLeft(0);
        quoteAtRef.current = 0;
        if (ttlIntervalRef.current) clearInterval(ttlIntervalRef.current);
    }, []);

    // ── Core fetch logic ──────────────────────────────────────────────────────
    const doFetch = useCallback(async (seq: number) => {
        const wei = parseAmountToWei(fromAmount, fromToken.decimals);
        if (!wei || !recipient) { setQuoting(false); return; }
        try {
            const params: SwapParams = {
                tokenIn: fromToken,
                tokenOut: toToken,
                amountIn: wei,
                slippageBps,
                recipient,
            };
            const q = await getSwapQuotes(params);
            if (seq !== requestSeqRef.current) return;
            setQuotes(q);
            setBestQuote(getBestQuote(q));
            if (q.length === 0) {
                setQuoteError('No routes found for this pair');
                setQuoteSecondsLeft(0);
            } else {
                setQuoteError('');
                quoteAtRef.current = Date.now();
                setQuoteSecondsLeft(Math.round(QUOTE_TTL_MS / 1000));
            }
        } catch (e: any) {
            if (seq !== requestSeqRef.current) return;
            setQuoteError(e.message || 'Quote failed');
            setQuoteSecondsLeft(0);
        } finally {
            if (seq === requestSeqRef.current) setQuoting(false);
        }
    }, [fromAmount, fromToken, toToken, slippageBps, recipient]);

    // Keep the ref in sync with latest doFetch so the TTL interval always calls
    // the current version without re-registering the interval on every render.
    useEffect(() => {
        fetchQuotesRef.current = () => {
            if (debounceRef.current) clearTimeout(debounceRef.current);
            setQuoting(true);
            setBestQuote(null);
            setQuotes([]);
            setQuoteError('');
            const seq = ++requestSeqRef.current;
            doFetch(seq);
        };
    });

    // ── TTL countdown + auto-refresh ─────────────────────────────────────────
    useEffect(() => {
        if (ttlIntervalRef.current) clearInterval(ttlIntervalRef.current);

        ttlIntervalRef.current = setInterval(() => {
            if (!quoteAtRef.current) return;
            const elapsed = Date.now() - quoteAtRef.current;
            const left = Math.max(0, Math.round((QUOTE_TTL_MS - elapsed) / 1000));
            setQuoteSecondsLeft(left);
            if (left === 0) {
                quoteAtRef.current = 0;
                fetchQuotesRef.current();
            }
        }, 1_000);

        return () => { if (ttlIntervalRef.current) clearInterval(ttlIntervalRef.current); };
    }, []); // intentionally empty — interval is stable, ref keeps it current

    // ── Debounced fetch on input change ───────────────────────────────────────
    useEffect(() => {
        if (debounceRef.current) clearTimeout(debounceRef.current);
        setBestQuote(null);
        setQuotes([]);
        setQuoteError('');
        setQuoteSecondsLeft(0);
        quoteAtRef.current = 0;

        const wei = parseAmountToWei(fromAmount, fromToken.decimals);
        if (!wei || !recipient) { setQuoting(false); return; }

        setQuoting(true);
        const seq = ++requestSeqRef.current;

        debounceRef.current = setTimeout(() => doFetch(seq), debounceMs);
        return () => { if (debounceRef.current) clearTimeout(debounceRef.current); };
    }, [fromAmount, fromToken, toToken, slippageBps, recipient, debounceMs, doFetch]);

    const refresh = useCallback(() => fetchQuotesRef.current(), []);

    return { quotes, bestQuote, quoting, quoteError, quoteSecondsLeft, clear, refresh };
}
