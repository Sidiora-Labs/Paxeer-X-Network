'use client';

import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useRef,
    useState,
    type ReactNode,
} from 'react';
import { ethers } from 'ethers';
import {
    PaxeerProvider,
    type CustodyMode,
    type Eip6963ProviderDetail,
    type Hex,
    type TypedDataPayload,
    type WalletInterface,
    type WalletTransaction,
} from '@paxeer/wallet';
import type { WalletAccount } from '@/lib/wallet/types';
import { getActiveRpcUrl } from '@/lib/constants';
import { custodyChoiceRepository } from '@/platform/storage/repositories';
import { authRedirectUrl, resolveWalletConfig, type WalletConfig } from './config';
import { IdentitySession, type IdentityProvider, type IdentityUser } from './identity';
import {
    authorisedAccount,
    discoverInjectedWallets,
    embeddedWallet,
    injectedWallet,
    transferTransaction,
    type TransferRequest,
} from './session';
import { WalletSigner } from './signer';

export type WalletStatus = 'loading' | 'signed-out' | 'connecting' | 'ready';

export interface WalletContextValue {
    readonly status: WalletStatus;
    readonly mode: CustodyMode | null;
    readonly address: Hex | null;
    readonly identity: IdentityUser | null;
    readonly wallet: WalletInterface | null;
    readonly injected: readonly Eip6963ProviderDetail[];
    readonly embeddedAvailable: boolean;
    readonly configError: string | null;
    readonly error: string | null;
    readonly busy: boolean;
    readonly sendEmailCode: (email: string) => Promise<void>;
    readonly verifyEmailCode: (email: string, code: string) => Promise<Hex>;
    readonly signInWithProvider: (provider: IdentityProvider) => Promise<void>;
    readonly connectEmbedded: () => Promise<Hex>;
    readonly connectInjected: (uuid: string) => Promise<Hex>;
    readonly refresh: () => Promise<void>;
    readonly signOut: () => Promise<void>;
    readonly sendTransaction: (tx: WalletTransaction) => Promise<Hex>;
    readonly send: (transfer: TransferRequest) => Promise<Hex>;
    readonly signMessage: (message: string) => Promise<Hex>;
    readonly signTypedData: (typedData: TypedDataPayload) => Promise<Hex>;
    readonly signer: (readProvider: ethers.Provider) => WalletSigner;
}

export class WalletNotReadyError extends Error {
    constructor(action: string) {
        super(`${action} requires a connected wallet`);
        this.name = 'WalletNotReadyError';
    }
}

export class WalletUnavailableError extends Error {
    constructor(message: string) {
        super(message);
        this.name = 'WalletUnavailableError';
    }
}

const WalletContext = createContext<WalletContextValue | null>(null);

export interface WalletProviderProps {
    readonly children: ReactNode;
    readonly config?: WalletConfig | null;
    readonly identity?: IdentitySession | null;
    readonly eventTarget?: EventTarget;
    readonly fetch?: typeof fetch;
}

interface Connection {
    readonly status: WalletStatus;
    readonly mode: CustodyMode | null;
    readonly address: Hex | null;
    readonly identity: IdentityUser | null;
    readonly wallet: WalletInterface | null;
}

const LOADING: Connection = { status: 'loading', mode: null, address: null, identity: null, wallet: null };

function signedOut(mode: CustodyMode | null): Connection {
    return { status: 'signed-out', mode, address: null, identity: null, wallet: null };
}

function message(error: unknown): string {
    return error instanceof Error && error.message ? error.message : 'the wallet request failed';
}

function defaultTarget(): EventTarget | undefined {
    return typeof window === 'undefined' ? undefined : window;
}

