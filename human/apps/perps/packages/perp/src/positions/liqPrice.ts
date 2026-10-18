import { API } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { IMRFactorPower } from "../constants";
import { DMax } from "../utils";

const MaxIterates = 30;
const CONVERGENCE_THRESHOLD = 0.0001;

const mmForOtherSymbols = (
  positions: Pick<API.Position, "position_qty" | "mark_price" | "mmr">[],
) => {
  // sum_i ( abs(position_qty_i) * mark_price_i * mmr_i )
  return positions.reduce<Decimal>((acc, cur) => {
    return acc.add(
      new Decimal(cur.position_qty).abs().mul(cur.mark_price).mul(cur.mmr),
    );
  }, zero);
};

const calculateLiqPrice = (
  // symbol: string,
  markPrice: number,
  positionQty: number,
  MMR: number,
  totalCollateral: number,
  positions: Pick<API.Position, "position_qty" | "mark_price" | "mmr">[],
): Decimal => {
  const decimalMarkPrice = new Decimal(markPrice);
  const absQty = new Decimal(positionQty).abs();
  const denominator = absQty.mul(MMR).sub(positionQty);

  const liqPrice = new Decimal(totalCollateral)
    .sub(absQty.mul(decimalMarkPrice).mul(MMR))
    .sub(mmForOtherSymbols(positions))
    .div(denominator)
    .add(decimalMarkPrice);

  return DMax(liqPrice, zero);
};

const compareCollateralWithMM = (
  // price: number,
  inputs: {
    totalCollateral: number;
    positionQty: number;
    markPrice: number;
    baseMMR: number;
    baseIMR: number;
    IMRFactor: number;
    // IMRFactorPower: number;
    positions: Pick<
      API.PositionExt,
      "position_qty" | "mark_price" | "mmr" | "symbol"
    >[];
  },
) => {
  return (price: Decimal) => {
    const {
      totalCollateral,
      positionQty,
      markPrice,
      baseMMR,
      baseIMR,
      IMRFactor,
      positions,
    } = inputs;
    const decimalPositionQty = new Decimal(positionQty);
    const collateral = new Decimal(totalCollateral)
      .sub(decimalPositionQty.mul(markPrice))
      .add(decimalPositionQty.mul(price));

    const mm = decimalPositionQty
      .abs()
      .mul(price)
      .mul(
        Math.max(
          baseMMR,
          new Decimal(baseMMR)
            .div(baseIMR)
            .mul(IMRFactor)
            .mul(decimalPositionQty.mul(price).abs().toPower(IMRFactorPower))
            .toNumber(),
        ),
      )
      .add(mmForOtherSymbols(positions));

    // console.log("*****", {
    //   collateral: collateral.toNumber(),
    //   mm: mm.toNumber(),
    // });

    return collateral.gte(mm);
  };
};

/**
 * @formulaId liqPrice
 * @name Position Liquidation Price
 * @description
 *
 * ## Define:
 *
 * ### (1) calculate_liq_price function
 *
 * ```
 * calculate_liq_price( mark_price, position_qty, mmr )
 * ```
 *
 * If `position_qty >= 0` AND if `abs(position_qty) * mmr - position_qty >= 0`:
 *
 * Return `mark_price`
 *
 * Else:
 *
 * Return `max( mark_price + [ total_collateral_value - abs(position_qty) * mark_price * mmr - mm_for_other_symbols ] / [ abs(position_qty) * mmr - position_qty ], 0 )`
 *
 * Where `total_collateral_value` and `mm_for_other_symbols` are constants.
 *
 * - **total_collateral_value**
 * - **mm_for_other_symbols** = `sum_i ( abs(position_qty_i) * mark_price_i * mmr_i )` for i != current symbol
 *
 * ### (2) compare_collateral_w_mm function
 *
 * ```
 * compare_collateral_w_mm( price ) = collateral >= mm
 * ```
 *
 * Where:
 * - **collateral** = `total_collateral_value - position_qty_i * mark_price + position_qty_i * price`
 * - **mm** = `abs(position_qty_i) * price * Max(Base MMR i, (Base MMR i / Base IMR i) * IMR Factor i * Abs(position_qty_i * price)^(4/5)) + mm_for_other_symbols`
 *
 * ## Given:
 *
 * Position liquidation price for symbol i with:
 * - current mark price = `mark_price_i`
 * - current position qty = `position_qty_i`
 * - current mmr = `mmr_i = Max(Base MMR i, (Base MMR i / Base IMR i) * IMR Factor i * Abs(Position Notional i)^(4/5))`
 * - symbol base mmr = `base_mmr_i`
 *
 * ## For LONG position
 *
 * ```
 * liq_price_left = calculate_liq_price( mark_price_i, position_qty_i, base_mmr_i )
 * liq_price_right = calculate_liq_price( mark_price_i, position_qty_i, mmr_i )
 *
 * ITERATE 30 times:
 *     if liq_price_left >= liq_price_right:
 *         return liq_price_right
 *
 *     mid = ( liq_price_left + liq_price_right ) / 2
 *
 *     if compare_collateral_w_mm( mid ):
 *         liq_price_right = mid
 *     else:
 *         liq_price_left = mid
 *
 *     if (liq_price_right - liq_price_left) / (liq_price_left + liq_price_right) * 2 <= 0.0001:
 *         break
 *
 * return liq_price_right
 * ```
 *
 * ## For SHORT position
 *
 * ```
 * liq_price_right = calculate_liq_price( mark_price_i, position_qty_i, mmr_i )
 * liq_price_left = calculate_liq_price( mark_price_i, position_qty_i,
 *   Max(Base MMR i, (Base MMR i / Base IMR i) * IMR Factor i * Abs(position_qty_i * liq_price_right)^(4/5))
 * )
 *
 * ITERATE 30 times:
 *     if liq_price_left >= liq_price_right:
 *         return liq_price_left
 *
 *     mid = ( liq_price_left + liq_price_right ) / 2
 *
 *     if compare_collateral_w_mm( mid ):
 *         liq_price_left = mid
 *     else:
 *         liq_price_right = mid
 *
 *     if (liq_price_right - liq_price_left) / (liq_price_left + liq_price_right) * 2 <= 0.0001:
 *         break
 *
 * return liq_price_left
 * ```
 *
 * @returns The liquidation price of the position.
 */
