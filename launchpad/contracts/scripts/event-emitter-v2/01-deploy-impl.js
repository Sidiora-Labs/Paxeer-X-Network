/**
 * Stage 2 / Step 01 — Deploy EventEmitter v2 implementation.
 *
 * Deploys a fresh EventEmitter (v2) impl contract. NO proxy mutation here;
 * the impl is just compiled-and-deployed bytecode. The Timelock-routed
 * upgrade is queued in step 02 and executed in step 03.
 *
 * Idempotent: if `deployments/<network>/EventEmitterV2Impl.json` already
 * exists AND its `bytecodeHash` matches the freshly compiled artifact,
 * the script reuses the recorded address and skips deploy.
 *
 * Run:
 *   npx hardhat run scripts/event-emitter-v2/01-deploy-impl.js --network paxeer-network
 *   npx hardhat run scripts/event-emitter-v2/01-deploy-impl.js --network localhost
 *
 * Optional env:
 *   FORCE_REDEPLOY=1   — ignore existing record and deploy fresh impl.
 *   SKIP_VERIFY=1      — skip the explorer-verify attempt (default tries).
 */
const hre = require("hardhat");
const { ethers } = hre;
const fs = require("fs");
const path = require("path");

const DEPLOYMENTS_ROOT = path.join(__dirname, "..", "..", "deployments");

function networkDir(network) {
  return path.join(DEPLOYMENTS_ROOT, network);
}

function recordPath(network) {
  return path.join(networkDir(network), "EventEmitterV2Impl.json");
}

function bytecodeHash(bytecode) {
  return ethers.keccak256(bytecode);
}

async function loadExisting(network) {
  const p = recordPath(network);
  if (!fs.existsSync(p)) return null;
  try {
    return JSON.parse(fs.readFileSync(p, "utf8"));
  } catch (err) {
    console.warn(`  ⚠ Could not parse existing record at ${p}: ${err.message}`);
    return null;
  }
}

async function tryVerify(address) {
  if (process.env.SKIP_VERIFY === "1") {
    console.log("  ⏭  SKIP_VERIFY=1 — explorer verification skipped");
    return false;
  }
  if (hre.network.name === "localhost" || hre.network.name === "hardhat") {
    console.log("  ⏭  Local network — verification skipped");
    return false;
  }
  try {
    await hre.run("verify:verify", {
      address,
      constructorArguments: [],
      contract: "contracts/data/EventEmitter.sol:EventEmitter",
    });
    console.log("  ✅ Verified on explorer");
    return true;
  } catch (err) {
    const msg = (err && err.message) || String(err);
    if (msg.toLowerCase().includes("already verified")) {
      console.log("  ✅ Already verified");
      return true;
    }
    console.warn(`  ⚠ Verification failed (non-fatal): ${msg.split("\n")[0]}`);
    return false;
  }
}

