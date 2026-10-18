import { useMemo } from "react";
import useSWRSubscription from "swr/subscription";
import { Decimal } from "@paxeer-network/utils";
import { useAccountInfo } from "./paxeer-network/useAccountInfo";
import type { RefferalAPI } from "./referral";
import { usePrivateQuery } from "./usePrivateQuery";
import { useWS } from "./useWS";
import { noCacheConfig } from "./utils";

const bpsToFraction = (bps?: number) => new Decimal(bps ?? 0).mul(0.0001); // 1 bps -> 0.0001

const formatFractionAsPercent = (val: Decimal) => `${val.mul(100).toNumber()}%`;

export const useFeeState = () => {
  const { data: accountInfo, isLoading: isAccountLoading } = useAccountInfo();

  const { data: referralData } = usePrivateQuery<RefferalAPI.ReferralInfo>(
    "/v1/referral/info",
    {
      revalidateOnFocus: true,
      errorRetryCount: 3,
      ...noCacheConfig,
    },
  );

  const ws = useWS();
  const topic = "account";
  const { data: wsAccountData } = useSWRSubscription(
    "account",
    (_, { next }) => {
      const unsubscribe = ws.subscribe(topic, {
        onMessage: (message: unknown) => {
          next(null, message);
        },
      });

      return () => {
        unsubscribe?.();
      };
    },
  );

  const takerFeeBps = useMemo(() => {
    if (wsAccountData?.accountDetail?.futuresTakerFeeRate) {
      return wsAccountData?.accountDetail?.futuresTakerFeeRate;
    }
    return accountInfo?.futures_taker_fee_rate;
  }, [
    accountInfo?.futures_taker_fee_rate,
    wsAccountData?.accountDetail?.futuresTakerFeeRate,
  ]);
  const makerFeeBps = useMemo(() => {
    if (wsAccountData?.accountDetail?.futuresMakerFeeRate) {
      return wsAccountData?.accountDetail?.futuresMakerFeeRate;
    }
    return accountInfo?.futures_maker_fee_rate;
  }, [
    accountInfo?.futures_maker_fee_rate,
    wsAccountData?.accountDetail?.futuresMakerFeeRate,
  ]);
  const rwaTakerFeeBps = useMemo(() => {
    if (wsAccountData?.accountDetail?.rwaFuturesTakerFeeRate) {
      return wsAccountData?.accountDetail?.rwaFuturesTakerFeeRate;
    }
    return accountInfo?.rwa_taker_fee_rate;
  }, [
    accountInfo?.rwa_taker_fee_rate,
    wsAccountData?.accountDetail?.rwaFuturesTakerFeeRate,
  ]);
  const rwaMakerFeeBps = useMemo(() => {
    if (wsAccountData?.accountDetail?.rwaFuturesMakerFeeRate) {
      return wsAccountData?.accountDetail?.rwaFuturesMakerFeeRate;
    }
    return accountInfo?.rwa_maker_fee_rate;
  }, [
    accountInfo?.rwa_maker_fee_rate,
    wsAccountData?.accountDetail?.rwaFuturesMakerFeeRate,
  ]);

  const refereeRebate = referralData?.referee_info?.referee_rebate_rate;

  const takerFee = useMemo(() => {
    if (isAccountLoading || takerFeeBps === null || takerFeeBps === undefined) {
      return "-";
    }
    return formatFractionAsPercent(bpsToFraction(takerFeeBps));
  }, [isAccountLoading, takerFeeBps]);

  const makerFee = useMemo(() => {
    if (isAccountLoading || makerFeeBps === null || makerFeeBps === undefined) {
      return "-";
    }
    return formatFractionAsPercent(bpsToFraction(makerFeeBps));
  }, [isAccountLoading, makerFeeBps]);

  const rwaTakerFee = useMemo(() => {
    if (
      isAccountLoading ||
      rwaTakerFeeBps === null ||
      rwaTakerFeeBps === undefined
    ) {
      return "-";
    }
    return formatFractionAsPercent(bpsToFraction(rwaTakerFeeBps));
  }, [isAccountLoading, rwaTakerFeeBps]);

  const rwaMakerFee = useMemo(() => {
    if (
      isAccountLoading ||
      rwaMakerFeeBps === null ||
      rwaMakerFeeBps === undefined
    ) {
      return "-";
    }
    return formatFractionAsPercent(bpsToFraction(rwaMakerFeeBps));
  }, [isAccountLoading, rwaMakerFeeBps]);

  return {
    takerFee,
    makerFee,
    refereeRebate,
    rwaTakerFee,
    rwaMakerFee,
  } as const;
};
