import { useMemo } from "react";
import { API, chainsInfoMap } from "@paxeer-network/types";
import { useQuery } from "../useQuery";

/**
 * @param token @deprecated, use useTokenInfo instead
 */
export const useChain = (token: string) => {
  const { data, isLoading } = useQuery<API.Chain[]>("/v1/public/token");

  const chains = useMemo(() => {
    if (!data) {
      return null;
    }

    const item = data.find((chain) => chain.token === token);

    if (item) {
      item.chain_details = item.chain_details.map((d) => {
        const chain = chainsInfoMap.get(Number(d.chain_id));
        return {
          ...d,
          chain_name: chain?.chainName ?? "--",
        };
      });
    }

    return item || null;
  }, [data, token]);

  return { chains, isLoading };
};
