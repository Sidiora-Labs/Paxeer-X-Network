/**
 * React bindings for the Paxeer Embedded Wallet — the high-level surface
 * frontend devs use to wire embedded auth into the PaxPort PWA.
 *
 * Three things shipped from this file:
 *
 *   1. `<EmbeddedWalletProvider>` — context provider that wires up the
 *      singleton, hydrates the session, and provisions the wallet. Drop it
 *      in the PWA tree once, anywhere above the screens that need it.
 *
 *   2. `useEmbeddedWallet()` — primary hook. Returns the consolidated
 *      auth + wallet state plus all the action handlers a sign-in screen
 *      or wallet panel needs. This is the only hook most components need.
 *
 *   3. `useEmbeddedAvailability()` — boolean check for "is the embedded
 *      wallet feature wired up at all". Use it to gate the UI when the
 *      app is running without `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` set
 *      (e.g. in a fork or local dev where only self-custody is desired).
 *
 * `react` is a peer dependency declared by the consumer of /paxport/wallet.
 */

import {
    createContext,
    createElement,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useState,
    type ReactNode,
} from 'react';
import type { Session, User } from '@supabase/supabase-js';
import { EmbeddedWallet } from './EmbeddedWallet';
import { FundedWallet } from './FundedWallet';
import {
    getAuthRedirectUrl,
    getEmbeddedWallet,
    type EmbeddedWalletOptions,
} from './client';
import type {
    ChainInfo,
    FundedProvisionResponse,
    FundedSelfResponse,
    FundedTier,
    OAuthProvider,
    PublicWallet,
} from './sdk/types';

// ── Types ────────────────────────────────────────────────────────────

export type EmbeddedSignInProvider = OAuthProvider;

export interface EmbeddedWalletContextValue {
    /** The embedded facade, or null when env is missing / SSR. */
    wallet: EmbeddedWallet | null;
    /** The funded facade — shares the same Supabase session as `wallet`. */
    fundedWallet: FundedWallet | null;
    session: Session | null;
    user: User | null;
    /** Server-side standard wallet record (id, address, chain). */
    publicWallet: PublicWallet | null;
    /** Live chain info from the server (RPC URL, explorer URL). */
    chain: ChainInfo | null;

    /** True before the first session-hydrate completes. */
    isLoading: boolean;
    /** True if a Supabase session is present (regardless of wallet state). */
    isAuthenticated: boolean;
    /** True if a session AND a provisioned standard wallet exist (the "fully ready" state for embedded mode). */
    isReady: boolean;
    /** True while an auth call (signIn / signOut) is in flight. */
    authBusy: boolean;
    /** Last auth or wallet error message, surfaceable to the user. */
    authError: string | null;

    signInWithEmail: (email: string) => Promise<{ ok: boolean; error?: string }>;
    signInWithOAuth: (provider: EmbeddedSignInProvider) => Promise<void>;
    signOut: () => Promise<void>;
    /** Force a re-fetch of the wallet record (e.g. after a successful tx). */
    refresh: () => void;
    /**
     * Explicitly provision a standard embedded wallet for the current
     * session. Required because the provider does NOT auto-provision
     * anymore — a freshly signed-in user might be heading for the funded
     * tier picker instead. Embedded onboarding calls this after the user
     * commits to embedded mode.
     */
    provisionStandard: () => Promise<PublicWallet>;

    // ── Funded account surface ─────────────────────────────────────────
    // Same Supabase session, separate server-side resource. Funded state
    // is fetched once on auth so the UI can tell whether the user already
    // has a funded account before showing the tier picker. Polling for
    // live equity / drawdown updates is opt-in via `useFundedAccountLive`.

    /** Funded account record + tier + balances + whitelist. Null when not provisioned. */
    fundedSelf: FundedSelfResponse | null;
    /** Public list of active tiers (loaded lazily on first access). */
    fundedTiers: FundedTier[];
    /** True while a `provisionFunded` call is in flight. */
    fundedBusy: boolean;
    /** Last funded-specific error (provision, fetch, or tier listing failure). */
    fundedError: string | null;
    /** Provision a funded account in the given tier. Idempotent. */
    provisionFunded: (tier_id?: string) => Promise<FundedProvisionResponse>;
    /** Force a re-fetch of the funded record (e.g. after a successful tx). */
    refreshFunded: () => void;
}

