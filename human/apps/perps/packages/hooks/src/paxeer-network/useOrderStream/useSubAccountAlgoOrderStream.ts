import { useCallback, useEffect, useMemo, useState } from "react";
import { useDebouncedCallback } from "use-debounce";
import {
  OrderSide,
  OrderEntity,
  OrderStatus,
  API,
  AlgoOrderRootType,
} from "@paxeer-network/types";
import { SDKError } from "@paxeer-network/types";
import {
  sanitizeTPSLChildUpdates,
  TPSLChildUpdate,
} from "../../services/orderCreator/tpslOrderUpdates";
import { useSubAccountMutation, useSubAccountQuery } from "../../subAccount";
import { useEventEmitter } from "../../useEventEmitter";
import version from "../../version";
import { useMarkPricesStream } from "../useMarkPricesStream";

type CreateOrderType = "normalOrder" | "algoOrder";

type CombineOrderType = AlgoOrderRootType | "ALL";

/**
 * TODO: let useOrderStream support pass accountId, it will be better to use this hook
 */
export const useSubAccountAlgoOrderStream = (
  /**
   * Orders query params
   */
  params: {
    symbol?: string;
    status?: OrderStatus;
    page?: number;
    size?: number;
    side?: OrderSide;
    /**
     * Include the order type
     * @default ["ALL"]
     */
    includes?: CombineOrderType[];
    /**
     * Exclude the order type
     * @default []
     */
    excludes?: CombineOrderType[];
    dateRange?: {
      from?: Date;
      to?: Date;
    };
  },
  options: { accountId: string },
) => {
  const { status } = params;
  const accountId = options.accountId;
  const ee = useEventEmitter();

  const [includes, setIncludes] = useState<CombineOrderType[]>(
    params.includes ?? ["ALL"],
  );
  const [excludes, setExcludes] = useState<CombineOrderType[]>(
    params.excludes ?? [],
  );

  const { data: markPrices } = useMarkPricesStream();

  const [doCancelOrder, { isMutating: cancelMutating }] = useSubAccountMutation(
    "/v1/order",
    "DELETE",
    { accountId },
  );

  const [doCancelAllOrders] = useSubAccountMutation("/v1/orders", "DELETE", {
    accountId,
  });

  const [doUpdateOrder, { isMutating: updateMutating }] = useSubAccountMutation(
    "/v1/order",
    "PUT",
    { accountId },
  );

  const [doCancelAlgolOrder, { isMutating: cancelAlgoMutating }] =
    useSubAccountMutation("/v1/algo/order", "DELETE", { accountId });

  const [doCancelAllAlgoOrders] = useSubAccountMutation(
    "/v1/algo/orders",
    "DELETE",
    {
      accountId,
    },
  );

  const [doUpdateAlgoOrder, { isMutating: updateAlgoMutating }] =
    useSubAccountMutation("/v1/algo/order", "PUT", { accountId });

  const algoOrderKey = useMemo(() => {
    const { status, symbol, side, size = 50, page = 1, dateRange } = params;

    const search = new URLSearchParams([
      ["size", size.toString()],
      ["page", `${page}`],
    ]);

    if (dateRange) {
      if (dateRange.from) {
        search.set("start_t", `${dateRange.from.getTime()}`);
      }

      if (dateRange.to) {
        search.set("end_t", `${dateRange.to.getTime()}`);
      }
    }

    if (page) {
      search.set("page", `${page}`);
    }

    if (status) {
      search.set(`status`, status);
    }

    if (symbol) {
      search.set(`symbol`, symbol);
    }

    if (side) {
      search.set(`side`, side);
    }

    return `/v1/algo/orders?${search.toString()}`;
  }, [params]);

  const { data, mutate, isLoading } = useSubAccountQuery(
    accountId ? algoOrderKey : null,
    {
      accountId: options.accountId,
      formatter: (data) => data,
      revalidateOnFocus: false,
    },
  );

  const flattenOrders = useMemo(() => {
    const orders = data?.rows || [];

    if (includes.includes("ALL") && excludes.length === 0) {
      return orders;
    }

    if (includes.includes("ALL") && excludes.length > 0) {
      return orders?.filter((item: any) => !excludes.includes(item.algo_type));
    }

    if (includes.length > 0 && excludes.length === 0) {
      return orders?.filter((item: any) => includes.includes(item.algo_type));
    }

    if (includes.length > 0 && excludes.length > 0) {
      return orders?.filter(
        (item: any) =>
          includes.includes(item.algo_type) &&
          !excludes.includes(item.algo_type),
      );
    }

    return orders;
  }, [data, includes, excludes]);

  const orders = useMemo(() => {
    if (!flattenOrders) {
      return null;
    }

    if (status !== OrderStatus.NEW && status !== OrderStatus.INCOMPLETE) {
      return flattenOrders;
    }
    return flattenOrders.map((item: any) => {
      const order = {
        ...item,
        mark_price: (markPrices ?? ({} as any))[item.symbol] ?? 0,
      };

      if (
        order.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL ||
        order.algo_type === AlgoOrderRootType.TP_SL
      ) {
        const childQuantity = order.child_orders?.[0]?.quantity;
        if (childQuantity !== undefined) {
          order.quantity = childQuantity;
        }
      }
      return order;
    });
  }, [flattenOrders, markPrices, status]);

  const cancelAlgoOrdersByTypes = (
    types: AlgoOrderRootType[],
    symbol?: string,
  ) => {
    if (!types) {
      throw new SDKError("Types is required");
    }

    if (!Array.isArray(types)) {
      throw new SDKError("Types should be an array");
    }

    return Promise.all(
      types.map((type) => {
        return doCancelAllAlgoOrders(null, {
          algo_type: type,
          ...(symbol && { symbol }),
        });
      }),
    );
  };

  /**
   * cancel all orders
   */
  const cancelAllOrders = useCallback(() => {
    return Promise.all([
      doCancelAllAlgoOrders(null, { algo_type: AlgoOrderRootType.STOP }),
      doCancelAllAlgoOrders(null, {
        algo_type: AlgoOrderRootType.TRAILING_STOP,
      }),
    ]);
  }, [data]);

  const cancelAllPendingOrders = useCallback((symbol?: string) => {
    doCancelAllOrders(null, { ...(symbol && { symbol }) });
  }, []);

  const cancelPostionOrdersByTypes = useCallback(
    (symbol: string, types: AlgoOrderRootType[]) => {
      return doCancelAllAlgoOrders(null, {
        symbol,
        algo_type: types,
      });
    },
    [data],
  );

  const cancelAllTPSLOrders = useCallback(
    (symbol?: string) => {
      return cancelAlgoOrdersByTypes(
        [AlgoOrderRootType.POSITIONAL_TP_SL, AlgoOrderRootType.TP_SL],
        symbol,
      );
    },
    [data],
  );

  const _updateOrder = useCallback(
    (orderId: string, order: OrderEntity, type: CreateOrderType) => {
      switch (type) {
        case "algoOrder":
          return doUpdateAlgoOrder({
            order_id: orderId,
            price: order.order_price,
            quantity: order.order_quantity,
            trigger_price: order.trigger_price,

            // trailing stop order fields
            activated_price: order.activated_price,
            callback_value: order.callback_value,
            callback_rate: order.callback_rate,
            // include margin_mode if present (align with useOrderStream)
            ...(order.margin_mode && { margin_mode: order.margin_mode }),
          });
        default:
          return doUpdateOrder({ ...order, order_id: orderId });
      }
    },
    [],
  );

  /**
   * update order
   */
  const updateOrder = useCallback((orderId: string, order: OrderEntity) => {
    return _updateOrder(orderId, order, "normalOrder");
  }, []);

  /**
   * update algo order
   */
  const updateAlgoOrder = useCallback((orderId: string, order: OrderEntity) => {
    return _updateOrder(orderId, order, "algoOrder");
  }, []);

  const _cancelOrder = useCallback(
    (orderId: number, type: CreateOrderType, symbol?: string) => {
      switch (type) {
        case "algoOrder":
          return doCancelAlgolOrder(null, {
            // @ts-ignore
            order_id: orderId,
            symbol,
            source: `SDK${version}`,
          }).then((res: any) => {
            if (res.success) {
              mutate();
              return res;
            } else {
              throw new Error(res.message);
            }
          });
        default:
          return doCancelOrder(null, {
            order_id: orderId,
            symbol,
            source: `SDK_${version}`,
          }).then((res: any) => {
            if (res.success) {
              return res;
            } else {
              throw new Error(res.message);
            }
          });
      }
    },
    [],
  );

  /**
   * calcel order
   */
  const cancelOrder = useCallback((orderId: number, symbol?: string) => {
    return _cancelOrder(orderId, "normalOrder", symbol);
  }, []);

  /**
   * calcel algo order
   */
  const cancelAlgoOrder = useCallback((orderId: number, symbol?: string) => {
    return _cancelOrder(orderId, "algoOrder", symbol);
  }, []);

  const cancelTPSLChildOrder = useCallback(
    (orderId: number, rootAlgoOrderId: number): Promise<any> => {
      return doUpdateAlgoOrder({
        order_id: rootAlgoOrderId,
        child_orders: [
          {
            order_id: orderId,
            is_activated: false,
          },
        ],
      });
    },
    [],
  );

  const updateTPSLOrder = useCallback(
    (
      /**
       * the root algo order id
       */
      orderId: number,
      childOrders: TPSLChildUpdate[],
    ) => {
      if (!Array.isArray(childOrders)) {
        throw new SDKError("Children orders is required");
      }
      const original = flattenOrders?.find(
        (order: API.AlgoOrder) =>
          Number(order.algo_order_id) === Number(orderId),
      );
      const changes = sanitizeTPSLChildUpdates(childOrders, original);
      if (!changes.length) return Promise.resolve();
      return doUpdateAlgoOrder({
        order_id: orderId,
        child_orders: changes,
      });
    },
    [flattenOrders, doUpdateAlgoOrder],
  );

  const refresh = useDebouncedCallback(() => {
    mutate();
  }, 200);

  useEffect(() => {
    const handler = (position: API.PositionExt) => {
      if (position.account_id === accountId) {
        refresh();
      }
    };

    ee.on("tpsl:updateOrder", handler);

    return () => {
      ee.off("tpsl:updateOrder", handler);
    };
  }, [ee, accountId]);

  return [
    orders,
    {
      isLoading,
      refresh,
      cancelAllOrders,
      cancelAllPendingOrders,
      cancelAllTPSLOrders,
      cancelAlgoOrdersByTypes,
      updateOrder,
      cancelOrder,
      updateAlgoOrder,
      cancelAlgoOrder,
      cancelTPSLChildOrder,
      updateTPSLOrder,
      cancelPostionOrdersByTypes,
      submitting: {
        cancelOrder: cancelMutating,
        updateOrder: updateMutating,
        cancelAlgoOrder: cancelAlgoMutating,
        updateAlglOrder: updateAlgoMutating,
      },
    },
  ] as const;
};
