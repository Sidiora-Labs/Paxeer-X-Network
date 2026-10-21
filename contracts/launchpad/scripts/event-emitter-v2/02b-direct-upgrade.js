/**
 * Stage 2 / Step 02b — Direct deployer upgrade (no Timelock).
 *
 * Use this when the deployer wallet still holds `DEFAULT_ADMIN_ROLE` on the
 * EventEmitter proxy and the Timelock route is not yet active. Atomically:
 *
 *   proxy.upgradeToAndCall(newImpl,
 *     encodeCall(EventEmitter.reinitializeV2, [adminWithEmitterRole]))
 *
 * is broadcast in a single tx by the deployer, then post-state is asserted.
 *
 * Run (as the DEFAULT_ADMIN_ROLE holder, currently the deployer):
 *   ADMIN_WITH_EMITTER_ROLE=0x...                                  \
 *   npx hardhat run scripts/event-emitter-v2/02b-direct-upgrade.js \
 *     --network paxeer-network
 *
 * Required env:
 *   ADMIN_WITH_EMITTER_ROLE   address granted EVENT_EMITTER_ROLE by reinitializeV2.
 *
 * Optional env:
 *   IMPL_ADDRESS              override the impl from step 01.
 *   PROXY_ADDRESS             override the EventEmitter proxy address.
 *
 * On completion this script rotates impl pointers in
 *   deployments/paxeer-network-addresses.json
 * the same way 03-execute-upgrade.js does. The Timelock-route scripts (02 + 03)
 * remain available for the future when admin is transferred to Timelock.
 */
const hre = require("hardhat");
const { ethers } = hre;
const fs = require("fs");
const path = require("path");

const DEPLOYMENTS_ROOT = path.join(__dirname, "..", "..", "deployments");
const EVENT_EMITTER_ROLE = ethers.id("EVENT_EMITTER_ROLE");
const DEFAULT_ADMIN_ROLE = ethers.ZeroHash;

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

function loadImplRecord(network) {
  const p = path.join(DEPLOYMENTS_ROOT, network, "EventEmitterV2Impl.json");
  if (!fs.existsSync(p)) {
    throw new Error(
      `Missing ${p}. Run scripts/event-emitter-v2/01-deploy-impl.js first ` +
      `(or pass IMPL_ADDRESS env).`
    );
  }
  return readJson(p);
}

function requireAddress(value, label) {
  if (!value || !ethers.isAddress(value)) {
    throw new Error(`Invalid or missing address for ${label}: ${value}`);
  }
  return ethers.getAddress(value);
}

