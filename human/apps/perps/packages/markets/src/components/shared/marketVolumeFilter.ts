const MIN_24H_VOLUME = 1000;

export const isMarketAboveMinVolume = (volume: unknown): boolean =>
  typeof volume === "number" &&
  Number.isFinite(volume) &&
  volume >= MIN_24H_VOLUME;

export function filterMarketsByLowVolume<
  T extends { "24h_amount"?: number | null },
>(markets: T[], showLowVolume: boolean) {
  const visibleMarkets = showLowVolume
    ? markets
    : markets.filter((market) => isMarketAboveMinVolume(market["24h_amount"]));

  return {
    visibleMarkets,
    allHidden: markets.length > 0 && visibleMarkets.length === 0,
  };
}
