import { FC, ReactNode } from "react";
import { SubAccountScript } from "./subAccount.script";
import { SubAccountUI } from "./subAccount.ui";

type SubAccountWidgetProps = {
  customTrigger?: ReactNode;
};

export const SubAccountWidget: FC<SubAccountWidgetProps> = (props) => {
  const state = SubAccountScript();
  return <SubAccountUI {...state} customTrigger={props?.customTrigger} />;
};
