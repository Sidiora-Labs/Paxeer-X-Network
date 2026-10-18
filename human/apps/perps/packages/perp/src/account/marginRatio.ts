import { API } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";

/**
 * total margin ratio
 */
export function totalMarginRatio(
  inputs: {
    totalCollateral: number;
    markPrices: { [key: string]: number };
    positions: API.Position[];
  },
  dp?: number,
): number {
  const { totalCollateral, markPrices, positions } = inputs;

  if (totalCollateral === 0) {
    return 0;
  }

  const totalCollateralDecimal = new Decimal(totalCollateral);

  const totalPositionNotional = positions.reduce((acc, cur) => {
    const markPrice = markPrices[cur.symbol] || 0;
    return acc.add(new Decimal(cur.position_qty).mul(markPrice).abs());
  }, zero);

  if (totalPositionNotional.eq(zero)) {
    return 0;
  }

  return totalCollateralDecimal.div(totalPositionNotional).toNumber();
}
