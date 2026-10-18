import { useMemo } from "react";
import {
  MarketsType,
  useFundingRates,
  useMarkets,
  useMarketsStream,
  useQuery,
} from "@paxeer-network/hooks";
import { usePagination } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useMarketsContext } from "../../components/marketsProvider";
import { useSort, searchBySymbol } from "../../utils";
import { useEXchanges } from "./useEXchanges";

function getOpenInterest(open_interest?: number, index_price?: number) {
  return new Decimal(open_interest || 0).mul(index_price || 0).toNumber();
}

export type FundingComparisonReturn = ReturnType<
  typeof useFundingComparisonScript
>;

export const useFundingComparisonScript = () => {
  const { pagination } = usePagination({ pageSize: 100 });
  const { onSort, getSortedList } = useSort();
  const { searchValue } = useMarketsContext();
  const { exchanges, brokerName } = useEXchanges();
  const fundingRates = useFundingRates();
  const [markets] = useMarkets(MarketsType.ALL);

  const { data, isLoading } = useQuery<
    Array<{ symbol: string; exchanges: Array<{ name: string; last: number }> }>
  >("/v1/public/market_info/funding_comparison");
  const { data: futures } = useMarketsStream();

  const processedData = useMemo(() => {
    if (!Array.isArray(data) || !data.length) {
      return [];
    }
    return data.map((row) => {
      const target = futures?.find((item) => item.symbol === row.symbol);
      const marketMeta = markets?.find((item) => item.symbol === row.symbol);
      const result: Record<PropertyKey, any> = {
        symbol: row.symbol,
        leverage: marketMeta?.leverage,
        openInterest: target
          ? getOpenInterest(
              target?.open_interest as number,
              target?.index_price as number,
            )
          : "-",
      };
      for (const item of exchanges) {
        const isCompare = item.includes(` - `);
        if (!isCompare) {
          if (item === brokerName) {
            const rate = fundingRates[row.symbol];
            result[item] = rate("last_funding_rate") ?? null;
          } else {
            const exchange = row.exchanges?.find(
              (e) => e.name.toLowerCase() === item.toLowerCase(),
            );
            result[item] = exchange?.last ?? null;
          }
        } else {
          const [, exchangeName] = item.replace(/ /g, "").split("-");
          const rate = fundingRates[row.symbol];
          const wooFiRate = rate("last_funding_rate") ?? null;
          const exchange = row.exchanges?.find(
            (e) => e.name.toLowerCase() === exchangeName.toLowerCase(),
          );
          const otherRate = exchange?.last ?? null;
          if (wooFiRate !== null && otherRate !== null) {
            result[item] = new Decimal(wooFiRate).sub(otherRate).toString();
          } else {
            result[item] = null;
          }
        }
      }
      return result;
    });
  }, [data, futures, markets, fundingRates, exchanges, brokerName]);

  const filteredData = useMemo(() => {
    return searchBySymbol(processedData, searchValue, "base-type");
  }, [processedData, searchValue, pagination]);

  const dataSource = useMemo(
    () => getSortedList(filteredData),
    [getSortedList, filteredData],
  );

  return {
    dataSource,
    isLoading,
    pagination,
    onSort,
  };
};
