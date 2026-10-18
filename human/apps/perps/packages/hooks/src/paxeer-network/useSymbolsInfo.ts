import { useMemo } from "react";
import { createGetter } from "../utils/createGetter";
import { useAppStore } from "./appStore";

/**
 * Type alias for the return type of useSymbolsInfo hook
 */
export type SymbolsInfo = ReturnType<typeof useSymbolsInfo>;

/**
 * A hook that provides access to symbol information.
 *
 * @returns A getter object that provides access to symbol information.
 * The getter allows accessing symbol data either by symbol name directly,
 * or through a two-level access pattern (symbol and property).
 *
 * @example
 * ```typescript
 * const symbolsInfo = useSymbolsInfo();
 *
 * // Get all info for a symbol
 * const ethInfo = symbolsInfo["PERP_ETH_USDC"]();
 *
 * // Get specific property for a symbol
 * const baseDP = symbolsInfo["PERP_ETH_USDC"]('base_dp');
 *
 * // Get specific property for a symbol with default value
 * const quoteDP = symbolsInfo["PERP_ETH_USDC"]('quote_dp', 2);
 * ```
 */
export const useSymbolsInfo = () => {
  const symbolsInfo = useAppStore((state) => state.symbolsInfo);

  return useMemo(
    () => createGetter(symbolsInfo ? { ...symbolsInfo } : symbolsInfo),
    [symbolsInfo],
  );
};

export const useSymbolsInfoStore = () => {
  return useAppStore((state) => state.symbolsInfo);
};
