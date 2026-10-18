import {
  API,
  ArbitrumMainnetChainInfo,
  ArbitrumSepoliaChainInfo,
  SolanaDevnetChainInfo,
  SolanaMainnetChainInfo,
} from "@paxeer-network/types";

/** Static mainnet chain_info used when broker API data is unavailable. */
export const mainnetChainFallback = [
  ArbitrumMainnetChainInfo,
  SolanaMainnetChainInfo,
] as unknown as API.Chain[];

/** Static testnet chain_info used when broker API data is unavailable. */
export const testnetChainFallback = [
  ArbitrumSepoliaChainInfo,
  SolanaDevnetChainInfo,
] as unknown as API.Chain[];
