import { afterEach, beforeEach, describe, it, mock } from "node:test";
import { strict as assert } from "node:assert";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { encodeFunctionData, type Address, type Hex } from "viem";

import * as calls from "../src/core/perps/calls";
import {
  acceptablePrice,
  buildAddCollateral,
  buildAddSize,
  buildCancelByCloid,
  buildCancelOrder,
  buildClosePosition,
  buildDeposit,
  buildDepositPlan,
  buildGetAccount,
  buildGetAccountSummary,
  buildGetBalance,
  buildGetLockedBalance,
  buildGetOrder,
  buildGetPosition,
  buildGetPositionDetails,
  buildGetPositionMargin,
  buildGetUserOrderIds,
  buildGetUserPositionIds,
  buildOpenPosition,
  buildPartialClose,
  buildPlaceLimitOrder,
  buildPlaceStopLimitOrder,
  buildRemoveCollateral,
  buildSetPositionTpSl,
  buildTopUpPosition,
  buildWithdraw,
  buildWithdrawAll,
  ladderCloid,
  type AccountState,
} from "../src/core/perps/calls";
import { deriveImpliedCollateral, safeParseUnits } from "../src/core/perps/amounts";
import { PerpsPrecompileABI, OrderType, TimeInForce } from "../src/core/network/abi/PerpsPrecompile";
import { buildDeadline, parsePrice } from "../src/core/spot/slippage";

const ROOT = path.resolve(__dirname, "..");
const PERPS: Address = "0x0000000000000000000000000000000000001015";
const USDL: Address = "0x85FcD13735F4309833A503EE804ea32395851479";
const ACCOUNT: Address = "0x00000000000000000000000000000000000000a1";
const NATIVE = new Set<Address>([
  "0xAa00000000000000000000000000000000000001",
  "0xbB00000000000000000000000000000000000002",
]);
const E18 = 10n ** 18n;
const REFERENCE_SECONDS = 1_000_000;
const NO_CLOID: Hex = "0x00000000000000000000000000000000";

function refusedWith(code: string): (err: unknown) => boolean {
  return (err) => {
    assert.equal((err as { code?: unknown }).code, code);
    return true;
  };
}

function encodes(c: { abi: typeof PerpsPrecompileABI; functionName: string; args: readonly unknown[] }): void {
  const data = encodeFunctionData(c as Parameters<typeof encodeFunctionData>[0]);
  assert.match(data, /^0x[0-9a-f]{8}/);
}

beforeEach(() => {
  mock.timers.enable({ apis: ["Date"], now: REFERENCE_SECONDS * 1000 });
});

afterEach(() => {
  mock.timers.reset();
});

