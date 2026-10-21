import { it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { Address } from "viem";

import * as hook from "../src/hooks/spot/useSpotApprovals";
import { SPOT_SPENDERS } from "../src/core/network/approvals";
import { NATIVE_PAX_ADDRESS, SPOT_TOKENS } from "../src/core/spot/addresses";

const SOURCE = readFileSync(join(__dirname, "../src/hooks/spot/useSpotApprovals.ts"), "utf8");
const OWNER: Address = "0x1111111111111111111111111111111111111111";
const MAX = (1n << 256n) - 1n;

it("hookExportsNoApproveMax", () => {
  assert.deepEqual(Object.keys(hook).sort(), [
    "SPOT_NATIVE_TOKENS",
    "approveExact",
    "ensureAllowance",
    "getAllowance",
    "useSpotApprovals",
  ]);
  assert.deepEqual(Object.keys(hook.useSpotApprovals()).sort(), [
    "approveExact",
    "ensureAllowance",
    "getAllowance",
  ]);
});

it("sourceNamesNoUnlimitedAllowance", () => {
  assert.equal(SOURCE.includes("MAX_UINT256"), false);
  assert.equal(SOURCE.includes("approveMax"), false);
  assert.equal(SOURCE.includes("ALLOWANCE_TOPUP_THRESHOLD"), false);
  assert.equal(/MaxUint256|maxUint256|2n\s*\*\*\s*256n|1n\s*<<\s*256n/.test(SOURCE), false);
});

it("everyApprovalRoutesThroughPlanApproval", () => {
  const body = SOURCE.slice(SOURCE.indexOf("export async function ensureAllowance"));
  assert.equal((SOURCE.match(/planApproval\(/g) ?? []).length, 1);
  assert.ok(body.indexOf("planApproval(") >= 0);
  assert.equal((SOURCE.match(/functionName: "approve"/g) ?? []).length, 1);
  assert.equal((SOURCE.match(/approveExact\(\{/g) ?? []).length, 1);
  assert.ok(body.indexOf("approveExact({") > body.indexOf("planApproval("));
  assert.match(body, /for \(const call of plan\)/);
  assert.match(body, /amount: approvedAmount/);
  assert.match(body, /spender: approvedSpender/);
});

it("nativeSetIsTheTokenTableNativeEntries", () => {
  assert.deepEqual([...hook.SPOT_NATIVE_TOKENS], [SPOT_TOKENS.PAX.address]);
  assert.equal(SPOT_TOKENS.PAX.isNative, true);
});

it("nativeCoinsAndValueSkipReadAndApproval", async () => {
  const tenPax = 10n * 10n ** 18n;
  assert.deepEqual(
    await hook.ensureAllowance({
      token: null,
      owner: OWNER,
      spender: SPOT_SPENDERS.market,
      amount: tenPax,
    }),
    { approved: false, allowance: null },
  );
  assert.deepEqual(
    await hook.ensureAllowance({
      token: NATIVE_PAX_ADDRESS,
      owner: OWNER,
      spender: SPOT_SPENDERS.stop,
      amount: tenPax,
    }),
    { approved: false, allowance: null },
  );
  const sidPointer = SPOT_TOKENS.SID.address;
  assert.deepEqual(
    await hook.ensureAllowance({
      token: sidPointer,
      owner: OWNER,
      spender: SPOT_SPENDERS.stop,
      amount: tenPax,
      native: new Set<Address>([NATIVE_PAX_ADDRESS, sidPointer.toLowerCase() as Address]),
    }),
    { approved: false, allowance: null },
  );
});

it("outOfRangeAmountsRejectBeforeAnyRead", async () => {
  const usdl = SPOT_TOKENS.USDL.address;
  for (const amount of [MAX, -1n]) {
    await assert.rejects(
      hook.ensureAllowance({ token: usdl, owner: OWNER, spender: SPOT_SPENDERS.limit, amount }),
      RangeError,
    );
  }
});
