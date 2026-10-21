/**
 * Sidiora Launchpad AMM — Set Creation Fee
 *
 * Lowers the pool creation fee to $1 USDL (1_000_000 = 1e6).
 *
 * Usage:
 *   npx hardhat run scripts/set-creation-fee.js --network paxeer-network
 *
 * Requires: PRIVATE_KEY in .env (must be DEFAULT_ADMIN_ROLE on ProtocolConfig)
 */

const { ethers } = require("hardhat");
const path = require("path");
const fs = require("fs");

async function main() {
  const addrPath = path.join(__dirname, "..", "deployments", require("./lib/operations").networkName() + "-addresses.json");
  if (!fs.existsSync(addrPath)) {
    throw new Error(`Addresses file not found: ${addrPath}\nRun deploy.js first.`);
  }

  const addresses = JSON.parse(fs.readFileSync(addrPath, "utf8"));
  await require("./lib/operations").validateManifest(addresses, ethers.provider);
  const configProxy = addresses.ProtocolConfig_proxy;

  if (!configProxy) {
    throw new Error("ProtocolConfig_proxy not found in addresses file");
  }

  const [deployer] = await ethers.getSigners();
  console.log(`\n🔧 Setting creation fee`);
  console.log(`   Signer:          ${deployer.address}`);
  console.log(`   ProtocolConfig:   ${configProxy}`);

  const config = await ethers.getContractAt("ProtocolConfig", configProxy);

  const currentFee = await config.creationFee();
  console.log(`   Current fee:      ${ethers.formatUnits(currentFee, 6)} USDL`);

  const newFee = 1_000_000n; // 1 USDL (6 decimals)
  if (currentFee === newFee) {
    console.log(`\n   ✅ Fee is already $1. No action needed.\n`);
    return;
  }

  console.log(`   New fee:          ${ethers.formatUnits(newFee, 6)} USDL`);

  const tx = await config.setCreationFee(newFee);
  console.log(`   Tx hash:          ${tx.hash}`);

  const receipt = await tx.wait();
  console.log(`   Block:            ${receipt.blockNumber}`);
  console.log(`   Gas used:         ${receipt.gasUsed.toString()}`);

  // Verify
  const updatedFee = await config.creationFee();
  console.log(`   Verified fee:     ${ethers.formatUnits(updatedFee, 6)} USDL`);
  console.log(`\n   ✅ Creation fee updated to $1 USDL\n`);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error("\n❌ Error:", error.message || error);
    process.exit(1);
  });
