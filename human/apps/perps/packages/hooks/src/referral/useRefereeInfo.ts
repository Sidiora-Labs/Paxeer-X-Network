import { generateKeyFun } from "./swr";
import { useMemo } from "react";
import { RefferalAPI } from "./api";
import { usePrivateInfiniteQuery } from "../usePrivateInfiniteQuery";
import { API } from "@paxeer-network/types";

type Params = {
  //** default is 10 */
  size?: number;
  /**
   * @deprecated 
   * YYYY-MM-dd */
  startDate?: string;
  /**
   * @deprecated 
   * YYYY-MM-dd */
  endDate?: string;
  /** default is 1 */
  initialSize?: number;
  sort?:
    | "ascending_code_binding_time"
    | "descending_code_binding_time"
    | "ascending_referral_rebate"
    | "descending_referral_rebate"
    | "ascending_volume"
    | "descending_volume";
  page?: number;
};

export const useRefereeInfo = (params: Params) => {
  const { size = 10, startDate, endDate, initialSize, page, sort } = params;

  const response = usePrivateInfiniteQuery<any>(
    generateKeyFun({
      path: "/v1/referral/referee_info",
      size,
      startDate,
      endDate,
      page,
      sort,
    }),
    {
      initialSize: initialSize,
      // revalidateFirstPage: false,
      // onError: (err) => {
      //   console.error("fetch failed::::", err);
      // },
      formatter: (data) => data,
      revalidateOnFocus: false,
    }
  );

  const loadMore = () => {
    response.setSize(response.size + 1);
  };

  const meta = useMemo(():
    | {
        total: number;
        records_per_page: number;
        current_page: number;
      }
    | undefined => {
    return response.data?.[0]?.meta;
  }, [response.data]);

  const total = useMemo(() => {
    return meta?.total || 0;
  }, [meta]);

  const flattenOrders = useMemo((): RefferalAPI.RefereeInfoItem[] | null => {
    if (!response.data) {
      return null;
    }

    return response.data?.map((item) => item.rows)?.flat();
  }, [response.data]);

  return [
    flattenOrders,
    {
      total,
      isLoading: response.isLoading,
      refresh: response.mutate,
      loadMore,
      meta,
    },
  ] as const;
};
