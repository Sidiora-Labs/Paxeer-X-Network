import { useEffect, useState } from "react";
import {
  ERROR_MSG_CODES,
  OrderValidationResult,
  useOrderEntry,
  useTpslPriceChecker,
} from "@paxeer-network/hooks";
import { PaxeerNetworkOrder, OrderType, PositionType } from "@paxeer-network/types";

type Props = {
  order: PaxeerNetworkOrder;
  setOrderValue: (key: string, value: any) => void;
  onSubmit: (formattedOrder: PaxeerNetworkOrder) => void;
  onClose: () => void;
  symbolLeverage?: number;
};

const isTPSLEnable = (order: PaxeerNetworkOrder, type: "tp" | "sl") => {
  if (order.tp_trigger_price || order.sl_trigger_price) {
    if (type === "tp") {
      return !!order.tp_trigger_price;
    }
    if (type === "sl") {
      return !!order.sl_trigger_price;
    }
  }
  // no edit, disabled tp /sl
  return false;
};

export const useTPSLAdvanced = (props: Props) => {
  const { order, setOrderValue, onClose } = props;
  const [innerMetaState, setInnerMetaState] =
    useState<OrderValidationResult | null>(null);
  const {
    formattedOrder,
    setValue,
    setValues,
    helper,
    symbolInfo,
    metaState,
    ...state
  } = useOrderEntry(order.symbol, {
    initialOrder: {
      symbol: order.symbol,
      order_type: order.order_type,
      side: order.side,
      order_price: order.order_price,
      order_quantity: order.order_quantity,
      position_type: order.position_type ?? PositionType.PARTIAL,
      trigger_price: order.trigger_price,
      // tp_enable: isTPSLEnable(order, "tp"),
      // sl_enable: isTPSLEnable(order, "sl"),
      tp_trigger_price: order.tp_trigger_price,
      sl_trigger_price: order.sl_trigger_price,
      tp_order_price: order.tp_order_price,
      sl_order_price: order.sl_order_price,
      tp_order_type: order.tp_order_type,
      sl_order_type: order.sl_order_type,
      sl_pnl: order.sl_pnl,
      sl_offset: order.sl_offset,
      sl_offset_percentage: order.sl_offset_percentage,
      sl_offset_from_mark: order.sl_offset_from_mark,
      sl_offset_percentage_from_mark: order.sl_offset_percentage_from_mark,
      tp_pnl: order.tp_pnl,
      tp_offset: order.tp_offset,
      tp_offset_percentage: order.tp_offset_percentage,
      tp_offset_from_mark: order.tp_offset_from_mark,
      tp_offset_percentage_from_mark: order.tp_offset_percentage_from_mark,
      margin_mode: order.margin_mode,
    },
  });

  const slPriceError = useTpslPriceChecker({
    slPrice: formattedOrder.sl_trigger_price,
    liqPrice: state.estLiqPrice,
    side: formattedOrder.side,
    markPrice: state.markPrice,
    currentPosition: state.currentPosition,
    orderQuantity: Number(formattedOrder.order_quantity),
  });

  const onSubmit = () => {
    const isSlPriceError =
      slPriceError?.sl_trigger_price?.type === ERROR_MSG_CODES.SL_PRICE_ERROR;
    helper
      .validate(isSlPriceError ? slPriceError : undefined)
      .then(() => {
        props.onSubmit(formattedOrder as PaxeerNetworkOrder);
      })
      .catch((err) => {
        console.log("metaState", metaState);
        console.log("err", err);
      });
  };

  return {
    order,
    formattedOrder,
    symbolInfo,
    slPriceError,
    estLiqPrice: state.estLiqPrice,
    estLiqPriceDistance: state.estLiqPriceDistance,
    setValue,
    setValues,
    onSubmit,
    onClose,
    metaState,
    symbolLeverage: props.symbolLeverage,
  };
};
