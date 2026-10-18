import { useCallback, useEffect, useMemo, useState } from "react";
import {
  OrderSide,
  OrderEntity,
  OrderStatus,
  API,
  AlgoOrderRootType,
  OrderType,
} from "@paxeer-network/types";
import { SDKError } from "@paxeer-network/types";
import { withTPSLProvenance, isPositionalTPSL } from "@paxeer-network/utils";
import { useDataCenterContext } from "../../provider/dataCenter/dataCenterContext";
import {
  normalizeTPSLChildType,
  sanitizeTPSLChildUpdates,
  TPSLChildUpdate,
} from "../../services/orderCreator/tpslOrderUpdates";
import { validateTPSLChild } from "../../services/orderCreator/validateTPSLChild";
import { useMutation } from "../../useMutation";
import { usePrivateInfiniteQuery } from "../../usePrivateInfiniteQuery";
import { generateKeyFun } from "../../utils/swr";
import version from "../../version";
import { useMarkPricesStream } from "../useMarkPricesStream";
import { useSymbolsInfo } from "../useSymbolsInfo";

type CreateOrderType = "normalOrder" | "algoOrder";

type CombineOrderType = AlgoOrderRootType | "ALL";

export const useOrderStream = (
  /**
   * Orders query params
   */
  params: {
    symbol?: string;
    status?: OrderStatus;
    page?: number;
    size?: number;
    side?: OrderSide;
    sourceTypeAll?: boolean;
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
  options?: {
    /**
     * Keep the state update alive
     */
    keeplive?: boolean;
    /**
     * Stop the state update when the component unmount
     */
    stopOnUnmount?: boolean;
  },
) => {
  const {
    status,
    symbol,
    side,
    size = 50,
    page,
    dateRange,
    sourceTypeAll,
  } = params;

  const [includes, setIncludes] = useState<CombineOrderType[]>(
    params.includes ?? ["ALL"],
  );
  const [excludes, setExcludes] = useState<CombineOrderType[]>(
    params.excludes ?? [],
  );

  const { data: markPrices } = useMarkPricesStream();
  const symbolsInfo = useSymbolsInfo();

  const { registerKeyHandler, unregisterKeyHandler } = useDataCenterContext();
  const [
    doCancelOrder,
    { error: cancelOrderError, isMutating: cancelMutating },
  ] = useMutation("/v1/order", "DELETE");

  const [doCancelAllOrders] = useMutation("/v1/orders", "DELETE");

  const [
    doUpdateOrder,
    { error: updateOrderError, isMutating: updateMutating },
  ] = useMutation("/v1/order", "PUT");

  const [
    doCancelAlgolOrder,
    { error: cancelAlgoOrderError, isMutating: cancelAlgoMutating },
  ] = useMutation("/v1/algo/order", "DELETE");

  const [doCancelAllAlgoOrders] = useMutation("/v1/algo/orders", "DELETE");

  const [
    doUpdateAlgoOrder,
    { error: updateAlgoOrderError, isMutating: updateAlgoMutating },
  ] = useMutation("/v1/algo/order", "PUT");

  const normalOrderKeyFn = useMemo(() => {
    return generateKeyFun("/v1/orders", {
      status,
      symbol,
      side,
      size,
      page,
      dateRange,
      sourceTypeAll,
    });
  }, [status, symbol, side, size, page, dateRange]);

  const algoOrderKeyFn = useMemo(() => {
    return sourceTypeAll
      ? null
      : generateKeyFun("/v1/algo/orders", {
          status,
          symbol,
          side,
          size: 100,
          page,
          dateRange,
        });
  }, [status, symbol, side, dateRange, size]);

  useEffect(() => {
    const formatKey = (value?: string) => (value ? `:${value}` : "");
    const key = `orders${formatKey(status)}${formatKey(symbol)}${formatKey(
      side,
    )}${formatKey(size.toString())}`;

    registerKeyHandler?.(key, normalOrderKeyFn);

    if (algoOrderKeyFn) {
      registerKeyHandler?.(key.replace("orders", "algoOrders"), algoOrderKeyFn);
    }

    return () => {
      if (!options?.stopOnUnmount) return;

      unregisterKeyHandler(key);

      if (algoOrderKeyFn) {
        unregisterKeyHandler(key.replace("orders", "algoOrders"));
      }
    };
  }, [normalOrderKeyFn, options?.keeplive]);

  const normalOrdersResponse = usePrivateInfiniteQuery<API.OrderResponse>(
    normalOrderKeyFn,
    {
      initialSize: 1,
      formatter: (data) => data,
      revalidateOnFocus: false,
    },
  );

  // console.log("ordersResponse", ordersResponse);

  const algoOrdersResponse = usePrivateInfiniteQuery<API.OrderResponse>(
    algoOrderKeyFn,
    {
      formatter: (data) => data,
      revalidateOnFocus: false,
    },
  );

  // console.log("algoOrdersResponse", algoOrdersResponse, algoOrdersResponse);

  const flattenOrders = useMemo(() => {
    if (
      !normalOrdersResponse.data ||
      (!algoOrdersResponse.data && !sourceTypeAll)
    ) {
      return null;
    }

    let orders = normalOrdersResponse.data
      ?.map((item: any) => item.rows)
      ?.flat();

    if (algoOrdersResponse.data) {
      const algoOrders = algoOrdersResponse.data
        ?.map((item: any) => item.rows)
        ?.flat();

      orders = [...orders, ...algoOrders];
    }

    orders = orders?.map((order) => withTPSLProvenance(order));

    if (includes.includes("ALL") && excludes.length === 0) {
      return orders;
    }

    if (includes.includes("ALL") && excludes.length > 0) {
      return orders?.filter((item) => !excludes.includes(item.algo_type));
    }

    if (includes.length > 0 && excludes.length === 0) {
      return orders?.filter((item) => includes.includes(item.algo_type));
    }

    if (includes.length > 0 && excludes.length > 0) {
      return orders?.filter(
        (item) =>
          includes.includes(item.algo_type) &&
          !excludes.includes(item.algo_type),
      );
    }

    return orders;
  }, [normalOrdersResponse.data, algoOrdersResponse.data, includes, excludes]);

  // console.log(ordersResponse.data);

  const orders = useMemo(() => {
    if (!flattenOrders) {
      return null;
    }

    if (status !== OrderStatus.NEW && status !== OrderStatus.INCOMPLETE) {
      return flattenOrders;
    }
    return flattenOrders.map((item) => {
      const order = {
        ...item,
        mark_price: (markPrices ?? ({} as any))[item.symbol] ?? 0,
      };

      ///TODO: remove this when BE provides the correct data
      // console.log("------------->>>>>>>>", order);
      if (
        order.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL ||
        order.algo_type === AlgoOrderRootType.TP_SL
      ) {
        const childQuantity = order.child_orders?.[0]?.quantity;
        if (childQuantity !== undefined) {
          order.quantity = childQuantity;
        }
      }
      ///-----------------todo end----------------

      return order;
    });
  }, [flattenOrders, markPrices, status]);

  const total = useMemo(() => {
    return orders?.length || 0;
    // return ordersResponse.data?.[0]?.meta?.total || 0;
  }, [orders?.length]);

  // console.log("---->>>>>>!!!! orders", total, orders, {
  //   status,
  //   symbol,
  //   side,
  //   size,
  //   page,
  //   dateRange,
  //   sourceTypeAll,
  // });

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

    // TODO: order type check

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
      doCancelAllOrders(null),
      doCancelAllAlgoOrders(null, { algo_type: AlgoOrderRootType.STOP }),
      doCancelAllAlgoOrders(null, {
        algo_type: AlgoOrderRootType.TRAILING_STOP,
      }),
    ]);
  }, [normalOrdersResponse.data, algoOrdersResponse.data]);

  const cancelAllPendingOrders = useCallback(
    (symbol?: string) => {
      return Promise.all([
        doCancelAllOrders(null, { ...(symbol && { symbol }) }),
        doCancelAllAlgoOrders(null, {
          algo_type: AlgoOrderRootType.STOP,
          ...(symbol && { symbol }),
        }),
        doCancelAllAlgoOrders(null, {
          algo_type: AlgoOrderRootType.TRAILING_STOP,
          ...(symbol && { symbol }),
        }),
      ]);
    },
    [symbol],
  );

  const cancelPostionOrdersByTypes = useCallback(
    (symbol: string, types: AlgoOrderRootType[]) => {
      return doCancelAllAlgoOrders(null, {
        symbol,
        algo_type: types,
      });
    },
    [algoOrdersResponse.data],
  );

  const cancelAllTPSLOrders = useCallback(
    (symbol?: string) => {
      return cancelAlgoOrdersByTypes(
        [AlgoOrderRootType.POSITIONAL_TP_SL, AlgoOrderRootType.TP_SL],
        symbol,
      );
    },
    [algoOrdersResponse.data],
  );

  const _updateOrder = useCallback(
    (orderId: string, order: OrderEntity, type: CreateOrderType) => {
      const findOrder = (items: any[], id: string | number = orderId): any =>
        items?.find((item) => Number(item.algo_order_id) === Number(id)) ??
        items
          ?.map((item) => findOrder(item.child_orders ?? [], id))
          .find(Boolean);
      const original = findOrder(flattenOrders ?? []);
      switch (type) {
        case "algoOrder":
          if (
            original &&
            ["TAKE_PROFIT", "STOP_LOSS"].includes(original.algo_type)
          ) {
            const requestedType = order.order_type?.replace("STOP_", "");
            const updateType = normalizeTPSLChildType(
              requestedType as OrderType | undefined,
              isPositionalTPSL(original),
            );
            if (requestedType && !updateType) {
              return Promise.reject(
                new SDKError("A TP/SL order type must be LIMIT or MARKET"),
              );
            }
            const errors = validateTPSLChild(
              original,
              {
                type: updateType,
                price: order.order_price,
                trigger_price: order.trigger_price,
              },
              {
                symbol: symbolsInfo[original.symbol](),
                markPrice: markPrices?.[original.symbol],
              },
              findOrder(flattenOrders ?? [], original.parent_algo_order_id),
            );
            if (Object.keys(errors).length)
              return Promise.reject(
                new SDKError(
                  Object.values(errors)[0]?.message ?? "Invalid TP/SL price",
                ),
              );
            const change = {
              order_id: original.algo_order_id,
              ...(updateType && { type: updateType }),
              quantity: isPositionalTPSL(original)
                ? undefined
                : order.order_quantity,
              price: order.order_price,
              trigger_price: order.trigger_price,
            };
            const nested =
              original.parent_algo_order_id !== original.root_algo_order_id
                ? [
                    {
                      order_id: original.parent_algo_order_id,
                      child_orders: [change],
                    },
                  ]
                : [change];
            const root = flattenOrders?.find(
              (item) => item.algo_order_id === original.root_algo_order_id,
            );
            const child_orders = sanitizeTPSLChildUpdates(nested as any, root);
            if (!child_orders.length) return Promise.resolve();
            return doUpdateAlgoOrder({
              order_id: original.root_algo_order_id,
              child_orders,
            });
          }
          return doUpdateAlgoOrder({
            order_id: orderId,
            price: order.order_price,
            quantity:
              original && isPositionalTPSL(original)
                ? undefined
                : order.order_quantity,
            trigger_price: order.trigger_price,

            // trailing stop order fields
            activated_price: order.activated_price,
            callback_value: order.callback_value,
            callback_rate: order.callback_rate,
            // Include margin_mode if present
            ...(order.margin_mode && { margin_mode: order.margin_mode }),
          });
        default:
          return doUpdateOrder({ ...order, order_id: orderId });
      }
    },
    [flattenOrders, doUpdateAlgoOrder, doUpdateOrder, markPrices, symbolsInfo],
  );

  /**
   * update order
   */
  const updateOrder = useCallback(
    (orderId: string, order: OrderEntity) => {
      return _updateOrder(orderId, order, "normalOrder");
    },
    [_updateOrder],
  );

  /**
   * update algo order
   */
  const updateAlgoOrder = useCallback(
    (orderId: string, order: OrderEntity) => {
      return _updateOrder(orderId, order, "algoOrder");
    },
    [_updateOrder],
  );

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
              // ordersResponse.mutate();
              normalOrdersResponse.mutate();
              algoOrdersResponse.mutate();
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
              // return ordersResponse.mutate().then(() => {
              //   return res;
              // });
              //Optimistic Updates
              // ordersResponse.mutate();
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

  const loadMore = () => {
    // ordersResponse.setSize(ordersResponse.size + 1);
  };

  // const cancelTPSLOrder = useCallback((orderId:number, symbol:string)=>{
  //   return
  // });

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
    async (
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
        (order) => Number(order.algo_order_id) === Number(orderId),
      );
      const validateChildren = (changes: any[], parent: any) => {
        for (const change of changes) {
          const child = parent?.child_orders?.find(
            (item: API.AlgoOrder) =>
              Number(item.algo_order_id) === Number(change.order_id),
          );
          if (!child || change.is_activated === false) continue;
          if (change.child_orders) validateChildren(change.child_orders, child);
          else if (
            ["TAKE_PROFIT", "STOP_LOSS"].includes(child.algo_type) &&
            (change.type != null ||
              change.trigger_price != null ||
              change.price != null)
          ) {
            const errors = validateTPSLChild(
              child,
              {
                type: change.type,
                price: change.price,
                trigger_price: change.trigger_price,
              },
              {
                symbol: symbolsInfo[child.symbol ?? parent.symbol](),
                markPrice: markPrices?.[child.symbol ?? parent.symbol],
              },
              parent,
            );
            if (Object.keys(errors).length)
              throw new SDKError(
                Object.values(errors)[0]?.message ?? "Invalid TP/SL price",
              );
          }
        }
      };
      validateChildren(childOrders, original);
      const changes = sanitizeTPSLChildUpdates(childOrders, original);
      if (!changes.length) return Promise.resolve();
      return doUpdateAlgoOrder({
        order_id: orderId,
        child_orders: changes,
      });
    },
    [flattenOrders, doUpdateAlgoOrder, markPrices, symbolsInfo],
  );

  const meta = useMemo(() => {
    return normalOrdersResponse.data?.[0]?.meta;
  }, [normalOrdersResponse.data?.[0]]);

  const refresh = useCallback(() => {
    normalOrdersResponse.mutate();
    algoOrdersResponse.mutate();
  }, []);

  return [
    orders,
    {
      total,
      isLoading: normalOrdersResponse.isLoading || algoOrdersResponse.isLoading,
      refresh,
      loadMore,
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
      meta,
      errors: {
        cancelOrder: cancelOrderError,
        updateOrder: updateOrderError,
        cancelAlgoOrder: cancelAlgoOrderError,
        updateAlgoOrder: updateAlgoOrderError,
      },
      submitting: {
        cancelOrder: cancelMutating,
        updateOrder: updateMutating,
        cancelAlgoOrder: cancelAlgoMutating,
        updateAlglOrder: updateAlgoMutating,
      },
    },
  ] as const;
};
