export const leverage = {
  "leverage.maxAccountLeverage": "Max account leverage",
  "leverage.accountLeverage": "Account leverage",
  "leverage.currentLeverage": "Current leverage",
  "leverage.maxLeverage": "Max leverage",
  "leverage.updated": "Leverage updated",
  "leverage.adjustedLeverage": "Adjust leverage",
  "leverage.maxPositionLeverage.tips":
    "* Maximum position at current leverage: <0>{{amount}}</0> USDC",
  "leverage.maxAvailableLeverage.tips":
    "* Highest available leverage <0>{{leverage}}</0>. Selecting higher leverage increases your liquidation risk.",
  "leverage.overRequiredMargin.tips":
    "Margin is not enough. Please try adjusting to another leverage level.",
  "leverage.overMaxPositionLeverage.tips":
    "Current highest available leverage is <0>{{leverage}}</0>. Please try adjusting to another leverage level.",
  "leverage.confirm": "Leverage confirm",
  "leverage.confirm.content":
    "If you modify the leverage, it will also affect your ongoing positions on this symbol.",
  "leverage.confirm.disable.confirmation": "Disable confirmation",
  "leverage.actualPositionLeverage.tips":
    "* Actual position leverage adjusts with notional changes and may fall below your selected leverage.",
};

export type Leverage = typeof leverage;
