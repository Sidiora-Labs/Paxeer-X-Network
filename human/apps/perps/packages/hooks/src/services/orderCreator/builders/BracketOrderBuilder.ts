import {
  AlgoOrderRootType,
  AlgoOrderChildOrders,
  ChildOrder,
  AlgoOrderType,
  OrderType,
  OrderSide,
  PositionType,
} from "@paxeer-network/types";
import { resolveTPSLOrderType } from "@paxeer-network/utils";
import { OrderBuilder } from "./OrderBuilder";

/**
 * Builder for bracket orders (orders with TP/SL)
 * Extends OrderBuilder with bracket-specific functionality
 */
export class BracketOrderBuilder extends OrderBuilder {
  private tpTriggerPrice?: number | string;
  private tpOrderPrice?: number | string;
  private slTriggerPrice?: number | string;
  private slOrderPrice?: number | string;
  private positionType?: PositionType;

  /**
   * Sets the take profit trigger price
   * @param triggerPrice - TP trigger price
   * @returns This builder instance for method chaining
   */
  withTPTrigger(triggerPrice: number | string): this {
    this.tpTriggerPrice = triggerPrice;
    return this;
  }

  /**
   * Sets the take profit order price (for limit TP orders)
   * @param orderPrice - TP order price
   * @returns This builder instance for method chaining
   */
  withTPOrderPrice(orderPrice: number | string): this {
    this.tpOrderPrice = orderPrice;
    return this;
  }

  /**
   * Sets the stop loss trigger price
   * @param triggerPrice - SL trigger price
   * @returns This builder instance for method chaining
   */
  withSLTrigger(triggerPrice: number | string): this {
    this.slTriggerPrice = triggerPrice;
    return this;
  }

  /**
   * Sets the stop loss order price (for limit SL orders)
   * @param orderPrice - SL order price
   * @returns This builder instance for method chaining
   */
  withSLOrderPrice(orderPrice: number | string): this {
    this.slOrderPrice = orderPrice;
    return this;
  }

  /**
   * Sets the position type (FULL or PARTIAL)
   * @param positionType - Position type
   * @returns This builder instance for method chaining
   */
  withPositionType(positionType: PositionType): this {
    this.positionType = positionType;
    return this;
  }

  /**
   * Builds the bracket order with child orders
   * @returns The constructed bracket order
   */
  override build(): any {
    const baseOrder = super.build();
    const bracketOrder = this.buildBracketOrder();

    if (!bracketOrder) {
      return baseOrder;
    }

    return {
      ...baseOrder,
      algo_type: AlgoOrderRootType.BRACKET,
      child_orders: [bracketOrder],
    };
  }

  /**
   * Builds the bracket order child orders
   * @returns Child orders or null if no TP/SL set
   */
  private buildBracketOrder(): AlgoOrderChildOrders | null {
    const orders: ChildOrder[] = [];
    const side =
      this.order.side === OrderSide.BUY ? OrderSide.SELL : OrderSide.BUY;
    const algoType: AlgoOrderRootType =
      this.positionType === PositionType.PARTIAL
        ? AlgoOrderRootType.TP_SL
        : AlgoOrderRootType.POSITIONAL_TP_SL;

    // Add TP order if trigger price is set
    if (this.tpTriggerPrice) {
      const tpOrder: ChildOrder = {
        algo_type: AlgoOrderType.TAKE_PROFIT,
        side: side,
        type: this.getChildOrderType(this.positionType, this.tpOrderPrice),
        trigger_price: this.tpTriggerPrice,
        symbol: this.order.symbol!,
        reduce_only: true,
      };

      if (this.tpOrderPrice) {
        tpOrder.price = this.tpOrderPrice;
      }

      orders.push(tpOrder);
    }

    // Add SL order if trigger price is set
    if (this.slTriggerPrice) {
      const slOrder: ChildOrder = {
        algo_type: AlgoOrderType.STOP_LOSS,
        side: side,
        type: this.getChildOrderType(this.positionType, this.slOrderPrice),
        trigger_price: this.slTriggerPrice,
        symbol: this.order.symbol!,
        reduce_only: true,
      };

      if (this.slOrderPrice) {
        slOrder.price = this.slOrderPrice;
      }

      orders.push(slOrder);
    }

    if (orders.length === 0) {
      return null;
    }

    return {
      symbol: this.order.symbol!,
      algo_type: algoType,
      child_orders: orders,
    };
  }

  /**
   * Determines child order type based on position type and order price
   * @param positionType - Position type
   * @param orderPrice - Order price (if limit order)
   * @returns Order type for child order
   */
  private getChildOrderType(
    positionType?: PositionType,
    orderPrice?: number | string,
  ): OrderType {
    return resolveTPSLOrderType(
      undefined,
      orderPrice,
      positionType !== PositionType.PARTIAL,
    );
  }

  /**
   * Resets the builder including bracket-specific fields
   * @returns This builder instance for method chaining
   */
  override reset(): this {
    super.reset();
    this.tpTriggerPrice = undefined;
    this.tpOrderPrice = undefined;
    this.slTriggerPrice = undefined;
    this.slOrderPrice = undefined;
    this.positionType = undefined;
    return this;
  }
}
