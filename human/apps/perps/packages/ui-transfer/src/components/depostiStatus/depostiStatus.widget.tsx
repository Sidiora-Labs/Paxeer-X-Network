import React from "react";
import { useDepositStatusScript } from "./depostiStatus.script";
import { DepositStatus, DepositStatusProps } from "./depostiStatus.ui";

export type DepositStatusWidgetProps = Pick<
  DepositStatusProps,
  "className" | "onClick"
>;

export const DepositStatusWidget: React.FC<DepositStatusWidgetProps> = (
  props,
) => {
  const state = useDepositStatusScript();
  return <DepositStatus {...state} {...props} />;
};
