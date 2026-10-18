import { useMemo } from "react";
import { API } from "@paxeer-network/types";
import { usePaxeerNetworkContext } from "../paxeerNetworkContext";
import { useQuery } from "../useQuery";
import { useRwaSymbolsInfoStore } from "./useRwaSymbolsInfo";

/**
 * get symbol list
 */
export const useFutures = () => {
  const { dataAdapter } = usePaxeerNetworkContext();

  const rwaSymbolsInfo = useRwaSymbolsInfoStore();

  const { data } = useQuery<API.MarketInfo[]>("/v1/public/futures", {
    // 24 hours
    focusThrottleInterval: 1000 * 60 * 60 * 24,
    revalidateOnFocus: true,
    // 24 hours
    dedupingInterval: 1000 * 60 * 60 * 24,
    // onSuccess will not be called, because /v1/public/futures trigger multiple times
    // onSuccess(data: API.MarketInfo[]) {
    // if (!data || !data?.length) {
    //   return [];
    // }
    // updateMarket(data as API.MarketInfoExt[]);
    // },
  });

  const futures = useMemo(() => {
    if (Array.isArray(data)) {
      return typeof dataAdapter?.symbolList === "function"
        ? dataAdapter.symbolList(data as API.MarketInfoExt[], {
            rwaSymbolsInfo,
          })
        : data;
    }

    return [];
  }, [data, dataAdapter?.symbolList, rwaSymbolsInfo]);

  return {
    data: futures,
  };
};
