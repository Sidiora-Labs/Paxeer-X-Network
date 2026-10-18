import { createContext, useContext } from "react";
import { TWType } from "@paxeer-network/hooks";
import { UseRewardsDataReturn } from "../mobile/useRewardsData.script";
import { useAssetsHistoryDataReturn } from "../shared/useAssetHistory";

export type OverviewContextState = {
  // period: PeriodType;
  type?: TWType;
} & useAssetsHistoryDataReturn &
  UseRewardsDataReturn;

export const OverviewContext = createContext<OverviewContextState>(
  {} as OverviewContextState,
);

export const useOverviewContext = () => {
  return useContext(OverviewContext);
};
