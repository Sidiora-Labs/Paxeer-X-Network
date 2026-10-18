import { useMemo } from "react";
import { RefferalAPI, useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useReferralContext } from "../../../provider";
import { generateReferralLink } from "../../../utils/utils";

export type ReferralLinkReturns = {
  onCopy?: (value: string) => void;
  refLink?: string;
  refCode?: string;
  earn?: string;
  share?: string;
  brokerName?: string;
};

export const useReferralLinkScript = (): ReferralLinkReturns => {
  const { t } = useTranslation();

  const onCopy = (value: string) => {
    toast.success(t("common.copy.copied"));
  };

  const { referralInfo, referralLinkUrl, overwrite } = useReferralContext();
  const [pinCodes, setPinCodes] = useLocalStorage<string[]>(
    "paxeerNetwork_referral_codes",
    [] as string[],
  );

  const codes = useMemo((): RefferalAPI.ReferralCode[] => {
    if (!referralInfo?.referrer_info?.referral_codes)
      return [] as RefferalAPI.ReferralCode[];
    const referralCodes = [...referralInfo?.referrer_info?.referral_codes];

    const pinedItems: RefferalAPI.ReferralCode[] = [];

    for (let i = 0; i < pinCodes.length; i++) {
      const code = pinCodes[i];

      const index = referralCodes.findIndex((item) => item.code === code);
      if (index !== -1) {
        pinedItems.push({ ...referralCodes[index] });
        referralCodes.splice(index, 1);
      }
    }

    return [...pinedItems, ...referralCodes];
  }, [referralInfo?.referrer_info?.referral_codes, pinCodes]);

  const firstCode = useMemo(() => {
    if (codes.length === 0) {
      return undefined;
    }

    return codes[0];
  }, [codes]);

  const code = useMemo(() => {
    return firstCode?.code;
  }, [firstCode]);

  const referralLink = useMemo(() => {
    if (!firstCode) return "";

    return generateReferralLink(referralLinkUrl, firstCode.code);
  }, [firstCode]);

  const earn = useMemo(() => {
    const value = new Decimal(firstCode?.referrer_rebate_rate || "0")
      .mul(100)
      .toDecimalPlaces(0, Decimal.ROUND_DOWN)
      .toString();
    return `${value}%`;
  }, [firstCode?.referrer_rebate_rate]);

  const share = useMemo(() => {
    const value = new Decimal(firstCode?.referee_rebate_rate || "0")
      .mul(100)
      .toDecimalPlaces(0, Decimal.ROUND_DOWN)
      .toString();
    return `${value}%`;
  }, [firstCode?.referee_rebate_rate]);

  return {
    onCopy,
    refLink: referralLink,
    refCode: code,
    share,
    earn,
    brokerName: overwrite?.brokerName ?? overwrite?.shortBrokerName,
  };
};
