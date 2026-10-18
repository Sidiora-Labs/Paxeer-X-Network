import { useCallback, useMemo } from "react";
import { API, EMPTY_LIST } from "@paxeer-network/types";
import { useQuery } from "../useQuery";

export type PeriodKey = "1d" | "3d" | "7d" | "14d" | "30d" | "90d";

export const calculatePositiveRate = (
  periodData?: API.FundingPeriodData,
  period?: PeriodKey,
): number => {
  if (!periodData || !period) {
    return 0;
  }

  const daysMap: Record<PeriodKey, number> = {
    "1d": 1,
    "3d": 3,
    "7d": 7,
    "14d": 14,
    "30d": 30,
    "90d": 90,
  };

  const totalTimes = daysMap[period] * 3;
  return periodData.positive / totalTimes;
};

export const useFundingRateHistory = () => {
  const { data: historyData, isLoading } = useQuery<API.FundingHistory[]>(
    "/v1/public/market_info/funding_history",
  );

  const getPositiveRates = useCallback(
    (
      data: ReadonlyArray<API.FundingHistory> | API.FundingHistory[],
      period: PeriodKey,
    ): Record<string, number> => {
      if (!data?.length) {
        return {};
      }
      return (data as API.FundingHistory[]).reduce(
        (acc, item) => {
          acc[item.symbol] = calculatePositiveRate(
            item.funding[period],
            period,
          );
          return acc;
        },
        {} as Record<string, number>,
      );
    },
    [],
  );

  return useMemo(() => {
    return {
      data: historyData ?? EMPTY_LIST,
      isLoading: isLoading,
      getPositiveRates,
    };
  }, [historyData, isLoading, getPositiveRates]);
};
