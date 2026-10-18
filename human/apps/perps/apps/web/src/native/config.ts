import type { ConfigStore, ConfigKey } from "@paxeer-network/core";
import { ChainNamespace } from "@paxeer-network/types";
import { REST_API_URL } from "@paxeer-network/paxeer-backend/core/perps/config";
export class NativeConfigStore implements ConfigStore {
  private values = new Map<string, unknown>(
    Object.entries({
      apiBaseUrl: REST_API_URL,
      klineDataUrl: REST_API_URL,
      privateWsUrl: "",
      publicWsUrl: "",
      operatorUrl: "",
      domain: "paxeer-network",
      brokerId: "paxeer-network",
      brokerName: "paxeer-network",
      networkId: "mainnet",
      env: "prod",
      chainNamespace: ChainNamespace.evm,
    }),
  );
  get<T = string>(key: ConfigKey): T {
    return this.values.get(key) as T;
  }
  getOr<T = string>(key: ConfigKey, fallback: T): T {
    return this.values.has(key) ? this.get<T>(key) : fallback;
  }
  set<T>(key: ConfigKey, value: T) {
    this.values.set(key, value);
  }
  clear() {
    this.values.clear();
  }
}

export {
  erc20Abi,
  DIAMOND_ADDRESS,
  COLLATERAL_TOKENS,
} from "@paxeer-network/paxeer-backend/core/perps/config";
