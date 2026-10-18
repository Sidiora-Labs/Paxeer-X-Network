import { Decimal, zero } from "@paxeer-network/utils";
import { positiveCollateralContribution } from "./collateral";

export type NonUSDCHolding = {
  holding: number;
  /** Signed pending quantity. Pending sells are negative. */
  pendingShort?: number;
  indexPrice: number;
  collateralCap: number;
  collateralRatio: Decimal;
  /** Defaults to true for backward compatibility. */
  isCollateral?: boolean;
};

/**
 * @formulaId totalCollateral
 * @name Total Collateral
 * @formula Total collateral = usdc balance.holding + usdc balance.pending_short - usdc balance.isolated_order_frozen + SUM(non-usdc effective holding value) + total cross unsettlement PNL
 * @description
 *
 * ## Definition
 *
 * **discount**: Collateral substitution rate
 *
 * **Total collateral**: Total value of collateral assets in the user's account (denominated in USDC)
 *
 * **usdc balance.holding**: USDC holding quantity
 *
 * **non-USDC effective holding**: `holding + pending_short`. Positive eligible collateral applies its cap and collateral ratio. Negative quantities are valued at the full index price as debt.
 *
 * **holding**: Asset quantity held by the user, from `/v1/client/holding` or v2 Websocket API | Balance
 *
 * **pending_short**: Signed pending quantity; pending sells are negative
 *
 * **is_collateral**: Positive assets contribute only when enabled as collateral
 *
 * **mark price**: Current price of the asset, from v2 Websocket API | Balance
 *
 * **total unsettlement PNL**: Sum of user's account unsettled PNL
 *
 * ## Example
 *
 * ```
 * Total collateral = 2000 + 1000 * 1.001 * 0 - 18.34 = 1981.66
 * total unsettlement PNL = -18.34
 * ```
 */
export function totalCollateral(inputs: {
  USDCHolding: number;
  nonUSDCHolding: NonUSDCHolding[];
  /**
   * Sum of user's account unsettled PNL
   */
  unsettlementPnL: number;
  /**
   * @description USDC balance frozen for pending short orders (for freeCollateral calculation)
   * @default 0
   */
  usdcBalancePendingShortQty?: number;
  /**
   * @description USDC balance frozen for isolated margin orders (for freeCollateral calculation)
   * @default 0
   */
  usdcBalanceIsolatedOrderFrozen?: number;
  /**
   * @description Total cross margin unsettled PNL (for freeCollateral calculation). If provided, this will be used instead of unsettlementPnL.
   */
  totalCrossUnsettledPnL?: number;
}): Decimal {
  const {
    USDCHolding,
    nonUSDCHolding,
    unsettlementPnL,
    usdcBalancePendingShortQty = 0,
    usdcBalanceIsolatedOrderFrozen = 0,
    totalCrossUnsettledPnL,
  } = inputs;

  // Calculate USDC part: holding + pending_short_qty - isolated_order_frozen
  const usdcPart = new Decimal(USDCHolding)
    .add(usdcBalancePendingShortQty)
    .sub(Math.abs(usdcBalanceIsolatedOrderFrozen));

  let hasUnpricedDebt = false;
  const nonUSDCHoldingValue = nonUSDCHolding.reduce<Decimal>((acc, cur) => {
    const effectiveHolding = new Decimal(cur.holding)
      .add(cur.pendingShort ?? 0)
      .toNumber();

    if (effectiveHolding < 0) {
      if (!Number.isFinite(cur.indexPrice) || cur.indexPrice <= 0) {
        hasUnpricedDebt = true;
        return acc;
      }

      // Debt is valued at full index price without collateral discounts or caps.
      return acc.add(new Decimal(effectiveHolding).mul(cur.indexPrice));
    }

    if (cur.isCollateral === false) {
      return acc;
    }

    return acc.add(
      positiveCollateralContribution({
        collateralQty: effectiveHolding,
        collateralCap: cur.collateralCap,
        collateralRatio: cur.collateralRatio,
        indexPrice: cur.indexPrice,
      }),
    );
  }, zero);

  // Match backend fail-closed behavior when a debt cannot be priced.
  if (hasUnpricedDebt) {
    return zero;
  }

  // Use totalCrossUnsettledPnL if provided, otherwise use unsettlementPnL
  const pnl =
    totalCrossUnsettledPnL !== undefined
      ? totalCrossUnsettledPnL
      : unsettlementPnL;

  return usdcPart.add(nonUSDCHoldingValue).add(pnl);
}
