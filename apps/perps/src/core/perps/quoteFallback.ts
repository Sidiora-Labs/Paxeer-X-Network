import type { ContractFunctionReturnType } from "viem";
import type { PerpsPrecompileABI } from "@/core/network/abi/PerpsPrecompile";

export interface MarketQuote {
  indexPrice: bigint;
  markPrice: bigint;
  oracleTWAP: bigint;
  fundingRatePerSecond: bigint;
  fundingRate24h: bigint;
  longOI: bigint;
  shortOI: bigint;
  maxLeverage: bigint;
  maintenanceMarginBps: bigint;
  enabled: boolean;
  priceStale: boolean;
}

export interface PartialCloseQuote {
  exitPrice: bigint;
  closedPnl: bigint;
  fee: bigint;
  estimatedPayout: bigint;
}

export type PrecompileMarketQuote = ContractFunctionReturnType<typeof PerpsPrecompileABI, "view", "quoteMarket">;
export type PrecompilePartialCloseQuote = ContractFunctionReturnType<
  typeof PerpsPrecompileABI,
  "view",
  "quotePartialClose"
>;

/** Map the 0x1015 quoteMarket tuple to the hook quote, keeping the market's own leverage cap and margin. */
export function marketQuoteFromPrecompile(result: PrecompileMarketQuote): MarketQuote {
  return {
    indexPrice: result.indexPrice,
    markPrice: result.markPrice,
    oracleTWAP: result.oracleTWAP,
    fundingRatePerSecond: result.fundingRatePerSecond,
    fundingRate24h: result.fundingRate24h,
    longOI: result.longOI,
    shortOI: result.shortOI,
    maxLeverage: result.maxLeverage,
    maintenanceMarginBps: result.maintenanceMarginBps,
    enabled: result.enabled,
    priceStale: result.priceStale,
  };
}

/** Map the four positional outputs of 0x1015 quotePartialClose to a named quote. */
export function partialCloseQuoteFromPrecompile(result: PrecompilePartialCloseQuote): PartialCloseQuote {
  const [exitPrice, closedPnl, fee, estimatedPayout] = result;
  return { exitPrice, closedPnl, fee, estimatedPayout };
}
