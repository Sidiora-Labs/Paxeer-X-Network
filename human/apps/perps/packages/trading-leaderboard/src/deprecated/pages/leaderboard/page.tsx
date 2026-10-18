import { FC, ReactNode } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, cn, Flex, Text, useScreen } from "@paxeer-network/ui";
import { LeaderboardBackground } from "../../components/background";
import { CampaignsWidget } from "../../components/campaigns/campaigns.widget";
import { GeneralLeaderboardIWidget } from "../../components/leaderboard128/generalLeaderboard";
import { CampaignLeaderboardWidget } from "../../components/leaderboard/campaignLeaderboard";
import {
  GeneralLeaderboardWidget,
  GeneralLeaderboardWidgetProps,
} from "../../components/leaderboard/generalLeaderboard";
import {
  TradingLeaderboardProvider,
  TradingLeaderboardProviderProps,
  useTradingLeaderboardContext,
} from "../../components/provider";
import { RewardsWidget } from "../../components/rewards/rewards.widget";
import { RuleWidget } from "../../components/rule";

export type LeaderboardPageProps = GeneralLeaderboardWidgetProps &
  TradingLeaderboardProviderProps & {
    style?: React.CSSProperties;
    className?: string;
    hideCampaignsBanner?: boolean;
  };

export const LeaderboardPage: FC<LeaderboardPageProps> = (props) => {
  const { hideCampaignsBanner, ...rest } = props;
  return (
    <TradingLeaderboardProvider {...rest}>
      <div
        style={{
          paddingBottom: "calc(64px + env(safe-area-inset-bottom))",
        }}
        className={cn(
          "oui-trading-leaderboard-page",
          "oui-relative oui-bg-base-10",
          "oui-font-semibold",
          props.className,
        )}
      >
        <CampaignsWidget
          hideCampaignsBanner={hideCampaignsBanner}
          className="oui-relative oui-z-[1] oui-mx-6"
        />
        <RewardsWidget />
        <LeaderboardSection {...props} />
        <RuleWidget />
      </div>
    </TradingLeaderboardProvider>
  );
};

type LeaderboardSectionProps = {
  style?: React.CSSProperties;
  className?: string;
};

export const LeaderboardSection: FC<LeaderboardSectionProps> = (props) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const {
    currentCampaignId,
    currentCampaign,
    backgroundSrc,
    campaignDateRange,
  } = useTradingLeaderboardContext();

  if (currentCampaignId === "general") {
    return (
      <Box px={3} className={cn("oui-mix-blend-screen")}>
        <LeaderboardBackground backgroundSrc={backgroundSrc} />
        <GeneralLeaderboardWidget {...props} className="oui-mt-10" />
      </Box>
    );
  }

  if (currentCampaign?.leaderboard_config?.use_general_leaderboard) {
    return (
      <Box px={3}>
        <LeaderboardTitle
          title={t("tradingLeaderboard.arena")}
          isMobile={isMobile}
        />
        <GeneralLeaderboardIWidget
          {...props}
          className="oui-mt-10"
          campaignDateRange={campaignDateRange}
          weekOneAddresses={
            currentCampaign?.leaderboard_config?.week_one_addresses
          }
        />
      </Box>
    );
  }

  if (
    currentCampaign &&
    !currentCampaign.hide_arena &&
    currentCampaignId != "general"
  ) {
    return (
      <Box px={3}>
        <LeaderboardTitle
          title={t("tradingLeaderboard.arena")}
          isMobile={isMobile}
        />
        <CampaignLeaderboardWidget {...props} campaignId={currentCampaignId} />
      </Box>
    );
  }

  return null;
};

export const LeaderboardTitle = (props: {
  title: ReactNode;
  isMobile?: boolean;
}) => {
  return (
    <Flex
      mb={props.isMobile ? 3 : 6}
      gapY={1}
      justify="center"
      direction="column"
    >
      <Text
        className={cn(
          "oui-trading-leaderboard-title oui-font-bold",
          props.isMobile
            ? "oui-text-[20px] oui-leading-[24px]"
            : "oui-text-[32px] oui-leading-[44px]",
        )}
      >
        {props.title}
      </Text>
      <Box
        width={24}
        height={6}
        r="base"
        className="oui-bg-[linear-gradient(270deg,rgb(var(--oui-gradient-brand-start))_0%,rgb(var(--oui-gradient-brand-end))_100%)]"
      />
    </Flex>
  );
};
