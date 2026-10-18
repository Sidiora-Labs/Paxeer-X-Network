import { format } from "date-fns";
import { i18n } from "@paxeer-network/i18n";
import { TimelinePoint } from "./components/axis";
import { CampaignConfig, CampaignTagEnum, CampaignStatistics } from "./type";

// Default timezone display for campaigns
const DEFAULT_TIMEZONE_DISPLAY = "UTC";

/**
 * Get user's current timezone display name
 * @returns Display name for the user's timezone (e.g., 'UTC', 'EST', 'PST')
 */
export const getUserTimezoneDisplay = (): string => {
  try {
    const date = new Date();
    const timeString = date.toLocaleTimeString("en-US", {
      timeZoneName: "short",
    });
    const shortName = timeString.split(" ").pop();
    return shortName || DEFAULT_TIMEZONE_DISPLAY;
  } catch (error) {
    console.warn("Failed to detect user timezone, using default:", error);
    return DEFAULT_TIMEZONE_DISPLAY;
  }
};

/**
 * Get the appropriate tag for a campaign based on its configuration and current conditions
 * @param campaign Campaign configuration object
 * @param userReferralCode User's referral code (optional)
 * @returns CampaignTagEnum representing the current status of the campaign
 */
export const getCampaignTag = (
  campaign: CampaignConfig,
  userReferralCode?: string,
): CampaignTagEnum => {
  const currentTime = new Date();
  const startTime = new Date(campaign.start_time);
  const endTime = new Date(campaign.end_time);

  // For non-exclusive campaigns, check time-based status
  if (currentTime < startTime) {
    return CampaignTagEnum.COMING;
  } else if (currentTime > endTime) {
    return CampaignTagEnum.ENDED;
  } else {
    if (campaign.referral_codes && campaign.referral_codes.length > 0) {
      if (
        !userReferralCode ||
        !campaign.referral_codes.includes(userReferralCode)
      ) {
        return CampaignTagEnum.EXCLUSIVE;
      }
    }
    return CampaignTagEnum.ONGOING;
  }
};

/**
 * Check if a campaign is visible to the current user
 * @param campaign Campaign configuration object
 * @param userReferralCode User's referral code (optional)
 * @returns boolean indicating if the campaign should be displayed
 */
export const isCampaignVisible = (
  campaign: CampaignConfig,
  userReferralCode?: string,
): boolean => {
  // If campaign has referral code restrictions
  if (campaign.referral_codes && campaign.referral_codes.length > 0) {
    // Only show if user has a valid referral code
    return (
      !!userReferralCode && campaign.referral_codes.includes(userReferralCode)
    );
  }

  // For non-exclusive campaigns, always visible
  return true;
};

/**
 * Format campaign date range to display format like "Feb 28, 2025 - March 2, 2025 PST"
 * @param startTime Start time string or timestamp (should be UTC)
 * @param endTime End time string or timestamp (should be UTC)
 * @param showTimezone Whether to show timezone suffix (defaults to true)
 * @returns Formatted date range string with user's timezone
 */
export const formatCampaignDateRange = (
  startTime: string | number | Date,
  endTime: string | number | Date,
  showTimezone: boolean = true,
): string => {
  try {
    // Convert to Date objects - JavaScript automatically handles UTC conversion to local time
    const startDate = new Date(startTime);
    const endDate = new Date(endTime);

    // Check if dates are valid
    if (isNaN(startDate.getTime()) || isNaN(endDate.getTime())) {
      console.warn("Invalid date value provided to formatCampaignDateRange:", {
        startTime,
        endTime,
      });
      return "Invalid date range";
    }

    // Format dates using date-fns (will display in user's local timezone)
    // MMM d, yyyy format gives us "Feb 28, 2025" style
    const startFormatted = format(startDate, "MMM d, yyyy");
    const endFormatted = format(endDate, "MMM d, yyyy");

    const dateRange = `${startFormatted} - ${endFormatted}`;

    if (!showTimezone) {
      return dateRange;
    }

    // Add user's timezone suffix
    const timezoneDisplay = getUserTimezoneDisplay();
    return `${dateRange} ${timezoneDisplay}`;
  } catch (error) {
    console.error("Error formatting campaign date range:", error, {
      startTime,
      endTime,
    });
    return "Date formatting error";
  }
};

/**
 * Get total participants count from campaign statistics
 * @param statistics Campaign statistics data from API
 * @returns Formatted participants count
 */
export const getParticipantsCount = (
  statistics?: CampaignStatistics,
): number => {
  return statistics?.total_participants || 0;
};

/**
 * Get total trading volume from campaign statistics
 * @param statistics Campaign statistics data from API
 * @returns Total trading volume amount
 */
export const getTradingVolume = (statistics?: CampaignStatistics): number => {
  return statistics?.total_volume || 0;
};

/**
 * Calculate total prize pool amount from campaign configuration
 * @param campaign Campaign configuration object
 * @returns Object with total amount and currency, or null if no prize pools
 */
