import React from "react";
import { useOrderBookAndTradesScript } from "./orderBookAndTrades.script";
import { OrderBookAndTrades } from "./orderBookAndTrades.ui";

export const OrderBookAndTradesWidget: React.FC<{ symbol: string }> = (
  props,
) => {
  const state = useOrderBookAndTradesScript(props.symbol);
  return <OrderBookAndTrades {...state} />;
};
