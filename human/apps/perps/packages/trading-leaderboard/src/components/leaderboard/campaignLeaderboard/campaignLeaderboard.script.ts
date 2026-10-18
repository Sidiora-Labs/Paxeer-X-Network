import { useState } from "react";
import { LeaderboardTab } from "../../../type";

export type CampaignLeaderboardScriptReturn = ReturnType<
  typeof useCampaignLeaderboardScript
>;

export function useCampaignLeaderboardScript() {
  const [activeTab, setActiveTab] = useState<LeaderboardTab>(
    LeaderboardTab.Volume,
  );

  return {
    activeTab,
    onTabChange: setActiveTab,
  };
}
