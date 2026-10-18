import React from "react";
import { usePositionHeaderScript } from "./positionHeader.script";
import { PositionHeader } from "./positionHeader.ui";

export const PositionHeaderWidget: React.FC<{
  pnlNotionalDecimalPrecision?: number;
  setPnlNotionalDecimalPrecision: (value: number) => void;
  unPnlPriceBasis: "markPrice" | "lastPrice";
  setUnPnlPriceBasic: (value: string) => void;
  symbol?: string;
}> = (props) => {
  const state = usePositionHeaderScript(props);
  return <PositionHeader {...state} />;
};