export const getTotalPrizePool = (
  campaign?: CampaignConfig,
  tieredIndex?: number,
): { amount: number; currency: string } | null => {
  if (campaign?.tiered_prize_pools && campaign.tiered_prize_pools.length > 0) {
    return {
      amount: campaign.tiered_prize_pools[tieredIndex || 0]?.[0]?.total_prize,
      currency: campaign.tiered_prize_pools[tieredIndex || 0]?.[0]?.currency,
    };
  }

  if (!campaign?.prize_pools || campaign.prize_pools.length === 0) {
    return null;
  }

  // Group by currency and sum up the amounts
  const currencyTotals: Record<string, number> = {};

  campaign.prize_pools.forEach((pool) => {
    currencyTotals[pool.currency] =
      (currencyTotals[pool.currency] || 0) + pool.total_prize;
  });

  // For now, return the first currency found (typically USDC)
  // In the future, we might want to handle multiple currencies differently
  const currencies = Object.keys(currencyTotals);
  if (currencies.length === 0) return null;

  const mainCurrency = currencies[0];
  return {
    amount: currencyTotals[mainCurrency],
    currency: mainCurrency,
  };
};

/**
 * Get ticket prize pool amount from campaign configuration
 * @param campaign Campaign configuration object
 * @returns Object with amount and currency, or null if no ticket rules
 */
export const getTicketPrizePool = (
  campaign?: CampaignConfig,
): { amount: number; currency: string } | null => {
  if (!campaign?.ticket_rules) {
    return null;
  }

  return {
    amount: campaign.ticket_rules.total_prize,
    currency: campaign.ticket_rules.currency,
  };
};

/**
 * Format prize amount for display
 * @param amount Prize amount
 * @param currency Currency symbol
 * @returns Formatted string like "15,000 USDC"
 */
export const formatPrizeAmount = (amount: number, currency: string): string => {
  return `${amount?.toLocaleString()} ${currency}`;
};

/**
 * Format trading volume for display with currency symbol
 * @param volume Trading volume amount
 * @returns Formatted string with $ prefix
 */
export const formatTradingVolume = (
  volume: number,
  decimalPlaces: number = 1,
): string => {
  if (volume >= 1_000_000) {
    return `$${(volume / 1_000_000).toFixed(decimalPlaces)}M`;
  } else if (volume >= 1_000) {
    return `$${(volume / 1_000).toFixed(decimalPlaces)}K`;
  } else {
    return `$${volume.toFixed(decimalPlaces)}`;
  }
};

/**
 * Format participants count for display
 * @param count Participants count
 * @returns Formatted string with proper formatting
 */
export const formatParticipantsCount = (
  count: number,
  decimalPlaces: number = 1,
): string => {
  if (count >= 1_000_000) {
    return `${(count / 1_000_000).toFixed(decimalPlaces)}M`;
  } else if (count >= 1_000) {
    return `${(count / 1_000).toFixed(decimalPlaces)}K`;
  } else {
    return count.toLocaleString();
  }
};

/**
 * Generate timeline data points for campaign visualization
 * @param campaign Campaign configuration object
 * @returns Array of TimelinePoint objects (max 4 points) in user's local timezone
 */
export const generateCampaignTimeline = (
  campaign: CampaignConfig,
): TimelinePoint[] => {
  const currentTime = new Date();
  const registerTime = campaign.register_time
    ? new Date(campaign.register_time)
    : null;
  const startTime = new Date(campaign.start_time);
  const endTime = new Date(campaign.end_time);
  const rewardTime = campaign.reward_distribution_time
    ? new Date(campaign.reward_distribution_time)
    : null;

  const timeline: TimelinePoint[] = [];

  // Helper function to determine point type based on time
  const getTimelineType = (
    time: Date,
    isNow: boolean = false,
  ): "past" | "active" | "future" => {
    if (isNow) return "active";
    return currentTime >= time ? "past" : "future";
  };

  // Helper function to format time for display in user's local timezone
  const formatTimeDisplay = (time: Date): string => {
    try {
      // Format time using date-fns (displays in user's local timezone)
      const formatted = format(time, "yyyy-MM-dd HH:mm");
      const timezoneDisplay = getUserTimezoneDisplay();
      return `${formatted} ${timezoneDisplay}`;
    } catch (error) {
      console.error("Error formatting time:", error);
      return time.toISOString();
    }
  };

  if (registerTime) {
    timeline.push({
      title: "Battle Registration",
      type: getTimelineType(registerTime),
      time: formatTimeDisplay(registerTime),
    });

    const isInRegisterTime =
      currentTime >= registerTime && currentTime <= startTime;
    if (isInRegisterTime) {
      timeline.push({
        title: i18n.t("chart.now"),
        type: "active",
        time: formatTimeDisplay(currentTime),
      });
    }
  }

  // Battle starts point
  timeline.push({
    title: i18n.t("tradingLeaderboard.battleStarts"),
    type: getTimelineType(startTime),
    time: formatTimeDisplay(startTime),
  });

  // Add "Now" point if battle is ongoing
  const isOngoing = currentTime >= startTime && currentTime <= endTime;
  if (isOngoing) {
    timeline.push({
      title: i18n.t("chart.now"),
      type: "active",
      time: formatTimeDisplay(currentTime),
    });
  }

  // Battle ends point
  timeline.push({
    title: i18n.t("tradingLeaderboard.battleEnds"),
    type: getTimelineType(endTime),
    time: formatTimeDisplay(endTime),
  });

  // Reward distribution point (if provided)
  if (rewardTime) {
    timeline.push({
      title: i18n.t("tradingLeaderboard.rewardDistribution"),
      type: getTimelineType(rewardTime),
      time: formatTimeDisplay(rewardTime),
    });
  }

  // Ensure we don't exceed 4 points
  return timeline.slice(0, 4);
};
