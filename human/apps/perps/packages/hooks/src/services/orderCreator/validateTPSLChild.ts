import {
  API,
  AlgoOrderType,
  OrderSide,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import { isPositionalTPSL, isTPSLTriggered } from "@paxeer-network/utils";
import { ValuesDepConfig, OrderValidationResult } from "./interface";
import { TPSLValidationStrategy } from "./validators/TPSLValidationStrategy";

/** Validate a leaf using its effective update type and the TP/SL form rules. */
export function validateTPSLChild(
  order: Partial<API.AlgoOrder>,
  changes: {
    type?: OrderType;
    trigger_price?: number | string;
    price?: number | string;
  },
  config: Pick<ValuesDepConfig, "symbol" | "markPrice">,
  parent?: Partial<API.AlgoOrder>,
): OrderValidationResult {
  const leg = order.algo_type === AlgoOrderType.TAKE_PROFIT ? "tp" : "sl";
  const effectiveType = changes.type ?? order.type;
  const typeChanged =
    (order.type === OrderType.LIMIT) !== (effectiveType === OrderType.LIMIT);
  const triggerPriceUnchanged =
    changes.trigger_price == null ||
    Number(changes.trigger_price) === Number(order.trigger_price);
  const skipTriggerPriceAgainstMark =
    effectiveType === OrderType.LIMIT &&
    !typeChanged &&
    (parent?.is_triggered === true || isTPSLTriggered(order)) &&
    triggerPriceUnchanged;
  const errors = new TPSLValidationStrategy().validate(
    {
      side: order.side === OrderSide.SELL ? OrderSide.BUY : OrderSide.SELL,
      position_type: isPositionalTPSL(order)
        ? PositionType.FULL
        : PositionType.PARTIAL,
      [`${leg}_trigger_price`]: changes.trigger_price ?? order.trigger_price,
      [`${leg}_order_price`]: typeChanged
        ? changes.price
        : (changes.price ?? order.price),
      [`${leg}_order_type`]:
        effectiveType === OrderType.LIMIT ? OrderType.LIMIT : OrderType.MARKET,
    },
    // No quantity is ever set below, so the strategy's quantity checks are
    // unreachable; maxQty is a required placeholder in ValuesDepConfig.
    {
      ...config,
      maxQty: 0,
      skipTPSLTriggerPriceAgainstMark: skipTriggerPriceAgainstMark
        ? { [leg]: true }
        : undefined,
    },
  );
  const mapped: OrderValidationResult = {};
  if (errors?.[`${leg}_trigger_price`])
    mapped.trigger_price = errors[`${leg}_trigger_price`];
  if (errors?.[`${leg}_order_price`])
    mapped.order_price = errors[`${leg}_order_price`];
  return mapped;
}
