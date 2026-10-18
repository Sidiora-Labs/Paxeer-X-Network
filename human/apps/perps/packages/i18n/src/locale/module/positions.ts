export const positions = {
  "positions.closePosition": "Close Position",
  "positions.fundingFee.title": "Funding Fee",
  "positions.fundingFee.tooltip":
    "Total funding fees from closed positions only, including partially closed portions.",
  "positions.unsettledFundingFee": "Unsettled funding fee",
  "positions.unsettledFundingFee.tooltip":
    "Funding fees accrued since the most recent settlement or position cost update. After settlement or position adjustments, the amount is incorporated into the position cost and starts accruing again. It does not represent the total funding fees since the position was opened.",
  "positions.fundingRate.tooltip":
    "Funding fee amount for each funding interval during the time the position was open. Records may include funding fees from both closed positions and currently open positions.",
  "positions.liquidation": "Liquidation",
  "positions.positionHistory": "Position history",
  "positions.closeAll": "Close all",
  "positions.closeAll.ofSymbol": "Close all of {{symbol}}",
  "positions.closeAll.description":
    "Are you sure you want to close all of your positions? They will be closed using market orders.",
  "positions.closeAll.success": "All positions closed successfully",
  "positions.closeAll.optionAll": "Close all positions",
  "positions.closeAll.optionProfit": "Close profitable positions",
  "positions.closeAll.optionLoss": "Close loss-making positions",
  "positions.closeAll.noPositions.profit": "No profitable positions to close.",
  "positions.closeAll.noPositions.loss": "No loss-making positions to close.",
  "positions.closeAll.popover.desc":
    "Select which positions you want to close. They will  be closed at the market price of the corresponding market.",

  "positions.column.liqPrice": "Liq. price",
  "positions.column.unrealPnl.tooltip":
    "Current unrealized profit or loss on your open positions across all widgets calculated using Mark Price.",
  "positions.column.unrealPnl.priceBasis": "Unrealized PnL Price Basis",
  "positions.column.margin": "Margin",
  "positions.column.margin.tooltip": "Isolated margin locked in the position",
  "positions.column.margin.formula":
    "Margin = Position size * Open price * IMR",
  "positions.column.close": "Close",

  "positions.limitClose": "Limit close",
  "positions.limitClose.description":
    "You agree closing {{quantity}} {{base}} position at limit price.",
  "positions.marketClose": "Market close",
  "positions.marketClose.description":
    "You agree closing {{quantity}} {{base}} position at market price.",
  "positions.limitClose.errors.exceed.title": "Close size limit exceeded",
  "positions.limitClose.errors.exceed.description":
    "Cannot close {{quantity}} {{symbol}} position. Max allowed per close is {{maxQuantity}} {{symbol}}.",

  "positions.history.status.closed": "Closed",
  "positions.history.status.partialClosed": "Partially closed",
  "positions.history.type.adl": "Adl",
  "positions.history.type.liquidated": "Liquidated",
  "positions.history.liquidated.liquidationId": "Liquidation id",
  "positions.history.liquidated.liquidatorFee": "Liquidator fee",
  "positions.history.liquidated.insFundFee": "Ins. Fund fee",

  "positions.history.column.closed": "Closed",
  "positions.history.column.maxClosed": "Max closed",
  "positions.history.column.closed&maxClosed": "Closed / Max closed",
  "positions.history.column.netPnl": "Net PnL",
  "positions.history.column.timeOpened": "Time opened",
  "positions.history.column.timeClosed": "Time closed",
  "positions.history.column.updatedTime": "Updated time",
  "positions.history.netPnl.tradingFee": "Trading fee",

  "positions.Liquidation.column.liquidationId": "Liquidation id",
  "positions.Liquidation.column.insFundTransfer": "Ins. fund transfer",
  "positions.Liquidation.column.liquidationFee": "Liquidation fee",
  "positions.Liquidation.column.liquidationFeeRate": "Liquidation fee rate",
  "positions.Liquidation.column.markPrice": "Liq. mark price",

  "positions.Liquidation.tooltip.liquidation":
    "An account is subject to liquidation if its Account Margin Ratio falls below its Maintenance Margin Ratio.",
  "positions.Liquidation.tooltip.viewMore": "View more",
  "positions.Liquidation.col.tooltip.feeRate":
    "The percentage charged for this liquidation, covering both the liquidator’s fee and the insurance fund contribution. This rate varies by symbol.",
  "positions.Liquidation.col.tooltip.fee":
    "The total fee charged for this liquidation, including both the liquidator’s fee and the insurance fund contribution.",

  "positions.Liquidation.expand.label.mr": "Margin ratio",
  "positions.Liquidation.expand.label.mmr": "Maint. margin ratio",
  "positions.Liquidation.expand.label.collateral": "Collateral value",
  "positions.Liquidation.expand.label.notional": "Position notional",
  "positions.Liquidation.expand.tooltip.mr":
    "The ratio of collateral to position size at the time of liquidation.",
  "positions.Liquidation.expand.tooltip.mmr":
    "The minimum margin required to keep the position open.",
  "positions.Liquidation.expand.tooltip.collateral":
    "Total collateral value in the account when liquidation occurred.",
  "positions.Liquidation.expand.tooltip.notional":
    "The total notional value of positions in the account at liquidation.",

  "positions.reverse.title": "Reverse Position",
  "positions.reverse.description":
    "Close your current position and open the opposite at market price. If your account balance isn't enough, the reverse order will fail. Orders exceeding the maximum quantity will be automatically split into multiple smaller orders.",
  "positions.reverse.error.belowMin":
    "The reverse order quantity is below the minimum allowed. Please close your position and place a new order manually.",
  "positions.reverse.marketCloseLong": "Market Close Long",
  "positions.reverse.marketCloseShort": "Market Close Short",
  "positions.reverse.marketOpenLong": "Market Open Long",
  "positions.reverse.marketOpenShort": "Market Open Short",
  "positions.reverse.reverseToLong": "Reverse to Long",
  "positions.reverse.reverseToShort": "Reverse to Short",

  // Adjust Margin
  "positions.adjustMargin.title": "Adjust Margin",
  "positions.adjustMargin.add": "Add Margin",
  "positions.adjustMargin.reduce": "Reduce Margin",
  "positions.adjustMargin.quantity": "Quantity",
  "positions.adjustMargin.max": "Max",
  "positions.adjustMargin.currentMargin": "Current margin:",
  "positions.adjustMargin.liqPriceAfter": "Liquidation price after adjustment:",
  "positions.adjustMargin.leverageAfter":
    "Effective leverage after adjustment:",
  "positions.adjustMargin.success": "Margin adjusted successfully",
  "positions.adjustMargin.failed": "Failed to adjust margin",
  "positions.adjustMargin.marginCannotMoreThanMax":
    "Margin cannot be more than max",
};

export type Positions = typeof positions;
