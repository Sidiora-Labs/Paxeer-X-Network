import { afterEach, beforeEach, describe, it, mock } from "node:test";
import { strict as assert } from "node:assert";
import { encodeFunctionData, type Address } from "viem";

import {
  MAX_SCALED_ORDERS,
  QUOTE_MAX_AGE_MS,
  buildConfirmation,
  buildOrderPlan,
  needsConfirmation,
  needsRequote,
  parseLeverage,
  selectOrderOption,
  successReset,
  timeInForceOf,
  type OrderFormState,
  type OrderPlan,
} from "../src/core/perps/orderPlan";
import { ladderCloid } from "../src/core/perps/calls";
import { OrderType, TimeInForce } from "../src/core/network/abi/PerpsPrecompile";
import { useOrderLineStore } from "../src/hooks/perps/useOrderLineStore";
import type { OpenPositionQuote } from "../src/hooks/perps/useQuoter";

const PERPS: Address = "0x0000000000000000000000000000000000001015";
const E18 = 10n ** 18n;
const REFERENCE_SECONDS = 1_000_000;
const LADDER = 0xabcdefn;

function form(overrides: Partial<OrderFormState> = {}): OrderFormState {
  return {
    orderType: "market",
    marketId: 0n,
    isLong: true,
    collateralAmount: "100",
    leverage: "50",
    triggerPrice: "",
    limitPrice: "",
    sizeUsd: "",
    scaledStartPrice: "",
    scaledEndPrice: "",
    scaledTotalOrders: "5",
    scaledDistribution: "flat",
    tpslEnabled: false,
    tpPrice: "",
    slPrice: "",
    reduceOnly: false,
    postOnly: false,
    ioc: false,
    fok: false,
    ladderId: LADDER,
    ...overrides,
  };
}

function quote(overrides: Partial<OpenPositionQuote> = {}): OpenPositionQuote {
  return {
    entryPrice: 95_000n * E18,
    sizeUsd: 5_000n * E18,
    collateralUsd: 100n * E18,
    leverage: 50n * E18,
    tradingFee: 1_400_000n,
    tradingFeeUsd: 14n * 10n ** 17n,
    priceImpact: 3n,
    liquidationPrice: 90_500n * E18,
    estimatedFunding24h: 0n,
    maintenanceMarginBps: 5n,
    ...overrides,
  };
}

function only(plan: OrderPlan) {
  assert.equal(plan.error, null);
  assert.equal(plan.calls.length, 1);
  return plan.calls[0].args[0];
}

function refusedWith(plan: OrderPlan, code: string): void {
  assert.deepEqual(plan.calls, []);
  assert.equal(plan.error?.code, code);
}

const LIMIT = { orderType: "limit", triggerPrice: "3000", sizeUsd: "1000", leverage: "10" } as const;
const STOP_LIMIT = {
  orderType: "stop-limit",
  triggerPrice: "3000",
  limitPrice: "2990",
  sizeUsd: "1000",
  leverage: "10",
} as const;

beforeEach(() => {
  mock.timers.enable({ apis: ["Date"], now: REFERENCE_SECONDS * 1000 });
});

afterEach(() => {
  mock.timers.reset();
});

