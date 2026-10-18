import { OrderSide } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { IsoTakerFeeBuffer, MaxQtySafetyFactor } from "../constants";

/**
 * @formulaId maxQtyIsolated
 * @name Maximum Tradeable Quantity for Isolated Margin
 * @formula max_notional = min((1 / leverage / imr_factor)^(5/4), symbol_max_notional)
 * @description
 *
 * ## Definition
 *
 * **maxQtyIsolated**: Maximum tradeable quantity for isolated margin positions
 *
 * This function calculates the maximum quantity that can be traded for an isolated margin position,
 * considering available balance, leverage, position limits, and pending orders.
 *
 * ## Business Rules
 *
 * ### For BUY Orders:
 * - If `reduce_only == False` and `position_qty >= 0` (long or no position): Use simplified formula
 * - If `reduce_only == False` and `position_qty < 0` (short position): Use binary search iteration
 * - If `reduce_only == True`: Return `MAX(0, -position_qty)` (can only reduce short position)
 *
 * ### For SELL Orders:
 * - If `reduce_only == False` and `position_qty <= 0` (short or no position): Use simplified formula
 * - If `reduce_only == False` and `position_qty > 0` (long position): Use binary search iteration
 * - If `reduce_only == True`: Return `MAX(0, position_qty)` (can only reduce long position)
 *
 * ### Binary Search Algorithm:
 * - Used for reverse position scenarios (e.g., buying when holding short position)
 * - Maximum 30 iterations
 * - Searches for maximum quantity that satisfies: `iso_order_frozen <= available_balance` and `open_notional <= max_notional`
 *
 * ## Example
 *
 * ```
 * order_side = BUY
 * reduce_only = False
 * position_qty = 5 (long)
 * available_balance = 1000 USDC
 * leverage = 25
 * mark_price = 100000 USDC
 * current_order_reference_price = 99900 USDC
 * max_notional = 10059467.44 USDC
 * pending_long_notional = 299200 USDC
 * max_qty = MIN(1000 / (1 / 25 + 0.0006) / 99900 * 0.995, (10059467.44 - 100000 * 5 - 299200) / 99900) = 0.245 BTC
 * ```
 *
 * @param inputs Input parameters for calculating maximum tradeable quantity
 * @returns Maximum tradeable quantity
 */
export function isolatedMarginRate(inputs: {
  leverage: number;
  isoTakerFeeBuffer?: number;
}): Decimal {
  // The risk engine rounds the leverage inverse UP to 10 decimal places
  // before adding the taker fee buffer.
  return new Decimal(1)
    .div(inputs.leverage)
    .toDecimalPlaces(10, Decimal.ROUND_UP)
    .add(inputs.isoTakerFeeBuffer ?? IsoTakerFeeBuffer);
}

/** Pending isolated order input for the risk-engine frozen simulation. */
export type IsolatedPendingOrder = {
  side: OrderSide;
  /**
   * Executable reference price of the order (limit price, or trigger price
   * for stop orders).
   */
  referencePrice: number;
  /** Remaining (unfilled) quantity. */
  quantity: number;
  /** Creation time in ms; the earlier order wins when prices are equal. */
  createdTime?: number;
};

/**
 * Closing-allocation priority of the risk engine: BUY orders close a short
 * position from the highest price down, SELL orders close a long position
 * from the lowest price up, and on equal price the earlier order wins.
 */
const reverseSidePriority = (
  a: IsolatedPendingOrder,
  b: IsolatedPendingOrder,
): number => {
  if (a.referencePrice !== b.referencePrice) {
    return a.side === OrderSide.BUY
      ? b.referencePrice - a.referencePrice
      : a.referencePrice - b.referencePrice;
  }
  return (a.createdTime ?? 0) - (b.createdTime ?? 0);
};

/** USDC quote precision the risk engine rounds each frozen amount to. */
const USDC_DECIMALS = 6;

/** Round one order's frozen amount UP to USDC precision, like the risk engine. */
const roundUpFrozen = (value: Decimal): Decimal =>
  value.toDecimalPlaces(USDC_DECIMALS, Decimal.ROUND_UP);

/**
 * Simulate the risk engine's close/open allocation and return the total
 * isolated frozen margin for a set of pending orders:
 *
 * - Orders on the position's side (and both sides when flat) are opening
 *   quantity and freeze `referencePrice * qty * marginRate`.
 * - Reverse-side orders consume `abs(positionQty)` as closing quantity in
 *   priority order; only their remainder freezes margin.
 *
 * Each order's frozen amount is rounded UP to USDC precision before summing,
 * matching the risk engine's per-order rounding.
 */
