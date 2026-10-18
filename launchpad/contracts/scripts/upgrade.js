/**
 * Sidiora Launchpad AMM — General Purpose UUPS Upgrade Script
 *
 * Deploys a new implementation contract and upgrades the proxy to point to it.
 * Works for any UUPS-proxied contract in the protocol.
 *
 * Usage:
 *   CONTRACT=Router npx hardhat run scripts/upgrade.js --network paxeer-network
 *   CONTRACT=SidioraPool BEACON=true npx hardhat run scripts/upgrade.js --network paxeer-network
 *
 * Environment:
 *   CONTRACT  — Name of the contract to upgrade (e.g. Router, Quoter, SidioraFactory)
 *   BEACON    — Set to "true" if upgrading the PoolBeacon (SidioraPool implementation)
 *
 * Requires: deployments/paxeer-addresses.json
 */

const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

async function main() {
  const upgradePlan = await require("./lib/upgrade-preflight").upgradePreflight([process.env.CONTRACT]);
  const contractName = process.env.CONTRACT;
  const isBeacon = process.env.BEACON === "true";

  if (!contractName) {
    console.error("❌ CONTRACT env var not set. Example: CONTRACT=Router npx hardhat run scripts/upgrade.js");
    process.exit(1);
  }

  const [deployer] = await ethers.getSigners();
  console.log(`\n🔄 Upgrading ${contractName}`);
  console.log(`   Deployer: ${deployer.address}`);
  console.log(`   Mode:     ${isBeacon ? "Beacon upgrade (all pools)" : "UUPS proxy upgrade"}\n`);

  const addrPath = path.join(__dirname, "..", "deployments", require("./lib/operations").networkName() + "-addresses.json");
  if (!fs.existsSync(addrPath)) {
    throw new Error(`Addresses file not found: ${addrPath}\nRun deploy.js first.`);
  }
  const addresses = JSON.parse(fs.readFileSync(addrPath, "utf8"));
  await require("./lib/operations").validateManifest(addresses, ethers.provider);

  // Deploy new implementation
  console.log(`  Deploying new ${contractName} implementation...`);
  const Factory = await ethers.getContractFactory(contractName);
  const newImpl = await Factory.deploy();
  await newImpl.waitForDeployment();
  const newImplAddr = await newImpl.getAddress();
    await require("./lib/operations").checkpointDeployment(newImplAddr, "newImpl");
  console.log(`  New impl: ${newImplAddr}`);

  if (isBeacon) {
    // ─── Beacon Upgrade (SidioraPool) ───
    const beaconAddr = addresses.PoolBeacon;
    if (!beaconAddr) throw new Error("PoolBeacon address not found in deployment file");

    const PoolBeacon = await ethers.getContractFactory("PoolBeacon");
    const beacon = PoolBeacon.attach(beaconAddr);

    const oldImpl = await beacon.implementation();
    console.log(`  Old impl: ${oldImpl}`);
    console.log(`  Upgrading beacon at ${beaconAddr}...`);

    const tx = await beacon.upgradeTo(newImplAddr);
    await tx.wait();

    console.log(`  ✅ Beacon upgraded. All pool proxies now use new implementation.`);

    // Update addresses file
    addresses[`${contractName}_impl`] = newImplAddr;
    addresses[`${contractName}_impl_prev`] = oldImpl;
  } else {
    // ─── UUPS Proxy Upgrade ───
    const proxyKey = `${contractName}_proxy`;
    const proxyAddr = addresses[proxyKey];
    if (!proxyAddr) throw new Error(`${proxyKey} not found in deployment file`);

    const proxy = Factory.attach(proxyAddr);
    const oldImplKey = `${contractName}_impl`;
    const oldImpl = addresses[oldImplKey];

    console.log(`  Proxy:    ${proxyAddr}`);
    console.log(`  Old impl: ${oldImpl}`);
    console.log(`  Upgrading...`);

    const tx = await proxy.upgradeToAndCall(newImplAddr, upgradePlan.migrationCalldata);
    await tx.wait();

    console.log(`  ✅ UUPS proxy upgraded.`);

    // Update addresses file
    addresses[oldImplKey] = newImplAddr;
    addresses[`${contractName}_impl_prev`] = oldImpl;
  }

  // Save updated addresses
  addresses._meta.lastUpgrade = {
    contract: contractName,
    newImpl: newImplAddr,
    timestamp: new Date().toISOString(),
    mode: isBeacon ? "beacon" : "uups",
  };

  require("./lib/operations").atomicWrite(addrPath, addresses);
  console.log(`  Addresses file updated.\n`);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error("❌ Upgrade failed:", error);
    process.exit(1);
  });
