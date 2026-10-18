import { Decimal, zero } from "@paxeer-network/utils";

/**
 * max(0, min(USDC_balance, free_collateral - max(upnl, 0)))
 */
export const maxWithdrawalUSDC = (inputs: {
  USDCBalance: number;
  freeCollateral: Decimal;
  upnl: number;
}) => {
  const { USDCBalance, freeCollateral, upnl } = inputs;
  const value = Math.min(
    new Decimal(USDCBalance).toNumber(),
    new Decimal(freeCollateral).sub(Math.max(upnl, 0)).toNumber(),
  );
  return Math.max(0, value);
};

/**
 *
 * Other collateral: min(collateral_qty_i, free_collateral / (index_price_i × weight_i)
 * Other collateral with negative USDC: min(collateral_qty_i, free_collateral / (index_price_i × (1 + buffer) × weight_i)
 * buffer: 0.2%
 */
export const maxWithdrawalOtherCollateral = (inputs: {
  USDCBalance: number;
  collateralQty: number;
  freeCollateral: Decimal;
  indexPrice: number;
  weight: Decimal;
}) => {
  const { USDCBalance, collateralQty, freeCollateral, indexPrice, weight } =
    inputs;
  const usdcBalance = new Decimal(USDCBalance);
  const denominator = usdcBalance.isNegative()
    ? new Decimal(indexPrice).mul(weight).mul(new Decimal(1).add(0.002))
    : new Decimal(indexPrice).mul(weight);
  if (denominator.isZero()) {
    return zero;
  }
  const qty = new Decimal(collateralQty);
  const maxQtyByValue = new Decimal(freeCollateral).div(denominator);
  return maxQtyByValue.lt(qty) ? maxQtyByValue : qty;
};

export const calcMinimumReceived = (inputs: {
  amount: number;
  slippage: number;
}) => {
  const { amount, slippage } = inputs;
  const slippageRatio = new Decimal(slippage).div(100);
  return new Decimal(amount)
    .mul(new Decimal(1).minus(slippageRatio))
    .toNumber();
};
