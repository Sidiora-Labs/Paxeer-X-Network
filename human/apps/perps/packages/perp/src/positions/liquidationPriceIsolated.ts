import { Decimal } from "@paxeer-network/utils";
import { IMRFactorPower, IsoTakerFeeBuffer } from "../constants";

/**
 * @formulaId liquidationPriceIsolated
 * @name Liquidation Price for Isolated Margin Position
 * @formula liquidation_price = (isolated_position_margin' - cost_position' - funding_adjustment) / (abs(position_qty') * MMR' - position_qty')
 * funding_adjustment = position_qty' * (sum_unitary_funding - last_sum_unitary_funding)
 * position_qty' = position_qty + order_side * order_qty
 * MMR' = max(base_MMR, (base_MMR / base_IMR) * IMR_factor * abs(position_qty' * reference_price)^(4/5))
 * @description
 *
 * ## Definition
 *
 * **liquidation_price**: Price at which the isolated margin position will be liquidated
 *
 * **isolated_position_margin'**: Isolated position margin after order execution (if applicable)
 *
 * **cost_position'**: Position cost after order execution (if applicable)
 *
 * **funding_adjustment**: Adjustment for funding fees
 *
 * **position_qty'**: Position quantity after order execution
 *
 * **MMR'**: Maintenance margin rate after order execution
 *
 * ## Scenarios
 *
 * ### 1. No Order (order_qty = 0)
 * - `isolated_position_margin' = isolated_position_margin`
 * - `cost_position' = cost_position`
 *
 * ### 2. Open/Add Position (position_qty = 0 or order_side = sign(position_qty))
 * - `isolated_position_margin' = isolated_position_margin + order_qty * reference_price * (1 / leverage + iso_taker_fee_buffer)`
 * - `cost_position' = cost_position + order_side * order_qty * reference_price`
 *
 * ### 3. Close/Reduce Position (order_side ≠ sign(position_qty) and sign(position_qty') = sign(position_qty))
 * - `isolated_position_margin' = isolated_position_margin * position_qty' / position_qty`
 * - `cost_position' = cost_position + order_side * order_qty * reference_price`
 *
 * ### 4. Flip Position (order_side ≠ sign(position_qty) and sign(position_qty') ≠ sign(position_qty))
 * - `isolated_position_margin' = abs(position_qty') * reference_price * (1 / leverage + iso_taker_fee_buffer)`
 * - `cost_position' = position_qty' * reference_price`
 *
 * ## Example
 *
 * ```
 * isolated_position_margin = 2000
 * cost_position = 100000
 * position_qty = 2 (long)
 * sum_unitary_funding = 0.001
 * last_sum_unitary_funding = 0.0008
 * base_MMR = 0.025
 * base_IMR = 0.04
 * IMR_factor = 0.0000001
 * reference_price = 50000
 * liquidation_price = (2000 - 100000 - 0.0004) / (2 * 0.025 - 2) ≈ 50256.41
 * ```
 *
 * @param inputs Input parameters for calculating liquidation price
 * @returns Liquidation price (in USDC) or null if invalid
 */
