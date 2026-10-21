'use client';

import { createContext, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import { useAccount, useConnect } from 'wagmi';
import { useQueryClient } from '@tanstack/react-query';
import { BrowserIdentitySession, type LoginConsent } from './auth';
import { backendOrigins } from '@/core/backend-origins';
import { connectorProvider, getPublicClient, isWalletProvider, PaxeerWallet } from './client';
import { installWalletMeasurementCapture, measureWalletOperation } from './measurement';
import { WalletSessionController, type WalletSession } from './session';
import { approvedWalletAssets, isWalletPrivateQueryV2, type WalletNetworkAuthorityV2 } from './backend';
import type { AssetRef, WalletPublicClient } from './assets';
import { reportUserFacingError } from '@/core/user-facing-error';

interface WalletRuntimeV2 {
  sessions: WalletSessionController; session: Readonly<WalletSession> | null;
  wallet?: PaxeerWallet; identity?: BrowserIdentitySession;
  authority?: WalletNetworkAuthorityV2; publicClient?: WalletPublicClient;
  assets: readonly AssetRef[]; unavailableReason?: string;
  login(consent: (review: LoginConsent) => Promise<boolean>): Promise<void>;
  clearSigningAccess(): void;
}
const Context = createContext<WalletRuntimeV2 | undefined>(undefined);
const emptyAssets: readonly AssetRef[] = Object.freeze([]);
export function WalletRuntimeProvider({ children, authority }: { children: ReactNode; authority?: WalletNetworkAuthorityV2 }) {
  const queryClient = useQueryClient();
  const { isConnected, isReconnecting, address, connector, chainId } = useAccount();
  const { connectors } = useConnect();
  const [sessions] = useState(() => new WalletSessionController(previous => {
    void queryClient.cancelQueries({ predicate: query => isWalletPrivateQueryV2(query, previous?.account) });
    queryClient.removeQueries({ predicate: query => isWalletPrivateQueryV2(query, previous?.account) });
  }));
  const subscribe = useMemo(() => (listener: () => void) => sessions.subscribe(listener), [sessions]);
  const session = useSyncExternalStore(subscribe, () => sessions.current, () => null);
  const selectedSession = isConnected && session && session.account.toLowerCase() === address?.toLowerCase() && session.chainId === chainId && session.connectorId === connector?.id ? session : null;
  const [wallet, setWallet] = useState<PaxeerWallet>();
  const [identity, setIdentity] = useState<BrowserIdentitySession>();
  const [providerError, setProviderError] = useState<string>();
  const activeProvider = useRef<unknown>(undefined);
  useEffect(() => installWalletMeasurementCapture(window), []);
  useEffect(() => {
    let service: BrowserIdentitySession | undefined;
    try {
      service = new BrowserIdentitySession(sessions, window.location.origin, backendOrigins().privateApi ?? window.location.origin, backendOrigins().privateStream ?? `wss://${window.location.host}`, async scope => {
        const provider = activeProvider.current;
        if (!isWalletProvider(provider)) throw new Error('Reconnect your wallet before restoring private access');
        const currentWallet = new PaxeerWallet(provider);
        const accounts = await currentWallet.getAccounts();
        const actualChain = Number(await provider.request({ method: 'eth_chainId' }));
        sessions.assertCurrent(scope, true);
        if (accounts[0]?.toLowerCase() !== scope.account.toLowerCase() || actualChain !== 125) throw new Error('Wallet changed while the page was suspended; sign in again');
      });
      setIdentity(service);
    } catch (error) { setProviderError(reportUserFacingError(error, 'Sign-in did not work, try again', 'wallet-session')); }
    const remember = service?.subscribe(() => {
      const current = service?.current;
      if (current) { try { localStorage.setItem('kindle:last-authenticated-wallet:v2', current.wallet); } catch { /* Wallet custody never depends on browser persistence. */ } }
      else {
        const account = sessions.current?.account;
        void queryClient.cancelQueries({ predicate: query => isWalletPrivateQueryV2(query, account) });
        queryClient.removeQueries({ predicate: query => isWalletPrivateQueryV2(query, account) });
      }
    });
    const timer = setInterval(() => { if (service?.current) void service.readSession().catch(() => { service?.invalidate(false); setProviderError('You were signed out, sign in again'); }); }, 30_000);
    return () => { clearInterval(timer); remember?.(); service?.dispose(); setIdentity(undefined); };
  }, [sessions, queryClient]);
  useEffect(() => {
    let cancelled = false, detach: (() => void) | undefined;
    setWallet(undefined); activeProvider.current = undefined;
    sessions.disconnect();
    if (!isConnected || isReconnecting || !address || !connector || !chainId) return;
    const selectedAddress = address, selectedChain = chainId;
    const initializationEpoch = sessions.sessionEpoch, cancellationEpoch = sessions.cancellationEpoch;
    void measureWalletOperation('connector.initialize', `connector:${connector.id}`, () => connectorProvider(connector)).then(async provider => {
      if (cancelled) return;
      if (!isWalletProvider(provider)) throw new Error('The selected connector has no compatible wallet provider');
      detach = sessions.bind(provider);
      const signer = new PaxeerWallet(provider);
      const accounts = await signer.getAccounts();
      const actualChain = Number(await provider.request({ method: 'eth_chainId' }));
      if (cancelled) return;
      sessions.assertConnecting(initializationEpoch, selectedAddress, selectedChain, cancellationEpoch);
      if (actualChain !== selectedChain || accounts[0]?.toLowerCase() !== selectedAddress.toLowerCase()) throw new Error('Wallet selection changed during initialization');
      sessions.select(connector.id, connector.type === 'walletConnect' ? 'walletConnect' : 'injected', selectedAddress, selectedChain, connectors.map(item => item.id));
      activeProvider.current = provider; setWallet(signer); setProviderError(undefined);
    }).catch(error => { if (!cancelled) { sessions.disconnect(); setWallet(undefined); setProviderError(reportUserFacingError(error, 'Could not connect your wallet, try again', 'wallet-connect')); } });
    return () => { cancelled = true; detach?.(); activeProvider.current = undefined; setWallet(undefined); };
  }, [isConnected, isReconnecting, address, chainId, connector?.uid, sessions]);
  const configured = useMemo(() => {
    if (!authority) return { assets: emptyAssets, reason: 'Your wallet is still loading, try again in a moment' };
    try { return { assets: approvedWalletAssets(authority), client: getPublicClient(authority.network.publicRPC, session ? sessions.capture().signal : undefined) }; }
    catch (error) { return { assets: emptyAssets, reason: reportUserFacingError(error, 'Funding is not available yet, try again in a moment', 'wallet-assets') }; }
  }, [authority, session?.sessionEpoch, sessions]);
  const value: WalletRuntimeV2 = { sessions, session: selectedSession, wallet: selectedSession ? wallet : undefined, identity: selectedSession ? identity : undefined, authority, publicClient: selectedSession ? configured.client : undefined, assets: configured.assets, unavailableReason: providerError || configured.reason,
    async login(consent) {
      if (!wallet || !identity || !selectedSession || activeProvider.current !== wallet.provider) throw new Error('Select your wallet before signing in');
      const scope = sessions.capture(); sessions.assertCurrent(scope, true);
      const kind = await wallet.proofKind(scope.account); sessions.assertCurrent(scope, true);
      await identity.login(wallet, kind, consent);
    },
    clearSigningAccess() { sessions.disconnect(); identity?.invalidate(); setWallet(undefined); activeProvider.current = undefined; },
  };
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function useWalletRuntime() {
  const runtime = useContext(Context);
  if (!runtime) throw new Error('Wallet runtime must be mounted under the registered Web3 provider');
  return runtime;
}
