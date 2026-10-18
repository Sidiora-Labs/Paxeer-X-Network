import React from "react";
import { useLiquidationScript } from "./liquidation.script";
import { Liquidation, MobileLiquidation } from "./liquidation.ui";

export type LiquidationProps = {
  symbol?: string;
  enableLoadMore?: boolean;
};

export const LiquidationWidget: React.FC<LiquidationProps> = (props) => {
  const state = useLiquidationScript(props);
  return <Liquidation {...state} />;
};

export const MobileLiquidationWidget: React.FC<
  LiquidationProps & {
    classNames?: { root?: string; content?: string; cell?: string };
  }
> = (props) => {
  const { classNames, ...rest } = props;
  const state = useLiquidationScript(rest);
  return <MobileLiquidation classNames={classNames} {...state} />;
};
