import { account } from "@paxeer-network/perp";
import type { API } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

type NonUSDCHolding = {
  holding: number;
  pendingShort: number;
  indexPrice: number;
  // margin replacement rate, default 0
  collateralCap: number;
  collateralRatio: Decimal;
  isCollateral: boolean;
};

export const parseHolding = (
  holding: API.Holding[] | ReadonlyArray<API.Holding>,
  indexPrices: Record<string, number>,
  tokensInfo: API.Token[] | ReadonlyArray<API.Token>,
): [number, NonUSDCHolding[]] => {
  // if (!holding || !indexPrices) {
  //   return [zero, zero];
  // }
  const nonUSDC: NonUSDCHolding[] = [];

  let USDC_holding = 0;

  holding.forEach((item) => {
    if (item.token === "USDC") {
      USDC_holding = item.holding;
    } else {
      const tokenInfo = tokensInfo.find(({ token }) => token === item.token);
      const {
        base_weight = 0,
        discount_factor = 0,
        user_max_qty = 0,
        is_collateral = false,
      } = tokenInfo || {};

      const holdingQty = item?.holding ?? 0;
      const pendingShort = item?.pending_short ?? 0;
      const effectiveHolding = new Decimal(holdingQty)
        .add(pendingShort)
        .toNumber();

      const indexPrice = indexPrices[`PERP_${item.token}_USDC`] ?? 0;

      const collateralRatio = account.collateralRatio({
        baseWeight: base_weight,
        discountFactor: discount_factor,
        collateralQty: Math.max(effectiveHolding, 0),
        collateralCap: user_max_qty,
        indexPrice,
      });

      nonUSDC.push({
        holding: holdingQty,
        pendingShort,
        indexPrice,
        collateralCap: user_max_qty,
        collateralRatio: collateralRatio,
        isCollateral: is_collateral,
      });
    }
  });

  return [USDC_holding, nonUSDC];
};