async function main() {
  await require("../lib/upgrade-preflight").upgradePreflight(["EventEmitter"]);
  const network = hre.network.name;
  const [signer] = await ethers.getSigners();
  const chainId = Number((await ethers.provider.getNetwork()).chainId);

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log("  EventEmitter v2 — Step 02b: Direct deployer upgrade (no Timelock)");
  console.log("══════════════════════════════════════════════════════════════════");
  console.log(`  Network:  ${network} (chainId=${chainId})`);
  console.log(`  Signer:   ${signer.address}`);

  // ── Resolve inputs ──────────────────────────────────────────────────
  const adminWithEmitterRole = requireAddress(
    process.env.ADMIN_WITH_EMITTER_ROLE,
    "ADMIN_WITH_EMITTER_ROLE (env)"
  );
  const newImpl = process.env.IMPL_ADDRESS
    ? requireAddress(process.env.IMPL_ADDRESS, "IMPL_ADDRESS (env)")
    : requireAddress(loadImplRecord(network).address, "EventEmitterV2Impl.json#address");

  const { path: addrPath, data: addresses } = loadAddresses(network);
  const proxyAddr = process.env.PROXY_ADDRESS
    ? requireAddress(process.env.PROXY_ADDRESS, "PROXY_ADDRESS (env)")
    : requireAddress(addresses.EventEmitter_proxy, `${addrPath}#EventEmitter_proxy`);

  console.log(`  EmitterRoleHolder: ${adminWithEmitterRole}`);
  console.log(`  NewImpl:           ${newImpl}`);
  console.log(`  EventEmitter:      ${proxyAddr}`);

  // ── Pre-flight: signer must hold DEFAULT_ADMIN_ROLE ─────────────────
  const proxy = await ethers.getContractAt("EventEmitter", proxyAddr, signer);
  const hasAdmin = await proxy.hasRole(DEFAULT_ADMIN_ROLE, signer.address);
  if (!hasAdmin) {
    throw new Error(
      `Signer ${signer.address} does NOT hold DEFAULT_ADMIN_ROLE on ${proxyAddr}.\n` +
      `  upgradeToAndCall is gated by _authorizeUpgrade -> onlyRole(DEFAULT_ADMIN_ROLE).`
    );
  }
  console.log(`  Role check:        DEFAULT_ADMIN_ROLE held by signer ✅`);

  // ── Pre-flight: capture v1 storage for diff ─────────────────────────
  const preRegistry = await proxy.poolRegistry();
  console.log(`  Pre poolRegistry:  ${preRegistry}`);

  // ── Encode calldata ────────────────────────────────────────────────
  const Factory = await ethers.getContractFactory("EventEmitter");
  const initCalldata = Factory.interface.encodeFunctionData("reinitializeV2", [
    adminWithEmitterRole,
  ]);
  console.log(`  reinitializeV2 calldata (${(initCalldata.length - 2) / 2} bytes):`);
  console.log(`    ${initCalldata}`);

  // ── Submit ─────────────────────────────────────────────────────────
  console.log("\n  Submitting proxy.upgradeToAndCall(newImpl, reinitializeV2)…");
  const tx = await proxy.upgradeToAndCall(newImpl, initCalldata);
  console.log(`    tx: ${tx.hash}`);
  const receipt = await tx.wait();
  console.log(`    block: ${receipt.blockNumber}, gas: ${receipt.gasUsed.toString()}`);

  // ── Post-assertions ────────────────────────────────────────────────
  console.log("\n  Asserting post-state…");
  const version = await proxy.VERSION();
  if (version !== "2.0.0") {
    throw new Error(`VERSION() returned "${version}" — expected "2.0.0"`);
  }
  console.log(`    ✅ VERSION() = ${version}`);

  if (adminWithEmitterRole !== ethers.ZeroAddress) {
    const hasRole = await proxy.hasRole(EVENT_EMITTER_ROLE, adminWithEmitterRole);
    if (!hasRole) {
      throw new Error(
        `EVENT_EMITTER_ROLE NOT granted to ${adminWithEmitterRole}.`
      );
    }
    console.log(`    ✅ hasRole(EVENT_EMITTER_ROLE, ${adminWithEmitterRole}) = true`);
  }

  const postRegistry = await proxy.poolRegistry();
  if (postRegistry !== preRegistry) {
    throw new Error(
      `Storage drift: poolRegistry changed from ${preRegistry} to ${postRegistry}!`
    );
  }
  console.log(`    ✅ poolRegistry preserved (${postRegistry})`);

  try {
    await proxy.reinitializeV2.staticCall(adminWithEmitterRole);
    throw new Error("reinitializeV2 should have reverted on second call but didn't");
  } catch (err) {
    if (err.message.includes("should have reverted")) throw err;
    console.log(`    ✅ reinitializeV2 reverts on second call`);
  }

  // ── Rotate impl pointers in addresses file ─────────────────────────
  const oldImpl = addresses.EventEmitter_impl;
  addresses.EventEmitter_impl_prev = oldImpl;
  addresses.EventEmitter_impl = newImpl;
  if (!addresses._meta) addresses._meta = {};
  addresses._meta.lastUpgrade = {
    contract: "EventEmitter",
    newImpl,
    timestamp: new Date().toISOString(),
    mode: "uups-direct-deployer",
    executionTxHash: tx.hash,
  };
  fs.writeFileSync(addrPath, JSON.stringify(addresses, null, 2) + "\n");
  console.log(`\n  Updated: ${path.relative(path.join(__dirname, "..", ".."), addrPath)}`);
  console.log(`    EventEmitter_impl_prev: ${oldImpl}`);
  console.log(`    EventEmitter_impl:      ${newImpl}`);

  // ── Save direct-upgrade receipt ─────────────────────────────────────
  const receiptPath = path.join(
    DEPLOYMENTS_ROOT,
    network,
    "EventEmitterV2DirectUpgrade.json"
  );
  fs.writeFileSync(
    receiptPath,
    JSON.stringify(
      {
        network,
        chainId,
        executedAt: new Date().toISOString(),
        executedAtBlock: receipt.blockNumber,
        executionTxHash: tx.hash,
        target: proxyAddr,
        oldImpl,
        newImpl,
        adminWithEmitterRole,
        signer: signer.address,
        mode: "uups-direct-deployer",
      },
      null,
      2
    ) + "\n"
  );
  console.log(`  Receipt: ${path.relative(path.join(__dirname, "..", ".."), receiptPath)}`);

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log("  ✅ Step 02b complete. Proxy now points at v2 impl.");
  console.log("  Next: scripts/event-emitter-v2/04-wire-registries.js");
  console.log("══════════════════════════════════════════════════════════════════\n");
}

main()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error("❌ 02b-direct-upgrade failed:", err);
    process.exit(1);
  });
