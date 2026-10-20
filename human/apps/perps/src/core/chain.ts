// Layer 2 — Core chain definition.
// Canonical Paxeer Mainnet chain object. Single source of truth — used by
// wagmi, viem clients, and any UI that needs `chain.id` / explorer URLs.
//
// Imports allowed from: types only. (Layer 2 may import viem at runtime —
// viem is a primitive, not a domain dep.)

import { defineChain } from "viem";
import { getPaxeerRpcUrls } from "@/core/rpc";

// `rpcUrls.default.http` is what wagmi advertises when a wallet adds
// the Paxeer network for the first time (`wallet_addEthereumChain`).
// Listing every endpoint here lets wallets that ignore the user's own
// RPC config (e.g. Rabby's "use the chain default") still hit a
// healthy fallback if one node is down.
export const paxeerChain = defineChain({
  id: 125,
  name: "Paxeer Mainnet",
  nativeCurrency: { name: "PAX", symbol: "PAX", decimals: 18 },
  rpcUrls: {
    default: {
      http: getPaxeerRpcUrls(),
    },
  },
  blockExplorers: {
    default: { name: "PaxScan", url: "https://paxscan.io" },
  },
});
