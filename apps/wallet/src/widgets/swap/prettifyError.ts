/**
 * Map a raw error string from a swap quote/exec failure to a user-facing
 * { title, reason, tip } triple.
 *
 * The mapping is intentionally aggressive on substring matching because
 * different routers (PECOR, Launchpad, V2/V3 AMMs) wrap their reverts in
 * different envelopes. A single string match is enough to pick the friendly
 * copy; ambiguous matches fall through to the generic catch-all.
 */

export interface PrettyError {
    title: string;
    reason: string;
    tip: string;
}

interface ErrorMapping {
    needles: readonly string[];
    pretty: PrettyError;
}

const ERROR_TABLE: readonly ErrorMapping[] = [
    // ── Contract-level reverts ───────────────────────────────────────────
    {
        needles: ['poolnotfound', 'pool not found'],
        pretty: {
            title: 'Pool not available',
            reason: 'No liquidity pool exists for this token pair.',
            tip: 'Try swapping through a different intermediate token or check the token address.',
        },
    },
    {
        needles: ['insufficientoutput', 'insufficient output'],
        pretty: {
            title: 'Slippage exceeded',
            reason: 'Price moved and the output fell below your minimum.',
            tip: 'Increase slippage tolerance in settings or try a smaller amount.',
        },
    },
    {
        needles: ['expired'],
        pretty: {
            title: 'Transaction expired',
            reason: 'The swap deadline passed before the tx was mined.',
            tip: 'Try again — the network may be congested.',
        },
    },
    {
        needles: ['invalidpath', 'invalid path'],
        pretty: {
            title: 'Invalid route',
            reason: 'The swap path between these tokens is not supported.',
            tip: 'Try swapping through a stablecoin (USDC, USDT) as an intermediate.',
        },
    },
    {
        needles: ['notapprovedstablecoin', 'not approved stablecoin'],
        pretty: {
            title: 'Stablecoin not supported',
            reason: 'This stablecoin is not approved for trading on the Launchpad.',
            tip: 'Use USDC, USDT, or USDL instead.',
        },
    },
    {
        needles: ['zeroamount', 'zero amount'],
        pretty: {
            title: 'Amount is zero',
            reason: 'Cannot swap zero tokens.',
            tip: 'Enter a valid amount greater than zero.',
        },
    },

    // ── PECOR contract errors ────────────────────────────────────────────
    {
        needles: ['enforcedpause', 'paused'],
        pretty: {
            title: 'Exchange paused',
            reason: 'PECOR Exchange is temporarily paused for maintenance.',
            tip: 'Try again later or use the Launchpad route if available.',
        },
    },
    {
        needles: ['safeerc20failedoperation', 'failed operation'],
        pretty: {
            title: 'Token transfer failed',
            reason: 'The token contract rejected the transfer.',
            tip: 'Ensure you have enough balance and the token isn\u2019t restricted.',
        },
    },
    {
        needles: ['reentrancyguard', 'reentrant'],
        pretty: {
            title: 'Security block',
            reason: 'The contract blocked a re-entrant call for safety.',
            tip: 'Wait a moment and try again — this is a temporary protection.',
        },
    },
    {
        needles: ['stale', 'pricestale'],
        pretty: {
            title: 'Price data stale',
            reason: 'The price oracle hasn\u2019t updated recently, so quotes may be inaccurate.',
            tip: 'Wait a few minutes for the oracle to refresh, then retry.',
        },
    },
    {
        needles: ['insufficient liquidity', 'sufficientliquidity'],
        pretty: {
            title: 'Low liquidity',
            reason: 'The pool doesn\u2019t have enough liquidity for this swap size.',
            tip: 'Try a smaller amount or split into multiple swaps.',
        },
    },

    // ── Wallet / ethers errors ───────────────────────────────────────────
    {
        needles: ['insufficient funds', 'insufficient balance'],
        pretty: {
            title: 'Insufficient balance',
            reason: 'Your wallet doesn\u2019t have enough tokens for this swap (including gas).',
            tip: 'Lower the amount or add funds to your wallet.',
        },
    },
    {
        needles: ['user rejected', 'action_rejected', 'user denied'],
        pretty: {
            title: 'Transaction cancelled',
            reason: 'You rejected the transaction.',
            tip: 'Tap Confirm Swap when you\u2019re ready.',
        },
    },
    {
        needles: ['unpredictable_gas', 'cannot estimate gas'],
        pretty: {
            title: 'Transaction would fail',
            reason: 'The contract rejected this swap during simulation.',
            tip: 'Check that you have enough balance, the token pair is valid, and try increasing slippage.',
        },
    },
    {
        needles: ['call_exception', 'execution reverted'],
        pretty: {
            title: 'Contract error',
            reason: 'The swap contract reverted the transaction.',
            tip: 'Try a smaller amount, increase slippage, or swap through a different route.',
        },
    },
    {
        needles: ['timeout', 'network', 'could not detect'],
        pretty: {
            title: 'Network issue',
            reason: 'Could not reach the blockchain network.',
            tip: 'Check your internet connection and try again.',
        },
    },
    {
        needles: ['allowance', 'approve'],
        pretty: {
            title: 'Approval needed',
            reason: 'The router doesn\u2019t have permission to spend your tokens yet.',
            tip: 'The approval transaction may still be pending — wait a moment and retry.',
        },
    },

    // ── Quote errors ─────────────────────────────────────────────────────
    {
        needles: ['no routes found', 'no route'],
        pretty: {
            title: 'No route found',
            reason: 'No liquidity path exists between these tokens.',
            tip: 'Try a different token pair or a smaller amount.',
        },
    },
    {
        needles: ['quote failed'],
        pretty: {
            title: 'Quote unavailable',
            reason: 'Could not fetch a price for this swap.',
            tip: 'The pool may have low liquidity — try a smaller amount or different pair.',
        },
    },
];

