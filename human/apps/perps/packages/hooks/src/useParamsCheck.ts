import { useEffect } from "react";

export const useParamsCheck = (params: { brokerId?: string }) => {
  useEffect(() => {
    if (!params.brokerId || params.brokerId === "paxeer-network") {
      console.warn(
        "WARNING:" +
          "\n" +
          "========================" +
          "\n" +
          "[PaxeerNetworkConfigProvider]: please provide your brokerId" +
          "\n" +
          "========================"
      );
    }
  }, [params.brokerId]);
};
