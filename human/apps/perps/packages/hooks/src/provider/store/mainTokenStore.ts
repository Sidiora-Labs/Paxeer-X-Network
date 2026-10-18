import { API } from "@paxeer-network/types";
import { PAXEER_NETWORK_MAIN_TOKEN } from "../../middleware/indexedDBManager";
import { createDataStore } from "./createDataStore";

export const useMainTokenStore = createDataStore<API.Token>({
  name: "paxeer-network-main-token",
  dbName: "PAXEER_NETWORK_STORE",
  storeName: PAXEER_NETWORK_MAIN_TOKEN.name,
  keyPath: PAXEER_NETWORK_MAIN_TOKEN.keyPath,
  endpoint: "/v1/public/token",
  baseUrl: "https://api.paxeer-network.org",
});
