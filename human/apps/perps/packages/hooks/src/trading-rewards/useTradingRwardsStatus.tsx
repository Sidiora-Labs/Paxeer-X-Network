import { useEffect, useState } from "react";
import { useQuery } from "..";

export enum EpochStatus {
  active = "Active",
  paused = "Paused",
  ended = "Ended",
}
export type StatusInfo = {
  epochStatus: EpochStatus;
  currentEpoch: number | undefined;
  lastCompletedEpoch: number | undefined;
};

export function useTradingRewardsStatus(isMMRewards: boolean): {
  statusInfo: StatusInfo | undefined;
} {
  const path = isMMRewards
    ? "/v1/public/market_making_rewards/status"
    : "/v1/public/trading_rewards/status";
  const { data: statusInfo } = useQuery<StatusInfo | undefined>(path, {
    formatter: (res: any) => {
      const data = {
        ...res,
        epochStatus: res.epoch_status as EpochStatus,
        currentEpoch: res.current_epoch,
        lastCompletedEpoch: res.last_completed_epoch,
      };
      return data;
    },
  });

  return {
    statusInfo,
  };
}
