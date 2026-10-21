import { afterEach, beforeEach, describe, it, mock } from "node:test";
import { strict as assert } from "node:assert";
import type { Address } from "viem";

import {
  planAddSize,
  planClose,
  planCollateral,
  planOpen,
  planOrder,
  planPartialClose,
  positionFromTuple,
  type AllowanceSource,
  type PositionTuple,
} from "../src/core/perps/positionWrites";
import {
  buildAddCollateral,
  buildAddSize,
  buildClosePosition,
  buildOpenPosition,
  buildPartialClose,
  buildPlaceLimitOrder,
  buildRemoveCollateral,
  ladderCloid,
  type AddSizeInput,
  type LimitOrderInput,
  type OpenPositionInput,
} from "../src/core/perps/calls";
import { OrderType, TimeInForce } from "../src/core/network/abi/PerpsPrecompile";

const PERPS: Address = "0x0000000000000000000000000000000000001015";
const USDL: Address = "0x85FcD13735F4309833A503EE804ea32395851479";
const ALICE: Address = "0x00000000000000000000000000000000000A11CE";
const E18 = 10n ** 18n;
const REFERENCE_SECONDS = 1_000_000;
const NO_NATIVE = new Set<Address>();

/** An allowance source that counts its reads. */
function source(allowance: bigint) {
  const reads = { count: 0 };
  const src: AllowanceSource = {
    allowance: async () => {
      reads.count += 1;
      return allowance;
    },
    native: NO_NATIVE,
  };
  return { src, reads };
}

const OPEN: OpenPositionInput = {
  marketId: 0n,
  isLong: true,
  collateralAmount: 100_000_000n,
  leverage: 10n * E18,
  quote: { entryPrice: 60_000n * E18, sizeUsd: 1_000n * E18 },
  takeProfitPrice: 66_000n * E18,
  stopLossPrice: 57_000n * E18,
};

const ADD_SIZE: AddSizeInput = {
  position: { marketId: 1n, isLong: false },
  additionalCollateral: 25_000_000n,
  leverage: 20n * E18,
  quote: { entryPrice: 3_000n * E18, sizeUsd: 500n * E18 },
};

function limit(overrides: Partial<LimitOrderInput> = {}): LimitOrderInput {
  return {
    marketId: 0n,
    isLong: true,
    limitPrice: 59_000n * E18,
    sizeUsd: 6_000n * E18,
    leverage: 20n * E18,
    collateralAmount: 300_000_000n,
    timeInForce: TimeInForce.GTC,
    reduceOnly: false,
    cloid: ladderCloid(7n, 0),
    ...overrides,
  };
}

function approve(amount: bigint) {
  return { address: USDL, functionName: "approve", args: [PERPS, amount] };
}

beforeEach(() => {
  mock.method(Date, "now", () => REFERENCE_SECONDS * 1000);
});

afterEach(() => {
  mock.restoreAll();
});

