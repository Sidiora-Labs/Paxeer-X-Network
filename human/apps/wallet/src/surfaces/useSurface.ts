'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
    SIDIORA_FEE_DENOM,
    feeToken,
    type FeeChoiceId,
    type ModuleProvider,
    type ModuleTransaction,
} from '@paxeer/wallet';
import { useWallet } from '@/wallet/WalletProvider';

export interface SurfaceLog {
    readonly address: string;
    readonly topics: readonly string[];
    readonly data: string;
}

export interface SurfaceEvent {
    readonly event: string;
    readonly fields: Readonly<Record<string, bigint | boolean | string>>;
}

export interface SurfaceModule {
    readonly address: string;
    send(from: string, tx: ModuleTransaction): Promise<string>;
    decodeEvent(log: SurfaceLog): SurfaceEvent;
}

export type ReceiptStatus = 'pending' | 'confirmed' | 'reverted';

export interface SentTransaction {
    readonly hash: string;
    readonly status: ReceiptStatus;
    readonly events: readonly SurfaceEvent[];
}

export interface SurfaceWallet {
    readonly provider: ModuleProvider | null;
    readonly address: string | null;
}

export const RECEIPT_ATTEMPTS = 30;
export const RECEIPT_INTERVAL_MS = 1_000;

export function useSurfaceWallet(): SurfaceWallet {
    const { status, wallet, address } = useWallet();
    return useMemo(
        () => (status === 'ready' && wallet && address ? { provider: wallet.provider, address } : { provider: null, address: null }),
        [status, wallet, address],
    );
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isLog(value: unknown): value is SurfaceLog {
    return (
        isRecord(value) &&
        typeof value.address === 'string' &&
        typeof value.data === 'string' &&
        Array.isArray(value.topics) &&
        value.topics.every((topic) => typeof topic === 'string')
    );
}

export function receiptEvents(receipt: unknown, surfaceModule: Pick<SurfaceModule, 'address' | 'decodeEvent'>): SentTransaction['events'] {
    if (!isRecord(receipt) || !Array.isArray(receipt.logs)) throw new Error('the receipt carries no logs');
    return receipt.logs
        .filter(isLog)
        .filter((log) => log.address.toLowerCase() === surfaceModule.address)
        .map((log) => surfaceModule.decodeEvent(log));
}

export function receiptStatus(receipt: unknown): ReceiptStatus {
    if (!isRecord(receipt)) return 'pending';
    return receipt.status === '0x1' ? 'confirmed' : 'reverted';
}

export async function waitForReceipt(provider: ModuleProvider, hash: string, attempts = RECEIPT_ATTEMPTS, intervalMs = RECEIPT_INTERVAL_MS): Promise<unknown> {
    for (let attempt = 0; attempt < attempts; attempt += 1) {
        const receipt = await provider.request({ method: 'eth_getTransactionReceipt', params: [hash] });
        if (receipt !== null && receipt !== undefined) return receipt;
        await new Promise((resolve) => setTimeout(resolve, intervalMs));
    }
    return null;
}

export function formatField(value: bigint | boolean | string): string {
    return typeof value === 'bigint' ? value.toString(10) : String(value);
}

export function errorMessage(error: unknown): string {
    return error instanceof Error && error.message ? error.message : 'the request failed';
}

export interface ModuleSend {
    readonly sending: boolean;
    readonly sent: SentTransaction | null;
    readonly error: string | null;
    readonly send: (tx: ModuleTransaction) => Promise<void>;
}

export function useModuleSend(surfaceModule: SurfaceModule | null, provider: ModuleProvider | null, address: string | null): ModuleSend {
    const [sending, setSending] = useState(false);
    const [sent, setSent] = useState<SentTransaction | null>(null);
    const [error, setError] = useState<string | null>(null);
    const alive = useRef(true);
    useEffect(() => {
        alive.current = true;
        return () => {
            alive.current = false;
        };
    }, []);

    const send = useCallback(
        async (tx: ModuleTransaction) => {
            if (!surfaceModule || !provider || !address) {
                setError('connect a wallet first');
                return;
            }
            setSending(true);
            setError(null);
            setSent(null);
            try {
                const hash = await surfaceModule.send(address, tx);
                if (alive.current) setSent({ hash, status: 'pending', events: [] });
                const receipt = await waitForReceipt(provider, hash);
                if (!alive.current) return;
                if (receipt === null) {
                    setSent({ hash, status: 'pending', events: [] });
                    return;
                }
                setSent({ hash, status: receiptStatus(receipt), events: receiptEvents(receipt, surfaceModule) });
            } catch (cause) {
                if (alive.current) setError(errorMessage(cause));
            } finally {
                if (alive.current) setSending(false);
            }
        },
        [surfaceModule, provider, address],
    );

    return { sending, sent, error, send };
}

export interface FeeSelection {
    readonly choice: FeeChoiceId;
    readonly setChoice: (choice: FeeChoiceId) => void;
    readonly feeDenom: string | null;
    readonly denomError: string | null;
    readonly updating: boolean;
    readonly blocked: string | null;
    readonly applyPreference: () => Promise<void>;
}

export const SPONSORED_UNAVAILABLE = 'the gas station quote service is not configured for this app';

export function feeBlocked(choice: FeeChoiceId, feeDenom: string | null): string | null {
    if (feeDenom === null) return 'the current fee token is not known yet';
    if (choice === 'sid_sponsored') return SPONSORED_UNAVAILABLE;
    if (choice === 'sid_native' && feeDenom !== SIDIORA_FEE_DENOM) return `set ${SIDIORA_FEE_DENOM} as the fee token first`;
    if (choice === 'pax_gas' && feeDenom !== '') return 'clear the fee token preference first';
    return null;
}

export function useFeeSelection(provider: ModuleProvider | null, address: string | null): FeeSelection {
    const [choice, setChoice] = useState<FeeChoiceId>('pax_gas');
    const [feeDenom, setFeeDenom] = useState<string | null>(null);
    const [denomError, setDenomError] = useState<string | null>(null);
    const [updating, setUpdating] = useState(false);
    const surfaceModule = useMemo(() => (provider ? feeToken(provider) : null), [provider]);

    const read = useCallback(async () => {
        if (!surfaceModule || !address) return;
        try {
            const denom = await surfaceModule.getFeeDenom(address);
            setFeeDenom(denom);
            setDenomError(null);
        } catch (cause) {
            setDenomError(errorMessage(cause));
        }
    }, [surfaceModule, address]);

    useEffect(() => {
        setFeeDenom(null);
        void read();
    }, [read]);

    const applyPreference = useCallback(async () => {
        if (!surfaceModule || !provider || !address) return;
        const tx = choice === 'sid_native' ? surfaceModule.setFeeDenom(SIDIORA_FEE_DENOM) : surfaceModule.clearFeeDenom();
        setUpdating(true);
        try {
            const hash = await surfaceModule.send(address, tx);
            const receipt = await waitForReceipt(provider, hash);
            if (receiptStatus(receipt) !== 'confirmed') throw new Error('the fee token preference was not applied');
            await read();
        } catch (cause) {
            setDenomError(errorMessage(cause));
        } finally {
            setUpdating(false);
        }
    }, [surfaceModule, provider, address, choice, read]);

    return { choice, setChoice, feeDenom, denomError, updating, blocked: feeBlocked(choice, feeDenom), applyPreference };
}
