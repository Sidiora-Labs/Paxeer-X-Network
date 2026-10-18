
import { useEffect, useMemo, useState } from "react";
import { TWType } from "./types";
import { useAccount } from "../useAccount";
import { useWalletConnector } from "../walletConnectorContext";
import { useQuery } from "../useQuery";
import { useAllBrokers } from "./useAllBrokers";

export type CurrentEpochEstimateRow = {
  broker_id: string;
  est_r_account: number;
  /// front end added
  broker_name: string;
};

export type CurrentEpochEstimate = {
  est_r_wallet: string;
  est_stake_boost?: number;
  est_avg_stake?: number;
  est_trading_volume: number;
  rows: CurrentEpochEstimateRow[];
};

export const useCurEpochEstimate = (type: TWType) => {
  const [data, setData] = useState<CurrentEpochEstimate | undefined>(undefined);
  // const { address, brokers, isNotSupportChain } = useAppContext();
  const { account } = useAccount();

  const brokers = useAllBrokers();

  /// TODO: use right value
  const isNotSupportChain = false;

  const address = account.address;



  const path =
  type === TWType.normal
    ? `/v1/public/trading_rewards/current_epoch_estimate?address=${address}`
    : `/v1/public/market_making_rewards/current_epoch_estimate?address=${address}`;
  const { data: estimateData } = useQuery<CurrentEpochEstimate | undefined>(address !== undefined ? path : '', {
    formatter: (res: any) => {
      const data = {
        ...res,
        rows:
          res.rows?.map((item: any) => ({
            ...item,
            broker_name: item.broker_id,
          })) || [],
        est_trading_volume: res?.est_maker_volume || res?.est_trading_volume,
      };
      return data;
    }
  });
  


  const reuslt = useMemo(() => {
    if (estimateData) {
      const newData = { ...estimateData };
      newData.rows = newData.rows?.map((item: any) => ({
        ...item,
        broker_name: brokers[item.broker_id] || item.broker_id,
      }));

      return newData;
    }
    return data;
  }, [estimateData, brokers]);

  return [reuslt] as const;
};
