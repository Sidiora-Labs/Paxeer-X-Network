/**
 * Client configuration — deploy inputs only.
 *
 * The chain configs in contracts/bridge/** ship every deployment value as a
 * PLACEHOLDER (owner, attestors, Solana program id). Nothing here invents a
 * value: an unset variable surfaces in the UI as "not deployed" / "backend
 * not configured", and write actions stay disabled. NEXT_PUBLIC_* variables
 * are inlined at build time, so a deployer must rebuild after changing them.
 */
import { getAddress, isAddress, type Address } from "viem";

function readUrl(value: string | undefined): string | undefined {
  const raw = value?.trim();
  if (!raw) return undefined;
  try {
    const parsed = new URL(raw);
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") return undefined;
    if (parsed.username || parsed.password) return undefined;
    return raw.replace(/\/+$/, "");
  } catch {
    return undefined;
  }
}

function readAddress(value: string | undefined): Address | undefined {
  const raw = value?.trim();
  return raw && isAddress(raw, { strict: false }) ? getAddress(raw) : undefined;
}

export const bridgeConfig = {
  /** Paxeer JSON-RPC. Unset → Paxeer reads are disabled, never guessed. */
  paxeerRpcUrl: readUrl(process.env.NEXT_PUBLIC_PAXEER_RPC_URL),
  paxeerExplorerUrl: readUrl(process.env.NEXT_PUBLIC_PAXEER_EXPLORER_URL) ?? "https://paxscan.io",
  /** Attestation API base (the backend your devs run). Unset → claim/release disabled. */
  attestationApiUrl: readUrl(process.env.NEXT_PUBLIC_BRIDGE_API_URL),
  /** PaxeerXVault deployments per foreign EVM chain, from the deploy records. */
  vaults: {
    ethereum: readAddress(process.env.NEXT_PUBLIC_BRIDGE_VAULT_ETHEREUM),
    base: readAddress(process.env.NEXT_PUBLIC_BRIDGE_VAULT_BASE),
    arbitrum: readAddress(process.env.NEXT_PUBLIC_BRIDGE_VAULT_ARBITRUM),
    optimism: readAddress(process.env.NEXT_PUBLIC_BRIDGE_VAULT_OPTIMISM),
    bnb: readAddress(process.env.NEXT_PUBLIC_BRIDGE_VAULT_BNB),
    polygon: readAddress(process.env.NEXT_PUBLIC_BRIDGE_VAULT_POLYGON),
    avalanche: readAddress(process.env.NEXT_PUBLIC_BRIDGE_VAULT_AVALANCHE),
    hyperevm: readAddress(process.env.NEXT_PUBLIC_BRIDGE_VAULT_HYPEREVM),
  },
  /** Foreign RPC overrides (wagmi `http(url)`); public viem defaults otherwise. */
  foreignRpcUrls: {
    ethereum: readUrl(process.env.NEXT_PUBLIC_BRIDGE_RPC_ETHEREUM),
    base: readUrl(process.env.NEXT_PUBLIC_BRIDGE_RPC_BASE),
    arbitrum: readUrl(process.env.NEXT_PUBLIC_BRIDGE_RPC_ARBITRUM),
    optimism: readUrl(process.env.NEXT_PUBLIC_BRIDGE_RPC_OPTIMISM),
    bnb: readUrl(process.env.NEXT_PUBLIC_BRIDGE_RPC_BNB),
    polygon: readUrl(process.env.NEXT_PUBLIC_BRIDGE_RPC_POLYGON),
    avalanche: readUrl(process.env.NEXT_PUBLIC_BRIDGE_RPC_AVALANCHE),
    hyperevm: readUrl(process.env.NEXT_PUBLIC_BRIDGE_RPC_HYPEREVM),
  },
} as const;
