import { useTradingFeeRebateScript } from "./tradingFeeRebate.script";
import { TradingFeeRebate } from "./tradingFeeRebate.ui";

export type TradingFeeRebateWidgetProps = {
  defaultRefereeKickbackRatio: number;
  maxRefereeKickbackRatio: number;
  directTradesRate: number;
  close?: () => void;
  onSuccess?: () => void | Promise<unknown>;
};

export const TradingFeeRebateWidget = (props: TradingFeeRebateWidgetProps) => {
  const state = useTradingFeeRebateScript(props);
  return <TradingFeeRebate {...props} {...state} />;
};
