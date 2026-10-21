import { http, fallback, createConfig, createStorage, cookieStorage, type CreateConnectorFn } from 'wagmi';
import { type Chain } from 'viem';
import { injected, walletConnect } from 'wagmi/connectors';
import { PAXEER_RPC_URL, getPaxeerRpcUrls } from '@/core/rpc';
import { walletConnectProjectId } from '@/core/backend-origins';
import { getPaxeerWallet, paxeerEmbeddedConnector } from '@/core/wallet-sdk/embedded_sdk/embedded';

export { PAXEER_RPC_URL } from '@/core/rpc';

/** Origin this deployment is served from; used for wallet pairing metadata. */
export const CANONICAL_ORIGIN = (process.env.NEXT_PUBLIC_CANONICAL_ORIGIN || 'https://sidiora.fun').replace(/\/+$/, '');

export const paxeer = {
  id: 125,
  name: 'Paxeer Network',
  nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 },
  rpcUrls: {
    default: {
      http: [PAXEER_RPC_URL],
    },
  },
  blockExplorers: {
    default: { name: 'Paxscan', url: 'https://paxscan.paxeer.app' },
  },
} as const satisfies Chain;

export const appMetadata = {
  name: 'Sidiora',
  description: 'Token launchpad on Paxeer Network',
  url: CANONICAL_ORIGIN,
  icons: [`${CANONICAL_ORIGIN}/icon-512.png`],
} as const;

/** Set only when a WalletConnect project id is configured for this deployment. */
export const walletConnectConfigured = walletConnectProjectId() !== null;

const primaryRpc = http(
  PAXEER_RPC_URL,
  {
    batch: { batchSize: 1024, wait: 16 },
    retryCount: 2,
  },
);

// Second usable endpoint from the shared list (core/rpc.ts), so a retired or
// malformed fallback is never contacted and every RPC host is in the CSP.
const fallbackRpcUrl = getPaxeerRpcUrls()[1];
const transports = fallbackRpcUrl
  ? fallback(
      [
        primaryRpc,
        http(fallbackRpcUrl, { batch: true, retryCount: 1 }),
      ],
      { rank: { interval: 60_000 } },
    )
  : primaryRpc;

/**
 * Set only when the Paxeer Embedded Wallet is configured for this deployment
 * (NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY present). The connector is created in
 * the browser only: the SDK singleton holds a Supabase session, which does not
 * exist on the server, and wagmi state is restored from cookies regardless of
 * the connector list.
 */
export const embeddedWalletConfigured = typeof window !== 'undefined' && getPaxeerWallet() !== null;

function createConnectors(): CreateConnectorFn[] {
  const list: CreateConnectorFn[] = [];
  const embedded = typeof window !== 'undefined' ? getPaxeerWallet() : null;
  if (embedded) {
    list.push(paxeerEmbeddedConnector({ wallet: embedded, chainId: paxeer.id, rpcUrl: PAXEER_RPC_URL }));
  }
  list.push(injected());
  // The CSP allows the WalletConnect hosts exactly when this id is set (core/backend-origins.ts).
  const wcProjectId = walletConnectProjectId();
  if (wcProjectId) {
    list.push(walletConnect({ projectId: wcProjectId, metadata: { ...appMetadata, icons: [...appMetadata.icons] } }));
  }
  return list;
}

/** Builds a fresh wagmi config. Callers should use {@link getWagmiConfig}. */
export function createWagmiConfig() {
  return createConfig({
    chains: [paxeer],
    connectors: createConnectors(),
    multiInjectedProviderDiscovery: true,
    storage: createStorage({ storage: cookieStorage }),
    transports: {
      [paxeer.id]: transports,
    },
    ssr: true,
  });
}

export type AppWagmiConfig = ReturnType<typeof createWagmiConfig>;

let browserConfig: AppWagmiConfig | undefined;

/**
 * The wagmi config for the current render.
 *
 * In the browser there is exactly one config for the page's lifetime, shared by
 * every provider. On the server a fresh config is returned on every call: a
 * wagmi config is a mutable store, and WagmiProvider's <Hydrate> writes the
 * request's cookie state into it during render. A module-level config would
 * carry one visitor's connected account into every later request that has no
 * wagmi cookie, so the server would render the connected branches while the
 * browser renders the disconnected ones (React error #418), and it would leak
 * that visitor's account into other people's pages.
 */
export function getWagmiConfig(): AppWagmiConfig {
  if (typeof window === 'undefined') return createWagmiConfig();
  browserConfig ??= createWagmiConfig();
  return browserConfig;
}
