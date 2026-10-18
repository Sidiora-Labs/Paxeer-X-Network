export enum InputType {
  NONE,
  PRICE, // price input focus
  TRIGGER_PRICE, // trigger price input focus
  QUANTITY, // quantity input focus
  QUANTITY_SLIDER, // quantity slider input focus
  TOTAL, // total input focus
  MARGIN, // margin input focus

  /**
   * Scaled order
   */
  START_PRICE, // scaled order start price input focus
  END_PRICE, // scaled order end price input focus
  TOTAL_ORDERS, // scaled order total orders input focus
  SKEW, // scaled order skew input focus

  /**
   * Trailing stop
   */
  ACTIVATED_PRICE, // trailing stop activated price input focus
  CALLBACK_VALUE, // trailing stop callback value input focus
  CALLBACK_RATE, // trailing stop callback rate input focus
}

export type QuantityInputType = InputType.QUANTITY | InputType.TOTAL | InputType.MARGIN;
