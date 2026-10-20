import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import path from "node:path";
import { getAddress, type Address } from "viem";

import { buildGetAccount, type AccountState } from "../src/core/perps/calls";
import { portfolioFromAccount } from "../src/core/perps/portfolio";

const ROOT = path.resolve(__dirname, "..");
const PERPS: Address = getAddress("0x0000000000000000000000000000000000001015");
const ACCOUNT: Address = getAddress("0x00000000000000000000000000000000000a11ce");
const E18 = 10n ** 18n;

const FIXTURE: AccountState = {
  collateral: 250_000_000n,
  equityUsd: 2625n * 10n ** 17n,
  initialMarginUsd: 100n * E18,
  maintenanceMarginUsd: 30n * E18,
  withdrawable: 120_000_000n,
  feeTier: 1,
  volume14dUsd: 5_000n * E18,
  positionCount: 2n,
  openOrderCount: 0n,
};

const POSITIONS = [
  { sizeUsd: (1_000n * E18).toString(), leverage: (5n * E18).toString() },
  { sizeUsd: (3_000n * E18).toString(), leverage: (10n * E18).toString() },
];

const HOOK = readFileSync(path.join(ROOT, "src/hooks/perps/usePortfolioStats.ts"), "utf8");

describe("portfolio stats", () => {
  it("portfolioFromAccountTotalsTheAccountState", () => {
    assert.deepEqual(portfolioFromAccount(FIXTURE, POSITIONS), {
      totalVaultUsd: 250,
      freeCollateral: 120,
      unrealizedPnl: 12.5,
      totalLeverage: 8.75,
    });
  });

  it("unrealizedPnlIsNegativeWhenLossesExceedCollateral", () => {
    const totals = portfolioFromAccount(
      { collateral: 100_000_000n, equityUsd: -5n * E18, withdrawable: 0n },
      [],
    );
    assert.deepEqual(totals, { totalVaultUsd: 100, freeCollateral: 0, unrealizedPnl: -105, totalLeverage: 0 });
  });

  it("missingPositionFieldsCountAsZero", () => {
    const totals = portfolioFromAccount(FIXTURE, [{ sizeUsd: null, leverage: null }, POSITIONS[0]]);
    assert.equal(totals.totalLeverage, 5);
  });

  it("theAccountReadIsGetAccountAt0x1015", () => {
    const c = buildGetAccount(ACCOUNT);
    assert.equal(c.address, PERPS);
    assert.equal(c.functionName, "getAccount");
    assert.deepEqual(c.args, [ACCOUNT]);
  });

  it("hookReadsGetAccountWithoutMulticall", () => {
    assert.equal(HOOK.includes("MULTICALL3"), false);
    assert.equal(/multicall/i.test(HOOK), false);
    for (const name of ["TradingAccount", "getVaultAddress", "_vaultAddrCache"]) {
      assert.equal(HOOK.includes(name), false, `hook names ${name}`);
    }
    assert.match(HOOK, /readContract\(buildGetAccount\(/);
    assert.match(HOOK, /portfolioFromAccount\(raw\.account, visiblePositions\)/);
    assert.match(HOOK, /subscription PositionChanged\(\$userAddress: String!\)/);
  });
});
