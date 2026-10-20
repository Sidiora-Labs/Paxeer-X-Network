/**
 * Viem client helpers — wagmi-backed.
 *
 * Public client: standalone singleton (for read calls against Paxeer RPC).
 * Wallet client: fetched from wagmi on demand (injected provider signs).
 */
import { createPublicClient } from "viem";
import type { PublicClient, WalletClient } from "viem";
import { getWalletClient as wagmiGetWalletClient, getAccount } from "wagmi/actions";
import { paxeerChain } from "@/core/chain";
import { wagmiConfig } from "@/core/wagmi";
import { buildPaxeerTransport } from "@/core/rpc";

let publicClientSingleton: PublicClient | null = null;

export function getPublicClient(): PublicClient {
  if (!publicClientSingleton) {
    // F-10: share the fallback transport between wagmi and the
    // standalone public client so reads and writes hit the same
    // healthy nodes. A single bad endpoint takes down only the
    // request that landed on it, not the whole UI.
    publicClientSingleton = createPublicClient({
      chain: paxeerChain,
      transport: buildPaxeerTransport(),
    });
  }
  return publicClientSingleton;
}

/**
 * Get the wallet client from wagmi's injected provider.
 */
export async function getWalletClient(): Promise<WalletClient | null> {
  try {
    const client = await wagmiGetWalletClient(wagmiConfig);
    return client ?? null;
  } catch (err) {
    // Wallet not connected is the normal sad path; surface as warn so
    // it shows up in production sinks but doesn't page anyone.
    const msg = err instanceof Error ? err.message : String(err);
    console.warn("[viem:clients] getWalletClient failed:", msg);
    return null;
  }
}

/**
 * Get the connected account address without needing a wallet client.
 */
export function getConnectedAddress(): `0x${string}` | null {
  try {
    const acct = getAccount(wagmiConfig);
    return acct?.address ?? null;
  } catch {
    return null;
  }
}

/**
 * Get the connected account address from the wallet client.
 */
export async function getWalletAccount(): Promise<`0x${string}` | null> {
  const client = await getWalletClient();
  if (!client?.account) return null;
  return client.account.address;
}
