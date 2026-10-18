import { order as orderUtils } from "@paxeer-network/perp";
import {
  API,
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";

/**
 * Compute the effective execution price for an order.
 *
 * Business rules:
 *
 * - MARKET / STOP_MARKET
 *   - BUY:  price = Ask1
 *   - SELL: price = Bid1
 *
 * - LIMIT
 *   - BUY:
 *     - if limit_price >= Ask1 -> price = Ask1
 *     - else                   -> price = limit_price
 *   - SELL:
 *     - if limit_price <= Bid1 -> price = Bid1
 *     - else                   -> price = limit_price
 *
 * This function pre-dates isolated-margin reference price logic and is kept
 * for backward compatibility with existing order helpers.
 */
export function getOrderPrice(
  order: Partial<PaxeerNetworkOrder>,
  askAndBid: number[],
) {
  const orderPrice = Number(order.order_price);
  if (
    order.order_type === OrderType.MARKET ||
    order.order_type === OrderType.STOP_MARKET
  ) {
    if (order.side === OrderSide.BUY) {
      return askAndBid[0];
    } else {
      return askAndBid[1];
    }
  } else {
    // LIMIT order
    if (order.side === OrderSide.BUY) {
      if (orderPrice >= askAndBid[0]) {
        return askAndBid[0];
      } else {
        return orderPrice;
      }
    } else {
      if (orderPrice <= askAndBid[1]) {
        return askAndBid[1];
      } else {
        return orderPrice;
      }
    }
  }
}

/**
 * Convenience re-export of the core reference price calculator from
 * `@paxeer-network/perp`.
 *
 * NOTE: This exposes the low-level API that operates on the light-weight
 * `OrderReferencePriceInput` type. Most UI callers should instead use
 * `getOrderReferencePriceFromOrder`, which adapts a full `PaxeerNetworkOrder`.
 */
export const getOrderReferencePrice = orderUtils.getOrderReferencePrice;

/**
 * Context required to price MARKET orders the same way the backend risk
 * engine does (mark price +/- symbol price range instead of Ask1/Bid1).
 */
export type MarketReferencePriceContext = {
  /** Mark price of the symbol */
  markPrice?: number;
  /** Symbol `price_range` config, e.g. 0.1 = 10% */
  priceRange?: number;
  /** Decimal places to round the MARKET reference price to (usually quote_dp) */
  pricePrecision?: number;
};

/**
 * Adapter to calculate reference price directly from an `PaxeerNetworkOrder`
 * and the best bid / ask prices from orderbook.
 *
 * @param order    Partial order entity (side / type / price fields)
 * @param askAndBid `[Ask1, Bid1]` tuple
 * @param marketContext Mark price / price range context for backend-aligned
 *   MARKET reference pricing; when omitted MARKET falls back to Ask1/Bid1
 * @returns Reference price or null when it cannot be determined
 */
export function getOrderReferencePriceFromOrder(
  order: Partial<PaxeerNetworkOrder>,
  askAndBid: number[],
  marketContext?: MarketReferencePriceContext,
): number | null {
  if (!order.order_type || !order.side) {
    return null;
  }

  const askPrice = askAndBid?.[0] ?? 0;
  const bidPrice = askAndBid?.[1] ?? 0;

  return orderUtils.getOrderReferencePrice(
    {
      orderType: order.order_type,
      orderTypeExt: order.order_type_ext,
      side: order.side,
      limitPrice: order.order_price ? Number(order.order_price) : undefined,
      triggerPrice: order.trigger_price
        ? Number(order.trigger_price)
        : undefined,
      markPrice: marketContext?.markPrice,
      priceRange: marketContext?.priceRange,
      pricePrecision: marketContext?.pricePrecision,
    },
    askPrice,
    bidPrice,
  );
}

export function getPriceRange(inputs: {
  side: OrderSide;
  basePrice: number;
  symbolInfo: API.SymbolExt;
}) {
  const { basePrice, side, symbolInfo } = inputs;
  const { price_range, price_scope, quote_min, quote_max } = symbolInfo;

  const maxPriceNumber = orderUtils.maxPrice(basePrice, price_range);
  const minPriceNumber = orderUtils.minPrice(basePrice, price_range);
  const scopePriceNumber = orderUtils.scopePrice(basePrice, price_scope, side);

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

  const minPrice = Math.max(quote_min, priceRange?.min);
  const maxPrice = Math.min(quote_max, priceRange?.max);

  return {
    minPrice,
    maxPrice,
  };
}

export function getTPSLTriggerPriceScope(inputs: {
  side: OrderSide;
  basePrice: number;
  symbolInfo: API.SymbolExt;
}) {
  const { basePrice, side, symbolInfo } = inputs;
  const { price_scope, quote_min, quote_max } = symbolInfo;

  const scopePriceNumber = orderUtils.scopePrice(basePrice, price_scope, side);

  const priceRange =
    side === OrderSide.BUY
      ? {
          min: scopePriceNumber,
          max: quote_max,
        }
      : {
          min: quote_min,
          max: scopePriceNumber,
        };

  return {
    minPrice: priceRange.min,
    maxPrice: priceRange.max,
  };
}

/** @deprecated use getTPSLTriggerPriceScope instead */
export function getTPSLTriggerPriceRange(inputs: {
  side: OrderSide;
  basePrice: number;
  symbolInfo: API.SymbolExt;
}) {
  const { basePrice, side, symbolInfo } = inputs;
  const { price_scope, quote_min, quote_max } = symbolInfo;

  const scopePriceNumber = orderUtils.scopePrice(basePrice, price_scope, side);

  const priceRange =
    side === OrderSide.BUY
      ? {
          min: scopePriceNumber,
          max: quote_max,
        }
      : {
          min: quote_min,
          max: scopePriceNumber,
        };

  return {
    minPrice: priceRange.min,
    maxPrice: priceRange.max,
  };
}
