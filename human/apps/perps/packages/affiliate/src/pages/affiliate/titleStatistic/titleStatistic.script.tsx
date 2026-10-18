import { useMemo, useState } from "react";
import { fillData } from "../../../utils/chartUtils";
import { RefferalAPI, useReferralRebateSummary } from "@paxeer-network/hooks";
import { format, subDays } from "date-fns";
import { useTranslation } from "@paxeer-network/i18n";

export type TitleStatisticReturns = {
  period: string;
  periodTypes: { label: string; value: string }[];
  onPeriodChange: (item: string) => void;
  volType: string;
  volTypes: { label: string; value: string }[];
  onVolTypeChange: (item: string) => void;
  dataSource?: {
    date: string;
    volume: number;
  }[];
};

export const useTitleStatisticScript = (): TitleStatisticReturns => {
  const { t } = useTranslation();

  const [period, setPeriod] = useState("7");

  const periodTypes = [
    { label: t("common.select.7d"), value: "7" },
    { label: t("common.select.30d"), value: "30" },
    { label: t("common.select.90d"), value: "90" },
  ];

  const onPeriodChange = (item: string) => {
    setPeriod(item);
  };

  const [volType, setVolType] = useState("Commission");
  const volTypes = [
    { label: t("affiliate.commission"), value: "Commission" },
    { label: t("affiliate.referralVol"), value: "ref" },
  ];

  const onVolTypeChange = (item: string) => {
    setVolType(item);
  };

  const dateRange = useMemo((): {
    startDate: Date;
    endDate: Date;
  } => {
    if (period === "7") {
      return {
        startDate: subDays(new Date(), 7),
        endDate: subDays(new Date(), 1),
      };
    } else if (period === "30") {
      return {
        startDate: subDays(new Date(), 30),
        endDate: subDays(new Date(), 1),
      };
    } else if (period === "90") {
      return {
        startDate: subDays(new Date(), 90),
        endDate: subDays(new Date(), 1),
      };
    } else {
      return {
        startDate: subDays(new Date(), 7),
        endDate: subDays(new Date(), 1),
      };
    }
  }, [period]);

  const [rebateSummary] = useReferralRebateSummary({
    startDate: format(dateRange.startDate, "yyyy-MM-dd"),
    endDate: format(dateRange.endDate, "yyyy-MM-dd"),
    size: Number(period),
  });

  const dataSource = useMemo(() => {
    return (
      (rebateSummary as RefferalAPI.ReferralRebateSummary[] | null)?.map(
        (e) => ({
          date: e.date,
          volume: volType === "Commission" ? e.referral_rebate : e.volume,
        })
      ) || []
    ).reverse();
  }, [rebateSummary, volType]);

  return {
    period,
    periodTypes,
    onPeriodChange,
    volType,
    volTypes,
    onVolTypeChange,
    dataSource: fillData(Number(period), dataSource),
  };
};
