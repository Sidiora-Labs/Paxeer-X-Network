import { useCallback, useEffect, useState } from "react";
import { getYieldBearingAsset } from "../../../constants/yieldBearingAssets";

export interface YieldAPYData {
  /** APY value in percentage (e.g., 8.5 for 8.5%) */
  apy: number | null;
  /** Whether the APY data is currently loading */
  loading: boolean;
  /** Error message if failed to fetch APY */
  error: string | null;
  /** External URL to the asset issuer's website */
  externalUrl: string | null;
}

/**
 * Hook to fetch and manage APY data for yield-bearing assets
 * @param symbol - Token symbol to fetch APY for
 * @returns APY data including value, loading state, and error
 */
export function useYieldAPY(symbol?: string): YieldAPYData {
  const [apy, setApy] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const assetConfig = getYieldBearingAsset(symbol);

  const fetchAPY = useCallback(async () => {
    if (!assetConfig) {
      setApy(null);
      setError(null);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 10000); // 10s timeout

      const response = await fetch(assetConfig.apyApiUrl, {
        signal: controller.signal,
        headers: {
          Accept: "application/json",
        },
      });

      clearTimeout(timeoutId);

      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      const data = await response.json();

      // Extract APY value using the path from config
      const apyValue = getNestedValue(data, assetConfig.apyPath);

      if (typeof apyValue === "number" && !isNaN(apyValue)) {
        setApy(apyValue);
        setError(null);
      } else {
        throw new Error("Invalid APY data format");
      }
    } catch (err) {
      console.error(`[useYieldAPY] Failed to fetch APY for ${symbol}:`, err);

      // CRITICAL FIX: Clear APY data when fetch fails
      // This prevents showing stale data from previous token
      setApy(null);

      if (err instanceof Error) {
        if (err.name === "AbortError") {
          setError("timeout");
        } else {
          setError(err.message);
        }
      } else {
        setError("unknown_error");
      }
    } finally {
      setLoading(false);
    }
  }, [symbol, assetConfig]);

  // Fetch APY when symbol changes
  useEffect(() => {
    // CRITICAL FIX: Clear APY immediately when symbol changes
    // This prevents showing stale data during the transition
    setApy(null);
    setError(null);

    if (assetConfig?.symbol === "deUSD") {
      fetchAPY();
    } else {
      setLoading(false);
    }
  }, [assetConfig, fetchAPY]);

  return {
    apy,
    loading,
    error,
    externalUrl: assetConfig?.externalUrl || null,
  };
}

/**
 * Helper function to extract nested value from object using dot notation path
 * e.g., "data.efficient_apr" => obj.data.efficient_apr
 */
function getNestedValue(obj: any, path: string): any {
  return path.split(".").reduce((current, key) => {
    return current?.[key];
  }, obj);
}
