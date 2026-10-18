import { useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Decimal } from "@paxeer-network/utils";
import { useReferralContext } from "../../../provider";
import { SummaryFilter } from "../../../utils/types";

export type SummaryReturns = {
  period: string;
  periodTypes: { label: string; value: string }[];
  onPeriodChange: (item: string) => void;
  rebates: number;
  vol?: number;
  code?: string;
  rebateText: string;
};

export const useSummaryScript = (): SummaryReturns => {
  const { t } = useTranslation();
  const [period, setPeriod] = useState<SummaryFilter>("All");

  const periodTypes: {
    label: SummaryFilter;
    value: SummaryFilter;
  }[] = [
    { label: t("common.all"), value: "All" },
    { label: t("common.select.1d"), value: "1D" },
    { label: t("common.select.7d"), value: "7D" },
    { label: t("common.select.30d"), value: "30D" },
  ];

  const { referralInfo, userVolume } = useReferralContext();

  const rebates = useMemo(() => {
    if (!referralInfo) return 0;
    //
    switch (period) {
      case "All":
        return referralInfo.referee_info.total_referee_rebate;
      case "1D":
        return referralInfo.referee_info["1d_referee_rebate"];
      case "7D":
        return referralInfo.referee_info["7d_referee_rebate"];
      case "30D":
        return referralInfo.referee_info["30d_referee_rebate"];
    }
  }, [referralInfo, period]);

  const vol = useMemo(() => {
    if (!userVolume) return undefined;
    switch (period) {
      case "All":
        return userVolume.all_volume;
      case "1D":
        return userVolume["1d_volume"];
      case "7D":
        return userVolume["7d_volume"];
      case "30D":
        return userVolume["30d_volume"];
    }
  }, [userVolume, period]);

  const code = referralInfo?.referee_info?.referer_code;
  const rebate = referralInfo?.referee_info?.referee_rebate_rate;

  const rebateText = useMemo(() => {
    if (!!rebate) {
      return (
        new Decimal(rebate)
          .mul(100)
          .toDecimalPlaces(2, Decimal.ROUND_DOWN)
          .toString() + "%"
      );
    }
    return "-";
  }, [rebate]);

  const onPeriodChange = (item: string) => {
    setPeriod(item as SummaryFilter);
  };
  return {
    period,
    periodTypes,
    onPeriodChange,
    rebates,
    vol,
    rebateText,
    code,
  };
};
