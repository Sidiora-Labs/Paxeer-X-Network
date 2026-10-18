import { useMemo } from "react";
import type { SWRConfiguration } from "swr";
import type { API } from "@paxeer-network/types";
import { usePrivateQuery } from "../../usePrivateQuery";
import { useSymbolsInfo } from "../useSymbolsInfo";

type FundingSearchParams = {
  /**
   * Data range for the funding fee history
   * noted that the time stamp is a 13-digits timestamp
   * the first element is the start date and the second element is the end date
   * @default [Now subtract 3 months, Now]
   */
  dataRange?: number[];
  symbol?: string;
  page?: number;
  pageSize?: number;
};

export const useFundingFeeHistory = (
  params: FundingSearchParams,
  options?: SWRConfiguration,
) => {
  const { symbol, dataRange, page = 1, pageSize = 10 } = params;

  const infos = useSymbolsInfo();

  const getKey = () => {
    // console.log("symbol", symbol, dataRange);

    const search = new URLSearchParams();

    if (typeof symbol !== "undefined" && symbol !== "All") {
      search.set("symbol", symbol);
    }
    search.set("page", `${page}`);
    search.set("size", `${pageSize}`);

    if (dataRange) {
      search.set("start_t", dataRange[0].toString());
      search.set("end_t", dataRange[1].toString());
    }

    return `/v1/funding_fee/history?${search.toString()}`;
  };
  const { data, isLoading, isValidating } =
    usePrivateQuery<API.FundingFeeHistory>(getKey(), {
      // initialSize: 1,
      formatter: (data) => data,
      revalidateOnFocus: false,
      errorRetryCount: 3,
      // keepPreviousData: true,
      ...options,
    });

  const parsedData = useMemo<
    (API.FundingFeeRow & { annual_rate: number })[] | null
  >(() => {
    if (!data || !Array.isArray(data?.rows) || infos.isNil) {
      return null;
    }

    return data.rows.map((row: API.FundingFeeRow) => {
      const config = infos[row.symbol];
      return {
        ...row,
        annual_rate: row.funding_rate * (24 / config("funding_period")) * 365,
      };
    });
  }, [data, infos]);

  return useMemo(
    () => [parsedData, { meta: data?.meta, isLoading, isValidating }] as const,
    [parsedData, data?.meta, isLoading, isValidating],
  );
};

export type UseFundingFeeHistoryReturn = ReturnType<
  typeof useFundingFeeHistory
>;
