import { account } from "@paxeer-network/perp";
import { type API, MarginMode, OrderSide } from "@paxeer-network/types";

type IsolatedPendingOrder = account.IsolatedPendingOrder;

/**
 * Map a symbol's open orders (normal + algo) to risk-engine style pending
 * order inputs for the isolated frozen simulation.
 *
 * - reduce-only orders never open positions and are excluded;
 * - cross orders are excluded (their margin is not isolated frozen);
 * - remaining quantity = `quantity - total_executed_quantity`;
 * - reference price = limit price, falling back to the stop trigger price,
 *   then to `fallbackPrice` (mark price) when neither is usable.
 */
export const toIsolatedPendingOrders = (
  orders: Array<API.Order | API.AlgoOrder> | null | undefined,
  inputs: { symbol: string; fallbackPrice: number },
): IsolatedPendingOrder[] => {
  const { symbol, fallbackPrice } = inputs;
  if (!Array.isArray(orders) || orders.length === 0) {
    return [];
  }

  const result: IsolatedPendingOrder[] = [];

  for (const order of orders) {
    if (order.symbol !== symbol) continue;
    if (order.margin_mode !== MarginMode.ISOLATED) continue;
    if (order.reduce_only) continue;

    const remainingQty =
      Number(order.quantity) - Number(order.total_executed_quantity ?? 0);
    const rawPrice =
      (order as API.Order).price ?? (order as API.AlgoOrder).trigger_price;
    const referencePrice =
      Number.isFinite(Number(rawPrice)) && Number(rawPrice) > 0
        ? Number(rawPrice)
        : fallbackPrice;

    if (
      !Number.isFinite(remainingQty) ||
      remainingQty <= 0 ||
      !Number.isFinite(referencePrice) ||
      referencePrice <= 0
    ) {
      continue;
    }

    result.push({
      side: order.side === OrderSide.SELL ? OrderSide.SELL : OrderSide.BUY,
      referencePrice,
      quantity: remainingQty,
      createdTime: order.created_time ?? 0,
    });
  }

  return result;
};

/** Quantities below this are treated as dust when comparing totals. */
const DUST_QTY = 1e-8;

/**
 * Resolve per-order pending inputs, falling back to `undefined` (aggregate
 * approximation) when the order stream cannot be trusted:
 *
 * - the stream has not loaded yet; or
 * - the position aggregates report pending quantity the stream does not
 *   positively cover as isolated — WebSocket order payloads may omit
 *   `margin_mode`, and the query only covers the first page (100 orders).
 *
 * Only orders explicitly marked `ISOLATED` count toward the stream side
 * (reduce-only included, since the aggregates contain them but they never
 * freeze margin). Quantity the aggregates claim beyond that — including
 * `margin_mode`-less orders the mapper would drop — forces the conservative
 * fallback, because under-counting pending orders would under-freeze margin.
 */
export const resolveIsolatedPendingOrders = (
  orders: Array<API.Order | API.AlgoOrder> | null | undefined,
  inputs: {
    symbol: string;
    fallbackPrice: number;
    /** Isolated position aggregate of pending BUY quantity. */
    pendingLongQty: number;
    /** Isolated position aggregate of pending SELL quantity. */
    pendingShortQty: number;
  },
): IsolatedPendingOrder[] | undefined => {
  if (orders === null || orders === undefined) {
    return undefined;
  }

  let streamLongQty = 0;
  let streamShortQty = 0;

  for (const order of orders) {
    if (order.symbol !== inputs.symbol) continue;
    if (order.margin_mode !== MarginMode.ISOLATED) continue;

    const remainingQty =
      Number(order.quantity) - Number(order.total_executed_quantity ?? 0);
    if (!Number.isFinite(remainingQty) || remainingQty <= 0) continue;

    if (order.side === OrderSide.SELL) {
      streamShortQty += remainingQty;
    } else {
      streamLongQty += remainingQty;
    }
  }

  const missingQty =
    Math.max(0, inputs.pendingLongQty - streamLongQty) +
    Math.max(0, inputs.pendingShortQty - streamShortQty);

  if (missingQty > DUST_QTY) {
    return undefined;
  }

  return toIsolatedPendingOrders(orders, inputs);
};