describe("position writes", () => {
  it("openPositionCarriesBoundAndTpSl", () => {
    const c = buildOpenPosition({
      marketId: 0n,
      isLong: true,
      collateralAmount: safeParseUnits("100", 6),
      leverage: 20n * E18,
      quote: { entryPrice: 95_000n * E18, sizeUsd: 2_000n * E18 },
      takeProfitPrice: 100_000n * E18,
      stopLossPrice: 94_000n * E18,
    });
    assert.equal(c.address, PERPS);
    assert.equal(c.abi, PerpsPrecompileABI);
    assert.equal(c.functionName, "placeOrder");
    assert.deepEqual(c.args, [
      {
        marketId: 0n,
        isLong: true,
        orderType: OrderType.MARKET,
        timeInForce: TimeInForce.IOC,
        sizeUsd: 2_000n * E18,
        leverage: 20n * E18,
        limitPrice: 0n,
        triggerPrice: 0n,
        acceptablePrice: 95_475n * E18,
        collateralAmount: 100_000_000n,
        reduceOnly: false,
        takeProfitPrice: 100_000n * E18,
        stopLossPrice: 94_000n * E18,
        deadline: 1_000_060n,
        cloid: NO_CLOID,
      },
    ]);
    encodes(c);

    const fok = buildOpenPosition({
      marketId: 0n,
      isLong: false,
      collateralAmount: 100_000_000n,
      leverage: 20n * E18,
      quote: { entryPrice: 95_000n * E18, sizeUsd: 2_000n * E18 },
      timeInForce: TimeInForce.FOK,
    });
    assert.equal(fok.args[0].timeInForce, TimeInForce.FOK);
    assert.equal(fok.args[0].acceptablePrice, 94_525n * E18);
    assert.equal(fok.args[0].takeProfitPrice, 0n);
    assert.equal(fok.args[0].stopLossPrice, 0n);
  });

  it("addSizeCarriesBound", () => {
    const c = buildAddSize({
      position: { marketId: 0n, isLong: true },
      additionalCollateral: safeParseUnits("50", 6),
      leverage: safeParseUnits("10", 18),
      quote: { entryPrice: 62_000n * E18, sizeUsd: 497n * E18 },
    });
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "placeOrder");
    const p = c.args[0];
    assert.equal(p.orderType, OrderType.MARKET);
    assert.equal(p.timeInForce, TimeInForce.IOC);
    assert.equal(p.marketId, 0n);
    assert.equal(p.isLong, true);
    assert.equal(p.collateralAmount, 50_000_000n);
    assert.equal(p.leverage, 10_000_000_000_000_000_000n);
    assert.equal(p.acceptablePrice, 62_310n * E18);
    assert.equal(p.deadline, BigInt(REFERENCE_SECONDS + 60));
    assert.equal(p.reduceOnly, false);
    encodes(c);
  });

  it("closeLongBoundRoundsDown", () => {
    const c = buildClosePosition({ positionId: 7n, isLong: true, sizeUsd: 2_000n * E18, exitPrice: 96_000n * E18 });
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "closePositionWithParams");
    assert.deepEqual(c.args, [7n, 2_000n * E18, 95_520n * E18, 1_000_060n]);
    encodes(c);
  });

  it("partialCloseShortBoundRoundsUp", () => {
    const c = buildPartialClose({ positionId: 7n, isLong: false, closeSizeUsd: 1_000n * E18, exitPrice: 90_000n * E18 });
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "closePositionWithParams");
    assert.deepEqual(c.args, [7n, 1_000n * E18, 90_450n * E18, 1_000_060n]);
    encodes(c);
  });

  it("collateralCallsUseSixDecimals", () => {
    const amount = safeParseUnits("25.5", 6);
    const add = buildAddCollateral(7n, amount);
    const remove = buildRemoveCollateral(7n, amount);
    assert.equal(add.address, PERPS);
    assert.equal(add.functionName, "addCollateral");
    assert.deepEqual(add.args, [7n, 25_500_000n]);
    assert.equal(remove.address, PERPS);
    assert.equal(remove.functionName, "removeCollateral");
    assert.deepEqual(remove.args, [7n, 25_500_000n]);
    encodes(add);
    encodes(remove);
  });

  it("positionReadsTargetPerps", () => {
    const ids = buildGetUserPositionIds(ACCOUNT);
    assert.equal(ids.address, PERPS);
    assert.equal(ids.functionName, "getUserPositionIds");
    assert.deepEqual(ids.args, [ACCOUNT]);
    for (const [c, name] of [
      [buildGetPosition(7n), "getPosition"],
      [buildGetPositionDetails(7n), "getPositionDetails"],
      [buildGetPositionMargin(7n), "getPositionMargin"],
    ] as const) {
      assert.equal(c.address, PERPS);
      assert.equal(c.functionName, name);
      assert.deepEqual(c.args, [7n]);
      encodes(c);
    }
    encodes(ids);
  });
});

