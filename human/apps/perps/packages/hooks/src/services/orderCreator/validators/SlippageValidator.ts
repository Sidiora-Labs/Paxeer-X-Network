import { PaxeerNetworkOrder } from "@paxeer-network/types";
import { OrderValidationItem, ValuesDepConfig } from "../interface";
import { BaseValidator } from "./BaseValidator";
import { SlippageValidationStrategy } from "./SlippageValidationStrategy";

/**
 * Validator for slippage using Strategy pattern
 * Implements Chain of Responsibility pattern
 */
export class SlippageValidator extends BaseValidator {
  private strategy: SlippageValidationStrategy;

  constructor() {
    super();
    this.strategy = new SlippageValidationStrategy();
  }

  protected doValidate(
    values: any,
    config: ValuesDepConfig,
  ): OrderValidationItem | undefined {
    return this.strategy.validate(
      {
        slippage: values.slippage,
      },
      config,
    );
  }

  protected getFieldName(): keyof PaxeerNetworkOrder {
    return "slippage";
  }
}
