import { API } from "@paxeer-network/types";
import { PAXEER_NETWORK_SYMBOLS } from "../../middleware/indexedDBManager";
import { createDataStore } from "./createDataStore";

export const useSymbolStore = createDataStore<API.Symbol>({
  name: "paxeer-network-main-symbols",
  dbName: "PAXEER_NETWORK_STORE",
  storeName: PAXEER_NETWORK_SYMBOLS.name,
  keyPath: PAXEER_NETWORK_SYMBOLS.keyPath,
  endpoint: "/v1/public/info",
});
