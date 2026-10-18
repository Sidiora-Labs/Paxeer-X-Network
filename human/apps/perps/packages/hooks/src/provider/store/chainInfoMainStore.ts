import { API } from "@paxeer-network/types";
import { PAXEER_NETWORK_MAIN_CHAIN_INFO } from "../../middleware/indexedDBManager";
import { mainnetChainFallback } from "./chainInfoFallback";
import { sanitizeChainInfoData } from "./chainInfoValidation";
import { createDataStore } from "./createDataStore";

const CHAIN_INFO_TIMEOUT_MS = 10_000;

export const useMainnetChainsStore = createDataStore<API.Chain>({
  name: "paxeer-network-main-chain-info",
  dbName: "PAXEER_NETWORK_STORE",
  storeName: PAXEER_NETWORK_MAIN_CHAIN_INFO.name,
  keyPath: PAXEER_NETWORK_MAIN_CHAIN_INFO.keyPath,
  endpoint: "/v1/public/chain_info",
  baseUrl: "https://api.paxeer-network.org",
  initData: null,
  fallbackData: mainnetChainFallback,
  timeoutMs: CHAIN_INFO_TIMEOUT_MS,
  sanitizeData: sanitizeChainInfoData,
  persistDataOrigin: true,
});
