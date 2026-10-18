import { useMemo } from "react";
import { useSWR } from "@paxeer-network/hooks";
import { fetcher } from "../utils";
import type { SupportedChainItem, SupportedChainsResponse } from "./type";

const SUPPORTED_CHAINS_URL =
  "https://api.woofi.com/woofi_dex/depositor/supported_chains";

export const useSwapSupportedChains = (chainId?: number | string) => {
  const { data, isLoading } = useSWR<SupportedChainsResponse>(
    SUPPORTED_CHAINS_URL,
    fetcher,
    {
      revalidateOnFocus: false,
    },
  );

  const chains = useMemo((): SupportedChainItem[] => {
    if (!data || data.status !== "ok") return [];
    return data.data?.perp_vault?.filter((c) => c.same_chain_deposit === true);
  }, [data]);

  const chain = useMemo(() => {
    if (!chainId || !chains?.length) return null;
    return chains.find((c) => c.chain_id === parseInt(chainId as string));
  }, [chains, chainId]);

  return {
    chains,
    chain,
    isLoading,
  };
};
