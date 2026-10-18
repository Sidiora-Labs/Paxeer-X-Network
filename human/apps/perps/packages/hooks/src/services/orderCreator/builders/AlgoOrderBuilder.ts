import {
  AlgoOrderEntity,
  AlgoOrderRootType,
  ChildOrder,
  AlgoOrderType,
  OrderType,
  OrderSide,
  TriggerPriceType,
} from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { ValuesDepConfig } from "../interface";
import { OrderBuilder } from "./OrderBuilder";

/**
 * Builder for algo orders (stop orders, trailing stop, etc.)
 * Extends OrderBuilder with algo-specific functionality
 */
export class AlgoOrderBuilder extends OrderBuilder {
  private triggerPrice?: number | string;
  private triggerPriceType: TriggerPriceType = TriggerPriceType.MARK_PRICE;
  private algoType?: AlgoOrderRootType;
  private quantity?: number | string;

  /**
   * Sets the trigger price for stop orders
   * @param triggerPrice - Trigger price
   * @returns This builder instance for method chaining
   */
  withTriggerPrice(triggerPrice: number | string): this {
    this.triggerPrice = triggerPrice;
    return this;
  }

  /**
   * Sets the trigger price type
   * @param triggerPriceType - Type of trigger price (MARK_PRICE, LAST_PRICE, etc.)
   * @returns This builder instance for method chaining
   */
  withTriggerPriceType(triggerPriceType: TriggerPriceType): this {
    this.triggerPriceType = triggerPriceType;
    return this;
  }

  /**
   * Sets the algo order type
   * @param algoType - Algo order root type (STOP, TRAILING_STOP, etc.)
   * @returns This builder instance for method chaining
   */
  withAlgoType(algoType: AlgoOrderRootType): this {
    this.algoType = algoType;
    return this;
  }

  /**
   * Sets the quantity for algo orders
   * Note: Algo orders use 'quantity' instead of 'order_quantity'
   * @param quantity - Order quantity
   * @returns This builder instance for method chaining
   */
  withQuantity(quantity: number | string): this {
    this.quantity = quantity;
    return this;
  }

  /**
   * Builds the algo order
   * @returns The constructed algo order entity
   */
  override build(): AlgoOrderEntity {
    const baseOrder = super.build();

    const algoOrder: Partial<AlgoOrderEntity> = {
      ...baseOrder,
      algo_type: this.algoType || AlgoOrderRootType.STOP,
      trigger_price: this.triggerPrice,
      trigger_price_type: this.triggerPriceType,
      quantity: this.quantity || baseOrder.order_quantity,
      type: baseOrder.order_type || OrderType.LIMIT,
    };

    // Remove regular order fields that don't apply to algo orders
    delete (algoOrder as any).order_quantity;
    delete (algoOrder as any).order_price;

    return algoOrder as AlgoOrderEntity;
  }

  /**
   * Builds a stop limit order
   * @param orderPrice - Limit order price
   * @returns The constructed stop limit order
   */
  buildStopLimit(orderPrice: number | string): AlgoOrderEntity {
    this.withAlgoType(AlgoOrderRootType.STOP);
    const algoOrder = this.build() as any;
    algoOrder.price = orderPrice;
    algoOrder.type = OrderType.LIMIT;
    return algoOrder;
  }

  /**
   * Builds a stop market order
   * @returns The constructed stop market order
   */
  buildStopMarket(): AlgoOrderEntity {
    this.withAlgoType(AlgoOrderRootType.STOP);
    const algoOrder = this.build() as any;
    algoOrder.type = OrderType.MARKET;
    return algoOrder;
  }

  /**
   * Resets the builder including algo-specific fields
   * @returns This builder instance for method chaining
   */
  override reset(): this {
    super.reset();
    this.triggerPrice = undefined;
    this.triggerPriceType = TriggerPriceType.MARK_PRICE;
    this.algoType = undefined;
    this.quantity = undefined;
    return this;
  }
}