async function main() {
  await require("../lib/upgrade-preflight").upgradePreflight(["EventEmitter"]);
  const network = hre.network.name;
  const [deployer] = await ethers.getSigners();
  const chainId = Number((await ethers.provider.getNetwork()).chainId);

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log("  EventEmitter v2 — Step 01: Deploy implementation");
  console.log("══════════════════════════════════════════════════════════════════");
  console.log(`  Network:    ${network} (chainId=${chainId})`);
  console.log(`  Deployer:   ${deployer.address}`);

  await hre.run("compile", { quiet: true });

  const Factory = await ethers.getContractFactory("EventEmitter");
  const expectedHash = bytecodeHash(Factory.bytecode);
  console.log(`  ImplHash:   ${expectedHash}`);

  // ── Idempotency check ───────────────────────────────────────────────
  const existing = await loadExisting(network);
  if (existing && existing.bytecodeHash === expectedHash && process.env.FORCE_REDEPLOY !== "1") {
    const code = await ethers.provider.getCode(existing.address);
    const artifact = await hre.artifacts.readArtifact("EventEmitter");
    if (existing.chainId !== chainId) throw new Error("Implementation record chain mismatch");
    if (ethers.keccak256(code) !== ethers.keccak256(artifact.deployedBytecode)) throw new Error("Implementation runtime differs from artifact");
    const existingImpl = Factory.attach(existing.address);
    if (await existingImpl.VERSION() !== "2.0.0" || await existingImpl.proxiableUUID() !== "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc") throw new Error("Implementation identity probe failed");
    if (code && code !== "0x") {
      console.log("\n  ✅ Existing v2 impl matches current bytecode — reusing.");
      console.log(`     Address: ${existing.address}`);
      console.log(`     Block:   ${existing.blockNumber}`);
      console.log(`     Tx:      ${existing.txHash}`);
      console.log("\n  Pass FORCE_REDEPLOY=1 to deploy a fresh copy.");
      return;
    }
    console.log("  ⚠ Existing record found but no bytecode at address — redeploying.");
  }

  // ── Deploy ──────────────────────────────────────────────────────────
  console.log("\n  Deploying EventEmitter (v2 impl)…");
  const impl = await Factory.deploy();
  const deployTx = impl.deploymentTransaction();
  console.log(`    tx:  ${deployTx.hash}`);
  const receipt = await impl.waitForDeployment();
  // ethers v6 returns the contract instance, not a receipt — fetch the receipt manually.
  const txReceipt = await ethers.provider.getTransactionReceipt(deployTx.hash);
  const address = await impl.getAddress();
    await require("../lib/operations").checkpointDeployment(address, "impl");
  console.log(`    addr:  ${address}`);
  console.log(`    block: ${txReceipt.blockNumber}`);
  console.log(`    gas:   ${txReceipt.gasUsed.toString()}`);

  // ── Smoke probe: VERSION() must return "2.0.0" on the bare impl too ──
  const versionFromImpl = await impl.VERSION();
  if (versionFromImpl !== "2.0.0") {
    throw new Error(`Sanity check failed: impl.VERSION() returned "${versionFromImpl}" (expected "2.0.0")`);
  }
  console.log(`    VERSION(): ${versionFromImpl}`);

  // ── Persist ─────────────────────────────────────────────────────────
  fs.mkdirSync(networkDir(network), { recursive: true });
  const artifact = await hre.artifacts.readArtifact("EventEmitter");
  const buildInfo = await hre.artifacts.getBuildInfo(`${artifact.sourceName}:${artifact.contractName}`);
  if (!buildInfo) throw new Error("Missing implementation compiler build identity");
  const record = {
    network,
    chainId,
    contract: "EventEmitter",
    version: "2.0.0",
    address,
    deployer: deployer.address,
    txHash: deployTx.hash,
    blockNumber: txReceipt.blockNumber,
    gasUsed: txReceipt.gasUsed.toString(),
    deployedAt: new Date().toISOString(),
    bytecodeHash: expectedHash,
    compiler: buildInfo.solcLongVersion,
    viaIR: buildInfo.input.settings.viaIR === true,
    optimizer: buildInfo.input.settings.optimizer,
    runtimeHash: ethers.keccak256(await ethers.provider.getCode(address)),
    verified: false,
  };
  require("../lib/operations").atomicWrite(recordPath(network), record);
  console.log(`\n  Recorded: ${path.relative(path.join(__dirname, "..", ".."), recordPath(network))}`);

  // ── Verify (best-effort) ────────────────────────────────────────────
  console.log("\n  Verifying on explorer…");
  const ok = await tryVerify(address);
  if (ok) {
    record.verified = true;
    require("../lib/operations").atomicWrite(recordPath(network), record);
  }

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log("  ✅ Step 01 complete. Next: scripts/event-emitter-v2/02-queue-upgrade.js");
  console.log("══════════════════════════════════════════════════════════════════\n");
}

main()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error("❌ 01-deploy-impl failed:", err);
    process.exit(1);
  });
