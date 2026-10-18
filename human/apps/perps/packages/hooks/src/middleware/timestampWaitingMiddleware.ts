import { Middleware, SWRHook } from "swr";
import { getGlobalObject } from "@paxeer-network/utils";

// Global state management to avoid duplicate polling
let timestampOffsetPromise: Promise<void> | null = null;
let timestampOffsetReady = false;

/**
 * SWR middleware that waits for __PAXEER_NETWORK_timestamp_offset to be initialized
 * before allowing requests to proceed.
 */
export const timestampWaitingMiddleware: Middleware = (useSWRNext: SWRHook) => {
  return (key, fetcher, config) => {
    // Only wrap if fetcher exists
    if (!fetcher) {
      return useSWRNext(key, fetcher, config);
    }

    // Wrap the original fetcher to wait for timestamp offset before making requests
    const wrappedFetcher = async (...args: unknown[]) => {
      // Performance optimization: Use shared waiting Promise to avoid duplicate polling
      await getTimestampOffsetPromise();

      // Call the original fetcher
      return fetcher(...args);
    };

    return useSWRNext(key, wrappedFetcher, config);
  };
};

/**
 * Get timestamp offset waiting Promise (singleton pattern)
 * Ensures only one polling instance is running
 */
function getTimestampOffsetPromise(): Promise<void> {
  // If already ready, return immediately
  if (timestampOffsetReady) {
    return Promise.resolve();
  }

  // If there's already a waiting Promise, reuse it
  if (timestampOffsetPromise) {
    return timestampOffsetPromise;
  }

  // Create new waiting Promise
  timestampOffsetPromise = waitForTimestampOffset();

  // Clean up state after completion
  timestampOffsetPromise
    .then(() => {
      timestampOffsetReady = true;
    })
    .catch(() => {
      // Reset Promise on failure to allow retry
      timestampOffsetPromise = null;
    });

  return timestampOffsetPromise;
}

/**
 * Helper function to wait for timestamp offset initialization
 * Optimized version: reduces unnecessary function calls and object creation
 */
async function waitForTimestampOffset(
  maxWaitTime: number = 30000,
): Promise<void> {
  // Cache global object reference to avoid repeated access
  let globalObj: Record<string, unknown>;

  try {
    globalObj = getGlobalObject() as Record<string, unknown>;
  } catch {
    throw new Error("Failed to access global object");
  }

  const checkTimestampOffset = () => {
    return typeof globalObj.__PAXEER_NETWORK_timestamp_offset === "number";
  };

  // Check immediately
  if (checkTimestampOffset()) {
    return;
  }

  return new Promise((resolve, reject) => {
    let attempts = 0;
    const maxAttempts = maxWaitTime / 100; // Reduce check frequency to 100ms to reduce CPU usage

    const pollInterval = setInterval(() => {
      attempts++;

      if (checkTimestampOffset()) {
        clearInterval(pollInterval);
        resolve();
      } else if (attempts >= maxAttempts) {
        clearInterval(pollInterval);
        console.warn(
          "Timeout waiting for __PAXEER_NETWORK_timestamp_offset initialization",
        );
        reject(
          new Error("Timeout waiting for timestamp offset initialization"),
        );
      }
    }, 100); // Increase interval to 100ms to balance responsiveness and performance
  });
}

/**
 * Reset timestamp state (for testing or re-initialization)
 */
export function resetTimestampOffsetState(): void {
  timestampOffsetPromise = null;
  timestampOffsetReady = false;
}
