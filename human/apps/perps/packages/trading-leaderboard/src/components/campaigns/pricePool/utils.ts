export const isReachedVolumeLimit = (volume: number, volumeLimit: number) => {
  return volume >= volumeLimit;
};

/**
 * Get the highest tier index that the user has reached based on trading volume
 * @param tradingVolume Current trading volume
 * @param tieredPrizePools Array of prize pool tiers with volume limits
 * @returns Highest reached tier index (0-based), or -1 if invalid data or no tiers reached
 */
export const getCurrentTierIndex = (
  tradingVolume: number,
  tieredPrizePools: Array<Array<{ volume_limit?: number }>>,
): number => {
  if (!tieredPrizePools || tieredPrizePools.length === 0) {
    return -1;
  }

  // Extract volume limits and sort them in ascending order
  const volumeLimits = tieredPrizePools
    .map((tier) => tier[0]?.volume_limit || 0)
    .filter((limit) => limit >= 0)
    .sort((a, b) => a - b);

  if (volumeLimits.length === 0) {
    return -1;
  }

  // If trading volume is 0 or negative, no tiers reached
  // if (tradingVolume <= 0) {
  //   return -1;
  // }

  // Find the highest tier that the user has reached
  let highestReachedTier = -1;
  for (let i = 0; i < volumeLimits.length; i++) {
    if (tradingVolume >= volumeLimits[i]) {
      highestReachedTier = i;
    } else {
      break;
    }
  }

  return highestReachedTier;
};

/**
 * Calculate progress bar width percentage based on current trading volume and tiered volume limits
 * @param tradingVolume Current trading volume
 * @param tieredPrizePools Array of prize pool tiers with volume limits
 * @returns Width percentage (0-100)
 */
export const calculateProgressWidth = (
  tradingVolume: number,
  tieredPrizePools: Array<Array<{ volume_limit?: number }>>,
  offset: number = 0,
): number => {
  if (!tieredPrizePools || tieredPrizePools.length === 0) {
    return 0;
  }

  // Extract volume limits and sort them in ascending order
  const volumeLimits = tieredPrizePools
    .map((tier) => tier[0]?.volume_limit || 0)
    .filter((limit) => limit >= 0)
    .sort((a, b) => a - b);

  if (volumeLimits.length === 0) {
    return 0;
  }

  const firstSegmentWidth = getFirstSegmentWidth(volumeLimits.length);

  const segmentWidth = (100 - firstSegmentWidth) / (volumeLimits.length - 1);

  // Calculate the width of each segment (equally distributed)
  // const segmentWidth = 100 / volumeLimits.length;

  // If trading volume exceeds the highest limit, return 100%
  const maxLimit = volumeLimits[volumeLimits.length - 1];
  if (tradingVolume >= maxLimit) {
    return 100;
  }

  // Find which segment the current volume falls into (not necessarily reached)
  let segmentIndex = 0;
  for (let i = 0; i < volumeLimits.length; i++) {
    if (tradingVolume < volumeLimits[i]) {
      segmentIndex = i;
      break;
    }
  }

  // Calculate progress within the segment
  const segmentStart = segmentIndex === 0 ? 0 : volumeLimits[segmentIndex - 1];
  const segmentEnd = volumeLimits[segmentIndex];
  const segmentRange = segmentEnd - segmentStart;
  const volumeInSegment = tradingVolume - segmentStart;

  // Calculate progress percentage within current segment
  const progressInSegment =
    segmentRange > 0 ? volumeInSegment / segmentRange : 0;

  let totalWidth = firstSegmentWidth;
  for (let i = 1; i <= segmentIndex; i++) {
    if (i === segmentIndex) {
      const limitWidth = totalWidth + segmentWidth;
      const progressWidth = totalWidth + progressInSegment * segmentWidth;

      if (limitWidth - progressWidth < offset) {
        totalWidth = limitWidth - offset;
      } else {
        totalWidth = progressWidth;
      }
    } else {
      totalWidth += segmentWidth;
    }
  }

  return Math.min(Math.max(totalWidth, 0), 100);
};

const getFirstSegmentWidth = (totalTiers: number) => {
  return Math.floor(100 / (totalTiers * 2));
};

export const getProgressLeft = (totalTiers: number, index: number) => {
  const firstSegmentWidth = getFirstSegmentWidth(totalTiers);

  if (totalTiers === 1) {
    return `${firstSegmentWidth}%`;
  }

  const left =
    firstSegmentWidth + (index * (100 - firstSegmentWidth)) / (totalTiers - 1);
  return `${Math.min(left, 100)}%`;
};
