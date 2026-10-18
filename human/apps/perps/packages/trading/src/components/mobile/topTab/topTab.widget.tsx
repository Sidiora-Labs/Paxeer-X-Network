import React from "react";
import { useTopTabScript } from "./topTab.script";
import { TopTab } from "./topTab.ui";

export const TopTabWidget: React.FC<{ className?: string }> = (props) => {
  const state = useTopTabScript();
  return <TopTab className={props.className} {...state} />;
};
