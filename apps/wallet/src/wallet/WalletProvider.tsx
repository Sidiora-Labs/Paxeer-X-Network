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
import { getActiveRpcUrl, PAXEER_CONFIG } from '@/lib/constants';
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
    readonly chainId: number | null;
    readonly writeEpoch: number;
    readonly isWriteCurrent: (epoch: number, address: string, chain: number) => boolean;
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
    readonly chainId: number | null;
    readonly status: WalletStatus;
    readonly mode: CustodyMode | null;
    readonly address: Hex | null;
    readonly identity: IdentityUser | null;
    readonly wallet: WalletInterface | null;
}

const LOADING: Connection = { chainId: null, status: 'loading', mode: null, address: null, identity: null, wallet: null };

function signedOut(mode: CustodyMode | null): Connection {
    return { chainId: null, status: 'signed-out', mode, address: null, identity: null, wallet: null };
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
    const expectedChain = config?.chainId ?? PAXEER_CONFIG.chainId;
    if (!Number.isSafeInteger(expectedChain) || expectedChain <= 0) throw new WalletUnavailableError('the configured wallet chain is invalid');

    const identity = useMemo(() => {
        if (identityProp !== undefined) return identityProp;
        if (!config || typeof window === 'undefined') return null;
        return IdentitySession.create(config, { fetch: fetchImpl });
    }, [identityProp, config, fetchImpl]);

    const [connection, publishConnection] = useState<Connection>(LOADING);
    const [writeEpoch, setWriteEpoch] = useState(0);
    const epochRef = useRef(0);
    const adoptionRef = useRef(0);
    const expectedRef = useRef(expectedChain);
    expectedRef.current = expectedChain;
    const [injected, setInjected] = useState<readonly Eip6963ProviderDetail[]>([]);
    const [error, setError] = useState<string | null>(null);
    const [busy, setBusy] = useState(false);
    const connectionRef = useRef(connection);
    const setConnection = useCallback((next: Connection | ((previous: Connection) => Connection)) => {
        const value = typeof next === 'function' ? next(connectionRef.current) : next;
        epochRef.current += 1;
        connectionRef.current = value;
        setWriteEpoch(epochRef.current);
        publishConnection(value);
    }, []);
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
            const chain = await wallet.chainId();
            if (chain !== expectedChain) throw new WalletUnavailableError('the embedded wallet chain differs from the configured chain');
            setConnection({ chainId: chain, status: 'ready', mode: 'embedded', address, identity: user, wallet });
            return address;
        } catch (cause) {
            setError(message(cause));
            setConnection(signedOut('embedded'));
            throw cause;
        } finally {
            setBusy(false);
        }
    }, [config, identity, fetchImpl, resolved.error, expectedChain]);

    const adoptInjected = useCallback(async (detail: Eip6963ProviderDetail, prompt: boolean): Promise<Hex | null> => {
        const adoption = ++adoptionRef.current;
        const probe = injectedWallet(detail);
        try {
            let address: Hex | null;
            if (prompt) {
                const [first] = await probe.accounts();
                address = first ?? null;
            } else {
                address = await authorisedAccount(detail);
            }
            if (!address) return null;
            if ((await probe.chainId()) !== expectedChain) {
                if (!prompt) return null;
                await detail.provider.request({
                    method: 'wallet_switchEthereumChain',
                    params: [{ chainId: `0x${expectedChain.toString(16)}` }],
                });
            }
            const account = await authorisedAccount(detail);
            const chain = await probe.chainId();
            if (chain !== expectedChain || !account || account.toLowerCase() !== address.toLowerCase()) {
                throw new WalletUnavailableError('the injected wallet account or configured chain was not admitted');
            }
            if (adoption !== adoptionRef.current || expectedRef.current !== expectedChain) throw new WalletNotReadyError('connect injected wallet');
            const epoch = epochRef.current + 1;
            const wallet: WalletInterface = injectedWallet(detail, {
                chainId: expectedChain,
                account,
                current: () => epochRef.current === epoch && expectedRef.current === expectedChain && connectionRef.current.status === 'ready'
                    && connectionRef.current.wallet === wallet,
            });
            custodyChoiceRepository.write('injected');
            setConnection({ chainId: chain, status: 'ready', mode: 'injected', address: account, identity: null, wallet });
            return account;
        } finally {
            probe.dispose();
        }
    }, [expectedChain, setConnection]);

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
            adoptionRef.current += 1;
            discovery?.stop();
        };
    }, [eventTarget, identity, connectEmbedded, adoptInjected]);

    useEffect(() => {
        if (!identity) return undefined;
        return identity.onChange((event) => {
            const provider = connectionRef.current.wallet?.provider;
            if (provider instanceof PaxeerProvider) provider.invalidateCapsSession();
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
        const invalidate = () => {
            if (connectionRef.current.wallet !== wallet) return;
            setConnection(signedOut(connectionRef.current.mode));
        };
        const onAccounts = (payload: unknown) => {
            if (wallet.mode === 'injected') { invalidate(); return; }
            const accounts = Array.isArray(payload) ? payload : [];
            const [first] = accounts;
            if (typeof first === 'string' && ethers.isAddress(first)) {
                setConnection((prev) => (prev.wallet === wallet ? { ...prev, address: first as Hex } : prev));
            } else {
                invalidate();
            }
        };
        wallet.on('accountsChanged', onAccounts);
        wallet.on('chainChanged', invalidate);
        wallet.on('disconnect', invalidate);
        return () => {
            wallet.off('accountsChanged', onAccounts);
            wallet.off('chainChanged', invalidate);
            wallet.off('disconnect', invalidate);
            wallet.dispose();
        };
    }, [connection.wallet, setConnection]);

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
        if (current.status !== 'ready' || !current.wallet || current.chainId !== expectedRef.current) throw new WalletNotReadyError(action);
        return current.wallet;
    }, []);

    const refresh = useCallback(async (): Promise<void> => {
        const wallet = requireWallet('refresh');
        const epoch = epochRef.current;
        const [address] = await wallet.accounts();
        const chain = await wallet.chainId();
        if (epoch !== epochRef.current || connectionRef.current.wallet !== wallet) throw new WalletNotReadyError('refresh');
        if (!address || chain !== expectedChain || address.toLowerCase() !== connectionRef.current.address?.toLowerCase()) {
            setConnection(signedOut(connectionRef.current.mode));
            throw new WalletUnavailableError('the wallet account or configured chain changed; reconnect');
        }
    }, [requireWallet, expectedChain, setConnection]);

    const signOut = useCallback(async (): Promise<void> => {
        const current = connectionRef.current;
        adoptionRef.current += 1;
        setConnection(signedOut(null));
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
        async (tx: WalletTransaction) => requireWallet('sendTransaction').sendTransaction({ ...tx, chainId: tx.chainId ?? expectedChain }),
        [requireWallet, expectedChain],
    );
    const send = useCallback(
        async (transfer: TransferRequest) => requireWallet('send').sendTransaction(transferTransaction(transfer, expectedChain)),
        [requireWallet, expectedChain],
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

    const isWriteCurrent = useCallback((epoch: number, address: string, chain: number): boolean => {
        const current = connectionRef.current;
        return current.status === 'ready' && epochRef.current === epoch && current.address === address
            && current.chainId === chain && expectedRef.current === chain;
    }, []);

    const value = useMemo<WalletContextValue>(
        () => ({
            status: connection.status,
            mode: connection.mode,
            address: connection.address,
            chainId: connection.status === 'ready' ? connection.chainId : null,
            writeEpoch,
            isWriteCurrent,
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
            expectedChain,
            writeEpoch,
            isWriteCurrent,
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
