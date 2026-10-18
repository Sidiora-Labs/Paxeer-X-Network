import type { FC } from "react";
import { useScreen } from "@paxeer-network/ui";
import { MobileFundingComparison } from "./fundingComparison.mobile.ui";
import { useFundingComparisonScript } from "./fundingComparison.script";
import { FundingComparison } from "./fundingComparison.ui";

export const FundingComparisonWidget: FC = () => {
  const state = useFundingComparisonScript();
  const { isMobile } = useScreen();
  return isMobile ? (
    <MobileFundingComparison {...state} />
  ) : (
    <FundingComparison {...state} />
  );
};
