import { FC, useMemo } from "react";
import { cn, Box, useScreen } from "@paxeer-network/ui";
import { LeaderboardTab } from "../../../type";
import { CampaignRankingWidget } from "../../ranking/campaignRanking";
import { RankingColumnFields } from "../../ranking/shared/column";
import { LeaderboardTabs } from "../shared/LeaderboardTabs";
import { CampaignLeaderboardScriptReturn } from "./campaignLeaderboard.script";

export type CampaignLeaderboardProps = {
  style?: React.CSSProperties;
  className?: string;
  campaignId: number | string;
} & CampaignLeaderboardScriptReturn;

export const CampaignLeaderboard: FC<CampaignLeaderboardProps> = (props) => {
  const { isMobile } = useScreen();

  const sortKey = useMemo(() => {
    return props.activeTab === LeaderboardTab.Volume ? "volume" : "pnl";
  }, [props.activeTab]);

  const fields = useMemo<RankingColumnFields[]>(() => {
    const allFields: RankingColumnFields[] = [
      "rank",
      "address",
      sortKey,
      "rewards",
    ];
    return allFields;
  }, [isMobile, sortKey]);

  if (isMobile) {
    return (
      <Box
        pt={2}
        px={3}
        r="2xl"
        intensity={900}
        width="100%"
        className={cn(
          "oui-trading-leaderboard-campaign-leaderboard oui-relative",
        )}
      >
        <LeaderboardTabs
          isMobile={isMobile}
          activeTab={props.activeTab}
          onTabChange={props.onTabChange}
        />

        <CampaignRankingWidget
          campaignId={props.campaignId}
          fields={fields}
          sortKey={sortKey}
        />
      </Box>
    );
  }

  return (
    <Box
      pt={2}
      px={6}
      r="2xl"
      intensity={900}
      className={cn(
        "oui-trading-leaderboard-campaign-leaderboard oui-relative",
        "oui-mx-auto oui-max-w-[992px]",
      )}
    >
      <LeaderboardTabs
        isMobile={isMobile}
        activeTab={props.activeTab}
        onTabChange={props.onTabChange}
      />
      <CampaignRankingWidget
        campaignId={props.campaignId}
        fields={fields}
        sortKey={sortKey}
      />
    </Box>
  );
};
