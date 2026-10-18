import { useScreen } from "@paxeer-network/ui";
import { CampaignConfig } from "../type";
import { CampaignsHeaderUI } from "./campaigns.header.desktop.ui";
import { CampaignsHeaderMobileUI } from "./campaigns.header.mobile.ui";

export const CampaignsHeaderWidget = (props: {
  campaigns: CampaignConfig[];
  currentCampaignId: string;
  onCampaignChange: (campaignId: string) => void;
  backgroundSrc?: string;
  totalPrizePool?: { amount: number; currency: string };
}) => {
  const { isMobile } = useScreen();
  return isMobile ? (
    <CampaignsHeaderMobileUI {...props} />
  ) : (
    <CampaignsHeaderUI {...props} />
  );
};
