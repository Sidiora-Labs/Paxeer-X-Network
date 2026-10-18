import { pick } from "ramda";
import {
  AlgoOrderEntity,
  AlgoOrderRootType,
  TriggerPriceType,
  PaxeerNetworkOrder,
  OrderType,
  OrderSide,
} from "@paxeer-network/types";
import { BaseOrderCreator } from "./baseCreator";
import { ValuesDepConfig, OrderValidationResult } from "./interface";
import { OrderValidation } from "./orderValidation";
import { PriceValidationStrategy } from "./validators/PriceValidationStrategy";
import { TriggerPriceValidationStrategy } from "./validators/PriceValidationStrategy";
import { PriceValidator } from "./validators/PriceValidator";
import { QuantityValidator } from "./validators/QuantityValidator";
import { TriggerPriceValidator } from "./validators/TriggerPriceValidator";
import { ValidationChain } from "./validators/ValidationChain";

/**
 * Creator for stop-limit orders
 * Uses Strategy pattern for validation and Template Method pattern for creation
 */
export class StopLimitOrderCreator extends BaseOrderCreator<AlgoOrderEntity> {
  private priceValidationStrategy = new PriceValidationStrategy();
  private triggerPriceValidationStrategy = new TriggerPriceValidationStrategy();
  private validationChain = new ValidationChain()
    .addValidator(new QuantityValidator())
    .addValidator(new TriggerPriceValidator())
    .addValidator(new PriceValidator());

  /**
   * Builds the stop-limit order
   * Implements template method hook
   */
  protected buildOrder(
    values: AlgoOrderEntity & {
      order_quantity: number;
      order_price: number;
    },
    config?: ValuesDepConfig,
  ): AlgoOrderEntity<AlgoOrderRootType.STOP> {
    this.totalToQuantity(values, config!);

    const order: AlgoOrderEntity<AlgoOrderRootType.STOP> = {
      ...this.baseOrder(values as unknown as PaxeerNetworkOrder),
      trigger_price: values.trigger_price!,
      algo_type: AlgoOrderRootType.STOP,
      type: OrderType.LIMIT,
      quantity: values["order_quantity"]!,
      price: values["order_price"],
      trigger_price_type: TriggerPriceType.MARK_PRICE,
    };

    return pick(
      [
        "symbol",
        "trigger_price",
        "algo_type",
        "type",
        "quantity",
        "price",
        "trigger_price_type",
        "side",
        "reduce_only",
        "margin_mode",
        "visible_quantity",
      ],
      order,
    );
  }

  /**
   * Runs validations using validation chain
   * Implements template method hook
   */
  protected runValidations(
    values: AlgoOrderEntity,
    config: ValuesDepConfig,
  ): OrderValidationResult {
    // Cast to PaxeerNetworkOrder for baseValidate method which expects PaxeerNetworkOrder
    const paxeerNetworkValues = values as unknown as PaxeerNetworkOrder;
    const errors = this.baseValidate(paxeerNetworkValues, config);

    // Validate trigger price
    // Access trigger_price from AlgoOrderEntity - using type assertion because
    // AlgoOrderEntity may have different field structures depending on the algo type
    const triggerPrice = (values as { trigger_price?: number | string })
      .trigger_price;

    // Check if trigger_price is missing or 0 (both should be treated as required error)
    if (!triggerPrice || triggerPrice === 0) {
      errors.trigger_price = OrderValidation.required("trigger_price");
    } else {
      const triggerError = this.triggerPriceValidationStrategy.validate(
        { trigger_price: triggerPrice },
        config,
      );
      if (triggerError) {
        errors.trigger_price = triggerError;
      }
    }

    // Validate order price using trigger price as base
    // Note: For stop orders, we need to validate order_price against trigger_price range
    // Access order_price and side from values - may be in different field names
    // (order_price in input form, price in AlgoOrderEntity)
    const valuesWithFields = values as {
      order_price?: number | string;
      price?: number | string;
      side?: OrderSide | string;
    };
    const orderPrice = valuesWithFields.order_price ?? valuesWithFields.price;
    const triggerPriceForPriceValidation = triggerPrice;
    // Get side from paxeerNetworkValues if not in valuesWithFields
    const side = (valuesWithFields.side ?? paxeerNetworkValues.side) as
      | OrderSide
      | undefined;

    if (!orderPrice) {
      errors.order_price = OrderValidation.required("order_price");
    } else if (orderPrice && triggerPriceForPriceValidation && side) {
      // Create a modified config with trigger_price as base price
      const modifiedConfig = {
        ...config,
        markPrice: Number(triggerPriceForPriceValidation),
      };
      // side is guaranteed to be defined at this point due to the if condition
      const priceError = this.priceValidationStrategy.validate(
        {
          order_price: orderPrice,
          side: side as OrderSide,
          order_type: OrderType.LIMIT,
        },
        modifiedConfig,
      );
      if (priceError) {
        errors.order_price = priceError;
      }
    } else if (orderPrice && triggerPriceForPriceValidation) {
      // If side is missing, at least validate against quote_max and quote_min
      // This handles cases where side might not be available but we still need basic validation
      const { quote_max, quote_min } = config.symbol;
      const price = Number(orderPrice);
      if (quote_max && price > quote_max) {
        errors.order_price = OrderValidation.max("order_price", quote_max);
      } else if (quote_min && price < quote_min) {
        errors.order_price = OrderValidation.min("order_price", quote_min);
      }
    }

    return errors;
  }

  orderType: OrderType.STOP_LIMIT = OrderType.STOP_LIMIT;
}
