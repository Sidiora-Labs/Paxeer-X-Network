import React from "react";
import { useFundingRateScript } from "./fundingRate.script";
import { FundingRate } from "./fundingRate.ui";

export const FundingRateWidget: React.FC<{ symbol: string }> = (props) => {
  const state = useFundingRateScript(props.symbol);
  return <FundingRate {...state} />;
};
