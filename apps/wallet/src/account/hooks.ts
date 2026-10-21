'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import type { KernelAvailabilityState, UnifiedAccountDocument } from '@paxeer/wallet';
import { useAccountClients } from './AccountProvider';

export type AsyncState<T> =
    | { readonly status: 'loading' }
    | { readonly status: 'ready'; readonly value: T }
    | { readonly status: 'error'; readonly error: unknown };

export function errorMessage(error: unknown): string {
    return error instanceof Error && error.message ? error.message : 'the request failed';
}

export function useAsync<T>(load: () => Promise<T>, key: string): AsyncState<T> & { readonly reload: () => void } {
    const [state, setState] = useState<AsyncState<T>>({ status: 'loading' });
    const [generation, setGeneration] = useState(0);
    const loadRef = useRef(load);
    loadRef.current = load;
    useEffect(() => {
        let alive = true;
        setState({ status: 'loading' });
        loadRef.current().then(
            (value) => {
                if (alive) setState({ status: 'ready', value });
            },
            (error: unknown) => {
                if (alive) setState({ status: 'error', error });
            },
        );
        return () => {
            alive = false;
        };
    }, [key, generation]);
    const reload = useCallback(() => setGeneration((value) => value + 1), []);
    return { ...state, reload };
}

export function useKernelState(): AsyncState<KernelAvailabilityState> & { readonly reload: () => void } {
    const { kernel } = useAccountClients();
    return useAsync(() => kernel.current(), 'kernel');
}

export function useAccountDocument(account: string): AsyncState<UnifiedAccountDocument> & { readonly reload: () => void } {
    const { endpoint } = useAccountClients();
    return useAsync(() => endpoint.resolveAccount(account), `account:${account.toLowerCase()}`);
}

export type KernelGate =
    | { readonly open: true }
    | { readonly open: false; readonly reason: string; readonly checking?: true };

export function kernelGate(state: AsyncState<KernelAvailabilityState>): KernelGate {
    if (state.status === 'loading') return { open: false, reason: 'Checking the LayerX kernel state', checking: true };
    if (state.status === 'error') {
        return { open: false, reason: `The endpoint did not report the LayerX kernel state: ${errorMessage(state.error)}` };
    }
    return kernelStateGate(state.value);
}

export function kernelStateGate(state: KernelAvailabilityState): KernelGate {
    if (state.available) return { open: true };
    return { open: false, reason: kernelReasonText(state) };
}

export function kernelReasonText(state: KernelAvailabilityState): string {
    if (state.available) return 'The LayerX kernel is available';
    const backend = state.backend ? ` (${state.backend.replace(/_/g, ' ')})` : '';
    switch (state.reason) {
        case 'not_configured':
            return `The LayerX kernel is not configured on the endpoint${backend}`;
        case 'unreachable':
            return `The LayerX kernel is unreachable${backend}`;
        case 'no_finalised_checkpoint':
            return 'The LayerX kernel has no finalised checkpoint on the chain yet';
    }
}