export function WalletProvider({ children, config: configProp, identity: identityProp, eventTarget, fetch: fetchImpl }: WalletProviderProps) {
    const resolved = useMemo(() => {
        if (configProp !== undefined) {
            return configProp
                ? { config: configProp, error: null }
                : { config: null, error: 'the embedded wallet is not configured' };
        }
        const result = resolveWalletConfig();
        return result.ok ? { config: result.config, error: null } : { config: null, error: result.error.message };
    }, [configProp]);
    const config = resolved.config;

    const identity = useMemo(() => {
        if (identityProp !== undefined) return identityProp;
        if (!config || typeof window === 'undefined') return null;
        return IdentitySession.create(config, { fetch: fetchImpl });
    }, [identityProp, config, fetchImpl]);

    const [connection, setConnection] = useState<Connection>(LOADING);
    const [injected, setInjected] = useState<readonly Eip6963ProviderDetail[]>([]);
    const [error, setError] = useState<string | null>(null);
    const [busy, setBusy] = useState(false);
    const connectionRef = useRef(connection);
    connectionRef.current = connection;
    const injectedRef = useRef<Eip6963ProviderDetail[]>([]);

    const connectEmbedded = useCallback(async (): Promise<Hex> => {
        if (!config || !identity) throw new WalletUnavailableError(resolved.error ?? 'the embedded wallet is not configured');
        setBusy(true);
        setError(null);
        setConnection((prev) => ({ ...prev, status: 'connecting', mode: 'embedded' }));
        try {
            const wallet = embeddedWallet(config, identity, { fetch: fetchImpl });
            const [address] = await wallet.accounts();
            if (!address) throw new WalletUnavailableError('the gateway returned no wallet');
            const user = await identity.user();
            custodyChoiceRepository.write('embedded');
            setConnection({ status: 'ready', mode: 'embedded', address, identity: user, wallet });
            return address;
        } catch (cause) {
            setError(message(cause));
            setConnection(signedOut('embedded'));
            throw cause;
        } finally {
            setBusy(false);
        }
    }, [config, identity, fetchImpl, resolved.error]);

    const adoptInjected = useCallback(async (detail: Eip6963ProviderDetail, prompt: boolean): Promise<Hex | null> => {
        const wallet = injectedWallet(detail);
        let address: Hex | null;
        if (prompt) {
            const [first] = await wallet.accounts();
            address = first ?? null;
        } else {
            address = await authorisedAccount(detail);
        }
        if (!address) return null;
        const expected = config?.chainId;
        if (expected !== undefined && (await wallet.chainId()) !== expected) {
            if (!prompt) return null;
            await wallet.provider.request({
                method: 'wallet_switchEthereumChain',
                params: [{ chainId: `0x${expected.toString(16)}` }],
            });
            const switched = await wallet.chainId();
            if (switched !== expected) {
                throw new WalletUnavailableError(`the injected wallet is on chain ${switched}, not ${expected}`);
            }
        }
        custodyChoiceRepository.write('injected');
        setConnection({ status: 'ready', mode: 'injected', address, identity: null, wallet });
        return address;
    }, [config]);

    const connectInjected = useCallback(async (uuid: string): Promise<Hex> => {
        const detail = injectedRef.current.find((candidate) => candidate.info.uuid === uuid);
        if (!detail) throw new WalletUnavailableError('the injected wallet is no longer announced');
        setBusy(true);
        setError(null);
        setConnection((prev) => ({ ...prev, status: 'connecting', mode: 'injected' }));
        try {
            const address = await adoptInjected(detail, true);
            if (!address) throw new WalletUnavailableError('the injected wallet exposed no account');
            return address;
        } catch (cause) {
            setError(message(cause));
            setConnection(signedOut('injected'));
            throw cause;
        } finally {
            setBusy(false);
        }
    }, [adoptInjected]);

    useEffect(() => {
        const target = eventTarget ?? defaultTarget();
        let alive = true;
        const discovery = target
            ? discoverInjectedWallets((detail) => {
                injectedRef.current = [...injectedRef.current, detail];
                if (alive) setInjected(injectedRef.current);
            }, target)
            : null;
        const choice = custodyChoiceRepository.read();
        void (async () => {
            try {
                if (choice === 'embedded' && identity && (await identity.session())) {
                    await connectEmbedded();
                    return;
                }
                if (choice === 'injected') {
                    for (const detail of injectedRef.current) {
                        if (!alive) return;
                        if (await adoptInjected(detail, false)) return;
                    }
                }
                if (alive) setConnection(signedOut(choice));
            } catch (cause) {
                if (!alive) return;
                setError(message(cause));
                setConnection(signedOut(choice));
            }
        })();
        return () => {
            alive = false;
            discovery?.stop();
        };
    }, [eventTarget, identity, connectEmbedded, adoptInjected]);

    useEffect(() => {
        if (!identity) return undefined;
        return identity.onChange((event) => {
            if (event !== 'SIGNED_OUT') return;
            const current = connectionRef.current;
            if (current.mode !== 'embedded') return;
            if (current.wallet?.provider instanceof PaxeerProvider) current.wallet.provider.disconnect();
            setConnection(signedOut('embedded'));
        });
    }, [identity]);

    useEffect(() => {
        const wallet = connection.wallet;
        if (!wallet) return undefined;
        const onAccounts = (payload: unknown) => {
            const accounts = Array.isArray(payload) ? payload : [];
            const [first] = accounts;
            if (typeof first === 'string' && ethers.isAddress(first)) {
                setConnection((prev) => (prev.wallet === wallet ? { ...prev, address: first as Hex } : prev));
            } else {
                setConnection((prev) => (prev.wallet === wallet ? signedOut(prev.mode) : prev));
            }
        };
        wallet.on('accountsChanged', onAccounts);
        return () => {
            wallet.off('accountsChanged', onAccounts);
        };
    }, [connection.wallet]);

    const redirect = useCallback((): string => {
        if (!config) throw new WalletUnavailableError(resolved.error ?? 'the embedded wallet is not configured');
        if (typeof window === 'undefined') throw new WalletUnavailableError('sign-in runs in the browser only');
        return authRedirectUrl(config, window.location.origin);
    }, [config, resolved.error]);

    const sendEmailCode = useCallback(async (email: string): Promise<void> => {
        if (!identity) throw new WalletUnavailableError(resolved.error ?? 'the embedded wallet is not configured');
        setBusy(true);
        setError(null);
        try {
            await identity.sendEmailCode(email, redirect());
        } catch (cause) {
            setError(message(cause));
            throw cause;
        } finally {
            setBusy(false);
        }
    }, [identity, redirect, resolved.error]);

    const verifyEmailCode = useCallback(async (email: string, code: string): Promise<Hex> => {
        if (!identity) throw new WalletUnavailableError(resolved.error ?? 'the embedded wallet is not configured');
        setBusy(true);
        setError(null);
        try {
            await identity.verifyEmailCode(email, code);
        } catch (cause) {
            setError(message(cause));
            setBusy(false);
            throw cause;
        }
        return connectEmbedded();
    }, [identity, connectEmbedded, resolved.error]);

    const signInWithProvider = useCallback(async (provider: IdentityProvider): Promise<void> => {
        if (!identity) throw new WalletUnavailableError(resolved.error ?? 'the embedded wallet is not configured');
        setBusy(true);
        setError(null);
        try {
            custodyChoiceRepository.write('embedded');
            await identity.signInWithProvider(provider, redirect());
        } catch (cause) {
            setError(message(cause));
            throw cause;
        } finally {
            setBusy(false);
        }
    }, [identity, redirect, resolved.error]);

    const requireWallet = useCallback((action: string): WalletInterface => {
        const current = connectionRef.current;
        if (current.status !== 'ready' || !current.wallet) throw new WalletNotReadyError(action);
        return current.wallet;
    }, []);

    const refresh = useCallback(async (): Promise<void> => {
        const wallet = requireWallet('refresh');
        const [address] = await wallet.accounts();
        setConnection((prev) =>
            prev.wallet === wallet ? (address ? { ...prev, address } : signedOut(prev.mode)) : prev,
        );
    }, [requireWallet]);

    const signOut = useCallback(async (): Promise<void> => {
        const current = connectionRef.current;
        setBusy(true);
        try {
            if (current.mode === 'embedded' && identity) await identity.signOut();
            if (current.wallet?.provider instanceof PaxeerProvider) current.wallet.provider.disconnect();
            custodyChoiceRepository.remove();
            setError(null);
            setConnection(signedOut(null));
        } finally {
            setBusy(false);
        }
    }, [identity]);

    const sendTransaction = useCallback(
        async (tx: WalletTransaction) => requireWallet('sendTransaction').sendTransaction(tx),
        [requireWallet],
    );
    const send = useCallback(
        async (transfer: TransferRequest) => requireWallet('send').sendTransaction(transferTransaction(transfer)),
        [requireWallet],
    );
    const signMessage = useCallback(
        async (text: string) => requireWallet('signMessage').signMessage(text),
        [requireWallet],
    );
    const signTypedData = useCallback(
        async (typedData: TypedDataPayload) => requireWallet('signTypedData').signTypedData(typedData),
        [requireWallet],
    );
    const signer = useCallback(
        (readProvider: ethers.Provider) => new WalletSigner(requireWallet('signer'), readProvider),
        [requireWallet],
    );

    const value = useMemo<WalletContextValue>(
        () => ({
            status: connection.status,
            mode: connection.mode,
            address: connection.address,
            identity: connection.identity,
            wallet: connection.wallet,
            injected,
            embeddedAvailable: Boolean(config && identity),
            configError: resolved.error,
            error,
            busy,
            sendEmailCode,
            verifyEmailCode,
            signInWithProvider,
            connectEmbedded,
            connectInjected,
            refresh,
            signOut,
            sendTransaction,
            send,
            signMessage,
            signTypedData,
            signer,
        }),
        [
            connection,
            injected,
            config,
            identity,
            resolved.error,
            error,
            busy,
            sendEmailCode,
            verifyEmailCode,
            signInWithProvider,
            connectEmbedded,
            connectInjected,
            refresh,
            signOut,
            sendTransaction,
            send,
            signMessage,
            signTypedData,
            signer,
        ],
    );

    return <WalletContext.Provider value={value}>{children}</WalletContext.Provider>;
}

