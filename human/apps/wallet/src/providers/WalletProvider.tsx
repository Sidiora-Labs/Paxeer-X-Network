'use client';

import React, {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useState,
} from 'react';
import { ethers } from 'ethers';
import type { WalletAccount } from '@/lib/wallet';
import {
    EmbeddedSigner,
    FundedSigner,
    useOptionalEmbeddedWallet,
} from '@/lib/wallet';
import { getActiveRpcUrl } from '@/lib/constants';
import { useWalletKind } from '@/providers/WalletKindProvider';

// ── Public state ─────────────────────────────────────────────────────────────
//
// Identical shape across both custody models so the data layer
// (`useTokenBalances`, `useTransactions`, etc.) — which only reads
// `activeAccount.address` — keeps working unchanged.

export interface WalletState {
    ready: boolean;
    hasWallet: boolean;
    accounts: WalletAccount[];
    activeAccount: WalletAccount | null;
}

const initial: WalletState = {
    ready: false,
    hasWallet: false,
    accounts: [],
    activeAccount: null,
};

const signedOut: WalletState = {
    ready: true,
    hasWallet: false,
    accounts: [],
    activeAccount: null,
};

// ── Public actions ───────────────────────────────────────────────────────────
//
// Embedded-specific actions (sign-in, sign-out) live on the
// `EmbeddedWalletProvider` from `@paxport/wallet`. Components import
// `useEmbeddedWallet()` directly when they need those.

export interface WalletActions {
    send: (tx: { to: string; value: string; tokenAddress?: string; decimals?: number }) => Promise<string>;
    getReceiveAddress: () => Promise<string>;
    reset: () => Promise<void>;
    refresh: () => Promise<void>;

    // Embedded returns an `EmbeddedSigner` whose `sendTransaction()`
    // delegates to the embedded API server; funded returns a `FundedSigner`
    // that routes every transaction through the funded policy engine.
    getSigner: () => Promise<ethers.Signer>;
}

const StateCtx = createContext<WalletState>(initial);
const ActionsCtx = createContext<WalletActions | null>(null);

export function useWalletState() {
    return useContext(StateCtx);
}

export function useWalletActions() {
    const ctx = useContext(ActionsCtx);
    if (!ctx) throw new Error('useWalletActions must be used inside WalletProvider');
    return ctx;
}

// ── Errors ───────────────────────────────────────────────────────────────────

/**
 * Thrown when the UI attempts an action that's structurally disabled in
 * Funded mode — sending arbitrary tokens, receiving funds, off-ramping,
 * or anything that would require the funded account to move value
 * outside the tier whitelist. The PWA shell gates the entry points but
 * we throw here too as a defence-in-depth so a stray call site can't
 * accidentally surface a denied-tx error from the server.
 */
export class FundedNotSupportedError extends Error {
    constructor(action: string) {
        super(
            `[paxeer/wallet] "${action}" is disabled in Funded mode. ` +
            'Funded accounts can only interact with whitelisted contracts.',
        );
        this.name = 'FundedNotSupportedError';
    }
}

const fundedNotSupported = (action: string) => () => {
    throw new FundedNotSupportedError(action);
};

export class NoWalletSelectedError extends Error {
    constructor(action: string) {
        super(`[paxeer/wallet] "${action}" requires a signed-in wallet.`);
        this.name = 'NoWalletSelectedError';
    }
}

const noWalletSelected = (action: string) => () => {
    throw new NoWalletSelectedError(action);
};

// ── Provider ─────────────────────────────────────────────────────────────────

