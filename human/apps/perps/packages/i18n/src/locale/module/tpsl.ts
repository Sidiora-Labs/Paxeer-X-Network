export const tpsl = {
  "tpsl.tp": "TP",
  "tpsl.sl": "SL",
  "tpsl.mode": "Mode",
  "tpsl.fullPosition": "Full position",
  "tpsl.partialPosition": "Partial position",
  "tpsl.tpPrice": "TP Price",
  "tpsl.slPrice": "SL Price",
  "tpsl.tpPnl": "TP PnL",
  "tpsl.slPnl": "SL PnL",
  "tpsl.tpTrigger": "TP trigger",
  "tpsl.slTrigger": "SL trigger",
  "tpsl.pnl": "PnL",
  "tpsl.offsetHolder": "Offset",
  "tpsl.offset": "Offset (From entry)",
  "tpsl.offsetPercent": "Offset% (From entry)",
  "tpsl.add": "Add",
  "tpsl.cancelAll": "Cancel all",
  "tpsl.dragToSet": "Drag to set TP/SL",

  "tpsl.tpOrderPrice": "TP order price",
  "tpsl.slOrderPrice": "SL order price",
  "tpsl.tpTriggerPrice": "TP trigger price",
  "tpsl.slTriggerPrice": "SL trigger price",

  "tpsl.positionTpsl": "Position TP/SL",
  "tpsl.entirePosition": "Entire position",
  "tpsl.estPnl": "Est. PNL",
  "tpsl.takeProfit": "Take profit",
  "tpsl.stopLoss": "Stop loss",

  "tpsl.cancelOrder.description":
    "Are you sure you want to cancel this TP/SL order?",
  "tpsl.confirmOrder": "Confirm Order",
  "tpsl.advanced": "Advanced",
  "tpsl.advancedSetting": "Advanced setting",
  "tpsl.TPOrderConfirm": "TP order confirm",
  "tpsl.SLOrderConfirm": "SL order confirm",
  "tpsl.positionType.full": "TP/SL: Full position",
  "tpsl.positionType.partial": "TP/SL: Partial position",
  "tpsl.positionType.full.tips":
    "Full-position TP/SL closes the position using a market or limit order. Its quantity is determined when triggered. A triggered limit order keeps that quantity even if the position changes, and may not fill immediately.",
  "tpsl.positionType.partial.tips":
    "Partial TP/SL closes the specified quantity using a market or limit order when the mark price reaches the trigger price.",
  "tpsl.advanced.ROI":
    "When the mark price reaches <0/>, it will trigger a <1/> order, and estimated PnL will be <2/> and ROI is <3/>.",
  "tpsl.advanced.submit": "Submit",
  "tpsl.totalEstTpPnl": "Total est. TP PnL",
  "tpsl.totalEstSlPnl": "Total est. SL PnL",
  "tpsl.riskRewardRatio": "Risk reward ratio",

  "tpsl.validate.tpOrderPrice.error.required": "TP order price is required",
  "tpsl.validate.tpOrderPrice.error.min":
    "TP order price must be greater than {{value}}",
  "tpsl.validate.tpOrderPrice.error.max":
    "TP order price must be less than {{value}}",
  "tpsl.validate.slOrderPrice.error.required": "SL order price is required",
  "tpsl.validate.slOrderPrice.error.min":
    "SL order price must be greater than {{value}}",
  "tpsl.validate.slOrderPrice.error.max":
    "SL order price must be less than {{value}}",
  "tpsl.validate.tpTriggerPrice.error.priceErrorMin":
    "Your TP trigger price should be set lower than your order price.",
  "tpsl.validate.tpTriggerPrice.error.priceErrorMax":
    "Your TP trigger price should be set higher than your order price.",
  "tpsl.validate.slTriggerPrice.error.priceErrorMin":
    "Your SL trigger price should be set lower than your order price.",
  "tpsl.validate.slTriggerPrice.error.priceErrorMax":
    "Your SL trigger price should be set higher than your order price.",
  "tpsl.validate.tpTriggerPrice.error.required": "TP trigger price is required",
  "tpsl.validate.slTriggerPrice.error.required": "SL trigger price is required",
  "tpsl.validate.slTriggerPrice.warning.closeToLiqPrice":
    "Stop losses set near the liq. price may not trigger. Note: the liq. price can change with position notional.",
  "tpsl.validate.slTriggerPrice.error.crossLiqPrice":
    "Stop loss crosses the liq. price. Please adjust your SL.",

  "tpsl.tpslDetail.qty": "Qty.",
  "tpsl.tpslDetail.estPnl": "Est. PnL",
  "tpsl.tpslDetail.estPnl.tooltip":
    "The actual value may differ based on the actual trading price. This value is only for reference.",
  "tpsl.agreement": "You agree to edit your {{symbol}} order.",

  "tpsl.offsetMark": "Offset (From mark)",
  "tpsl.offsetPercentMark": "Offset% (From mark)",
};

export type TPSL = typeof tpsl;
