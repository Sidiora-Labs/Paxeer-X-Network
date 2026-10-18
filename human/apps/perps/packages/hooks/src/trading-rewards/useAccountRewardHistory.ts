import { useEffect, useMemo, useState } from "react";
import { useAllBrokers } from "./useAllBrokers";
import { usePrivateQuery } from "../usePrivateQuery";
// import { assert } from "ethers";

export type AccountRewardsHistoryRow = {
  broker_id: string;
  wallet_epoch_avg_staked?: number;
  trader_score_major?: number;
  trader_score_alts?: number;
  epoch_token?: string;
  reward_status?: string;
  r_major?: number;
  r_alts?: number;
  r_account?: number;
  broker_name?: string;
};

export type AccountRewardsHistory = {
  epoch_id: number;
  broker: AccountRewardsHistoryRow[];
};

export const useAccountRewardsHistory = (address?: string) => {
  const [brokers] = useAllBrokers();
  const { data } = usePrivateQuery<AccountRewardsHistory[] | undefined>(
    `/v1/public/trading_rewards/account_rewards_history?address=${
      address ?? ""
    }`,
    {
      revalidateOnFocus: false,
      formatter: (res) => {
        const data = res?.rows?.map((e: any) => {
          e.broker = e.broker.map((item: any) => ({
            ...item,
            broker_name: item?.broker_id,
          }));
          return {
            epoch_id: e.epoch_id,
            broker: e.broker,
          };
        });

        return data;
      },
    }
  );

  const result = useMemo(() => {
    if (data && brokers) {
      const newData = [...data];
      newData.forEach((data) => {
        const list = data.broker;
        for (let i = 0; i < list.length; i++) {
          const broker = list[i];

          const name = brokers[broker.broker_id];
          list[i].broker_name = name || list[i].broker_name;
        }
      });
      return newData;
    }
    return data;
  }, [data, brokers]);

  return { data: result };
};
