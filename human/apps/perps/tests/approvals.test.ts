import { it } from "node:test";
import { strict as assert } from "node:assert";
import type { Address } from "viem";

import {
  LAUNCHPAD_SPENDER,
  PERPS_SPENDER,
  SPOT_SPENDERS,
  needsApproval,
  planApproval,
  type ApprovalPlan,
} from "../src/core/network/approvals";

const USDL: Address = "0x5f2b3c6d8e9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c";
const PAX_POINTER: Address = "0xAa00000000000000000000000000000000000001";
const SID_POINTER: Address = "0xbB00000000000000000000000000000000000002";
const NATIVE = new Set<Address>([PAX_POINTER, SID_POINTER]);
const MAX_UINT256 = (1n << 256n) - 1n;

function reader(value: bigint): { read: () => Promise<bigint>; calls: () => number } {
  let calls = 0;
  return {
    read: async () => {
      calls += 1;
      return value;
    },
    calls: () => calls,
  };
}

function approve(spender: Address, amount: bigint): ApprovalPlan {
  return [{ address: USDL, functionName: "approve", args: [spender, amount] }];
}

it("sufficientAllowanceSkipsApproval", async () => {
  assert.equal(needsApproval(USDL, 50_000_000n, 40_000_000n, NATIVE), false);
  assert.equal(needsApproval(USDL, 40_000_000n, 40_000_000n, NATIVE), false);
  assert.equal(needsApproval(USDL, 39_999_999n, 40_000_000n, NATIVE), true);
  for (const allowance of [0n, 1n, 40_000_000n, MAX_UINT256]) {
    assert.equal(needsApproval(PAX_POINTER, allowance, 40_000_000n, NATIVE), false);
    assert.equal(needsApproval(null, allowance, 40_000_000n, NATIVE), false);
  }
  const allowance = reader(50_000_000n);
  assert.deepEqual(await planApproval(USDL, PERPS_SPENDER, allowance.read, 40_000_000n, NATIVE), []);
  assert.equal(allowance.calls(), 1);
});

it("shortAllowanceApprovesExact", async () => {
  const allowance = reader(10_000_000n);
  const plan = await planApproval(USDL, SPOT_SPENDERS.market, allowance.read, 40_000_000n, NATIVE);
  assert.deepEqual(plan, approve("0x0000000000000000000000000000000000001021", 40_000_000n));
  assert.equal(allowance.calls(), 1);
});

it("perpsSpenderIsPrecompile", async () => {
  assert.equal(PERPS_SPENDER, "0x0000000000000000000000000000000000001015");
  const placeOrder = await planApproval(USDL, PERPS_SPENDER, reader(0n).read, 100_000_000n, NATIVE);
  const deposit = await planApproval(USDL, PERPS_SPENDER, reader(0n).read, 100_000_000n, NATIVE);
  for (const plan of [placeOrder, deposit]) {
    assert.deepEqual(plan, approve("0x0000000000000000000000000000000000001015", 100_000_000n));
  }
});

it("spotSpendersAreRailAddresses", async () => {
  assert.deepEqual(SPOT_SPENDERS, {
    market: "0x0000000000000000000000000000000000001021",
    limit: "0x0000000000000000000000000000000000001022",
    stop: "0x0000000000000000000000000000000000001023",
  });
  assert.equal(LAUNCHPAD_SPENDER, "0x0000000000000000000000000000000000001017");
  const plan = await planApproval(USDL, SPOT_SPENDERS.limit, reader(0n).read, 25_000_000n, NATIVE);
  assert.deepEqual(plan, approve("0x0000000000000000000000000000000000001022", 25_000_000n));
});

it("noUnlimitedApprovals", async () => {
  const matrix: ReadonlyArray<readonly [Address, bigint, bigint]> = [
    [PERPS_SPENDER, 0n, 100_000_000n],
    [PERPS_SPENDER, 99_999_999n, 100_000_000n],
    [LAUNCHPAD_SPENDER, 0n, 7_500_000n],
    [SPOT_SPENDERS.market, 1n, 40_000_000n],
    [SPOT_SPENDERS.limit, 0n, 25_000_000n],
    [SPOT_SPENDERS.stop, 5n, MAX_UINT256 - 1n],
  ];
  for (const [spender, current, amount] of matrix) {
    const plan = await planApproval(USDL, spender, reader(current).read, amount, NATIVE);
    assert.equal(plan.length, 1);
    const [, approved] = plan[0].args;
    assert.equal(approved, amount);
    assert.notEqual(approved, MAX_UINT256);
  }
  await assert.rejects(planApproval(USDL, PERPS_SPENDER, reader(0n).read, MAX_UINT256, NATIVE), RangeError);
  await assert.rejects(planApproval(USDL, PERPS_SPENDER, reader(0n).read, -1n, NATIVE), RangeError);
  assert.throws(() => needsApproval(USDL, 0n, MAX_UINT256, NATIVE), RangeError);
});

it("nativeCoinsNeedNoApproval", async () => {
  const paxRead = reader(0n);
  const sidRead = reader(0n);
  const valueRead = reader(0n);
  const tenPax = 10n * 10n ** 18n;
  assert.deepEqual(await planApproval(PAX_POINTER, SPOT_SPENDERS.market, paxRead.read, tenPax, NATIVE), []);
  assert.deepEqual(await planApproval(SID_POINTER, SPOT_SPENDERS.stop, sidRead.read, tenPax, NATIVE), []);
  assert.deepEqual(await planApproval(null, SPOT_SPENDERS.market, valueRead.read, tenPax, NATIVE), []);
  assert.deepEqual(
    await planApproval(
      PAX_POINTER.toLowerCase() as Address,
      SPOT_SPENDERS.market,
      paxRead.read,
      tenPax,
      NATIVE,
    ),
    [],
  );
  assert.equal(paxRead.calls(), 0);
  assert.equal(sidRead.calls(), 0);
  assert.equal(valueRead.calls(), 0);
});