describe("time in force", () => {
  it("togglesMapToTif", () => {
    assert.equal(only(buildOrderPlan(form({ ...LIMIT, fok: true }), null)).timeInForce, TimeInForce.FOK);
    assert.equal(only(buildOrderPlan(form({ ...LIMIT, ioc: true }), null)).timeInForce, TimeInForce.IOC);
    assert.equal(only(buildOrderPlan(form({ ...LIMIT, postOnly: true }), null)).timeInForce, TimeInForce.POST_ONLY);
    assert.equal(only(buildOrderPlan(form(LIMIT), null)).timeInForce, TimeInForce.GTC);
    assert.equal(only(buildOrderPlan(form(STOP_LIMIT), null)).timeInForce, TimeInForce.GTC);
    assert.equal(only(buildOrderPlan(form(), quote())).timeInForce, TimeInForce.IOC);
    assert.equal(only(buildOrderPlan(form({ fok: true }), quote())).timeInForce, TimeInForce.FOK);

    const ioc = only(buildOrderPlan(form({ ...LIMIT, ioc: true }), null));
    assert.equal(ioc.deadline, BigInt(REFERENCE_SECONDS + 60));
    assert.equal(only(buildOrderPlan(form(LIMIT), null)).deadline, 0n);
  });

  it("postOnlyClearsIocFok", () => {
    const options = selectOrderOption({ postOnly: false, ioc: true, fok: false }, "postOnly", true);
    assert.deepEqual(options, { postOnly: true, ioc: false, fok: false });
    assert.equal(timeInForceOf("limit", options), TimeInForce.POST_ONLY);
    assert.equal(only(buildOrderPlan(form({ ...LIMIT, ...options }), null)).timeInForce, TimeInForce.POST_ONLY);

    assert.deepEqual(selectOrderOption({ postOnly: false, ioc: true, fok: false }, "fok", true), {
      postOnly: false,
      ioc: false,
      fok: true,
    });
    assert.deepEqual(selectOrderOption(options, "postOnly", false), { postOnly: false, ioc: false, fok: false });
    assert.equal(timeInForceOf("limit", { postOnly: true, ioc: true, fok: true }), TimeInForce.POST_ONLY);
  });

  it("marketNeverGtc", () => {
    const toggles = [false, true];
    for (const postOnly of toggles) {
      for (const ioc of toggles) {
        for (const fok of toggles) {
          for (const isLong of toggles) {
            const tif = only(buildOrderPlan(form({ postOnly, ioc, fok, isLong }), quote())).timeInForce;
            assert.notEqual(tif, TimeInForce.GTC);
            assert.notEqual(tif, TimeInForce.POST_ONLY);
            assert.equal(timeInForceOf("market", { postOnly, ioc, fok }), tif);
          }
        }
      }
    }
  });

  it("reduceOnlyIsSent", () => {
    const p = only(buildOrderPlan(form({ ...STOP_LIMIT, isLong: false, limitPrice: "3010", reduceOnly: true }), null));
    assert.equal(p.orderType, OrderType.STOP_LIMIT);
    assert.equal(p.isLong, false);
    assert.equal(p.reduceOnly, true);
    assert.equal(p.triggerPrice, 3_000n * E18);
    assert.equal(p.limitPrice, 3_010n * E18);
    assert.equal(only(buildOrderPlan(form({ ...LIMIT, reduceOnly: true }), null)).reduceOnly, true);
    assert.equal(only(buildOrderPlan(form(LIMIT), null)).reduceOnly, false);
  });
});

describe("take-profit and stop-loss", () => {
  it("marketCarriesTpSl", () => {
    const p = only(
      buildOrderPlan(form({ tpslEnabled: true, tpPrice: "100000", slPrice: "94000" }), quote()),
    );
    assert.equal(p.orderType, OrderType.MARKET);
    assert.equal(p.leverage, 50n * E18);
    assert.equal(p.takeProfitPrice, 100_000n * E18);
    assert.equal(p.stopLossPrice, 94_000n * E18);
    assert.equal(p.collateralAmount, 100_000_000n);

    const off = only(buildOrderPlan(form({ tpslEnabled: false, tpPrice: "100000", slPrice: "94000" }), quote()));
    assert.equal(off.takeProfitPrice, 0n);
    assert.equal(off.stopLossPrice, 0n);
  });

  it("limitCarriesTpOnly", () => {
    const p = only(buildOrderPlan(form({ ...LIMIT, tpslEnabled: true, tpPrice: "3300" }), null));
    assert.equal(p.orderType, OrderType.LIMIT);
    assert.equal(p.takeProfitPrice, 3_300n * E18);
    assert.equal(p.stopLossPrice, 0n);

    const s = only(buildOrderPlan(form({ ...STOP_LIMIT, tpslEnabled: true, tpPrice: "3300", slPrice: "2900" }), null));
    assert.equal(s.takeProfitPrice, 3_300n * E18);
    assert.equal(s.stopLossPrice, 2_900n * E18);
  });

  it("invertedTpSlRefused", () => {
    refusedWith(buildOrderPlan(form({ tpslEnabled: true, tpPrice: "94000", slPrice: "93000" }), quote()), "TRADE_TP_SL_INVALID");
    refusedWith(
      buildOrderPlan(form({ ...LIMIT, isLong: false, tpslEnabled: true, tpPrice: "3100" }), null),
      "TRADE_TP_SL_INVALID",
    );
    refusedWith(
      buildOrderPlan(form({ ...STOP_LIMIT, tpslEnabled: true, slPrice: "3050" }), null),
      "TRADE_TP_SL_INVALID",
    );
  });
});

