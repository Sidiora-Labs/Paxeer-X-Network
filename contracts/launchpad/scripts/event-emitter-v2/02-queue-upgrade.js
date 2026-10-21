/**
 * Stage 2 / Step 02 — Queue the v2 upgrade through Timelock.
 *
 * Encodes the atomic upgrade-and-reinit calldata
 *
 *   EventEmitter.upgradeToAndCall(
 *     newImpl,
 *     EventEmitter.interface.encodeFunctionData("reinitializeV2", [adminWithEmitterRole])
 *   )
 *
 * and submits it via `Timelock.queueTransaction(target, value, data, eta)`
 * where `target = EventEmitter_proxy`, `value = 0`, and
 * `eta = block.timestamp + minDelay + buffer`.
 *
 * Run (as the Timelock proposer):
 *   ADMIN_WITH_EMITTER_ROLE=0x...                         \
 *   npx hardhat run scripts/event-emitter-v2/02-queue-upgrade.js \
 *     --network paxeer-network
 *
 * Required env:
 *   ADMIN_WITH_EMITTER_ROLE   address granted EVENT_EMITTER_ROLE in reinitializeV2.
 *
 * Optional env:
 *   BUFFER_SECONDS            extra seconds added to eta beyond minDelay (default 60)
 *   IMPL_ADDRESS              override the impl address from step 01
 *   PROXY_ADDRESS             override the EventEmitter proxy address
 *   TIMELOCK_ADDRESS          override the Timelock address
 */
const hre = require("hardhat");
const { ethers } = hre;
const fs = require("fs");
const path = require("path");

const DEPLOYMENTS_ROOT = path.join(__dirname, "..", "..", "deployments");

function networkDir(network) {
  return path.join(DEPLOYMENTS_ROOT, network);
}

function readJson(p) {
  return JSON.parse(fs.readFileSync(p, "utf8"));
}

function requireFile(p, hint) {
  if (!fs.existsSync(p)) {
    throw new Error(`Missing required file: ${p}\n  ${hint || ""}`);
  }
  return p;
}

function requireAddress(value, label) {
  if (!value || typeof value !== "string" || !ethers.isAddress(value)) {
    throw new Error(`Invalid or missing address for ${label}: ${value}`);
  }
  return ethers.getAddress(value);
}

function loadAddresses(network) {
  // Canonical mainnet file is `deployments/paxeer-network-addresses.json`.
  // For other networks fall back to `deployments/<network>-addresses.json`
  // or `deployments/<network>/addresses.json`.
  const candidates = [
    path.join(DEPLOYMENTS_ROOT, `${network}-addresses.json`),
    path.join(DEPLOYMENTS_ROOT, network, "addresses.json"),
  ];
  for (const p of candidates) {
    if (fs.existsSync(p)) return { path: p, data: readJson(p) };
  }
  throw new Error(
    `No addresses file found for network "${network}". Looked in:\n` +
    candidates.map((c) => `   ${c}`).join("\n")
  );
}

function loadImplRecord(network) {
  const p = path.join(networkDir(network), "EventEmitterV2Impl.json");
  requireFile(
    p,
    "Run scripts/event-emitter-v2/01-deploy-impl.js first (or set IMPL_ADDRESS env)."
  );
  return readJson(p);
}

