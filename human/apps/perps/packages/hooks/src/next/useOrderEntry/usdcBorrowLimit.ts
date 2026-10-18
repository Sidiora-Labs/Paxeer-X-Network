import { account } from "@paxeer-network/perp";
import {
  MarginMode,
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

type IsolatedPendingOrder = account.IsolatedPendingOrder;

export const DEFAULT_USDC_BORROW_LIMIT = 50_000;

export class USDCBorrowLimitExceededError extends Error {
  readonly projectedBorrow: number;
  readonly borrowLimit: number;

  constructor(projectedBorrow: number, borrowLimit: number) {
    super("Projected USDC borrow limit exceeded");
    this.name = "USDCBorrowLimitExceededError";
    this.projectedBorrow = projectedBorrow;
    this.borrowLimit = borrowLimit;
  }
}

/**
 * The backend returns `negative_usdc_threshold` as a positive number (e.g. 50000).
 * Missing, non-finite, or signed values fall back to a normalized positive limit
 * so the submission guard always compares against a concrete number.
 */
export const normalizeUSDCBorrowLimit = (
  value: number | null | undefined,
): number => {
  return typeof value === "number" && Number.isFinite(value)
    ? Math.abs(value)
    : DEFAULT_USDC_BORROW_LIMIT;
};

export type ProjectedUSDCBorrowInputs = {
  marginMode?: MarginMode;
  reduceOnly?: boolean;
  orderSide?: OrderSide;
  orderQuantity: number;
  orderNotional: number;
  leverage: number;
  markPrice: number;
  positionQty?: number;
  pendingLongQty?: number;
  pendingShortQty?: number;
  /**
   * Per-order isolated open orders for the symbol (reduce-only excluded).
   * When provided, the frozen delta is simulated with the risk-engine
   * close/open allocation (price + time priority). When omitted,
   * `pendingLongQty`/`pendingShortQty` are approximated at `markPrice`.
   */
  pendingOrders?: IsolatedPendingOrder[];
  /**
   * Per-child entries of the submitted order (scaled children). Defaults to a
   * single entry derived from `orderQuantity`/`orderNotional`.
   */
  newOrderEntries?: Array<{ quantity: number; referencePrice: number }>;
  usdcHolding: number;
  usdcPendingShort?: number;
  usdcIsolatedOrderFrozen?: number;
  totalUnsettledPnL?: number;
};

export type GeneratedOrderForBorrowProjection = Partial<PaxeerNetworkOrder> & {
  orders?: Partial<PaxeerNetworkOrder>[];
  price?: string | number;
  quantity?: string | number;
  type?: OrderType;
};

export const aggregateOrderQuantityAndNotional = (
  orders: Array<{ quantity: number; referencePrice: number }>,
): {
  orderQuantity: number;
  orderNotional: number;
  orders: Array<{ quantity: number; referencePrice: number }>;
} | null => {
  if (orders.length === 0) {
    return null;
  }

  let orderQuantity = new Decimal(0);
  let orderNotional = new Decimal(0);

  for (const order of orders) {
    if (
      !Number.isFinite(order.quantity) ||
      order.quantity <= 0 ||
      !Number.isFinite(order.referencePrice) ||
      order.referencePrice <= 0
    ) {
      return null;
    }
    orderQuantity = orderQuantity.add(order.quantity);
    orderNotional = orderNotional.add(
      new Decimal(order.quantity).mul(order.referencePrice),
    );
  }

  return {
    orderQuantity: orderQuantity.toNumber(),
    orderNotional: orderNotional.toNumber(),
    orders: orders.map((order) => ({ ...order })),
  };
};

export const getOrderQuantityAndNotional = (inputs: {
  generatedOrder: GeneratedOrderForBorrowProjection;
  formattedOrder: Partial<PaxeerNetworkOrder>;
  marginMode: MarginMode;
  getReferencePrice: (order: Partial<PaxeerNetworkOrder>) => number | null;
}): {
  orderQuantity: number;
  orderNotional: number;
  orders: Array<{ quantity: number; referencePrice: number }>;
} | null => {
  const { generatedOrder, formattedOrder, marginMode, getReferencePrice } =
    inputs;

  if (formattedOrder.order_type === OrderType.SCALED) {
    if (
      !Array.isArray(generatedOrder.orders) ||
      generatedOrder.orders.length === 0
    ) {
      return null;
    }

    return aggregateOrderQuantityAndNotional(
      generatedOrder.orders.map((childOrder) => ({
        quantity: Number(childOrder.order_quantity),
        referencePrice: getReferencePrice(childOrder) ?? NaN,
      })),
    );
  }

  const normalizedOrder: Partial<PaxeerNetworkOrder> = {
    ...formattedOrder,
    ...generatedOrder,
    order_type: formattedOrder.order_type,
    order_type_ext: formattedOrder.order_type_ext,
    order_price:
      generatedOrder.order_price ??
      (generatedOrder.price === undefined
        ? undefined
        : String(generatedOrder.price)) ??
      formattedOrder.order_price,
    order_quantity:
      generatedOrder.order_quantity ??
      (generatedOrder.quantity === undefined
        ? undefined
        : String(generatedOrder.quantity)) ??
      formattedOrder.order_quantity,
    slippage: formattedOrder.slippage,
    margin_mode: marginMode,
  };
  const referencePrice = getReferencePrice(normalizedOrder);
  const orderQuantity = Number(normalizedOrder.order_quantity);

  if (
    !Number.isFinite(orderQuantity) ||
    orderQuantity <= 0 ||
    !referencePrice ||
    !Number.isFinite(referencePrice)
  ) {
    return null;
  }

  return aggregateOrderQuantityAndNotional([
    { quantity: orderQuantity, referencePrice },
  ]);
};

export const calculateProjectedUSDCBorrow = (
  inputs: ProjectedUSDCBorrowInputs,
): number | null => {
  const {
    marginMode,
    reduceOnly,
    orderSide,
    orderQuantity,
    orderNotional,
    leverage,
    markPrice,
    positionQty = 0,
    pendingLongQty = 0,
    pendingShortQty = 0,
    pendingOrders,
    newOrderEntries,
    usdcHolding,
    usdcPendingShort = 0,
    usdcIsolatedOrderFrozen = 0,
    totalUnsettledPnL = 0,
  } = inputs;

  if (marginMode !== MarginMode.ISOLATED || reduceOnly) {
    return 0;
  }

  const numericInputs = [
    orderQuantity,
    orderNotional,
    leverage,
    positionQty,
    pendingLongQty,
    pendingShortQty,
    usdcHolding,
    usdcPendingShort,
    usdcIsolatedOrderFrozen,
    totalUnsettledPnL,
  ];

  if (
    !orderSide ||
    numericInputs.some((value) => !Number.isFinite(value)) ||
    orderQuantity <= 0 ||
    orderNotional <= 0 ||
    leverage <= 0
  ) {
    return null;
  }

  const isReverseOrder =
    (orderSide === OrderSide.BUY && positionQty < 0) ||
    (orderSide === OrderSide.SELL && positionQty > 0);

  // Per-order data takes precedence. Without it, approximate both sides of
  // the aggregate pending quantity at mark price; the mark price is only
  // load-bearing for reverse-side closing allocation.
  let effectivePendingOrders: IsolatedPendingOrder[];
  if (pendingOrders !== undefined) {
    effectivePendingOrders = pendingOrders;
  } else {
    const reversePendingQty =
      positionQty > 0 ? pendingShortQty : pendingLongQty;
    if (
      isReverseOrder &&
      reversePendingQty > 0 &&
      !(Number.isFinite(markPrice) && markPrice > 0)
    ) {
      return null;
    }

    effectivePendingOrders = [];
    if (Number.isFinite(markPrice) && markPrice > 0) {
      if (pendingLongQty > 0) {
        effectivePendingOrders.push({
          side: OrderSide.BUY,
          referencePrice: markPrice,
          quantity: pendingLongQty,
        });
      }
      if (pendingShortQty > 0) {
        effectivePendingOrders.push({
          side: OrderSide.SELL,
          referencePrice: markPrice,
          quantity: pendingShortQty,
        });
      }
    }
  }

  const newOrderEntriesResolved =
    newOrderEntries !== undefined && newOrderEntries.length > 0
      ? newOrderEntries
      : [
          {
            quantity: orderQuantity,
            referencePrice: orderNotional / orderQuantity,
          },
        ];

  // The risk engine prices closing priority (BUY high→low, SELL low→high,
  // earlier order first) and only freezes the opening remainder, including
  // the reallocation this order causes among existing pending orders.
  const additionalFrozen = account.additionalIsolatedOrderFrozenByOrders({
    positionQty,
    newOrders: newOrderEntriesResolved.map((entry) => ({
      ...entry,
      side: orderSide,
      createdTime: Date.now(),
    })),
    pendingOrders: effectivePendingOrders,
    leverage,
  });

  // Orders that add no frozen margin (pure closing, no reallocation) must not
  // be blocked by the existing negative USDC balance.
  if (additionalFrozen.lte(0)) {
    return 0;
  }

  const effectiveUSDCBalance = new Decimal(usdcHolding)
    .add(usdcPendingShort)
    .add(totalUnsettledPnL)
    .sub(Math.abs(usdcIsolatedOrderFrozen));
  const projectedBorrow = effectiveUSDCBalance.sub(additionalFrozen).negated();

  return projectedBorrow.gt(0) ? projectedBorrow.toNumber() : 0;
};

export const assertUSDCBorrowWithinLimit = (
  projectedBorrow: number | null,
  borrowLimit: number,
) => {
  if (projectedBorrow !== null && projectedBorrow > borrowLimit) {
    throw new USDCBorrowLimitExceededError(projectedBorrow, borrowLimit);
  }
};
