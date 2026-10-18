import { FC } from "react";
import { useLeftNavScript } from "./leftNav.script";
import { LeftNavUI, LeftNavUIProps } from "./leftNav.ui";

export const LeftNavWidget: FC<LeftNavUIProps> = (props) => {
  const state = useLeftNavScript();
  return <LeftNavUI {...props} {...state} />;
};
