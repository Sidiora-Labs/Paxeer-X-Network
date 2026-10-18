import { useCallback, useEffect, useMemo, useState } from "react";
import { differenceInDays } from "date-fns";
import { DateRange, LeaderboardTab } from "../../../type";
import {
  formatDateRange,
  getDateRange,
  splitCampaignByWeeks,
  WeeklyDateRange,
  getCurrentWeeklyRange,
  getCurrentOrAllTimeRange,
} from "../../../utils";

export type GeneralLeaderboardIScriptReturn = ReturnType<
  typeof useGeneralLeaderboardIScript
>;

export const FilterDays = [7, 14, 30, 90] as const;
export type TFilterDays = (typeof FilterDays)[number];

export type GeneralLeaderboardIScriptOptions = {
  campaignDateRange?: {
    start_time: Date | string;
    end_time: Date | string;
  };
  weekOneAddresses?: string[];
};

export function useGeneralLeaderboardIScript(
  options?: GeneralLeaderboardIScriptOptions,
) {
  const { campaignDateRange } = options || {};

  const weeklyRanges = useMemo(() => {
    if (!campaignDateRange) return [];
    return splitCampaignByWeeks(campaignDateRange);
  }, [campaignDateRange]);

  const currentOrAllTimeRange = useMemo(() => {
    return getCurrentOrAllTimeRange(weeklyRanges);
  }, [weeklyRanges]);

  const [activeTab, setActiveTab] = useState<LeaderboardTab>(
    LeaderboardTab.Volume,
  );
  const filterState = useFilter({ defaultRange: currentOrAllTimeRange });
  const searchState = useSearch();

  const useCampaignDateRange = useMemo(() => {
    return !!campaignDateRange;
  }, [campaignDateRange]);

  return {
    ...filterState,
    ...searchState,
    activeTab,
    onTabChange: setActiveTab,
    useCampaignDateRange,
    weeklyRanges,
    currentOrAllTimeRange,
    weekOneAddresses: options?.weekOneAddresses,
  };
}

function useFilter({ defaultRange }: { defaultRange?: DateRange }) {
  // default is 90d
  const [filterDay, setFilterDay] = useState<TFilterDays | null>(90);

  const [dateRange, setDateRange] = useState<DateRange>(getDateRange(90));

  const updateFilterDay = (day: TFilterDays) => {
    setFilterDay(day);
    setDateRange(getDateRange(day));
  };

  useEffect(() => {
    setDateRange(defaultRange ?? getDateRange(90));
  }, [defaultRange]);

  const onFilter = (filter: { name: string; value: any }) => {
    if (filter.name === "dateRange") {
      const newDateRange = filter.value;
      setDateRange(newDateRange);

      if (newDateRange.from && newDateRange.to) {
        const offsetDay =
          Math.abs(differenceInDays(newDateRange.from, newDateRange.to)) + 1;

        const dateRange = getDateRange(offsetDay);
        if (
          formatDateRange(dateRange.from) ===
            formatDateRange(newDateRange.from) &&
          formatDateRange(dateRange.to) === formatDateRange(newDateRange.to)
        ) {
          setFilterDay(offsetDay as any);
        } else {
          setFilterDay(null);
        }
      }
    }
  };

  const filterItems = useMemo(() => {
    const dateRangeFilter = {
      type: "range",
      name: "dateRange",
      value: dateRange,
      max: 90,
    };

    return [dateRangeFilter] as any;
  }, [dateRange]);

  return {
    filterItems,
    onFilter,
    dateRange,
    filterDay,
    updateFilterDay,
    setDateRange,
  };
}

function useSearch() {
  const [searchValue, setSearchValue] = useState("");
  const onSearchValueChange = useCallback((value: string) => {
    setSearchValue(value);
  }, []);

  const clearSearchValue = useCallback(() => {
    setSearchValue("");
  }, []);

  return {
    searchValue,
    onSearchValueChange,
    clearSearchValue,
  };
}

/**
 * Example function demonstrating how to use splitCampaignByWeeks
 * @param campaignDateRange Campaign date range with start_time and end_time
 * @returns Array of weekly date ranges plus "All time" option
 */
export function useCampaignWeeklyRanges(campaignDateRange: {
  start_time: Date | string;
  end_time: Date | string;
}): WeeklyDateRange[] {
  return useMemo(() => {
    return splitCampaignByWeeks(campaignDateRange);
  }, [campaignDateRange]);
}

/**
 * Hook to get current weekly range information
 * @param campaignDateRange Campaign date range with start_time and end_time
 * @returns Object containing current weekly range and related functions
 */
export function useCurrentWeeklyRange(campaignDateRange: {
  start_time: Date | string;
  end_time: Date | string;
}) {
  const weeklyRanges = useCampaignWeeklyRanges(campaignDateRange);

  const currentRange = useMemo(() => {
    return getCurrentWeeklyRange(weeklyRanges);
  }, [weeklyRanges]);

  const currentOrAllTime = useMemo(() => {
    return getCurrentOrAllTimeRange(weeklyRanges);
  }, [weeklyRanges]);

  const isInCurrentWeek = useMemo(() => {
    return currentRange !== null;
  }, [currentRange]);

  const getCurrentRangeForDate = useCallback(
    (date: Date) => {
      return getCurrentWeeklyRange(weeklyRanges, date);
    },
    [weeklyRanges],
  );

  return {
    weeklyRanges,
    currentRange,
    currentOrAllTime,
    isInCurrentWeek,
    getCurrentRangeForDate,
  };
}
