import { Decimal, zero } from "@paxeer-network/utils";

export const LTV = (params: {
  usdcBalance: number;
  upnl: number;
  assets: Array<{ qty: number; indexPrice: number; weight: number }>;
}) => {
  const { usdcBalance, upnl, assets } = params;

  const usdcLoss = new Decimal(Math.min(usdcBalance, 0)).abs();
  const upnlLoss = new Decimal(Math.min(upnl, 0)).abs();
  const numerator = usdcLoss.add(upnlLoss);

  const collateralSum = assets.reduce<Decimal>((acc, asset) => {
    return acc.add(
      new Decimal(Math.max(asset.qty, 0))
        .mul(new Decimal(asset.indexPrice))
        .mul(new Decimal(asset.weight)),
    );
  }, zero);

  const denominator = collateralSum.add(new Decimal(Math.max(upnl, 0)));

  if (numerator.isZero() || denominator.isZero()) {
    return 0;
  }

  return numerator.div(denominator).toNumber();
};
