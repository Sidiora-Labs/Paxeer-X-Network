import { useMemo, useState } from "react";
import { LeaderboardTab } from "../../../type";
import { useTradingLeaderboardContext } from "../../provider";

export type CampaignLeaderboardScriptReturn = ReturnType<
  typeof useCampaignLeaderboardScript
>;

export function useCampaignLeaderboardScript() {
  const [activeTab, setActiveTab] = useState<LeaderboardTab>(
    LeaderboardTab.Volume,
  );
  const { currentCampaign } = useTradingLeaderboardContext();

  const excludeColumns = useMemo(() => {
    return (
      currentCampaign?.leaderboard_config?.exclude_leaderboard_columns || []
    );
  }, [currentCampaign]);

  return {
    activeTab,
    onTabChange: setActiveTab,
    excludeColumns,
  };
}
