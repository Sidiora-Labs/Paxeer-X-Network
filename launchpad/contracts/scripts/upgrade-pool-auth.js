/**
 * Upgrade FeeAccumulator + SidioraFactory on Paxeer
 * 
 * Changes:
 *   - FeeAccumulator: adds FACTORY_ROLE + authorizePool(address) function
 *   - SidioraFactory: calls feeAccumulator.authorizePool(pool) in _createMarket
 *
 * Then wires FACTORY_ROLE on FeeAccumulator → Factory proxy.
 *
 * Run: npx hardhat run scripts/upgrade-pool-auth.js --network paxeer-network
 */
const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

async function main() {
  await require("./lib/upgrade-preflight").upgradePreflight(["FeeAccumulator", "SidioraFactory"]);
  const [deployer] = await ethers.getSigners();
  const addrPath = path.join(__dirname, "..", "deployments", require("./lib/operations").networkName() + "-addresses.json");
  const addresses = JSON.parse(fs.readFileSync(addrPath, "utf8"));
  await require("./lib/operations").validateManifest(addresses, ethers.provider);

  console.log("\n🔄 Upgrading FeeAccumulator + SidioraFactory for auto pool authorization\n");

  // ─── 1. Upgrade FeeAccumulator ───
  console.log("── Step 1: Upgrade FeeAccumulator ──");
  const AccFactory = await ethers.getContractFactory("FeeAccumulator");
  const newAccImpl = await AccFactory.deploy();
  await newAccImpl.waitForDeployment();
  const newAccAddr = await newAccImpl.getAddress();
    await require("./lib/operations").checkpointDeployment(newAccAddr, "newAccImpl");
  console.log(`  New impl: ${newAccAddr}`);
  console.log(`  Old impl: ${addresses.FeeAccumulator_impl}`);

  const accProxy = AccFactory.attach(addresses.FeeAccumulator_proxy);
  const tx1 = await accProxy.upgradeToAndCall(newAccAddr, "0x");
  await tx1.wait();
  console.log("  ✅ FeeAccumulator proxy upgraded");

  // ─── 2. Upgrade SidioraFactory ───
  console.log("\n── Step 2: Upgrade SidioraFactory ──");
  const FactFactory = await ethers.getContractFactory("SidioraFactory");
  const newFactImpl = await FactFactory.deploy();
  await newFactImpl.waitForDeployment();
  const newFactAddr = await newFactImpl.getAddress();
    await require("./lib/operations").checkpointDeployment(newFactAddr, "newFactImpl");
  console.log(`  New impl: ${newFactAddr}`);
  console.log(`  Old impl: ${addresses.SidioraFactory_impl}`);

  const factProxy = FactFactory.attach(addresses.SidioraFactory_proxy);
  const tx2 = await factProxy.upgradeToAndCall(newFactAddr, "0x");
  await tx2.wait();
  console.log("  ✅ SidioraFactory proxy upgraded");

  // ─── 3. Grant FACTORY_ROLE on FeeAccumulator to Factory ───
  console.log("\n── Step 3: Wire FACTORY_ROLE ──");
  const FACTORY_ROLE = ethers.keccak256(ethers.toUtf8Bytes("FACTORY_ROLE"));
  const hasRole = await accProxy.hasRole(FACTORY_ROLE, addresses.SidioraFactory_proxy);
  if (hasRole) {
    console.log("  ✅ Factory already has FACTORY_ROLE on FeeAccumulator");
  } else {
    const tx3 = await accProxy.grantRole(FACTORY_ROLE, addresses.SidioraFactory_proxy);
    await tx3.wait();
    console.log("  ✅ FACTORY_ROLE granted to Factory on FeeAccumulator");
  }

  // ─── 4. Update addresses file ───
  addresses.FeeAccumulator_impl_prev = addresses.FeeAccumulator_impl;
  addresses.FeeAccumulator_impl = newAccAddr;
  addresses.SidioraFactory_impl_prev = addresses.SidioraFactory_impl;
  addresses.SidioraFactory_impl = newFactAddr;
  addresses._meta.lastUpgrade = {
    contracts: ["FeeAccumulator", "SidioraFactory"],
    timestamp: new Date().toISOString(),
    reason: "Auto pool authorization via Factory.authorizePool",
  };
  require("./lib/operations").atomicWrite(addrPath, addresses);
  console.log("\n  Addresses file updated");

  // ─── 5. Verify ───
  console.log("\n── Verification ──");
  const factoryHasRole = await accProxy.hasRole(FACTORY_ROLE, addresses.SidioraFactory_proxy);
  console.log(`  Factory has FACTORY_ROLE on FeeAccumulator: ${factoryHasRole}`);
  console.log("\n✅ Upgrade complete. New markets will auto-authorize pools on FeeAccumulator.\n");
}

main().then(() => process.exit(0)).catch(e => { console.error("❌", e); process.exit(1); });
