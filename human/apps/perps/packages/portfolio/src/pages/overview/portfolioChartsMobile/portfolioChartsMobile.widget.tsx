import React from "react";
import { EMPTY_LIST } from "@paxeer-network/types";
import { useAssetsChartScript } from "../assetChart";
import { usePortfolioChartsState } from "./portfolioChartsMobile.script";
import { PortfolioChartsMobileUI } from "./portfolioChartsMobile.ui";

export const PortfolioChartsMobileWidget: React.FC = () => {
  const { data, invisible } = useAssetsChartScript();
  const state = usePortfolioChartsState();
  return (
    <PortfolioChartsMobileUI
      data={data || EMPTY_LIST}
      invisible={invisible}
      {...state}
    />
  );
};
