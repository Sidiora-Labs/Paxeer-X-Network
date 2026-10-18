import { usePrivateInfiniteQuery } from "../usePrivateInfiniteQuery";
import { generateKeyFun } from "./swr";
import { useMemo } from "react";

type Params = {
  //** default is 10 */
  size?: number;
  //** YYYY-MM-dd */
  startDate?: string;
  //** YYYY-MM-dd */
  endDate?: string;
  //** default is 1 */
  initialSize?: number;
};

export const useRefereeHistory = (params: Params): any[] => {
  const { size = 10, startDate, endDate, initialSize } = params;

  const response: any = usePrivateInfiniteQuery(
    generateKeyFun({
      path: "/v1/referral/referee_history",
      size,
      startDate,
      endDate,
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

  const total = useMemo(() => {
    return response.data?.[0]?.meta?.total || 0;
  }, [response.data?.[0]?.meta?.total]);

  const flattenOrders = useMemo(() => {
    if (!response.data) {
      return null;
    }

    return response.data?.map((item: any) => item.rows)?.flat();
  }, [response.data]);

  return [
    flattenOrders,
    {
      total,
      isLoading: response.isLoading,
      refresh: response.mutate,
      loadMore,
    },
  ];
};
