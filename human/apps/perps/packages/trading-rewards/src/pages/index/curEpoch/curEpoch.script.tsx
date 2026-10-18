import { useMemo } from "react";
import { useAccount, useWalletConnector } from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { useTradingRewardsContext } from "../provider";
import { RewardsTooltipProps } from "./rewardsTooltip";

export const useCurEpochScript = () => {
  const {
    epochList,
    curEpochEstimate: estimate,
    brokerId,
    brokerName,
    statusInfo,
    showEpochPauseCountdown,
  } = useTradingRewardsContext();
  const { wrongNetwork, disabledConnect } = useAppContext();
  const { connect } = useWalletConnector();
  const { state } = useAccount();

  const hideData = useMemo(() => {
    return (
      state.status <= AccountStatusEnum.SignedIn ||
      wrongNetwork ||
      disabledConnect
    );
  }, [state, wrongNetwork, disabledConnect]);

  const notConnected = useMemo(() => {
    return state.status <= AccountStatusEnum.SignedIn || disabledConnect;
  }, [state, disabledConnect]);

  const rewardsTooltip = useMemo((): RewardsTooltipProps | undefined => {
    if (typeof estimate === "undefined" || estimate === null) return undefined;
    const otherRewards = estimate.rows
      .filter((item) => item.broker_id !== brokerId)
      .reduce((a, b) => a + b.est_r_account, 0);
    const curRewards = Number(estimate.est_r_wallet) - otherRewards;
    return {
      brokerName,
      curRewards,
      otherRewards,
    };
  }, [brokerId, brokerName, estimate]);

  return {
    epochList,
    estimate,
    hideData,
    notConnected,
    connect,
    rewardsTooltip: hideData ? undefined : rewardsTooltip,
    statusInfo,
    showEpochPauseCountdown,
  };
};

export type CurEpochReturns = ReturnType<typeof useCurEpochScript>;
