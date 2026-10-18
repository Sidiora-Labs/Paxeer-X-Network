/**
 * Trading page panel ID constants.
 * Used across layout strategies (split, grid) to identify trading panels.
 * Component mapping happens in the trading package.
 */
export const TRADING_PANEL_IDS = {
  SYMBOL_INFO_BAR: "symbolInfoBar",
  TRADING_VIEW: "tradingView",
  ORDERBOOK: "orderbook",
  DATA_LIST: "dataList",
  ORDER_ENTRY: "orderEntry",
  MARGIN: "orderEntryMargin",
  ASSETS: "orderEntryAssets",
  // MAIN: "orderEntryMain",
  MARKETS: "markets",
  HORIZONTAL_MARKETS: "horizontalMarkets",
} as const;

export type TradingPanelId =
  (typeof TRADING_PANEL_IDS)[keyof typeof TRADING_PANEL_IDS];

/**
 * Returns all trading panel IDs from layout-core.
 * Used by layout strategies (split, grid) to derive panel set; component mapping is in trading package.
 */
export function getTradingPanelIds(): readonly string[] {
  return Object.values(TRADING_PANEL_IDS);
}
