import React from "react";
import type { TradingviewUIPropsInterface } from "../type";

export const TradingviewChart: React.FC<
  React.PropsWithChildren<TradingviewUIPropsInterface>
> = ({ chartRef, direction = "ltr" }) => {
  return (
    <div
      data-testid="tradingview-chart"
      ref={chartRef}
      dir={direction}
      className="oui-size-full oui-overflow-hidden"
    />
  );
};
