import { useEffect, useMemo, useState } from "react";
import { DateRange } from "../../../utils/types";
import {
  useRefereeRebateSummary,
  RefferalAPI,
  useDaily,
} from "@paxeer-network/hooks";
import { compareDate, formatDateTimeToUTC } from "../../../utils/utils";
import { subDays, toDate } from "date-fns";
import { usePagination } from "@paxeer-network/ui";

export type RebatesItem = RefferalAPI.RefereeRebateSummary & {
  vol?: number;
};

export const useRebatesScript = () => {
  const [dateRange, setDateRange] = useState<DateRange | undefined>({
    from: subDays(new Date(), 90),
    to: subDays(new Date(), 1),
  });

  const {
    data: distributionData,
    mutate,
    isLoading,
  } = useRefereeRebateSummary({
    startDate: dateRange?.from,
    endDate: dateRange?.to,
  });
  // const { dailyVolume } = useReferralContext();

  const { data: dailyVolume, mutate: dailyVolumeMutate } = useDaily({
    startDate: dateRange?.to,
    endDate: dateRange?.from,
  });

  const dataSource = useMemo((): RebatesItem[] => {
    if (typeof distributionData === "undefined") return [];

    return (
      distributionData
        // .filter((item: any) => item.status === "COMPLETED" && item.type === "REFEREE_REBATE")
        .map((item) => {
          const createdTime = item.date;

          const volume = dailyVolume?.filter((item) => {
            return compareDate(toDate(createdTime), toDate(item.date));
          })?.[0];
          if (volume) {
            return { ...item, vol: volume.perp_volume };
          }

          return item;
        })
    );
  }, [distributionData, dailyVolume]);

  let displayDate = undefined;
  if ((dataSource?.length || 0) > 0) {
    displayDate = formatDateTimeToUTC(dataSource?.[0].date);
  }

  const { pagination, setPage } = usePagination();

  useEffect(() => {
    setPage(1);
  }, [dateRange]);

  return {
    dateRange,
    setDateRange,
    displayDate,
    dataSource,
    // meta,
    // onPageChange,
    // onPageSizeChange,
    pagination,
    isLoading,
  };
};

export type RebatesReturns = ReturnType<typeof useRebatesScript>;
