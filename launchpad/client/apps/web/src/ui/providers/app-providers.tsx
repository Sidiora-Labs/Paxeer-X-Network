'use client';

// Installs the postMessage EIP-1193 bridge when this app runs inside the
// PaxPort wallet iframe; it must evaluate before the wagmi config so the
// injected connector sees the bridged provider as window.ethereum.
import '@/core/iframe-bridge-init';
import { useMemo, type ReactNode } from 'react';
import { cookieStorage, createConfig, createStorage, http, WagmiProvider, type State } from 'wagmi';
import { injected } from 'wagmi/connectors';
import QueryProvider, { PrincipalCacheBarrier } from './query-provider';
import Web3Provider from './web3-provider';
import ToastProvider from '@/ui/shared/toast-provider';
import { WalletRuntimeProvider } from '@/core/wallet-sdk/runtime';
import { parsePrivateFixtureAuthorityProfileV1, RuntimeAuthorityProvider, useRuntimeAuthority, type PrivateFixtureAuthorityProfileV1 } from '@/core/runtime-authority';
import { TradeExecutionProvider } from '@/widgets/trading/transaction-controls/providers';
import { paxeer } from '@/core/wagmi-config';

function PrivateFixtureWeb3Provider({ profile, children }: { profile: PrivateFixtureAuthorityProfileV1; children: ReactNode }) {
  const config = useMemo(() => createConfig({
    chains: [{ ...paxeer, rpcUrls: { default: { http: [profile.publicRPC] } }, blockExplorers: { default: { name: 'Paxscan', url: profile.explorerBase } } }],
    connectors: [injected()],
    storage: createStorage({ key: 'kindle-private-qualification-v1', storage: cookieStorage }),
    transports: { 125: http(profile.publicRPC, { batch: { batchSize: 1024, wait: 16 }, retryCount: 0 }) },
    ssr: true,
  }), [profile]);
  return <WagmiProvider config={config}>{children}</WagmiProvider>;
}

function RuntimeProviders({ children }: { children: ReactNode }) {
  const { authority } = useRuntimeAuthority();

  return (
    <WalletRuntimeProvider authority={authority?.network}>
      <TradeExecutionProvider deployment={authority?.tradeDeployment}>
        <PrincipalCacheBarrier>
          <ToastProvider />
          {children}
        </PrincipalCacheBarrier>
      </TradeExecutionProvider>
    </WalletRuntimeProvider>
  );
}

export default function AppProviders({
  children,
  wagmiInitialState,
  privateFixtureProfile,
  privateFixtureProfileInput,
}: {
  children: ReactNode;
  wagmiInitialState?: State;
  privateFixtureProfile?: PrivateFixtureAuthorityProfileV1;
  privateFixtureProfileInput?: string;
}) {
  const configured = useMemo(() => {
    try {
      if (privateFixtureProfile !== undefined && privateFixtureProfileInput !== undefined) throw new Error('Approved network input is ambiguous');
      const input = privateFixtureProfileInput === undefined ? privateFixtureProfile : privateFixtureProfileInput;
      if (input === undefined) return {};
      const profile = parsePrivateFixtureAuthorityProfileV1(input);
      if (typeof window !== 'undefined' && window.location.origin !== profile.applicationOrigin) throw new Error('Approved local network origin is unavailable');
      return { profile };
    } catch (error) { return { error: error instanceof Error ? error.message : 'Approved network input is unavailable' }; }
  }, [privateFixtureProfile, privateFixtureProfileInput]);
  if (configured.error) {
    console.error('[app-providers]', configured.error);
    return <p role="status" className="p-4 text-sm text-white/60">Something went wrong, reload the page</p>;
  }
  const runtime = (
    <RuntimeAuthorityProvider key={configured.profile ? `${configured.profile.revision.revisionSha256}:${configured.profile.networkSha256}` : 'production'} privateFixtureProfile={configured.profile}>
      <RuntimeProviders>{children}</RuntimeProviders>
    </RuntimeAuthorityProvider>
  );
  return (
    <QueryProvider>
      {configured.profile ? (
        <PrivateFixtureWeb3Provider profile={configured.profile}>{runtime}</PrivateFixtureWeb3Provider>
      ) : (
        <Web3Provider initialState={wagmiInitialState}>{runtime}</Web3Provider>
      )}
    </QueryProvider>
  );
}
