import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import path from "node:path";
import { encodeFunctionData, type Address } from "viem";

import { accountViewFromState } from "../src/core/perps/accountView";
import { buildWithdrawAll, type AccountState } from "../src/core/perps/calls";
import { safeParseUnits } from "../src/core/perps/amounts";

const ROOT = path.resolve(__dirname, "..");
const PERPS: Address = "0x0000000000000000000000000000000000001015";
const USDL: Address = "0x85FcD13735F4309833A503EE804ea32395851479";
const E18 = 10n ** 18n;

const FIXTURE: AccountState = {
  collateral: 100_000_000n,
  equityUsd: -5n * E18,
  initialMarginUsd: 60n * E18,
  maintenanceMarginUsd: 30n * E18,
  withdrawable: 40_000_000n,
  feeTier: 2,
  volume14dUsd: 1_000n * E18,
  positionCount: 1n,
  openOrderCount: 3n,
};

function source(rel: string): string {
  return readFileSync(path.join(ROOT, rel), "utf8");
}

describe("account hook", () => {
  it("accountViewFromStateExposesTheFiveFields", () => {
    assert.deepEqual(accountViewFromState(FIXTURE), {
      collateral: 100_000_000n,
      equityUsd: -5n * E18,
      initialMarginUsd: 60n * E18,
      maintenanceMarginUsd: 30n * E18,
      withdrawable: 40_000_000n,
    });
  });

  it("withdrawAllSendsWithdrawable", () => {
    const c = buildWithdrawAll(accountViewFromState(FIXTURE));
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "withdraw");
    assert.deepEqual(c.args, [USDL, 40_000_000n]);
    assert.equal(
      encodeFunctionData({ abi: c.abi, functionName: c.functionName, args: c.args }),
      encodeFunctionData({ abi: c.abi, functionName: "withdraw", args: [USDL, 40_000_000n] }),
    );
    assert.throws(
      () => buildWithdrawAll(accountViewFromState({ ...FIXTURE, withdrawable: 0n })),
      (err: unknown) => (err as { code?: unknown }).code === "VALIDATION_AMOUNT_INVALID",
    );
  });

  it("depositAmountIsSixDecimalExact", () => {
    assert.equal(safeParseUnits("0.29", 6), 290_000n);
    assert.equal(safeParseUnits("100", 6), 100_000_000n);
  });

  it("noVaultCreationOrEmergencyWithdraw", () => {
    for (const rel of [
      "src/hooks/perps/useAccount.ts",
      "src/app/modals/DepositWithdrawModal.tsx",
      "src/page-modules/portfolio/index.tsx",
    ]) {
      const text = source(rel);
      for (const name of ["createVault", "getVaultAddress", "useVault", "useEmergencyWithdraw", "Math.floor"]) {
        assert.equal(text.includes(name), false, `${rel} names ${name}`);
      }
    }
    const modal = source("src/app/modals/DepositWithdrawModal.tsx");
    assert.match(modal, /safeParseUnits\(amount, USDL\.decimals\)/);
    assert.match(modal, /withdrawAll\(address\)/);
  });
});
