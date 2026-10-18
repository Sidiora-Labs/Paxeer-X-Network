import { useCallback, useEffect, useState } from "react";
import { useOrderEntity } from "@paxeer-network/hooks";
import {
  API,
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";

export const useEditOrderEntry = (props: {
  order: API.AlgoOrderExt;
  orderType: OrderType;
  maxQty: number;
}) => {
  const { order, orderType, maxQty } = props;
  const orderPrice = order.price || undefined;
  const callbackRate = order.callback_rate
    ? order.callback_rate * 100
    : undefined;
  const [previousOrderValues, setPreviousOrderValues] = useState({
    price: orderPrice,
    quantity: order.quantity,
    triggerPrice: order.trigger_price,
    activatedPrice: order.activated_price,
    callbackValue: order.callback_value,
    callbackRate,
  });

  const [formattedOrder, setFormattedOrder] = useState({
    symbol: order.symbol,
    parent_algo_type: order.parent_algo_type,
    algo_type: order.algo_type,
    tpsl_execution_type: order.type,
    side: order.side as OrderSide,
    reduce_only: order.reduce_only,
    order_type: orderType,
    margin_mode: order.margin_mode,
    // TODO: trailing stop order edit price twice, order.price will be 0
    order_price: orderPrice,
    order_quantity: order.quantity,
    trigger_price: order.trigger_price,
    activated_price: order.activated_price,
    callback_value: order.callback_value,
    callback_rate: callbackRate,
  });

  const priceChanged = !Object.is(previousOrderValues.price, orderPrice);
  const quantityChanged = !Object.is(
    previousOrderValues.quantity,
    order.quantity,
  );
  const triggerPriceChanged = !Object.is(
    previousOrderValues.triggerPrice,
    order.trigger_price,
  );
  const activatedPriceChanged = !Object.is(
    previousOrderValues.activatedPrice,
    order.activated_price,
  );
  const callbackValueChanged = !Object.is(
    previousOrderValues.callbackValue,
    order.callback_value,
  );
  const callbackRateChanged = !Object.is(
    previousOrderValues.callbackRate,
    callbackRate,
  );

  if (
    priceChanged ||
    quantityChanged ||
    triggerPriceChanged ||
    activatedPriceChanged ||
    callbackValueChanged ||
    callbackRateChanged
  ) {
    setPreviousOrderValues({
      price: orderPrice,
      quantity: order.quantity,
      triggerPrice: order.trigger_price,
      activatedPrice: order.activated_price,
      callbackValue: order.callback_value,
      callbackRate,
    });
    setFormattedOrder((current) => ({
      ...current,
      order_price: priceChanged ? orderPrice : current.order_price,
      order_quantity: quantityChanged ? order.quantity : current.order_quantity,
      trigger_price: triggerPriceChanged
        ? order.trigger_price
        : current.trigger_price,
      activated_price: activatedPriceChanged
        ? order.activated_price
        : current.activated_price,
      callback_value: callbackValueChanged
        ? order.callback_value
        : current.callback_value,
      callback_rate: callbackRateChanged ? callbackRate : current.callback_rate,
    }));
  }

  const { markPrice, errors, validate, clearErrors } = useOrderEntity(
    formattedOrder,
    {
      maxQty,
    },
  );

  const setOrderValue = useCallback((key: keyof PaxeerNetworkOrder, value: any) => {
    setFormattedOrder((oldValue) => ({
      ...oldValue,
      [key]: value,
    }));
  }, []);

  const {
    order_price,
    order_quantity,
    trigger_price,
    activated_price,
    callback_value,
    callback_rate,
  } = formattedOrder;

  const isChanged =
    (order.price && order.price != order_price) ||
    (order.quantity && order.quantity != order_quantity) ||
    (order.trigger_price && order.trigger_price != trigger_price) ||
    // trailing stop fields
    (order.activated_price && order.activated_price != activated_price) ||
    (order.callback_value && order.callback_value != callback_value) ||
    (order.callback_rate && order.callback_rate * 100 != callback_rate);

  useEffect(() => {
    if (isChanged) {
      validate()
        .then((order) => {
          console.log("validate success", order);
        })
        .catch((err) => {
          console.log("validate error", err);
        });
    } else {
      clearErrors();
    }
  }, [
    isChanged,
    order_price,
    order_quantity,
    trigger_price,
    activated_price,
    callback_value,
    callback_rate,
  ]);

  return {
    markPrice,
    errors,
    validate,
    setOrderValue,
    formattedOrder,
    isChanged,
  };
};
