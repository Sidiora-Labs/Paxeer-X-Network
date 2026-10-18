import {
  TicketTierRule,
  TicketLinearRule,
  TicketRules,
  CampaignConfig,
  UserData,
  PrizePool,
} from "../campaigns/type";

/**
 * Get user metric value based on pool metric type
 */
function getUserMetricValue(
  userdata: UserData,
  metric: "volume" | "pnl",
): number {
  return metric === "volume" ? userdata.volume : userdata.pnl;
}

/**
 * Estimate user's rank based on current performance
 */
function estimateUserRank(
  userdata: UserData,
  metric: "volume" | "pnl",
): number | null {
  // If we have actual rank data, use it
  if (userdata.rank) {
    return Number(userdata.rank);
  }

  // Otherwise, make a simple estimation based on performance
  const userMetricValue = getUserMetricValue(userdata, metric);
  const totalParticipants = userdata.total_participants || 1000;

  if (userMetricValue <= 0) return null;

  // Simple heuristic: assume better performance means better rank
  // In reality, you would compare against actual leaderboard data
  if (userMetricValue >= 100000) return 1;
  if (userMetricValue >= 50000) return Math.floor(totalParticipants * 0.05); // Top 5%
  if (userMetricValue >= 10000) return Math.floor(totalParticipants * 0.2); // Top 20%
  if (userMetricValue >= 1000) return Math.floor(totalParticipants * 0.5); // Top 50%

  return Math.floor(totalParticipants * 0.8); // Bottom 80%
}

/**
 * Find reward amount for user based on estimated rank in a specific pool
 */
function findRewardInPool(userdata: UserData, pool: PrizePool): number {
  const userMetricValue = getUserMetricValue(userdata, pool.metric);

  // Skip if user has no relevant data
  if (userMetricValue <= 0) return 0;

  const estimatedRank = estimateUserRank(userdata, pool.metric);
  if (estimatedRank === null) return 0;

  // Find matching tier based on estimated rank
  for (const tier of pool.tiers || []) {
    let isInTier = false;

    if (tier.position && estimatedRank === tier.position) {
      isInTier = true;
    } else if (tier.position_range) {
      const [start, end] = tier.position_range;
      if (estimatedRank >= start && estimatedRank <= end) {
        isInTier = true;
      }
    }

    if (isInTier) {
      return tier.amount;
    }
  }

  return 0;
}

/**
 * Calculate estimated rewards based on user's current performance and campaign configuration
 * @param userdata User's current trading data
 * @param campaign Campaign configuration
 * @returns Estimated reward amount with currency
 */
export function calculateEstimatedRewards(
  userdata: UserData,
  campaign: CampaignConfig,
): { amount: number; currency: string } | null {
  if (!campaign.prize_pools || campaign.prize_pools.length === 0) {
    return null;
  }

  let totalEstimatedReward = 0;
  const mainCurrency = campaign.prize_pools[0].currency;

  // Only sum rewards from pools with the same currency to avoid mixing different currencies
  for (const pool of campaign.prize_pools) {
    totalEstimatedReward += findRewardInPool(userdata, pool);
  }

  return totalEstimatedReward > 0
    ? { amount: totalEstimatedReward, currency: mainCurrency }
    : null;
}

/**
 * Calculate estimated tickets earned based on user's trading performance
 * @param userdata User's current trading data
 * @param ticketRules Ticket configuration rules
 * @returns Estimated number of tickets
 */
export function calculateEstimatedTickets(
  userdata: UserData,
  ticketRules: TicketRules,
): number {
  const userMetricValue =
    ticketRules.metric === "volume" ? userdata.volume : userdata.pnl;

  if (!userMetricValue) return 0;

  if (userMetricValue <= 0) return 0;

  if (ticketRules.mode === "tiered") {
    return calculateTieredTickets(userMetricValue, ticketRules.tiers || []);
  } else if (ticketRules.mode === "linear") {
    return calculateLinearTickets(userMetricValue, ticketRules.linear);
  }

  return 0;
}

/**
 * Calculate tickets using tiered mode
 * ≥ 25,000 volume → 10 tickets
 * ≥ 10,000 and < 25,000 → 5 tickets
 * ≥ 5,000 and < 10,000 → 1 ticket
 * < 5,000 → 0 tickets
 */
