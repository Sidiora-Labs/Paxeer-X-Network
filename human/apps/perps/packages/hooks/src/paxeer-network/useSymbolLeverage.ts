import { useMemo } from "react";
import { mutate } from "swr";
import { API, MarginMode } from "@paxeer-network/types";
import { useAccount } from "../useAccount";
import { useMutation } from "../useMutation";
import { useSymbolInfo } from "./useSymbolInfo";

/**
 * A custom hook that calculates the maximum allowed leverage for a given trading pair symbol.
 *
 * The final leverage is determined by taking the minimum value between the account's maximum
 * leverage and the symbol's maximum leverage.
 *
 * @param symbol - Trading pair symbol (e.g. "PERP_BTC_USDC")
 * @returns The maximum allowed leverage as a number, or "-" if the leverage cannot be determined
 *
 * @example
 * ```typescript
 * const leverage = useMaxSymbolLeverage("PERP_BTC_USDC");
 * console.log(`Maximum leverage for PERP_BTC_USDC: ${leverage}x`);
 * ```
 */
export const useSymbolLeverage = (symbol?: string) => {
  const symbolInfo = useSymbolInfo(symbol);
  const { state } = useAccount();

  const [updateMutation, { isMutating }] = useMutation("/v1/client/leverage");

  /**
   * Calculates the maximum leverage for the symbol based on its base initial margin requirement (IMR)
   */
  const maxLeverage = useMemo(() => {
    const baseIMR = symbolInfo?.("base_imr");
    return baseIMR ? 1 / baseIMR : 1;
  }, [symbolInfo]);

  const update = async (data: {
    leverage: number;
    symbol: string;
    margin_mode?: MarginMode;
  }) => {
    const result = await updateMutation(data);

    if (result?.success && data.symbol && state.accountId) {
      const key = ["/v1/client/leverages", state.accountId];

      mutate(
        key,
        (prev: API.LeverageInfo[] | undefined) => {
          if (!prev) {
            return [
              {
                symbol: data.symbol,
                leverage: data.leverage,
                margin_mode: data.margin_mode,
              },
            ];
          }

          const index = prev.findIndex(
            (item) =>
              item.symbol === data.symbol &&
              (item.margin_mode ?? MarginMode.CROSS) ===
                (data.margin_mode ?? MarginMode.CROSS),
          );

          if (index === -1) {
            return [
              ...prev,
              {
                symbol: data.symbol,
                leverage: data.leverage,
                margin_mode: data.margin_mode,
              },
            ];
          }

          const next = [...prev];
          next[index] = {
            ...next[index],
            leverage: data.leverage,
          };

          return next;
        },
        { revalidate: false },
      );
    }

    return result;
  };

  return {
    maxLeverage,
    update,
    isLoading: isMutating,
  };
};
