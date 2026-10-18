import { FC } from "react";
import { useCampaignLeaderboardScript } from "./campaignLeaderboard.script";
import {
  CampaignLeaderboard,
  CampaignLeaderboardProps,
} from "./campaignLeaderboard.ui";

export type CampaignLeaderboardWidgetProps = Pick<
  CampaignLeaderboardProps,
  "style" | "className" | "campaignId"
>;

export const CampaignLeaderboardWidget: FC<CampaignLeaderboardWidgetProps> = (
  props,
) => {
  const state = useCampaignLeaderboardScript();

  return (
    <CampaignLeaderboard
      {...state}
      className={props.className}
      style={props.style}
      campaignId={props.campaignId}
    />
  );
};
