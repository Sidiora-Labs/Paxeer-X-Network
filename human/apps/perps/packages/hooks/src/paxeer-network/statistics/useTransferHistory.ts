import { useMemo } from "react";
import { EMPTY_LIST, type API } from "@paxeer-network/types";
import { usePrivateQuery } from "../../usePrivateQuery";
import { useSymbolsInfo } from "../useSymbolsInfo";

interface TransferHistorySearchParams {
  dataRange?: number[];
  page: number;
  size: number;
  fromId?: string;
  toId?: string;
  side: "IN" | "OUT";
  /**
   * If True, return only internal transfers between main account and sub-accounts.
   * If False, return only internal transfers between main account and other main accounts.
   * If empty, return all transfer history.
   * @default true
   */
  main_sub_only?: boolean;
}

export const useTransferHistory = (parmas: TransferHistorySearchParams) => {
  const { dataRange, page, size, side, fromId, toId, main_sub_only } = parmas;
  const infos = useSymbolsInfo();

  const memoizedQueryKey = useMemo<string>(() => {
    const search = new URLSearchParams();
    search.set("page", page.toString());
    search.set("size", size.toString());
    search.set("side", side);
    if (main_sub_only === true || main_sub_only === false) {
      search.set("main_sub_only", main_sub_only.toString());
    }
    if (dataRange) {
      search.set("start_t", dataRange[0].toString());
      search.set("end_t", dataRange[1].toString());
    }
    return `/v1/internal_transfer_history?${search.toString()}`;
  }, [page, size, fromId, toId, dataRange, main_sub_only]);

  const { data, isLoading, mutate } = usePrivateQuery<API.TransferHistory>(
    memoizedQueryKey,
    {
      // initialSize: 1,
      formatter: (data) => data,
      revalidateOnFocus: false,
      errorRetryCount: 3,
    },
  );

  const parsedData = useMemo<API.TransferHistoryRow[]>(() => {
    if (!Array.isArray(data?.rows) || !data?.rows.length || infos.isNil) {
      return [];
    }
    return data.rows;
  }, [data, infos]);

  return useMemo(
    () =>
      [
        parsedData ?? EMPTY_LIST,
        { meta: data?.meta, isLoading, mutate },
      ] as const,
    [parsedData, data?.meta, isLoading, mutate],
  );
};

export type UseTransferHistoryReturn = ReturnType<typeof useTransferHistory>;