export function isolatedPendingOrdersFrozen(inputs: {
  positionQty: number;
  orders: IsolatedPendingOrder[];
  leverage: number;
  isoTakerFeeBuffer?: number;
}): Decimal {
  const { positionQty, orders, leverage, isoTakerFeeBuffer } = inputs;
  const marginRate = isolatedMarginRate({ leverage, isoTakerFeeBuffer });

  const reverseSide =
    positionQty > 0 ? OrderSide.SELL : positionQty < 0 ? OrderSide.BUY : null;

  let frozen = new Decimal(0);
  let closingBudget = new Decimal(positionQty).abs();

  if (reverseSide !== null) {
    const reverseOrders = orders
      .filter((order) => order.side === reverseSide)
      .sort(reverseSidePriority);

    for (const order of reverseOrders) {
      if (closingBudget.lte(0)) {
        frozen = frozen.add(
          roundUpFrozen(
            new Decimal(order.referencePrice)
              .mul(order.quantity)
              .mul(marginRate),
          ),
        );
        continue;
      }
      const orderQty = new Decimal(order.quantity);
      const closingQty = orderQty.lt(closingBudget) ? orderQty : closingBudget;
      const openQty = orderQty.sub(closingQty);
      closingBudget = closingBudget.sub(closingQty);
      if (openQty.gt(0)) {
        frozen = frozen.add(
          roundUpFrozen(
            new Decimal(order.referencePrice).mul(openQty).mul(marginRate),
          ),
        );
      }
    }
  }

  for (const order of orders) {
    if (reverseSide !== null && order.side === reverseSide) {
      continue;
    }
    frozen = frozen.add(
      roundUpFrozen(
        new Decimal(order.referencePrice).mul(order.quantity).mul(marginRate),
      ),
    );
  }

  return frozen;
}

/**
 * Additional isolated frozen margin introduced by a batch of same-side new
 * orders (a single order or scaled children), computed as
 * `frozenAfter - frozenBefore` over the same allocation simulation so the
 * closing-priority reallocation between new and existing pending orders is
 * priced the same way the risk engine does.
 */
export function additionalIsolatedOrderFrozenByOrders(inputs: {
  positionQty: number;
  newOrders: IsolatedPendingOrder[];
  pendingOrders: IsolatedPendingOrder[];
  leverage: number;
  isoTakerFeeBuffer?: number;
}): Decimal {
  const { positionQty, newOrders, pendingOrders, leverage, isoTakerFeeBuffer } =
    inputs;

  const frozenBefore = isolatedPendingOrdersFrozen({
    positionQty,
    orders: pendingOrders,
    leverage,
    isoTakerFeeBuffer,
  });
  const frozenAfter = isolatedPendingOrdersFrozen({
    positionQty,
    orders: [...pendingOrders, ...newOrders],
    leverage,
    isoTakerFeeBuffer,
  });

  const additionalFrozen = frozenAfter.sub(frozenBefore);
  return additionalFrozen.gt(0) ? additionalFrozen : new Decimal(0);
}

/**
 * Aggregate fallback for callers without per-order data: prices the new
 * order's full notional (including any closing portion) plus pending orders
 * against `existingFrozen`. Prefer {@link additionalIsolatedOrderFrozenByOrders}
 * when per-order price/quantity/time data is available.
 */
export function additionalIsolatedOrderFrozen(inputs: {
  newOrderNotional: number;
  pendingOrders?: Array<{ referencePrice: number; quantity: number }>;
  existingFrozen?: number;
  leverage: number;
  isoTakerFeeBuffer?: number;
}): Decimal {
  const {
    newOrderNotional,
    pendingOrders = [],
    existingFrozen = 0,
    leverage,
    isoTakerFeeBuffer = IsoTakerFeeBuffer,
  } = inputs;

  const totalOrderNotional = pendingOrders.reduce(
    (acc, order) =>
      acc.add(new Decimal(order.referencePrice).mul(order.quantity)),
    new Decimal(newOrderNotional),
  );
  const additionalFrozen = totalOrderNotional
    .mul(isolatedMarginRate({ leverage, isoTakerFeeBuffer }))
    .sub(existingFrozen);

  return additionalFrozen.gt(0) ? additionalFrozen : new Decimal(0);
}

