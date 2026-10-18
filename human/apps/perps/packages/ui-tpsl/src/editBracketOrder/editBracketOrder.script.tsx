import { useEffect, useMemo } from "react";
import {
  ERROR_MSG_CODES,
  createTPSLOrderUpdates,
  useMaxQty,
  useMutation,
  useOrderEntry,
  useSymbolsInfo,
  useTpslPriceChecker,
} from "@paxeer-network/hooks";
import {
  AlgoOrderRootType,
  AlgoOrderType,
  API,
  MarginMode,
  PaxeerNetworkOrder,
  OrderSide,
  PositionType,
  SDKError,
} from "@paxeer-network/types";
import { Decimal, resolveTPSLOrderType } from "@paxeer-network/utils";
import { isTPSLOrderTypeLocked } from "../positionTPSL/tpslOrderSync";
import {
  getBracketTPSLPriceInfo,
  hasBracketTPSLPriceChanged,
} from "./editBracketOrder.helpers";

function getInitialOrder(order: API.AlgoOrderExt) {
  const childOrder = order.child_orders[0];
  const positionType =
    childOrder.algo_type === AlgoOrderRootType.TP_SL
      ? PositionType.PARTIAL
      : PositionType.FULL;
  const tpOrder = childOrder.child_orders.find(
    (item) => item.algo_type === AlgoOrderType.TAKE_PROFIT,
  );
  const slOrder = childOrder.child_orders.find(
    (item) => item.algo_type === AlgoOrderType.STOP_LOSS,
  );
  const tpslPriceInfo = getBracketTPSLPriceInfo(childOrder);

  return {
    baseInfo: {
      symbol: order.symbol,
      margin_mode: order.margin_mode ?? MarginMode.CROSS,
      order_type: order.type,
      side: order.side,
      order_price: order.price,
      order_quantity: order.quantity,
      position_type: positionType,
      // tp_enable: !!tpOrder?.trigger_price,
      // sl_enable: !!slOrder?.trigger_price,
    },
    tpslPriceInfo,
    disableTPOrderTypeSelector: isTPSLOrderTypeLocked(childOrder, "tp"),
    disableSLOrderTypeSelector: isTPSLOrderTypeLocked(childOrder, "sl"),
    tpInfo: {
      orderId: tpOrder?.algo_order_id,
    },
    slInfo: {
      orderId: slOrder?.algo_order_id,
    },
  };
}

export const useEditBracketOrder = (props: { order: API.AlgoOrderExt }) => {
  if (!props.order) {
    throw new SDKError("order is required for editBracketOrder");
  }
  const {
    baseInfo,
    tpslPriceInfo,
    disableTPOrderTypeSelector,
    disableSLOrderTypeSelector,
  } = getInitialOrder(props.order);

  const [doUpdateOrder, { isMutating }] = useMutation("/v1/algo/order", "PUT");

  const maxQty = useEditOrderMaxQty(props.order, props.order.quantity);

  const {
    formattedOrder,
    setValue,
    setValues,
    estLiqPrice,
    metaState,
    symbolInfo,
    helper,
    ...state
  } = useOrderEntry(props.order.symbol, {
    initialOrder: baseInfo,
    maxQty,
  });
  const symbol = props.order.symbol;

  const isPriceChanged = useMemo(() => {
    return hasBracketTPSLPriceChanged(tpslPriceInfo, formattedOrder);
  }, [
    tpslPriceInfo,
    formattedOrder.tp_order_price,
    formattedOrder.sl_order_price,
    formattedOrder.tp_trigger_price,
    formattedOrder.sl_trigger_price,
  ]);
  useEffect(() => {
    setValues({
      ...tpslPriceInfo,
    });
  }, [props.order, setValues]);

  const slPriceError = useTpslPriceChecker({
    slPrice: formattedOrder.sl_trigger_price,
    liqPrice: estLiqPrice,
    side: formattedOrder.side,
  });

  const isSlPriceError =
    slPriceError?.sl_trigger_price?.type === ERROR_MSG_CODES.SL_PRICE_ERROR;

  const onSubmit = async () => {
    return helper
      .validate(isSlPriceError ? slPriceError : undefined)
      .then(() => {
        const parent = props.order.child_orders[0];
        const legs = (["tp", "sl"] as const).map((leg) => ({
          algo_type:
            leg === "tp" ? AlgoOrderType.TAKE_PROFIT : AlgoOrderType.STOP_LOSS,
          type: resolveTPSLOrderType(
            formattedOrder[`${leg}_order_type`],
            formattedOrder[`${leg}_order_price`],
            baseInfo.position_type === PositionType.FULL,
          ),
          is_activated: !!formattedOrder[`${leg}_trigger_price`],
          trigger_price: formattedOrder[`${leg}_trigger_price`]
            ? new Decimal(formattedOrder[`${leg}_trigger_price`]!)
                .todp(symbolInfo.quote_dp)
                .toNumber()
            : undefined,
          price: formattedOrder[`${leg}_order_price`]
            ? new Decimal(formattedOrder[`${leg}_order_price`]!)
                .todp(symbolInfo.quote_dp)
                .toNumber()
            : undefined,
        }));
        const childOrders = createTPSLOrderUpdates(legs, parent);
        if (!childOrders.length) return;
        return doUpdateOrder({
          order_id: props.order.algo_order_id,
          child_orders: [
            {
              order_id: props.order.child_orders[0].algo_order_id,
              child_orders: childOrders,
            },
          ],
        });
      });
  };

  return {
    symbol,
    symbolInfo,
    slPriceError,
    estLiqPrice,
    side: formattedOrder.side,
    formattedOrder,
    setValue,
    setValues,
    metaState,
    onSubmit,
    isMutating,
    isPriceChanged,
    disableTPOrderTypeSelector,
    disableSLOrderTypeSelector,
  };
};

export function useEditOrderMaxQty(
  order: API.AlgoOrderExt,
  positionQty?: number,
) {
  const { reduce_only } = order;

  const maxQty = useMaxQty(order.symbol, order.side as OrderSide, {
    reduceOnly: reduce_only,
    marginMode: order.margin_mode ?? MarginMode.CROSS,
  });

  return useMemo(() => {
    if (reduce_only) {
      return Math.abs(positionQty ?? 0);
    }
    return order.quantity + Math.abs(maxQty);
  }, [order.quantity, maxQty, reduce_only, positionQty]);
}
