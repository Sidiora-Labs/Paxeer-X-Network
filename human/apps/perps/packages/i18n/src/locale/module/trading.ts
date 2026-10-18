export const trading = {
  "trading.layout": "Layout",
  "trading.layout.advanced": "Advanced",
  "trading.layout.advanced.right": "Right",
  "trading.layout.advanced.left": "Left",
  "trading.layout.markets": "Markets",
  "trading.layout.markets.left": "Left",
  "trading.layout.markets.top": "Top",
  "trading.layout.markets.bottom": "Bottom",
  "trading.layout.markets.hide": "Hide",
  "trading.orders.closeAll": "Close All",
  "trading.hideOtherSymbols": "Hide other symbols",
  "trading.history": "History",

  "trading.portfolioSettings": "Preference",
  "trading.portfolioSettings.decimalPrecision":
    "Decimal Precision for PnL & Notional",
  "trading.portfolioSettings.unrealPnlPriceBasis": "Unrealized PnL Price Basis",
  "trading.portfolioSettings.reversePosition": "Reverse button",
  "trading.portfolioSettings.reversePosition.tooltip":
    "Close your current position and open the opposite at market price. If your account balance isn't enough, the reverse order will fail. Orders exceeding the maximum quantity will be automatically split into multiple smaller orders.",

  "trading.orderBook": "Order book",
  "trading.lastTrades": "Last trades",
  "trading.orderBook.column.value": "Value",
  "trading.orderBook.settings": "Settings",
  "trading.orderBook.showBuySellRatio": "Show buy/sell ratio",

  "trading.tabs.chart": "Chart",
  "trading.tabs.trades": "Trades",
  "trading.tabs.data": "Data",

  "trading.column.24High": "24h high",
  "trading.column.24Low": "24h low",

  "trading.orderBook.sum": "Sum",
  "trading.orderBook.markPrice.tooltip":
    "Obtained from a third-party oracle, the mark price is calculated as the median of three prices: the last price, the fair price based on the funding rate basis, and the fair price based on the order books.",
  "trading.orderBook.spreadRatio.tooltip": "Spread Ratio of the ask1 and bid1.",
  "trading.orderBook.middlePrice.tooltip":
    "A stabilized reference price calculated as the median of the best bid, best ask, and 24-hour closing price.",

  "trading.faucet.getTestUSDC": "Get test USDC",
  "trading.faucet.getTestUSDC.success":
    "{{quantity}} USDC will be added to your balance. Please note this may take up to 3 minutes. Please check back later.",

  "trading.asset.startTrading": "Start trading",
  "trading.asset.startTrading.description":
    "You can deposit assets from various networks",

  "trading.asset.myAssets": "My Assets",

  "trading.asset.freeCollateral": "Free collateral",
  "trading.asset.freeCollateral.tooltip":
    "Free collateral for placing new orders.",
  "trading.asset.freeCollateral.formula":
    "Free collateral = Total balance + Total unsettlement PnL - Total position initial margin",

  "trading.asset.marginRatio": "Margin ratio",
  "trading.asset.marginRatio.tooltip":
    "The margin ratio represents the proportion of collateral relative to the total position value.",
  "trading.asset.marginRatio.formula":
    "Account margin ratio = (Total collateral value / Total position notional) * 100%",

  "trading.asset.unsettledPnl": "Unsettled PnL",
  "trading.asset.free&TotalCollateral": "Free / Total Collateral",
  "trading.asset.availableBalance": "Available Balance",

  "trading.asset.maintenanceMarginRatio": "Maintenance margin ratio",
  "trading.asset.maintenanceMarginRatio.tooltip":
    "The minimum margin ratio required to protect your positions from being liquidated. If the Margin ratio falls below the Maintenance margin ratio, the account will be liquidated.",
  "trading.asset.maintenanceMarginRatio.formula":
    "Account maintenance margin ratio = Sum(Position notional * Symbol maintenance Margin Ratio)  / Total position notional * 100%",

  "trading.asset.maintenanceMargin": "Maintenance margin",
  "trading.asset.maintenanceMargin.tooltip":
    "Maintenance margin is the minimum amount of margin required to keep all your open positions alive. If your account equity falls below the maintenance margin, the account will be liquidated.",
  "trading.asset.maintenanceMargin.formula":
    "Maintenance margin = Sum(Position notional * Symbol maintenance margin ratio)",

  "trading.asset.currentLeverage": "Current leverage",
  "trading.asset.currentLeverage.tooltip":
    "Current Account Leverage shows how much leverage you are currently using based on your open positions.",
  "trading.asset.currentLeverage.formula":
    "Current leverage = (Total position notional / Total collateral value)",

  "trading.riskRate": "Risk rate",
  "trading.riskRate.tooltip":
    "The Risk rate is used to assess the risk level of an account. When the Risk rate reaches 100%, the account will be liquidated",
  "trading.riskRate.formula":
    "Risk rate = Maintenance margin ratio / Margin ratio * 100%",

  "trading.asset&Margin": "Asset & Margin",

  "trading.fundingRate.predFundingRate": "Est. funding rate",
  "trading.fundingRate.estFundingRateWithTimeframe":
    "Est. funding rate ({{timeframe}})",
  "trading.fundingRate.predFundingRate.interval": "Interval",
  "trading.fundingRate.predFundingRate.cap": "Funding cap",
  "trading.fundingRate.predFundingRate.floor": "floor",
  "trading.fundingRate.lastFundingRate": "Last funding rate",
  "trading.fundingRate.estimatedFundingRate": "Estimated funding rate",
  "trading.fundingRate.estimatedFundingFee": "Estimated funding fee",
  "trading.fundingRate.annualized": "Annualized",

  "trading.rwa.marketHours": "Market on",
  "trading.rwa.outsideMarketHours": "Market off",
  "trading.rwa.tooltip.description.open":
    "Trading aligns with the underlying market - higher liquidity, normal price movements.",
  "trading.rwa.tooltip.description.close":
    "Trading is available, but liquidity may be lower and price movement slower.",
  "trading.rwa.tooltip.closeIn":
    "Regular trading hours will close in <0>{{timeFormat}}</0>",
  "trading.rwa.tooltip.openIn":
    "Regular trading hours will open in <0>{{timeFormat}}</0>",
  "trading.rwa.tooltip.checkDetailRules": "Check detail rules",
  "trading.rwa.countdown.title":
    "US markets are closing soon, volatility may be lower.",
  "trading.symbolDelisting":
    "{{symbol}} has entered forced deleveraging mode (insufficient liquidity depth / insufficient IF balance). Your position on this symbol only allows closing operations.",
  "trading.rwa.outsideMarketHours.notify":
    "This market is currently outside regular trading hours. You can still place a trade, but please be aware of reduced liquidity and potential risks.",
  "trading.rwa.mWeb.outsideMarketHours.desc":
    "Regular trading hours are about to close - 24/7 trading continues; price may slow and liquidity may be lower.",
  "trading.rwa.mWeb.insideMarketHours.desc":
    "Regular trading hours are about to open - get ready.",
};

export type Trading = typeof trading;
