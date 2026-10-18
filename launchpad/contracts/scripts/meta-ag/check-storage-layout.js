#!/usr/bin/env node
/**
 * scripts/meta-ag/check-storage-layout.js
 *
 * Append-only enforcement for meta-ag UUPS storage layouts (S12).
 * Compares each tracked contract's current storage layout against the
 * frozen baseline under `storage-layout/meta-ag/<Name>.json` and exits
 * non-zero if any slot was removed, resized, renamed, or re-typed.
 *
 * Adding new slots AFTER the existing tail (i.e. inside `__gap[50]`) is
 * permitted. Phase 9 (Task 9.2) will harden this with richer diffs; this
 * Phase 2 drop covers the invariants required to protect the baselines
 * committed today.
 *
 * Usage:
 *   node scripts/meta-ag/check-storage-layout.js
 */

"use strict";

const fs = require("fs");
const path = require("path");

const { compareLayouts, readRequiredBaseline } = require("../lib/storage-layout");
const TRACKED = [
  "contracts/core/SidioraFactory.sol:SidioraFactory",
  "contracts/core/SidioraNFT.sol:SidioraNFT",
  "contracts/core/SidioraPool.sol:SidioraPool",
  "contracts/data/EventEmitter.sol:EventEmitter",
  "contracts/data/FeeAccumulator.sol:FeeAccumulator",
  "contracts/data/PoolRegistry.sol:PoolRegistry",
  "contracts/opticals/LaunchpadOpticalFactory.sol:LaunchpadOpticalFactory",
  "contracts/opticals/OpticalRegistry.sol:OpticalRegistry",
  "contracts/periphery/FeesRouter.sol:FeesRouter",
  "contracts/periphery/Quoter.sol:Quoter",
  "contracts/periphery/Router.sol:Router",
  "contracts/protocol/GovernanceModule.sol:GovernanceModule",
  "contracts/protocol/ProtocolConfig.sol:ProtocolConfig",
  "contracts/protocol/Treasury.sol:Treasury"
,

  "contracts/meta-ag/oracle/PriceOracle.sol:PriceOracle",
  "contracts/meta-ag/oracle/OracleHub.sol:OracleHub",
  "contracts/meta-ag/vault/PECORVault.sol:PECORVault",
  "contracts/meta-ag/engine/PECOR.sol:PECOR",
  "contracts/meta-ag/engine/PECOROrders.sol:PECOROrders",
  "contracts/meta-ag/analytics/TransactionTracker.sol:TransactionTracker",
  "contracts/meta-ag/router/MetaAGRouter.sol:MetaAGRouter",
  "contracts/meta-ag/quoter/MetaAGQuoter.sol:MetaAGQuoter",
];

async function main() {
  const hre = require("hardhat");
  await hre.run("compile", { quiet: true });

  const baseDir = path.resolve(__dirname, "..", "..", "storage-layout", "meta-ag");
  const errors = [];

  for (const fq of TRACKED) {
    const [sourceName, contractName] = fq.split(":");
    const family = sourceName.includes("/meta-ag/") ? "meta-ag" : "core";
    const baselinePath = path.join(baseDir, "..", family, `${contractName}.json`);
    if (!fs.existsSync(baselinePath)) {
      errors.push(`${fq}: required baseline missing`);
      continue;
    }

    const baseline = readRequiredBaseline(baselinePath);
    const buildInfo = await hre.artifacts.getBuildInfo(fq);
    const current =
      buildInfo.output.contracts[sourceName][contractName].storageLayout;

    errors.push(...compareLayouts(baseline, current).map(e => `${fq}: ${e}`));

  }

  if (errors.length > 0) {
    console.error("[meta-ag:layout:check] Layout drift detected:");
    for (const e of errors) console.error("  - " + e);
    process.exit(1);
  }
  console.log("[meta-ag:layout:check] OK — layouts match baselines.");
}

if (require.main === module) main().catch((err) => {
  console.error("[meta-ag:layout:check] failed:", err);
  process.exit(1);
});

module.exports = { TRACKED, main };
