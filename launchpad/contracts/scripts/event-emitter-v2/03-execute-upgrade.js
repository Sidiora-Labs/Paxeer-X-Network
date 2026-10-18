/**
 * Stage 2 / Step 03 — Execute the queued Timelock upgrade.
 *
 * Reads `deployments/<network>/EventEmitterV2UpgradeQueued.json`, calls
 * `Timelock.executeTransaction(target, value, data, eta)`, and asserts the
 * post-state:
 *   - VERSION() == "2.0.0"
 *   - hasRole(EVENT_EMITTER_ROLE, adminWithEmitterRole) == true
 *   - reinitializeV2 is no longer callable (idempotency)
 *
 * Then rotates the impl pointers in `deployments/paxeer-network-addresses.json`:
 *   - EventEmitter_impl_prev = (was) EventEmitter_impl
 *   - EventEmitter_impl       = newImpl
 *   - _meta.lastUpgrade       = { contract, newImpl, timestamp, mode: "uups-via-timelock" }
 *
 * Run (anyone can call executeTransaction once eta has passed):
 *   npx hardhat run scripts/event-emitter-v2/03-execute-upgrade.js --network paxeer-network
 *
 * Optional env:
 *   ALLOW_EARLY=1       skip the "wait for eta" guard (Timelock will still
 *                       revert if the chain is below eta, but useful for dry-runs).
 */
const hre = require("hardhat");
const { ethers } = hre;
const fs = require("fs");
const path = require("path");

const DEPLOYMENTS_ROOT = path.join(__dirname, "..", "..", "deployments");
const EVENT_EMITTER_ROLE = ethers.id("EVENT_EMITTER_ROLE");

function networkDir(network) {
  return path.join(DEPLOYMENTS_ROOT, network);
}

function readJson(p) {
  return JSON.parse(fs.readFileSync(p, "utf8"));
}

function loadAddresses(network) {
  const candidates = [
    path.join(DEPLOYMENTS_ROOT, `${network}-addresses.json`),
    path.join(DEPLOYMENTS_ROOT, network, "addresses.json"),
  ];
  for (const p of candidates) {
    if (fs.existsSync(p)) return { path: p, data: readJson(p) };
  }
  throw new Error(`No addresses file found for network "${network}"`);
}