export function liquidationPriceIsolated(inputs: {
  /**
   * @description Current isolated position margin
   */
  isolatedPositionMargin: number;
  /**
   * @description Current position cost
   */
  costPosition: number;
  /**
   * @description Current position quantity (positive for long, negative for short)
   */
  positionQty: number;
  /**
   * @description Current cumulative unitary funding
   */
  sumUnitaryFunding: number;
  /**
   * @description Last cumulative unitary funding (at last settlement)
   */
  lastSumUnitaryFunding: number;
  /**
   * @description Base maintenance margin rate
   */
  baseMMR: number;
  /**
   * @description Base initial margin rate
   */
  baseIMR: number;
  /**
   * @description IMR calculation factor
   */
  IMRFactor: number;
  /**
   * @description Reference price (mark price for current position, or order price for estimated liquidation)
   */
  referencePrice?: number;
  /**
   * @description Order side (BUY = +1, SELL = -1) for calculating estimated liquidation after order execution
   */
  orderSide?: "BUY" | "SELL";
  /**
   * @description Order quantity for calculating estimated liquidation after order execution
   */
  orderQty?: number;
  /**
   * @description Leverage for the position
   */
  leverage: number;
  /**
   * @description Fee buffer reserved in isolated margin estimates (default: 0.0006)
   */
  isoTakerFeeBuffer?: number;
}): number | null {
  const {
    isolatedPositionMargin,
    costPosition,
    positionQty,
    sumUnitaryFunding,
    lastSumUnitaryFunding,
    baseMMR,
    baseIMR,
    IMRFactor,
    referencePrice,
    orderSide,
    orderQty = 0,
    leverage,
    isoTakerFeeBuffer = IsoTakerFeeBuffer,
  } = inputs;

  // Use reference price or mark price (default to a reasonable value if not provided)
  const refPrice = referencePrice ?? 0;
  if (refPrice <= 0 && orderQty !== 0) {
    return null;
  }

  // Calculate position_qty' after order execution
  const orderSideMultiplier =
    orderSide === "BUY" ? 1 : orderSide === "SELL" ? -1 : 0;
  const newPositionQty = positionQty + orderSideMultiplier * orderQty;

  if (newPositionQty === 0) {
    return null; // No position after order execution
  }

  // Determine scenario and calculate isolated_position_margin' and cost_position'
  let newIsolatedPositionMargin: Decimal;
  let newCostPosition: Decimal;
  const getMarginRate = () =>
    new Decimal(1).div(leverage).add(isoTakerFeeBuffer);

  if (orderQty === 0) {
    // Scenario 1: No order
    newIsolatedPositionMargin = new Decimal(isolatedPositionMargin);
    newCostPosition = new Decimal(costPosition);
  } else if (
    positionQty === 0 ||
    (orderSideMultiplier > 0 && positionQty > 0) ||
    (orderSideMultiplier < 0 && positionQty < 0)
  ) {
    // Scenario 2: Open/Add position
    newIsolatedPositionMargin = new Decimal(isolatedPositionMargin).add(
      new Decimal(orderQty).mul(refPrice).mul(getMarginRate()),
    );
    newCostPosition = new Decimal(costPosition).add(
      new Decimal(orderSideMultiplier).mul(orderQty).mul(refPrice),
    );
  } else {
    const signPositionQty = positionQty > 0 ? 1 : -1;
    const signNewPositionQty = newPositionQty > 0 ? 1 : -1;

    if (signNewPositionQty === signPositionQty) {
      // Scenario 3: Close/Reduce position
      newIsolatedPositionMargin = new Decimal(isolatedPositionMargin)
        .mul(newPositionQty)
        .div(positionQty);
      newCostPosition = new Decimal(costPosition).add(
        new Decimal(orderSideMultiplier).mul(orderQty).mul(refPrice),
      );
    } else {
      // Scenario 4: Flip position
      newIsolatedPositionMargin = new Decimal(Math.abs(newPositionQty))
        .mul(refPrice)
        .mul(getMarginRate());
      newCostPosition = new Decimal(newPositionQty).mul(refPrice);
    }
  }

  // Calculate funding adjustment
  const fundingAdjustment = new Decimal(newPositionQty).mul(
    new Decimal(sumUnitaryFunding).sub(lastSumUnitaryFunding),
  );

  // Calculate MMR' based on new position notional
  const newPositionNotional = new Decimal(Math.abs(newPositionQty)).mul(
    refPrice,
  );
  const dynamicMMR = new Decimal(baseMMR)
    .div(baseIMR)
    .mul(IMRFactor)
    .mul(newPositionNotional.toPower(IMRFactorPower))
    .toNumber();
  const newMMR = Math.max(baseMMR, dynamicMMR);

  // Calculate denominator: abs(position_qty') * MMR' - position_qty'
  const denominator = new Decimal(Math.abs(newPositionQty))
    .mul(newMMR)
    .sub(newPositionQty);

  if (denominator.isZero()) {
    return null; // Invalid denominator
  }

  // Calculate liquidation price
  const numerator = newIsolatedPositionMargin
    .sub(newCostPosition)
    .sub(fundingAdjustment);

  const liquidationPrice = numerator.div(denominator).toNumber();

  // Return null for invalid prices (negative or zero)
  if (liquidationPrice <= 0) {
    return null;
  }

  return liquidationPrice;
}
