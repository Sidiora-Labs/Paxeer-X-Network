import { PaxeerNetworkOrder } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { OrderValidationItem, ValuesDepConfig } from "../interface";
import { OrderValidation } from "../orderValidation";
import { IValidationStrategy } from "./IValidationStrategy";

/**
 * Strategy for validating order quantities
 * Handles quantity validation including min/max checks
 */
export class QuantityValidationStrategy implements IValidationStrategy<{
  order_quantity?: number | string;
  total?: number | string;
  order_price?: number | string;
}> {
  /**
   * Validates order quantity against symbol constraints
   * Also handles conversion from total to quantity if needed
   * @param values - Object containing order_quantity, total, and order_price
   * @param config - Configuration with symbol info and max quantity
   * @returns Validation error if quantity is invalid, undefined otherwise
   */
  validate(
    values: {
      order_quantity?: number | string;
      total?: number | string;
      order_price?: number | string;
    },
    config: ValuesDepConfig,
  ): OrderValidationItem | undefined {
    let { order_quantity, total, order_price } = values;
    const { maxQty, symbol } = config;
    if (!symbol) {
      return;
    }
    const { base_min, base_dp, quote_dp } = symbol;

    // Calculate order_quantity from total if not provided
    if (!order_quantity && total && order_price) {
      const totalNumber = new Decimal(total);
      const qty = totalNumber.dividedBy(order_price).toFixed(quote_dp);
      order_quantity = qty;
    }

    if (!order_quantity) {
      return OrderValidation.required("order_quantity");
    }

    const qty = new Decimal(order_quantity);

    // Check minimum quantity
    if (qty.lt(base_min)) {
      return OrderValidation.min(
        "order_quantity",
        new Decimal(base_min).todp(base_dp).toString(),
      );
    }

    // Check maximum quantity
    if (qty.gt(maxQty)) {
      return OrderValidation.max(
        "order_quantity",
        new Decimal(maxQty).todp(base_dp).toString(),
      );
    }

    return undefined;
  }
}