describe("position write planners", () => {
  it("planOpenApprovesExactCollateralThenPlacesMarketOrder", async () => {
    const { src, reads } = source(99_999_999n);
    const plan = await planOpen(OPEN, src);
    assert.equal(reads.count, 1);
    assert.equal(plan.length, 2);
    assert.deepEqual(plan[0], approve(100_000_000n));
    assert.deepEqual(plan[1], buildOpenPosition(OPEN));
    const step = plan[1];
    assert.ok(step.functionName === "placeOrder");
    assert.equal(step.address, PERPS);
    const p = step.args[0];
    assert.equal(p.orderType, OrderType.MARKET);
    assert.equal(p.timeInForce, TimeInForce.IOC);
    assert.equal(p.collateralAmount, 100_000_000n);
    assert.equal(p.acceptablePrice, 60_300n * E18);
    assert.equal(p.deadline, BigInt(REFERENCE_SECONDS + 60));
    assert.equal(p.takeProfitPrice, 66_000n * E18);
    assert.equal(p.stopLossPrice, 57_000n * E18);
  });

  it("planOpenWithSufficientAllowanceIsTheCallAlone", async () => {
    const { src } = source(100_000_000n);
    const plan = await planOpen(OPEN, src);
    assert.deepEqual(plan, [buildOpenPosition(OPEN)]);
  });

  it("planAddSizeApprovesTheAddedCollateral", async () => {
    const short = await planAddSize(ADD_SIZE, source(0n).src);
    assert.deepEqual(short, [approve(25_000_000n), buildAddSize(ADD_SIZE)]);
    const p = buildAddSize(ADD_SIZE).args[0];
    assert.equal(p.marketId, 1n);
    assert.equal(p.isLong, false);
    assert.equal(p.orderType, OrderType.MARKET);
    assert.equal(p.timeInForce, TimeInForce.IOC);
    assert.equal(p.acceptablePrice, 2_985n * E18);
    const enough = await planAddSize(ADD_SIZE, source(25_000_000n).src);
    assert.deepEqual(enough, [buildAddSize(ADD_SIZE)]);
  });

  it("planCloseSendsClosePositionWithParamsAndNeverApproves", () => {
    const input = { positionId: 7n, isLong: true, sizeUsd: 1_000n * E18, exitPrice: 61_000n * E18 };
    const plan = planClose(input);
    assert.deepEqual(plan, [buildClosePosition(input)]);
    assert.equal(plan[0].address, PERPS);
    assert.equal(plan[0].functionName, "closePositionWithParams");
    assert.deepEqual(plan[0].args, [7n, 1_000n * E18, 60_695n * E18, BigInt(REFERENCE_SECONDS + 60)]);
  });

  it("planPartialCloseBoundsTheChosenSize", () => {
    const input = { positionId: 9n, isLong: false, closeSizeUsd: 250n * E18, exitPrice: 3_000n * E18 };
    const plan = planPartialClose(input);
    assert.deepEqual(plan, [buildPartialClose(input)]);
    assert.deepEqual(plan[0].args, [9n, 250n * E18, 3_015n * E18, BigInt(REFERENCE_SECONDS + 60)]);
  });

  it("planCollateralApprovesAddAndNeverRemove", async () => {
    const add = await planCollateral({ action: "add", positionId: 7n, amount: 20_000_000n }, source(5n).src);
    assert.deepEqual(add, [approve(20_000_000n), buildAddCollateral(7n, 20_000_000n)]);
    const enough = await planCollateral({ action: "add", positionId: 7n, amount: 20_000_000n }, source(20_000_000n).src);
    assert.deepEqual(enough, [buildAddCollateral(7n, 20_000_000n)]);
    const { src, reads } = source(0n);
    const remove = await planCollateral({ action: "remove", positionId: 7n, amount: 20_000_000n }, src);
    assert.deepEqual(remove, [buildRemoveCollateral(7n, 20_000_000n)]);
    assert.equal(reads.count, 0);
  });

  it("planOrderApprovesTheSummedLadderCollateralOnce", async () => {
    const rungs = [
      buildPlaceLimitOrder(limit({ collateralAmount: 100_000_000n, cloid: ladderCloid(7n, 0) })),
      buildPlaceLimitOrder(limit({ collateralAmount: 150_000_000n, cloid: ladderCloid(7n, 1) })),
      buildPlaceLimitOrder(limit({ collateralAmount: 50_000_001n, cloid: ladderCloid(7n, 2) })),
    ];
    const { src, reads } = source(300_000_000n);
    const plan = await planOrder(rungs, src);
    assert.equal(reads.count, 1);
    assert.deepEqual(plan, [approve(300_000_001n), ...rungs]);
    const enough = await planOrder(rungs, source(300_000_001n).src);
    assert.deepEqual(enough, rungs);
  });

  it("noApprovalExceedsTheCallAmount", async () => {
    for (const plan of [
      await planOpen(OPEN, source(0n).src),
      await planAddSize(ADD_SIZE, source(0n).src),
      await planCollateral({ action: "add", positionId: 1n, amount: 1n }, source(0n).src),
    ]) {
      const [first, second] = plan;
      assert.ok(first.functionName === "approve");
      assert.equal(first.args[0], PERPS);
      assert.ok(second.functionName !== "approve");
      const pulled = second.functionName === "placeOrder" ? second.args[0].collateralAmount : second.args[1];
      assert.equal(first.args[1], pulled);
    }
  });

  it("refusesBeforeReadingTheAllowance", async () => {
    const { src, reads } = source(0n);
    await assert.rejects(
      () => planOpen({ ...OPEN, collateralAmount: 0n }, src),
      (err: { code?: string }) => err.code === "VALIDATION_AMOUNT_INVALID",
    );
    await assert.rejects(
      () => planOpen({ ...OPEN, takeProfitPrice: 59_000n * E18 }, src),
      (err: { code?: string }) => err.code === "TRADE_TP_SL_INVALID",
    );
    assert.equal(reads.count, 0);
  });
});

describe("getPosition by name", () => {
  it("readsCollateralTokenByOutputName", () => {
    const tuple: PositionTuple = [
      ALICE,
      3n,
      true,
      1_000n * E18,
      100n * E18,
      USDL,
      100_000_000n,
      60_000n * E18,
      1_700_000_000n,
      true,
    ];
    const position = positionFromTuple(tuple);
    assert.equal(position.collateralToken, USDL);
    assert.equal(position.user, ALICE);
    assert.equal(position.marketId, 3n);
    assert.equal(position.isLong, true);
    assert.equal(position.sizeUsd, 1_000n * E18);
    assert.equal(position.collateralAmount, 100_000_000n);
    assert.equal(position.entryPrice, 60_000n * E18);
    assert.equal(position.active, true);
  });
});
