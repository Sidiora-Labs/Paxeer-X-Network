import { useMemo, useState } from "react";
import { format, subDays } from "date-fns";
import { RefferalAPI, useReferralRebateSummary } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { fillData } from "../../../../utils/chartUtils";

export type TitleStatisticReturns = {
  period: string;
  periodTypes: { label: string; value: string }[];
  onPeriodChange: (item: string) => void;
  dataSource?: {
    date: string;
    volume: number;
  }[];
};

export const useCommissionChartScript = (): TitleStatisticReturns => {
  const { t } = useTranslation();

  const [period, setPeriod] = useState("90");

  const periodTypes = useMemo(() => {
    return [
      { label: t("common.select.7d"), value: "7" },
      { label: t("common.select.30d"), value: "30" },
      { label: t("common.select.90d"), value: "90" },
    ];
  }, [t]);

  const onPeriodChange = (item: string) => {
    setPeriod(item);
  };

  const dateRange = useMemo((): {
    startDate: Date;
    endDate: Date;
  } => {
    const days = Number(period) || 7;
    const now = new Date();
    return {
      startDate: subDays(now, days),
      endDate: subDays(now, 1),
    };
  }, [period]);

  const [rebateSummary] = useReferralRebateSummary({
    startDate: format(dateRange.startDate, "yyyy-MM-dd"),
    endDate: format(dateRange.endDate, "yyyy-MM-dd"),
    size: Number(period),
  });

  const dataSource = useMemo(() => {
    const data =
      ((rebateSummary as RefferalAPI.ReferralRebateSummary[]) || [])?.map(
        (item) => ({
          date: item.date,
          volume:
            item.direct_rebate +
            item.indirect_rebate +
            item.direct_bonus_rebate,
        }),
      ) || [];
    data.reverse();

    return fillData(Number(period), data);
  }, [rebateSummary, period]);

  return {
    period,
    periodTypes,
    onPeriodChange,
    dataSource,
  };
};
