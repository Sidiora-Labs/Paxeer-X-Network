import React, { useMemo } from "react";
import type { API } from "@paxeer-network/types";
import { EMPTY_LIST } from "@paxeer-network/types";
import { usePrivateQuery } from "../../usePrivateQuery";
import { useSymbolsInfo } from "../useSymbolsInfo";

interface TransferHistorySearchParams {
  dataRange?: number[];
  page: number;
  size: number;
}

export const useVaultsHistory = (parmas: TransferHistorySearchParams) => {
  const { dataRange, page, size } = parmas;

  const infos = useSymbolsInfo();

  const memoizedQueryKey = React.useMemo<string>(() => {
    const search = new URLSearchParams();
    search.set("page", page.toString());
    search.set("size", size.toString());
    if (dataRange) {
      search.set("start_t", dataRange[0].toString());
      search.set("end_t", dataRange[1].toString());
    }
    return `/v1/account_sv_transaction_history?${search.toString()}`;
  }, [page, size, dataRange]);

  const { data, isLoading, mutate } = usePrivateQuery<API.StrategyVaultHistory>(
    memoizedQueryKey,
    {
      formatter: (data) => data,
      revalidateOnFocus: false,
      errorRetryCount: 3,
    },
  );

  const parsedData = React.useMemo<API.StrategyVaultHistoryRow[]>(() => {
    if (!Array.isArray(data?.rows) || !data?.rows.length || infos.isNil) {
      return [];
    }
    return data.rows;
  }, [data, infos]);

  return useMemo(
    () => [parsedData ?? EMPTY_LIST, { meta: data?.meta, isLoading, mutate }] as const,
    [parsedData, data?.meta, isLoading, mutate],
  );
};

export type VaultsHistoryReturn = ReturnType<typeof useVaultsHistory>;
