/**
 * The bridge chain registry — one entry per bridge pair, transcribed from
 * contracts/bridge/evm/chains/<name>/config.json and
 * contracts/bridge/solana/chains/solana/config.json.
 *
 * `chainId` is the uint64 registered in the layerxbridge module (the EVM
 * chain id; 91600046870081 for Solana). Finality depths and caps are the
 * configured values from those files; the values enforced on-chain are read
 * back live via getChain/getCap/caps and always win over what is here.
 *
 * Wired-up state is derived, never asserted: an EVM chain is `configured`
 * only when a vault address is deployed (env) — the repo configs carry no
 * vault, so chains ship as `not-deployed`. Solana is `awaiting-backend`:
 * deposits go to the Solana custody program (no program id yet) and
 * withdrawals need the Solana wallet + bridge backend; this EVM-wallet UI
 * renders the route but executes nothing for it.
 */
import {
  arbitrum,
  avalanche,
  base,
  bsc,
  hyperEvm,
  mainnet,
  optimism,
  polygon,
  type Chain,
} from "viem/chains";
import { defineChain, type Address } from "viem";

import { bridgeConfig } from "./config";

export const PAXEER_CHAIN_ID = 125;

export const paxeer = defineChain({
  id: PAXEER_CHAIN_ID,
  name: "Paxeer",
  nativeCurrency: { name: "Paxeer", symbol: "PAX", decimals: 18 },
  rpcUrls: {
    default: { http: bridgeConfig.paxeerRpcUrl ? [bridgeConfig.paxeerRpcUrl] : [] },
  },
  blockExplorers: {
    default: { name: "Paxscan", url: bridgeConfig.paxeerExplorerUrl },
  },
});

export type BridgeAsset = {
  /** Stable key within the chain. */
  key: string;
  symbol: string;
  decimals: number;
  /** ERC-20 address on the foreign chain; the zero address is the native coin. */
  address: Address;
  /**
   * The address the Paxeer precompile keys the pair by (config `asset_id`):
   * the token itself on EVM chains, the mint's 20-byte handle on Solana.
   */
  assetId: Address;
  isNative: boolean;
  /** Config caps (base units, decimal strings) — display fallback only. */
  perTxCap: string;
  totalCap: string;
};

export type RouteState = "configured" | "not-deployed" | "awaiting-backend";

export type BridgeChain = {
  /** Route key; also the folder name under contracts/bridge. */
  key: string;
  label: string;
  kind: "evm" | "solana";
  /** The chain id registered with the layerxbridge module. */
  chainId: bigint;
  /** Viem chain definition (EVM routes only). */
  viemChain?: Chain;
  /** PaxeerXVault deployment; undefined until the chain is deployed. */
  vault?: Address;
  finalityDepth: number;
  /** Rough per-block seconds — drives the "~N min" finality estimate only. */
  blockSeconds: number;
  nativeSymbol: string;
  state: RouteState;
  assets: BridgeAsset[];
};

const NATIVE: Address = "0x0000000000000000000000000000000000000000";

function evmNative(symbol: string, perTxCap: string, totalCap: string): BridgeAsset {
  return {
    key: symbol.toLowerCase(),
    symbol,
    decimals: 18,
    address: NATIVE,
    assetId: NATIVE,
    isNative: true,
    perTxCap,
    totalCap,
  };
}

type EvmChainInput = {
  key: string;
  label: string;
  viemChain: Chain;
  finalityDepth: number;
  blockSeconds: number;
  nativeSymbol: string;
  assets: BridgeAsset[];
  vault?: Address;
};

function evmChain(input: EvmChainInput): BridgeChain {
  return {
    key: input.key,
    label: input.label,
    kind: "evm",
    chainId: BigInt(input.viemChain.id),
    viemChain: input.viemChain,
    vault: input.vault,
    finalityDepth: input.finalityDepth,
    blockSeconds: input.blockSeconds,
    nativeSymbol: input.nativeSymbol,
    state: input.vault ? "configured" : "not-deployed",
    assets: input.assets,
  };
}

const ETH_CAPS: [string, string] = ["250000000000000000000", "5000000000000000000000"];

