import { type API, MarginMode, OrderSide } from "@paxeer-network/types";
import {
  resolveIsolatedPendingOrders,
  toIsolatedPendingOrders,
} from "../isolatedPendingOrders";

const symbol = "PERP_BTC_USDC";

const order = (overrides: Partial<API.Order> = {}): API.Order =>
  ({
    symbol,
    status: "INCOMPLETE",
    side: OrderSide.SELL,
    order_id: 1,
    user_id: 1,
    price: 100,
    type: "LIMIT",
    quantity: 2,
    visible: 2,
    executed: 0,
    total_executed_quantity: 0.5,
    created_time: 1000,
    updated_time: 1000,
    reduce_only: false,
    margin_mode: MarginMode.ISOLATED,
    ...overrides,
  }) as API.Order;

describe("toIsolatedPendingOrders", () => {
  it("maps remaining quantity with the order price and creation time", () => {
    const result = toIsolatedPendingOrders([order()], {
      symbol,
      fallbackPrice: 90,
    });

    expect(result).toEqual([
      {
        side: OrderSide.SELL,
        referencePrice: 100,
        quantity: 1.5,
        createdTime: 1000,
      },
    ]);
  });

  it("excludes other symbols, cross orders, and reduce-only orders", () => {
    const result = toIsolatedPendingOrders(
      [
        order({ order_id: 2, symbol: "PERP_ETH_USDC" }),
        order({ order_id: 3, margin_mode: MarginMode.CROSS }),
        order({ order_id: 4, reduce_only: true }),
      ],
      { symbol, fallbackPrice: 90 },
    );

    expect(result).toEqual([]);
  });

  it("falls back to the trigger price and then the fallback price", () => {
    const stop = order({
      order_id: 5,
      price: null,
      trigger_price: 105,
      type: "STOP_MARKET",
    });
    const market = order({ order_id: 6, price: null, type: "MARKET" });

    const result = toIsolatedPendingOrders([stop, market], {
      symbol,
      fallbackPrice: 90,
    });

    expect(result.map((item) => item.referencePrice)).toEqual([105, 90]);
  });
});

describe("resolveIsolatedPendingOrders", () => {
  it("returns undefined while the order stream has not loaded", () => {
    expect(
      resolveIsolatedPendingOrders(null, {
        symbol,
        fallbackPrice: 90,
        pendingLongQty: 0,
        pendingShortQty: 0,
      }),
    ).toBeUndefined();
  });

  it("keeps per-order data when the stream covers the aggregates", () => {
    // reduce-only quantity counts toward the stream side of the comparison
    const result = resolveIsolatedPendingOrders(
      [
        order({ order_id: 7, quantity: 2, total_executed_quantity: 0 }),
        order({
          order_id: 8,
          reduce_only: true,
          quantity: 1,
          total_executed_quantity: 0,
        }),
      ],
      {
        symbol,
        fallbackPrice: 90,
        pendingLongQty: 0,
        pendingShortQty: 3,
      },
    );

    expect(result).toHaveLength(1);
    expect(result?.[0].quantity).toBe(2);
  });

  it("falls back when aggregates report quantity the stream is missing", () => {
    // WS-merged order without margin_mode would be invisible to the strict
    // filter, leaving the aggregates ahead of the stream
    const result = resolveIsolatedPendingOrders(
      [order({ order_id: 9, quantity: 2 })],
      {
        symbol,
        fallbackPrice: 90,
        pendingLongQty: 0,
        pendingShortQty: 5,
      },
    );

    expect(result).toBeUndefined();
  });

  it("falls back when a margin_mode-less order hides quantity from the mapper", () => {
    // The stream-side totals must use the same inclusion rule as the mapper:
    // this WS-merged order counts toward the aggregates but is invisible to
    // the per-order array, so the resolver must not let it pass the check
    const result = resolveIsolatedPendingOrders(
      [
        order({ order_id: 12, quantity: 1, total_executed_quantity: 0 }),
        order({
          order_id: 13,
          quantity: 1,
          total_executed_quantity: 0,
          margin_mode: undefined,
        }),
      ],
      {
        symbol,
        fallbackPrice: 90,
        pendingLongQty: 0,
        pendingShortQty: 2,
      },
    );

    expect(result).toBeUndefined();
  });

  it("ignores margin_mode-less quantity the aggregates do not claim", () => {
    const result = resolveIsolatedPendingOrders(
      [
        order({ order_id: 14, quantity: 2, total_executed_quantity: 0 }),
        order({
          order_id: 15,
          quantity: 1,
          total_executed_quantity: 0,
          margin_mode: undefined,
        }),
      ],
      {
        symbol,
        fallbackPrice: 90,
        pendingLongQty: 0,
        pendingShortQty: 2,
      },
    );

    // The unknown-mode order is most likely cross and is excluded from both
    // the comparison and the mapped result
    expect(result).toHaveLength(1);
    expect(result?.[0].quantity).toBe(2);
  });

  it("does not fall back when the stream simply has extra cross quantity", () => {
    const result = resolveIsolatedPendingOrders(
      [
        order({ order_id: 10, quantity: 2, total_executed_quantity: 0 }),
        order({
          order_id: 11,
          quantity: 3,
          total_executed_quantity: 0,
          margin_mode: MarginMode.CROSS,
        }),
      ],
      {
        symbol,
        fallbackPrice: 90,
        pendingLongQty: 0,
        pendingShortQty: 2,
      },
    );

    expect(result).toHaveLength(1);
  });
});
