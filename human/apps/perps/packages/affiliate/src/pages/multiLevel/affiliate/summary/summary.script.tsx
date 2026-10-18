import { useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { useMultiLevelStatistics } from "../../../../hooks/useReferralApi";
import { useReferralContext } from "../../../../provider";
import { StatisticsTimeRange } from "../../../../types";

export type SummaryReturns = ReturnType<typeof useSummaryScript>;

export const useSummaryScript = () => {
  const { t } = useTranslation();
  const { referralInfo } = useReferralContext();
  const [period, setPeriod] = useState(StatisticsTimeRange.All);

  const { data } = useMultiLevelStatistics(period);

  const referredByCode = useMemo(() => {
    const code = referralInfo?.referee_info?.referer_code?.trim();
    return code || undefined;
  }, [referralInfo]);

  const periodTypes: { label: string; value: StatisticsTimeRange }[] = [
    { label: t("common.all"), value: StatisticsTimeRange.All },
    { label: t("common.select.1d"), value: StatisticsTimeRange["1d"] },
    { label: t("common.select.7d"), value: StatisticsTimeRange["7d"] },
    { label: t("common.select.30d"), value: StatisticsTimeRange["30d"] },
  ];

  const onPeriodChange = (item: string) => {
    setPeriod(item as StatisticsTimeRange);
  };

  const statistics = useMemo(() => {
    const {
      direct_invites = 0,
      indirect_invites = 0,
      direct_traded = 0,
      indirect_traded = 0,
      direct_volume = 0,
      indirect_volume = 0,
      direct_rebate = 0,
      indirect_rebate = 0,
      direct_bonus_rebate = 0,
    } = data || {};
    return {
      direct_invites,
      indirect_invites,
      total_invites: direct_invites + indirect_invites,
      direct_traded,
      indirect_traded,
      total_traded: direct_traded + indirect_traded,
      direct_volume,
      indirect_volume,
      total_volume: direct_volume + indirect_volume,
      direct_rebate,
      indirect_rebate,
      direct_bonus_rebate,
      total_rebate: direct_rebate + indirect_rebate + direct_bonus_rebate,
    };
  }, [data]);

  return {
    period,
    periodTypes,
    onPeriodChange,
    statistics,
    referredByCode,
  };
};
