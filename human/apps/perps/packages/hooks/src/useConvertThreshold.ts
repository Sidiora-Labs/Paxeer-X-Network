import type { API } from "@paxeer-network/types";
import { useQuery } from "./useQuery";

/**
 * Fetches the auto-convert thresholds from `/v1/public/auto_convert_threshold`.
 *
 * Fields are returned raw; `ltv_threshold` is a ratio (e.g. `0.9` means 90%),
 * multiply by 100 at the consumer when a percentage is needed. While loading
 * or on request failure the fields are `undefined`.
 */
export const useConvertThreshold = () => {
  const { data, error, isLoading } = useQuery<API.ConvertThreshold>(
    "/v1/public/auto_convert_threshold",
    { errorRetryCount: 3, revalidateOnFocus: false },
  );

  return {
    ltv_threshold: data?.ltv_threshold,
    negative_usdc_threshold: data?.negative_usdc_threshold,
    isLoading,
    error,
  } as const;
};

export type ConvertThresholdReturns = ReturnType<typeof useConvertThreshold>;
