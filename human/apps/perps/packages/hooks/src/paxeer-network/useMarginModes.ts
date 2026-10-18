import { useCallback, useMemo } from "react";
import { MarginMode } from "@paxeer-network/types";
import { useBadgeBySymbol } from "../useBadgeBySymbol";
import { useMutation } from "../useMutation";
import { usePrivateQuery } from "../usePrivateQuery";

export type SetMarginModePayload = {
  symbol_list: string[];
  default_margin_mode: MarginMode;
};

type SetMarginModeResult = {
  success: boolean;
  message?: string;
};

type MarginModesResponseItem = {
  symbol: string;
  default_margin_mode: MarginMode;
};

/**
 * A high-level hook to manage margin modes for all symbols.
 *
 * It encapsulates both:
 * - fetching current default margin modes for all symbols
 * - updating margin mode for one or multiple symbols
 */
export const useMarginModes = () => {
  const { data, error, isLoading, mutate } = usePrivateQuery<
    Array<MarginModesResponseItem>
  >("/v1/client/margin_modes", {
    revalidateOnFocus: false,
    revalidateOnMount: true,
  });

  const [setMarginModeInternal, { isMutating }] = useMutation(
    "/v1/client/margin_mode",
    "POST",
  );

  const marginModes = useMemo(() => {
    if (!data || !Array.isArray(data)) return {};

    const map: Record<string, MarginMode> = {};
    for (const item of data) {
      map[item.symbol] = item.default_margin_mode;
    }

    return map;
  }, [data]);

  const setMarginMode = (payload: SetMarginModePayload) =>
    setMarginModeInternal(payload) as Promise<SetMarginModeResult>;

  const updateMarginMode = useCallback(
    async (payload: SetMarginModePayload) => {
      const result = await setMarginMode(payload);

      if (result.success) {
        await mutate();
        return result;
      }

      throw new Error(result.message ?? "Failed to update margin mode");
    },
    [setMarginMode, mutate],
  );

  return {
    marginModes,
    isLoading,
    error,
    refresh: mutate,
    setMarginMode,
    updateMarginMode,
    isMutating,
  };
};

export const useMarginModeBySymbol = (
  symbol: string,
  fallback: MarginMode | null = MarginMode.CROSS,
) => {
  const { marginModes, isLoading, error, refresh, updateMarginMode } =
    useMarginModes();

  // Permissionless-listing is determined by the API-returned `broker_id`.
  // `brokerName` is for display only (from a separate broker mapping cache)
  // and must not be used for permission/trading constraint enforcement.
  const { brokerId } = useBadgeBySymbol(symbol);

  const marginMode = useMemo(() => {
    // If brokerId exists, it means permissionless-listing.
    // Permissionless-listing symbols can ONLY be ISOLATED.
    if (brokerId) return MarginMode.ISOLATED;

    return fallback === null
      ? marginModes[symbol]
      : (marginModes[symbol] ?? fallback);
  }, [brokerId, fallback, marginModes, symbol]);

  const update = useCallback(
    async (mode: MarginMode) => {
      // Enforce permissionless-listing symbols to be ISOLATED.
      if (brokerId) mode = MarginMode.ISOLATED;
      return updateMarginMode({
        symbol_list: [symbol],
        default_margin_mode: mode,
      });
    },
    [brokerId, symbol, updateMarginMode],
  );

  return {
    marginMode,
    isLoading,
    error,
    refresh,
    update,
    isPermissionlessListing: !!brokerId,
  };
};
