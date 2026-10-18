import { OrderEntity, OrderType, PaxeerNetworkOrder } from "@paxeer-network/types";
import { BaseOrderCreator } from "./baseCreator";
import { ValuesDepConfig, OrderValidationResult } from "./interface";
import { QuantityValidator } from "./validators/QuantityValidator";
import { SlippageValidationStrategy } from "./validators/SlippageValidationStrategy";
import { SlippageValidator } from "./validators/SlippageValidator";
import { ValidationChain } from "./validators/ValidationChain";

/**
 * Creator for market orders
 * Uses Strategy pattern for validation and Template Method pattern for creation
 */
export class MarketOrderCreator extends BaseOrderCreator<OrderEntity> {
  private slippageValidationStrategy = new SlippageValidationStrategy();
  private validationChain = new ValidationChain()
    .addValidator(new QuantityValidator())
    .addValidator(new SlippageValidator());

  /**
   * Builds the market order
   * Implements template method hook
   */
  protected buildOrder(
    values: OrderEntity,
    config?: ValuesDepConfig,
  ): OrderEntity {
    // Cast to PaxeerNetworkOrder for baseOrder method which expects PaxeerNetworkOrder
    const paxeerNetworkValues = values as unknown as PaxeerNetworkOrder;
    const data = this.baseOrder(paxeerNetworkValues, config);

    // Remove fields that should not be in market orders
    // Using type assertion to safely delete optional properties
    const result = { ...data } as Partial<PaxeerNetworkOrder>;
    delete result.order_price;
    delete result.total;
    delete result.trigger_price;
    // isStopOrder might not be in the type definition but could exist at runtime
    if ("isStopOrder" in result) {
      delete (result as any).isStopOrder;
    }

    return result as OrderEntity;
  }

  /**
   * Runs validations using validation chain
   * Implements template method hook
   */
  protected runValidations(
    values: OrderEntity,
    configs: ValuesDepConfig,
  ): OrderValidationResult {
    // Cast to PaxeerNetworkOrder for baseValidate method which expects PaxeerNetworkOrder
    const paxeerNetworkValues = values as unknown as PaxeerNetworkOrder;
    const result = this.baseValidate(paxeerNetworkValues, configs);

    // Use validation chain for quantity and slippage validation
    const chainErrors = this.validationChain.validate(paxeerNetworkValues, configs);
    Object.assign(result, chainErrors);

    return result;
  }

  orderType = OrderType.MARKET;
}