export function liqPrice(inputs: {
  markPrice: number;
  symbol: string;
  totalCollateral: number;
  positionQty: number;
  positions: Pick<
    API.PositionExt,
    "position_qty" | "mark_price" | "mmr" | "symbol"
  >[];
  MMR: number;
  baseMMR: number;
  baseIMR: number;
  IMRFactor: number;
  costPosition: number;
}): number | null {
  const {
    positionQty,
    markPrice,
    totalCollateral,
    positions,
    MMR,
    baseMMR,
    baseIMR,
    IMRFactor,
    symbol,
  } = inputs;

  if (positionQty === 0 || totalCollateral === 0) {
    return null;
  }
  const isLONG = positionQty > 0;

  const otherPositions = positions.filter((item) => item.symbol !== symbol);

  if (isLONG) {
    let liqPriceLeft = calculateLiqPrice(
      markPrice,
      positionQty,
      baseMMR,
      totalCollateral,
      otherPositions,
    );
    let liqPriceRight = calculateLiqPrice(
      markPrice,
      positionQty,
      MMR,
      totalCollateral,
      otherPositions,
    );

    const compareCollateralWithMMFunc = compareCollateralWithMM({
      totalCollateral,
      positionQty,
      markPrice,
      baseIMR,
      baseMMR,
      IMRFactor,
      positions: otherPositions,
    });

    for (let i = 0; i < MaxIterates; i++) {
      if (liqPriceLeft.gte(liqPriceRight)) {
        return liqPriceRight.toNumber();
      }

      const mid = new Decimal(liqPriceLeft).add(liqPriceRight).div(2);

      if (compareCollateralWithMMFunc(mid)) {
        liqPriceRight = mid;
      } else {
        liqPriceLeft = mid;
      }

      if (
        liqPriceRight
          .sub(liqPriceLeft)
          .div(liqPriceLeft.add(liqPriceRight))
          .mul(2)
          .lte(CONVERGENCE_THRESHOLD)
      ) {
        break;
      }
    }
    return liqPriceRight.toNumber();
  } else {
    // const decimalBaseMMR = new Decimal(baseMMR);
    let liqPriceRight = calculateLiqPrice(
      markPrice,
      positionQty,
      MMR,
      totalCollateral,
      otherPositions,
    );

    let liqPriceLeft = calculateLiqPrice(
      markPrice,
      positionQty,
      Math.max(
        baseIMR,
        new Decimal(baseMMR)
          .div(baseIMR)
          .mul(IMRFactor)
          .mul(
            new Decimal(positionQty)
              .mul(liqPriceRight)
              .abs()
              .toPower(IMRFactorPower),
          )
          .toNumber(),
      ),
      totalCollateral,
      otherPositions,
    );

    const compareCollateralWithMMFunc = compareCollateralWithMM({
      totalCollateral,
      positionQty,
      markPrice,
      baseMMR,
      baseIMR,
      IMRFactor,
      positions: otherPositions,
    });

    for (let i = 0; i < MaxIterates; i++) {
      if (liqPriceLeft.gte(liqPriceRight)) {
        return liqPriceLeft.toNumber();
      }

      const mid = liqPriceLeft.add(liqPriceRight).div(2);

      if (compareCollateralWithMMFunc(mid)) {
        liqPriceLeft = mid;
      } else {
        liqPriceRight = mid;
      }

      if (
        liqPriceRight
          .sub(liqPriceLeft)
          .div(liqPriceLeft.add(liqPriceRight))
          .mul(2)
          .lte(CONVERGENCE_THRESHOLD)
      ) {
        break;
      }

      // return liqPriceLeft.toNumber();
    }

    return liqPriceLeft.toNumber();
  }
}
