import { generateKeyFun } from "./swr";
import { useMemo } from "react";
import { RefferalAPI } from "./api";
import { usePrivateInfiniteQuery } from "../usePrivateInfiniteQuery";

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

export const useDistribution = (params: Params): any => {
  const { size = 10, startDate, endDate, initialSize } = params;

  const ordersResponse: any = usePrivateInfiniteQuery(
    generateKeyFun({
      path: "/v1/client/distribution_history",
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
      formatter: (data: any) => data,
      revalidateOnFocus: false,
    }
  );

  const loadMore = () => {
    ordersResponse.setSize(ordersResponse.size + 1);
  };

  const total = useMemo(() => {
    return ordersResponse.data?.[0]?.meta?.total || 0;
  }, [ordersResponse.data?.[0]?.meta?.total]);

  const flattenOrders = useMemo((): RefferalAPI.Distribution[] | null => {
    if (!ordersResponse.data) {
      return null;
    }

    return (
      ordersResponse.data
        ?.map((item: any) => item.rows)
        ?.flat()
        /// TODO: next version will be remove this code
        .map((item: any) => {
          return {
            ...item,
            created_time: item.created_time - 86400000,
            updated_time: item.updated_time - 86400000,
          };
        })
    );
  }, [ordersResponse.data]);

  return [
    flattenOrders,
    {
      total,
      isLoading: ordersResponse.isLoading,
      refresh: ordersResponse.mutate,
      loadMore,
    },
  ] as const;
};
