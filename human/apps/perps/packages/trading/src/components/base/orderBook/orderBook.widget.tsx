import React from "react";
import { useOrderBookScript } from "./orderBook.script";
import { OrderBook } from "./orderBook.ui";

interface OrderBookWidgetProps {
  className?: string;
  symbol: string;
  height?: number;
}

export const OrderBookWidget: React.FC<OrderBookWidgetProps> = (props) => {
  const { className, symbol, height } = props;
  const state = useOrderBookScript({ symbol, height });
  return <OrderBook {...state} className={className} />;
};