export function useWallet(): WalletContextValue {
    const context = useContext(WalletContext);
    if (!context) throw new Error('useWallet must be used inside WalletProvider');
    return context;
}

export interface WalletState {
    ready: boolean;
    hasWallet: boolean;
    accounts: WalletAccount[];
    activeAccount: WalletAccount | null;
}

export interface WalletActions {
    readonly send: (tx: TransferRequest) => Promise<string>;
    readonly getReceiveAddress: () => Promise<string>;
    readonly reset: () => Promise<void>;
    readonly refresh: () => Promise<void>;
    readonly getSigner: () => Promise<ethers.Signer>;
}

export function walletAccount(mode: CustodyMode, address: Hex, label: string | null): WalletAccount {
    return {
        id: `${mode}:${address.toLowerCase()}`,
        kind: mode === 'embedded' ? 'managed' : 'injected',
        address,
        name: label ?? `${address.slice(0, 6)}…${address.slice(-4)}`,
        derivationPath: '',
        accountIndex: 0,
    };
}

export function useWalletState(): WalletState {
    const { status, mode, address, identity, wallet } = useWallet();
    return useMemo(() => {
        if (status !== 'ready' || !mode || !address) {
            return { ready: status !== 'loading' && status !== 'connecting', hasWallet: false, accounts: [], activeAccount: null };
        }
        const account = walletAccount(mode, address, identity?.email ?? wallet?.info?.name ?? null);
        return { ready: true, hasWallet: true, accounts: [account], activeAccount: account };
    }, [status, mode, address, identity, wallet]);
}

export function useWalletActions(): WalletActions {
    const { address, send, signOut, refresh, signer } = useWallet();
    return useMemo<WalletActions>(
        () => ({
            send,
            getReceiveAddress: async () => {
                if (!address) throw new WalletNotReadyError('getReceiveAddress');
                return address;
            },
            reset: signOut,
            refresh,
            getSigner: async () => signer(new ethers.JsonRpcProvider(getActiveRpcUrl())),
        }),
        [address, send, signOut, refresh, signer],
    );
}
