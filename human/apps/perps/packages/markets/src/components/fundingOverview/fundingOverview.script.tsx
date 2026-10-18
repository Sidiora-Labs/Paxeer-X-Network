import { useMemo } from "react";
import {
  MarketsType,
  useFundingRateHistory,
  useFundingRates,
  useMarkets,
} from "@paxeer-network/hooks";
import { usePagination } from "@paxeer-network/ui";
import { useMarketsContext } from "../../components/marketsProvider";
import { useSort, searchBySymbol } from "../../utils";

export type ProcessedFundingData = {
  symbol: string;
  leverage?: number;
  estFunding: number;
  lastFunding: number;
  fundingInterval: number;
  funding1d: number | string;
  funding3d: number | string;
  funding7d: number | string;
  funding14d: number | string;
  funding30d: number | string;
  funding90d: number | string;
  "1dPositive": number | string;
  "3dPositive": number | string;
  "7dPositive": number | string;
  "14dPositive": number | string;
  "30dPositive": number | string;
  "90dPositive": number | string;
};

export type FundingOverviewReturn = ReturnType<typeof useFundingOverviewScript>;

export const useFundingOverviewScript = () => {
  const { pagination } = usePagination({ pageSize: 100 });
  const [marketData] = useMarkets(MarketsType.ALL);

  const {
    data: fundingHistory,
    isLoading: isHistoryLoading,
    getPositiveRates,
  } = useFundingRateHistory();
  const fundingRates = useFundingRates();

  const { onSort, getSortedList } = useSort();
  const { searchValue } = useMarketsContext();

  const processedData = useMemo((): ProcessedFundingData[] => {
    if (!marketData?.length) {
      return [];
    }

    const periods = ["1d", "3d", "7d", "14d", "30d", "90d"] as const;
    const posRates = periods.reduce(
      (acc, period) => {
        acc[period] = getPositiveRates(fundingHistory, period);
        return acc;
      },
      {} as Record<string, Record<string, number>>,
    );

    // because fundingHistory list is unstable sort, so use marketData instead
    // TODO: feedback to backend to fix this if possible
    return marketData.map((item) => {
      const symbol = item.symbol;
      const history = fundingHistory?.find((h) => h.symbol === symbol);

      const fundingRate = fundingRates[symbol];

      const fundingInterval = getFundingInterval(
        fundingRate("next_funding_time"),
        fundingRate("last_funding_rate_timestamp"),
      );

      return {
        symbol,
        leverage: item.leverage,
        estFunding: fundingRate("est_funding_rate"),
        lastFunding: fundingRate("last_funding_rate"),
        fundingInterval,
        funding1d: history?.funding?.["1d"]?.rate ?? 0,
        funding3d: history?.funding?.["3d"]?.rate ?? 0,
        funding7d: history?.funding?.["7d"]?.rate ?? 0,
        funding14d: history?.funding?.["14d"]?.rate ?? 0,
        funding30d: history?.funding?.["30d"]?.rate ?? 0,
        funding90d: history?.funding?.["90d"]?.rate ?? 0,
        "1dPositive": posRates["1d"][symbol] ?? "-",
        "3dPositive": posRates["3d"][symbol] ?? "-",
        "7dPositive": posRates["7d"][symbol] ?? "-",
        "14dPositive": posRates["14d"][symbol] ?? "-",
        "30dPositive": posRates["30d"][symbol] ?? "-",
        "90dPositive": posRates["90d"][symbol] ?? "-",
      };
    });
  }, [marketData, fundingHistory, fundingRates, getPositiveRates]);

  const filteredData = useMemo(() => {
    return searchBySymbol(processedData, searchValue, "base-type");
  }, [processedData, searchValue, pagination]);

  const dataSource = useMemo(
    () => getSortedList(filteredData),
    [filteredData, getSortedList],
  );

  return { dataSource, isLoading: isHistoryLoading, pagination, onSort };
};

function getFundingInterval(
  next_funding_time: number,
  last_funding_rate_timestamp: number,
) {
  // default interval is 8 hours
  if (!next_funding_time || !last_funding_rate_timestamp) return 8;
  const diff = next_funding_time - last_funding_rate_timestamp;

  return Math.round(diff / 3600000);
}
