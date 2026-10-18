import test from "node:test";
import assert from "node:assert/strict";
import { planNativeOrder } from "../packages/paxeer-backend/src/nativeOrderPlan";
import {
  PERPS_MARKETS,
  COLLATERAL_TOKENS,
} from "../packages/paxeer-backend/src/core/perps/config";
const order = {
  symbol: "PERP_ETH_USDC",
  side: "BUY",
  order_type: "MARKET",
  order_quantity: "0.01000001",
  order_price: "",
  trigger_price: "",
};
test("market form converts exact USD collateral and 18 decimal contract leverage", () => {
  const plan = planNativeOrder(order, 3000, 3);
  assert.equal(plan.collateralAmount, 10_000_010n);
  assert.equal(plan.leverage, 3_000_000_000_000_000_000n);
  assert.equal(plan.collateralToken, COLLATERAL_TOKENS.usdc.address);
  assert.equal(plan.sizeUsd, 30_000_030_000_000_000_000n);
});
test("limit order uses entered price and rounds collateral down at token precision", () => {
  const plan = planNativeOrder(
    {
      ...order,
      order_type: "LIMIT",
      order_price: "2999.99",
      order_quantity: "0.1",
    },
    3000,
    10,
  );
  assert.equal(plan.collateralAmount, 29_999_900n);
  assert.equal(plan.triggerPrice, 2_999_990_000_000_000_000_000n);
});
test("native plans reject unsupported markets, sides, invalid size, missing prices and excessive leverage", () => {
  const max = PERPS_MARKETS.find((m) => m.symbol === "ETH")!.maxLeverage;
  for (const [input, price, lev] of [
    [{ ...order, symbol: "PERP_UNKNOWN_USDC" }, 3000, 2],
    [{ ...order, side: "BAD" }, 3000, 2],
    [{ ...order, order_quantity: "0" }, 3000, 2],
    [order, Number.NaN, 2],
    [order, 3000, max + 1],
  ] as const)
    assert.throws(() => planNativeOrder(input, price, lev));
});
test("short orders preserve direction and explicit stop/limit triggers", () => {
  const plan = planNativeOrder(
    {
      ...order,
      side: "SELL",
      order_type: "STOP_LIMIT",
      order_price: "2900",
      trigger_price: "2950",
    },
    3000,
    2,
  );
  assert.equal(plan.isLong, false);
  assert.equal(plan.triggerPrice, 2950n * 10n ** 18n);
  assert.equal(plan.limitPrice, 2900n * 10n ** 18n);
});

test("unsupported order modes fail before a transaction can be prepared", () => {
  assert.throws(() =>
    planNativeOrder({ ...order, order_type: "TRAILING_STOP" }, 3000, 2),
  );
  assert.throws(() =>
    planNativeOrder(
      { ...order, reduce_only: true, order_type: "LIMIT", order_price: "3000" },
      3000,
      2,
    ),
  );
});

test("execution flags and collateral cannot be silently changed by the native bridge", () => {
  for (const input of [
    { ...order, order_type_ext: "IOC" },
    { ...order, visible_quantity: 0 },
    { ...order, collateral_token: "USDT" },
  ])
    assert.throws(() => planNativeOrder(input, 3000, 2));
});
