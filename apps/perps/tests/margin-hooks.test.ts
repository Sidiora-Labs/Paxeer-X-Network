import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { existsSync, readFileSync } from "node:fs";
import { getAddress, type Address } from "viem";

import {
  buildGetAccountSummary,
  buildGetPositionMargin,
  buildTopUpPosition,
} from "../src/core/perps/calls";

const PERPS: Address = getAddress("0x0000000000000000000000000000000000001015");
const USDL: Address = getAddress("0x85FcD13735F4309833A503EE804ea32395851479");
const ACCOUNT: Address = getAddress("0x00000000000000000000000000000000000000a1");

const path = (file: string) => new URL("../" + file, import.meta.url);
const read = (file: string) => readFileSync(path(file), "utf8");
const HOOKS = "src/hooks/perps/useMarginMode.ts";

describe("margin hooks", () => {
  it("exportsOnlyTheThreeAccountHooks", async () => {
    const mod = await import("../src/hooks/perps/useMarginMode");
    assert.deepEqual(Object.keys(mod).sort(), ["useAccountSummary", "usePositionMargin", "useTopUpPosition"]);
    const exported = [...read(HOOKS).matchAll(/^export\s+(?:async\s+)?(?:function|const|let|class)\s+(\w+)/gm)].map(
      (m) => m[1],
    );
    assert.deepEqual(exported.sort(), ["useAccountSummary", "usePositionMargin", "useTopUpPosition"]);
  });

  it("vaultHookIsAbsent", () => {
    assert.equal(existsSync(path("src/hooks/perps/useVault.ts")), false);
    const text = read(HOOKS);
    for (const name of [
      "useVault",
      "getVaultAddress",
      "createVault",
      "TradingAccountABI",
      "useMarginMode",
      "useSetMarginMode",
      "useTransferMargin",
      "useEmergencyWithdraw",
      "transferMargin",
      "emergencyWithdraw",
    ]) {
      assert.equal(text.includes(name), false, `${HOOKS} names ${name}`);
    }
  });

  it("hooksSendThePerpsBuilders", () => {
    const text = read(HOOKS);
    assert.match(text, /writeContract\(buildTopUpPosition\(args\.positionId, args\.amount\)\)/);
    assert.match(text, /readContract\(buildGetPositionMargin\(positionId\)\)/);
    assert.match(text, /readContract\(\s*buildGetAccountSummary\(userAddress, tokenAddress\),?\s*\)/);
  });

  it("buildersTargetTheAccountAt0x1015", () => {
    const topUp = buildTopUpPosition(7n, 15_000_000n);
    assert.equal(topUp.address, PERPS);
    assert.equal(topUp.functionName, "topUpPosition");
    assert.deepEqual(topUp.args, [7n, 15_000_000n]);

    const margin = buildGetPositionMargin(7n);
    assert.equal(margin.address, PERPS);
    assert.equal(margin.functionName, "getPositionMargin");
    assert.deepEqual(margin.args, [7n]);

    const summary = buildGetAccountSummary(ACCOUNT, USDL);
    assert.equal(summary.address, PERPS);
    assert.equal(summary.functionName, "getAccountSummary");
    assert.deepEqual(summary.args, [ACCOUNT, USDL]);
  });

  it("refusesZeroTopUpAndZeroAccount", () => {
    assert.throws(
      () => buildTopUpPosition(7n, 0n),
      (err: unknown) => (err as { code?: unknown }).code === "VALIDATION_AMOUNT_INVALID",
    );
    assert.throws(
      () => buildGetAccountSummary(getAddress("0x0000000000000000000000000000000000000000"), USDL),
      (err: unknown) => (err as { code?: unknown }).code === "WALLET_NOT_CONNECTED",
    );
  });
});
