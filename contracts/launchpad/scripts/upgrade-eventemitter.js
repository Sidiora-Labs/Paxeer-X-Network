/**
 * Upgrade EventEmitter proxy to new impl (with poolRegistry auto-auth),
 * then call setPoolRegistry.
 *
 * Run: npx hardhat run scripts/upgrade-eventemitter.js --network paxeer-network
 */
const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

async function main() {
  await require("./lib/upgrade-preflight").upgradePreflight(["EventEmitter"]);
  const [deployer] = await ethers.getSigners();
  const addrPath = path.join(__dirname, "..", "deployments", require("./lib/operations").networkName() + "-addresses.json");
  const addresses = JSON.parse(fs.readFileSync(addrPath, "utf8"));
  await require("./lib/operations").validateManifest(addresses, ethers.provider);

  console.log("\n🔄 Upgrading EventEmitter on Paxeer\n");

  // 1. Deploy new implementation
  console.log("  Deploying new EventEmitter impl...");
  const Factory = await ethers.getContractFactory("EventEmitter");
  const newImpl = await Factory.deploy();
  await newImpl.waitForDeployment();
  const newImplAddr = await newImpl.getAddress();
    await require("./lib/operations").checkpointDeployment(newImplAddr, "newImpl");
  console.log(`  New impl: ${newImplAddr}`);
  console.log(`  Old impl: ${addresses.EventEmitter_impl}`);

  // 2. Upgrade proxy
  const proxy = Factory.attach(addresses.EventEmitter_proxy);
  console.log(`  Upgrading proxy ${addresses.EventEmitter_proxy}...`);
  const upgradeTx = await proxy.upgradeToAndCall(newImplAddr, "0x");
  await upgradeTx.wait();
  console.log("  ✅ Proxy upgraded");

  // 3. Set PoolRegistry
  console.log(`  Setting PoolRegistry: ${addresses.PoolRegistry_proxy}`);
  const setTx = await proxy.setPoolRegistry(addresses.PoolRegistry_proxy);
  await setTx.wait();
  console.log("  ✅ PoolRegistry set");

  // 4. Verify
  const regAddr = await proxy.poolRegistry();
  console.log(`  Verify poolRegistry: ${regAddr}`);

  // 5. Update addresses file
  addresses.EventEmitter_impl_prev = addresses.EventEmitter_impl;
  addresses.EventEmitter_impl = newImplAddr;
  require("./lib/operations").atomicWrite(addrPath, addresses);
  console.log("  Addresses file updated\n");
  console.log("✅ EventEmitter upgrade complete. Registered pools can now emit events.\n");
}

main().then(() => process.exit(0)).catch(e => { console.error("❌", e); process.exit(1); });
