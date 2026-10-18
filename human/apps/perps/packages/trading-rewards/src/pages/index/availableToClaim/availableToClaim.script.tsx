import { useCallback, useMemo } from "react";
import { useDataTap } from "@paxeer-network/react-app";
import { Decimal } from "@paxeer-network/utils";
import { ChainNamespace } from "@paxeer-network/types";
import { ENVType, useGetEnv, useWalletConnector } from "@paxeer-network/hooks";
import { useTradingRewardsContext } from "../provider";

export type AvailableReturns = {
  order?: number;
  esOrder?: number;
  goToClaim?: (e: any) => void;
};

export const useAvailableScript = (): AvailableReturns => {
  const {
    totalOrderClaimedReward,
    walletRewardsHistory,
    totalEsOrderClaimedReward,
  } = useTradingRewardsContext();
  const [orderClaimedRewardData] = totalOrderClaimedReward;
  const [esOrderClaimedRewardData] = totalEsOrderClaimedReward;

  const { namespace } = useWalletConnector();

  const [data] = walletRewardsHistory;

  const lifetimeOrderReward = useMemo(() => {
    if (namespace === ChainNamespace.evm) {
      return data?.wallet_lifetime_trading_rewards_order;
    }
    return data?.wallet_pending_trading_rewards_order;
  }, [namespace, data]);

  const lifetimeEsOrderReward = useMemo(() => {
    if (namespace === ChainNamespace.evm) {
      return data?.wallet_lifetime_trading_rewards_escrow;
    }
    return data?.wallet_pending_trading_rewards_escrow;
  }, [data, namespace]);

  const env = useGetEnv();
  const goToClaim = (e: any) => {
    const url = `https://${
      env !== ENVType.prod ? `${env}-` : ""
    }app.paxeer-network.network/tradingRewards`;
    window.open(url, "_blank");
  };

  const calculateRemainingReward = useCallback(
    (totalReward: string | undefined, claimedReward: number | undefined) => {
      if (
        typeof claimedReward !== "undefined" &&
        typeof totalReward !== "undefined"
      ) {
        const remainingReward = new Decimal(totalReward)
          .sub(claimedReward)
          .toFixed(18, Decimal.ROUND_DOWN);
        return Number(remainingReward);
      }
      return undefined;
    },
    []
  );

  const availableOrder = useMemo(
    () => calculateRemainingReward(lifetimeOrderReward, orderClaimedRewardData),
    [lifetimeOrderReward, orderClaimedRewardData, calculateRemainingReward]
  );

  const availableEsOrder = useMemo(
    () =>
      calculateRemainingReward(lifetimeEsOrderReward, esOrderClaimedRewardData),
    [lifetimeEsOrderReward, esOrderClaimedRewardData, calculateRemainingReward]
  );

  const orderValue = useDataTap<number | undefined>(availableOrder);
  const esorderValue = useDataTap<number | undefined>(availableEsOrder);
  return {
    order: orderValue ?? undefined,
    esOrder: esorderValue ?? undefined,
    goToClaim,
  };
};
