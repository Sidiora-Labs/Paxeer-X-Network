/**
 * Sidiora Launchpad AMM — Upgrade FeeAccumulator + Deploy LaunchpadOpticalFactory
 *
 * This script performs:
 *   1. Upgrades FeeAccumulator UUPS proxy to the new implementation
 *      (adds opticalSurplus, beforeFeeDistribution hook wiring, OPTICAL_GRANTER_ROLE)
 *   2. Deploys LaunchpadOpticalFactory as a UUPS proxy
 *      (self-service: any creator can deploy their own LaunchpadOptical on-chain)
 *   3. Grants OPTICAL_GRANTER_ROLE to the factory on FeeAccumulator
 *   4. Registers the factory in OpticalRegistry
 *
 * Usage:
 *   npx hardhat run scripts/upgrade-launchpad-optical.js --network paxeer-network
 *
 * No per-project env vars needed — creators deploy their own opticals on-chain
 * by calling LaunchpadOpticalFactory.createLaunchpadOptical() with their params.
 *
 * Requires: deployments/paxeer-addresses.json
 */

const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

async function main() {
  await require("./lib/upgrade-preflight").upgradePreflight(["FeeAccumulator"]);
  const [deployer] = await ethers.getSigners();
  const balance = await ethers.provider.getBalance(deployer.address);
  console.log(`\n🚀 Upgrade FeeAccumulator + Deploy LaunchpadOptical`);
  console.log(`   Deployer:  ${deployer.address}`);
  console.log(`   Balance:   ${ethers.formatEther(balance)} PAX`);
  console.log(`   Network:   ${(await ethers.provider.getNetwork()).chainId}\n`);

  // ─── Load existing addresses ───
  const addrPath = path.join(__dirname, "..", "deployments", require("./lib/operations").networkName() + "-addresses.json");
  if (!fs.existsSync(addrPath)) {
    throw new Error(`Addresses file not found: ${addrPath}\nRun deploy.js first.`);
  }
  const addresses = JSON.parse(fs.readFileSync(addrPath, "utf8"));
  await require("./lib/operations").validateManifest(addresses, ethers.provider);

  const FEE_ACC_PROXY = addresses.FeeAccumulator_proxy;
  const POOL_REGISTRY_PROXY = addresses.PoolRegistry_proxy;
  const OPT_REGISTRY_PROXY = addresses.OpticalRegistry_proxy;

  if (!FEE_ACC_PROXY) throw new Error("FeeAccumulator_proxy not found in addresses file");
  if (!POOL_REGISTRY_PROXY) throw new Error("PoolRegistry_proxy not found in addresses file");

  // ═══════════════════════════════════════════════════════════════════
  // Step 1: Upgrade FeeAccumulator
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 1: Upgrade FeeAccumulator ──");

  const FeeAccFactory = await ethers.getContractFactory("FeeAccumulator");
  const newAccImpl = await FeeAccFactory.deploy();
  await newAccImpl.waitForDeployment();
  const newAccImplAddr = await newAccImpl.getAddress();
    await require("./lib/operations").checkpointDeployment(newAccImplAddr, "newAccImpl");
  console.log(`  New impl deployed: ${newAccImplAddr}`);

  const accProxy = FeeAccFactory.attach(FEE_ACC_PROXY);
  const oldAccImpl = addresses.FeeAccumulator_impl;
  console.log(`  Old impl:          ${oldAccImpl}`);
  console.log(`  Proxy:             ${FEE_ACC_PROXY}`);

  console.log(`  Upgrading proxy...`);
  const upgradeTx = await accProxy.upgradeToAndCall(newAccImplAddr, "0x");
  await upgradeTx.wait();
  console.log(`  ✅ FeeAccumulator upgraded`);

  // Verify new function exists
  const optSurplus = await accProxy.getOpticalSurplus(ethers.ZeroAddress);
  console.log(`  Verified: getOpticalSurplus() callable (returns ${optSurplus})`);

  // Update addresses
  addresses.FeeAccumulator_impl_prev = oldAccImpl;
  addresses.FeeAccumulator_impl = newAccImplAddr;

  // ═══════════════════════════════════════════════════════════════════
  // Step 2: Deploy LaunchpadOpticalFactory (UUPS proxy)
  // ═══════════════════════════════════════════════════════════════════
  console.log("\n── Step 2: Deploy LaunchpadOpticalFactory ──");

  const Proxy = await ethers.getContractFactory("UUPSProxy");
  const LOFFactory = await ethers.getContractFactory("LaunchpadOpticalFactory");
  const lofImpl = await LOFFactory.deploy();
  await lofImpl.waitForDeployment();
  const lofImplAddr = await lofImpl.getAddress();
    await require("./lib/operations").checkpointDeployment(lofImplAddr, "lofImpl");
  console.log(`  Impl deployed: ${lofImplAddr}`);

  const lofInitData = LOFFactory.interface.encodeFunctionData("initialize", [
    POOL_REGISTRY_PROXY,
    FEE_ACC_PROXY,
    OPT_REGISTRY_PROXY || ethers.ZeroAddress,
    deployer.address,
  ]);
  const lofProxy = await Proxy.deploy(lofImplAddr, lofInitData);
  await lofProxy.waitForDeployment();
  const lofProxyAddr = await lofProxy.getAddress();
    await require("./lib/operations").checkpointDeployment(lofProxyAddr, "lofProxy");
  console.log(`  ✅ LaunchpadOpticalFactory proxy: ${lofProxyAddr}`);

  addresses.LaunchpadOpticalFactory_impl = lofImplAddr;
  addresses.LaunchpadOpticalFactory_proxy = lofProxyAddr;

  // ═══════════════════════════════════════════════════════════════════
  // Step 3: Grant OPTICAL_GRANTER_ROLE to Factory
  // ═══════════════════════════════════════════════════════════════════
  console.log("\n── Step 3: Grant OPTICAL_GRANTER_ROLE ──");

  const OPTICAL_GRANTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("OPTICAL_GRANTER_ROLE"));
  console.log(`  Granting OPTICAL_GRANTER_ROLE to LaunchpadOpticalFactory...`);
  const grantTx = await accProxy.grantRole(OPTICAL_GRANTER_ROLE, lofProxyAddr);
  await grantTx.wait();
  console.log(`  ✅ Role granted`);

  // Verify
  const hasRole = await accProxy.hasRole(OPTICAL_GRANTER_ROLE, lofProxyAddr);
  console.log(`  Verified: hasRole(OPTICAL_GRANTER_ROLE) = ${hasRole}`);

  // ═══════════════════════════════════════════════════════════════════
  // Step 4: Register in OpticalRegistry (optional)
  // ═══════════════════════════════════════════════════════════════════
  if (OPT_REGISTRY_PROXY) {
    console.log("\n── Step 4: Register Factory in OpticalRegistry ──");
    const OptRegistry = await ethers.getContractFactory("OpticalRegistry");
    const optReg = OptRegistry.attach(OPT_REGISTRY_PROXY);

    try {
      const regTx = await optReg.registerOptical(
        lofProxyAddr,
        "LaunchpadOpticalFactory",
        "Self-service factory for deploying vesting + capital-raise opticals",
        2, // riskLevel (medium — has fee diversion)
        "Sidiora"
      );
      await regTx.wait();
      console.log(`  ✅ Factory registered in OpticalRegistry`);
    } catch (err) {
      console.log(`  ⚠️  OpticalRegistry registration failed (non-critical): ${err.message}`);
    }
  }

  // ═══════════════════════════════════════════════════════════════════
  // Save updated addresses
  // ═══════════════════════════════════════════════════════════════════
  addresses._meta.lastUpgrade = {
    contracts: ["FeeAccumulator", "LaunchpadOpticalFactory"],
    timestamp: new Date().toISOString(),
    reason: "Add opticalSurplus to FeeAccumulator + deploy self-service LaunchpadOpticalFactory",
  };

  require("./lib/operations").atomicWrite(addrPath, addresses);
  console.log(`\n✅ Complete! Addresses saved to ${addrPath}`);

  // ─── Summary ───
  console.log(`\n═══ Summary ═══`);
  console.log(`  FeeAccumulator upgraded:           ${FEE_ACC_PROXY}`);
  console.log(`    new impl:                        ${newAccImplAddr}`);
  console.log(`  LaunchpadOpticalFactory deployed:  ${lofProxyAddr}`);
  console.log(`    impl:                            ${lofImplAddr}`);
  console.log(`  OPTICAL_GRANTER_ROLE granted:       ✓`);
  console.log(`  OpticalRegistry updated:            ${OPT_REGISTRY_PROXY ? "✓" : "skipped"}`);
  console.log(``);
  console.log(`  Creators can now call:`);
  console.log(`    LaunchpadOpticalFactory(${lofProxyAddr}).createLaunchpadOptical(...)`);
  console.log(`  to deploy their own vesting + capital-raise optical for their pool.`);
  console.log(``);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error("❌ Upgrade failed:", error);
    process.exit(1);
  });
