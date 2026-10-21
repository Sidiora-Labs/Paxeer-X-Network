import { formatUnits } from "viem";

import { weiOrZero as wei } from "@/ui/utils/format";

import type { AccountState } from "./calls";

/** USD notionals and leverage are 18-decimal; USDL amounts are 6-decimal. */
const USD_DECIMALS = 18;
const USDL_DECIMALS = 6;
const USDL_TO_USD = 10n ** BigInt(USD_DECIMALS - USDL_DECIMALS);

/** The 18-decimal sizeUsd and leverage of an indexer position row. */
export interface PortfolioPosition {
  sizeUsd?: string | null;
  leverage?: string | null;
}

export interface PortfolioTotals {
  /** Account collateral in USD. */
  totalVaultUsd: number;
  /** getAccount(account).withdrawable in USD. */
  freeCollateral: number;
  /** equityUsd less collateral, in USD. */
  unrealizedPnl: number;
  /** Size-weighted leverage of the open positions. */
  totalLeverage: number;
}

function usd(value: bigint, decimals: number): number {
  return Number(formatUnits(value, decimals));
}

/** The totals of usePortfolioStats from getAccount(account) at 0x1015 and the open positions. */
export function portfolioFromAccount(
  accountState: Pick<AccountState, "collateral" | "equityUsd" | "withdrawable">,
  positions: readonly PortfolioPosition[],
): PortfolioTotals {
  let weightedLeverage = 0n;
  let totalSize = 0n;
  for (const position of positions) {
    const size = wei(position.sizeUsd);
    weightedLeverage += wei(position.leverage) * size;
    totalSize += size;
  }
  return {
    totalVaultUsd: usd(accountState.collateral, USDL_DECIMALS),
    freeCollateral: usd(accountState.withdrawable, USDL_DECIMALS),
    unrealizedPnl: usd(accountState.equityUsd - accountState.collateral * USDL_TO_USD, USD_DECIMALS),
    totalLeverage: totalSize > 0n ? usd(weightedLeverage / totalSize, USD_DECIMALS) : 0,
  };
}
