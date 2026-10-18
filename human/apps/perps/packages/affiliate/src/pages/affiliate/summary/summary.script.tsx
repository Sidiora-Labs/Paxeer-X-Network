import { useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { useReferralContext } from "../../../provider";
import { SummaryFilter } from "../../../utils/types";

export type SummaryReturns = {
  period: string;
  periodTypes: { label: string; value: string }[];
  onPeriodChange: (item: string) => void;
  commission: number;
  referralVol: number;
  referees: number;
  refereesTades: number;
};

export const useSummaryScript = (): SummaryReturns => {
  const { t } = useTranslation();
  const [period, setPeriod] = useState<SummaryFilter>("All");

  const periodTypes: { label: string; value: SummaryFilter }[] = [
    { label: t("common.all"), value: "All" },
    { label: t("common.select.1d"), value: "1D" },
    { label: t("common.select.7d"), value: "7D" },
    { label: t("common.select.30d"), value: "30D" },
  ];

  const { referralInfo } = useReferralContext();

  const commission = useMemo(() => {
    if (!referralInfo || !referralInfo.referrer_info) {
      return 0;
    }
    switch (period) {
      case "All":
        return referralInfo.referrer_info.total_referrer_rebate;
      case "1D":
        return referralInfo.referrer_info["1d_referrer_rebate"];
      case "7D":
        return referralInfo.referrer_info["7d_referrer_rebate"];
      case "30D":
        return referralInfo.referrer_info["30d_referrer_rebate"];
    }
  }, [referralInfo, period]);

  const referralVol = useMemo(() => {
    if (!referralInfo || !referralInfo.referrer_info) {
      return 0;
    }
    switch (period) {
      case "All":
        return referralInfo.referrer_info.total_referee_volume;
      case "1D":
        return referralInfo.referrer_info["1d_referee_volume"];
      case "7D":
        return referralInfo.referrer_info["7d_referee_volume"];
      case "30D":
        return referralInfo.referrer_info["30d_referee_volume"];
    }
  }, [referralInfo, period]);

  const referees = useMemo(() => {
    if (!referralInfo || !referralInfo.referrer_info) {
      return 0;
    }
    switch (period) {
      case "All":
        return referralInfo.referrer_info.total_invites;
      case "1D":
        return referralInfo.referrer_info["1d_invites"];
      case "7D":
        return referralInfo.referrer_info["7d_invites"];
      case "30D":
        return referralInfo.referrer_info["30d_invites"];
    }
  }, [referralInfo, period]);

  const refereesTades = useMemo(() => {
    if (!referralInfo || !referralInfo.referrer_info) {
      return 0;
    }
    switch (period) {
      case "All":
        return referralInfo.referrer_info.total_traded;
      case "1D":
        return referralInfo.referrer_info["1d_traded"];
      case "7D":
        return referralInfo.referrer_info["7d_traded"];
      case "30D":
        return referralInfo.referrer_info["30d_traded"];
    }
  }, [referralInfo, period]);

  const onPeriodChange = (item: string) => {
    setPeriod(item as SummaryFilter);
  };

  return {
    period,
    periodTypes,
    onPeriodChange,
    commission,
    referralVol,
    referees,
    refereesTades,
  };
};
