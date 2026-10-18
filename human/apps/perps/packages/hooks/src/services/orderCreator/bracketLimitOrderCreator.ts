import { OrderEntity, OrderType, PaxeerNetworkOrder } from "@paxeer-network/types";
import { BracketOrder } from "@paxeer-network/types";
import { bracketOrderValidator } from "./baseBracketOrderCreator";
import { ValuesDepConfig, OrderValidationResult } from "./interface";
import { LimitOrderCreator } from "./limitOrderCreator";

export class BracketLimitOrderCreator extends LimitOrderCreator {
  // orderType: OrderType;
  create(values: PaxeerNetworkOrder, config?: ValuesDepConfig): BracketOrder {
    const order = super.create(values, config);

    return {
      ...order,
      quantity: order.order_quantity,
      type: order.order_type,
      price: order.order_price,
    };
  }

  async validate(
    values: PaxeerNetworkOrder,
    config: ValuesDepConfig,
  ): Promise<OrderValidationResult> {
    const value = await super.validate(values, config);

    // bracketOrderValidator is async, so we need to await it
    const bracketData = await bracketOrderValidator(values as any, config);

    return { ...value, ...bracketData };
  }
}
