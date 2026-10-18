export const DEFAULT_TICK_SIZES: Record<PropertyKey, string> = {
  PERP_BTC_USDC: "1",
  PERP_ETH_USDC: "0.1",
  PERP_SOL_USDC: "0.01",
};

export const DEFAULT_SYMBOL_DEPTHS: Record<PropertyKey, number[]> = {
  PERP_ETH_USDC: [0.01, 0.1, 0.5, 1, 10],
};
