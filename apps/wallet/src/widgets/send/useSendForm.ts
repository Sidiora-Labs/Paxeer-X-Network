'use client';

/**
 * Send-form state machine.
 *
 * Owns the recipient address, amount, error/loading flags, the confirm-drawer
 * open state, and the resulting tx hash. Wires to {@link useWalletActions} for
 * the actual transfer and persists a pending optimistic event so the
 * portfolio screen can subtract immediately on the next mount.
 */

import { useCallback, useEffect, useState } from 'react';
import { submittedTransfer, type TransferIdentity } from '@paxeer/wallet';
import { PAXEER_CONFIG } from '@/lib/constants';
import { useWalletActions, useWalletState } from '@/providers/WalletProvider';
import { storePendingSend } from '@/lib/optimistic';
import { saveRecentRecipient } from '@/lib/recentRecipients';
import {
    validateEvmAddress,
    validateSpendableBalance,
    validateTokenAmount,
} from '@/lib/txValidation';
import type { SendableToken } from './useSendableTokens';

export interface SubmittedTransfer extends TransferIdentity {
    sender: string;
    symbol: string;
    decimals: number;
    recipient: string;
    amount: string;
}

export interface UseSendFormResult {
    to: string;
    amount: string;
    loading: boolean;
    error: string;
    txHash: string;
    transfer: SubmittedTransfer | null;
    clearTransfer: () => void;
    confirmOpen: boolean;

    setTo: (value: string) => void;
    setAmount: (value: string) => void;
    applyPercentage: (pct: number, token: SendableToken | null) => void;

    openConfirm: (token?: SendableToken | null) => void;
    closeConfirm: () => void;
    validateBeforeConfirm: (token: SendableToken | null) => boolean;
    submit: (token: SendableToken | null) => Promise<void>;
}

const computePortion = (
    balanceRaw: string,
    pct: number,
    decimals: number,
): string | null => {
    try {
        const raw = BigInt(balanceRaw);
        const portion = (raw * BigInt(pct)) / BigInt(100);
        const divisor = BigInt(10) ** BigInt(decimals);
        const whole = portion / divisor;
        const frac = (portion % divisor).toString().padStart(decimals, '0');
        const trimmed = frac.replace(/0+$/, '');
        return trimmed ? `${whole}.${trimmed}` : `${whole}`;
    } catch {
        return null;
    }
};

export const SUBMITTED_TRANSFER_KEY = 'paxeer.wallet.submittedTransfer';

function readSubmittedTransfer(sender: string): SubmittedTransfer | null {
    try {
        const raw = window.localStorage.getItem(`${SUBMITTED_TRANSFER_KEY}:${PAXEER_CONFIG.chainId}:${sender.toLowerCase()}`);
        if (!raw) return null;
        const value = JSON.parse(raw) as Partial<SubmittedTransfer>;
        if (typeof value.hash !== 'string' || typeof value.chainId !== 'number' ||
            typeof value.sender !== 'string' || value.sender.toLowerCase() !== sender.toLowerCase() ||
            typeof value.symbol !== 'string' || !value.symbol || typeof value.decimals !== 'number' ||
            !Number.isInteger(value.decimals) || value.decimals < 0 || value.decimals > 36 || typeof value.recipient !== 'string' ||
            typeof value.amount !== 'string' || !value.intent || value.intent.sender?.toLowerCase() !== sender.toLowerCase() ||
            value.intent.recipient?.toLowerCase() !== value.recipient.toLowerCase()) return null;
        const identity = submittedTransfer({ hash: value.hash, chainId: value.chainId, intent: value.intent }).identity;
        if (identity.chainId !== PAXEER_CONFIG.chainId) return null;
        validateEvmAddress(value.recipient);
        if (validateTokenAmount(value.amount, value.decimals).toString() !== value.intent.amountRaw) return null;
        return { ...identity, sender: value.sender, symbol: value.symbol, decimals: value.decimals, recipient: value.recipient, amount: value.amount };
    } catch {
        return null;
    }
}