describe("orders", () => {
  it("limitOrderCarriesTif", () => {
    const sizeUsd = 6_000n * E18;
    const leverage = 10n * E18;
    const c = buildPlaceLimitOrder({
      marketId: 1n,
      isLong: true,
      limitPrice: 3_000n * E18,
      sizeUsd,
      leverage,
      collateralAmount: deriveImpliedCollateral(sizeUsd, leverage, 6),
      timeInForce: TimeInForce.IOC,
      reduceOnly: false,
      cloid: ladderCloid(0x0an, 0),
    });
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "placeOrder");
    const p = c.args[0];
    assert.equal(p.orderType, OrderType.LIMIT);
    assert.equal(p.collateralAmount, 600_000_000n);
    assert.equal(p.timeInForce, TimeInForce.IOC);
    assert.equal(p.limitPrice, 3_000n * E18);
    assert.equal(p.reduceOnly, false);
    assert.equal(p.deadline, 1_000_060n);
    assert.match(p.cloid, /^0x[0-9a-f]{32}$/);
    assert.notEqual(BigInt(p.cloid), 0n);
    encodes(c);

    const gtc = buildPlaceLimitOrder({
      marketId: 1n,
      isLong: true,
      limitPrice: 3_000n * E18,
      sizeUsd,
      leverage,
      collateralAmount: 600_000_000n,
      timeInForce: TimeInForce.GTC,
      reduceOnly: true,
      takeProfitPrice: 3_300n * E18,
      stopLossPrice: 2_800n * E18,
      cloid: ladderCloid(0x0an, 1),
    });
    assert.equal(gtc.args[0].deadline, 0n);
    assert.equal(gtc.args[0].reduceOnly, true);
    assert.equal(gtc.args[0].takeProfitPrice, 3_300n * E18);
    assert.equal(gtc.args[0].stopLossPrice, 2_800n * E18);
  });

  it("stopLimitCarriesLimitPrice", () => {
    const c = buildPlaceStopLimitOrder({
      marketId: 1n,
      isLong: false,
      triggerPrice: 2_900n * E18,
      limitPrice: 2_890n * E18,
      sizeUsd: 6_000n * E18,
      leverage: 10n * E18,
      collateralAmount: 600_000_000n,
      timeInForce: TimeInForce.GTC,
      reduceOnly: false,
      cloid: ladderCloid(0x0bn, 0),
    });
    assert.equal(c.functionName, "placeOrder");
    const p = c.args[0];
    assert.equal(p.orderType, OrderType.STOP_LIMIT);
    assert.equal(p.triggerPrice, 2_900n * E18);
    assert.equal(p.limitPrice, 2_890n * E18);
    assert.equal(p.timeInForce, TimeInForce.GTC);
    assert.equal(p.isLong, false);
    assert.equal(p.deadline, 0n);
    encodes(c);
  });

  it("positionTpSlOrders", () => {
    const c = buildSetPositionTpSl({
      positionId: 7n,
      isLong: true,
      takeProfitPrice: parsePrice("100000"),
      stopLossPrice: parsePrice("94000"),
    });
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "setPositionTpSl");
    assert.deepEqual(c.args, [7n, 100000000000000000000000n, 94000000000000000000000n]);
    encodes(c);
    const cleared = buildSetPositionTpSl({
      positionId: 7n,
      isLong: true,
      takeProfitPrice: parsePrice("100000"),
      stopLossPrice: 0n,
    });
    assert.equal(cleared.args[2], 0n);
  });

  it("cancelTargetsPerps", () => {
    const cancel = buildCancelOrder(12n);
    assert.equal(cancel.address, PERPS);
    assert.equal(cancel.functionName, "cancelOrder");
    assert.deepEqual(cancel.args, [12n]);
    const cloid = ladderCloid(0x0an, 3);
    assert.equal(cloid, "0x00000000000000000000000a00000003");
    const byCloid = buildCancelByCloid(1n, cloid);
    assert.equal(byCloid.address, PERPS);
    assert.equal(byCloid.functionName, "cancelByCloid");
    assert.deepEqual(byCloid.args, [1n, "0x00000000000000000000000a00000003"]);
    encodes(cancel);
    encodes(byCloid);
    assert.throws(() => buildCancelByCloid(1n, NO_CLOID), RangeError);
    assert.throws(() => ladderCloid(0n, 3), RangeError);
    assert.throws(() => ladderCloid(1n, -1), RangeError);
  });

  it("orderReadsTargetPerps", () => {
    const order = buildGetOrder(12n);
    const ids = buildGetUserOrderIds(ACCOUNT);
    assert.equal(order.address, PERPS);
    assert.equal(order.functionName, "getOrder");
    assert.deepEqual(order.args, [12n]);
    assert.equal(ids.address, PERPS);
    assert.equal(ids.functionName, "getUserOrderIds");
    assert.deepEqual(ids.args, [ACCOUNT]);
    encodes(order);
    encodes(ids);
  });
});

