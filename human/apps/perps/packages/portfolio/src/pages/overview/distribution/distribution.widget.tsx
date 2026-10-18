import React from "react";
import { useScreen } from "@paxeer-network/ui";
import { DistributionHistoryDesktop } from "./distribution.ui";
import { DistributionHistoryMobile } from "./distribution.ui.mobile";
import { useDistributionHistoryHook } from "./useDataSource.script";

export const DistributionHistoryWidget: React.FC = () => {
  const state = useDistributionHistoryHook();
  const { isMobile } = useScreen();
  if (isMobile) {
    return <DistributionHistoryMobile {...state} />;
  }
  return <DistributionHistoryDesktop {...state} />;
};
