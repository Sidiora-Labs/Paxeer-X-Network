import { Decimal } from "@paxeer-network/utils";

/**
 * get the min notional for the order
 */
export function getMinNotional(props: {
  base_tick?: number;
  price?: string | number;
  qty?: string | number;
  min_notional?: number;
  quote_dp?: number;
  base_dp?: number;
  quote_tick?: number;
}) {
  const { price, base_tick, qty, min_notional, base_dp, quote_dp, quote_tick } =
    props;
  if (price && qty && min_notional) {
    try {
      const calcMinNotional = new Decimal(price).mul(new Decimal(qty));

      if (calcMinNotional.lt(min_notional)) {
        let minQty = new Decimal(min_notional)
          .div(price)
          .toDecimalPlaces(base_dp, Decimal.ROUND_DOWN)
          .add(base_tick ?? 0);

        if (base_tick && base_tick > 0) {
          minQty = new Decimal(
            getRoundedDownDivision(minQty.toNumber(), base_tick),
          );
        }

        const newMinNotional = minQty
          .mul(price)
          .add(quote_tick ?? 0)
          .toFixed(quote_dp);

        return newMinNotional;
      }
    } catch (e) {
      console.log("getMinNotional error", e);
    }
  }
}

/**
 * @deprecated please use getMinNotional instead, it will be removed in next major version
 */
export function checkNotional(props: {
  base_tick: number;
  price?: string | number;
  qty?: string | number;
  min_notional?: number;
  quote_dp?: number;
  base_dp?: number;
  quote_tick?: number;
}): string | undefined {
  const { price, base_tick, qty, min_notional, base_dp, quote_dp, quote_tick } =
    props;
  if (price !== undefined && qty !== undefined && min_notional !== undefined) {
    try {
      const calcNotional = new Decimal(price).mul(new Decimal(qty)).toNumber();
      const notional = Number.parseFloat(`${min_notional}`);

      if (calcNotional < notional) {
        let minQty = new Decimal(notional)
          .div(price)
          .toDecimalPlaces(base_dp, Decimal.ROUND_DOWN)
          .add(base_tick ?? 0);
        if (base_tick && base_tick > 0) {
          minQty = new Decimal(
            getRoundedDownDivision(minQty.toNumber(), base_tick),
          );
        }
        const newMinNotional = minQty
          .mul(price)
          .add(quote_tick ?? 0)
          .toFixed(quote_dp);
        return `The order value should be greater or equal to ${newMinNotional} USDC`;
      }

      return undefined;
    } catch (e) {
      return undefined;
    }
  }

  return undefined;
}

/**
 *
 * 452, 100 callback: 400
 * 333, 10 callback:  330
 * 222, 1 callback:  222
 */
function getRoundedDownDivision(
  value: number | string,
  tick: number | string,
): number {
  const decimalValue = new Decimal(value);
  const decimalTick = new Decimal(tick);

  const quotient = decimalValue.dividedToIntegerBy(decimalTick);

  return quotient.mul(decimalTick).toNumber();
}
