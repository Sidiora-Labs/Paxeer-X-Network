import { FC } from "react";
import { OrderCellState } from "../orderCell.script";
import { useBracketOrderPriceScript } from "./bracketOrderPrice.script";
import { BracketOrderPrice } from "./bracketOrderPrice.ui";

export const BracketOrderPriceWidget: FC<OrderCellState> = (props) => {
  const state = useBracketOrderPriceScript(props);
  return <BracketOrderPrice {...state} />;
};
