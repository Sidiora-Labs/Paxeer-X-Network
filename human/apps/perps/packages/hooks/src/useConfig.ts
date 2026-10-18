import { useContext } from "react";
import { ConfigStore, type ConfigKey } from "@paxeer-network/core";
import { SDKError } from "@paxeer-network/types";
import { PaxeerNetworkContext } from "./paxeerNetworkContext";

export function useConfig(): ConfigStore;
export function useConfig<T = string>(key: ConfigKey, defaultValue?: T): T;
export function useConfig(key?: ConfigKey, defaultValue?: any) {
  const { configStore } = useContext(PaxeerNetworkContext);

  if (!configStore) {
    throw new SDKError(
      "useConfig must be used within PaxeerNetworkConfigProvider or PaxeerNetworkAppProvider",
    );
  }

  if (typeof key !== "undefined") {
    if (typeof defaultValue !== "undefined") {
      return configStore.getOr(key, defaultValue) as any;
    }
    return configStore.get(key);
  }

  return configStore;
}
