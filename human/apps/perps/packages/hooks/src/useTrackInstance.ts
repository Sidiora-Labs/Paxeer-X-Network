import { useContext } from "react";
import useConstant from "use-constant";
import { SimpleDI } from "@paxeer-network/core";
import { PaxeerNetworkContext } from "./paxeerNetworkContext";
import { ENVType, AmplitudeTracker } from "./services/amplitudeTracker";

export const useTrackingInstance = () => {
  const { configStore, amplitudeConfig } = useContext(PaxeerNetworkContext);

  if (!configStore) {
    throw new Error("configStore is not defined, please use PaxeerNetworkProvider");
  }

  const env = configStore.get("env") as ENVType;
  const brokerId = configStore.get("brokerId") as string;

  const trackInstace = useConstant(() => {
    let instance = SimpleDI.get<AmplitudeTracker>("amplitudeTracker");
    if (!instance) {
      instance = new AmplitudeTracker(env, amplitudeConfig ?? undefined, {
        brokerId,
        sdk_version:
          window?.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/net"] ?? "",
      });

      SimpleDI.registerByName("instance", instance);
    }
    return instance;
  });

  return trackInstace;
};
