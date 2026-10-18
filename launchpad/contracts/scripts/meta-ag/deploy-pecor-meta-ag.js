/**
 * Sidiora Meta-AG + PECOR — Layered Deployment Script
 *
 * Deploys the 12 new PECOR + Meta-AG contracts ON TOP of an already-deployed
 * Sidiora launchpad (USDL, Router, Quoter, PoolRegistry, Timelock). Reads the
 * Sidiora addresses from `deployments/{NETWORK}-addresses.json` and merges the
 * new addresses back into the same file.
 *
 * Contract order (mirrors spec §11 build order):
 *   1.  PriceOracle          (UUPS)
 *   2.  OracleHub            (UUPS)
 *   3.  TransactionTracker   (UUPS)
 *   4.  PECORVault           (UUPS)
 *   5.  PECOR                (UUPS)
 *   6.  PECOROrders          (UUPS)
 *   7.  PriceOracleAdapter   (immutable)
 *   8.  SidioraFeedAdapter   (immutable)
 *   9.  VaultAdapter         (immutable)
 *  10.  SidioraAdapter       (immutable)
 *  11.  MetaAGRouter         (UUPS)
 *  12.  MetaAGQuoter         (UUPS)
 *
 * Plus (for local testing only):
 *   - MockWETH9               (WPAX rail for the vault)
 *
 * Wiring performed:
 *   - PriceOracle.RELAYER_ROLE            → RELAYER_ADDRESS (default: deployer)
 *   - PECORVault.OPERATOR_ROLE            → PECOR + PECOROrders + VaultAdapter
 *   - TransactionTracker.EMITTER_ROLE     → PECOR + PECOROrders + MetaAGRouter
 *   - PECOROrders.KEEPER_ROLE             → KEEPER_ADDRESS (default: deployer)
 *   - OracleHub                           → PriceOracleAdapter (priority 10) + SidioraFeedAdapter (priority 20)
 *   - MetaAGRouter                        → VaultAdapter + SidioraAdapter registered
 *   - PECORVault token registry           → USDL (stable), WPAX (non-stable)
 *   - PriceOracle token registry          → USDL + WPAX
 *
 * Optional (controlled by env flags, default ON for localhost):
 *   - SEED_PRICES=true                    → push canonical USD prices for USDL/WPAX
 *   - SEED_VAULT_LIQUIDITY=false          → seed vault reserves from deployer balance
 *
 * Usage:
 *   npx hardhat run scripts/meta-ag/deploy-pecor-meta-ag.js --network localhost
 *   npx hardhat run scripts/meta-ag/deploy-pecor-meta-ag.js --network paxeer-network
 *
 * Environment overrides:
 *   NETWORK_TYPE         = localhost | paxeer-network   (default: localhost)
 *   ADMIN_ADDRESS        = <timelock or EOA>             (default: Timelock from file, else deployer)
 *   RELAYER_ADDRESS      = <EOA or keeper bot>           (default: deployer)
 *   KEEPER_ADDRESS       = <EOA>                         (default: deployer)
 *   INCIDENT_GUARDIAN_ADDRESS = <pause-only incident operator> (default: validated core guardian; else Timelock)
 *   FEE_COLLECTOR        = <EOA or treasury>             (default: deployer)
 *   VAULT_ADAPTER_FEE_BPS = 0..200                       (default: 0 — spec Q6)
 *   ORACLE_DEVIATION_BPS  = 0..10000                     (default: 500 — 5%)
 *   WPAX_ADDRESS         = <pre-deployed WETH>           (optional; deploys MockWETH9 if absent)
 *   SEED_PRICES          = true | false                  (default: true on localhost)
 */

const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

const COLORS = {
  reset: "\x1b[0m",
  green: "\x1b[32m",
  yellow: "\x1b[33m",
  cyan: "\x1b[36m",
  gray: "\x1b[90m",
};
const c = (col, s) => `${COLORS[col]}${s}${COLORS.reset}`;
const step = (s) => console.log(`\n${c("yellow", `── ${s} ──`)}`);
const line = (label, val) => console.log(`  ${label.padEnd(22)} ${c("cyan", val)}`);
const ok = (msg) => console.log(`  ${c("green", "✓")} ${msg}`);

