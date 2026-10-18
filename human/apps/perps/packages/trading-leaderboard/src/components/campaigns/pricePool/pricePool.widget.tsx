import { FC } from "react";
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

  return (
    <div>
      <PricePoolDesktopUI {...state} {...props} />
    </div>
  );
};
