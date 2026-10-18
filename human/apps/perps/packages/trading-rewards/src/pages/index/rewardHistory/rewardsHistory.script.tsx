import { useMemo } from "react";
import {
  EpochInfoItem,
  useAccount,
  useAccountRewardsHistory,
  WalletRewardsItem,
} from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { usePagination } from "@paxeer-network/ui";
import { getTimestamp } from "@paxeer-network/utils";
import { RewardsTooltipProps } from "../curEpoch/rewardsTooltip";
import { useTradingRewardsContext } from "../provider";

export type ListType = EpochInfoItem & {
  info?: WalletRewardsItem;
  state?: string;
  rewardsTooltip?: RewardsTooltipProps;
};

export type RewardsHistoryReturns = ReturnType<typeof useRewardsHistoryScript>;

export const useRewardsHistoryScript = () => {
  const { account } = useAccount();
  const {
    epochList,
    walletRewardsHistory,
    totalOrderClaimedReward,
    brokerId,
    brokerName,
  } = useTradingRewardsContext();
  const epochInfos = epochList?.[0];
  const { isNotStared } = epochList?.[1];
  const [history] = walletRewardsHistory;
  const { wrongNetwork, disabledConnect } = useAppContext();

  const { data: accountHistory } = useAccountRewardsHistory(account.address);

  const data = useMemo(() => {
    if (isNotStared || disabledConnect) {
      return [];
    }
    const list = [...(epochInfos || [])];

    const combineData = list.map((e): ListType => {
      const id = e.epoch_id;
      const index = history?.rows.findIndex((info) => id === info.epoch_id);

      let rewardsTooltip: RewardsTooltipProps | undefined = undefined;

      if (index !== -1 && !wrongNetwork) {
        const info = history?.rows?.[index as number];
        const _findIndex = accountHistory?.findIndex((item: any) => {
          return item?.epoch_id === e.epoch_id;
        });

        if (
          accountHistory !== undefined &&
          _findIndex !== undefined &&
          _findIndex !== -1
        ) {
          // get broker
          const brokerList = accountHistory[_findIndex].broker;
          const curBrokerIndex = brokerList.findIndex(
            (item) => item.broker_id === brokerId,
          );
          const curBroker =
            curBrokerIndex !== -1 ? brokerList[curBrokerIndex] : undefined;
          const curRewards = curBroker?.r_account ?? 0;
          const otherRewards = Math.max(0, (info?.r_wallet ?? 0) - curRewards);
          rewardsTooltip = {
            brokerName,
            curRewards,
            otherRewards,
          };
        }
        return {
          ...e,
          info,
          rewardsTooltip,
        } as ListType;
      }
      return {
        ...e,
      } as ListType;
    });

    combineData.sort((a, b) => a.epoch_id - b.epoch_id); // asc
    let [claimedReward] = totalOrderClaimedReward;
    if (typeof claimedReward !== "undefined") {
      for (let i = 0; i < combineData.length; i++) {
        const element = combineData[i];
        if (typeof element?.info === "undefined") {
          element.state = "Null";
          continue;
        }
        const status = element.info?.reward_status;
        if (status === "Confirmed") {
          if (claimedReward - element.info?.r_wallet >= 0) {
            element.state = "Claimed";
          } else {
            element.state = "Claimable";
          }
          claimedReward -= element.info?.r_wallet;
        } else if (status === "Pending") {
          element.state = "Processing";
        }
      }
    }
    combineData.sort((a, b) => b.epoch_id - a.epoch_id);
    const curDate = getTimestamp();
    return combineData.filter((item) => item.end_time <= curDate);
  }, [
    history,
    epochInfos,
    totalOrderClaimedReward,
    isNotStared,
    wrongNetwork,
    accountHistory,
    disabledConnect,
  ]);

  const { pagination } = usePagination();

  return {
    data,
    originalData: data,
    pagination,
    isLoading: epochList[1].isLoading,
  };
};
