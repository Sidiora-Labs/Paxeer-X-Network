import { PaxeerNetworkOrder } from "@paxeer-network/types";
import { OrderValidationItem, ValuesDepConfig } from "../interface";
import { BaseValidator } from "./BaseValidator";
import { QuantityValidationStrategy } from "./QuantityValidationStrategy";

/**
 * Validator for order quantity using Strategy pattern
 * Implements Chain of Responsibility pattern
 */
export class QuantityValidator extends BaseValidator {
  private strategy: QuantityValidationStrategy;

  constructor() {
    super();
    this.strategy = new QuantityValidationStrategy();
  }

  protected doValidate(
    values: any,
    config: ValuesDepConfig,
  ): OrderValidationItem | undefined {
    return this.strategy.validate(
      {
        order_quantity: values.order_quantity,
        total: values.total,
        order_price: values.order_price,
      },
      config,
    );
  }

  protected getFieldName(): keyof PaxeerNetworkOrder {
    return "order_quantity";
  }
}
