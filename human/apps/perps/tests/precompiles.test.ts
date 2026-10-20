// Address module tests: release defaults, validated overrides and the single chain id.
// Run via `pnpm test` — uses node:test + tsx loader (no Jest/Vitest required).

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";

import { PRECOMPILES } from "../src/core/network/precompiles";
import * as precompilesModule from "../src/core/network/precompiles";
import { paxeerChain } from "../src/core/chain";
import { SPOT_TOKENS } from "../src/core/spot/addresses";

const ROOT = path.resolve(__dirname, "..");
const MODULE_PATH = path.join(ROOT, "src/core/network/precompiles.ts");
const SPOT_ADDRESSES_PATH = path.join(ROOT, "src/core/spot/addresses.ts");
const USDL = "0x85FcD13735F4309833A503EE804ea32395851479";

const OVERRIDE_KEYS = [
  "NEXT_PUBLIC_PERPS_PRECOMPILE",
  "NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE",
  "NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE",
  "NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE",
  "NEXT_PUBLIC_PECOR_STOP_PRECOMPILE",
  "NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE",
  "NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE",
] as const;

/** Load a module in a fresh process with the given overrides; prints one of its exports as JSON. */
function loadWith(
  overrides: Partial<Record<(typeof OVERRIDE_KEYS)[number], string>>,
  modulePath: string = MODULE_PATH,
  exportName: string = "PRECOMPILES",
) {
  const env: NodeJS.ProcessEnv = { ...process.env };
  for (const key of OVERRIDE_KEYS) delete env[key];
  Object.assign(env, overrides);
  return spawnSync(
    process.execPath,
    [
      "--import",
      "tsx",
      "-e",
      `process.stdout.write(JSON.stringify(require(${JSON.stringify(modulePath)})[${JSON.stringify(exportName)}]))`,
    ],
    { cwd: ROOT, env, encoding: "utf8" },
  );
}

describe("precompiles", () => {
  it("defaultsAreTheReleaseAddresses", () => {
    const run = loadWith({});
    assert.equal(run.status, 0, run.stderr);
    const loaded = JSON.parse(run.stdout);
    const expected = {
      perps: "0x0000000000000000000000000000000000001015",
      launchpad: "0x0000000000000000000000000000000000001017",
      spot: {
        PECOR: "0x0000000000000000000000000000000000001021",
        PECOROrders: "0x0000000000000000000000000000000000001022",
        PECORStopOrders: "0x0000000000000000000000000000000000001023",
        PECORQuoter: "0x0000000000000000000000000000000000001024",
        PECORVault: "0x0000000000000000000000000000000000001025",
      },
    };
    assert.deepEqual(loaded, expected);
    assert.equal(loaded.perps, "0x0000000000000000000000000000000000001015");
    assert.equal(
      loaded.spot.PECOROrders,
      "0x0000000000000000000000000000000000001022",
    );
    assert.equal("pricefeed" in PRECOMPILES, false);
  });

  it("overrideMustBeTwentyBytes", () => {
    const short = loadWith({ NEXT_PUBLIC_PERPS_PRECOMPILE: "0x1234" });
    assert.notEqual(short.status, 0);
    assert.match(
      short.stderr,
      /NEXT_PUBLIC_PERPS_PRECOMPILE must be 0x followed by 40 hex digits, got "0x1234"/,
    );

    const unprefixed = loadWith({
      NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE: "1".repeat(40),
    });
    assert.notEqual(unprefixed.status, 0);
    assert.match(
      unprefixed.stderr,
      /NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE must be 0x followed by 40 hex digits/,
    );

    const local = "0x00000000000000000000000000000000000a1015";
    const valid = loadWith({ NEXT_PUBLIC_PERPS_PRECOMPILE: local });
    assert.equal(valid.status, 0, valid.stderr);
    const loaded = JSON.parse(valid.stdout);
    assert.equal(loaded.perps, local);
    assert.equal(
      loaded.launchpad,
      "0x0000000000000000000000000000000000001017",
    );

    const source = readFileSync(MODULE_PATH, "utf8");
    assert.equal(source.includes("NEXT_PUBLIC_SPOT_"), false);
    for (const key of OVERRIDE_KEYS)
      assert.equal(source.includes(`process.env.${key}`), true, key);
  });

  it("chainIdIsOneTwentyFive", () => {
    assert.equal(paxeerChain.id, 125);
    assert.deepEqual(
      Object.keys(precompilesModule).filter((key) => /chain/i.test(key)),
      [],
    );
    assert.equal(JSON.stringify(PRECOMPILES).includes("125"), false);
  });

  it("spotContractsResolveToPrecompiles", () => {
    const run = loadWith({}, SPOT_ADDRESSES_PATH, "PECOR_CONTRACTS");
    assert.equal(run.status, 0, run.stderr);
    const loaded = JSON.parse(run.stdout);
    assert.deepEqual(loaded, {
      PECOR: "0x0000000000000000000000000000000000001021",
      PECOROrders: "0x0000000000000000000000000000000000001022",
      PECORStopOrders: "0x0000000000000000000000000000000000001023",
      PECORQuoter: "0x0000000000000000000000000000000000001024",
      PECORVault: "0x0000000000000000000000000000000000001025",
    });
    assert.equal("TransactionTracker" in loaded, false);
    assert.equal("PriceOracle" in loaded, false);
    assert.equal("pricefeed" in PRECOMPILES, false);

    const overridden = "0x00000000000000000000000000000000000a1022";
    const viaModule = loadWith(
      { NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE: overridden },
      SPOT_ADDRESSES_PATH,
      "PECOR_CONTRACTS",
    );
    assert.equal(viaModule.status, 0, viaModule.stderr);
    assert.equal(JSON.parse(viaModule.stdout).PECOROrders, overridden);

    const source = readFileSync(SPOT_ADDRESSES_PATH, "utf8");
    assert.equal(source.includes("NEXT_PUBLIC_SPOT_"), false);
    assert.equal(source.includes("process.env"), false);
  });

  it("usdlHasOneAddress", async () => {
    process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL ??= "https://gateway.test";
    const { COLLATERAL_TOKENS } = await import("../src/core/perps/config");
    assert.equal(
      COLLATERAL_TOKENS.usdl.address.toLowerCase(),
      USDL.toLowerCase(),
    );
    assert.equal(SPOT_TOKENS.USDL.address.toLowerCase(), USDL.toLowerCase());
    assert.equal(COLLATERAL_TOKENS.usdl.decimals, 6);
    assert.equal(SPOT_TOKENS.USDL.decimals, 6);
    assert.equal(SPOT_TOKENS.PAX.decimals, 6);
    assert.equal(SPOT_TOKENS.SID.decimals, 6);
    assert.equal("WPAX9" in SPOT_TOKENS, false);
    assert.equal(
      readFileSync(SPOT_ADDRESSES_PATH, "utf8").includes(
        "0x7c69c84daAEe90B21eeCABDb8f0387897E9B7B37",
      ),
      false,
    );
  });
});
