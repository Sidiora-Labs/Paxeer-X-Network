import { useMemo } from "react";
import { usePrivateQuery } from "../usePrivateQuery";
import { useQuery } from "../useQuery";
import { FlagKeys } from "./flagKeys";

/**
 * Feature flag item from API response
 */
export interface FeatureFlagItem {
  key: string;
  description: string;
}

/**
 * Return type for useFeatureFlag hook
 */
export interface UseFeatureFlagReturn {
  enabled: boolean;
  data: FeatureFlagItem | undefined;
}

/**
 * Hook to check if a feature flag is enabled
 *
 * Logic:
 * 1. Hidden by default - returns false when loading
 * 2. In public but not in private, hidden - returns { enabled: false, data: undefined }
 * 3. In both public and private, shown - returns { enabled: true, data: FeatureFlagItem }
 * 4. Not in public, shown - returns { enabled: true, data: undefined }
 *
 * @param key - The feature flag key to check
 * @returns { enabled: boolean, data: FeatureFlagItem | undefined }
 */
export const useFeatureFlag = (key: FlagKeys): UseFeatureFlagReturn => {
  // Always query public API
  const { data: publicFlags, isLoading: publicLoading } = useQuery<
    FeatureFlagItem[]
  >("/v1/public/feature_flags", {});

  // Find the key in public flags
  const publicFlag = useMemo(() => {
    if (!publicFlags || publicLoading) {
      return undefined;
    }
    return publicFlags.find((flag) => flag.key === key);
  }, [publicFlags, publicLoading, key]);

  // Only query private API if key is found in public API
  const shouldQueryPrivate = useMemo(() => {
    return publicFlag !== undefined;
  }, [publicFlag]);

  const { data: privateFlags, isLoading: privateLoading } = usePrivateQuery<
    FeatureFlagItem[]
  >(shouldQueryPrivate ? "/v1/feature_flags" : null, {});

  // Find the key in private flags
  const privateFlag = useMemo(() => {
    if (!shouldQueryPrivate || !privateFlags || privateLoading) {
      return undefined;
    }
    return privateFlags.find((flag) => flag.key === key);
  }, [shouldQueryPrivate, privateFlags, privateLoading, key]);

  // Return value based on the logic
  return useMemo(() => {
    // 1. Hidden by default - return false if still loading
    if (publicLoading || (shouldQueryPrivate && privateLoading)) {
      return {
        enabled: false,
        data: undefined,
      };
    }

    // 4. Not in public, shown
    if (publicFlag === undefined) {
      return {
        enabled: true,
        data: undefined,
      };
    }

    // 2. In public but not in private, hidden
    if (privateFlag === undefined) {
      return {
        enabled: false,
        data: undefined,
      };
    }

    // 3. In both public and private, shown
    return {
      enabled: true,
      data: privateFlag,
    };
  }, [
    publicFlag,
    privateFlag,
    publicLoading,
    shouldQueryPrivate,
    privateLoading,
    key,
  ]);
};
