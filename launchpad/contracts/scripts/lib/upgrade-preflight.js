"use strict";
const fs = require("fs");
const crypto = require("crypto");
const { manifestPath, validateManifest } = require("./operations");
const { compareLayouts, readRequiredBaseline } = require("./storage-layout");
// A reviewed, chain-specific plan is mandatory even for deployment-only upgrade stages.
async function upgradePreflight(requiredContracts) {
  const hre = require("hardhat");
  if (!process.env.UPGRADE_PLAN) throw new Error("UPGRADE_PLAN required: pinned old code, artifacts, layouts, migration and simulation evidence");
  const plan = JSON.parse(fs.readFileSync(process.env.UPGRADE_PLAN, "utf8"));
  if (!Array.isArray(requiredContracts) || !requiredContracts.length || requiredContracts.some(x => !x)) throw new Error("Upgrade script requires exact target contracts");
  if (JSON.stringify([...requiredContracts].sort()) !== JSON.stringify((plan.contracts || []).map(x => x.contract).sort())) throw new Error("Upgrade plan contract set differs from script targets");
  const addresses = JSON.parse(fs.readFileSync(manifestPath(), "utf8"));
  await validateManifest(addresses, hre.ethers.provider);
  if (String(plan.chainId) !== String((await hre.ethers.provider.getNetwork()).chainId)) throw new Error("Upgrade plan chain mismatch");
  if (!plan.simulation || !plan.migrationCalldata || !Array.isArray(plan.contracts) || !plan.contracts.length) throw new Error("Incomplete qualification plan");
  const evidence = fs.readFileSync(plan.simulation.file);
  if (crypto.createHash("sha256").update(evidence).digest("hex") !== plan.simulation.sha256) throw new Error("Simulation evidence hash mismatch");
  for (const item of plan.contracts) {
    if (process.env.BEACON === "true" && item.contract !== "SidioraPool") throw new Error("Beacon upgrade restricted to SidioraPool");
    if (process.env.CONTRACT && item.contract !== process.env.CONTRACT) throw new Error("Contract differs from reviewed plan");
    if (item.proxyKey !== (item.beacon ? "PoolBeacon" : `${item.contract}_proxy`)) throw new Error("Plan proxy key does not match contract identity");
    if (item.beacon && item.contract !== "SidioraPool") throw new Error("Wrong beacon implementation");
    if (process.env.PROXY_ADDRESS || process.env.IMPL_ADDRESS || process.env.TIMELOCK_ADDRESS) throw new Error("Address overrides bypass reviewed identity; update the pinned manifest/plan instead");
    const proxy = addresses[item.proxyKey];
    if (!proxy || !item.oldImplementation || !item.oldCodeHash) throw new Error("Incomplete implementation identity");
    let actual;
    if (item.beacon) actual = await (await hre.ethers.getContractAt("PoolBeacon", proxy)).implementation();
    else {
      const slot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc";
      actual = hre.ethers.getAddress("0x" + (await hre.ethers.provider.getStorage(proxy, slot)).slice(-40));
    }
    if (actual.toLowerCase() !== item.oldImplementation.toLowerCase()) throw new Error("Stale implementation identity");
    if (hre.ethers.keccak256(await hre.ethers.provider.getCode(actual)) !== item.oldCodeHash) throw new Error("Old implementation code hash mismatch");
    if (item.beacon) {
      if (!item.tokenInventory) throw new Error("Beacon upgrade requires complete reviewed immutable-token compatibility inventory");
      const raw = fs.readFileSync(item.tokenInventory.file);
      if (crypto.createHash("sha256").update(raw).digest("hex") !== item.tokenInventory.sha256) throw new Error("Token inventory hash mismatch");
      const inventory = JSON.parse(raw);
      const registry = await hre.ethers.getContractAt("PoolRegistry", addresses.PoolRegistry_proxy);
      const count = Number(await registry.getPoolCount());
      if (!Array.isArray(inventory) || inventory.length !== count) throw new Error("Incomplete pool-token inventory");
      const seen = new Set();
      const block = await hre.ethers.provider.getBlockNumber();
      for (let offset = 0; offset < count; offset += 100) {
        const pools = await registry.getAllPools(offset, Math.min(100, count - offset));
        for (const pool of pools) {
          const record = inventory.find(x => x.pool.toLowerCase() === pool.toLowerCase());
          if (!record || seen.has(pool.toLowerCase())) throw new Error("Missing or duplicate pool inventory");
          seen.add(pool.toLowerCase());
          const livePool = await hre.ethers.getContractAt("SidioraPool", pool);
          const tokenAddress = await livePool.tokenAddress();
          if (tokenAddress.toLowerCase() !== record.token.toLowerCase() || hre.ethers.keccak256(await hre.ethers.provider.getCode(tokenAddress)) !== record.runtimeHash) throw new Error("Immutable token identity mismatch");
          const token = await hre.ethers.getContractAt("SidioraERC20", tokenAddress);
          if ((await token.pool()).toLowerCase() !== pool.toLowerCase()) throw new Error("Legacy token lacks bound transfer hooks");
          await token.getPastBalance(hre.ethers.ZeroAddress, block - 1);
          await token.getPastTotalSupply(block - 1);
          if (!record.transferHookSimulation) throw new Error("Missing transfer-hook fork proof");
        }
      }
    }
    const old = readRequiredBaseline(item.baseline);
    if (crypto.createHash("sha256").update(fs.readFileSync(item.baseline)).digest("hex") !== item.baselineSha256) throw new Error("Pinned baseline hash mismatch");
    const artifact = await hre.artifacts.readArtifact(item.contract);
    if (hre.ethers.keccak256(artifact.bytecode) !== item.newArtifactHash) throw new Error("New artifact hash mismatch");
    const info = await hre.artifacts.getBuildInfo(`${artifact.sourceName}:${artifact.contractName}`);
    const current = info.output.contracts[artifact.sourceName][artifact.contractName].storageLayout;
    const errors = compareLayouts(old, current);
    if (errors.length) throw new Error(`Incompatible storage: ${errors.join("; ")}`);
  }
  return plan;
}
module.exports = { upgradePreflight };
