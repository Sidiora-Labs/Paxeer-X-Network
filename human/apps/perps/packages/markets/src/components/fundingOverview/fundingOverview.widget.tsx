import { FC } from "react";
import { useScreen } from "@paxeer-network/ui";
import { MobileFundingOverview } from "./fundingOverview.mobile.ui";
import { useFundingOverviewScript } from "./fundingOverview.script";
import { FundingOverview } from "./fundingOverview.ui";

export const FundingOverviewWidget: FC = () => {
  const props = useFundingOverviewScript();
  const { isMobile } = useScreen();
  return isMobile ? (
    <MobileFundingOverview {...props} />
  ) : (
    <FundingOverview {...props} />
  );
};
