import React, { useMemo } from "react";
import { useTradingRewardsStatus, EpochStatus } from "@paxeer-network/hooks";
import { Flex, Box, cn } from "@paxeer-network/ui";
import { TradingRewardsProvider } from "./provider";
import type { TitleConfig } from "./title/title.script";

const LazyTitleWidget = React.lazy(() =>
  import("./title/title.widget").then((mod) => {
    return { default: mod.TitleWidget };
  }),
);

const LazyCurEpochWidget = React.lazy(() =>
  import("./curEpoch").then((mod) => {
    return { default: mod.CurEpochWidget };
  }),
);

const LazyAvailableToClaimWidget = React.lazy(() =>
  import("./availableToClaim").then((mod) => {
    return { default: mod.AvailableToClaimWidget };
  }),
);

const LazyStakeBoosterWidget = React.lazy(() =>
  import("./stakeBooster").then((mod) => {
    return { default: mod.StakeBoosterWidget };
  }),
);

const LazyRewardsHistoryWidget = React.lazy(() =>
  import("./rewardHistory").then((mod) => {
    return { default: mod.RewardsHistoryWidget };
  }),
);

const StakeBooster: React.FC = () => {
  const { statusInfo } = useTradingRewardsStatus(false);
  const isStakeBoosterVisible = useMemo(() => {
    return statusInfo?.epochStatus === EpochStatus.active;
  }, [statusInfo?.epochStatus]);
  return isStakeBoosterVisible ? (
    <React.Suspense fallback={null}>
      <LazyStakeBoosterWidget />
    </React.Suspense>
  ) : null;
};

export const HomePage: React.FC<{
  titleConfig?: TitleConfig;
  className?: string;
  showEpochPauseCountdown?: boolean;
}> = (props) => {
  return (
    <TradingRewardsProvider
      titleConfig={props.titleConfig}
      showEpochPauseCountdown={props.showEpochPauseCountdown}
    >
      <Flex
        id="oui-tradingRewards-home-page"
        className={cn("oui-h-lvw oui-w-full", props.className)}
        direction={"column"}
        gap={4}
      >
        <React.Suspense fallback={null}>
          <LazyTitleWidget />
        </React.Suspense>
        <Flex className="oui-flex oui-size-full oui-flex-col oui-gap-4 2xl:oui-flex-row 2xl:oui-items-stretch">
          <Box className="oui-w-full 2xl:oui-size-auto 2xl:oui-flex-1">
            <React.Suspense fallback={null}>
              <LazyCurEpochWidget />
            </React.Suspense>
          </Box>
          <Flex className="oui-flex oui-w-full oui-flex-col oui-gap-4 xl:oui-flex-row 2xl:oui-flex-1 2xl:oui-flex-col">
            <React.Suspense fallback={null}>
              <LazyAvailableToClaimWidget />
            </React.Suspense>
            <StakeBooster />
          </Flex>
        </Flex>
        <React.Suspense fallback={null}>
          <LazyRewardsHistoryWidget />
        </React.Suspense>
      </Flex>
    </TradingRewardsProvider>
  );
};
