import { useCallback, useEffect, useMemo, useState } from "react";
import { TWType } from "./types";
import { useQuery } from "../useQuery";
import { getTimestamp } from "@paxeer-network/utils";

export type EpochInfoItem = {
  epoch_id: number;
  start_time: number;
  end_time: number;
  // power_fees_paid_major: number;
  // power_fees_paid_alts: number;
  // power_staked_major: number;
  // power_staked_alts: number;
  epoch_token: string;
  max_reward_amount: number;
  // k_constant_major: number;
  // k_constant_alts: number;
};

export type EpochInfoType = [
  data: EpochInfoItem[] | undefined,
  {
    isLoading: boolean;
    curEpochInfo: EpochInfoItem | undefined;
    isNotStared: boolean;
    refresh: () => void;
  }
];

export const useEpochInfo = (type: TWType): EpochInfoType => {
  // const [data, setData] = useState<EpochInfoItem[] | undefined>(undefined);
  const [curEpochInfo, setCurEpochInfo] = useState<EpochInfoItem | undefined>(
    undefined
  );

  const path =
    type === TWType.normal
      ? "/v1/public/trading_rewards/epoch_info"
      : "/v1/public/market_making_rewards/epoch_info";
  const {
    data: epochInfo,
    error,
    isLoading,
    mutate: refresh,
  } = useQuery(path, {
    formatter: (res) => {
      if (typeof res === "object" && "rows" in res! && "current_epoch" in res) {
        const { rows, current_epoch } = res;
        if (Array.isArray(rows)) {
          const list: EpochInfoItem[] = rows.map(
            (e: any) => e as EpochInfoItem
          );
          list.sort((a, b) => b.end_time - a.end_time);
          const curEpochIndex = list?.findIndex(
            (item: any) => item.epoch_id === current_epoch
          );

          const epochOne = list.find((item) => item.epoch_id === 1);

          if (epochOne && epochOne?.start_time > getTimestamp()) {
            // not start
            setCurEpochInfo(epochOne);
          } else {
            setCurEpochInfo(
              curEpochIndex !== -1 ? list?.[curEpochIndex] : undefined
            );
          }

          return list;
        }
      }
      return [];
    },
  });

  const isNotStared= useMemo(() => {
    // if (curEpochInfo) {
    //   return curEpochInfo?.start_time > Date.now();
    // }
    // return true;

    const epochOne = epochInfo?.find((item) => item.epoch_id === 1);
    if (epochOne) {
      return epochOne.start_time > getTimestamp();
    }
    return true;
  }, [epochInfo]);

  return [epochInfo, { isLoading, curEpochInfo, isNotStared, refresh }];
};
