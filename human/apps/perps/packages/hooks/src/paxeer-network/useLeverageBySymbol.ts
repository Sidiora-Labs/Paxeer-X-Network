import { MarginMode } from "@paxeer-network/types";
import { useSymbolLeverageMap } from "./useSymbolLeverageMap";

/**
 * A hook to fetch and subscribe to leverage for a given trading symbol.
 * It initially fetches the leverage data via a private query and then listens for real-time
 * updates through a WebSocket subscription to keep the leverage value current.
 *
 * @param symbol - The trading symbol (e.g. "PERP_BTC_USDC")
 * @param marginMode - Optional margin mode (CROSS or ISOLATED). If not provided, defaults to CROSS.
 * @returns The current leverage value associated with the symbol, or undefined if not available
 *
 * @example
 * ```typescript
 * const leverage = useLeverageBySymbol("PERP_BTC_USDC");
 * const isolatedLeverage = useLeverageBySymbol("PERP_BTC_USDC", MarginMode.ISOLATED);
 * ```
 */
export const useLeverageBySymbol = (
  symbol?: string,
  marginMode?: MarginMode,
) => {
  const { getSymbolLeverage } = useSymbolLeverageMap();

  return getSymbolLeverage(symbol, marginMode);
};