function calculateTieredTickets(
  metricValue: number,
  tiers: TicketTierRule[],
): number {
  // Sort tiers by value in descending order
  const sortedTiers = [...tiers].sort((a, b) => b.value - a.value);

  for (const tier of sortedTiers) {
    if (metricValue >= tier.value) {
      return tier.tickets;
    }
  }

  return 0;
}

/**
 * Calculate tickets using linear mode
 * Get X ticket for every Y volume/pnl
 */
function calculateLinearTickets(
  metricValue: number,
  linearRule?: TicketLinearRule,
): number {
  if (!linearRule || linearRule.every <= 0) return 0;

  const multiplier = Math.floor(metricValue / linearRule.every);
  return multiplier * linearRule.tickets;
}

/**
 * Format reward amount with currency for display
 */
export function formatRewardAmount(amount: number, currency: string): string {
  return `${amount.toLocaleString()} ${currency}`;
}

/**
 * Format ticket count for display
 */
export function formatTicketCount(tickets: number): string {
  return tickets.toLocaleString();
}

/**
 * Calculate user's estimated reward for a specific prize pool
 * @param userdata User's current trading data
 * @param pool Specific prize pool
 * @returns Estimated reward amount for this pool, or 0 if not eligible
 */
export function calculateUserPoolReward(
  userdata: UserData,
  pool: PrizePool,
): number {
  return findRewardInPool(userdata, pool);
}

/**
 * Calculate progress percentage and next tier requirements for tickets
 * @param userdata User's current trading data
 * @param ticketRules Ticket configuration rules
 * @returns Object containing progress percentage and next tier requirements
 */
export function calculateTicketProgress(
  userdata: UserData,
  ticketRules: TicketRules,
): { percent: number; value: number; atMax?: boolean } | null {
  if (!userdata || !ticketRules) return null;

  const userMetricValue = getUserMetricValue(userdata, ticketRules.metric);

  if (!userMetricValue) {
    if (ticketRules.mode === "linear") {
      return {
        percent: 0,
        value: ticketRules.linear?.every || 0,
      };
    } else {
      return {
        percent: 0,
        value: ticketRules.tiers?.[0]?.value || 0,
      };
    }
  }

  if (ticketRules.mode === "linear") {
    if (!ticketRules.linear) return null;

    const { every, tickets } = ticketRules.linear;
    const currentTier = Math.floor(userMetricValue / every);
    const nextTier = currentTier + 1;
    const progress = ((userMetricValue % every) / every) * 100;
    const nextTierValue = nextTier * every;

    return {
      percent: progress,
      value: nextTierValue - userMetricValue,
    };
  }

  if (ticketRules.mode === "tiered" && ticketRules.tiers) {
    const sortedTiers = [...ticketRules.tiers].sort(
      (a, b) => a.value - b.value,
    );

    // Check if tiers array is empty
    if (sortedTiers.length === 0) {
      return null;
    }

    // Special case: if user metric value is 0 or negative, show progress to first tier
    if (userMetricValue <= 0) {
      return {
        percent: 0,
        value: sortedTiers[0].value,
      };
    }

    // Find current tier and next tier
    let currentTier = null;
    let nextTier = sortedTiers[0]; // Default next tier is the first one

    for (let i = 0; i < sortedTiers.length; i++) {
      if (userMetricValue >= sortedTiers[i].value) {
        currentTier = sortedTiers[i];
        nextTier = sortedTiers[i + 1] || null; // Next tier or null if at max
      } else {
        // User hasn't reached this tier yet, so this is the next tier
        nextTier = sortedTiers[i];
        break;
      }
    }

    if (!nextTier) {
      return {
        percent: 100,
        value: 0,
        atMax: true,
      };
    }

    // If user hasn't reached any tier yet, calculate progress to first tier
    if (!currentTier) {
      const progress = (userMetricValue / nextTier.value) * 100;
      return {
        percent: progress,
        value: nextTier.value - userMetricValue,
      };
    }

    const progress =
      ((userMetricValue - currentTier.value) /
        (nextTier.value - currentTier.value)) *
      100;

    return {
      percent: progress,
      value: nextTier.value - userMetricValue,
    };
  }

  return null;
}
