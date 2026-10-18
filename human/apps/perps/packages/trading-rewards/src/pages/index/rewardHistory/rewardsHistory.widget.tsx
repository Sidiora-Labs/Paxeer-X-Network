import React from "react";
import { useRewardsHistoryScript } from "./rewardsHistory.script";
import { RewardHistory } from "./rewardsHistory.ui";

export const RewardsHistoryWidget: React.FC = () => {
  const state = useRewardsHistoryScript();
  return <RewardHistory {...state} />;
};
