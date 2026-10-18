import React from "react";
import { TradingViewConfigInterface } from "../../../types/types";
import { useTradingviewScript } from "./tradingview.script";
import { TradingviewUI } from "./tradingview.ui";

export interface TradingviewWidgetProps {
  symbol: string;
  tradingViewConfig: TradingViewConfigInterface;
}

export const TradingviewWidget: React.FC<TradingviewWidgetProps> = (props) => {
  const state = useTradingviewScript(props);
  return <TradingviewUI {...state} />;
};
