import { PaxeerNetworkOrder } from "@paxeer-network/types";
import { OrderValidationItem, ValuesDepConfig } from "../interface";
import { BaseValidator } from "./BaseValidator";
import { PriceValidationStrategy } from "./PriceValidationStrategy";

/**
 * Validator for order price using Strategy pattern
 * Implements Chain of Responsibility pattern
 */
export class PriceValidator extends BaseValidator {
  private strategy: PriceValidationStrategy;

  constructor() {
    super();
    this.strategy = new PriceValidationStrategy();
  }

  protected doValidate(
    values: any,
    config: ValuesDepConfig,
  ): OrderValidationItem | undefined {
    return this.strategy.validate(
      {
        order_price: values.order_price,
        side: values.side,
        order_type: values.order_type,
      },
      config,
    );
  }

  protected getFieldName(): keyof PaxeerNetworkOrder {
    return "order_price";
  }
}
