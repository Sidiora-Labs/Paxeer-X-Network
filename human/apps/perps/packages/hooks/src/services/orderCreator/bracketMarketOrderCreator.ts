import { PaxeerNetworkOrder, OrderType } from "@paxeer-network/types";
import { bracketOrderValidator } from "./baseBracketOrderCreator";
import { ValuesDepConfig, OrderValidationResult } from "./interface";
import { MarketOrderCreator } from "./marketOrderCreator";

export class BracketMarketOrderCreator extends MarketOrderCreator {
  orderType = OrderType.MARKET;
  create(values: PaxeerNetworkOrder, config?: ValuesDepConfig) {
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

    const bracketData = await bracketOrderValidator(values as any, config);

    return { ...value, ...bracketData };
  }
}
