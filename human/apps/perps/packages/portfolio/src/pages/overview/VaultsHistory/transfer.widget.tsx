import React from "react";
import { useScreen } from "@paxeer-network/ui";
import { VaultsHistoryUI } from "./transfer.ui";
import { useVaultsHistoryHook } from "./useDataSource.script";
import { VaultsHistoryMobile } from "./vaults.ui.mobile";

export const VaultsHistoryWidget: React.FC = () => {
  const state = useVaultsHistoryHook();
  const { isMobile } = useScreen();
  if (isMobile) {
    return <VaultsHistoryMobile {...state} />;
  }
  return <VaultsHistoryUI {...state} />;
};
