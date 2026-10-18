// Layer 2 — Wagmi config.
// Wagmi v2 connectors:
//   - injected wallets (EIP-6963 multi-injected discovery for MetaMask, Rabby,
//     Paxport extension, etc.)
//   - Paxeer Embedded Wallet — Supabase auth + REST signing via the
//     @paxeer/wallet SDK (vendored at @paxeer-network/paxeer-backend/core/wallet/embedded/sdk).
//
// No WalletConnect. No RainbowKit. The connect UI lives in
// `@paxeer-network/paxeer-backend/app/modals/ConnectWalletModal` and the signed-in popover in
// `@paxeer-network/paxeer-backend/app/modals/WalletControlPanel`.

import { createConfig, createStorage } from "wagmi";
import { injected } from "@wagmi/core";

import { paxeerChain } from "@paxeer-network/paxeer-backend/core/chain";
import { getPaxeerWallet, paxeerEmbeddedConnector } from "@paxeer-network/paxeer-backend/core/wallet/embedded";
import { buildPaxeerTransport, getPaxeerRpcUrls } from "@paxeer-network/paxeer-backend/core/rpc";

// ─── Build connectors ─────────────────────────────────────────────────────────

// Embedded SDK signs against a single REST endpoint; pick the highest-
// ranked URL but keep the env override path so operators can pin a
// specific node when debugging.
const EMBEDDED_RPC_URL = getPaxeerRpcUrls()[0] ?? "https://api1.mainnet-beta.paxeer.network";

function buildConnectors(): Parameters<typeof createConfig>[0]["connectors"] {
  // The embedded connector requires a browser context (Supabase + localStorage).
  // On the server pass we ship just the injected list so the SSR config object
  // is identical between server and client at module-init time.
  const embeddedWallet = typeof window !== "undefined" ? getPaxeerWallet() : null;
  const embedded = embeddedWallet
    ? [
        paxeerEmbeddedConnector({
          wallet: embeddedWallet,
          chainId: paxeerChain.id,
          rpcUrl: EMBEDDED_RPC_URL,
        }),
      ]
    : [];

  return [...embedded, injected({ shimDisconnect: true })];
}

export const wagmiConfig = createConfig({
  chains: [paxeerChain],
  connectors: buildConnectors(),
  // F-10: viem fallback transport spreads load across multiple Paxeer
  // RPC endpoints with per-request latency ranking. Adding nodes is
  // env-only (NEXT_PUBLIC_PAXEER_RPC_URLS, comma-separated).
  transports: {
    [paxeerChain.id]: buildPaxeerTransport(),
  },
  multiInjectedProviderDiscovery: true,
  storage: createStorage({
    storage: typeof window !== "undefined" ? window.localStorage : undefined,
    key: "paxeer-wallet",
  }),
});
