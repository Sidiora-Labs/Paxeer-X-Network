'use client';

import React, {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useRef,
    useState,
} from 'react';
import { ethers } from 'ethers';
import type { ISelfCustodyWallet, WalletAccount } from '@/lib/wallet';
import {
    EmbeddedSigner,
    FundedSigner,
    WalletError,
    WalletEvents,
    useOptionalEmbeddedWallet,
} from '@/lib/wallet';
import type { WalletErrorCode } from '@/lib/wallet';
import { getWallet } from '@/lib/wallet-instance';
import { PAXEER_CONFIG, getActiveRpcUrl } from '@/lib/constants';
import { useWalletKind } from '@/providers/WalletKindProvider';
import { preferencesRepository } from '@/platform/storage/repositories';

// ── Public state ─────────────────────────────────────────────────────────────
//
// Identical shape across both custody models so the data layer
// (`useTokenBalances`, `useTransactions`, etc.) — which only reads
// `activeAccount.address` — keeps working unchanged.

export interface WalletState {
    ready: boolean;
    hasWallet: boolean;
    isLocked: boolean;
    accounts: WalletAccount[];
    activeAccount: WalletAccount | null;
    sessionRemaining: number;
    migrationRequired?: boolean;
    securityError?: WalletErrorCode;
}

const initial: WalletState = {
    ready: false,
    hasWallet: false,
    isLocked: true,
    accounts: [],
    activeAccount: null,
    sessionRemaining: 0,
    migrationRequired: false,
};

function selfCustodyErrorCode(error: unknown): WalletErrorCode {
    return error instanceof WalletError ? error.code : 'STORAGE_UNAVAILABLE';
}

// ── Public actions ───────────────────────────────────────────────────────────
//
// Two surfaces:
//
//   - **Common actions** — always available regardless of custody model.
//     `send`, `getReceiveAddress`, `reset`, `refresh`.
//
//   - **Self-custody-only actions** — kept on the same context for
//     compatibility with existing call sites. In embedded mode they throw
//     `EmbeddedNotSupportedError` so call sites can surface a friendly
//     "this feature is for self-custody wallets" message.
//
// Embedded-specific actions (sign-in, sign-out) live on the
// `EmbeddedWalletProvider` from `@paxport/wallet`. Components import
// `useEmbeddedWallet()` directly when they need those.

export interface WalletActions {
    // ── Self-custody onboarding ──
    createWallet: (password: string, name?: string) => Promise<{ mnemonic: string }>;
    restoreWallet: (password: string, mnemonic: string) => Promise<void>;
    migrateLegacy: (legacyPin: string, newPassword: string) => Promise<void>;
    migratePassphraseToPin: (currentPassphrase: string, newPin: string) => Promise<void>;

    // ── Self-custody session ──
    unlock: (password: string) => Promise<boolean>;
    reauthenticate: (password: string) => Promise<void>;
    lock: () => Promise<void>;

    // ── Common ──
    send: (tx: { to: string; value: string; tokenAddress?: string; decimals?: number }) => Promise<string>;
    getReceiveAddress: () => Promise<string>;
    reset: () => Promise<void>;
    refresh: () => Promise<void>;

    // ── Self-custody account management ──
    switchAccount: (address: string) => Promise<void>;
    addAccount: (name: string) => Promise<void>;
    renameAccount: (address: string, newName: string) => Promise<void>;
    deleteAccount: (address: string) => Promise<void>;
    importPrivateKey: (privateKey: string, name: string) => Promise<void>;
    exportMnemonic: () => Promise<string>;
    exportPrivateKey: (address: string) => Promise<string>;