describe("plan shape", () => {
  it("marketPlanIsOneCall", () => {
    const plan = buildOrderPlan(form(), quote());
    assert.equal(plan.error, null);
    assert.equal(plan.calls.length, 1);
    const [c] = plan.calls;
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "placeOrder");
    assert.deepEqual(c.args, [
      {
        marketId: 0n,
        isLong: true,
        orderType: OrderType.MARKET,
        timeInForce: TimeInForce.IOC,
        sizeUsd: 5_000n * E18,
        leverage: 50n * E18,
        limitPrice: 0n,
        triggerPrice: 0n,
        acceptablePrice: 95_475n * E18,
        collateralAmount: 100_000_000n,
        reduceOnly: false,
        takeProfitPrice: 0n,
        stopLossPrice: 0n,
        deadline: BigInt(REFERENCE_SECONDS + 60),
        cloid: "0x00000000000000000000000000000000",
      },
    ]);
    assert.match(encodeFunctionData(c), /^0x[0-9a-f]{8}/);

    refusedWith(buildOrderPlan(form(), null), "VALIDATION_REQUIRED_FIELD");
    refusedWith(buildOrderPlan(form({ collateralAmount: "" }), quote()), "VALIDATION_AMOUNT_INVALID");
  });

  it("limitAndStopLimitAreOneCall", () => {
    const l = only(buildOrderPlan(form(LIMIT), null));
    assert.equal(l.orderType, OrderType.LIMIT);
    assert.equal(l.limitPrice, 3_000n * E18);
    assert.equal(l.triggerPrice, 0n);
    assert.equal(l.sizeUsd, 1_000n * E18);
    assert.equal(l.collateralAmount, 100_000_000n);
    assert.equal(l.acceptablePrice, 0n);
    assert.equal(l.cloid, ladderCloid(LADDER, 0));

    const s = only(buildOrderPlan(form(STOP_LIMIT), null));
    assert.equal(s.orderType, OrderType.STOP_LIMIT);
    assert.equal(s.triggerPrice, 3_000n * E18);
    assert.equal(s.limitPrice, 2_990n * E18);
    assert.equal(s.cloid, ladderCloid(LADDER, 0));

    refusedWith(buildOrderPlan(form({ ...LIMIT, triggerPrice: "" }), null), "VALIDATION_AMOUNT_INVALID");
    refusedWith(buildOrderPlan(form({ ...LIMIT, sizeUsd: "" }), null), "VALIDATION_AMOUNT_INVALID");
  });

  it("subOneLeverageBlocksPlan", () => {
    assert.equal(parseLeverage("0.5"), 0n);
    assert.equal(parseLeverage("1"), E18);
    assert.equal(parseLeverage("2.5"), 25n * 10n ** 17n);
    refusedWith(buildOrderPlan(form({ leverage: "0.5" }), quote()), "VALIDATION_BELOW_MIN");
    refusedWith(buildOrderPlan(form({ ...LIMIT, leverage: "0.5" }), null), "VALIDATION_BELOW_MIN");
  });
});

describe("scaled ladder", () => {
  const SCALED = { orderType: "scaled", leverage: "10", scaledStartPrice: "3000", scaledEndPrice: "2900" } as const;

  it("scaledFlatLadder", () => {
    const plan = buildOrderPlan(form({ ...SCALED, sizeUsd: "5000", scaledTotalOrders: "5" }), null);
    assert.equal(plan.error, null);
    assert.equal(plan.calls.length, 5);
    const prices = [3_000n, 2_975n, 2_950n, 2_925n, 2_900n];
    plan.calls.forEach((c, i) => {
      const p = c.args[0];
      assert.equal(c.address, PERPS);
      assert.equal(c.functionName, "placeOrder");
      assert.equal(p.orderType, OrderType.LIMIT);
      assert.equal(p.timeInForce, TimeInForce.GTC);
      assert.equal(p.limitPrice, prices[i] * E18);
      assert.equal(p.sizeUsd, 1_000n * E18);
      assert.equal(p.collateralAmount, 100_000_000n);
      assert.equal(p.cloid, ladderCloid(LADDER, i));
      assert.equal(p.takeProfitPrice, 0n);
      assert.equal(p.stopLossPrice, 0n);
      assert.match(encodeFunctionData(c), /^0x[0-9a-f]{8}/);
    });
  });

  it("scaledAscendingRemainder", () => {
    const asc = buildOrderPlan(
      form({ ...SCALED, sizeUsd: "100", scaledTotalOrders: "3", scaledDistribution: "asc" }),
      null,
    );
    assert.equal(asc.error, null);
    assert.deepEqual(
      asc.calls.map((c) => c.args[0].sizeUsd),
      [16666666666666666666n, 33333333333333333333n, 50000000000000000001n],
    );
    assert.deepEqual(
      asc.calls.map((c) => c.args[0].limitPrice),
      [3_000n * E18, 2_950n * E18, 2_900n * E18],
    );

    const desc = buildOrderPlan(
      form({ ...SCALED, sizeUsd: "100", scaledTotalOrders: "3", scaledDistribution: "desc" }),
      null,
    );
    assert.deepEqual(
      desc.calls.map((c) => c.args[0].sizeUsd),
      [50000000000000000000n, 33333333333333333333n, 16666666666666666667n],
    );
  });

  it("scaledRungBounds", () => {
    const over = buildOrderPlan(form({ ...SCALED, sizeUsd: "5000", scaledTotalOrders: "21" }), null);
    refusedWith(over, "VALIDATION_ABOVE_MAX");
    assert.match(over.error?.message ?? "", /2 to 20/);

    const under = buildOrderPlan(form({ ...SCALED, sizeUsd: "5000", scaledTotalOrders: "1" }), null);
    refusedWith(under, "VALIDATION_BELOW_MIN");
    assert.match(under.error?.message ?? "", /2 to 20/);

    refusedWith(buildOrderPlan(form({ ...SCALED, sizeUsd: "5000", scaledTotalOrders: "2.5" }), null), "VALIDATION_AMOUNT_INVALID");

    const max = buildOrderPlan(form({ ...SCALED, sizeUsd: "5000", scaledTotalOrders: String(MAX_SCALED_ORDERS) }), null);
    assert.equal(max.calls.length, MAX_SCALED_ORDERS);
    const two = buildOrderPlan(form({ ...SCALED, sizeUsd: "5000", scaledTotalOrders: "2" }), null);
    assert.deepEqual(
      two.calls.map((c) => c.args[0].limitPrice),
      [3_000n * E18, 2_900n * E18],
    );

    refusedWith(
      buildOrderPlan(form({ ...SCALED, sizeUsd: "5000", scaledDistribution: "custom" }), null),
      "VALIDATION_REQUIRED_FIELD",
    );
  });
});

