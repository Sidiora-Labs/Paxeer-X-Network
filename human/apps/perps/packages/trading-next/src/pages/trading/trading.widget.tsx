import React from "react";
import { useTradingScript } from "./trading.script";
import { Trading } from "./trading.ui";

export const TradingWidget: React.FC = () => {
  const state = useTradingScript();
  return <Trading {...state} />;
};
