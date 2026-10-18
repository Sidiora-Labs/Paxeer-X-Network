import { usePrivateQuery } from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { StatisticsTimeRange } from "../types";

/** Get Volume Prerequisite */
export const useVolumePrerequisite = () => {
  return usePrivateQuery<API.Referral.VolumePrerequisite>(
    "/v1/referral/multi_level/volume_prerequisite",
    {
      revalidateOnFocus: false,
      shouldRetryOnError: false,
    },
  );
};

/** Get Max Rebate Rate */
export const useMaxRebateRate = () => {
  return usePrivateQuery<API.Referral.MaxRebateRate>(
    "/v1/referral/multi_level/max_rebate_rate",
    {
      revalidateOnFocus: false,
      shouldRetryOnError: false,
    },
  );
};

/** Get the trader's multi-level fee kickback rate. The endpoint wraps the payload in `rows`, which the fetcher unwraps into an array. */
export const useRefereeKickbackInfo = (enabled: boolean) => {
  return usePrivateQuery<API.Referral.RefereeKickbackInfo[]>(
    enabled ? "/v1/referral/multi_level/referee_kickback_info" : null,
    {
      revalidateOnFocus: false,
      shouldRetryOnError: false,
    },
  );
};

/** Generate Referral Code */
export const useMultiLevelRebateInfo = () => {
  return usePrivateQuery<API.Referral.MultiLevelRebateInfo>(
    "/v1/referral/multi_level/rebate_info",
    {
      revalidateOnFocus: false,
      shouldRetryOnError: false,
    },
  );
};

/** Get Multi-Level Statistics */
export const useMultiLevelStatistics = (time_range: StatisticsTimeRange) => {
  return usePrivateQuery<API.Referral.MultiLevelStatistics>(
    `/v1/referral/multi_level/statistics?time_range=${time_range}`,
    {
      revalidateOnFocus: false,
      shouldRetryOnError: false,
    },
  );
};