export function WalletProvider({ children }: { children: React.ReactNode }) {
    const { kind, hydrated, setKind } = useWalletKind();
    const embedded = useOptionalEmbeddedWallet();

    const [state, setState] = useState<WalletState>(initial);

    // ── Auto-promote on first hydrate ─────────────────────────────────
    //
    // A live Supabase session implies the user previously chose embedded
    // or funded custody; honour it even if the stored choice was wiped.
    // Otherwise leave `kind === null` so the welcome screen renders.
    useEffect(() => {
        if (!hydrated) return;
        if (kind !== null) return;
        let cancelled = false;

        void (async () => {
            if (embedded?.isAuthenticated) {
                // Promotion order matters: prefer 'funded' when the user
                // has a funded account but no standard wallet, so they
                // don't land on the embedded portfolio with an empty
                // address. The provider has already passively read both
                // sides by the time `isAuthenticated` flips true.
                if (embedded.fundedSelf && !embedded.publicWallet) {
                    if (!cancelled) setKind('funded');
                    return;
                }
                if (!cancelled) setKind('embedded');
            }
        })();

        return () => {
            cancelled = true;
        };
    }, [
        hydrated,
        kind,
        embedded?.isAuthenticated,
        embedded?.fundedSelf,
        embedded?.publicWallet,
        setKind,
    ]);

    // ── Funded refresh ──────────────────────────────────────────────────
    //
    // Mirrors `refreshEmbedded` but pulls the active address out of the
    // funded account record (`fundedSelf.wallet.address`) instead of the
    // standard wallet. Funded mode treats the funded EOA as the single
    // account exposed to the rest of the UI — balance hooks, swap, and
    // settings all read `activeAccount.address` polymorphically.
    const refreshFunded = useCallback(() => {
        if (!embedded) {
            setState({ ...initial, ready: hydrated });
            return;
        }
        const { isLoading, isAuthenticated, fundedSelf, user } = embedded;
        if (isLoading) {
            setState((prev) => ({ ...prev, ready: false }));
            return;
        }
        if (!isAuthenticated) {
            setState(signedOut);
            return;
        }
        if (!fundedSelf) {
            // Authenticated but no funded account yet — the onboarding
            // tier picker will provision one. Surfacing `hasWallet: false`
            // routes the shell back to the onboarding screen.
            setState(signedOut);
            return;
        }
        const account: WalletAccount = {
            id: `funded:${fundedSelf.wallet.address.toLowerCase()}`,
            kind: 'funded',
            address: fundedSelf.wallet.address,
            name:
                fundedSelf.tier?.label ??
                user?.email ??
                `${fundedSelf.wallet.address.slice(0, 6)}…${fundedSelf.wallet.address.slice(-4)}`,
            derivationPath: '',
            accountIndex: 0,
        };
        setState({
            ready: true,
            hasWallet: true,
            accounts: [account],
            activeAccount: account,
        });
    }, [embedded, hydrated]);

    // ── Embedded refresh ────────────────────────────────────────────────
    const refreshEmbedded = useCallback(() => {
        if (!embedded) {
            // Singleton not configured (no Supabase env). Show "not ready"
            // so the shell falls back to onboarding which will surface an
            // error tile.
            setState({ ...initial, ready: hydrated });
            return;
        }
        const { isLoading, isAuthenticated, publicWallet, user } = embedded;
        if (isLoading) {
            setState((prev) => ({ ...prev, ready: false }));
            return;
        }
        if (!isAuthenticated) {
            setState(signedOut);
            return;
        }
        if (!publicWallet) {
            // Authenticated but standard wallet not yet provisioned.
            //
            // Behaviour change: previously the provider auto-provisioned on
            // first read, so this state never persisted. With the dual-mode
            // rollout (a freshly signed-in user might be heading for the
            // funded tier picker instead) provisioning is now an explicit
            // step. We surface `hasWallet: false` so the shell falls back
            // to onboarding, which renders the `'embedded-setup'` step and
            // triggers `provisionStandard()`.
            setState(signedOut);
            return;
        }
        const account: WalletAccount = {
            id: `managed:${publicWallet.address.toLowerCase()}`,
            kind: 'managed',
            address: publicWallet.address,
            name:
                user?.email ??
                (user?.user_metadata?.name as string | undefined) ??
                `${publicWallet.address.slice(0, 6)}…${publicWallet.address.slice(-4)}`,
            derivationPath: '',
            accountIndex: 0,
        };
        setState({
            ready: true,
            hasWallet: true,
            accounts: [account],
            activeAccount: account,
        });
    }, [embedded, hydrated]);

    // ── Effect: drive state on kind / backend changes ──────────────────
    useEffect(() => {
        if (!hydrated) return;

        if (kind === null) {
            // No choice yet — drive welcome screen.
            setState(signedOut);
            return;
        }

        if (kind === 'embedded') {
            refreshEmbedded();
            return undefined;
        }

        // kind === 'funded'
        refreshFunded();
        return undefined;
    }, [kind, hydrated, refreshEmbedded, refreshFunded]);

    // Re-run embedded refresh when underlying Supabase state changes.
    useEffect(() => {
        if (!hydrated) return;
        if (kind !== 'embedded') return;
        refreshEmbedded();
    }, [
        hydrated,
        kind,
        embedded?.isLoading,
        embedded?.isAuthenticated,
        embedded?.publicWallet?.address,
        refreshEmbedded,
    ]);

    // Re-run funded refresh when underlying Supabase state or the funded
    // account record changes (e.g. immediately after provisioning).
    useEffect(() => {
        if (!hydrated) return;
        if (kind !== 'funded') return;
        refreshFunded();
    }, [
        hydrated,
        kind,
        embedded?.isLoading,
        embedded?.isAuthenticated,
        embedded?.fundedSelf?.wallet.address,
        refreshFunded,
    ]);

    // ── Actions ─────────────────────────────────────────────────────────
    const actions = useMemo<WalletActions>(() => {
        // Default refresh dispatches based on the current kind.
        const refresh = async () => {
            if (kind === 'embedded') return refreshEmbedded();
            if (kind === 'funded') return refreshFunded();
            return;
        };

        if (kind === 'funded') {
            return {
                // Sends are policy-gated server-side. The UI hides direct
                // send entry points; this path exists so that
                // whitelisted-contract sends (e.g. an in-app swap that
                // touches an approve+swap pair) still route through the
                // funded endpoint.
                send: async (tx) => {
                    if (!embedded?.fundedWallet) {
                        throw new Error('Funded wallet not configured');
                    }
                    const hash = await embedded.fundedWallet.send(tx);
                    embedded.refreshFunded();
                    return hash;
                },
                // Funded mode hides the Receive screen. We throw here so a
                // stray caller can't silently copy the funded address to
                // the clipboard via the standard Receive flow.
                getReceiveAddress: fundedNotSupported('getReceiveAddress'),
                reset: async () => {
                    if (embedded?.fundedWallet) await embedded.fundedWallet.reset();
                },
                refresh,
                // Funded swaps run through `FundedSigner`, which presents
                // the same `ethers.Signer` surface but every
                // `sendTransaction` runs through the funded policy engine.
                // The swap SDK's approve+swap flow works unchanged as
                // long as both targets are in the tier whitelist.
                getSigner: async () => {
                    if (!embedded?.fundedWallet) {
                        throw new Error('Funded wallet not configured');
                    }
                    const self = await embedded.fundedWallet.getFundedSelf();
                    if (!self) {
                        throw new Error(
                            'Funded account not provisioned — cannot construct signer',
                        );
                    }
                    return new FundedSigner({
                        client: embedded.fundedWallet.client,
                        address: self.wallet.address,
                        rpcUrl: getActiveRpcUrl(),
                        chainId: self.wallet.chain_id,
                    });
                },
            };
        }

        if (kind === 'embedded') {
            return {
                send: async (tx) => {
                    if (!embedded?.wallet) throw new Error('Embedded wallet not configured');
                    return embedded.wallet.send(tx);
                },
                getReceiveAddress: async () => {
                    if (!embedded?.wallet) throw new Error('Embedded wallet not configured');
                    return embedded.wallet.getReceiveAddress();
                },
                reset: async () => {
                    if (embedded?.wallet) await embedded.wallet.reset();
                },
                refresh,
                // Embedded swaps run through `EmbeddedSigner`, which presents
                // the same `ethers.Signer` surface as a local `ethers.Wallet`
                // but delegates `sendTransaction` to the embedded API server.
                getSigner: async () => {
                    if (!embedded?.wallet) throw new Error('Embedded wallet not configured');
                    const info = await embedded.wallet.getWalletInfo();
                    return new EmbeddedSigner({
                        client: embedded.wallet.client,
                        address: info.wallet.address,
                        rpcUrl: getActiveRpcUrl(),
                        chainId: info.chain.id,
                    });
                },
            };
        }

        return {
            send: noWalletSelected('send'),
            getReceiveAddress: noWalletSelected('getReceiveAddress'),
            reset: async () => {},
            refresh,
            getSigner: noWalletSelected('getSigner'),
        };
    }, [kind, embedded, refreshEmbedded, refreshFunded]);

    return (
        <StateCtx.Provider value={state}>
            <ActionsCtx.Provider value={actions}>{children}</ActionsCtx.Provider>
        </StateCtx.Provider>
    );
}
