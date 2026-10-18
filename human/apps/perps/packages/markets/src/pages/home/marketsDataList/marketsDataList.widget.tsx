import React from "react";
import { useScreen } from "@paxeer-network/ui";
import { MobileMarketsDataList } from "./marketsDataList.mobile.ui";
import { useMarketsDataListScript } from "./marketsDataList.script";
import { MarketsDataList } from "./marketsDataList.ui";

export const MarketsDataListWidget: React.FC = () => {
  const { isMobile } = useScreen();
  const state = useMarketsDataListScript();
  return isMobile ? (
    <MobileMarketsDataList {...state} />
  ) : (
    <MarketsDataList {...state} />
  );
};
