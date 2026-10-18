import React from "react";
import { useScreen } from "@paxeer-network/ui";
import { MobileFunding } from "./funding.mobile.ui";
import { useFundingScript } from "./funding.script";
import { Funding } from "./funding.ui";

export const FundingWidget: React.FC = () => {
  const state = useFundingScript();
  const { isMobile } = useScreen();
  return isMobile ? <MobileFunding {...state} /> : <Funding {...state} />;
};
