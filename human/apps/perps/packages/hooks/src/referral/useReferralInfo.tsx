import { useCallback, useMemo } from "react";
import { useSubAccountQuery } from "../subAccount/useSubAccountQuery";
import { useAccount } from "../useAccount";
import { useLocalStorage } from "../useLocalStorage";
import { noCacheConfig } from "../utils";
import type { RefferalAPI } from "./api";

export const useReferralInfo = (): {
  data?: RefferalAPI.ReferralInfo;
  isTrader?: boolean;
  isAffiliate?: boolean;
  error: any;
  isLoading: boolean;
  getFirstRefCode: () => RefferalAPI.ReferralCode | undefined;
} => {
  const { state } = useAccount();

  const { data, isLoading, error } =
    useSubAccountQuery<RefferalAPI.ReferralInfo>("/v1/referral/info", {
      accountId: state?.mainAccountId,
      revalidateOnFocus: true,
      errorRetryCount: 3,
      ...noCacheConfig,
    });

  const isTrader = useMemo(() => {
    if (typeof data?.referee_info?.referer_code === "undefined") {
      return undefined;
    }
    return (data?.referee_info?.referer_code?.length || 0) > 0;
  }, [data?.referee_info]);

  const isAffiliate = useMemo(() => {
    if (typeof data?.referrer_info?.referral_codes === "undefined") {
      return undefined;
    }
    return (data?.referrer_info?.referral_codes?.length || 0) > 0;
  }, [data?.referrer_info]);

  const [pinCodes] = useLocalStorage<string[]>("paxeerNetwork_referral_codes", []);

  const getFirstRefCode = useCallback(() => {
    if (!data?.referrer_info?.referral_codes) {
      return undefined;
    }
    const referralCodes = [...data?.referrer_info?.referral_codes];

    const pinedItems: RefferalAPI.ReferralCode[] = [];

    for (let i = 0; i < pinCodes.length; i++) {
      const code = pinCodes[i];

      const index = referralCodes.findIndex((item) => item.code === code);
      if (index !== -1) {
        pinedItems.push({ ...referralCodes[index] });
        referralCodes.splice(index, 1);
      }
    }

    const newCodes = [...pinedItems, ...referralCodes];

    return newCodes?.[0];
  }, [pinCodes, data]);

  return {
    data,
    isTrader,
    isAffiliate,
    error,
    isLoading,
    getFirstRefCode,
  };
};