describe("account", () => {
  it("depositTargetsPerpsAccount", async () => {
    const plan = await buildDepositPlan(safeParseUnits("100", 6), async () => 0n, NATIVE);
    assert.equal(plan.length, 2);
    assert.deepEqual(plan[0], { address: USDL, functionName: "approve", args: [PERPS, 100_000_000n] });
    const deposit = plan[1];
    assert.ok("abi" in deposit);
    assert.equal(deposit.address, PERPS);
    assert.equal(deposit.functionName, "deposit");
    assert.deepEqual(deposit.args, [USDL, 100_000_000n]);
    assert.deepEqual(
      plan.map((c) => c.functionName),
      ["approve", "deposit"],
    );
    const covered = await buildDepositPlan(100_000_000n, async () => 100_000_000n, NATIVE);
    assert.deepEqual(covered, [buildDeposit(100_000_000n)]);
  });

  it("topUpUsesSixDecimals", () => {
    const c = buildTopUpPosition(7n, safeParseUnits("15", 6));
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "topUpPosition");
    assert.deepEqual(c.args, [7n, 15_000_000n]);
    encodes(c);
    assert.deepEqual(
      Object.keys(calls).filter((name) => /transfermargin/i.test(name)),
      [],
    );
  });

  it("accountReadsTargetPerps", () => {
    const summary = buildGetAccountSummary(ACCOUNT, USDL);
    assert.equal(summary.address, PERPS);
    assert.equal(summary.functionName, "getAccountSummary");
    assert.deepEqual(summary.args, [ACCOUNT, USDL]);
    for (const [c, name] of [
      [buildGetBalance(ACCOUNT, USDL), "getBalance"],
      [buildGetLockedBalance(ACCOUNT, USDL), "getLockedBalance"],
    ] as const) {
      assert.equal(c.address, PERPS);
      assert.equal(c.functionName, name);
      assert.deepEqual(c.args, [ACCOUNT, USDL]);
      encodes(c);
    }
    const account = buildGetAccount(ACCOUNT);
    assert.equal(account.address, PERPS);
    assert.equal(account.functionName, "getAccount");
    assert.deepEqual(account.args, [ACCOUNT]);
    encodes(summary);
    encodes(account);
    const zero: Address = "0x0000000000000000000000000000000000000000";
    assert.throws(() => buildGetAccount(zero), refusedWith("WALLET_NOT_CONNECTED"));
    assert.throws(() => buildGetUserPositionIds(zero), refusedWith("WALLET_NOT_CONNECTED"));
  });

  it("withdrawAllUsesAvailable", () => {
    const state: AccountState = {
      collateral: 100_000_000n,
      equityUsd: 100n * E18,
      initialMarginUsd: 60n * E18,
      maintenanceMarginUsd: 30n * E18,
      withdrawable: 40_000_000n,
      feeTier: 0,
      volume14dUsd: 0n,
      positionCount: 1n,
      openOrderCount: 0n,
    };
    const c = buildWithdrawAll(state);
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "withdraw");
    assert.deepEqual(c.args, [USDL, 40_000_000n]);
    assert.deepEqual(c, buildWithdraw(40_000_000n));
    encodes(c);
    assert.deepEqual(
      Object.keys(calls).filter((name) => /emergency/i.test(name)),
      [],
    );
    assert.throws(() => buildWithdrawAll({ withdrawable: 0n }), refusedWith("VALIDATION_AMOUNT_INVALID"));
  });
});

describe("price bound and deadline", () => {
  it("openShortBoundRoundsDown", () => {
    assert.equal(acceptablePrice(3_000n * E18, false, "open"), 2_985n * E18);
  });

  it("closeShortBoundRoundsUp", () => {
    assert.equal(acceptablePrice(3_000n * E18, false, "close"), 3_015n * E18);
    assert.equal(acceptablePrice(3_000n * E18, true, "close"), 2_985n * E18);
    assert.equal(acceptablePrice(3_000n * E18, true, "open"), 3_015n * E18);
  });

  it("deadlineIsSixtySeconds", () => {
    assert.equal(buildDeadline(), 1_000_060n);
  });

  it("boundIsIntegerOnly", () => {
    const entry = E18 + 1n;
    const bound = acceptablePrice(entry, true, "open");
    assert.equal(typeof bound, "bigint");
    assert.equal(bound, ((E18 + 1n) * 10_050n) / 10_000n);
    assert.equal(bound, 1_005_000_000_000_000_001n);
    assert.equal(acceptablePrice(entry, true, "open", 0), entry);
    assert.throws(() => acceptablePrice(entry, true, "open", 10_000), RangeError);
    assert.throws(() => acceptablePrice(entry, true, "open", 0.5), RangeError);
  });
});

