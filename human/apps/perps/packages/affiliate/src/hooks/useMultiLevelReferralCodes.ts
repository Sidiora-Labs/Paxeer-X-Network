import { useMemo } from "react";
import { useRefereeInfo } from "@paxeer-network/hooks";
import { useReferralContext } from "../provider";
import { copyText } from "../utils/utils";
import { useMultiLevelRebateInfo } from "./useReferralApi";

export type ReferralCodesRow = {
  code: string;
  direct_invites: number;
  indirect_invites: number;
  direct_volume: number;
  indirect_volume: number;
  direct_rebate: number;
  indirect_rebate: number;
  direct_bonus_rebate?: number;
  max_rebate_rate: number;
  base_rebate_rate?: number;
  referee_rebate_rate: number;
  referrer_rebate_rate: number;
  total_invites: number;
  total_volume: number;
  total_rebate: number;
  referral_type: "multi" | "single";
};

export type UseMultiLevelReferralCodesReturns = {
  codes?: ReferralCodesRow[];
  copyCode: (code: string) => void;
};

export const useMultiLevelReferralCodes =
  (): UseMultiLevelReferralCodesReturns => {
    const { referralInfo } = useReferralContext();
    const { data: multiLevelRebateInfo } = useMultiLevelRebateInfo();
    console.log("multiLevelRebateInfo", multiLevelRebateInfo);
    const [refereesData] = useRefereeInfo({});

    const copyCode = (code: string) => {
      copyText(code);
    };

    const singleLevelTotals = useMemo(() => {
      const totalsMap = new Map<
        string,
        { volume: number; commission: number }
      >();

      if (refereesData) {
        refereesData.forEach((referee: any) => {
          const code = referee?.referral_code;
          if (!code) return;
          const existing = totalsMap.get(code) || { volume: 0, commission: 0 };
          totalsMap.set(code, {
            volume: existing.volume + (referee?.volume || 0),
            commission: existing.commission + (referee?.referral_rebate || 0),
          });
        });
      }

      return totalsMap;
    }, [refereesData]);

    const codes = useMemo((): ReferralCodesRow[] | undefined => {
      const allCodes: ReferralCodesRow[] = [];
      const multiLevelReferralCode = multiLevelRebateInfo?.referral_code;

      // Add multi-level code if present
      if (multiLevelRebateInfo?.referral_code) {
        const maxRebateRate = multiLevelRebateInfo.max_rebate_rate ?? 0;
        const bonusMaxRebateRate =
          multiLevelRebateInfo.bonus_max_rebate_rate ?? maxRebateRate;
        const baseRebateRate = multiLevelRebateInfo.base_rebate_rate ?? 0;
        const defaultRefereeRebateRate =
          multiLevelRebateInfo.default_bonus_referee_rebate_rate ??
          multiLevelRebateInfo.default_referee_rebate_rate ??
          0;
        const referrerRebateRate = Math.max(
          0,
          bonusMaxRebateRate - defaultRefereeRebateRate,
        );

        const directInvites = multiLevelRebateInfo?.direct_invites ?? 0;
        const indirectInvites = multiLevelRebateInfo?.indirect_invites ?? 0;
        const directVolume = multiLevelRebateInfo?.direct_volume ?? 0;
        const indirectVolume = multiLevelRebateInfo?.indirect_volume ?? 0;
        const directRebate = multiLevelRebateInfo?.direct_rebate ?? 0;
        const indirectRebate = multiLevelRebateInfo?.indirect_rebate ?? 0;
        const directBonusRebate =
          multiLevelRebateInfo?.direct_bonus_rebate ?? 0;

        const multiLevelCode: ReferralCodesRow = {
          code: multiLevelRebateInfo.referral_code ?? "",
          direct_invites: directInvites,
          indirect_invites: indirectInvites,
          direct_volume: directVolume,
          indirect_volume: indirectVolume,
          direct_rebate: directRebate,
          indirect_rebate: indirectRebate,
          direct_bonus_rebate: directBonusRebate,
          max_rebate_rate: maxRebateRate,
          base_rebate_rate: baseRebateRate,
          referee_rebate_rate: defaultRefereeRebateRate,
          referrer_rebate_rate: referrerRebateRate,
          total_invites: directInvites + indirectInvites,
          total_volume: directVolume + indirectVolume,
          total_rebate: directRebate + indirectRebate + directBonusRebate,
          referral_type: "multi",
        };

        allCodes.push(multiLevelCode);
      }

      // Add legacy single-level codes
      if (referralInfo?.referrer_info?.referral_codes) {
        allCodes.push(
          ...referralInfo.referrer_info.referral_codes
            .filter((item) =>
              multiLevelReferralCode
                ? item.code !== multiLevelReferralCode
                : true,
            )
            .map((item) => {
              const totals = singleLevelTotals.get(item.code);
              const totalVolume = totals?.volume ?? item.total_volume ?? 0;
              const totalRebate = totals?.commission ?? item.total_rebate ?? 0;
              const totalInvites = item.total_invites ?? 0;

              const row: ReferralCodesRow = {
                code: item.code,
                referral_type: "single",
                direct_invites: totalInvites,
                indirect_invites: 0,
                direct_volume: totalVolume,
                indirect_volume: 0,
                direct_rebate: totalRebate,
                indirect_rebate: 0,
                max_rebate_rate: item.max_rebate_rate ?? 0,
                base_rebate_rate: 0,
                referee_rebate_rate: item.referee_rebate_rate ?? 0,
                referrer_rebate_rate: item.referrer_rebate_rate ?? 0,
                total_invites: totalInvites,
                total_volume: totalVolume,
                total_rebate: totalRebate,
              };
              return row;
            }),
        );
      }

      if (allCodes.length === 0) return undefined;
      return allCodes;
    }, [
      referralInfo?.referrer_info?.referral_codes,
      multiLevelRebateInfo,
      singleLevelTotals,
    ]);

    return {
      codes,
      copyCode,
    };
  };
