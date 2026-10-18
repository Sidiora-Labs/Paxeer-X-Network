import { useDistributionHistory } from "@paxeer-network/hooks";
import { subtractDaysFromCurrentDate } from "@paxeer-network/utils";
import { useMemo, useState } from "react";
import { usePagination } from "@paxeer-network/ui";
import { parseDateRangeForFilter } from "../helper/date";
import { getDate, getMonth, getYear, set } from "date-fns";

export const useDistributionHistoryHook = () => {
  // const today = useRef(setMinutes(setHours(new Date(), 23), 59));

  const [today] = useState(() => {
    const d = new Date();

    return new Date(getYear(d), getMonth(d), getDate(d), 0, 0, 0);
  });

  const [dateRange, setDateRange] = useState<Date[]>([
    subtractDaysFromCurrentDate(90, today),
    today,
  ]);
  const [type, setType] = useState<string>("All");
  const { page, pageSize, setPage, parsePagination } = usePagination();

  const [data, { isLoading, meta, isValidating }] = useDistributionHistory({
    // dataRange: dateRange.map((date) => date.getTime()),
    dataRange: [
      dateRange[0].getTime(),
      set(dateRange[1], {
        hours: 23,
        minutes: 59,
        seconds: 59,
        milliseconds: 0,
      }).getTime(),
    ],
    type,
    pageSize,
    page,
  });

  // console.log("----", isLoading, isValidating);

  // const res = useQuery("v1/public/info/funding_period");

  const onFilter = (filter: { name: string; value: any }) => {
    if (filter.name === "type") {
      setType(filter.value);
      setPage(1);
    }

    if (filter.name === "dateRange") {
      // setDateRange([filter.value.from, filter.value.to]);
      setDateRange(parseDateRangeForFilter(filter.value));
      setPage(1);
    }
  };

  const pagination = useMemo(
    () => parsePagination(meta),
    [parsePagination, meta]
  );

  return {
    dataSource: data,
    isLoading,
    isValidating,
    // onDateRangeChange,
    queryParameter: {
      type,
      dateRange,
    },
    onFilter,
    pagination,
  } as const;
};

export type useDistributionHistoryHookReturn = ReturnType<
  typeof useDistributionHistoryHook
>;
