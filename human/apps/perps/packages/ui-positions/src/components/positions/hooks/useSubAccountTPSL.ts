import { useEffect, useMemo } from "react";
import { useDebouncedCallback, useEventEmitter } from "@paxeer-network/hooks";
import { AlgoOrderRootType, API, OrderStatus } from "@paxeer-network/types";
import { useSubAccountQuery } from "./useSubAccountQuery";

export function useSubAccountTPSL(subAccountIds: string[]) {
  const ee = useEventEmitter();

  // Get algo orders (TP/SL orders) for all sub-accounts
  const { data: algoOrdersResponse, mutate: mutateTPSLOrders } =
    useSubAccountQuery(
      `/v1/algo/orders?size=100&page=1&status=${OrderStatus.INCOMPLETE}`,
      {
        accountId: subAccountIds,
        formatter: (data) => data,
        revalidateOnFocus: false,
      },
    );

  // Flatten and filter algo orders to get only TP/SL orders
  const tpslOrders = useMemo(() => {
    if (!Array.isArray(algoOrdersResponse)) {
      return [];
    }
    const algoOrders = algoOrdersResponse
      ?.map((item, index) =>
        item.rows.map((order: any) => ({
          ...order,
          account_id: subAccountIds[index],
        })),
      )
      ?.flat();

    return algoOrders?.filter((order) =>
      [AlgoOrderRootType.POSITIONAL_TP_SL, AlgoOrderRootType.TP_SL].includes(
        order.algo_type as AlgoOrderRootType,
      ),
    ) as (API.AlgoOrder & { account_id: string })[];
  }, [algoOrdersResponse, subAccountIds]);

  const refresh = useDebouncedCallback(() => {
    mutateTPSLOrders();
  }, 200);

  useEffect(() => {
    const handler = (position: API.PositionExt) => {
      if (position.account_id) {
        refresh();
      }
    };

    ee.on("tpsl:updateOrder", handler);

    return () => {
      ee.off("tpsl:updateOrder", handler);
    };
  }, [ee]);

  return { tpslOrders, mutateTPSLOrders };
}
