import { FC } from "react";
import {
  useWithdrawFormScript,
  WithdrawFormScriptOptions,
} from "./withdrawForm.script";
import { WithdrawForm } from "./withdrawForm.ui";

export type WithdrawFormWidgetProps = WithdrawFormScriptOptions;

export const WithdrawFormWidget: FC<WithdrawFormWidgetProps> = (props) => {
  const state = useWithdrawFormScript(props);
  return <WithdrawForm {...state} />;
};
