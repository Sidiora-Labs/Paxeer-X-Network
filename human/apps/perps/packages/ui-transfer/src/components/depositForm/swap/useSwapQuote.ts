import { useMemo } from "react";
import { useSWR } from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { fetcher } from "../utils";
import { type SwapQuoteResponse } from "./type";
import { useSwapSupportedChains } from "./useSwapSupportedChains";

export const useSwapQuote = (options: {
  sourceToken?: API.TokenInfo;
  targetToken?: API.TokenInfo;
  quantity: string;
  chainId?: number;
  slippage: number;
}) => {
  const { sourceToken, targetToken, quantity, chainId, slippage } = options;

  const { chain: swapSupportedChain } = useSwapSupportedChains(chainId);

  const url = useMemo(() => {
    const chainKey = swapSupportedChain?.chain_key;

    const urlSearchParams = new URLSearchParams();

    const isSwapDeposit =
      sourceToken?.swap_enable &&
      sourceToken?.address &&
      targetToken?.address &&
      sourceToken?.address !== targetToken?.address;

    if (!isSwapDeposit || !chainKey || !quantity || !slippage) {
      return null;
    }

    const amount = new Decimal(quantity)
      .mul(Math.pow(10, sourceToken.decimals!))
      // use toFixed(0) to avoid show 1e+21 value
      .toFixed(0);

    urlSearchParams.set("from_token", sourceToken.address!);
    urlSearchParams.set("to_token", targetToken.address!);
    urlSearchParams.set("from_amount", amount);
    urlSearchParams.set("network", chainKey);
    urlSearchParams.set("slippage", slippage.toString());
    urlSearchParams.set("extra_fee_rate", "0");

    return `https://api.woofi.com/v2/swap?${urlSearchParams.toString()}`;
  }, [
    sourceToken,
    targetToken,
    quantity,
    chainId,
    slippage,
    swapSupportedChain,
  ]);

  const { data, isLoading: swapPriceRevalidating } = useSWR<SwapQuoteResponse>(
    url,
    fetcher,
    {
      revalidateOnFocus: false,
      refreshInterval: 10000,
      shouldRetryOnError: false,
    },
  );

  const swapData = useMemo(() => {
    if (!data) return;
    if (data.status === "ok") {
      return data.data;
    }
  }, [data]);

  const error = useMemo(() => {
    if (!data) return;
    if (data.status === "fail") {
      return data.error?.message?.["1inch"]?.error?.description;
    }
  }, [data]);

  const { swapQuantity, swapMinReceived } = useMemo(() => {
    if (!swapData) return {};
    const decimals = targetToken?.decimals! || 0;
    const swapQuantity = new Decimal(swapData.outcomes.amount || 0)
      .div(Math.pow(10, decimals))
      .toString();

    const swapMinReceived = new Decimal(swapData.infos.min_to_amount || 0)
      .div(Math.pow(10, decimals))
      .toString();

    return { swapQuantity, swapMinReceived };
  }, [swapData, targetToken?.decimals]);

  return {
    swapData,
    swapPrice: swapData?.price,
    swapMinReceived,
    swapQuantity,
    swapPriceRevalidating,
    error,
  };
};
