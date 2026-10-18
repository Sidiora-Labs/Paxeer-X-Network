import { FC } from "react";
import {
  useDepositFormScript,
  DepositFormScriptOptions,
} from "./depositForm.script";
import { DepositForm } from "./depositForm.ui";

export type DepositFormWidgetProps = DepositFormScriptOptions;

export const DepositFormWidget: FC<DepositFormWidgetProps> = (props) => {
  const state = useDepositFormScript(props);
  return <DepositForm {...state} />;
};