async function main({ afterCheckpoint } = {}) {
  const [deployer] = await ethers.getSigners();
  const net = await ethers.provider.getNetwork();
  const chainId = Number(net.chainId);
  const NETWORK_TYPE =
    require("../lib/operations").networkName();

  console.log(`\n${c("yellow", "╔══════════════════════════════════════════════════════════╗")}`);
  console.log(`${c("yellow", "║ Sidiora Meta-AG + PECOR — Layered Deployment             ║")}`);
  console.log(`${c("yellow", "╚══════════════════════════════════════════════════════════╝")}`);
  line("Deployer", deployer.address);
  line("Balance", `${ethers.formatEther(await ethers.provider.getBalance(deployer.address))} PAX`);
  line("Network", `${NETWORK_TYPE} (chainId ${chainId})`);
  line("Addresses file", `deployments/${NETWORK_TYPE}-addresses.json`);

  // ────────────────────────────────────────────────────────────────────── //
  // 0) Load existing Sidiora addresses (USDL, Router, Quoter, PoolRegistry, Timelock)
  // ────────────────────────────────────────────────────────────────────── //
  const addrPath = path.join(
    __dirname,
    "..",
    "..",
    "deployments",
    `${NETWORK_TYPE}-addresses.json`
  );
  if (!fs.existsSync(addrPath)) {
    throw new Error(
      `Addresses file not found: ${addrPath}\nRun scripts/deploy.js first to deploy the Sidiora launchpad.`
    );
  }
  const existing = JSON.parse(fs.readFileSync(addrPath, "utf8"));
  let addresses = existing;
  await require("../lib/operations").validateManifest(addresses, ethers.provider);
  const required = [
    "Router_proxy",
    "Quoter_proxy",
    "PoolRegistry_proxy",
    "Timelock",
  ];
  for (const key of required) {
    if (!addresses[key]) {
      throw new Error(
        `Missing required Sidiora address "${key}" in ${addrPath}. Deploy Sidiora first.`
      );
    }
  }
  if (!addresses._meta?.usdl) {
    throw new Error(
      `Missing USDL address in ${addrPath}._meta.usdl. Ensure Sidiora deployment saved it.`
    );
  }

  const USDL = addresses._meta.usdl;
  const TIMELOCK = addresses.Timelock;
  const SIDIORA_ROUTER = addresses.Router_proxy;
  const SIDIORA_QUOTER = addresses.Quoter_proxy;
  const SIDIORA_POOL_REGISTRY = addresses.PoolRegistry_proxy;

  await require("../lib/operations").validateManifest(addresses, ethers.provider);
  const FINAL_ADMIN = TIMELOCK;
  if (!FINAL_ADMIN || (process.env.ADMIN_ADDRESS && process.env.ADMIN_ADDRESS.toLowerCase() !== TIMELOCK.toLowerCase())) throw new Error("MetaAG final admin must be existing Timelock");
  // Never infer an immediate incident operator from the hot deployment signer.
  const guardianSource = process.env.INCIDENT_GUARDIAN_ADDRESS ? "explicit" : existing._meta?.guardian ? "core-guardian" : "timelock-only";
  const INCIDENT_GUARDIAN = process.env.INCIDENT_GUARDIAN_ADDRESS || existing._meta?.guardian || FINAL_ADMIN;
  if (!ethers.isAddress(INCIDENT_GUARDIAN) || INCIDENT_GUARDIAN === ethers.ZeroAddress) throw new Error("Invalid INCIDENT_GUARDIAN_ADDRESS");
  if (guardianSource === "core-guardian") {
    const liveGuardian = await new ethers.Contract(TIMELOCK, ["function guardian() view returns (address)"], ethers.provider).guardian();
    if (liveGuardian.toLowerCase() !== INCIDENT_GUARDIAN.toLowerCase()) throw new Error("Core manifest guardian differs from Timelock guardian; set INCIDENT_GUARDIAN_ADDRESS explicitly after review");
  }
  if (guardianSource === "timelock-only") console.log("  No incident guardian configured: Timelock-only incident authority; immediate guardian response disabled until governed role assignment.");
  const ADMIN = deployer.address; // Temporary authority for wiring only.
  const RELAYER = process.env.RELAYER_ADDRESS || deployer.address;
  const KEEPER = process.env.KEEPER_ADDRESS || deployer.address;
  const FEE_COLLECTOR = process.env.FEE_COLLECTOR || deployer.address;
  const VAULT_ADAPTER_FEE_BPS = BigInt(process.env.VAULT_ADAPTER_FEE_BPS || "0");
  const ORACLE_DEVIATION_BPS = BigInt(process.env.ORACLE_DEVIATION_BPS || "500");
  const usdlDecimals = Number(await new ethers.Contract(USDL, ["function decimals() view returns (uint8)"], ethers.provider).decimals());
  if (!Number.isInteger(usdlDecimals) || usdlDecimals < 0 || usdlDecimals > 36) throw new Error("Unsupported USDL decimals");
  const SIDIORA_MIN_LIQUIDITY = process.env.SIDIORA_MIN_LIQUIDITY_RAW === undefined
    ? ethers.parseUnits("1000", usdlDecimals) : BigInt(process.env.SIDIORA_MIN_LIQUIDITY_RAW);
  if (SIDIORA_MIN_LIQUIDITY <= 0n || SIDIORA_MIN_LIQUIDITY > ethers.MaxUint256) throw new Error("SIDIORA_MIN_LIQUIDITY_RAW must be positive uint256");
  if (process.env.ENABLE_SIDIORA_SPOT_REFERENCE && !["true", "false"].includes(process.env.ENABLE_SIDIORA_SPOT_REFERENCE)) throw new Error("ENABLE_SIDIORA_SPOT_REFERENCE must be true or false");
  const ENABLE_SPOT = process.env.ENABLE_SIDIORA_SPOT_REFERENCE === "true";
  const USDL_USD_ORACLE = process.env.USDL_USD_ORACLE_ADDRESS;
  if (ENABLE_SPOT) {
    if (!ethers.isAddress(USDL_USD_ORACLE) || await ethers.provider.getCode(USDL_USD_ORACLE) === "0x") throw new Error("Spot reference requires independent USDL_USD_ORACLE_ADDRESS contract");
    const reference = new ethers.Contract(USDL_USD_ORACLE, ["function getPrice(address) view returns (uint256)"], ethers.provider);
    if (await reference.getPrice(USDL) === 0n) throw new Error("USDL/USD reference must return a fresh positive price");
  }
  const SEED_PRICES =
    process.env.SEED_PRICES === undefined
      ? NETWORK_TYPE !== "paxeer-network"
      : process.env.SEED_PRICES === "true";

  line("Admin", `${ADMIN}${ADMIN === deployer.address ? " (deployer — localhost mode)" : ""}`);
  line("Relayer", RELAYER);
  line("Keeper", KEEPER);
  line("Fee collector", FEE_COLLECTOR);
  line("VaultAdapter fee", `${VAULT_ADAPTER_FEE_BPS} bps`);
  line("Oracle deviation", `${ORACLE_DEVIATION_BPS} bps`);
  line("Seed prices", SEED_PRICES ? "yes" : "no");

  step("Sidiora addresses loaded");
  line("USDL", USDL);
  line("Sidiora Router", SIDIORA_ROUTER);
  line("Sidiora Quoter", SIDIORA_QUOTER);
  line("Sidiora Registry", SIDIORA_POOL_REGISTRY);
  line("Timelock", TIMELOCK);

  // ────────────────────────────────────────────────────────────────────── //
  // Helpers
  // ────────────────────────────────────────────────────────────────────── //
  const layerNames = ["PriceOracle", "OracleHub", "TransactionTracker", "PECORVault", "PECOR", "PECOROrders", "MetaAGRouter", "MetaAGQuoter"];
  const layerKeys = new Set([...layerNames.flatMap(name => [`${name}_impl`, `${name}_proxy`]), "PriceOracleAdapter", "SidioraFeedAdapter", "VaultAdapter", "SidioraAdapter", "EmergencyPauseController"]);
  const previousSession = existing._metaAgDeployment;
  if (previousSession && previousSession.config?.incidentControlVersion !== 1) throw new Error("Legacy MetaAG deployment plan lacks incident-controller identity; explicit operator migration required before resume");
  const generatedWpax = !!previousSession?.records?.WPAX;
  const externalWpax = process.env.WPAX_ADDRESS || (!generatedWpax ? existing.WPAX : null);
  if (externalWpax && (!ethers.isAddress(externalWpax) || await ethers.provider.getCode(externalWpax) === "0x")) throw new Error("External WPAX has no contract code");
  if (!externalWpax && NETWORK_TYPE === "paxeer-network") throw new Error("WPAX_ADDRESS required for paxeer-network");
  if (VAULT_ADAPTER_FEE_BPS < 0n || VAULT_ADAPTER_FEE_BPS > 200n || ORACLE_DEVIATION_BPS < 0n || ORACLE_DEVIATION_BPS > 10000n) throw new Error("Invalid MetaAG fee/deviation configuration");
  for (const [name, address] of Object.entries({ RELAYER, KEEPER, FEE_COLLECTOR })) if (!ethers.isAddress(address) || address === ethers.ZeroAddress) throw new Error(`Invalid ${name}`);
  const inherited = {};
  for (const [key, value] of Object.entries(existing)) {
    if (key.startsWith("_") || layerKeys.has(key) || (key === "WPAX" && generatedWpax)) continue;
    if (typeof value === "string" && ethers.isAddress(value)) inherited[key] = value;
  }
  if (externalWpax) inherited.WPAX = externalWpax;
  const inheritedIdentity = {};
  const implementationSlot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc";
  for (const [key, address] of Object.entries(inherited)) {
    const code = await ethers.provider.getCode(address);
    if (code === "0x") throw new Error(`Inherited core address has no code: ${key}`);
    inheritedIdentity[key] = { address: address.toLowerCase(), codeHash: ethers.keccak256(code) };
    if (key.endsWith("_proxy")) {
      const implementation = ethers.getAddress("0x" + (await ethers.provider.getStorage(address, implementationSlot)).slice(-40));
      const implementationCode = await ethers.provider.getCode(implementation);
      if (implementationCode === "0x") throw new Error(`Inherited proxy implementation has no code: ${key}`);
      inheritedIdentity[key].implementation = implementation.toLowerCase();
      inheritedIdentity[key].implementationCodeHash = ethers.keccak256(implementationCode);
    }
  }
  const session = await require("../lib/deployment-session").deploymentSession({
    file: addrPath, signer: deployer, namespace: "_metaAgDeployment", inherited,
    resume: process.env.RESUME_DEPLOYMENT === "true", afterCheckpoint,
    config: { incidentControlVersion: 1, incidentGuardian: INCIDENT_GUARDIAN.toLowerCase(), incidentGuardianSource: guardianSource, inheritedIdentity, usdl: USDL.toLowerCase(), sid: existing._meta.sid?.toLowerCase(), network: NETWORK_TYPE,
      usdlCodeHash: ethers.keccak256(await ethers.provider.getCode(USDL)),
      sidCodeHash: existing._meta.sid ? ethers.keccak256(await ethers.provider.getCode(existing._meta.sid)) : null,
      admin: FINAL_ADMIN.toLowerCase(), relayer: RELAYER.toLowerCase(), keeper: KEEPER.toLowerCase(), feeCollector: FEE_COLLECTOR.toLowerCase(),
      vaultAdapterFeeBps: VAULT_ADAPTER_FEE_BPS.toString(), oracleDeviationBps: ORACLE_DEVIATION_BPS.toString(),
      sidioraMinLiquidityRaw: SIDIORA_MIN_LIQUIDITY.toString(), enableSpot: ENABLE_SPOT,
      usdlUsdOracle: ENABLE_SPOT ? USDL_USD_ORACLE.toLowerCase() : null,
      spotReferenceCodeHash: ENABLE_SPOT ? ethers.keccak256(await ethers.provider.getCode(USDL_USD_ORACLE)) : null,
      wpax: externalWpax ? externalWpax.toLowerCase() : "deploy-local", seedPrices: SEED_PRICES }
  });
  addresses = session.addresses;
  const { checkpoint, ensure } = session;
  const deployed = {};
  if (!addresses._meta_pecor) addresses._meta_pecor = { deployer: deployer.address, admin: FINAL_ADMIN, relayer: RELAYER, keeper: KEEPER, feeCollector: FEE_COLLECTOR, status: "bootstrap", network: String(chainId) };
  checkpoint("meta:bootstrap");
  const same = (a, b) => a.toLowerCase() === b.toLowerCase();
  async function deployUUPS(name, initArgs, keyPrefix = name) {
    const Factory = await ethers.getContractFactory(name);
    const impl = await session.deploy(name, [], `${keyPrefix}_impl`, `implementation:${name}`);
    const implAddr = await impl.getAddress();
    const initData = Factory.interface.encodeFunctionData("initialize", initArgs);
    const proxy = await session.deploy("ERC1967Proxy", [implAddr, initData], `${keyPrefix}_proxy`, `proxy:${name}`);
    const proxyAddr = await proxy.getAddress();
    const live = ethers.getAddress("0x" + (await ethers.provider.getStorage(proxyAddr, implementationSlot)).slice(-40));
    if (!same(live, implAddr)) throw new Error(`MetaAG proxy implementation mismatch: ${name}`);
    deployed[`${keyPrefix}_impl`] = implAddr;
    deployed[`${keyPrefix}_proxy`] = proxyAddr;
    return Factory.attach(proxyAddr);
  }
  async function deployImmutable(name, args, key = name) {
    const contract = await session.deploy(name, args, key, `immutable:${key}`);
    deployed[key] = await contract.getAddress();
    return contract;
  }
  async function ensureRole(label, contract, permission, account, send) {
    await ensure(label, () => contract.hasRole(permission, account), send);
    ok(label);
  }

  // ────────────────────────────────────────────────────────────────────── //
  // 1) WPAX (WETH rail) — deploy MockWETH9 on local; use pre-deployed on Paxeer
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 1 — WPAX (wrapped native)");
  let WPAX = externalWpax || (generatedWpax ? addresses.WPAX : null);
  if (!externalWpax) {
    if (NETWORK_TYPE === "paxeer-network") {
      throw new Error(
        "WPAX_ADDRESS not set for paxeer-network. Set env WPAX_ADDRESS to the live WPAX contract."
      );
    }
    console.log(`  ${c("gray", "No WPAX provided — deploying MockWETH9 for local testing")}`);
    const weth = await deployImmutable("MockWETH9", [], "WPAX");
    WPAX = await weth.getAddress();
  } else {
    line("WPAX (preexisting)", WPAX);
    deployed.WPAX = WPAX;
  }

  // ────────────────────────────────────────────────────────────────────── //
  // 2) PriceOracle
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 2 — PriceOracle");
  const priceOracle = await deployUUPS("PriceOracle", [deployer.address], "PriceOracle");

  // ────────────────────────────────────────────────────────────────────── //
  // 3) OracleHub
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 3 — OracleHub");
  const oracleHub = await deployUUPS(
    "OracleHub",
    [
      await priceOracle.getAddress(),
      ORACLE_DEVIATION_BPS,
      3000n,
      deployer.address,
    ],
    "OracleHub"
  );

  // ────────────────────────────────────────────────────────────────────── //
  // 4) TransactionTracker
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 4 — TransactionTracker");
  const tracker = await deployUUPS("TransactionTracker", [deployer.address], "TransactionTracker");

  // ────────────────────────────────────────────────────────────────────── //
  // 5) PECORVault
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 5 — PECORVault");
  const vault = await deployUUPS(
    "PECORVault",
    [WPAX, await tracker.getAddress(), deployer.address],
    "PECORVault"
  );

  // ────────────────────────────────────────────────────────────────────── //
  // 6) PECOR
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 6 — PECOR");
  const pecor = await deployUUPS(
    "PECOR",
    [
      await priceOracle.getAddress(),
      await vault.getAddress(),
      WPAX,
      await tracker.getAddress(),
      deployer.address,
    ],
    "PECOR"
  );

  // ────────────────────────────────────────────────────────────────────── //
  // 7) PECOROrders
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 7 — PECOROrders");
  const pecorOrders = await deployUUPS(
    "PECOROrders",
    [
      await priceOracle.getAddress(),
      await vault.getAddress(),
      await tracker.getAddress(),
      deployer.address,
    ],
    "PECOROrders"
  );

  // ────────────────────────────────────────────────────────────────────── //
  // 8) PriceOracleAdapter (immutable)
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 8 — PriceOracleAdapter");
  const priceOracleAdapter = await deployImmutable(
    "PriceOracleAdapter",
    [await priceOracle.getAddress()],
    "PriceOracleAdapter"
  );

  // ────────────────────────────────────────────────────────────────────── //
  // 9) SidioraFeedAdapter (immutable)
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 9 — SidioraFeedAdapter");
  const sidioraFeedAdapter = await deployImmutable(
    "SidioraFeedAdapter",
    [SIDIORA_POOL_REGISTRY, SIDIORA_MIN_LIQUIDITY, deployer.address],
    "SidioraFeedAdapter"
  );

  // ────────────────────────────────────────────────────────────────────── //
  if (ENABLE_SPOT) await ensure("wiring:spot-reference", async () => await sidioraFeedAdapter.spotReferenceEnabled() && same(await sidioraFeedAdapter.usdlUsdOracle(), USDL_USD_ORACLE) && same(await sidioraFeedAdapter.usdlToken(), USDL), () => sidioraFeedAdapter.configureSpotReference(true, USDL_USD_ORACLE, USDL));
  line("Sidiora spot reference", ENABLE_SPOT ? "enabled by explicit opt-in; instantaneous/manipulable" : "disabled; not a healthy price source by default");
  // 10) VaultAdapter (immutable)
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 10 — VaultAdapter");
  const vaultAdapter = await deployImmutable(
    "VaultAdapter",
    [
      await vault.getAddress(),
      await priceOracle.getAddress(),
      VAULT_ADAPTER_FEE_BPS,
      FEE_COLLECTOR,
      deployer.address,
    ],
    "VaultAdapter"
  );

  // ────────────────────────────────────────────────────────────────────── //
  // 11) SidioraAdapter (immutable)
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 11 — SidioraAdapter");
  const sidioraAdapter = await deployImmutable(
    "SidioraAdapter",
    [SIDIORA_POOL_REGISTRY, SIDIORA_QUOTER, SIDIORA_ROUTER, USDL, deployer.address],
    "SidioraAdapter"
  );

  // ────────────────────────────────────────────────────────────────────── //
  // 12) MetaAGRouter
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 12 — MetaAGRouter");
  const router = await deployUUPS(
    "MetaAGRouter",
    [await oracleHub.getAddress(), ORACLE_DEVIATION_BPS, deployer.address],
    "MetaAGRouter"
  );

  // ────────────────────────────────────────────────────────────────────── //
  // 13) MetaAGQuoter
  // ────────────────────────────────────────────────────────────────────── //
  step("Step 13 — MetaAGQuoter");
  const quoter = await deployUUPS(
    "MetaAGQuoter",
    [
      await priceOracle.getAddress(),
      await vault.getAddress(),
      WPAX,
      await pecor.getAddress(),
      deployer.address,
    ],
    "MetaAGQuoter"
  );

  // ────────────────────────────────────────────────────────────────────── //
  // Role wiring                                                            //
  // ────────────────────────────────────────────────────────────────────── //
  step("Role wiring");

  const role = name => ethers.id(name);
  const incidentTargets = [["PECOR", pecor], ["PECOROrders", pecorOrders], ["MetaAGRouter", router], ["OracleHub", oracleHub], ["PriceOracle", priceOracle]];
  const incidentController = await deployImmutable("EmergencyPauseController", [FINAL_ADMIN, INCIDENT_GUARDIAN, await Promise.all(incidentTargets.map(([, target]) => target.getAddress()))]);
  const incidentControllerAddress = await incidentController.getAddress();
  for (const [name, target] of incidentTargets) {
    await ensureRole(`wiring:incident-pauser:${name}`, target, role("PAUSER_ROLE"), incidentControllerAddress, () => target.grantRole(role("PAUSER_ROLE"), incidentControllerAddress));
  }
  if (!await incidentController.hasRole(ethers.ZeroHash, FINAL_ADMIN) || await incidentController.hasRole(ethers.ZeroHash, deployer.address) || !await incidentController.hasRole(role("INCIDENT_GUARDIAN_ROLE"), INCIDENT_GUARDIAN)) throw new Error("Incident controller authority verification failed");
  for (const [name, contract] of [["PECOR", pecor], ["PECOROrders", pecorOrders]]) {
    await ensure(`wiring:fee-collector:${name}`, async () => same(await contract.feeCollector(), FEE_COLLECTOR) && (name !== "PECOR" || await contract.hasRole(role("FEE_COLLECTOR_ROLE"), FEE_COLLECTOR)), () => contract.setFeeCollector(FEE_COLLECTOR));
  }
  await ensureRole("wiring:relayer", priceOracle, role("RELAYER_ROLE"), RELAYER, () => priceOracle.setRelayer(RELAYER, true));
  for (const [name, contract] of [["PECOR", pecor], ["PECOROrders", pecorOrders], ["VaultAdapter", vaultAdapter]]) {
    const address = await contract.getAddress();
    await ensureRole(`wiring:operator:${name}`, vault, role("OPERATOR_ROLE"), address, () => vault.setOperator(address, true));
  }
  await ensureRole("wiring:keeper", pecorOrders, role("KEEPER_ROLE"), KEEPER, () => pecorOrders.setKeeper(KEEPER, true));
  for (const [name, contract] of [["PECOR", pecor], ["PECOROrders", pecorOrders], ["MetaAGRouter", router]]) {
    const address = await contract.getAddress();
    await ensureRole(`wiring:emitter:${name}`, tracker, role("EMITTER_ROLE"), address, () => tracker.setAuthorizedEmitter(address, true));
  }
  for (const [name, adapter, priority] of [["PriceOracleAdapter", priceOracleAdapter, 10n], ["SidioraFeedAdapter", sidioraFeedAdapter, 20n]]) {
    const address = await adapter.getAddress();
    await ensure(`wiring:oracle-adapter:${name}`, async () => (await oracleHub.getAdapters()).some(entry => same(entry.adapter, address) && entry.priority === priority && entry.active), () => oracleHub.registerAdapter(address, priority));
  }
  for (const [name, adapter] of [["VaultAdapter", vaultAdapter], ["SidioraAdapter", sidioraAdapter]]) {
    const address = await adapter.getAddress();
    await ensure(`wiring:router-adapter:${name}`, async () => (await router.getAdapters()).some(entry => same(entry.adapter, address) && entry.active), () => router.registerAdapter(address));
  }

  // ────────────────────────────────────────────────────────────────────── //
  // Token registration (USDL + WPAX)                                      //
  // ────────────────────────────────────────────────────────────────────── //
  step("Token registry");
  const ONE = 10n ** 18n;
  const tokenConfig = [60, 100n, ONE / 100n, ONE * 1_000_000n, 3600];
  const bootstrapTokens = [
    { name: "USDL", addr: USDL, stable: true, price: ONE },
    { name: "WPAX", addr: WPAX, stable: false, price: ONE },
  ];

  for (const t of bootstrapTokens) {
    await ensure(`wiring:vault-token:${t.name}`, async () => { const info = await vault.getTokenInfo(t.addr); return info[0] && info[1] === t.stable; }, () => vault.registerToken(t.addr, t.stable));
    await ensure(`wiring:oracle-token:${t.name}`, async () => {
      const cfg = await priceOracle.getTokenConfig(t.addr);
      return cfg.isRegistered && cfg.heartbeatInterval === BigInt(tokenConfig[0]) && cfg.deviationThresholdBps === tokenConfig[1] && cfg.minPriceBound === tokenConfig[2] && cfg.maxPriceBound === tokenConfig[3] && cfg.maxStaleness === BigInt(tokenConfig[4]);
    }, () => priceOracle.registerToken(t.addr, ...tokenConfig));
  }
  if (SEED_PRICES) {
    const relayerSigner = same(RELAYER, deployer.address) ? deployer : await ethers.getSigner(RELAYER);
    // Publication is one-time bootstrap work. History proves it even after prices age.
    await ensure("wiring:seed-prices", async () => {
      for (const token of bootstrapTokens) {
        const history = await priceOracle.getPriceHistory(token.addr, 1);
        if (!history.length || history[0].price !== token.price) return false;
      }
      return true;
    }, () => priceOracle.connect(relayerSigner).batchUpdatePrices(bootstrapTokens.map(t => t.addr), bootstrapTokens.map(t => t.price)));
  }

  // ────────────────────────────────────────────────────────────────────── //
  // Transfer admin to Timelock if requested                                //
  // ────────────────────────────────────────────────────────────────────── //
  // ────────────────────────────────────────────────────────────────────── //
  // Save merged addresses                                                  //
  // ────────────────────────────────────────────────────────────────────── //
  step("Finalizing MetaAG administrative authority");
  for (const [key, addr] of Object.entries(deployed)) {
    if (!key.endsWith("_proxy") && !["VaultAdapter", "SidioraAdapter", "SidioraFeedAdapter"].includes(key)) continue;
    const name = key.replace(/_proxy$/, "");
    const ct = await ethers.getContractAt(name, addr);
    await ensure(`handoff:grant:${name}`, () => ct.hasRole(ethers.ZeroHash, FINAL_ADMIN), () => ct.grantRole(ethers.ZeroHash, FINAL_ADMIN));
    await ensure(`handoff:${name}`, async () => !await ct.hasRole(ethers.ZeroHash, deployer.address), () => ct.renounceRole(ethers.ZeroHash, deployer.address));
    if (!await ct.hasRole(ethers.ZeroHash, FINAL_ADMIN) || await ct.hasRole(ethers.ZeroHash, deployer.address)) throw new Error(`Finalization failed ${name}`);
  }
  step("Saving merged addresses");
  Object.assign(addresses, deployed);
  addresses._meta_pecor = {
    deployer: deployer.address,
    admin: FINAL_ADMIN,
    status: "finalized",
    sidioraMinLiquidityRaw: SIDIORA_MIN_LIQUIDITY.toString(),
    sidioraSpotReferenceEnabled: ENABLE_SPOT,
    usdlUsdOracle: ENABLE_SPOT ? USDL_USD_ORACLE : null,
    relayer: RELAYER,
    keeper: KEEPER,
    feeCollector: FEE_COLLECTOR,
    incidentController: incidentControllerAddress,
    incidentGuardian: INCIDENT_GUARDIAN,
    incidentGuardianSource: guardianSource,
    immediateIncidentResponse: guardianSource !== "timelock-only" && INCIDENT_GUARDIAN.toLowerCase() !== FINAL_ADMIN.toLowerCase(),
    vaultAdapterFeeBps: VAULT_ADAPTER_FEE_BPS.toString(),
    oracleDeviationBps: ORACLE_DEVIATION_BPS.toString(),
    wpax: WPAX,
    network: chainId.toString(),
    timestamp: addresses._meta_pecor?.timestamp || new Date().toISOString(),
  };
  checkpoint("complete");
  line("Saved", addrPath);

  step("Summary");
  console.log(`  ${c("green", "✓ Deployment complete")}`);
  console.log(`  Total PECOR + Meta-AG artifacts: ${Object.keys(deployed).length}`);
  console.log(`\n  Adapters registered on MetaAGRouter:`);
  line("    VaultAdapter", deployed.VaultAdapter);
  line("    SidioraAdapter", deployed.SidioraAdapter);
  console.log(`\n  Adapters registered on OracleHub:`);
  line("    PriceOracleAdapter", deployed.PriceOracleAdapter);
  line("    SidioraFeedAdapter", deployed.SidioraFeedAdapter);
  console.log(`\n  Router proxy (user-facing):`);
  line("    MetaAGRouter", deployed.MetaAGRouter_proxy);
  console.log();
}

module.exports = { main };
if (require.main === module) main()
  .then(() => process.exit(0))
  .catch((e) => {
    console.error(c("reset", "\n❌ Deployment failed:"), e);
    process.exit(1);
  });
