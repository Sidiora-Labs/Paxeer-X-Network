import { order as orderUtils } from "@paxeer-network/perp";
import { OrderSide, PaxeerNetworkOrder } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { getPriceRange } from "../../../utils/order/orderPrice";
import { OrderValidationItem, ValuesDepConfig } from "../interface";
import { OrderValidation } from "../orderValidation";
import { IValidationStrategy } from "./IValidationStrategy";

/**
 * Strategy for validating order prices
 * Handles price validation for limit orders and stop-limit orders
 */
export class PriceValidationStrategy implements IValidationStrategy<{
  order_price?: number | string;
  side: OrderSide;
  order_type?: string;
}> {
  /**
   * Validates order price against symbol constraints and price range
   * @param values - Object containing order_price, side, and order_type
   * @param config - Configuration with symbol info and mark price
   * @returns Validation error if price is invalid, undefined otherwise
   */
  validate(
    values: {
      order_price?: number | string;
      side: OrderSide;
      order_type?: string;
    },
    config: ValuesDepConfig,
  ): OrderValidationItem | undefined {
    const { order_price, side } = values;

    if (!order_price) {
      return OrderValidation.required("order_price");
    }

    const price = new Decimal(order_price);
    const { symbol } = config;
    if (!symbol) {
      return;
    }
    const { quote_max, quote_min, quote_dp, price_range, price_scope } = symbol;

    // Calculate price range based on side and mark price
    const maxPriceNumber = orderUtils.maxPrice(config.markPrice, price_range);
    const minPriceNumber = orderUtils.minPrice(config.markPrice, price_range);
    const scopePriceNumber = orderUtils.scopePrice(
      config.markPrice,
      price_scope,
      side,
    );

    const priceRange =
      side === OrderSide.BUY
        ? {
            min: scopePriceNumber,
            max: maxPriceNumber,
          }
        : {
            min: minPriceNumber,
            max: scopePriceNumber,
          };

    // Check against absolute quote limits first
    if (price.gt(quote_max)) {
      return OrderValidation.max("order_price", quote_max);
    }

    if (price.lt(quote_min)) {
      return OrderValidation.min("order_price", quote_min);
    }

    // Check against calculated price range
    if (price.gt(priceRange.max)) {
      return OrderValidation.max(
        "order_price",
        new Decimal(priceRange.max).todp(quote_dp).toString(),
      );
    }

    if (price.lt(priceRange.min)) {
      return OrderValidation.min(
        "order_price",
        new Decimal(priceRange.min).todp(quote_dp).toString(),
      );
    }

    return undefined;
  }
}

/**
 * Strategy for validating trigger prices in stop orders
 */
export class TriggerPriceValidationStrategy implements IValidationStrategy<{
  trigger_price?: number | string;
}> {
  /**
   * Validates trigger price against symbol constraints
   * @param values - Object containing trigger_price
   * @param config - Configuration with symbol info
   * @returns Validation error if trigger price is invalid, undefined otherwise
   */
  validate(
    values: { trigger_price?: number | string },
    config: ValuesDepConfig,
  ): OrderValidationItem | undefined {
    const { trigger_price } = values;
    const { symbol } = config;
    if (!symbol) {
      return;
    }
    const { quote_max, quote_min } = symbol;

    if (!trigger_price) {
      return OrderValidation.required("trigger_price");
    }

    const triggerPrice = Number(trigger_price);

    if (triggerPrice > quote_max) {
      return OrderValidation.max("trigger_price", quote_max);
    }

    if (triggerPrice < quote_min || triggerPrice === 0) {
      return OrderValidation.min("trigger_price", quote_min);
    }

    return undefined;
  }
}