describe("refusals", () => {
  const open = {
    marketId: 0n,
    isLong: true,
    collateralAmount: 100_000_000n,
    leverage: 20n * E18,
    quote: { entryPrice: 95_000n * E18, sizeUsd: 2_000n * E18 },
  };

  it("zeroAmountIsRefused", () => {
    const invalid = refusedWith("VALIDATION_AMOUNT_INVALID");
    assert.throws(() => buildOpenPosition({ ...open, collateralAmount: 0n }), invalid);
    assert.throws(() => buildAddCollateral(7n, 0n), invalid);
    assert.throws(() => buildRemoveCollateral(7n, 0n), invalid);
    assert.throws(() => buildTopUpPosition(7n, 0n), invalid);
    assert.throws(() => buildDeposit(0n), invalid);
    assert.throws(() => buildWithdraw(0n), invalid);
    assert.throws(() => buildPartialClose({ positionId: 7n, isLong: true, closeSizeUsd: 0n, exitPrice: E18 }), invalid);
    assert.throws(() => buildClosePosition({ positionId: 7n, isLong: true, sizeUsd: 2_000n * E18, exitPrice: 0n }), invalid);
    let reads = 0;
    return assert.rejects(
      buildDepositPlan(0n, async () => {
        reads += 1;
        return 0n;
      }, NATIVE),
      (err) => invalid(err) && reads === 0,
    );
  });

  it("subOneXLeverageIsRefused", () => {
    const below = refusedWith("VALIDATION_BELOW_MIN");
    assert.throws(() => buildOpenPosition({ ...open, leverage: E18 - 1n }), below);
    assert.throws(
      () =>
        buildAddSize({
          position: { marketId: 0n, isLong: true },
          additionalCollateral: 1n,
          leverage: 0n,
          quote: open.quote,
        }),
      below,
    );
    assert.equal(buildOpenPosition({ ...open, leverage: E18 }).args[0].leverage, E18);
  });

  it("invertedTpSlIsRefused", () => {
    const tpsl = refusedWith("TRADE_TP_SL_INVALID");
    assert.throws(() => buildOpenPosition({ ...open, takeProfitPrice: 94_000n * E18 }), tpsl);
    assert.throws(() => buildOpenPosition({ ...open, stopLossPrice: 96_000n * E18 }), tpsl);
    assert.throws(
      () => buildOpenPosition({ ...open, isLong: false, takeProfitPrice: 100_000n * E18, stopLossPrice: 94_000n * E18 }),
      tpsl,
    );
    assert.equal(
      buildOpenPosition({ ...open, isLong: false, takeProfitPrice: 90_000n * E18, stopLossPrice: 99_000n * E18 }).args[0]
        .takeProfitPrice,
      90_000n * E18,
    );
    assert.throws(
      () => buildSetPositionTpSl({ positionId: 7n, isLong: true, takeProfitPrice: 94_000n * E18, stopLossPrice: 100_000n * E18 }),
      tpsl,
    );
    assert.throws(
      () => buildSetPositionTpSl({ positionId: 7n, isLong: false, takeProfitPrice: 100_000n * E18, stopLossPrice: 94_000n * E18 }),
      tpsl,
    );
    assert.throws(
      () =>
        buildPlaceLimitOrder({
          marketId: 1n,
          isLong: true,
          limitPrice: 3_000n * E18,
          sizeUsd: 6_000n * E18,
          leverage: 10n * E18,
          collateralAmount: 600_000_000n,
          timeInForce: TimeInForce.GTC,
          reduceOnly: false,
          takeProfitPrice: 2_900n * E18,
          cloid: ladderCloid(1n, 0),
        }),
      tpsl,
    );
  });
});

describe("typing", () => {
  it("buildersAreTyped", { timeout: 300_000 }, () => {
    const dir = mkdtempSync(path.join(os.tmpdir(), "perps-calls-typed-"));
    try {
      const probe = [
        `import { PerpsPrecompileABI } from ${JSON.stringify(path.join(ROOT, "src/core/network/abi/PerpsPrecompile"))};`,
        `import type { PerpsCall } from ${JSON.stringify(path.join(ROOT, "src/core/perps/calls"))};`,
        `export const good: PerpsCall<"closePositionWithParams"> = { address: "${PERPS}", abi: PerpsPrecompileABI, functionName: "closePositionWithParams", args: [7n, 1n, 2n, 3n] };`,
        `export const bad: PerpsCall<"closePositionWithParams"> = { address: "${PERPS}", abi: PerpsPrecompileABI, functionName: "closePositionWithParams", args: [7n, 1n, 2n] };`,
        "",
      ].join("\n");
      writeFileSync(path.join(dir, "probe.ts"), probe);
      writeFileSync(
        path.join(dir, "tsconfig.json"),
        JSON.stringify({
          extends: path.join(ROOT, "tsconfig.json"),
          compilerOptions: { incremental: false, noEmit: true, typeRoots: [path.join(ROOT, "node_modules/@types")] },
          include: [],
          files: ["probe.ts"],
        }),
      );
      const result = spawnSync(path.join(ROOT, "node_modules/.bin/tsc"), ["-p", dir, "--pretty", "false"], {
        cwd: ROOT,
        encoding: "utf8",
      });
      assert.notEqual(result.status, 0);
      const errors = result.stdout.split("\n").filter((line) => line.includes("error TS"));
      assert.equal(errors.length, 1, result.stdout);
      assert.match(errors[0], /probe\.ts\(4,/);
      assert.match(result.stdout, /Source has 3 element\(s\) but target requires 4/);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
