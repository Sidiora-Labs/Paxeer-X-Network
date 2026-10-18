import React from "react";
import { useLtvScript } from "./ltv.script";
import { LtvUI } from "./ltv.ui";

export const LtvWidget: React.FC<
  Readonly<{
    currentLtv: number;
    nextLTV: number;
    showDiff?: boolean;
  }>
> = (props) => {
  const state = useLtvScript();
  return <LtvUI {...props} {...state} />;
};
