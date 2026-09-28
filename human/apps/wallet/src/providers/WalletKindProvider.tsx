'use client';

import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useState,
    type ReactNode,
} from 'react';
import type { WalletKind } from '@/lib/wallet';
import { custodyChoiceRepository } from '@/platform/storage/repositories';

/**
 * `WalletKindProvider` — owns the user's choice between the two custody
 * models the PaxPort wallet ships with:
 *
 *   - `'self-custody'` — PIN + BIP39 mnemonic, authenticated in
 *     storage. The current behavior, byte-for-byte.
 *
 *   - `'embedded'`     — Paxeer-managed custody via Supabase auth +
 *     `connect.paxportwallet.com`. Same wallet on every Paxeer app.
 *
 * Persisted in `localStorage['paxeer:wallet-kind']`. `null` means the user
 * has not chosen yet — the shell renders the onboarding welcome screen so
 * they can pick.
 *
 * The shape is deliberately tiny. Everything else (the actual wallet
 * facade, balances, signing, etc.) is dispatched off `kind` inside
 * `WalletProvider`. This provider only owns the *choice*.
 */

export type WalletKindValue = WalletKind | null;

export interface WalletKindContextValue {
    /** The user's selected custody model, or null if not yet chosen. */
    kind: WalletKindValue;
    /** True before the first read from localStorage completes (SSR-safe gate). */
    hydrated: boolean;
    /** Persist a new choice. Triggers a re-render across the tree. */
    setKind: (next: WalletKind) => void;
    /** Forget the current choice — used on "switch wallet mode" / full reset. */
    clearKind: () => void;
}

const WalletKindContext = createContext<WalletKindContextValue | null>(null);

function readStoredKind(): WalletKindValue {
    if (typeof window === 'undefined') return null;
    return custodyChoiceRepository.read();
}

export function WalletKindProvider({ children }: { children: ReactNode }) {
    // Start as `null` on every render path so SSR and the first client render
    // agree — we hydrate from localStorage in an effect to avoid a mismatch.
    const [kind, setKindState] = useState<WalletKindValue>(null);
    const [hydrated, setHydrated] = useState(false);

    useEffect(() => {
        setKindState(readStoredKind());
        setHydrated(true);
    }, []);

    const setKind = useCallback((next: WalletKind) => {
        custodyChoiceRepository.write(next);
        setKindState(next);
    }, []);

    const clearKind = useCallback(() => {
        custodyChoiceRepository.remove();
        setKindState(null);
    }, []);

    const value = useMemo<WalletKindContextValue>(
        () => ({ kind, hydrated, setKind, clearKind }),
        [kind, hydrated, setKind, clearKind],
    );

    return (
        <WalletKindContext.Provider value={value}>
            {children}
        </WalletKindContext.Provider>
    );
}

/**
 * Read the current custody choice. Throws if used outside
 * `<WalletKindProvider>` to make wiring mistakes loud during development.
 */
export function useWalletKind(): WalletKindContextValue {
    const ctx = useContext(WalletKindContext);
    if (!ctx) {
        throw new Error('useWalletKind() must be used inside <WalletKindProvider>');
    }
    return ctx;
}