export function useSendForm(): UseSendFormResult {
    const { send } = useWalletActions();
    const { activeAccount } = useWalletState();
    const sender = activeAccount?.address;

    const [to, setTo] = useState('');
    const [amount, setAmount] = useState('');
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [transfer, setTransfer] = useState<SubmittedTransfer | null>(null);
    const activeTransfer = transfer?.sender.toLowerCase() === sender?.toLowerCase() ? transfer : null;
    const txHash = activeTransfer?.hash ?? '';

    useEffect(() => {
        const recovered = sender ? readSubmittedTransfer(sender) : null;
        setTransfer(recovered);
        if (recovered) {
            setTo(recovered.recipient);
            setAmount(recovered.amount);
        }
    }, [sender]);

    const clearTransfer = useCallback(() => {
        try {
            if (sender) window.localStorage.removeItem(`${SUBMITTED_TRANSFER_KEY}:${PAXEER_CONFIG.chainId}:${sender.toLowerCase()}`);
        } catch {
            // storage unavailable: the in-memory identity is still cleared
        }
        setTransfer(null);
    }, [sender]);
    const [confirmOpen, setConfirmOpen] = useState(false);

    const applyPercentage = useCallback((pct: number, token: SendableToken | null) => {
        if (!token || !token.balanceRaw || token.balanceRaw === '0') return;
        const result = computePortion(token.balanceRaw, pct, token.decimals);
        if (result !== null) {
            setAmount(result);
        } else if (pct === 100 && token.balance) {
            setAmount(token.balance);
        }
    }, []);

    const validateBeforeConfirm = useCallback((token: SendableToken | null): boolean => {
        try {
            if (!token) throw new Error('Select a token to send.');
            validateEvmAddress(to);
            const amountRaw = validateTokenAmount(amount, token.decimals);
            validateSpendableBalance(amountRaw, token.balanceRaw);
            setError('');
            return true;
        } catch (e) {
            setError(e instanceof Error ? e.message : 'Invalid transaction');
            return false;
        }
    }, [to, amount]);

    const openConfirm = useCallback((token?: SendableToken | null) => {
        if (token !== undefined && !validateBeforeConfirm(token)) return;
        setError('');
        setConfirmOpen(true);
    }, [validateBeforeConfirm]);

    const closeConfirm = useCallback(() => {
        setConfirmOpen(false);
        setError('');
    }, []);

    const submit = useCallback(
        async (token: SendableToken | null) => {
            if (!to || !amount || !token || !sender) return;
            setLoading(true);
            setError('');
            try {
                const recipient = validateEvmAddress(to);
                const amountRaw = validateTokenAmount(amount, token.decimals);
                validateSpendableBalance(amountRaw, token.balanceRaw);
                const hash = await send({
                    to: recipient,
                    value: amount,
                    tokenAddress: token.address,
                    decimals: token.decimals,
                });
                const identity: SubmittedTransfer = {
                    ...submittedTransfer({ hash, chainId: PAXEER_CONFIG.chainId, intent: {
                        sender, recipient, amountRaw: amountRaw.toString(), tokenAddress: token.address,
                    } }).identity,
                    sender, recipient, amount, symbol: token.symbol, decimals: token.decimals,
                };
                try {
                    window.localStorage.setItem(`${SUBMITTED_TRANSFER_KEY}:${identity.chainId}:${sender.toLowerCase()}`, JSON.stringify(identity));
                } catch {
                    setError('Transfer submitted. This browser could not save its identity for reload.');
                }
                setTransfer(identity);
                saveRecentRecipient(recipient);
                storePendingSend({
                    tokenAddress: token.address,
                    symbol: token.symbol,
                    amount,
                    decimals: token.decimals,
                    recipient,
                    txHash: hash,
                    timestamp: Date.now(),
                });
            } catch (e) {
                setError(e instanceof Error ? e.message : 'Transaction failed');
            } finally {
                setLoading(false);
            }
        },
        [to, amount, send, sender],
    );

    return {
        to,
        amount,
        loading,
        error,
        txHash,
        transfer: activeTransfer,
        clearTransfer,
        confirmOpen,
        setTo,
        setAmount,
        applyPercentage,
        openConfirm,
        closeConfirm,
        validateBeforeConfirm,
        submit,
    };
}
