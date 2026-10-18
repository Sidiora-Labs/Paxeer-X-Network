import { API } from "@paxeer-network/types";
import { useLiquidationCellScript } from "./liquidationCell.script";
import { LiquidationCell } from "./liquidationCell.ui";

export const LiquidationCellWidget = (props: {
  item: API.Liquidation;
  onSymbolChange?: (symbol: API.Symbol) => void;
  index: number;
  classNames?: {
    root?: string;
  };
}) => {
  const { classNames, ...rest } = props;
  const state = useLiquidationCellScript(rest);
  return <LiquidationCell classNames={classNames} {...state} />;
};
