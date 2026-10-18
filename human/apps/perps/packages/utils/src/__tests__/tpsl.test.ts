/// <reference types="jest" />
import {
  getTPSLEstimatePrice,
  getTPSLLeg,
  getTPSLQuantity,
  isEntirePositionTPSL,
  isPositionalTPSL,
  isTPSLTriggered,
  matchesTPSLPosition,
  withTPSLProvenance,
} from "../tpsl";

const leaf = {
  symbol: "PERP_ETH_USDC",
  type: "LIMIT",
  algo_type: "TAKE_PROFIT",
  parent_algo_type: "POSITIONAL_TP_SL",
  trigger_price: 4100,
  price: 4110,
  quantity: 0,
  is_triggered: false,
  algo_status: "NEW",
};

describe("Positional lifecycle", () => {
  it("uses live quantity only before trigger", () => {
    expect(isPositionalTPSL(leaf)).toBe(true);
    expect(isEntirePositionTPSL(leaf)).toBe(true);
    expect(getTPSLQuantity(leaf, 2)).toBe(2);
    expect(getTPSLQuantity(leaf, 7)).toBe(7);
    expect(getTPSLQuantity(leaf)).toBeUndefined();
    const triggered = { ...leaf, is_triggered: true, quantity: 2 };
    expect(isEntirePositionTPSL(triggered)).toBe(false);
    expect(getTPSLQuantity(triggered, 7)).toBe(2);
    expect(
      getTPSLQuantity({ ...triggered, total_executed_quantity: 1 }, 9),
    ).toBe(2);
    expect(getTPSLQuantity({ ...triggered, quantity: 0 }, 7)).toBeUndefined();
  });
  it("uses execution evidence and trigger flags, not NEW or zero alone", () => {
    expect(isTPSLTriggered({ ...leaf, triggered: true })).toBe(true);
    expect(isTPSLTriggered({ ...leaf, trigger_time: 1000 })).toBe(true);
    expect(isTPSLTriggered({ ...leaf, is_triggered: true })).toBe(true);
    expect(isTPSLTriggered({ ...leaf, trigger_status: "SUCCESS" })).toBe(true);
    expect(isTPSLTriggered({ ...leaf, total_executed_quantity: 1 })).toBe(true);
    expect(isTPSLTriggered({ ...leaf, algo_status: "PARTIAL_FILLED" })).toBe(
      true,
    );
    expect(isTPSLTriggered({ ...leaf, quantity: 4 })).toBe(false);
  });
  it("does not reconstruct historical quantities from live positions", () => {
    expect(getTPSLQuantity(leaf, 5, true)).toBeUndefined();
    const cancelled = { ...leaf, algo_status: "CANCELLED" };
    expect(isEntirePositionTPSL(cancelled)).toBe(true);
    expect(getTPSLQuantity(cancelled, 5)).toBeUndefined();
    expect(
      getTPSLQuantity(
        { ...cancelled, is_triggered: true, quantity: 2 },
        5,
        true,
      ),
    ).toBe(2);
  });
  it("matches Cross and Isolated separately with CROSS default", () => {
    expect(
      matchesTPSLPosition(leaf, { symbol: leaf.symbol, margin_mode: "CROSS" }),
    ).toBe(true);
    expect(
      matchesTPSLPosition(leaf, {
        symbol: leaf.symbol,
        margin_mode: "ISOLATED",
      }),
    ).toBe(false);
    expect(
      matchesTPSLPosition(
        { ...leaf, margin_mode: "ISOLATED" },
        { symbol: leaf.symbol, marginMode: "ISOLATED" },
      ),
    ).toBe(true);
    expect(matchesTPSLPosition(leaf, { symbol: "PERP_BTC_USDC" })).toBe(false);
  });
  it("keeps direct Positional provenance below Bracket without mutating REST/WS inputs", () => {
    const raw = {
      algo_type: "BRACKET",
      margin_mode: "ISOLATED",
      child_orders: [
        {
          algo_type: "POSITIONAL_TP_SL",
          child_orders: [{ ...leaf, parent_algo_type: undefined }],
        },
      ],
    };
    const normalized = withTPSLProvenance(raw);
    const child = normalized.child_orders[0].child_orders[0];
    expect(child).toMatchObject({
      parent_algo_type: "POSITIONAL_TP_SL",
      margin_mode: "ISOLATED",
    });
    expect(isEntirePositionTPSL(child)).toBe(true);
    expect(
      raw.child_orders[0].child_orders[0].parent_algo_type,
    ).toBeUndefined();
  });
  it("separates estimate price from trigger price and preserves per-leg execution quantity", () => {
    expect(getTPSLEstimatePrice(leaf)).toBe(4110);
    expect(getTPSLEstimatePrice({ ...leaf, type: "CLOSE_POSITION" })).toBe(
      4100,
    );
    const parent = {
      algo_type: "POSITIONAL_TP_SL",
      quantity: 0,
      child_orders: [{ ...leaf, is_triggered: true, quantity: 2 }],
    };
    expect(getTPSLQuantity(getTPSLLeg(parent, "tp")!, 9)).toBe(2);
  });
});
