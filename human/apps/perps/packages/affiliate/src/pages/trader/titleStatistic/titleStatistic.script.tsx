import { useMemo, useState } from "react";
import { format, subDays } from "date-fns";
import { VolChartDataItem } from "@paxeer-network/chart";
import { useRefereeRebateSummary } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useReferralContext } from "../../../provider";
import { fillData } from "../../../utils/chartUtils";
import { BarDayFilter } from "../../../utils/types";

export type TitleStatisticReturns = {
  period: string;
  periodTypes: { label: string; value: string }[];
  onPeriodChange: (item: string) => void;
  volType: string;
  volTypes: { label: string; value: string }[];
  onVolTypeChange: (item: string) => void;
  dataSource: VolChartDataItem[];
};

export const useTitleStatisticScript = (): TitleStatisticReturns => {
  const { t } = useTranslation();
  const [period, setPeriod] = useState<BarDayFilter>("7");

  const periodTypes = [
    { label: t("common.select.7d"), value: "7" },
    { label: t("common.select.30d"), value: "30" },
    { label: t("common.select.90d"), value: "90" },
  ];

  const onPeriodChange = (item: string) => {
    setPeriod(item as BarDayFilter);
  };

  const [volType, setVolType] = useState("rebate");
  const volTypes = [
    { label: t("affiliate.trader.rebate"), value: "rebate" },
    { label: t("common.volume"), value: "volume" },
  ];

  const onVolTypeChange = (item: string) => {
    setVolType(item);
  };

  const dateRange = useMemo((): {
    startDate?: Date;
    endDate?: Date;
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

  const { data: distributionData, mutate } = useRefereeRebateSummary(dateRange);
  const { dailyVolume, chartConfig } = useReferralContext();

  const dataSource = useMemo(() => {
    if (volType === "rebate") {
      const newData = distributionData || [];
      return newData
        .map((e) => ({
          date: e.date,
          volume: e.referee_rebate,
        }))
        .reverse();
    } else if (volType === "volume") {
      return (
        dailyVolume
          ?.filter((e) => {
            return (
              e.date > format(subDays(Date(), Number(period) + 1), "yyyy-MM-dd")
            );
          })
          .map((e) => ({
            date: e.date,
            volume: e.perp_volume,
          })) || []
      );
    } else {
      return [];
    }
  }, [distributionData, dailyVolume, volType]);

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
