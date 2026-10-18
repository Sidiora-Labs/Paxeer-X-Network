import { pick } from "ramda";
import {
  AlgoOrderEntity,
  AlgoOrderRootType,
  OrderEntity,
  OrderType,
  TriggerPriceType,
} from "@paxeer-network/types";
import { BaseOrderCreator } from "./baseCreator";
import {
  OrderFormEntity,
  ValuesDepConfig,
  OrderValidationResult,
} from "./interface";
import { OrderValidation } from "./orderValidation";
import { TriggerPriceValidationStrategy } from "./validators/PriceValidationStrategy";
import { QuantityValidator } from "./validators/QuantityValidator";
import { TriggerPriceValidator } from "./validators/TriggerPriceValidator";
import { ValidationChain } from "./validators/ValidationChain";

/**
 * Creator for stop-market orders
 * Uses Strategy pattern for validation and Template Method pattern for creation
 */
export class StopMarketOrderCreator extends BaseOrderCreator<AlgoOrderEntity> {
  orderType=OrderType.STOP_MARKET;
  private triggerPriceValidationStrategy = new TriggerPriceValidationStrategy();
  private validationChain = new ValidationChain()
    .addValidator(new QuantityValidator())
    .addValidator(new TriggerPriceValidator());

  /**
   * Builds the stop-market order
   * Implements template method hook
   */
  protected buildOrder(
    values: AlgoOrderEntity & {
      order_quantity: number;
      order_price: number;
    },
  ) {
    const order = {
      ...this.baseOrder(values as unknown as OrderEntity),
      trigger_price: values.trigger_price!,
      algo_type: AlgoOrderRootType.STOP,
      type: OrderType.MARKET,
      quantity: values["order_quantity"]!,
      trigger_price_type: TriggerPriceType.MARK_PRICE,
    };

    return pick(
      [
        "symbol",
        "trigger_price",
        "algo_type",
        "type",
        "quantity",
        "trigger_price_type",
        "side",
        "reduce_only",
        "margin_mode",
        "visible_quantity",
      ],
      order,
    );
  }

  /**
   * Runs validations using validation chain
   * Implements template method hook
   */
  protected runValidations(
    values: OrderFormEntity,
    config: ValuesDepConfig,
  ): OrderValidationResult {
    const errors = this.baseValidate(values, config);

    // Use validation chain for trigger price and quantity validation
    const chainErrors = this.validationChain.validate(values, config);
    Object.assign(errors, chainErrors);

    return errors;
  }
}
