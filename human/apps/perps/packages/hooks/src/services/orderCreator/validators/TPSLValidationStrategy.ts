import {
  AlgoOrderEntity,
  AlgoOrderRootType,
  OrderSide,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import { Decimal, resolveTPSLOrderType } from "@paxeer-network/utils";
import { getPriceRange } from "../../../utils/order/orderPrice";
import {
  OrderValidationItem,
  OrderValidationResult,
  ValuesDepConfig,
} from "../interface";
import { OrderValidation } from "../orderValidation";
import { IValidationStrategy } from "./IValidationStrategy";

/**
 * Formats price to specified decimal places
 */
const formatPrice = (price: number, quote_dp: number): number => {
  return new Decimal(price).toDecimalPlaces(quote_dp).toNumber();
};

type TPSLValidationConfig = ValuesDepConfig & {
  /** Skip only the stage-specific trigger-to-mark check for selected legs. */
  skipTPSLTriggerPriceAgainstMark?: Partial<Record<"tp" | "sl", boolean>>;
};

/**
 * Strategy for validating Take Profit / Stop Loss orders
 * Consolidates validation logic from baseBracketOrderCreator and baseAlgoCreator
 * to eliminate code duplication
 */
export class TPSLValidationStrategy implements IValidationStrategy<
  Partial<
    AlgoOrderEntity<
      AlgoOrderRootType.POSITIONAL_TP_SL | AlgoOrderRootType.TP_SL
    >
  >,
  OrderValidationResult
> {
  /**
   * Validates TP/SL order values
   * @param values - TP/SL order values including trigger prices, order prices, etc.
   * @param config - Configuration with symbol info and mark price
   * @returns Validation result object with any errors found
   */
  validate(
    values: Partial<
      AlgoOrderEntity<
        AlgoOrderRootType.POSITIONAL_TP_SL | AlgoOrderRootType.TP_SL
      >
    >,
    config: TPSLValidationConfig,
  ): OrderValidationResult {
    const result: OrderValidationResult = Object.create(null);

    const normalized = { ...values };
    for (const leg of ["tp", "sl"] as const) {
      const triggerKey = `${leg}_trigger_price` as const;
      const priceKey = `${leg}_order_price` as const;
      const trigger = values[triggerKey];
      const price = values[priceKey];
      const hasTrigger =
        trigger !== undefined && trigger !== null && trigger !== "";
      const hasPrice = price !== undefined && price !== null && price !== "";
      const resolvedType = resolveTPSLOrderType(
        values[`${leg}_order_type`],
        price,
      );

      // A LIMIT price means the user is configuring this leg. Do not treat it
      // as an inactive placeholder when its trigger price is still empty.
      if (!hasTrigger) {
        if (hasPrice && resolvedType === OrderType.LIMIT) {
          result[triggerKey] = OrderValidation.required(triggerKey);
        }
        normalized[priceKey] = undefined;
        continue;
      }

      const triggerNumber = Number(trigger);
      if (!Number.isFinite(triggerNumber) || triggerNumber <= 0) {
        result[triggerKey] = OrderValidation.min(
          triggerKey,
          config.symbol.quote_tick,
        );
        normalized[triggerKey] = undefined;
      }

      if (resolvedType !== OrderType.LIMIT) {
        normalized[priceKey] = undefined;
        continue;
      }
      if (price == null || price === "") {
        result[priceKey] = OrderValidation.required(priceKey);
        normalized[priceKey] = undefined;
      } else if (
        !Number.isFinite(Number(price)) ||
        new Decimal(price).todp(config.symbol.quote_dp).lte(0)
      ) {
        result[priceKey] = OrderValidation.min(
          priceKey,
          config.symbol.quote_tick,
        );
        normalized[priceKey] = undefined;
      } else {
        normalized[priceKey] = new Decimal(price)
          .todp(config.symbol.quote_dp)
          .toString();
      }
    }

    const {
      tp_trigger_price,
      tp_order_price,
      tp_order_type,
      sl_trigger_price,
      sl_order_price,
      sl_order_type,
      side,
      quantity,
      order_type,
      order_price,
    } = normalized;

    const qty = Number(quantity);
    const maxQty = config.maxQty;
    const { quote_max, quote_min, quote_dp, base_min } = config.symbol ?? {};

    // Determine mark price based on order type
    const mark_price =
      order_type === OrderType.MARKET || order_type == null
        ? config.markPrice
        : order_price
          ? Number(order_price)
          : undefined;

    // TP/SL side is opposite of order side
    const tpslSide = side === OrderSide.BUY ? OrderSide.SELL : OrderSide.BUY;

    // Validate quantity
    const fullPosition =
      values.position_type === PositionType.FULL ||
      values.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL;
    if (!fullPosition && !isNaN(qty) && qty > maxQty) {
      result.quantity = OrderValidation.max("quantity", config.maxQty);
    }
    if (!fullPosition && !isNaN(qty) && qty < (base_min ?? 0)) {
      result.quantity = OrderValidation.min("quantity", base_min ?? 0);
    }

    // Validate based on order side and mark price
    if (side === OrderSide.BUY && mark_price) {
      this.validateBuySide(
        {
          tp_trigger_price,
          tp_order_price,
          sl_trigger_price,
          sl_order_price,
        },
        {
          mark_price,
          quote_min: quote_min ?? 0,
          quote_max: quote_max ?? 0,
          quote_dp: quote_dp ?? 0,
          tpslSide,
          symbol: config.symbol,
          skipTriggerPriceAgainstMark: config.skipTPSLTriggerPriceAgainstMark,
        },
        result,
      );
    } else if (side === OrderSide.SELL && mark_price) {
      this.validateSellSide(
        {
          tp_trigger_price,
          tp_order_price,
          sl_trigger_price,
          sl_order_price,
        },
        {
          mark_price,
          quote_min: quote_min ?? 0,
          quote_max: quote_max ?? 0,
          quote_dp: quote_dp ?? 0,
          tpslSide,
          symbol: config.symbol,
          skipTriggerPriceAgainstMark: config.skipTPSLTriggerPriceAgainstMark,
        },
        result,
      );
    }

    return Object.keys(result).length > 0 ? result : (null as any);
  }

  /**
   * Validates TP/SL for BUY orders
   * For BUY orders:
   * - SL trigger price must be < mark price
   * - TP trigger price must be > mark price
   */
  private validateBuySide(
    prices: {
      tp_trigger_price?: number | string;
      tp_order_price?: number | string;
      sl_trigger_price?: number | string;
      sl_order_price?: number | string;
    },
    config: {
      mark_price: number;
      quote_min: number;
      quote_max: number;
      quote_dp: number;
      tpslSide: OrderSide;
      symbol: any;
      skipTriggerPriceAgainstMark?: Partial<Record<"tp" | "sl", boolean>>;
    },
    result: OrderValidationResult,
  ): void {
    const {
      tp_trigger_price,
      tp_order_price,
      sl_trigger_price,
      sl_order_price,
    } = prices;
    const {
      mark_price,
      quote_min,
      quote_max,
      quote_dp,
      tpslSide,
      symbol,
      skipTriggerPriceAgainstMark,
    } = config;

    // Validate SL trigger price
    if (
      sl_trigger_price !== undefined &&
      sl_trigger_price !== "" &&
      sl_trigger_price !== null
    ) {
      const slTrigger = Number(sl_trigger_price);
      if (!isNaN(slTrigger)) {
        // Only validate against quote_min if it's a valid number > 0
        if (quote_min > 0 && slTrigger < quote_min) {
          result.sl_trigger_price = OrderValidation.min(
            "sl_trigger_price",
            formatPrice(quote_min, quote_dp),
          );
        }
        if (!skipTriggerPriceAgainstMark?.sl && slTrigger >= mark_price) {
          result.sl_trigger_price = OrderValidation.max(
            "sl_trigger_price",
            formatPrice(mark_price, quote_dp),
          );
        }
      }
    }

    // Validate TP trigger price
    if (
      tp_trigger_price !== undefined &&
      tp_trigger_price !== "" &&
      tp_trigger_price !== null
    ) {
      const tpTrigger = Number(tp_trigger_price);
      if (!isNaN(tpTrigger)) {
        if (!skipTriggerPriceAgainstMark?.tp && tpTrigger <= mark_price) {
          result.tp_trigger_price = OrderValidation.min(
            "tp_trigger_price",
            formatPrice(mark_price, quote_dp),
          );
        }
        // Only validate against quote_max if it's a valid number > 0
        if (quote_max > 0 && tpTrigger > quote_max) {
          result.tp_trigger_price = OrderValidation.max(
            "tp_trigger_price",
            formatPrice(quote_max, quote_dp),
          );
        }
      }
    }

    // Validate SL order price (if limit order)
    if (sl_trigger_price && sl_order_price) {
      const priceRange = getPriceRange({
        side: tpslSide,
        basePrice: Number(sl_trigger_price),
        symbolInfo: symbol,
      });
      const slOrderPrice = Number(sl_order_price);
      const slTrigger = Number(sl_trigger_price);

      if (slOrderPrice < priceRange.minPrice) {
        result.sl_order_price = OrderValidation.min(
          "sl_order_price",
          formatPrice(priceRange.minPrice, quote_dp),
        );
      }
      if (slOrderPrice > priceRange.maxPrice) {
        result.sl_order_price = OrderValidation.max(
          "sl_order_price",
          formatPrice(priceRange.maxPrice, quote_dp),
        );
      }
      // FE limit: sl_order_price must be less than sl_trigger_price for BUY orders
      if (slTrigger < slOrderPrice) {
        result.sl_trigger_price =
          OrderValidation.priceErrorMax("sl_trigger_price");
      }
    }

    // Validate TP order price (if limit order)
    if (tp_trigger_price && tp_order_price) {
      const priceRange = getPriceRange({
        side: tpslSide,
        basePrice: Number(tp_trigger_price),
        symbolInfo: symbol,
      });
      const tpOrderPrice = Number(tp_order_price);
      const tpTrigger = Number(tp_trigger_price);

      if (tpOrderPrice > priceRange.maxPrice) {
        result.tp_order_price = OrderValidation.max(
          "tp_order_price",
          formatPrice(priceRange.maxPrice, quote_dp),
        );
      }
      if (tpOrderPrice < priceRange.minPrice) {
        result.tp_order_price = OrderValidation.min(
          "tp_order_price",
          formatPrice(priceRange.minPrice, quote_dp),
        );
      }
      // FE limit: tp_order_price must be greater than tp_trigger_price for BUY orders
      if (tpTrigger > tpOrderPrice) {
        result.tp_trigger_price =
          OrderValidation.priceErrorMin("tp_trigger_price");
      }
    }
  }

  /**
   * Validates TP/SL for SELL orders
   * For SELL orders:
   * - SL trigger price must be > mark price
   * - TP trigger price must be < mark price
   */
  private validateSellSide(
    prices: {
      tp_trigger_price?: number | string;
      tp_order_price?: number | string;
      sl_trigger_price?: number | string;
      sl_order_price?: number | string;
    },
    config: {
      mark_price: number;
      quote_min: number;
      quote_max: number;
      quote_dp: number;
      tpslSide: OrderSide;
      symbol: any;
      skipTriggerPriceAgainstMark?: Partial<Record<"tp" | "sl", boolean>>;
    },
    result: OrderValidationResult,
  ): void {
    const {
      tp_trigger_price,
      tp_order_price,
      sl_trigger_price,
      sl_order_price,
    } = prices;
    const {
      mark_price,
      quote_min,
      quote_max,
      quote_dp,
      tpslSide,
      symbol,
      skipTriggerPriceAgainstMark,
    } = config;

    // Validate SL trigger price
    if (
      sl_trigger_price !== undefined &&
      sl_trigger_price !== "" &&
      sl_trigger_price !== null
    ) {
      const slTrigger = Number(sl_trigger_price);
      if (!isNaN(slTrigger)) {
        // Only validate against quote_max if it's a valid number > 0
        if (quote_max > 0 && slTrigger > quote_max) {
          result.sl_trigger_price = OrderValidation.max(
            "sl_trigger_price",
            formatPrice(quote_max, quote_dp),
          );
        }
        if (!skipTriggerPriceAgainstMark?.sl && slTrigger <= mark_price) {
          result.sl_trigger_price = OrderValidation.min(
            "sl_trigger_price",
            formatPrice(mark_price, quote_dp),
          );
        }
      }
    }

    // Validate TP trigger price
    if (
      tp_trigger_price !== undefined &&
      tp_trigger_price !== "" &&
      tp_trigger_price !== null
    ) {
      const tpTrigger = Number(tp_trigger_price);
      if (!isNaN(tpTrigger)) {
        if (!skipTriggerPriceAgainstMark?.tp && tpTrigger >= mark_price) {
          result.tp_trigger_price = OrderValidation.max(
            "tp_trigger_price",
            formatPrice(mark_price, quote_dp),
          );
        }
        // Only validate against quote_min if it's a valid number > 0
        if (quote_min > 0 && tpTrigger < quote_min) {
          result.tp_trigger_price = OrderValidation.min(
            "tp_trigger_price",
            formatPrice(quote_min, quote_dp),
          );
        }
      }
    }

    // Validate SL order price (if limit order)
    if (sl_trigger_price && sl_order_price) {
      const priceRange = getPriceRange({
        side: tpslSide,
        basePrice: Number(sl_trigger_price),
        symbolInfo: symbol,
      });
      const slOrderPrice = Number(sl_order_price);
      const slTrigger = Number(sl_trigger_price);

      if (slOrderPrice < priceRange.minPrice) {
        result.sl_order_price = OrderValidation.min(
          "sl_order_price",
          formatPrice(priceRange.minPrice, quote_dp),
        );
      }
      if (slOrderPrice > priceRange.maxPrice) {
        result.sl_order_price = OrderValidation.max(
          "sl_order_price",
          formatPrice(priceRange.maxPrice, quote_dp),
        );
      }
      // FE limit: sl_order_price must be greater than sl_trigger_price for SELL orders
      if (slTrigger > slOrderPrice) {
        result.sl_trigger_price =
          OrderValidation.priceErrorMin("sl_trigger_price");
      }
    }

    // Validate TP order price (if limit order)
    if (tp_trigger_price && tp_order_price) {
      const priceRange = getPriceRange({
        side: tpslSide,
        basePrice: Number(tp_trigger_price),
        symbolInfo: symbol,
      });
      const tpOrderPrice = Number(tp_order_price);
      const tpTrigger = Number(tp_trigger_price);

      if (tpOrderPrice < priceRange.minPrice) {
        result.tp_order_price = OrderValidation.min(
          "tp_order_price",
          formatPrice(priceRange.minPrice, quote_dp),
        );
      }
      if (tpOrderPrice > priceRange.maxPrice) {
        result.tp_order_price = OrderValidation.max(
          "tp_order_price",
          formatPrice(priceRange.maxPrice, quote_dp),
        );
      }
      // FE limit: tp_order_price must be less than tp_trigger_price for SELL orders
      if (tpTrigger < tpOrderPrice) {
        result.tp_trigger_price =
          OrderValidation.priceErrorMax("tp_trigger_price");
      }
    }
  }
}
