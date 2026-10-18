import { PaxeerNetworkOrder } from "@paxeer-network/types";
import { OrderValidationItem, ValuesDepConfig } from "../interface";
import { BaseValidator } from "./BaseValidator";
import { TriggerPriceValidationStrategy } from "./PriceValidationStrategy";

/**
 * Validator for trigger price using Strategy pattern
 * Implements Chain of Responsibility pattern
 */
export class TriggerPriceValidator extends BaseValidator {
  private strategy: TriggerPriceValidationStrategy;

  constructor() {
    super();
    this.strategy = new TriggerPriceValidationStrategy();
  }

  protected doValidate(
    values: any,
    config: ValuesDepConfig,
  ): OrderValidationItem | undefined {
    return this.strategy.validate(
      {
        trigger_price: values.trigger_price,
      },
      config,
    );
  }

  protected getFieldName(): keyof PaxeerNetworkOrder {
    return "trigger_price";
  }
}
