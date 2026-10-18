import { useMemo } from "react";
import { Decimal } from "@paxeer-network/utils";
import {
  useMaxRebateRate,
  useMultiLevelRebateInfo,
  useRefereeKickbackInfo,
  useVolumePrerequisite,
} from "./useReferralApi";

export const useMultiLevelReferralData = () => {
  const { data: volumePrerequisite, isLoading: volumePrerequisiteLoading } =
    useVolumePrerequisite();

  const {
    data: maxRebateRateRes,
    mutate: maxRebateRateMutate,
    isLoading: maxRebateRateLoading,
  } = useMaxRebateRate();

  const {
    data: multiLevelRebateInfoRes,
    mutate: multiLevelRebateInfoMutate,
    isLoading: multiLevelRebateInfoLoading,
  } = useMultiLevelRebateInfo();

  const isMultiLevelReferralUnlocked =
    volumePrerequisite &&
    volumePrerequisite.current_volume >= volumePrerequisite.required_volume;

  // if maxRebateRateRes is undefined, it means the multi-level referral is not enabled
  const isMultiLevelEnabled = !!maxRebateRateRes;
  const {
    data: refereeKickbackInfoRows,
    isLoading: refereeKickbackInfoLoading,
  } = useRefereeKickbackInfo(isMultiLevelEnabled);
  // The fetcher unwraps the rows-wrapped payload into an array; keep the first row.
  const refereeKickbackInfo = refereeKickbackInfoRows?.[0];
  const maxRebateRate = maxRebateRateRes?.max_rebate_rate;
  const bonusMaxRebateRate =
    maxRebateRateRes?.bonus_max_rebate_rate ?? maxRebateRate ?? 0;
  const baseRebateRate = maxRebateRateRes?.base_rebate_rate ?? 0;

  const multiLevelRebateInfo = useMemo(() => {
    if (!multiLevelRebateInfoRes) return;

    const {
      default_referee_rebate_rate: referee_rebate_rate,
      default_bonus_referee_rebate_rate,
      bonus_max_rebate_rate,
      max_rebate_rate,
    } = multiLevelRebateInfoRes;

    const refereeBonusRebateRate =
      default_bonus_referee_rebate_rate ?? referee_rebate_rate ?? 0;
    const referrer_rebate_rate = new Decimal(
      bonus_max_rebate_rate ?? max_rebate_rate ?? 0,
    )
      .sub(refereeBonusRebateRate)
      .toNumber();

    return {
      ...multiLevelRebateInfoRes,
      referee_rebate_rate: refereeBonusRebateRate,
      referrer_rebate_rate,
    };
  }, [multiLevelRebateInfoRes]);

  const isLoading =
    volumePrerequisiteLoading ||
    maxRebateRateLoading ||
    multiLevelRebateInfoLoading ||
    refereeKickbackInfoLoading;

  return {
    volumePrerequisite,
    maxRebateRate,
    maxRebateRateInfo: maxRebateRateRes,
    bonusMaxRebateRate,
    baseRebateRate,
    multiLevelRebateInfo,
    refereeKickbackInfo,
    isMultiLevelEnabled,
    isMultiLevelReferralUnlocked,
    multiLevelRebateInfoMutate,
    maxRebateRateMutate,
    isLoading,
  };
};

export type MultiLevelReferralData = ReturnType<
  typeof useMultiLevelReferralData
>;
