import { pick } from "ramda";
import {
  DistributionType,
  OrderEntity,
  PaxeerNetworkOrder,
  OrderType,
} from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { getPriceRange } from "../../utils/order/orderPrice";
import {
  calcScaledOrderBatchBody,
  calcScaledOrderMinTotalAmount,
} from "../../utils/order/scaledOrder";
import { BaseOrderCreator } from "./baseCreator";
import {
  ValuesDepConfig,
  OrderValidationResult,
  OrderValidationItem,
} from "./interface";
import { OrderValidation } from "./orderValidation";

/**
 * Creator for scaled orders
 * Uses Template Method pattern from BaseOrderCreator
 */
export class ScaledOrderCreator extends BaseOrderCreator<OrderEntity> {
  orderType = OrderType.SCALED;

  /**
   * Builds the scaled order
   * Implements template method hook
   */
  protected buildOrder(values: OrderEntity, config: ValuesDepConfig) {
    const orders = calcScaledOrderBatchBody(values, config.symbol);
    const { total_orders, distribution_type, skew } = values;

    const order = {
      ...this.baseOrder(values as unknown as PaxeerNetworkOrder),
      total_orders,
      distribution_type,
      skew,
      orders,
    };

    return pick(
      [
        "symbol",
        "order_quantity",
        "visible_quantity",
        "reduce_only",
        "side",
        "order_type",
        "margin_mode",
        "orders",
        "total_orders",
        "distribution_type",
        "skew",
      ],
      order,
    );
  }

  /**
   * Runs validations for scaled order
   * Implements template method hook
   */
  protected runValidations(
    values: PaxeerNetworkOrder,
    config: ValuesDepConfig,
  ): OrderValidationResult {
    const { maxQty, askAndBid, markPrice, symbol } = config;
    const { base_dp, quote_dp } = config.symbol;
    const { order_quantity, total, total_orders, distribution_type, skew } =
      values;

    const errors = validatePrice(values, config);

    if (!total_orders) {
      errors.total_orders = OrderValidation.required("total_orders");
    } else {
      const totalOrdersNum = Number(total_orders);
      if (totalOrdersNum < 2 || totalOrdersNum > 20) {
        errors.total_orders = OrderValidation.range("total_orders", 2, 20);
      }
    }

    if (distribution_type === DistributionType.CUSTOM) {
      if (!skew) {
        errors.skew = OrderValidation.required("skew");
      } else {
        const skewNum = Number(skew);
        if (skewNum <= 0) {
          errors.skew = OrderValidation.min("skew", 0);
        } else if (skewNum > 100) {
          errors.skew = OrderValidation.max("skew", 100);
        }
      }
    }

    if (!order_quantity) {
      errors.order_quantity = OrderValidation.required("order_quantity");
    } else {
      const qty = new Decimal(order_quantity);

      if (qty.gt(maxQty)) {
        errors.order_quantity = OrderValidation.max(
          "order_quantity",
          new Decimal(maxQty).todp(base_dp).toString(),
        );
        errors.total = OrderValidation.max(
          "total",
          new Decimal(maxQty).mul(markPrice).todp(quote_dp).toString(),
        );
      } else if (!errors.skew && !errors.total_orders) {
        // if skew and total_orders are not errors, and order_quantity is less than minTotalAmount, set error
        const minTotalAmount = calcScaledOrderMinTotalAmount(
          values,
          symbol,
          askAndBid!,
        );

        if (minTotalAmount && qty.lt(minTotalAmount)) {
          errors.order_quantity = OrderValidation.min(
            "order_quantity",
            minTotalAmount,
          );
          errors.total = OrderValidation.min(
            "total",
            new Decimal(minTotalAmount)
              .mul(markPrice)
              .todp(quote_dp)
              .toString(),
          );
        }
      }
    }

    if (!total) {
      errors.total = OrderValidation.required("total");
    }

    return errors;
  }
}

function validatePrice(values: PaxeerNetworkOrder, config: ValuesDepConfig) {
  const errors: {
    [P in keyof OrderEntity]?: OrderValidationItem;
  } = {};

  const { start_price, end_price } = values;
  const { quote_dp } = config.symbol;

  // when markPrice is empty, skip price validation
  if (!config.markPrice) {
    return errors;
  }

  const { minPrice, maxPrice } = getPriceRange({
    side: values.side,
    basePrice: config.markPrice,
    symbolInfo: config.symbol,
  });

  const comparePrice = (key: "start_price" | "end_price", value?: string) => {
    const price = new Decimal(value || 0);

    if (price.lt(minPrice)) {
      errors[key] = OrderValidation.min(
        key,
        new Decimal(minPrice).todp(quote_dp).toString(),
      );
    } else if (price.gt(maxPrice)) {
      errors[key] = OrderValidation.max(
        key,
        new Decimal(maxPrice).todp(quote_dp).toString(),
      );
    }
  };

  comparePrice("start_price", start_price);
  comparePrice("end_price", end_price);

  return errors;
}
