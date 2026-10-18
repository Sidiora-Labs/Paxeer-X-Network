import { pick } from "ramda";
import {
  AlgoOrderRootType,
  API,
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { getPriceRange } from "../../utils/order/orderPrice";
import { BaseOrderCreator } from "./baseCreator";
import { ValuesDepConfig, OrderValidationResult } from "./interface";
import { OrderValidation } from "./orderValidation";

/**
 * Creator for trailing stop orders
 * Uses Template Method pattern from BaseOrderCreator
 */
export class TrailingStopOrderCreator extends BaseOrderCreator<PaxeerNetworkOrder> {
  orderType = OrderType.TRAILING_STOP;

  /**
   * Builds the trailing stop order
   * Implements template method hook
   */
  protected buildOrder(values: PaxeerNetworkOrder, config: ValuesDepConfig) {
    const { order_quantity, activated_price, callback_value, callback_rate } =
      values;

    const order = {
      ...this.baseOrder(values),
      algo_type: AlgoOrderRootType.TRAILING_STOP,
      type: OrderType.MARKET,
      trigger_price_type: "MARK_PRICE",
      quantity: order_quantity,
      activated_price,
      callback_value,
      callback_rate: callback_rate
        ? new Decimal(callback_rate).div(100).toString()
        : undefined,
    };

    return pick(
      [
        "symbol",
        "algo_type",
        "type",
        "trigger_price_type",
        "quantity",
        "side",
        "reduce_only",
        "margin_mode",
        "visible_quantity",
        "activated_price",
        "callback_value",
        "callback_rate",
      ],
      order,
    );
  }

  /**
   * Runs validations for trailing stop order
   * Implements template method hook
   */
  protected runValidations(
    values: PaxeerNetworkOrder,
    config: ValuesDepConfig,
  ): OrderValidationResult {
    const { markPrice, symbol } = config;
    const { quote_dp } = config.symbol;
    const { side, activated_price, callback_value, callback_rate } = values;

    const errors = this.baseValidate(values, config);

    if (activated_price) {
      const { minPrice, maxPrice } = getPriceRange({
        side,
        basePrice: markPrice,
        symbolInfo: symbol,
      });

      // Only validate if minPrice and maxPrice are valid numbers
      if (!isNaN(minPrice) && !isNaN(maxPrice)) {
        const activatedPrice = new Decimal(activated_price);

        if (activatedPrice.lt(minPrice) || activatedPrice.equals(0)) {
          errors.activated_price = OrderValidation.min(
            "activated_price",
            new Decimal(minPrice).todp(quote_dp).toString(),
          );
        } else if (activatedPrice.gt(maxPrice)) {
          errors.activated_price = OrderValidation.max(
            "activated_price",
            new Decimal(maxPrice).todp(quote_dp).toString(),
          );
        }
      }
    }

    if (!callback_value && !callback_rate) {
      errors.callback_value = OrderValidation.required("callback_value");
      errors.callback_rate = OrderValidation.required("callback_rate");
    }

    if (callback_value) {
      // 0 < callback_value < market_price
      // (callback_value - min_quote) % quote_tick == 0
      const callbackValue = new Decimal(callback_value);
      if (callbackValue.equals(0)) {
        errors.callback_value = OrderValidation.min("callback_value", "0");
      } else if (callbackValue.gt(markPrice)) {
        errors.callback_value = OrderValidation.range(
          "callback_value",
          "0",
          new Decimal(markPrice).todp(quote_dp).toString(),
        );
      }
    }

    if (callback_rate) {
      // 0.001 < callback_rate < 0.05
      // (callback_rate - 0.001) % 0.001 == 0
      const callbackRate = new Decimal(callback_rate).div(100);
      if (callbackRate.lt(0.001) || callbackRate.gt(0.05)) {
        errors.callback_rate = OrderValidation.range(
          "callback_rate",
          "0.1%",
          "5%",
        );
      }
    }
    return errors;
  }
}
