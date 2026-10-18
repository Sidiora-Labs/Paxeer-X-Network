export const markets = {
  "markets.favorites": "Favorites",
  "markets.recent": "Recent",
  "markets.newListings": "New listings",
  "markets.community": "Community",
  "markets.crypto": "Crypto",
  "markets.tradFi": "TradFi",
  "markets.preTge": "Pre-launch",
  "markets.allMarkets": "All markets",
  "markets.openInterest": "Open interest",
  "markets.openInterest.tooltip": "Total size of positions per side.",

  "markets.topGainers": "Top gainers",
  "markets.topLosers": "Top losers",

  "markets.search.placeholder": "Search market",
  "markets.showLowVolume": "Show low vol.",
  "markets.showLowVolume.tooltip":
    "Include markets with less than $1,000 in 24h trading volume.",
  "markets.showLowVolume.empty":
    "Matching markets are hidden by the 24h volume filter.",
  "markets.showLowVolume.action": "Show low-volume markets",
  "markets.dataList.favorites.empty":
    "Click on the <0/> icon next to a market to add it to your list.",

  "markets.dataList.column.8hFunding": "8h funding",
  "markets.dataList.column.moveTop": "Move to top",

  "markets.favorites.dropdown.title": "Select lists for",
  "markets.favorites.dropdown.addPlaceholder": "Add a new watchlist",
  "markets.favorites.tabs.maxList": "Maximum 10 groups in the favorite list",
  "markets.favorites.tabs.maxName": "List name cannot exceed 15 characters",
  "markets.favorites.tabs.delete.dialog.title": "Delete list",
  "markets.favorites.tabs.delete.dialog.description":
    "Are you sure you want to delete {{name}}?",
  "markets.favorites.addFavorites": "Add favorites",

  "markets.column.market": "Market",
  "markets.column.24hChange": "24h%",
  "markets.column.24hVolume": "24h volume",
  // TODO: use markets.column.market and common.volume
  "markets.column.market&Volume": "Market / Volume",
  // TODO: confirm "change" first letter capital
  "markets.column.price&Change": "Price / Change",
  "markets.column.last": "Last",
  "markets.column.24hPercentage": "24h %",
  "markets.column.24hVol": "24h Vol",
  "markets.column.OI": "OI",
  "markets.column.last&24hPercentage": "Last/24h %",
  "markets.column.mark": "Mark",
  "markets.column.index": "Index",
  "markets.column.mark&Index": "Mark / Index",

  "markets.funding.comparison": "Comparison",
  "markets.funding.column.estFunding": "Est. funding",
  "markets.funding.column.lastFunding": "Last funding",
  "markets.funding.column.1dAvg": "1D avg.",
  "markets.funding.column.3dAvg": "3D avg.",
  "markets.funding.column.7dAvg": "7D avg.",
  "markets.funding.column.14dAvg": "14D avg.",
  "markets.funding.column.30dAvg": "30D avg.",
  "markets.funding.column.90dAvg": "90D avg.",
  "markets.funding.column.positiveRate": "Positive rate",

  "markets.symbolInfoBar.lastPrice.tooltip":
    "The price of the most recent executed trade on this symbol.",
  "markets.symbolInfoBar.Mark": "Mark",
  "markets.symbolInfoBar.Mark.tooltip":
    "Price for the computation of unrealized PnL and liquidation.",
  "markets.symbolInfoBar.Index": "Index",
  "markets.symbolInfoBar.Index.tooltip":
    "Average of the last prices across other exchanges.",
  "markets.symbolInfoBar.24hVolume": "24h volume",
  "markets.symbolInfoBar.24hVolume.tooltip":
    "24 hour total trading volume on the paxeer-network Network.",
  "markets.symbolInfoBar.predFundingRate": "Est. funding rate",
  "markets.symbolInfoBar.predFundingRate.tooltip":
    "Funding rates are payments between traders who are long and short. When positive, long positions pay short positions funding. When negative, short positions pay long positions.",

  "markets.symbolInfoBar.riskNotice.content":
    "{{symbolWithBroker}} is a permissionless listing supported on Isolated Margin only, deployed and operated independently by {{brokerName}}. Please be aware of elevated risks, including low liquidity, high volatility, and increased liquidation risk.",
  "markets.symbolInfoBar.riskNotice.preLaunch.content":
    "{{symbolWithBroker}} is a Pre-launch permissionless listing supported on Isolated Margin only, deployed and operated independently by {{brokerName}}. The underlying token has not yet launched. Please be aware of elevated risks, including low liquidity, high volatility, and increased liquidation risk. No funding fee is charged during the pre-launch phase.",
};

export type Markets = typeof markets;