const EmbeddedWalletContext = createContext<EmbeddedWalletContextValue | null>(null);

// ── Provider ─────────────────────────────────────────────────────────

export interface EmbeddedWalletProviderProps {
    children: ReactNode;
    /**
     * Optional explicit config. When omitted, the provider falls back to
     * `getEmbeddedWallet()` which reads from `process.env`.
     */
    config?: EmbeddedWalletOptions;
    /**
     * Optional explicit redirect URL for OAuth providers and magic links.
     * Defaults to `${window.location.origin}/auth/callback`.
     */
    redirectTo?: string;
}

export function EmbeddedWalletProvider({
    children,
    config,
    redirectTo,
}: EmbeddedWalletProviderProps) {
    const wallet = useMemo<EmbeddedWallet | null>(
        () => getEmbeddedWallet(config),
        // Singleton key is `(apiUrl, supabaseUrl, supabaseAnonKey)`; we surface
        // those three to the dep array so a parent that swaps Supabase project
        // mid-flight rebuilds the wallet.
        [config?.apiUrl, config?.supabaseUrl, config?.supabaseAnonKey], // eslint-disable-line react-hooks/exhaustive-deps
    );

    // Funded facade — reuses the embedded wallet's Supabase client so the
    // two share one auth session. We rebuild it only when the embedded
    // singleton itself rebuilds (i.e. when the Supabase config changes).
    const fundedWallet = useMemo<FundedWallet | null>(() => {
        if (!wallet) return null;
        return new FundedWallet(
            {
                apiUrl: '',           // unused — client is passed via `deps.client`
                supabaseUrl: '',
                supabaseAnonKey: '',
            },
            { client: wallet.client },
        );
    }, [wallet]);

    const [session, setSession] = useState<Session | null>(null);
    const [user, setUser] = useState<User | null>(null);
    const [publicWallet, setPublicWallet] = useState<PublicWallet | null>(null);
    const [chain, setChain] = useState<ChainInfo | null>(null);
    const [isLoading, setIsLoading] = useState(true);
    const [authBusy, setAuthBusy] = useState(false);
    const [authError, setAuthError] = useState<string | null>(null);
    const [refreshTick, setRefreshTick] = useState(0);

    // ── Funded state ────────────────────────────────────────────────────
    const [fundedSelf, setFundedSelf] = useState<FundedSelfResponse | null>(null);
    const [fundedTiers, setFundedTiers] = useState<FundedTier[]>([]);
    const [fundedBusy, setFundedBusy] = useState(false);
    const [fundedError, setFundedError] = useState<string | null>(null);
    const [fundedRefreshTick, setFundedRefreshTick] = useState(0);

    // Hydrate session + subscribe to auth state changes.
    useEffect(() => {
        if (!wallet) {
            setIsLoading(false);
            return;
        }
        let alive = true;
        void (async () => {
            const s = await wallet.getSession();
            if (!alive) return;
            setSession(s);
            setUser(s?.user ?? null);
        })();
        const unsub = wallet.onAuthStateChange((_event, s) => {
            setSession(s);
            setUser(s?.user ?? null);
            if (!s) {
                setPublicWallet(null);
                setChain(null);
            }
        });
        return () => {
            alive = false;
            unsub();
        };
    }, [wallet]);

    // Provision/fetch the wallet record whenever we have an auth session.
    //
    // Behaviour change for the funded-mode rollout: we no longer
    // auto-provision a standard wallet here. Auto-provisioning was the
    // right call when the only signed-in flow was embedded; with funded
    // accounts in play, a freshly signed-in user might be heading for the
    // tier picker instead. We now use the SDK's standard-self read endpoint
    // (which returns `null` instead of auto-provisioning) and only fall
    // through to provisioning when the caller has explicitly committed to
    // embedded mode (i.e. `kind === 'embedded'` in `WalletProvider`).
    useEffect(() => {
        if (!wallet) {
            setIsLoading(false);
            return;
        }
        let alive = true;
        setIsLoading(true);
        void (async () => {
            try {
                if (!session) {
                    if (alive) {
                        setPublicWallet(null);
                        setChain(null);
                        setIsLoading(false);
                    }
                    return;
                }
                // Passive read — returns null if the user has no standard wallet
                // yet (e.g. funded-only user). The kind-aware caller decides
                // whether to provision.
                const r = await wallet.client.getStandardSelf();
                if (!alive) return;
                if (r) {
                    setPublicWallet(r.wallet);
                    setChain(r.chain);
                } else {
                    setPublicWallet(null);
                    setChain(null);
                }
                setAuthError(null);
            } catch (err) {
                if (alive) setAuthError((err as Error).message);
            } finally {
                if (alive) setIsLoading(false);
            }
        })();
        return () => {
            alive = false;
        };
    }, [wallet, session, refreshTick]);

    // ── Funded self — passive read on auth, refresh on tick ─────────────
    //
    // Returns null when the user has no funded account yet (the cue to show
    // the tier picker). Cheap call — single DB lookup server-side — so we
    // run it on every session change. Live polling for equity / drawdown
    // updates is opt-in via `useFundedAccountLive` to avoid wasted API
    // calls for embedded users.
    useEffect(() => {
        if (!fundedWallet) return;
        let alive = true;
        void (async () => {
            try {
                if (!session) {
                    if (alive) {
                        setFundedSelf(null);
                        setFundedError(null);
                    }
                    return;
                }
                const r = await fundedWallet.getFundedSelf();
                if (!alive) return;
                setFundedSelf(r);
                setFundedError(null);
            } catch (err) {
                if (alive) {
                    setFundedSelf(null);
                    setFundedError((err as Error).message || 'Failed to load funded account');
                }
            }
        })();
        return () => {
            alive = false;
        };
    }, [fundedWallet, session, fundedRefreshTick]);

    // ── Funded tiers — fetched once when we first have a session ────────
    // Tiers are public and stable. We could fetch eagerly on mount but
    // gating on `session` matches when the onboarding tier picker would
    // actually need them.
    useEffect(() => {
        if (!fundedWallet || !session || fundedTiers.length > 0) return;
        let alive = true;
        void (async () => {
            try {
                const tiers = await fundedWallet.listTiers();
                if (alive) setFundedTiers(tiers);
            } catch (err) {
                // Tier listing is non-critical; the UI can still show a generic
                // "funded coming soon" state when this fails.
                if (alive) {
                    setFundedError((err as Error).message || 'Failed to load funded tiers');
                }
            }
        })();
        return () => {
            alive = false;
        };
    }, [fundedWallet, session, fundedTiers.length]);

    const resolveRedirect = useCallback((): string => {
        if (redirectTo) return redirectTo;
        return getAuthRedirectUrl();
    }, [redirectTo]);

    const signInWithEmail = useCallback(
        async (email: string): Promise<{ ok: boolean; error?: string }> => {
            if (!wallet) return { ok: false, error: 'Embedded wallet not configured' };
            setAuthBusy(true);
            setAuthError(null);
            try {
                const r = await wallet.signInWithEmail(email, resolveRedirect());
                if (!r.ok && r.error) setAuthError(r.error);
                return r;
            } finally {
                setAuthBusy(false);
            }
        },
        [wallet, resolveRedirect],
    );

    const signInWithOAuth = useCallback(
        async (provider: EmbeddedSignInProvider): Promise<void> => {
            if (!wallet) {
                setAuthError('Embedded wallet not configured');
                return;
            }
            setAuthBusy(true);
            setAuthError(null);
            try {
                await wallet.signInWithOAuth(provider, resolveRedirect());
            } catch (err) {
                setAuthError((err as Error).message);
                throw err;
            } finally {
                // Note: OAuth redirects away from this page so this rarely runs.
                setAuthBusy(false);
            }
        },
        [wallet, resolveRedirect],
    );

    const signOut = useCallback(async (): Promise<void> => {
        if (!wallet) return;
        setAuthBusy(true);
        try {
            await wallet.signOut();
        } finally {
            setAuthBusy(false);
        }
    }, [wallet]);

    const refresh = useCallback(() => {
        setRefreshTick(t => t + 1);
    }, []);

    const provisionStandard = useCallback(async (): Promise<PublicWallet> => {
        if (!wallet) {
            throw new Error('[paxeer/wallet] embedded wallet not configured');
        }
        setAuthBusy(true);
        setAuthError(null);
        try {
            const r = await wallet.client.provisionStandardWallet();
            // Re-read full record (with chain info) so the context is consistent.
            setRefreshTick(t => t + 1);
            return r.wallet;
        } catch (err) {
            setAuthError((err as Error).message);
            throw err;
        } finally {
            setAuthBusy(false);
        }
    }, [wallet]);

    const refreshFunded = useCallback(() => {
        setFundedRefreshTick(t => t + 1);
    }, []);

    const provisionFunded = useCallback(
        async (tier_id?: string): Promise<FundedProvisionResponse> => {
            if (!fundedWallet) {
                throw new Error('[paxeer/wallet] funded wallet not available');
            }
            setFundedBusy(true);
            setAuthError(null);
            setFundedError(null);
            try {
                const r = await fundedWallet.provisionFunded(tier_id);
                // Trigger the funded-self effect above to re-read with the freshly
                // provisioned account so the UI flips to "active" immediately.
                setFundedRefreshTick(t => t + 1);
                return r;
            } catch (err) {
                const msg = (err as Error).message || 'Failed to provision funded account';
                setAuthError(msg);
                setFundedError(msg);
                throw err;
            } finally {
                setFundedBusy(false);
            }
        },
        [fundedWallet],
    );

    const value = useMemo<EmbeddedWalletContextValue>(
        () => ({
            wallet,
            fundedWallet,
            session,
            user,
            publicWallet,
            chain,
            isLoading,
            isAuthenticated: !!session,
            isReady: !!session && !!publicWallet,
            authBusy,
            authError,
            signInWithEmail,
            signInWithOAuth,
            signOut,
            refresh,
            provisionStandard,
            fundedSelf,
            fundedTiers,
            fundedBusy,
            fundedError,
            provisionFunded,
            refreshFunded,
        }),
        [
            wallet,
            fundedWallet,
            session,
            user,
            publicWallet,
            chain,
            isLoading,
            authBusy,
            authError,
            signInWithEmail,
            signInWithOAuth,
            signOut,
            refresh,
            provisionStandard,
            fundedSelf,
            fundedTiers,
            fundedBusy,
            fundedError,
            provisionFunded,
            refreshFunded,
        ],
    );

    return createElement(EmbeddedWalletContext.Provider, { value }, children);
}

