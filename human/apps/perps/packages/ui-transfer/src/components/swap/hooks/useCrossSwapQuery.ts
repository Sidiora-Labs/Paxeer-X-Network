import { useCallback } from "react";
import { utils } from "@paxeer-network/core";
import {
  useAccountInstance,
  useBoolean,
  useConfig,
} from "@paxeer-network/hooks";
import {
  swapSupportApiUrl,
  woofiDexCrossSwapChainRouterAbi,
} from "../constants";

export const useWooCrossSwapQuery = () => {
  const [loading, { setTrue: start, setFalse: stop }] = useBoolean(false);
  const brokerId = useConfig("brokerId");

  const account = useAccountInstance();

  const dstValutDeposit = useCallback(() => {
    return {
      accountId: account.accountIdHashStr,
      brokerHash: utils.parseBrokerHash(brokerId),
      tokenHash: utils.parseTokenHash("USDC"),
    };
  }, [account, brokerId]);

  const queryDestinationFee = useCallback(
    async (
      crossChainRouteAddress: string,
      dst: {
        chainId: string;
        bridgedToken: string;
        toToken: string;
        minToAmount: bigint;
        paxeerNetworkNativeFees: bigint;
      },
    ) => {
      if (!account.walletAdapter) {
        throw new Error("walletAdapter is not ready");
      }
      const quotoLZFee = await account.walletAdapter.call(
        crossChainRouteAddress,
        "quoteLayerZeroFee",
        [account.address, dst, dstValutDeposit()],
        {
          abi: woofiDexCrossSwapChainRouterAbi,
        },
      );

      return utils.formatByUnits(quotoLZFee[0]);
    },
    [],
  );

  /// swap enquiry
  const query = useCallback(
    (inputs: {
      srcNetwork: string;
      srcToken: string;
      amount: bigint;
      slippage: number;
      dstToken: string;
      crossChainRouteAddress: string;
    }) => {
      if (loading) return Promise.resolve();
      start();

      const params = {
        // src_network: inputs.srcNetwork,
        src_network: inputs.srcNetwork,
        dst_network: "arbitrum",
        src_token: inputs.srcToken,
        dst_token: inputs.dstToken, //account.assetsManager.usdcAddress,
        src_amount: inputs.amount, //inputs.amount,
        slippage: inputs.slippage || 1,
        // to_token:account.assetsManager.usdcAddress,
      };

      const queryString = Object.entries(params)
        .map(([key, value]) => `${key}=${encodeURIComponent(value as string)}`)
        .join("&");

      return fetch(
        `${swapSupportApiUrl}/woofi_dex/cross_chain_swap?${queryString}`,
      )
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
        .then((swapInfo) => {
          return queryDestinationFee(inputs.crossChainRouteAddress, {
            chainId: swapInfo.dst_infos.chain_id,
            bridgedToken: swapInfo.dst_infos.bridged_token,
            toToken: swapInfo.dst_infos.to_token,
            minToAmount: BigInt(swapInfo.dst_infos.min_to_amount),
            paxeerNetworkNativeFees: 0n,
          })
            .then((data) => {
              return {
                ...swapInfo,
                dst_infos: {
                  ...swapInfo.dst_infos,
                  gas_fee: data,
                },
              };
            })
            .catch((e) => {
              console.error("queryDestinationFee error", e);
            });
        })
        .finally(() => stop());
    },

    [loading, account],
  );

  return {
    query,
    loading,
  };
};
