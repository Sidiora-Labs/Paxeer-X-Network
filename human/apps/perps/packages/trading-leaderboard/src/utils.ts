import {
  format,
  subDays,
  addDays,
  isBefore,
  isAfter,
  isWithinInterval,
} from "date-fns";

export const getDateRange = (offsetDay: number) => {
  return {
    from: subDays(new Date(), offsetDay - 1)!,
    to: new Date()!,
  };
};

/**
 * Format a date to "yyyy-MM-dd" format (e.g., "2025-03-10")
 * @param date The date to format
 * @returns Formatted date string
 */
export const formatDateRange = (date: Date): string => {
  return format(date, "yyyy-MM-dd");
};

function getUTCDateInfo(date: Date) {
  const year = date.getUTCFullYear();
  const month = date.getUTCMonth();
  const day = date.getUTCDate();
  const hours = String(date.getUTCHours()).padStart(2, "0");
  const minutes = String(date.getUTCMinutes()).padStart(2, "0");

  return { year, month, day, hours, minutes };
}

export function formatCampaignDate(date: Date | string): string {
  const monthNames = [
    "Jan",
    "Feb",
    "Mar",
    "Apr",
    "May",
    "Jun",
    "Jul",
    "Aug",
    "Sep",
    "Oct",
    "Nov",
    "Dec",
  ];
  if (typeof date === "string") {
    date = new Date(date);
  }
  const { year, month, day, hours, minutes } = getUTCDateInfo(date);
  return `${monthNames[month]} ${day}, ${year} ${hours}:${minutes}`;
}

export function formatUpdateDate(timestamp: number) {
  const time = new Date(timestamp);
  try {
    return format(time, "yyyy-MM-dd HH:mm");
  } catch (error) {
    console.error("Error formatting time:", error);
    return "";
  }
}

export type WeeklyDateRange = {
  from: Date;
  to: Date;
  label: string;
};

/**
 * Split campaign date range into weekly periods and add an "All time" option
 * @param campaignDateRange Object containing start_time and end_time
 * @returns Array of date ranges with weekly splits plus "All time" option
 */
export function splitCampaignByWeeks(campaignDateRange: {
  start_time: Date | string;
  end_time: Date | string;
}): WeeklyDateRange[] {
  const startDate =
    typeof campaignDateRange.start_time === "string"
      ? new Date(campaignDateRange.start_time)
      : campaignDateRange.start_time;

  const endDate =
    typeof campaignDateRange.end_time === "string"
      ? new Date(campaignDateRange.end_time)
      : campaignDateRange.end_time;

  const result: WeeklyDateRange[] = [];
  let currentWeekStart = new Date(startDate);
  let weekNumber = 1;

  while (isBefore(currentWeekStart, endDate)) {
    // Calculate the end of current week (6 days after start)
    const currentWeekEnd = addDays(currentWeekStart, 6);

    // If this week extends beyond the campaign end date, use the campaign end date
    // But subtract 1 millisecond to ensure we don't include the end date itself
    const actualWeekEnd = isAfter(currentWeekEnd, endDate)
      ? new Date(endDate.getTime() - 1)
      : currentWeekEnd;

    result.push({
      from: new Date(currentWeekStart),
      to: actualWeekEnd,
      label: `Week ${weekNumber}`,
    });

    // If the current week already reached the end date, break
    if (!isBefore(actualWeekEnd, endDate)) {
      break;
    }

    // Move to next week
    if (weekNumber === 1) {
      // For week 2 and onwards, start from 00:00:00 UTC of the next day
      const nextDayStart = addDays(currentWeekEnd, 1);
      const year = nextDayStart.getUTCFullYear();
      const month = nextDayStart.getUTCMonth();
      const date = nextDayStart.getUTCDate();
      currentWeekStart = new Date(Date.UTC(year, month, date, 0, 0, 0, 0));
    } else {
      // For subsequent weeks, continue the same pattern
      const nextDayStart = addDays(currentWeekEnd, 1);
      const year = nextDayStart.getUTCFullYear();
      const month = nextDayStart.getUTCMonth();
      const date = nextDayStart.getUTCDate();
      currentWeekStart = new Date(Date.UTC(year, month, date, 0, 0, 0, 0));
    }

    weekNumber++;

    // Safety check to prevent infinite loop
    if (weekNumber > 100) {
      console.warn(
        "Too many weeks detected, breaking loop to prevent infinite iteration",
      );
      break;
    }
  }

  // Add "All time" option covering the entire campaign period
  // result.push({
  //   from: new Date(startDate),
  //   to: new Date(endDate),
  //   label: "All time",
  // });

  return result;
}

/**
 * Find the current weekly range that contains the given date (or current time)
 * @param weeklyRanges Array of weekly date ranges
 * @param targetDate The date to check (defaults to current time)
 * @returns The weekly range that contains the target date, or null if not found
 */
export function getCurrentWeeklyRange(
  weeklyRanges: WeeklyDateRange[],
  targetDate: Date = new Date(),
): WeeklyDateRange | null {
  // Exclude "All time" range from the search since it's not a specific weekly period
  const weeklyOnlyRanges = weeklyRanges.filter(
    (range) => range.label !== "All time",
  );

  // First try exact match
  for (const range of weeklyOnlyRanges) {
    // Ensure range dates are properly converted to Date objects
    const rangeFrom = new Date(range.from);
    const rangeTo = new Date(range.to);

    // Check if target date is within the range (inclusive of both start and end)
    if (isWithinInterval(targetDate, { start: rangeFrom, end: rangeTo })) {
      return range;
    }
  }

  // If no exact match found, find the closest upcoming range
  // This handles cases where there are gaps between weekly periods
  const targetTime = targetDate.getTime();
  let closestFutureRange: WeeklyDateRange | null = null;
  let smallestGap = Infinity;

  for (const range of weeklyOnlyRanges) {
    const rangeFrom = new Date(range.from);
    const rangeFromTime = rangeFrom.getTime();

    // If the range starts after the target date, consider it as a candidate
    if (rangeFromTime > targetTime) {
      const gap = rangeFromTime - targetTime;
      if (gap < smallestGap) {
        smallestGap = gap;
        closestFutureRange = range;
      }
    }
  }

  // If there's a close upcoming range (within 24 hours), return it
  const ONE_DAY_MS = 24 * 60 * 60 * 1000;
  if (closestFutureRange && smallestGap <= ONE_DAY_MS) {
    return closestFutureRange;
  }

  return null;
}

/**
 * Get the current active weekly range or fallback to "All time"
 * @param weeklyRanges Array of weekly date ranges
 * @param targetDate The date to check (defaults to current time)
 * @returns The current weekly range or "All time" range as fallback
 */
export function getCurrentOrAllTimeRange(
  weeklyRanges: WeeklyDateRange[],
  targetDate: Date = new Date(),
): WeeklyDateRange | undefined {
  // First try to find current weekly range
  const currentWeekly = getCurrentWeeklyRange(weeklyRanges, targetDate);
  if (currentWeekly) {
    return currentWeekly;
  }

  // If not in any weekly range, return "All time" range
  return (
    weeklyRanges.find((range) => range.label === "All time") ||
    weeklyRanges[weeklyRanges.length - 1]
  );
}
