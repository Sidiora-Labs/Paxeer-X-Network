import { useMemo } from "react";
import { UseDepositReturn, useSWR } from "@paxeer-network/hooks";
import { API, nativeTokenAddress } from "@paxeer-network/types";

export function useNativeBalance(options: {
  targetChain?: API.Chain;
  fetchBalance: UseDepositReturn["fetchBalance"];
}) {
  const { targetChain, fetchBalance } = options;

  const decimal = targetChain?.network_infos?.currency_decimal;

  const key = useMemo(() => {
    const chainId = targetChain?.network_infos?.chain_id;

    if (!chainId || !decimal) {
      return null;
    }

    return ["nativeBalance", chainId, decimal];
  }, [fetchBalance, targetChain, decimal]);

  const fetcher = async () => {
    const balance = await fetchBalance(nativeTokenAddress, decimal);
    return balance;
  };

  const { data: balance, isLoading } = useSWR<string>(key, fetcher, {
    revalidateOnFocus: true,
    refreshInterval: 3000,
    errorRetryCount: 3,
  });

  return { balance, isLoading };
}