export function maxQtyForIsolatedMargin(inputs: {
  /**
   * @description Trading symbol
   */
  symbol: string;
  /**
   * @description Order side (BUY or SELL)
   */
  orderSide: OrderSide;
  /**
   * @description Current order reference price
   */
  currentOrderReferencePrice: number;
  /**
   * @description Eligible available collateral denominated in USDC
   */
  availableBalance: number;
  /**
   * @description Leverage for the trading pair
   */
  leverage: number;
  /**
   * @description Base initial margin rate
   */
  baseIMR: number;
  /**
   * @description IMR calculation factor
   */
  IMR_Factor: number;
  /**
   * @description Mark price
   */
  markPrice: number;
  /**
   * @description Current position quantity (positive for long, negative for short)
   */
  positionQty: number;
  /**
   * @description Pending long orders (excluding current order)
   */
  pendingLongOrders: Array<{ referencePrice: number; quantity: number }>;
  /**
   * @description Pending sell orders (excluding current order)
   */
  pendingSellOrders: Array<{ referencePrice: number; quantity: number }>;
  /**
   * @description Already frozen margin for long orders
   */
  isoOrderFrozenLong: number;
  /**
   * @description Already frozen margin for short orders
   */
  isoOrderFrozenShort: number;
  /**
   * Per-order isolated pending orders (both sides, reduce-only excluded).
   * When provided, the flip binary search uses the risk-engine close/open
   * allocation simulation. When omitted, it falls back to the aggregate
   * `isoOrderFrozenLong/Short` approximation, which freezes the closing
   * portion too (kept for backward compatibility).
   */
  isolatedPendingOrders?: IsolatedPendingOrder[];
  /**
   * @description Maximum notional value for the symbol
   */
  symbolMaxNotional: number;
  /**
   * @description Precision threshold for binary search (default: 1)
   */
  epsilon?: number;
  /**
   * @description Fee buffer reserved in isolated frozen margin (default: 0.0006)
   */
  isoTakerFeeBuffer?: number;
}): number {
  const {
    orderSide,
    currentOrderReferencePrice,
    availableBalance,
    leverage,
    IMR_Factor,
    markPrice,
    positionQty,
    pendingLongOrders,
    pendingSellOrders,
    symbolMaxNotional,
    epsilon = 1,
    isoTakerFeeBuffer = IsoTakerFeeBuffer,
  } = inputs;

  const marginRate = isolatedMarginRate({ leverage, isoTakerFeeBuffer });

  // Calculate max_notional
  const maxNotional = Math.min(
    new Decimal(1)
      .div(new Decimal(leverage).mul(IMR_Factor))
      .pow(5 / 4)
      .toNumber(),
    symbolMaxNotional,
  );

  // Handle BUY orders
  if (orderSide === OrderSide.BUY) {
    if (positionQty >= 0) {
      // Long position or no position - use simplified formula
      const pendingLongNotional = pendingLongOrders.reduce(
        (acc, order) =>
          acc +
          new Decimal(order.referencePrice).mul(order.quantity).toNumber(),
        0,
      );
      const maxQtyByBalance = new Decimal(availableBalance)
        .div(marginRate)
        .div(currentOrderReferencePrice)
        .mul(MaxQtySafetyFactor)
        .toNumber();
      const maxQtyByNotional = new Decimal(maxNotional)
        .sub(new Decimal(markPrice).mul(positionQty))
        .sub(pendingLongNotional)
        .div(currentOrderReferencePrice)
        .toNumber();
      return Math.max(0, Math.min(maxQtyByBalance, maxQtyByNotional));
    } else {
      // Short position - use binary search
      return maxQtyIsolatedBinarySearch(
        {
          currentOrderReferencePrice,
          availableBalance,
          leverage,
          baseIMR: inputs.baseIMR,
          IMR_Factor,
          positionQty,
          pendingLongOrders,
          pendingSellOrders,
          isoOrderFrozenLong: inputs.isoOrderFrozenLong,
          isoOrderFrozenShort: inputs.isoOrderFrozenShort,
          isoTakerFeeBuffer,
          isolatedPendingOrders: inputs.isolatedPendingOrders,
        },
        maxNotional,
        epsilon,
        OrderSide.BUY,
      );
    }
  } else {
    // SELL orders
    if (positionQty <= 0) {
      // Short position or no position - use simplified formula
      const pendingSellNotional = pendingSellOrders.reduce(
        (acc, order) =>
          acc +
          new Decimal(order.referencePrice).mul(order.quantity).toNumber(),
        0,
      );
      const maxQtyByBalance = new Decimal(availableBalance)
        .div(marginRate)
        .div(currentOrderReferencePrice)
        .mul(MaxQtySafetyFactor)
        .toNumber();
      // Use abs(position_qty) for short positions
      const maxQtyByNotional = new Decimal(maxNotional)
        .sub(new Decimal(markPrice).mul(Math.abs(positionQty)))
        .sub(pendingSellNotional)
        .div(currentOrderReferencePrice)
        .toNumber();
      return Math.max(0, Math.min(maxQtyByBalance, maxQtyByNotional));
    } else {
      // Long position - use binary search
      return maxQtyIsolatedBinarySearch(
        {
          currentOrderReferencePrice,
          availableBalance,
          leverage,
          baseIMR: inputs.baseIMR,
          IMR_Factor,
          positionQty,
          pendingLongOrders,
          pendingSellOrders,
          isoOrderFrozenLong: inputs.isoOrderFrozenLong,
          isoOrderFrozenShort: inputs.isoOrderFrozenShort,
          isoTakerFeeBuffer,
          isolatedPendingOrders: inputs.isolatedPendingOrders,
        },
        maxNotional,
        epsilon,
        OrderSide.SELL,
      );
    }
  }
}

