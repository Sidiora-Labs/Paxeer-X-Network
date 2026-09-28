'use client';

/**
 * Send-form state machine.
 *
 * Owns the recipient address, amount, error/loading flags, the confirm-drawer
 * open state, and the resulting tx hash. Wires to {@link useWalletActions} for
 * the actual transfer and persists a pending optimistic event so the
 * portfolio screen can subtract immediately on the next mount.
 */

import { useCallback, useState } from 'react';
import { useWalletActions } from '@/providers/WalletProvider';
import { storePendingSend } from '@/lib/optimistic';
import { saveRecentRecipient } from '@/lib/recentRecipients';
import {
    validateEvmAddress,
    validateSpendableBalance,
    validateTokenAmount,
} from '@/lib/txValidation';
import type { SendableToken } from './useSendableTokens';

export interface UseSendFormResult {
    to: string;
    amount: string;
    loading: boolean;
    error: string;
    txHash: string;
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

export function useSendForm(): UseSendFormResult {
    const { send } = useWalletActions();

    const [to, setTo] = useState('');
    const [amount, setAmount] = useState('');
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [txHash, setTxHash] = useState('');
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
            if (!to || !amount || !token) return;
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
                setTxHash(hash);
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
        [to, amount, send],
    );

    return {
        to,
        amount,
        loading,
        error,
        txHash,
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
