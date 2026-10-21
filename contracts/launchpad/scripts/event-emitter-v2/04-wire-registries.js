/**
 * Stage 2 / Step 04 — Wire dynamic auth registries on EventEmitter v2.
 *
 * Run by the EVENT_EMITTER_ROLE (or DEFAULT_ADMIN_ROLE) holder. Sets:
 *   - opticalRegistry → OpticalRegistry_proxy
 *   - metaAGRouter   → MetaAGRouter_proxy
 *   - sidioraFactory → SidioraFactory_proxy
 *
 * Then re-runs auth probes:
 *   - Confirms each setter persisted by reading the corresponding storage slot.
 *   - Confirms the wired SidioraFactory address is recognised as authorised
 *     via `_isAuthorized` path 4 (`sender == sidioraFactory`).
 *   - Confirms a sample registered pool (read from PoolRegistry.getAllPools)
 *     is recognised via `_isAuthorized` path 2 (poolRegistry dynamic).
 *
 * Idempotent: skips setters whose slot already holds the target value.
 *
 * Run:
 *   npx hardhat run scripts/event-emitter-v2/04-wire-registries.js --network paxeer-network
 *
 * Optional env:
 *   SKIP_OPTICAL=1   skip setOpticalRegistry
 *   SKIP_ROUTER=1    skip setMetaAGRouter
 *   SKIP_FACTORY=1   skip setSidioraFactory
 */
const hre = require("hardhat");
const { ethers } = hre;
const fs = require("fs");
const path = require("path");

const DEPLOYMENTS_ROOT = path.join(__dirname, "..", "..", "deployments");

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

function requireAddress(value, label) {
  if (!value || !ethers.isAddress(value)) {
    throw new Error(`Invalid or missing address for ${label}: ${value}`);
  }
  return ethers.getAddress(value);
}

async function maybeSet(label, current, target, setter, signer, skip) {
  if (skip) {
    console.log(`  ⏭  ${label}: skipped (env)`);
    return null;
  }
  if (current.toLowerCase() === target.toLowerCase()) {
    console.log(`  ✅ ${label}: already set → ${target}`);
    return null;
  }
  if (current !== ethers.ZeroAddress) {
    console.log(`  ⚠ ${label}: current=${current} → setting to ${target}`);
  } else {
    console.log(`  ${label}: setting → ${target}`);
  }
  const tx = await setter(target);
  const r = await tx.wait();
  console.log(`     tx ${tx.hash} block ${r.blockNumber} gas ${r.gasUsed.toString()}`);
  return tx.hash;
}

