import { Middleware, SWRHook } from "swr";
import { getGlobalObject } from "@paxeer-network/utils";

/**
 * SWR middleware for local time correction.
 * Automatically sets the timestamp offset by comparing server timestamp with local time
 * when the global offset is not yet initialized.
 */
export const localTimeCorrectionMiddleware: Middleware = (
  useSWRNext: SWRHook,
) => {
  return (key, fetcher, config) => {
    // Only wrap if fetcher exists
    if (!fetcher) {
      return useSWRNext(key, fetcher, config);
    }

    // Wrap the original fetcher to handle timestamp offset after successful response
    const wrappedFetcher = async (...args: unknown[]) => {
      try {
        // Call the original fetcher
        const result = await fetcher(...args);

        // Check if timestamp offset is already set
        const globalObj = getGlobalObject() as Record<string, unknown>;
        if (typeof globalObj.__PAXEER_NETWORK_timestamp_offset === "undefined") {
          // Check if the response contains a timestamp field
          if (
            result &&
            typeof result === "object" &&
            result !== null &&
            "timestamp" in result
          ) {
            const response = result as { timestamp?: unknown };
            if (typeof response.timestamp === "number") {
              const serverTimestamp = response.timestamp;
              const localTimestamp = Date.now();
              const diff = serverTimestamp - localTimestamp;

              // Only set if the difference is a valid number
              if (!isNaN(diff) && isFinite(diff)) {
                globalObj.__PAXEER_NETWORK_timestamp_offset = diff;
              }
            }
          }
        }

        return result;
      } catch (error) {
        // Re-throw the error to maintain SWR's error handling behavior
        throw error;
      }
    };

    return useSWRNext(key, wrappedFetcher, config);
  };
};
