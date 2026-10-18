import React, {
  PropsWithChildren,
  createContext,
  useContext,
  useMemo,
} from "react";
import {
  TWType,
  EpochInfoType,
  useEpochInfo,
  useAllBrokers,
  useGetClaimed,
  DistributionId,
  useCurEpochEstimate,
  CurrentEpochEstimate,
  useWalletRewardsHistory,
  WalletRewardsHistoryReturns,
  Brokers,
  useConfig,
  useTradingRewardsStatus,
  StatusInfo,
} from "@paxeer-network/hooks";
import type { TitleConfig } from "./title/title.script";

export type TradingRewardsState = {
  type: TWType;
  brokerId: string;
  brokerName?: string;
  brokers?: Brokers;
  epochList: EpochInfoType;
  totalOrderClaimedReward: [number | undefined, { refresh: () => void }];
  totalEsOrderClaimedReward: [number | undefined, { refresh: () => void }];
  curEpochEstimate?: CurrentEpochEstimate;
  walletRewardsHistory: WalletRewardsHistoryReturns;
  titleConfig: TitleConfig;
  statusInfo: StatusInfo | undefined;
  showEpochPauseCountdown?: boolean;
};

export const TradingRewardsContext = createContext<TradingRewardsState>(
  {} as TradingRewardsState,
);

export const TradingRewardsProvider: React.FC<
  PropsWithChildren<{
    type?: TWType;
    titleConfig?: TitleConfig;
    showEpochPauseCountdown?: boolean;
  }>
> = (props) => {
  const {
    type = TWType.normal,
    showEpochPauseCountdown,
    titleConfig = {
      docOpenOptions: {
        url: "https://paxeer-network.network/docs/introduction/tokenomics/trading-rewards",
        target: "_blank",
      },
    },
    children,
  } = props;

  const brokerId = useConfig("brokerId");

  const { statusInfo } = useTradingRewardsStatus(type === TWType.mm);
  const totalOrderClaimedReward = useGetClaimed(
    type === TWType.mm ? DistributionId.mmOrder : DistributionId.order,
  );
  const totalEsOrderClaimedReward = useGetClaimed(
    type === TWType.mm ? DistributionId.mmEsOrder : DistributionId.esORder,
  );

  const [brokers] = useAllBrokers();

  const [curEpochEstimate] = useCurEpochEstimate(type);

  const walletRewardsHistory = useWalletRewardsHistory(type);

  const epochList = useEpochInfo(type);

  const brokerName = useMemo(() => {
    return brokers?.[brokerId];
  }, [brokerId, brokers]);

  const memoizedValue = useMemo<TradingRewardsState>(() => {
    return {
      type: type,
      totalOrderClaimedReward,
      totalEsOrderClaimedReward,
      epochList,
      curEpochEstimate,
      walletRewardsHistory,
      titleConfig,
      brokerId,
      brokerName,
      brokers,
      statusInfo,
      showEpochPauseCountdown: showEpochPauseCountdown ?? false,
    };
  }, [
    type,
    totalOrderClaimedReward,
    totalEsOrderClaimedReward,
    epochList,
    curEpochEstimate,
    walletRewardsHistory,
    titleConfig,
    brokerId,
    brokerName,
    brokers,
    statusInfo,
    showEpochPauseCountdown,
  ]);

  return (
    <TradingRewardsContext.Provider value={memoizedValue}>
      {children}
    </TradingRewardsContext.Provider>
  );
};

export const useTradingRewardsContext = () => {
  return useContext<TradingRewardsState>(TradingRewardsContext);
};
