import { pick } from "ramda";
import { OrderEntity, OrderType, PaxeerNetworkOrder } from "@paxeer-network/types";
import { BaseOrderCreator } from "./baseCreator";
import { ValuesDepConfig, OrderValidationResult } from "./interface";
import { PriceValidationStrategy } from "./validators/PriceValidationStrategy";
import { PriceValidator } from "./validators/PriceValidator";
import { QuantityValidator } from "./validators/QuantityValidator";
import { ValidationChain } from "./validators/ValidationChain";

/**
 * Creator for limit orders
 * Uses Strategy pattern for validation and Template Method pattern for creation
 */
export class LimitOrderCreator<
  T extends OrderEntity = PaxeerNetworkOrder,
> extends BaseOrderCreator<T> {
  // private priceValidationStrategy = new PriceValidationStrategy();
  private validationChain = new ValidationChain()
    .addValidator(new QuantityValidator())
    .addValidator(new PriceValidator());

  /**
   * Builds the limit order
   * Implements template method hook
   */
  protected buildOrder(values: T, config?: ValuesDepConfig): T {
    // Cast to PaxeerNetworkOrder for baseOrder method which expects PaxeerNetworkOrder
    const paxeerNetworkValues = values as unknown as PaxeerNetworkOrder;
    const order = {
      ...this.baseOrder(paxeerNetworkValues, config),
      order_price: paxeerNetworkValues.order_price,
    };

    this.totalToQuantity(order, config!);

    // Use unknown as intermediate type to safely convert pick result to T
    return pick(
      [
        "symbol",
        "order_price",
        "order_quantity",
        "visible_quantity",
        "reduce_only",
        "side",
        "order_type",
        "margin_mode",
        "algo_type",
        "child_orders",
      ],
      order,
    ) as unknown as T;
  }

  /**
   * Runs validations using validation chain
   * Implements template method hook
   */
  protected runValidations(
    values: T,
    config: ValuesDepConfig,
  ): OrderValidationResult {
    // Cast to PaxeerNetworkOrder for baseValidate method which expects PaxeerNetworkOrder
    const paxeerNetworkValues = values as unknown as PaxeerNetworkOrder;
    const errors = this.baseValidate(paxeerNetworkValues, config);

    // Use validation chain for price and quantity validation
    const chainErrors = this.validationChain.validate(paxeerNetworkValues, config);
    Object.assign(errors, chainErrors);

    return errors;
  }

  orderType = OrderType.LIMIT;
}