export const BRIDGE_CHAINS: BridgeChain[] = [
  evmChain({
    key: "ethereum",
    label: "Ethereum",
    viemChain: mainnet,
    finalityDepth: 64,
    blockSeconds: 12,
    nativeSymbol: "ETH",
    assets: [evmNative("ETH", ...ETH_CAPS)],
    vault: bridgeConfig.vaults.ethereum,
  }),
  evmChain({
    key: "base",
    label: "Base",
    viemChain: base,
    finalityDepth: 1800,
    blockSeconds: 2,
    nativeSymbol: "ETH",
    assets: [evmNative("ETH", ...ETH_CAPS)],
    vault: bridgeConfig.vaults.base,
  }),
  evmChain({
    key: "arbitrum",
    label: "Arbitrum",
    viemChain: arbitrum,
    finalityDepth: 3600,
    blockSeconds: 0.25,
    nativeSymbol: "ETH",
    assets: [evmNative("ETH", ...ETH_CAPS)],
    vault: bridgeConfig.vaults.arbitrum,
  }),
  evmChain({
    key: "optimism",
    label: "Optimism",
    viemChain: optimism,
    finalityDepth: 1800,
    blockSeconds: 2,
    nativeSymbol: "ETH",
    assets: [evmNative("ETH", ...ETH_CAPS)],
    vault: bridgeConfig.vaults.optimism,
  }),
  evmChain({
    key: "bnb",
    label: "BNB Chain",
    viemChain: bsc,
    finalityDepth: 30,
    blockSeconds: 3,
    nativeSymbol: "BNB",
    assets: [evmNative("BNB", "1000000000000000000000", "20000000000000000000000")],
    vault: bridgeConfig.vaults.bnb,
  }),
  evmChain({
    key: "polygon",
    label: "Polygon",
    viemChain: polygon,
    finalityDepth: 256,
    blockSeconds: 2,
    nativeSymbol: "POL",
    assets: [evmNative("POL", "500000000000000000000000", "10000000000000000000000000")],
    vault: bridgeConfig.vaults.polygon,
  }),
  evmChain({
    key: "avalanche",
    label: "Avalanche",
    viemChain: avalanche,
    finalityDepth: 12,
    blockSeconds: 2,
    nativeSymbol: "AVAX",
    assets: [evmNative("AVAX", "10000000000000000000000", "200000000000000000000000")],
    vault: bridgeConfig.vaults.avalanche,
  }),
  evmChain({
    key: "hyperevm",
    label: "HyperEVM",
    viemChain: hyperEvm,
    finalityDepth: 60,
    blockSeconds: 1,
    nativeSymbol: "HYPE",
    assets: [evmNative("HYPE", "20000000000000000000000", "400000000000000000000000")],
    vault: bridgeConfig.vaults.hyperevm,
  }),
  {
    key: "solana",
    label: "Solana",
    kind: "solana",
    chainId: 91600046870081n,
    finalityDepth: 32,
    blockSeconds: 0.4,
    nativeSymbol: "SOL",
    state: "awaiting-backend",
    assets: [
      {
        key: "sol",
        symbol: "SOL",
        decimals: 9,
        address: NATIVE,
        assetId: "0xcf996523B5d068A26f0aa8a116602fE5033Ee3A1",
        isNative: true,
        perTxCap: "5000000000000",
        totalCap: "100000000000000",
      },
      {
        key: "sid",
        symbol: "SID",
        decimals: 6,
        address: NATIVE,
        assetId: "0x21f7b20a555199fa73A238B1a91FD0f549068fEe",
        isNative: false,
        perTxCap: "1000000000000",
        totalCap: "50000000000000",
      },
    ],
  },
];

export const PAXEER_ROUTE_KEY = "paxeer";

export function bridgeChainByKey(key: string): BridgeChain | undefined {
  return BRIDGE_CHAINS.find((c) => c.key === key);
}

export function bridgeChainById(chainId: bigint): BridgeChain | undefined {
  return BRIDGE_CHAINS.find((c) => c.chainId === chainId);
}

/** Human "~N min" estimate for reaching a chain's finality depth. */
export function finalityEstimate(chain: BridgeChain): string {
  const seconds = chain.finalityDepth * chain.blockSeconds;
  if (seconds < 90) return `~${Math.max(1, Math.round(seconds))} sec`;
  const minutes = Math.round(seconds / 60);
  return minutes < 90 ? `~${minutes} min` : `~${(minutes / 60).toFixed(1)} hr`;
}
