import { useMemo } from "react";
import { useSWR } from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { fetcher } from "../utils";
import { useSwapSupportedChains } from "./useSwapSupportedChains";

type SwapToken = {
  address: string;
  symbol: string;
  decimals: number;
  name: string;
  logoURI: string;
  eip2612: boolean;
  tags: string[];
};

export const useSwapTokens = (
  chainId?: string | number,
  enableSwapDeposit?: boolean,
) => {
  const { chain: swapSupportedChain } = useSwapSupportedChains(chainId);
  const url = useMemo(() => {
    if (enableSwapDeposit && swapSupportedChain) {
      return `https://widget-api.woofi.com/1inch_tokens?network=${swapSupportedChain.chain_key}`;
    }

    return null;
  }, [chainId, enableSwapDeposit, swapSupportedChain]);

  const { data } = useSWR(url, fetcher, {
    revalidateOnFocus: false,
  });

  // {
  //   "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee": {
  //     address: "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
  //     symbol: "ETH",
  //     decimals: 18,
  //     name: "Ether",
  //     logoURI:
  //       "https://tokens.1inch.io/0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee.png",
  //     eip2612: false,
  //     tags: ["crosschain", "GROUP:ETH", "native", "PEG:ETH"],
  //   }
  // }
  const tokens = useMemo(() => {
    const tokenMap = data?.tokens as Record<string, SwapToken>;
    return Object.values(tokenMap || {}).map((item) => ({
      symbol: item.symbol,
      address: item.address,
      decimals: item.decimals,
      // default precision is 8 for 1inch tokens
      precision: 8,
      swap_enable: true,
      // need to show token icon
      logo_uri: item.logoURI,
    }));
  }, [data]);

  return tokens as unknown as API.TokenInfo[];
};
