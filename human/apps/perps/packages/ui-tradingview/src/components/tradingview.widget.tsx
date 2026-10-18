import { forwardRef } from "react";
import type { TradingviewWidgetPropsInterface } from "../type";
import { useTradingviewScript } from "./tradingview.script";
import { TradingviewUI } from "./tradingview.ui";

export const TradingviewWidget = forwardRef<
  HTMLDivElement,
  TradingviewWidgetPropsInterface
>((props, ref) => {
  const state = useTradingviewScript(props);
  return <TradingviewUI {...state} />;
});