const NONCE_TOO_LOW: PrettyError = {
    title: 'Nonce conflict',
    reason: 'A pending transaction is blocking this one.',
    tip: 'Wait for your pending transaction to confirm, then retry.',
};

const TRUNCATED_FALLBACK = (raw: string): PrettyError => ({
    title: 'Something went wrong',
    reason: raw.length > 120 ? raw.slice(0, 120) + '…' : raw,
    tip: 'Try again or use a different token pair.',
});

export type SwapErrorKind = 'gas' | 'revert' | 'timeout' | 'ratelimit' | null;

/**
 * Classify a raw error string into a modal kind.
 * Returns null for errors better shown inline (e.g. user cancel, no-route).
 */
export function classifySwapError(raw: string): SwapErrorKind {
    const e = (raw || '').toLowerCase();
    if (e.includes('429') || e.includes('rate limit') || e.includes('too many requests')) return 'ratelimit';
    if (e.includes('insufficient funds') || e.includes('insufficient balance')) return 'gas';
    if (e.includes('execution reverted') || e.includes('call_exception') || e.includes('missing revert')) return 'revert';
    if (e.includes('timeout') || e.includes('could not detect') || e.includes('network')) return 'timeout';
    return null;
}

export function prettifySwapError(raw: string): PrettyError {
    const msg = (raw || '').toLowerCase();

    // Special-case: nonce too low requires a 2-needle match.
    if (msg.includes('nonce') && msg.includes('too low')) return NONCE_TOO_LOW;

    for (const entry of ERROR_TABLE) {
        for (const needle of entry.needles) {
            if (msg.includes(needle)) return entry.pretty;
        }
    }

    // Special-case: estimateGas + missing revert data.
    if (msg.includes('missing revert data') && msg.includes('estimategas')) {
        return ERROR_TABLE.find((e) => e.needles.includes('unpredictable_gas'))!.pretty;
    }

    return TRUNCATED_FALLBACK(raw);
}
