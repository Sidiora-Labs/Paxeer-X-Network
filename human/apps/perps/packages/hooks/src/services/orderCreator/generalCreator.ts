import { OrderEntity } from "@paxeer-network/types";
import { BaseOrderCreator } from "./baseCreator";
import {
  OrderFormEntity,
  ValuesDepConfig,
  OrderValidationResult,
} from "./interface";

/**
 * General order creator for basic orders
 * Uses Template Method pattern from BaseOrderCreator
 */
export class GeneralOrderCreator extends BaseOrderCreator<OrderEntity> {
  /**
   * Builds the general order
   * Implements template method hook
   */
  protected buildOrder(data: OrderEntity): OrderEntity {
    return {
      ...this.baseOrder(data),
      order_price: data.order_price,
      order_quantity: data.order_quantity,
    };
  }

  /**
   * Runs base validations
   * Implements template method hook
   */
  protected runValidations(
    values: OrderFormEntity,
    configs: ValuesDepConfig,
  ): OrderValidationResult {
    return super.baseValidate(values, configs);
  }

  orderType: any = undefined;
}
