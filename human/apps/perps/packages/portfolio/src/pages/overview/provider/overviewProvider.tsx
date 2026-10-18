import { FC, PropsWithChildren, useMemo } from "react";
import { TWType } from "@paxeer-network/hooks";
import { useRewardsData } from "../mobile/useRewardsData.script";
import { useAssetsHistoryData } from "../shared/useAssetHistory";
import { OverviewContext, OverviewContextState } from "./overviewContext";

export const localKey = "portfolio_performance_period";

export const OverviewProvider: FC<PropsWithChildren<{ type?: TWType }>> = (
  props,
) => {
  const { type, children } = props;
  const state = useAssetsHistoryData(localKey, { isRealtime: true });
  const rewardsData = useRewardsData({ type: type });

  const memoizedValue = useMemo<OverviewContextState>(() => {
    return {
      ...state,
      type: type,
      ...rewardsData,
    };
  }, [state, type, rewardsData]);

  return (
    <OverviewContext.Provider value={memoizedValue}>
      {children}
    </OverviewContext.Provider>
  );
};
