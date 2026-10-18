import { useConvertThreshold } from "@paxeer-network/hooks";
import { Decimal } from "@paxeer-network/utils";

export const useLtvScript = () => {
  const { ltv_threshold, negative_usdc_threshold, isLoading, error } =
    useConvertThreshold();

  // The shared hook returns the raw ratio (e.g. 0.9); format as "90%" for display.
  const ltvThreshold =
    ltv_threshold === undefined || isLoading
      ? "-"
      : `${new Decimal(ltv_threshold).mul(100).toNumber()}%`;

  return {
    ltv_threshold: ltvThreshold,
    negative_usdc_threshold,
    isLoading,
    error,
  } as const;
};

export type LtvScriptReturns = ReturnType<typeof useLtvScript>;
