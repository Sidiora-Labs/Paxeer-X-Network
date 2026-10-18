import React from "react";
import { useTradeDataScript } from "./tradeData.script";
import { TradeData } from "./tradeData.ui";

export const TradeDataWidget: React.FC<{ symbol: string }> = (props) => {
  const state = useTradeDataScript(props);
  return <TradeData {...state} />;
};