/**
 * Binary search algorithm for calculating maxQtyIsolated in reverse position scenarios
 * @param inputs Input parameters
 * @param maxNotional Maximum notional value
 * @param epsilon Precision threshold
 * @param orderSide Order side (BUY or SELL)
 * @returns Maximum tradeable quantity
 */
function maxQtyIsolatedBinarySearch(
  inputs: {
    currentOrderReferencePrice: number;
    availableBalance: number;
    leverage: number;
    baseIMR: number;
    IMR_Factor: number;
    positionQty: number;
    pendingLongOrders: Array<{ referencePrice: number; quantity: number }>;
    pendingSellOrders: Array<{ referencePrice: number; quantity: number }>;
    isoOrderFrozenLong: number;
    isoOrderFrozenShort: number;
    isoTakerFeeBuffer: number;
    isolatedPendingOrders?: IsolatedPendingOrder[];
  },
  maxNotional: number,
  epsilon: number,
  orderSide: OrderSide,
): number {
  const {
    currentOrderReferencePrice,
    availableBalance,
    leverage,
    positionQty,
    pendingLongOrders,
    pendingSellOrders,
    isoOrderFrozenLong,
    isoOrderFrozenShort,
    isoTakerFeeBuffer,
    isolatedPendingOrders,
  } = inputs;
  // baseIMR and IMR_Factor are kept in the interface for future use but not currently used in binary search
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const _baseIMR = inputs.baseIMR;
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const _IMR_Factor = inputs.IMR_Factor;

  // Calculate sum of pending orders quantity
  const pendingOrdersQty =
    orderSide === OrderSide.BUY
      ? pendingLongOrders.reduce((acc, order) => acc + order.quantity, 0)
      : pendingSellOrders.reduce((acc, order) => acc + order.quantity, 0);

  // Initialize search interval
  let left = Math.max(0, Math.max(0, Math.abs(positionQty)) - pendingOrdersQty);
  let right = new Decimal(maxNotional)
    .div(currentOrderReferencePrice)
    .add(Math.abs(positionQty))
    .toNumber();

  // Binary search (max 30 iterations)
  for (let i = 0; i < 30; i++) {
    const mid = (left + right) / 2;

    const pendingOrders =
      orderSide === OrderSide.BUY ? pendingLongOrders : pendingSellOrders;
    const existingFrozen =
      orderSide === OrderSide.BUY ? isoOrderFrozenLong : isoOrderFrozenShort;
    const orderFrozen =
      isolatedPendingOrders !== undefined
        ? additionalIsolatedOrderFrozenByOrders({
            positionQty,
            newOrders: [
              {
                side: orderSide,
                referencePrice: currentOrderReferencePrice,
                quantity: mid,
              },
            ],
            pendingOrders: isolatedPendingOrders,
            leverage,
            isoTakerFeeBuffer,
          })
        : additionalIsolatedOrderFrozen({
            newOrderNotional: new Decimal(mid)
              .mul(currentOrderReferencePrice)
              .toNumber(),
            pendingOrders,
            existingFrozen,
            leverage,
            isoTakerFeeBuffer,
          });

    // Calculate open notional after order execution
    const newPositionQty =
      orderSide === OrderSide.BUY ? positionQty + mid : positionQty - mid;
    const openNotional = new Decimal(Math.abs(newPositionQty)).mul(
      currentOrderReferencePrice,
    );

    // Check conditions
    const frozenOk = orderFrozen.lte(availableBalance);
    const notionalOk = openNotional.lte(maxNotional);

    if (frozenOk && notionalOk) {
      left = mid;
      // Early termination if precision is reached
      if (new Decimal(availableBalance).sub(orderFrozen).lte(epsilon)) {
        break;
      }
    } else {
      right = mid;
    }
  }

  return Math.max(0, left);
}
