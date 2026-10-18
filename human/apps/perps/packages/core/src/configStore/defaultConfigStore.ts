import { ChainNamespace } from "@paxeer-network/types";
import { NetworkId } from "@paxeer-network/types";
import { ConfigKey, ConfigStore } from "./configStore";

type ChainNamespaceType = (typeof ChainNamespace)[keyof typeof ChainNamespace];

export type URLS = {
  apiBaseUrl: string;
  publicWsUrl: string;
  privateWsUrl: string;
  operatorUrl: Record<ChainNamespaceType, string>;
};

export const API_URLS: Record<NetworkId, URLS> = {
  mainnet: {
    apiBaseUrl: "https://api.paxeer-network.org",
    publicWsUrl: "wss://ws-evm.paxeer-network.org",
    privateWsUrl: "wss://ws-private-evm.paxeer-network.org",
    operatorUrl: {
      EVM: "https://operator-evm.paxeer-network.org",
      SOL: "https://operator-solana.paxeer-network.org",
    },
  },
  testnet: {
    apiBaseUrl: "https://testnet-api.paxeer-network.org",
    publicWsUrl: "wss://testnet-ws-evm.paxeer-network.org",
    privateWsUrl: "wss://testnet-ws-private-evm.paxeer-network.org",
    operatorUrl: {
      EVM: "https://testnet-operator-evm.paxeer-network.org",
      SOL: "https://testnet-operator-sol.paxeer-network.org",
    },
  },
};

export class DefaultConfigStore implements ConfigStore {
  protected map: Map<ConfigKey, any>;

  constructor(init: Partial<Record<ConfigKey, any>>) {
    const env = init.env || "prod";
    const networkId = (init.networkId || "mainnet") as NetworkId;
    const urls = API_URLS[networkId];
    const brokerId = init?.brokerId || "paxeer-network";
    const brokerName = init?.brokerName || "paxeer-network";
    const chainNamespace = init?.chainNamespace || ChainNamespace.evm;

    this.map = new Map<ConfigKey, any>([
      ["env", env],
      ["brokerId", brokerId],
      ["brokerName", brokerName],
      ["networkId", networkId],
      ["chainNamespace", chainNamespace],
      ["apiBaseUrl", urls["apiBaseUrl"]],
      ["publicWsUrl", urls["publicWsUrl"]],
      ["privateWsUrl", urls["privateWsUrl"]],
      ["operatorUrl", urls["operatorUrl"]],
    ]);

    // this.solanaMap = new Map<ConfigKey, any>([]);
  }
  get<T>(key: ConfigKey): T {
    const value = this.map.get(key);
    if (typeof value !== "object" || value === null) {
      return value;
    }

    return value[this.get("chainNamespace") as ChainNamespace] as T;
  }
  getOr<T>(key: ConfigKey, defaultValue: T): T {
    return this.map.get(key) ?? defaultValue;
  }
  set<T>(key: ConfigKey, value: T): void {
    this.map.set(key, value);
  }
  clear(): void {
    throw new Error("Method not implemented.");
  }
}
