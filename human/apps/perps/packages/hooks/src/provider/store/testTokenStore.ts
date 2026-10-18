import {
  API,
  ArbitrumSepoliaTokenInfo,
  SolanaDevnetTokenInfo,
  TesnetTokenFallback,
} from "@paxeer-network/types";
import { PAXEER_NETWORK_TEST_TOKEN } from "../../middleware/indexedDBManager";
import { createDataStore } from "./createDataStore";

const testnetTokenFallback = TesnetTokenFallback([
  ArbitrumSepoliaTokenInfo,
  SolanaDevnetTokenInfo,
]);

export const useTestTokenStore = createDataStore<API.Token>({
  name: "paxeer-network-main-token",
  dbName: "PAXEER_NETWORK_STORE",
  storeName: PAXEER_NETWORK_TEST_TOKEN.name,
  keyPath: PAXEER_NETWORK_TEST_TOKEN.keyPath,
  endpoint: "/v1/public/token",
  initData: testnetTokenFallback as API.Token[],
});
