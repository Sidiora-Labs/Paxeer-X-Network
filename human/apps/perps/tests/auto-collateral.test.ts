import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import path from "node:path";
import { getAddress } from "viem";

import { pickCollateral } from "../src/core/perps/accountView";
import type { AccountState } from "../src/core/perps/calls";

const ROOT = path.resolve(__dirname, "..");
const USDL = getAddress("0x85fcd13735f4309833a503ee804ea32395851479");
const E18 = 10n ** 18n;

const ACCOUNT: AccountState = {
  collateral: 25_000_000n,
  equityUsd: 25n * E18,
  initialMarginUsd: 0n,
  maintenanceMarginUsd: 0n,
  withdrawable: 25_000_000n,
  feeTier: 0,
  volume14dUsd: 0n,
  positionCount: 0n,
  openOrderCount: 0n,
};

describe("auto collateral", () => {
  it("autoCollateralReadsAccount", () => {
    assert.deepEqual(pickCollateral(ACCOUNT), {
      token: USDL,
      symbol: "USDL",
      decimals: 6,
      collateral: 25_000_000n,
    });
    assert.deepEqual(pickCollateral({ collateral: 0n }), {
      token: USDL,
      symbol: "USDL",
      decimals: 6,
      collateral: 0n,
    });

    const hook = readFileSync(path.join(ROOT, "src/hooks/perps/useAutoCollateral.ts"), "utf8");
    assert.match(hook, /const \{ getAccount \} = useAccount\(\);/);
    assert.match(hook, /pickCollateral\(await getAccount\(address as Address\)\)/);
    for (const name of ["getVaultAddress", "getBalance", "useVault", "USID"]) {
      assert.equal(hook.includes(name), false, `useAutoCollateral names ${name}`);
    }
  });
});
