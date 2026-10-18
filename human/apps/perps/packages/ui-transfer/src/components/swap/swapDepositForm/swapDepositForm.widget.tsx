import { FC } from "react";
import {
  useSwapDepositFormScript,
  UseSwapDepositFormScriptOptions,
} from "./swapDepositForm.script";
import { SwapDepositForm } from "./swapDepositForm.ui";

export type SwapDepositFormWidgetProps = UseSwapDepositFormScriptOptions;

/** @deprecated unused, will be removed in the future */
export const SwapDepositFormWidget: FC<SwapDepositFormWidgetProps> = (
  props,
) => {
  const state = useSwapDepositFormScript(props);
  return <SwapDepositForm {...state} />;
};
