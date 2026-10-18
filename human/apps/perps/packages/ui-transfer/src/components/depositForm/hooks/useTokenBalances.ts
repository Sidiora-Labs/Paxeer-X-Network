import { useMemo } from "react";
import { UseDepositReturn, useSWR } from "@paxeer-network/hooks";
import { API, EMPTY_OBJECT } from "@paxeer-network/types";
import { mergeTokens } from "../utils";

export const useTokenBalances = (options: {
  paxeerNetworkTokens: API.TokenInfo[];
  swapTokens: API.TokenInfo[];
  fetchBalances: UseDepositReturn["fetchBalances"];
}) => {
  const { paxeerNetworkTokens, swapTokens, fetchBalances } = options;

  const tokens = useMemo(
    () => mergeTokens(paxeerNetworkTokens, swapTokens),
    [paxeerNetworkTokens, swapTokens],
  );

  const key = useMemo(() => {
    if (tokens?.length > 0) {
      // use tokens serialization as key, ensure tokens change to re-request
      return ["tokenBalances", tokens.map((item) => item.symbol).join(",")];
    }
    return null;
  }, [tokens]);

  const fetcher = async () => {
    if (!tokens || tokens.length === 0) {
      return EMPTY_OBJECT;
    }
    const balances = await fetchBalances(tokens);
    // console.info("tokenBalances =>", balances);

    return balances;
  };

  const { data: balances = EMPTY_OBJECT, isLoading } = useSWR(key, fetcher, {
    revalidateOnFocus: true,
    refreshInterval: 10000,
    errorRetryInterval: 3000,
    errorRetryCount: 3,
  });

  const loading = useMemo(() => {
    if (Object.keys(balances).length === 0) {
      return true;
    }
    return isLoading;
  }, [isLoading, balances]);

  return { balances, isLoading: loading };
};
