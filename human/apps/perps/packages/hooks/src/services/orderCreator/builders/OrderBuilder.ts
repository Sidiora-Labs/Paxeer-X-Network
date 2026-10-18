import {
  OrderEntity,
  PaxeerNetworkOrder,
  OrderType,
  OrderSide,
  MarginMode,
} from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

/**
 * Builder for constructing orders
 * Implements Builder pattern to provide a fluent interface for order creation
 */
export class OrderBuilder {
  protected order: Partial<PaxeerNetworkOrder> = {};

  /**
   * Sets the symbol
   * @param symbol - Trading symbol
   * @returns This builder instance for method chaining
   */
  withSymbol(symbol: string): this {
    this.order.symbol = symbol;
    return this;
  }

  /**
   * Sets the order type
   * @param orderType - Type of order (LIMIT, MARKET, etc.)
   * @returns This builder instance for method chaining
   */
  withOrderType(orderType: OrderType | string): this {
    this.order.order_type = orderType as OrderType;
    return this;
  }

  /**
   * Sets the order side
   * @param side - BUY or SELL
   * @returns This builder instance for method chaining
   */
  withSide(side: OrderSide): this {
    this.order.side = side;
    return this;
  }

  /**
   * Sets the order price
   * @param price - Order price
   * @returns This builder instance for method chaining
   */
  withPrice(price: number | string): this {
    this.order.order_price = price;
    return this;
  }

  /**
   * Sets the order quantity
   * @param quantity - Order quantity
   * @returns This builder instance for method chaining
   */
  withQuantity(quantity: number | string): this {
    this.order.order_quantity = quantity;
    return this;
  }

  /**
   * Sets the total order value
   * @param total - Total order value
   * @returns This builder instance for method chaining
   */
  withTotal(total: number | string): this {
    this.order.total = total;
    return this;
  }

  /**
   * Sets the reduce only flag
   * @param reduceOnly - Whether the order is reduce only
   * @returns This builder instance for method chaining
   */
  withReduceOnly(reduceOnly: boolean): this {
    this.order.reduce_only = reduceOnly;
    return this;
  }

  /**
   * Sets the visible quantity (for iceberg orders)
   * @param visibleQuantity - Visible quantity
   * @returns This builder instance for method chaining
   */
  withVisibleQuantity(visibleQuantity: number): this {
    this.order.visible_quantity = visibleQuantity;
    return this;
  }

  /**
   * Sets the slippage tolerance (for market orders)
   * @param slippage - Slippage percentage (e.g., 0.5 for 0.5%)
   * @returns This builder instance for method chaining
   */
  withSlippage(slippage: number | string): this {
    if (this.order.order_type === OrderType.MARKET && slippage) {
      this.order.slippage = new Decimal(slippage).div(100).toNumber();
    }
    return this;
  }

  /**
   * Sets the margin mode
   * @param marginMode - Margin mode (CROSS or ISOLATED)
   * @returns This builder instance for method chaining
   */
  withMarginMode(marginMode?: MarginMode): this {
    this.order.margin_mode = marginMode || MarginMode.CROSS;
    return this;
  }

  /**
   * Builds the final order
   * @returns The constructed order
   */
  build(): PaxeerNetworkOrder {
    return this.order as PaxeerNetworkOrder;
  }

  /**
   * Resets the builder to start a new order
   * @returns This builder instance for method chaining
   */
  reset(): this {
    this.order = {};
    return this;
  }
}