async function main() {
  await require("../lib/upgrade-preflight").upgradePreflight(["EventEmitter"]);
  const network = hre.network.name;
  const [signer] = await ethers.getSigners();
  const chainId = Number((await ethers.provider.getNetwork()).chainId);

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log("  EventEmitter v2 — Step 03: Execute queued upgrade");
  console.log("══════════════════════════════════════════════════════════════════");
  console.log(`  Network:  ${network} (chainId=${chainId})`);
  console.log(`  Signer:   ${signer.address}`);

  // ── Load queued record ─────────────────────────────────────────────
  const queuedPath = path.join(networkDir(network), "EventEmitterV2UpgradeQueued.json");
  if (!fs.existsSync(queuedPath)) {
    throw new Error(
      `No queued record found at ${queuedPath}.\n` +
      `Run scripts/event-emitter-v2/02-queue-upgrade.js first.`
    );
  }
  const queued = readJson(queuedPath);
  console.log(`  Queued tx: ${queued.timelockTxHash}`);
  console.log(`  Target:    ${queued.target}`);
  console.log(`  NewImpl:   ${queued.newImpl}`);
  console.log(`  Eta:       ${queued.eta} (${queued.etaIso})`);

  // ── Eta gate ───────────────────────────────────────────────────────
  const block = await ethers.provider.getBlock("latest");
  const remaining = Number(BigInt(queued.eta) - BigInt(block.timestamp));
  if (remaining > 0 && process.env.ALLOW_EARLY !== "1") {
    throw new Error(
      `Eta not reached. Wait ${remaining}s (${(remaining / 3600).toFixed(2)}h) ` +
      `or pass ALLOW_EARLY=1 to attempt anyway (will revert if too early).`
    );
  }
  console.log(`  Eta status: ${remaining <= 0 ? "✅ reached" : `⚠ ${remaining}s remaining (ALLOW_EARLY=1)`}`);

  // ── Confirm still queued ───────────────────────────────────────────
  const Timelock = await ethers.getContractAt("Timelock", queued.timelock, signer);
  const isQueued = await Timelock.queuedTransactions(queued.timelockTxHash);
  if (!isQueued) {
    throw new Error(
      `Tx ${queued.timelockTxHash} is NOT in the Timelock queue.\n` +
      `It may have been executed already, cancelled by the guardian, or never queued.`
    );
  }

  // ── Snapshot pre-state for diff ────────────────────────────────────
  const proxyAddr = queued.target;
  const proxyV1 = await ethers.getContractAt(
    [
      "function poolRegistry() view returns (address)",
      "function isAuthorizedEmitter(address) view returns (bool)",
    ],
    proxyAddr,
    signer
  );
  const preRegistry = await proxyV1.poolRegistry();
  console.log(`  Pre poolRegistry:    ${preRegistry}`);

  // ── Execute ────────────────────────────────────────────────────────
  console.log("\n  Submitting Timelock.executeTransaction…");
  const tx = await Timelock.executeTransaction(
    queued.target,
    queued.value,
    queued.data,
    queued.eta
  );
  console.log(`    tx: ${tx.hash}`);
  const receipt = await tx.wait();
  console.log(`    block: ${receipt.blockNumber}, gas: ${receipt.gasUsed.toString()}`);

  // ── Post-assertions ────────────────────────────────────────────────
  console.log("\n  Asserting post-state…");
  const proxyV2 = await ethers.getContractAt("EventEmitter", proxyAddr, signer);
  const version = await proxyV2.VERSION();
  if (version !== "2.0.0") {
    throw new Error(`VERSION() returned "${version}" — expected "2.0.0"`);
  }
  console.log(`    ✅ VERSION() = ${version}`);

  const hasRole = await proxyV2.hasRole(EVENT_EMITTER_ROLE, queued.adminWithEmitterRole);
  if (!hasRole) {
    throw new Error(
      `EVENT_EMITTER_ROLE NOT granted to ${queued.adminWithEmitterRole}. ` +
      `reinitializeV2 either reverted silently or was passed address(0).`
    );
  }
  console.log(`    ✅ hasRole(EVENT_EMITTER_ROLE, ${queued.adminWithEmitterRole}) = true`);

  // Storage preservation: poolRegistry value must survive impl swap.
  const postRegistry = await proxyV2.poolRegistry();
  if (postRegistry !== preRegistry) {
    throw new Error(
      `Storage drift: poolRegistry changed from ${preRegistry} to ${postRegistry}!`
    );
  }
  console.log(`    ✅ poolRegistry preserved across impl swap (${postRegistry})`);

  // Idempotency: reinitializeV2 must revert.
  try {
    await proxyV2
      .connect(signer)
      .reinitializeV2.staticCall(queued.adminWithEmitterRole);
    throw new Error("reinitializeV2 should have reverted (already initialized) but didn't");
  } catch (err) {
    if (err.message.includes("should have reverted")) throw err;
    console.log(`    ✅ reinitializeV2 reverts on second call (${err.shortMessage || err.message.split("\n")[0]})`);
  }

  // ── Rotate impl pointers in addresses file ─────────────────────────
  const { path: addrPath, data: addresses } = loadAddresses(network);
  const oldImpl = addresses.EventEmitter_impl;
  addresses.EventEmitter_impl_prev = oldImpl;
  addresses.EventEmitter_impl = queued.newImpl;
  if (!addresses._meta) addresses._meta = {};
  addresses._meta.lastUpgrade = {
    contract: "EventEmitter",
    newImpl: queued.newImpl,
    timestamp: new Date().toISOString(),
    mode: "uups-via-timelock",
    timelockTxHash: queued.timelockTxHash,
    executionTxHash: tx.hash,
  };
  fs.writeFileSync(addrPath, JSON.stringify(addresses, null, 2) + "\n");
  console.log(`\n  Updated: ${path.relative(path.join(__dirname, "..", ".."), addrPath)}`);
  console.log(`    EventEmitter_impl_prev: ${oldImpl}`);
  console.log(`    EventEmitter_impl:      ${queued.newImpl}`);

  // ── Mark queue record as executed ──────────────────────────────────
  queued.executedAt = new Date().toISOString();
  queued.executedAtBlock = receipt.blockNumber;
  queued.executionTxHash = tx.hash;
  queued.status = "executed";
  fs.writeFileSync(queuedPath, JSON.stringify(queued, null, 2) + "\n");

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log("  ✅ Step 03 complete. Proxy now points at v2 impl.");
  console.log(`  Next: scripts/event-emitter-v2/04-wire-registries.js (run as EVENT_EMITTER_ROLE holder)`);
  console.log("══════════════════════════════════════════════════════════════════\n");
}

main()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error("❌ 03-execute-upgrade failed:", err);
    process.exit(1);
  });