// ── Hooks ────────────────────────────────────────────────────────────

/**
 * Primary hook. Returns the full embedded-wallet state + actions.
 *
 * Throws if used outside `<EmbeddedWalletProvider>`. If you want a hook
 * that returns null instead of throwing — for components that may render
 * outside the provider tree — use `useOptionalEmbeddedWallet()`.
 */
export function useEmbeddedWallet(): EmbeddedWalletContextValue {
    const ctx = useContext(EmbeddedWalletContext);
    if (!ctx) {
        throw new Error(
            '[paxeer/wallet] useEmbeddedWallet() must be used inside <EmbeddedWalletProvider>',
        );
    }
    return ctx;
}

/** Same as `useEmbeddedWallet` but returns null outside the provider. */
export function useOptionalEmbeddedWallet(): EmbeddedWalletContextValue | null {
    return useContext(EmbeddedWalletContext);
}

/**
 * True if the embedded wallet feature is wired up — i.e. running in a
 * browser context and configured with a Supabase publishable key. Use
 * this to conditionally render the embedded-wallet sign-in tiles next
 * to the self-custody onboarding option.
 */
export function useEmbeddedAvailability(): boolean {
    const [available, setAvailable] = useState(false);
    useEffect(() => {
        setAvailable(!!getEmbeddedWallet());
    }, []);
    return available;
}