describe("quote and confirmation", () => {
  it("agedQuoteIsRefreshed", () => {
    assert.equal(QUOTE_MAX_AGE_MS, 6_000);
    assert.equal(needsRequote(7_000), true);
    assert.equal(needsRequote(QUOTE_MAX_AGE_MS), false);
    assert.equal(needsRequote(0), false);
    assert.equal(needsRequote(-1), true);
    assert.equal(needsRequote(Number.NaN), true);

    const stale = only(buildOrderPlan(form(), quote()));
    const fresh = only(buildOrderPlan(form(), quote({ entryPrice: 96_000n * E18 })));
    assert.equal(stale.acceptablePrice, 95_475n * E18);
    assert.equal(fresh.acceptablePrice, 96_480n * E18);
  });

  it("confirmationPrecedesSigning", () => {
    assert.equal(needsConfirmation(true), true);
    assert.equal(needsConfirmation(false), false);
    assert.equal(buildConfirmation(quote(), buildOrderPlan(form({ leverage: "0.5" }), quote())), null);

    const limit = buildConfirmation(null, buildOrderPlan(form({ ...LIMIT, isLong: false }), null));
    assert.deepEqual(limit, {
      side: "short",
      sizeUsd: 1_000n * E18,
      acceptablePrice: 3_000n * E18,
      tradingFeeUsd: null,
      liquidationPrice: null,
      display: { side: "Short", size: "1000.00 USD", acceptablePrice: "3000", fee: null, liquidationPrice: null },
    });
  });

  it("confirmationShowsQuotedFee", () => {
    const q = quote();
    const confirmation = buildConfirmation(q, buildOrderPlan(form(), q));
    assert.deepEqual(confirmation, {
      side: "long",
      sizeUsd: 5_000n * E18,
      acceptablePrice: 95_475n * E18,
      tradingFeeUsd: 14n * 10n ** 17n,
      liquidationPrice: 90_500n * E18,
      display: {
        side: "Long",
        size: "5000.00 USD",
        acceptablePrice: "95475",
        fee: "1.40 USD",
        liquidationPrice: "90500",
      },
    });
  });
});

describe("after submit", () => {
  it("successClearsLines", () => {
    const store = useOrderLineStore;
    store.getState().setTpslEnabled(true);
    store.getState().setEntryPrice(3_000);
    store.getState().setTpPrice(3_300);
    store.getState().setSlPrice(2_900);

    assert.equal(successReset("reverted"), null);
    const reset = successReset("success");
    assert.ok(reset);
    assert.deepEqual(reset.inputs, {
      collateralAmount: "",
      triggerPrice: "",
      limitPrice: "",
      sizeUsd: "",
      tpPrice: "",
      slPrice: "",
      scaledStartPrice: "",
      scaledEndPrice: "",
    });
    store.setState(reset.lines);
    const lines = store.getState();
    assert.equal(lines.entryPrice, 0);
    assert.equal(lines.tpPrice, 0);
    assert.equal(lines.slPrice, 0);
  });
});
