import React from "react";
import { useStakeBoosterScript } from "./stakeBooster.script";
import { StakeBooster } from "./stakeBooster.ui";

export const StakeBoosterWidget: React.FC = () => {
  const state = useStakeBoosterScript();
  return <StakeBooster {...state} />;
};