    // ── Signer (used by swap, dapp browser, pns) ──
    //
    // Self-custody returns a keyless `VaultSigner` connected to the Paxeer RPC;
    // signing stays inside wallet-core and broadcasts directly. Embedded returns an
    // `EmbeddedSigner` — same `ethers.Signer` surface, but
    // `sendTransaction()` delegates to `connect.paxportwallet.com`. The
    // swap SDK works unchanged for both. DApp browser and PNS are gated
    // to self-custody mode in the shell because they need
    // `signTransaction` / typed-data signing that the embedded API
    // doesn't expose yet.
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

export class EmbeddedNotSupportedError extends Error {
    constructor(action: string) {
        super(
            `[paxeer/wallet] "${action}" is only available for self-custody wallets. ` +
            'The user is currently signed in with a Paxeer-managed embedded wallet.',
        );
        this.name = 'EmbeddedNotSupportedError';
    }
}

const embeddedNotSupported = (action: string) => () => {
    throw new EmbeddedNotSupportedError(action);
};

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

// ── Provider ─────────────────────────────────────────────────────────────────

export function WalletProvider({ children }: { children: React.ReactNode }) {
    const { kind, hydrated, setKind } = useWalletKind();
    const embedded = useOptionalEmbeddedWallet();

    // Self-custody backend (lazily resolved). The singleton is constructed on
    // first call and reused for the life of the page, exactly as before.
    const walletRef = useRef<ISelfCustodyWallet | null>(null);
    const pw = useCallback(() => {
        if (!walletRef.current) walletRef.current = getWallet();
        return walletRef.current;
    }, []);

    const [state, setState] = useState<WalletState>(initial);

    // ── Migration / auto-promote on first hydrate ─────────────────────
    //
    // For users who installed the app **before** the dual-mode rollout,
    // `paxeer:wallet-kind` will be unset (`kind === null`) on first
    // launch. Without a migration, those users would land on the new
    // onboarding welcome screen even though their encrypted self-custody
    // wallet is sitting in legacy localStorage. They must be routed to the
    // explicit migration screen before any new vault can be created.
    //
    // To preserve every existing user's access to their funds we:
    //
    //   1. Promote to `'embedded'` if there's a live Supabase session.
    //      An active session implies the user has previously chosen
    //      embedded; we honour it even if `kind` was wiped.
    //
    //   2. Otherwise promote to `'self-custody'` if the underlying
    //      `PaxeerWallet` reports `hasWallet === true` — i.e. the user
    //      already has an encrypted mnemonic on this device. They land
    //      back in the lock screen, which handles legacy migration explicitly.
    //
    //   3. Otherwise leave `kind === null` so genuinely new installs see
    //      the welcome screen and pick a custody model.
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
                return;
            }
            try {
                const exists = await pw().hasWallet();
                if (!cancelled && exists) setKind('self-custody');
            } catch (error) {
                // Fail closed. Storage corruption or unavailability must never
                // make an existing encrypted wallet look like a fresh install.
                if (!cancelled) {
                    setState({
                        ...initial,
                        ready: true,
                        hasWallet: true,
                        securityError: selfCustodyErrorCode(error),
                    });
                }
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
        pw,
    ]);

    // ── Self-custody refresh ────────────────────────────────────────────
    const refreshSelfCustody = useCallback(async () => {
        try {
            const snapshot = await pw().getSnapshot();
            setState({
                ready: true,
                ...snapshot,
            });
        } catch (error) {
            setState({
                ...initial,
                ready: true,
                hasWallet: true,
                securityError: selfCustodyErrorCode(error),
            });
        }
    }, [pw]);

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
            setState({
                ready: true,
                hasWallet: false,
                isLocked: true,
                accounts: [],
                activeAccount: null,
                sessionRemaining: 0,
            });
            return;
        }
        if (!fundedSelf) {
            // Authenticated but no funded account yet — the onboarding
            // tier picker will provision one. Surfacing `hasWallet: false`
            // routes the shell back to the onboarding screen.
            setState({
                ready: true,
                hasWallet: false,
                isLocked: true,
                accounts: [],
                activeAccount: null,
                sessionRemaining: 0,
            });
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
            isLocked: false,
            accounts: [account],
            activeAccount: account,
            sessionRemaining: Number.MAX_SAFE_INTEGER,
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
            setState({
                ready: true,
                hasWallet: false,
                isLocked: true,
                accounts: [],
                activeAccount: null,
                sessionRemaining: 0,
            });
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
            setState({
                ready: true,
                hasWallet: false,
                isLocked: true,
                accounts: [],
                activeAccount: null,
                sessionRemaining: 0,
            });
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
            isLocked: false,
            accounts: [account],
            activeAccount: account,
            // Embedded sessions don't expire on a wallet timer — Supabase
            // refreshes the JWT silently.
            sessionRemaining: Number.MAX_SAFE_INTEGER,
        });
    }, [embedded, hydrated]);

    // ── Effect: drive state on kind / backend changes ──────────────────
    useEffect(() => {
        if (!hydrated) return;

        if (kind === null) {
            // No choice yet — drive welcome screen.
            setState({
                ready: true,
                hasWallet: false,
                isLocked: true,
                accounts: [],
                activeAccount: null,
                sessionRemaining: 0,
            });
            return;
        }

        if (kind === 'self-custody') {
            const w = pw();
            const sync = () => refreshSelfCustody();

            w.events.on(WalletEvents.SESSION_EXPIRED, sync);
            w.events.on(WalletEvents.SESSION_CREATED, sync);
            w.events.on(WalletEvents.MANUAL_LOCK, sync);
            w.events.on(WalletEvents.ACCOUNT_CHANGED, sync);
            w.events.on(WalletEvents.WALLET_CLEARED, sync);

            sync();

            const poll = setInterval(async () => {
                const rem = w.getSessionTimeRemaining();
                setState((prev) => {
                    if (rem === 0 && !prev.isLocked) sync();
                    return { ...prev, sessionRemaining: rem };
                });
            }, 30_000);

            return () => {
                w.events.off(WalletEvents.SESSION_EXPIRED, sync);
                w.events.off(WalletEvents.SESSION_CREATED, sync);
                w.events.off(WalletEvents.MANUAL_LOCK, sync);
                w.events.off(WalletEvents.ACCOUNT_CHANGED, sync);
                w.events.off(WalletEvents.WALLET_CLEARED, sync);
                clearInterval(poll);
            };
        }

        if (kind === 'embedded') {
            refreshEmbedded();
            return undefined;
        }

        // kind === 'funded'
        refreshFunded();
        return undefined;
    }, [kind, hydrated, pw, refreshSelfCustody, refreshEmbedded, refreshFunded]);

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
            if (kind === 'self-custody') return refreshSelfCustody();
            if (kind === 'embedded') return refreshEmbedded();
            if (kind === 'funded') return refreshFunded();
            return;
        };

        if (kind === 'funded') {
            return {
                // Funded mode shares no self-custody surface.
                createWallet: fundedNotSupported('createWallet'),
                restoreWallet: fundedNotSupported('restoreWallet'),
                migrateLegacy: fundedNotSupported('migrateLegacy'),
                migratePassphraseToPin: fundedNotSupported('migratePassphraseToPin'),
                unlock: fundedNotSupported('unlock'),
                reauthenticate: fundedNotSupported('reauthenticate'),
                lock: fundedNotSupported('lock'),
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
                switchAccount: fundedNotSupported('switchAccount'),
                addAccount: fundedNotSupported('addAccount'),
                renameAccount: fundedNotSupported('renameAccount'),
                deleteAccount: fundedNotSupported('deleteAccount'),
                importPrivateKey: fundedNotSupported('importPrivateKey'),
                exportMnemonic: fundedNotSupported('exportMnemonic'),
                exportPrivateKey: fundedNotSupported('exportPrivateKey'),
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
                createWallet: embeddedNotSupported('createWallet'),
                restoreWallet: embeddedNotSupported('restoreWallet'),
                migrateLegacy: embeddedNotSupported('migrateLegacy'),
                migratePassphraseToPin: embeddedNotSupported('migratePassphraseToPin'),
                unlock: embeddedNotSupported('unlock'),
                reauthenticate: embeddedNotSupported('reauthenticate'),
                lock: embeddedNotSupported('lock'),
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
                switchAccount: embeddedNotSupported('switchAccount'),
                addAccount: embeddedNotSupported('addAccount'),
                renameAccount: embeddedNotSupported('renameAccount'),
                deleteAccount: embeddedNotSupported('deleteAccount'),
                importPrivateKey: embeddedNotSupported('importPrivateKey'),
                exportMnemonic: embeddedNotSupported('exportMnemonic'),
                exportPrivateKey: embeddedNotSupported('exportPrivateKey'),
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

        // Self-custody (or kind === null — onboarding calls
        // createWallet / restoreWallet which set the kind on success).
        return {
            createWallet: async (password, name) => {
                const { mnemonic } = await pw().createNewWallet(password, name);
                if (kind === 'self-custody') await refreshSelfCustody();
                return { mnemonic };
            },
            restoreWallet: async (password, mnemonic) => {
                await pw().restoreFromMnemonic(password, mnemonic);
                if (kind === 'self-custody') await refreshSelfCustody();
            },
            migrateLegacy: async (legacyPin, newPassword) => {
                await pw().migrateLegacy(legacyPin, newPassword);
                await refreshSelfCustody();
            },
            migratePassphraseToPin: async (currentPassphrase, newPin) => {
                await pw().migratePassphraseToPin(currentPassphrase, newPin);
                await refreshSelfCustody();
            },
            unlock: async (password) => {
                const ok = await pw().unlock(password);
                await refreshSelfCustody();
                return ok;
            },
            reauthenticate: async (password) => {
                await pw().reauthenticate(password);
            },
            lock: async () => {
                await pw().lock();
                await refreshSelfCustody();
            },
            send: async (tx) => {
                const customNonce = preferencesRepository.read().customNonce;
                const hash = await pw().send({ ...tx, nonce: customNonce ?? undefined });
                if (customNonce !== null) {
                    preferencesRepository.update((current) => ({
                        ...current,
                        customNonce: null,
                    }));
                }
                await refreshSelfCustody();
                return hash;
            },
            getReceiveAddress: () => pw().getReceiveAddress(),
            switchAccount: async (addr) => {
                await pw().setActiveAccount(addr);
                await refreshSelfCustody();
            },
            addAccount: async (name) => {
                await pw().deriveNextAccount(name);
                await refreshSelfCustody();
            },
            renameAccount: async (address, newName) => {
                await pw().renameAccount(address, newName);
                await refreshSelfCustody();
            },
            deleteAccount: async (address) => {
                await pw().deleteAccount(address);
                await refreshSelfCustody();
            },
            importPrivateKey: async (pk, name) => {
                await pw().importPrivateKey(pk, name);
                await refreshSelfCustody();
            },
            exportMnemonic: async () => {
                return pw().exportMnemonic();
            },
            exportPrivateKey: async (address: string) => {
                return pw().exportPrivateKey(address);
            },
            getSigner: async () => {
                const active = await pw().getActiveAccount();
                if (!active) throw new Error('No active account');
                return pw().getSigner(active.address);
            },
            reset: async () => {
                await pw().reset();
                await refreshSelfCustody();
            },
            refresh,
        };
    }, [kind, embedded, pw, refreshSelfCustody, refreshEmbedded, refreshFunded]);

    return (
        <StateCtx.Provider value={state}>
            <ActionsCtx.Provider value={actions}>{children}</ActionsCtx.Provider>
        </StateCtx.Provider>
    );
}
