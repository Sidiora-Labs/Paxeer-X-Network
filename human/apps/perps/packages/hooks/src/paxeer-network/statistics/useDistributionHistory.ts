import { useMemo } from "react";
import { API, EMPTY_LIST } from "@paxeer-network/types";
import { usePrivateQuery } from "../../usePrivateQuery";
import { useSymbolsInfo } from "../useSymbolsInfo";

type DistributionSearchParams = {
  /**
   * Data range for the distribution history
   * noted that the time stamp is a 13-digits timestamp
   * the first element is the start date and the second element is the end date
   * @default [Now subtract 3 months, Now]
   */
  dataRange?: number[];
  type?: string;
  page: number;
  pageSize: number;
};

export const useDistributionHistory = (parmas: DistributionSearchParams) => {
  const { type, dataRange, page, pageSize } = parmas;

  const infos = useSymbolsInfo();

  const getKey = () => {
    const search = new URLSearchParams();

    if (typeof type !== "undefined" && type !== "All") {
      search.set("type", type);
    }
    search.set("page", page.toString());
    search.set("size", pageSize.toString());

    if (dataRange) {
      search.set("start_t", dataRange[0].toString());
      search.set("end_t", dataRange[1].toString());
    }

    return `/v1/client/distribution_history?${search.toString()}`;
  };
  const { data, isLoading, isValidating } =
    usePrivateQuery<API.FundingFeeHistory>(getKey(), {
      // initialSize: 1,
      formatter: (data) => data,
      revalidateOnFocus: false,
      errorRetryCount: 3,
    });

  const parsedData = useMemo<
    (API.FundingFeeRow & { annual_rate: number })[]
  >(() => {
    if (!Array.isArray(data?.rows) || !data?.rows.length || infos.isNil) {
      return [];
    }

    return data.rows.map((row: API.FundingFeeRow) => {
      const config = infos[row.symbol];
      return {
        ...row,
        annual_rate: row.funding_rate * (24 / config("funding_period")),
      };
    });
  }, [data, infos]);

  return useMemo(
    () =>
      [
        parsedData ?? EMPTY_LIST,
        { meta: data?.meta, isLoading, isValidating },
      ] as const,
    [parsedData, data?.meta, isLoading, isValidating],
  );
};

export type UseDistributionHistoryReturn = ReturnType<
  typeof useDistributionHistory
>;
