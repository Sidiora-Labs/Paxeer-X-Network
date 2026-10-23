/**
 * Explorer links. EVM explorers come from the viem chain definitions;
 * Paxeer from config; Solana from its public explorer. A missing explorer
 * yields undefined and the dxui components render the hash as plain text.
 */
import type { Address, Hex } from "viem";

import { BRIDGE_CHAINS, PAXEER_ROUTE_KEY, paxeer, type BridgeChain } from "./chains";
import { bridgeConfig } from "./config";

function trim(base: string): string {
  return base.replace(/\/+$/, "");
}

function evmTx(base: string | undefined, hash: Hex): string | undefined {
  return base ? `${trim(base)}/tx/${hash}` : undefined;
}

/** Source-tx explorer URL for a transfer on a foreign chain. */
export function foreignTxUrl(chain: BridgeChain, hash: Hex): string | undefined {
  if (chain.kind === "solana") return `https://explorer.solana.com/tx/${hash}`;
  return evmTx(chain.viemChain?.blockExplorers?.default.url, hash);
}

export function paxeerTxUrl(hash: Hex): string | undefined {
  return evmTx(bridgeConfig.paxeerExplorerUrl, hash);
}

export function paxeerAddressUrl(address: Address): string | undefined {
  return `${trim(bridgeConfig.paxeerExplorerUrl)}/address/${address}`;
}

export function foreignAddressUrl(chain: BridgeChain, address: Address): string | undefined {
  if (chain.kind === "solana") return `https://explorer.solana.com/address/${address}`;
  const base = chain.viemChain?.blockExplorers?.default.url;
  return base ? `${trim(base)}/address/${address}` : undefined;
}

/** Explorer URL by route key ("paxeer" or a BRIDGE_CHAINS key). */
export function txUrlByRouteKey(routeKey: string, hash: Hex): string | undefined {
  if (routeKey === PAXEER_ROUTE_KEY) return paxeerTxUrl(hash);
  const chain = BRIDGE_CHAINS.find((c) => c.key === routeKey);
  return chain ? foreignTxUrl(chain, hash) : undefined;
}

export { paxeer };
