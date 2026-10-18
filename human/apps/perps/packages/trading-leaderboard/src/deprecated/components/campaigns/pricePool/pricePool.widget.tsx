import { FC } from "react";
import { Box, Button, cn, Flex, Text, useScreen } from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { CampaignConfig } from "../type";
import { PricePoolDesktopUI } from "./pricePool.desktop.ui";
import { usePricePoolScript } from "./pricePool.script";

type PricePoolWidgetProps = {
  campaign: CampaignConfig;
  statistics: any;
  tradingVolume: number;
  isMobile: boolean;
  isCampaignStarted: boolean;
  tooltipProps: any;
  showTradeButton: boolean;
  onLearnMore: () => void;
  onTradeNow: () => void;
  shouldShowJoinButton: boolean;
  joinCampaign: (data: { campaign_id: string | number }) => Promise<any>;
  isJoining: boolean;
  canTrade: boolean;
  totalPrizePool: { amount: number; currency: string } | null;
};

export const PricePoolWidget: FC<PricePoolWidgetProps> = (props) => {
  const state = usePricePoolScript(props.campaign);

  const emphasisConfig = props.campaign?.emphasisConfig;
  const { isMobile } = useScreen();

  return (
    <div>
      <PricePoolDesktopUI {...state} {...props} />
      {emphasisConfig?.hideConnectWallet && (
        <Box mt={4}>
          <AuthGuard
            buttonProps={{
              size: isMobile ? "lg" : "xl",
              fullWidth: true,
              className: "oui-rounded-full lg:oui-rounded-2xl",
            }}
            labels={{
              connectWallet: emphasisConfig?.walletConnect?.title,
            }}
            descriptions={{
              // @ts-ignore
              connectWallet: (
                <Text.gradient
                  color="brand"
                  className={cn([
                    "oui-text-center oui-font-semibold",
                    "oui-text-[14px] oui-leading-[20px] lg:oui-text-[16px] lg:oui-leading-[24px]",
                  ])}
                >
                  {emphasisConfig?.walletConnect?.description}
                </Text.gradient>
              ),
            }}
          />
        </Box>
      )}
    </div>
  );
};
