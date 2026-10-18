import { useEffect } from "react";
import { type API } from "@paxeer-network/types";
import { getPrecisionByNumber } from "@paxeer-network/utils";
import { useSymbolStore } from "../provider/store/symbolStore";
import { getSymbolDisplayName } from "../useBadgeBySymbol";
import { useQuery } from "../useQuery";
import { useAppStore } from "./appStore";
import { RWA_SYMBOLS_INFO_QUERY_KEY } from "./rwaSymbolsInfoRevalidator";
import { useFutures } from "./useFutures";
import { useMarketStore } from "./useMarket/market.store";

// import { useTokensInfoStore } from "./useTokensInfo/tokensInfo.store";

const publicQueryOptions = {
  // 24 hours, not effective, because revalidateOnFocus is false
  focusThrottleInterval: 1000 * 60 * 60 * 24,
  revalidateOnFocus: false,
  // 24 hours
  dedupingInterval: 1000 * 60 * 60 * 24,
};

const rwaSymbolsInfoQueryOptions = {
  ...publicQueryOptions,
  revalidateOnFocus: true,
  focusThrottleInterval: 1000 * 60,
  dedupingInterval: 1000 * 30,
};

export const usePublicDataObserver = () => {
  const { setSymbolsInfo, setFundingRates, setRwaSymbolsInfo } = useAppStore(
    (state) => state.actions,
  );

  // const setTokensInfo = useTokensInfoStore((state) => state.setTokensInfo);

  const symbols = useSymbolStore((state) => state.data);

  /**
   * symbol config
   */
  // useQuery<Record<string, API.SymbolExt>>(`/v1/public/info`, {
  //   ...publicQueryOptions,
  //   onSuccess(data: API.Symbol[]) {
  //     if (!data || !data?.length) {
  //       return {};
  //     }

  //   },
  // });

  useEffect(() => {
    if (!symbols || !symbols?.length) {
      return;
    }
    const obj: Record<string, API.SymbolExt> = {};

    for (let index = 0; index < symbols.length; index++) {
      const item = symbols[index];
      const arr = item.symbol.split("_");
      const base_dp = getPrecisionByNumber(item.base_tick);
      const quote_dp = getPrecisionByNumber(item.quote_tick);
      const symbol = item.symbol;

      obj[symbol] = {
        ...item,
        base_dp,
        quote_dp,
        base: arr[1],
        quote: arr[2],
        type: arr[0],
        name: `${arr[1]}-${arr[0]}`,
        display_symbol_name: getSymbolDisplayName(
          symbol,
          item.display_symbol_name,
        ),
      };
    }
    setSymbolsInfo(obj);
  }, [symbols]);

  /**
   * symbol config
   * TODO: remove onSuccess because it can't be called when trigger multiple times
   */
  useQuery<API.RwaSymbol[]>(RWA_SYMBOLS_INFO_QUERY_KEY, {
    ...rwaSymbolsInfoQueryOptions,
    onSuccess(data: API.RwaSymbol[]) {
      if (!data || !data?.length) {
        return {};
      }
      const obj: Record<string, API.RwaSymbol> = {};

      for (let index = 0; index < data.length; index++) {
        const item = data[index];
        const arr = item.symbol.split("_");
        obj[item.symbol] = {
          ...item,
          base: arr[1],
          quote: arr[2],
          type: arr[0],
          name: `${arr[1]}-${arr[0]}`,
        };
      }
      setRwaSymbolsInfo(obj);
    },
  });

  /**
   * funding rates
   * TODO: remove onSuccess because it can't be called when trigger multiple times
   */
  useQuery<{ [key: string]: API.FundingRate }>(`/v1/public/funding_rates`, {
    ...publicQueryOptions,
    onSuccess(data: API.FundingRate[]) {
      if (!data || !data?.length) {
        return {};
      }
      const obj = Object.create(null);

      for (let index = 0; index < data.length; index++) {
        const item = data[index];
        obj[item.symbol] = {
          ...item,
          est_funding_rate: getEstFundingRate(item),
        };
      }

      // return obj;
      setFundingRates(obj);
    },
  });

  const { data: futures } = useFutures();
  const { updateMarket } = useMarketStore((state) => state.actions);

  useEffect(() => {
    updateMarket(futures);
  }, [futures]);

  /**
   * token info
   */
  // useQuery<API.Chain[]>(`/v1/public/token`, {
  //   // revalidateOnFocus: false,
  //   ...publicQueryOptions,
  //   onSuccess(data: API.Chain[]) {
  //     if (!data || !data.length) {
  //       return [];
  //     }
  //     setTokensInfo(data);
  //   },
  // });
};

function getEstFundingRate(data: API.FundingRate) {
  if (!data) {
    return;
  }

  const { next_funding_time, est_funding_rate } = data;

  if (Date.now() > next_funding_time) {
    return null;
  }

  return est_funding_rate;
}
