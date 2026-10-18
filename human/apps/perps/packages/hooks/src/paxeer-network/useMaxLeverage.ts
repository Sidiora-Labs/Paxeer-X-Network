import { useMemo } from "react";
import { useAccountInfo } from "./appStore";
import { useSymbolsInfo } from "./useSymbolsInfo";

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
 * const leverage = useMaxLeverage("PERP_BTC_USDC");
 * console.log(`Maximum leverage for PERP_BTC_USDC: ${leverage}x`);
 * ```
 */
export const useMaxLeverage = (symbol: string): number => {
  const symbolsInfo = useSymbolsInfo();
  const accountInfo = useAccountInfo();
  const maxAccountLeverage = accountInfo?.max_leverage;

  /**
   * Calculates the maximum leverage for the symbol based on its base initial margin requirement (IMR)
   */
  const maxSymbolLeverage = useMemo(() => {
    const symbolInfo = symbolsInfo[symbol];
    const baseIMR = symbolInfo("base_imr");
    return baseIMR ? 1 / baseIMR : 1;
  }, [symbolsInfo, symbol]);

  /**
   * Determines the final maximum leverage by taking the minimum between
   * account leverage limit and symbol leverage limit
   */
  const maxLeverage = useMemo(() => {
    if (!maxAccountLeverage || !maxSymbolLeverage) {
      return 1;
    }

    return Math.min(maxAccountLeverage, maxSymbolLeverage);
  }, [maxAccountLeverage, maxSymbolLeverage]);

  return maxLeverage;
};
