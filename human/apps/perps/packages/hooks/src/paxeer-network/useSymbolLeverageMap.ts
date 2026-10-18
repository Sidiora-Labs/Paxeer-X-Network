import { useMemo } from "react";
import { MarginMode, type API } from "@paxeer-network/types";
import { usePrivateQuery } from "../usePrivateQuery";

const buildKey = (symbol: string, marginMode?: MarginMode) =>
  `${symbol}_${marginMode ?? MarginMode.CROSS}`;

type SymbolLeverageMap = Record<string, number>;

export const useSymbolLeverageMap = () => {
  const { data, error, isLoading, mutate } = usePrivateQuery<
    API.LeverageInfo[]
  >("/v1/client/leverages", {
    revalidateOnFocus: false,
    revalidateOnReconnect: false,
    errorRetryCount: 1,
  });

  const leverages: SymbolLeverageMap = useMemo(() => {
    if (!data || !Array.isArray(data)) return {};

    const map: SymbolLeverageMap = {};
    for (const item of data) {
      const key = buildKey(item.symbol, item.margin_mode);
      map[key] = item.leverage;
    }

    return map;
  }, [data]);

  const getSymbolLeverage = (symbol?: string, marginMode?: MarginMode) => {
    if (!symbol) return undefined;
    const key = buildKey(symbol, marginMode);
    return leverages[key];
  };

  return {
    leverages,
    getSymbolLeverage,
    isLoading,
    error,
    refresh: mutate,
  } as const;
};