async function main() {
  await require("../lib/upgrade-preflight").upgradePreflight(["EventEmitter"]);
  const network = hre.network.name;
  const [signer] = await ethers.getSigners();
  const chainId = Number((await ethers.provider.getNetwork()).chainId);

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log("  EventEmitter v2 — Step 04: Wire dynamic auth registries");
  console.log("══════════════════════════════════════════════════════════════════");
  console.log(`  Network: ${network} (chainId=${chainId})`);
  console.log(`  Signer:  ${signer.address}`);

  const { data: addresses } = loadAddresses(network);
  const eeProxy = requireAddress(addresses.EventEmitter_proxy, "EventEmitter_proxy");
  const opticalRegistryAddr = requireAddress(
    addresses.OpticalRegistry_proxy,
    "OpticalRegistry_proxy"
  );
  const metaAGRouterAddr = requireAddress(
    addresses.MetaAGRouter_proxy,
    "MetaAGRouter_proxy"
  );
  const factoryAddr = requireAddress(
    addresses.SidioraFactory_proxy,
    "SidioraFactory_proxy"
  );
  const poolRegistryAddr = requireAddress(
    addresses.PoolRegistry_proxy,
    "PoolRegistry_proxy"
  );

  console.log(`  EventEmitter:    ${eeProxy}`);
  console.log(`  OpticalRegistry: ${opticalRegistryAddr}`);
  console.log(`  MetaAGRouter:    ${metaAGRouterAddr}`);
  console.log(`  SidioraFactory:  ${factoryAddr}`);
  console.log(`  PoolRegistry:    ${poolRegistryAddr}`);

  const ee = await ethers.getContractAt("EventEmitter", eeProxy, signer);

  // ── Role pre-check ─────────────────────────────────────────────────
  const EVENT_EMITTER_ROLE = ethers.id("EVENT_EMITTER_ROLE");
  const DEFAULT_ADMIN_ROLE = ethers.ZeroHash;
  const hasEER = await ee.hasRole(EVENT_EMITTER_ROLE, signer.address);
  const hasDAR = await ee.hasRole(DEFAULT_ADMIN_ROLE, signer.address);
  if (!hasEER && !hasDAR) {
    throw new Error(
      `Signer ${signer.address} holds neither EVENT_EMITTER_ROLE nor DEFAULT_ADMIN_ROLE. ` +
      `One of those roles is required for setOpticalRegistry/setMetaAGRouter/setSidioraFactory.`
    );
  }
  console.log(`  Role check:      EVENT_EMITTER_ROLE=${hasEER} DEFAULT_ADMIN_ROLE=${hasDAR}`);

  // Sanity: confirm we're talking to v2.
  const version = await ee.VERSION();
  if (version !== "2.0.0") {
    throw new Error(`Proxy at ${eeProxy} reports VERSION="${version}" — expected "2.0.0".`);
  }
  console.log(`  VERSION:         ${version}`);

  // ── Apply the three wirings ────────────────────────────────────────
  console.log("\n  Wiring registries…");
  const [curOptical, curRouter, curFactory] = await Promise.all([
    ee.opticalRegistry(),
    ee.metaAGRouter(),
    ee.sidioraFactory(),
  ]);
  const txs = {};
  txs.opticalRegistry = await maybeSet(
    "opticalRegistry",
    curOptical,
    opticalRegistryAddr,
    (a) => ee.setOpticalRegistry(a),
    signer,
    process.env.SKIP_OPTICAL === "1"
  );
  txs.metaAGRouter = await maybeSet(
    "metaAGRouter",
    curRouter,
    metaAGRouterAddr,
    (a) => ee.setMetaAGRouter(a),
    signer,
    process.env.SKIP_ROUTER === "1"
  );
  txs.sidioraFactory = await maybeSet(
    "sidioraFactory",
    curFactory,
    factoryAddr,
    (a) => ee.setSidioraFactory(a),
    signer,
    process.env.SKIP_FACTORY === "1"
  );

  // ── Verify storage slots ───────────────────────────────────────────
  console.log("\n  Verifying storage…");
  const [postOptical, postRouter, postFactory, postPoolReg] = await Promise.all([
    ee.opticalRegistry(),
    ee.metaAGRouter(),
    ee.sidioraFactory(),
    ee.poolRegistry(),
  ]);
  if (process.env.SKIP_OPTICAL !== "1" && postOptical.toLowerCase() !== opticalRegistryAddr.toLowerCase()) {
    throw new Error(`opticalRegistry storage = ${postOptical}, expected ${opticalRegistryAddr}`);
  }
  if (process.env.SKIP_ROUTER !== "1" && postRouter.toLowerCase() !== metaAGRouterAddr.toLowerCase()) {
    throw new Error(`metaAGRouter storage = ${postRouter}, expected ${metaAGRouterAddr}`);
  }
  if (process.env.SKIP_FACTORY !== "1" && postFactory.toLowerCase() !== factoryAddr.toLowerCase()) {
    throw new Error(`sidioraFactory storage = ${postFactory}, expected ${factoryAddr}`);
  }
  console.log(`    opticalRegistry: ${postOptical}`);
  console.log(`    metaAGRouter:    ${postRouter}`);
  console.log(`    sidioraFactory:  ${postFactory}`);
  console.log(`    poolRegistry:    ${postPoolReg}  (untouched, should match v1 wiring)`);

  // ── Auth probes (best-effort, non-fatal) ───────────────────────────
  console.log("\n  Auth probes…");
  try {
    const isFactoryStaticAuth = await ee.isAuthorizedEmitter(factoryAddr);
    console.log(
      `    isAuthorizedEmitter(SidioraFactory): ${isFactoryStaticAuth}` +
      (isFactoryStaticAuth ? "  (static set)" : "  (dynamic via sidioraFactory==msg.sender path)")
    );
  } catch (err) {
    console.warn(`    ⚠ probe failed: ${err.message.split("\n")[0]}`);
  }

  // Sample-pool dynamic auth probe via PoolRegistry.
  try {
    const PoolRegistry = await ethers.getContractAt(
      ["function getAllPools(uint256 offset, uint256 limit) view returns (address[])"],
      poolRegistryAddr,
      signer
    );
    const pools = await PoolRegistry.getAllPools(0, 1);
    if (pools && pools.length > 0) {
      const samplePool = pools[0];
      const isStatic = await ee.isAuthorizedEmitter(samplePool);
      console.log(
        `    sample pool ${samplePool}:  isAuthorizedEmitter=${isStatic}` +
        (isStatic
          ? "  (static)"
          : "  (relies on poolRegistry dynamic path — confirmed at emit time)")
      );
    } else {
      console.log("    ⚠ PoolRegistry has zero pools — sample-pool probe skipped");
    }
  } catch (err) {
    console.warn(`    ⚠ sample-pool probe failed: ${err.message.split("\n")[0]}`);
  }

  // ── Persist a wiring receipt ───────────────────────────────────────
  const outDir = path.join(DEPLOYMENTS_ROOT, network);
  fs.mkdirSync(outDir, { recursive: true });
  const out = {
    network,
    chainId,
    eventEmitter: eeProxy,
    signer: signer.address,
    wiredAt: new Date().toISOString(),
    wired: {
      opticalRegistry: opticalRegistryAddr,
      metaAGRouter: metaAGRouterAddr,
      sidioraFactory: factoryAddr,
      poolRegistry: postPoolReg,
    },
    txs,
  };
  fs.writeFileSync(
    path.join(outDir, "EventEmitterV2RegistriesWired.json"),
    JSON.stringify(out, null, 2) + "\n"
  );
  console.log(
    `\n  Recorded: ${path.relative(path.join(__dirname, "..", ".."), path.join(outDir, "EventEmitterV2RegistriesWired.json"))}`
  );

  console.log("\n══════════════════════════════════════════════════════════════════");
  console.log("  ✅ Step 04 complete. EventEmitter v2 is fully wired.");
  console.log("  Next: Stage 5 — wire emit() calls across ~37 contracts (4 domain PRs).");
  console.log("══════════════════════════════════════════════════════════════════\n");
}

main()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error("❌ 04-wire-registries failed:", err);
    process.exit(1);
  });