async function main() {
  await require("../lib/upgrade-preflight").upgradePreflight(["EventEmitter"]);
  const network = hre.network.name;
  const [signer] = await ethers.getSigners();
  const chainId = Number((await ethers.provider.getNetwork()).chainId);

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log("  EventEmitter v2 — Step 02: Queue upgrade via Timelock");
  console.log("══════════════════════════════════════════════════════════════════");
  console.log(`  Network:  ${network} (chainId=${chainId})`);
  console.log(`  Signer:   ${signer.address}`);

  // ── Resolve addresses ───────────────────────────────────────────────
  const adminWithEmitterRole = requireAddress(
    process.env.ADMIN_WITH_EMITTER_ROLE,
    "ADMIN_WITH_EMITTER_ROLE (env)"
  );
  console.log(`  EmitterRoleHolder: ${adminWithEmitterRole}`);

  const newImpl = process.env.IMPL_ADDRESS
    ? requireAddress(process.env.IMPL_ADDRESS, "IMPL_ADDRESS (env)")
    : requireAddress(loadImplRecord(network).address, "EventEmitterV2Impl.json#address");
  console.log(`  NewImpl:           ${newImpl}`);

  const { path: addrPath, data: addresses } = loadAddresses(network);
  const proxy = process.env.PROXY_ADDRESS
    ? requireAddress(process.env.PROXY_ADDRESS, "PROXY_ADDRESS (env)")
    : requireAddress(addresses.EventEmitter_proxy, `${addrPath}#EventEmitter_proxy`);
  console.log(`  EventEmitter:      ${proxy}`);

  const timelock = process.env.TIMELOCK_ADDRESS
    ? requireAddress(process.env.TIMELOCK_ADDRESS, "TIMELOCK_ADDRESS (env)")
    : requireAddress(addresses.Timelock, `${addrPath}#Timelock`);
  console.log(`  Timelock:          ${timelock}`);

  // ── Encode calldata ────────────────────────────────────────────────
  const Factory = await ethers.getContractFactory("EventEmitter");
  const initCalldata = Factory.interface.encodeFunctionData("reinitializeV2", [
    adminWithEmitterRole,
  ]);
  const upgradeCalldata = Factory.interface.encodeFunctionData("upgradeToAndCall", [
    newImpl,
    initCalldata,
  ]);
  console.log(`\n  reinitializeV2 calldata (${(initCalldata.length - 2) / 2} bytes):`);
  console.log(`    ${initCalldata}`);
  console.log(`  upgradeToAndCall calldata (${(upgradeCalldata.length - 2) / 2} bytes):`);
  console.log(`    ${upgradeCalldata.slice(0, 100)}…`);

  // ── Compute eta ────────────────────────────────────────────────────
  const Timelock = await ethers.getContractAt("Timelock", timelock, signer);
  const minDelay = await Timelock.minDelay();
  const proposer = await Timelock.proposer();
  const guardian = await Timelock.guardian();
  console.log(`\n  Timelock minDelay: ${minDelay} seconds (${Number(minDelay) / 3600}h)`);
  console.log(`  Timelock proposer: ${proposer}`);
  console.log(`  Timelock guardian: ${guardian}`);

  if (proposer.toLowerCase() !== signer.address.toLowerCase()) {
    throw new Error(
      `Signer ${signer.address} is NOT the Timelock proposer (${proposer}).\n` +
      `  Switch PRIVATE_KEY or run from the proposer wallet.`
    );
  }

  const buffer = BigInt(process.env.BUFFER_SECONDS || "60");
  const block = await ethers.provider.getBlock("latest");
  const eta = BigInt(block.timestamp) + minDelay + buffer;
  console.log(`  Now:               ${block.timestamp} (${new Date(block.timestamp * 1000).toISOString()})`);
  console.log(`  Eta (queue):       ${eta} (${new Date(Number(eta) * 1000).toISOString()})`);
  console.log(`  Buffer:            ${buffer}s above minDelay`);

  // ── Compute Timelock txHash for record-keeping ─────────────────────
  const queuedTxHash = ethers.keccak256(
    ethers.AbiCoder.defaultAbiCoder().encode(
      ["address", "uint256", "bytes", "uint256"],
      [proxy, 0, upgradeCalldata, eta]
    )
  );
  console.log(`  Computed txHash:   ${queuedTxHash}`);

  // ── Submit ─────────────────────────────────────────────────────────
  console.log("\n  Submitting Timelock.queueTransaction…");
  const tx = await Timelock.queueTransaction(proxy, 0, upgradeCalldata, eta);
  console.log(`    tx: ${tx.hash}`);
  const receipt = await tx.wait();
  console.log(`    block: ${receipt.blockNumber}, gas: ${receipt.gasUsed.toString()}`);

  // Verify it landed in the mapping.
  const queued = await Timelock.queuedTransactions(queuedTxHash);
  if (!queued) {
    throw new Error(
      `Post-tx check failed: queuedTransactions[${queuedTxHash}] = false. ` +
      `The transaction reverted silently (impossible) or address mismatch.`
    );
  }
  console.log("  ✅ Confirmed queued in Timelock mapping");

  // ── Persist queue record ───────────────────────────────────────────
  const record = {
    network,
    chainId,
    queuedAt: new Date().toISOString(),
    queuedAtBlock: receipt.blockNumber,
    queueSubmissionTxHash: tx.hash,
    timelockTxHash: queuedTxHash,
    target: proxy,
    value: "0",
    data: upgradeCalldata,
    eta: eta.toString(),
    etaIso: new Date(Number(eta) * 1000).toISOString(),
    minDelay: minDelay.toString(),
    bufferSeconds: buffer.toString(),
    newImpl,
    adminWithEmitterRole,
    timelock,
    proposer: signer.address,
    decoded: {
      contract: "EventEmitter",
      method: "upgradeToAndCall",
      args: { newImpl, dataMethod: "reinitializeV2", reinitArgs: [adminWithEmitterRole] },
    },
  };
  fs.mkdirSync(networkDir(network), { recursive: true });
  const outPath = path.join(networkDir(network), "EventEmitterV2UpgradeQueued.json");
  fs.writeFileSync(outPath, JSON.stringify(record, null, 2) + "\n");
  console.log(`\n  Recorded: ${path.relative(path.join(__dirname, "..", ".."), outPath)}`);

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log(`  ✅ Step 02 complete.`);
  console.log(`  Wait until ${record.etaIso} then run:`);
  console.log(`     npx hardhat run scripts/event-emitter-v2/03-execute-upgrade.js --network ${network}`);
  console.log("══════════════════════════════════════════════════════════════════\n");
}

main()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error("❌ 02-queue-upgrade failed:", err);
    process.exit(1);
  });
