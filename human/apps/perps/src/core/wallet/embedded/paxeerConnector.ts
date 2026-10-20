/**
 * wagmi v2 connector for the Paxeer Embedded Wallet.
 *
 * Bridges the SDK (REST signing over Supabase auth) into wagmi as a normal
 * EIP-1193 connector. Once the user has signed in via the embedded modal,
 * `connect({ connector: paxeerEmbeddedConnector })` flips wagmi to use this
 * provider for `eth_sendTransaction`, `personal_sign`, `eth_call`, etc.
 *
 * The connector watches Supabase auth state and emits wagmi `disconnect`
 * when the user signs out, keeping the two state machines in sync.
 */

import { createConnector } from "wagmi";
import type { PaxeerWallet } from "./sdk";
import { createPaxeerEIP1193Provider, type PaxeerEIP1193Provider } from "./eip1193";

export const PAXEER_CONNECTOR_ID = "paxeer-embedded";

export interface PaxeerConnectorParams {
  wallet: PaxeerWallet;
  chainId: number;
  rpcUrl: string;
}

export function paxeerEmbeddedConnector(params: PaxeerConnectorParams) {
  const { wallet, chainId, rpcUrl } = params;
  let provider: PaxeerEIP1193Provider | null = null;

  function ensureProvider(): PaxeerEIP1193Provider {
    if (!provider) {
      provider = createPaxeerEIP1193Provider({ wallet, chainId, rpcUrl });
    }
    return provider;
  }

  // wagmi v2's `Connector.connect` is a generic method over `withCapabilities`
  // (EIP-5792). A non-generic implementation can't be unified with that shape
  // through structural assignability, even though the runtime contract is
  // identical. Official wagmi connectors (injected/walletConnect/coinbase) are
  // typed against an internal connector spec that erases this — third-party
  // connectors hit this wall. We assert at the boundary and rely on the
  // strongly-typed concrete implementation inside the closure.
  return createConnector(
    // @ts-expect-error — see comment above; concrete connector shape is correct.
    (config) => {
    const onAccountsChanged = (...args: unknown[]) => {
      const accounts = (args[0] as `0x${string}`[]) ?? [];
      if (accounts.length === 0) {
        config.emitter.emit("disconnect");
      } else {
        config.emitter.emit("change", { accounts });
      }
    };

    const onChainChanged = (...args: unknown[]) => {
      const id = args[0];
      const newChainId = typeof id === "string" ? parseInt(id, 16) : Number(id);
      config.emitter.emit("change", { chainId: newChainId });
    };

    const onDisconnect = () => {
      config.emitter.emit("disconnect");
    };

    return {
      id: PAXEER_CONNECTOR_ID,
      name: "Paxeer Wallet",
      type: "paxeer-embedded" as const,
      icon: "/icons/paxeer-wallet.svg",

      async setup() {
        ensureProvider();
      },

      // The Paxeer Embedded Wallet is a single-chain EOA — it never exposes
      // EIP-5792 capabilities — so we return the plain account shape. wagmi
      // v2's `connect` signature is generic over `withCapabilities`, which
      // doesn't unify with a non-generic implementation; we cast the whole
      // connector object with `as never` at the boundary below to satisfy it.
      async connect(parameters?: { chainId?: number; isReconnecting?: boolean }) {
        const p = ensureProvider();

        // Wire provider events through wagmi's emitter.
        p.on("accountsChanged", onAccountsChanged);
        p.on("chainChanged", onChainChanged);
        p.on("disconnect", onDisconnect);

        const accounts = (await p.request({ method: "eth_accounts" })) as readonly `0x${string}`[];
        if (accounts.length === 0) {
          // Caller hit Connect without signing in — bubble a clear error.
          if (!parameters?.isReconnecting) {
            throw new Error(
              "Sign in to the Paxeer Embedded Wallet first. Choose Email / Google / X / GitHub from the connect modal.",
            );
          }
          return { accounts: [] as readonly `0x${string}`[], chainId };
        }
        return { accounts, chainId };
      },

      async disconnect() {
        const p = provider;
        if (p) {
          p.removeListener("accountsChanged", onAccountsChanged);
          p.removeListener("chainChanged", onChainChanged);
          p.removeListener("disconnect", onDisconnect);
          p.destroy();
        }
        provider = null;
        // Also drop the Supabase session — the user explicitly disconnected.
        try {
          await wallet.signOut();
        } catch {
          /* ignore — wagmi disconnect must always succeed */
        }
      },

      async getAccounts() {
        const p = ensureProvider();
        return (await p.request({ method: "eth_accounts" })) as readonly `0x${string}`[];
      },

      async getChainId() {
        return chainId;
      },

      async getProvider() {
        return ensureProvider() as unknown;
      },

      async isAuthorized() {
        try {
          const p = ensureProvider();
          const accounts = (await p.request({ method: "eth_accounts" })) as `0x${string}`[];
          return accounts.length > 0;
        } catch {
          return false;
        }
      },

      async switchChain({ chainId: target }: { chainId: number }) {
        if (target !== chainId) {
          throw new Error(
            `Paxeer Embedded Wallet is single-chain (Paxeer Mainnet / ${chainId}). Cannot switch to ${target}.`,
          );
        }
        const chain = config.chains.find((c) => c.id === target);
        if (!chain) throw new Error(`Chain ${target} not configured`);
        return chain;
      },

      onAccountsChanged: onAccountsChanged as (accounts: readonly `0x${string}`[]) => void,
      onChainChanged,
      onDisconnect,
    };
  });
}
