import { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { useScreen } from "@paxeer-network/ui";
import { LeaderboardTitle } from "../../pages/leaderboard/page";
import { useCampaignsScript } from "../campaigns/campaigns.script";
import { RewardsDesktopUI } from "./rewards.desktop.ui";

export const RewardsWidget = () => {
  const state = useCampaignsScript();
  const { t } = useTranslation();
  const { isMobile } = useScreen();

  const hideConfig = useMemo(() => {
    return {
      estimatedRewards: state.currentCampaign?.hide_estimated_rewards,
    };
  }, [state.currentCampaign]);

  if (
    state.currentCampaignId === "general" ||
    state.currentCampaign?.hide_rewards
  ) {
    return null;
  }

  return (
    <>
      <LeaderboardTitle
        title={t("tradingRewards.rewards")}
        isMobile={isMobile}
      />
      <RewardsDesktopUI
        campaign={state.currentCampaign}
        userdata={state.userData}
        onLearnMore={state.onLearnMore}
        onTradeNow={state.onTradeNow}
        isMobile={isMobile}
        shouldShowJoinButton={state.shouldShowJoinButton}
        joinCampaign={state.joinCampaign}
        hideConfig={hideConfig}
      />
    </>
  );
};
