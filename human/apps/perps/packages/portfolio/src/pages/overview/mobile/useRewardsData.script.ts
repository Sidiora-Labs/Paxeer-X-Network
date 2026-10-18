import { useMemo, useRef, useEffect } from "react";
import {
  useAllBrokers,
  useConfig,
  useCurEpochEstimate,
  useEpochInfo,
  useGetClaimed,
  useAccount,
  RefferalAPI,
  usePrivateQuery,
  noCacheConfig,
} from "@paxeer-network/hooks";
import { DistributionId, TWType } from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { useOverviewContext } from "../provider/overviewContext";

export const useRewardsData = ({ type = TWType.normal }: { type?: TWType }) => {
  const totalOrderClaimedReward = useGetClaimed(
    type === TWType.mm ? DistributionId.mmOrder : DistributionId.order,
  );
  const [curEpochEstimate] = useCurEpochEstimate(type);
  const [brokers] = useAllBrokers();
  const { state } = useAccount();
  const { data, mutate } = usePrivateQuery<RefferalAPI.ReferralInfo>(
    "/v1/referral/info",
    { revalidateOnFocus: true, errorRetryCount: 3, ...noCacheConfig },
  );

  const epochList = useEpochInfo(type as TWType);
  const brokerId = useConfig("brokerId");

  const brokerName = useMemo(() => {
    return brokers?.[brokerId];
  }, [brokerId, brokers]);

  const lastStete = useRef<AccountStatusEnum>(AccountStatusEnum.NotConnected);

  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (lastStete.current !== state.status) {
      lastStete.current = state.status;
      timerRef.current = setTimeout(() => {
        mutate();
      }, 1000);
    }
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, [state.status]);

  return {
    totalOrderClaimedReward,
    curEpochEstimate,
    epochList,
    brokerName,
    referralInfo: data,
  };
};

export const useRewardsDataScript = () => {
  const {
    totalOrderClaimedReward,
    curEpochEstimate,
    epochList,
    brokerName,
    referralInfo,
  } = useOverviewContext();
  const { state } = useAccount();
  const { wrongNetwork } = useAppContext();
  const isSignIn =
    state.status === AccountStatusEnum.EnableTrading ||
    state.status === AccountStatusEnum.EnableTradingWithoutConnected;

  return {
    totalOrderClaimedReward,
    curEpochEstimate,
    epochList,
    brokerName,
    isSignIn,
    referralInfo,
    wrongNetwork,
  };
};

export type UseRewardsDataReturn = ReturnType<typeof useRewardsData>;
