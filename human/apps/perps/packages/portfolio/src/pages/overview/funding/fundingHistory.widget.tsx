import React from "react";
import { useScreen } from "@paxeer-network/ui";
import { FundingHistoryDesktop } from "./fundingHistory.ui";
import { FundingHistoryMobile } from "./fundingHistory.ui.mobile";
import { useFundingHistoryHook } from "./useDataSource.script";

export const FundingHistoryWidget: React.FC = () => {
  const state = useFundingHistoryHook();
  const { isMobile } = useScreen();
  if (isMobile) {
    return <FundingHistoryMobile {...state} />;
  }
  return <FundingHistoryDesktop {...state} />;
};
