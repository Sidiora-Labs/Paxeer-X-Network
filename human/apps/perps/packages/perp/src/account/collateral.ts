import { Decimal, type Numeric } from "@paxeer-network/utils";
import { IMRFactorPower } from "../constants";

export const normalizeCollateralCap = (
  collateralQty: number,
  collateralCap: number,
) => {
  return collateralCap === -1 ? collateralQty : Math.max(collateralCap, 0);
};

export const positiveCollateralContribution = (params: {
  collateralQty: number;
  collateralCap: number;
  collateralRatio: Numeric;
  indexPrice: number;
}): Decimal => {
  const { collateralQty, collateralCap, collateralRatio, indexPrice } = params;

  if (collateralQty <= 0 || !Number.isFinite(indexPrice) || indexPrice <= 0) {
    return new Decimal(0);
  }

  const cap = normalizeCollateralCap(collateralQty, collateralCap);

  return new Decimal(Math.min(collateralQty, cap))
    .mul(collateralRatio)
    .mul(indexPrice);
};

export const collateralRatio = (params: {
  baseWeight: number;
  discountFactor: number | null;
  collateralQty: number;
  collateralCap: number;
  indexPrice: number;
}) => {
  const {
    baseWeight,
    discountFactor,
    collateralQty,
    collateralCap,
    indexPrice,
  } = params;

  // if collateralCap is -1, it means the collateral is unlimited
  const cap = normalizeCollateralCap(collateralQty, collateralCap);

  const K = new Decimal(1.2);
  const DCF = new Decimal(discountFactor || 0);
  const qty = new Decimal(Math.min(collateralQty, cap));

  const notionalAbs = qty.mul(indexPrice).abs();
  const dynamicWeight = DCF.mul(notionalAbs.toPower(IMRFactorPower));
  const result = K.div(new Decimal(1).add(dynamicWeight));

  return result.lt(baseWeight) ? result : new Decimal(baseWeight);
};

/** collateral_value_i = min(collateral_qty_i , collateral_cap_i) * weight_i * index_price_i */
export const collateralContribution = (params: {
  collateralQty: number;
  collateralCap: number;
  collateralRatio: number;
  indexPrice: number;
}) => {
  const { collateralQty, collateralCap, collateralRatio, indexPrice } = params;

  return positiveCollateralContribution({
    collateralQty,
    collateralCap,
    collateralRatio,
    indexPrice,
  }).toNumber();
};
