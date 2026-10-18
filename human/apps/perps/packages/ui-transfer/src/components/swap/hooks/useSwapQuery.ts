import { useCallback } from "react";
import { useAccountInstance, useBoolean } from "@paxeer-network/hooks";
import { swapSupportApiUrl } from "../constants";

export const useWooSwapQuery = () => {
  const account = useAccountInstance();
  const [loading, { setTrue: start, setFalse: stop }] = useBoolean(false);

  /// Swap quote
  const query = useCallback(
    (inputs: any) => {
      if (loading) return Promise.resolve();
      start();

      const params = {
        network: inputs.srcNetwork,
        // network: "arbitrum",
        from_token: inputs.srcToken,
        to_token: inputs.dstToken, //account.assetsManager.usdcAddress,
        from_amount: inputs.amount, //inputs.amount,
        slippage: inputs.slippage || 1,
        // to_token:account.assetsManager.usdcAddress,
      };

      const queryString = Object.entries(params)
        .map(([key, value]) => `${key}=${encodeURIComponent(value as string)}`)
        .join("&");

      return fetch(`${swapSupportApiUrl}/woofi_dex/swap?${queryString}`)
        .then((res) => {
          if (!res.ok) {
            return res.json().then((data) => {
              throw new Error(data.error.message);
            });
          }
          return res.json();
        })
        .then((data) => {
          if (data.status === "ok") {
            return data.data;
          }
          throw new Error(data.message);
        })
        .finally(() => stop());
    },
    [account],
  );

  return {
    query,
    loading,
  };
};
