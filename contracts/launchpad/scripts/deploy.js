/**
 * Sidiora Launchpad AMM — Full Deployment Script
 *
 * Deploy order (20 steps):
 *  1. EventEmitter (UUPS)          9.  PoolBeacon
 *  2. ProtocolConfig (UUPS)       10.  SidioraNFT (UUPS)
 *  3. Treasury (UUPS)             11.  SidioraFactory (UUPS)
 *  4. Timelock (immutable)        12.  OpticalRegistry (UUPS)
 *  5. GovernanceModule (UUPS)     13.  Router (UUPS)
 *  6. PoolRegistry (UUPS)         14.  Quoter (UUPS)
 *  7. FeeAccumulator (UUPS)       15.  FeesRouter (UUPS)
 *  8. SidioraPool impl            16-20. Wire roles + optical presets
 *
 * Usage:
 *   npx hardhat run scripts/deploy.js --network paxeer-network
 *
 * Requires .env:
 *   PRIVATE_KEY, USDL_ADDRESS, SID_ADDRESS, GUARDIAN_ADDRESS, TIMELOCK_DELAY
 */

const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

async function main({ afterCheckpoint } = {}) {
  const [deployer] = await ethers.getSigners();
  const balance = await ethers.provider.getBalance(deployer.address);
  console.log(`\n🚀 Deploying Sidiora Launchpad AMM`);
  console.log(`   Deployer:  ${deployer.address}`);
  console.log(`   Balance:   ${ethers.formatEther(balance)} PAX`);
  console.log(`   Network:   ${(await ethers.provider.getNetwork()).chainId}\n`);

  const USDL = process.env.USDL_ADDRESS;
  const GOVERNANCE_MODE = process.env.GOVERNANCE_MODE || "admin";
  if (!["admin", "token"].includes(GOVERNANCE_MODE)) throw new Error("GOVERNANCE_MODE must be admin or token");
  const GOVERNANCE_ADMIN = GOVERNANCE_MODE === "admin" ? process.env.GOVERNANCE_ADMIN_ADDRESS || deployer.address : null;
  const SID = GOVERNANCE_MODE === "token" ? process.env.SID_ADDRESS : null;
  const GUARDIAN = process.env.GUARDIAN_ADDRESS || deployer.address;
  const ADMIN = deployer.address; // Temporary bootstrap, always finalized to Timelock.
  const FINAL_ADMIN = process.env.ADMIN_ADDRESS;
  const TIMELOCK_DELAY = parseInt(process.env.TIMELOCK_DELAY || "172800");
  const NETWORK_TYPE= require("./lib/operations").networkName();

  if (!USDL) throw new Error("USDL_ADDRESS not set in .env");
  if (GOVERNANCE_MODE === "token" && !SID) throw new Error("SID_ADDRESS required for token governance mode");
  if (GOVERNANCE_ADMIN && (!ethers.isAddress(GOVERNANCE_ADMIN) || GOVERNANCE_ADMIN === ethers.ZeroAddress)) throw new Error("Invalid GOVERNANCE_ADMIN_ADDRESS");

  const outPath = require("./lib/operations").manifestPath();

  for (const [name, addr] of Object.entries({ USDL, GUARDIAN, ...(GOVERNANCE_MODE === "token" ? { SID } : {}) })) {
    if (!ethers.isAddress(addr) || addr === ethers.ZeroAddress) throw new Error(`Invalid ${name}`);
    if (name !== "GUARDIAN" && await ethers.provider.getCode(addr) === "0x") throw new Error(`${name} has no contract code`);
  }
  if (FINAL_ADMIN) throw new Error("ADMIN_ADDRESS override unsupported for core: final admin is newly deployed Timelock; remove override before deploying");
  if (!Number.isSafeInteger(TIMELOCK_DELAY) || TIMELOCK_DELAY < 3600 || TIMELOCK_DELAY > 2592000) throw new Error("Invalid TIMELOCK_DELAY");
  const usdlToken = new ethers.Contract(USDL, ["function decimals() view returns (uint8)"], ethers.provider);
  if (await usdlToken.decimals() !== 6n) throw new Error("Sidiora requires 6-decimal USDL for creation fees and reserves");
  const session = await require("./lib/deployment-session").deploymentSession({
    file: outPath, signer: deployer, resume: process.env.RESUME_DEPLOYMENT === "true", afterCheckpoint,
    config: { usdl: USDL.toLowerCase(), sid: SID ? SID.toLowerCase() : null, governanceMode: GOVERNANCE_MODE, governanceAdmin: GOVERNANCE_ADMIN ? GOVERNANCE_ADMIN.toLowerCase() : null, guardian: GUARDIAN.toLowerCase(), delay: TIMELOCK_DELAY, network: NETWORK_TYPE, usdlCodeHash: ethers.keccak256(await ethers.provider.getCode(USDL)), sidCodeHash: SID ? ethers.keccak256(await ethers.provider.getCode(SID)) : null }
  });
  const { addresses, checkpoint, ensure } = session;
  async function deployUUPS(name, args, initializer = "initialize") {
    const Factory = await ethers.getContractFactory(name);
    const impl = await session.deploy(name, [], `${name}_impl`, `implementation:${name}`);
    const implAddr = await impl.getAddress();
    const initData = Factory.interface.encodeFunctionData(initializer, args);
    const proxy = await session.deploy("UUPSProxy", [implAddr, initData], `${name}_proxy`, `proxy:${name}`);
    const proxyAddr = await proxy.getAddress();
    const slot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc";
    const live = ethers.getAddress("0x" + (await ethers.provider.getStorage(proxyAddr, slot)).slice(-40));
    if (live.toLowerCase() !== implAddr.toLowerCase()) throw new Error(`Resume proxy implementation drift ${name}`);
    return Factory.attach(proxyAddr);
  }

  // ═══════════════════════════════════════════════════════════════════
  // Step 1: EventEmitter (deploy first — many contracts reference it)
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 1: EventEmitter ──");
  const eventEmitterProxy = await deployUUPS("EventEmitter", [
    ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 2: ProtocolConfig
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 2: ProtocolConfig ──");
  const configProxy = await deployUUPS("ProtocolConfig", [
    USDL, addresses.EventEmitter_proxy, ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 3: Treasury
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 3: Treasury ──");
  const treasuryProxy = await deployUUPS("Treasury", [
    addresses.EventEmitter_proxy, ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 4: Timelock (immutable)
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 4: Timelock ──");
  const timelock = await session.deploy("Timelock", [TIMELOCK_DELAY, ADMIN, GUARDIAN], "Timelock");
  console.log(`    addr:  ${addresses.Timelock}`);

  // ═══════════════════════════════════════════════════════════════════
  // Step 5: GovernanceModule (6 args: votingToken, timelock, admin,
  //         proposalThreshold, votingPeriod, quorumVotes)
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 5: GovernanceModule ──");
  const PROPOSAL_THRESHOLD = ethers.parseUnits("100000", 6);  // 100k SID to propose
  const VOTING_PERIOD = 17280;                              // ~3 days in blocks
  const QUORUM_VOTES = ethers.parseUnits("1000000", 6);        // 1M SID quorum
  const govProxy = GOVERNANCE_MODE === "admin"
    ? await deployUUPS("GovernanceModule", [addresses.Timelock, ADMIN], "initializeAdmin")
    : await deployUUPS("GovernanceModule", [SID, addresses.Timelock, ADMIN, PROPOSAL_THRESHOLD, VOTING_PERIOD, QUORUM_VOTES]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 6: PoolRegistry
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 6: PoolRegistry ──");
  const registryProxy = await deployUUPS("PoolRegistry", [
    addresses.EventEmitter_proxy, ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 7: FeeAccumulator
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 7: FeeAccumulator ──");
  const accProxy = await deployUUPS("FeeAccumulator", [
    addresses.ProtocolConfig_proxy,
    addresses.Treasury_proxy,
    addresses.PoolRegistry_proxy,
    addresses.EventEmitter_proxy,
    USDL,
    ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 8: SidioraPool implementation (not proxied — beacon target)
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 8: SidioraPool impl ──");
  await session.deploy("SidioraPool", [], "SidioraPool_impl", "implementation:SidioraPool");
  console.log(`    impl:  ${addresses.SidioraPool_impl}`);

  // ═══════════════════════════════════════════════════════════════════
  // Step 9: PoolBeacon
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 9: PoolBeacon ──");
  const beacon = await session.deploy("PoolBeacon", [addresses.SidioraPool_impl, ADMIN], "PoolBeacon");
  console.log(`    addr:  ${addresses.PoolBeacon}`);

  // ═══════════════════════════════════════════════════════════════════
  // Step 10: SidioraNFT
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 10: SidioraNFT ──");
  const nftProxy = await deployUUPS("SidioraNFT", [
    "Sidiora Pool NFT", "SIDNFT", addresses.EventEmitter_proxy, ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 11: SidioraFactory
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 11: SidioraFactory ──");
  const factoryProxy = await deployUUPS("SidioraFactory", [
    addresses.PoolBeacon,
    addresses.SidioraNFT_proxy,
    addresses.PoolRegistry_proxy,
    addresses.EventEmitter_proxy,
    addresses.ProtocolConfig_proxy,
    addresses.Treasury_proxy,
    addresses.FeeAccumulator_proxy,
    USDL,
    ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 12: OpticalRegistry
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 12: OpticalRegistry ──");
  const optRegistryProxy = await deployUUPS("OpticalRegistry", [
    addresses.EventEmitter_proxy, ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 13: Router
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 13: Router ──");
  const routerProxy = await deployUUPS("Router", [
    addresses.SidioraFactory_proxy,
    addresses.PoolRegistry_proxy,
    addresses.ProtocolConfig_proxy,
    USDL,
    ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 14: Quoter
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 14: Quoter ──");
  const quoterProxy = await deployUUPS("Quoter", [
    addresses.PoolRegistry_proxy,
    addresses.ProtocolConfig_proxy,
    ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 15: FeesRouter
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 15: FeesRouter ──");
  const feesRouterProxy = await deployUUPS("FeesRouter", [
    addresses.SidioraNFT_proxy,
    addresses.FeeAccumulator_proxy,
    addresses.PoolRegistry_proxy,
    ADMIN,
  ]);

  // ═══════════════════════════════════════════════════════════════════
  // Step 16: Deploy optical presets (immutable)
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 16: Optical Presets ──");
  const opticals = {};
  const presets = [
    { name: "AntiSnipeOptical", args: [addresses.PoolRegistry_proxy, addresses.Timelock, 100, 10] },
    { name: "MaxWalletOptical", args: [addresses.PoolRegistry_proxy, addresses.Timelock, 200] },
    { name: "TaxOptical", args: [addresses.PoolRegistry_proxy, addresses.Timelock, 200, 200] },
    { name: "CooldownOptical", args: [addresses.PoolRegistry_proxy, addresses.Timelock, 60] },
    { name: "BuybackBurnOptical", args: [addresses.PoolRegistry_proxy, addresses.Timelock, 1000] },
  ];

  for (const preset of presets) {
    await session.deploy(preset.name, preset.args, preset.name);
    opticals[preset.name] = addresses[preset.name];
  }

  // ═══════════════════════════════════════════════════════════════════
  // Step 17-19: Wire roles
  // ═══════════════════════════════════════════════════════════════════
  console.log("── Step 17-19: Wiring Roles ──");

  const role = name => ethers.id(name);
  const same = (a, b) => a.toLowerCase() === b.toLowerCase();
  async function grant(label, ct, permission, account) {
    await ensure(label, () => ct.hasRole(permission, account), () => ct.grantRole(permission, account));
  }
  await ensure("wiring:emitter-registry", async () => same(await eventEmitterProxy.poolRegistry(), addresses.PoolRegistry_proxy), () => eventEmitterProxy.setPoolRegistry(addresses.PoolRegistry_proxy));
  await ensure("wiring:emitter-factory", async () => same(await eventEmitterProxy.sidioraFactory(), addresses.SidioraFactory_proxy), () => eventEmitterProxy.setSidioraFactory(addresses.SidioraFactory_proxy));
  await ensure("wiring:emitter-opticals", async () => same(await eventEmitterProxy.opticalRegistry(), addresses.OpticalRegistry_proxy), () => eventEmitterProxy.setOpticalRegistry(addresses.OpticalRegistry_proxy));
  await grant("wiring:registry-factory", registryProxy, role("FACTORY_ROLE"), addresses.SidioraFactory_proxy);
  await grant("wiring:nft-minter", nftProxy, role("MINTER_ROLE"), addresses.SidioraFactory_proxy);
  await grant("wiring:nft-strategy", nftProxy, role("STRATEGY_SETTER_ROLE"), addresses.FeesRouter_proxy);
  for (const producer of ["FeeAccumulator", "SidioraFactory", "Router"]) await grant(`wiring:treasury-${producer}`, treasuryProxy, role("DEPOSITOR_ROLE"), addresses[`${producer}_proxy`]);
  await grant("wiring:factory-router", factoryProxy, role("ROUTER_ROLE"), addresses.Router_proxy);
  await grant("wiring:acc-fees-router", accProxy, role("FEES_ROUTER_ROLE"), addresses.FeesRouter_proxy);
  await grant("wiring:acc-factory", accProxy, role("FACTORY_ROLE"), addresses.SidioraFactory_proxy);
  const producers = ["SidioraFactory", "ProtocolConfig", "PoolRegistry", "FeeAccumulator", "SidioraNFT", "Router", "FeesRouter", "OpticalRegistry"];
  for (const producer of producers) {
    const emitter = addresses[`${producer}_proxy`];
    await ensure(`wiring:generic-${producer}`, () => eventEmitterProxy.isAuthorizedEmitter(emitter), () => eventEmitterProxy.setAuthorizedEmitter(emitter, true));
  }
  const typedProducers = { ProtocolConfig: ["emitConfigUpdated"], FeeAccumulator: ["emitFeeRecorded", "emitFeeDistributed", "emitFeeFlow"], SidioraNFT: ["emitFeeStrategyChanged", "emitNftMint"], Router: ["emitRouterTrade"], FeesRouter: ["emitFeeDistributed"] };
  for (const [name, methods] of Object.entries(typedProducers)) for (const method of methods) {
    const selector = eventEmitterProxy.interface.getFunction(method).selector;
    await ensure(`wiring:typed-${name}-${method}`, () => eventEmitterProxy.isTypedEmitter(addresses[`${name}_proxy`], selector), () => eventEmitterProxy.setTypedEmitter(addresses[`${name}_proxy`], selector, true));
  }
  for (const [name, addr] of Object.entries(opticals)) await ensure(`wiring:optical-${name}`, () => optRegistryProxy.isRegistered(addr), () => optRegistryProxy.registerOptical(addr, name, `Default ${name} preset`, 1, "Sidiora"));
  await grant("handoff:gov-admin", govProxy, role("ADMIN_ROLE"), addresses.Timelock);
  if (GOVERNANCE_MODE === "admin") {
    await grant("handoff:gov-controller", govProxy, role("ADMIN_ROLE"), GOVERNANCE_ADMIN);
    if (!same(GOVERNANCE_ADMIN, deployer.address)) await ensure("handoff:gov-bootstrap-role", async () => !await govProxy.hasRole(role("ADMIN_ROLE"), deployer.address), () => govProxy.renounceRole(role("ADMIN_ROLE"), deployer.address));
  } else {
    await ensure("handoff:gov-mode", async () => !await govProxy.adminModeActive(), () => govProxy.deactivateAdminMode());
    await ensure("handoff:gov-bootstrap-role", async () => !await govProxy.hasRole(role("ADMIN_ROLE"), deployer.address), () => govProxy.renounceRole(role("ADMIN_ROLE"), deployer.address));
  }
  for (const [key, addr] of Object.entries(addresses).filter(([k]) => k.endsWith("_proxy"))) {
    const name = key.replace("_proxy", "");
    const ct = await ethers.getContractAt(name, addr);
    await grant(`handoff:grant-${name}`, ct, ethers.ZeroHash, addresses.Timelock);
    await ensure(`handoff:${name}`, async () => !await ct.hasRole(ethers.ZeroHash, deployer.address), () => ct.renounceRole(ethers.ZeroHash, deployer.address));
  }
  await ensure("handoff:beacon-nominate", async () => same(await beacon.owner(), addresses.Timelock) || same(await beacon.pendingOwner(), addresses.Timelock), () => beacon.transferOwnership(addresses.Timelock));
  const accept = beacon.interface.encodeFunctionData("acceptOwnership");
  if (!addresses._governance) {
    const latest = await ethers.provider.getBlock("latest");
    const eta = latest.timestamp + TIMELOCK_DELAY + 60;
    addresses._governance = { governanceMode: GOVERNANCE_MODE, governanceAdmin: GOVERNANCE_ADMIN, status: "pending-beacon-acceptance", beaconAcceptance: { target: addresses.PoolBeacon, value: "0", data: accept, eta }, finalAdmin: addresses.Timelock };
    checkpoint("handoff:beacon-acceptance-plan");
  }
  const acceptance = addresses._governance.beaconAcceptance;
  if (!same(acceptance.target, addresses.PoolBeacon) || acceptance.data !== accept || acceptance.value !== "0") throw new Error("Beacon acceptance plan identity drift");
  if (!same(await beacon.owner(), addresses.Timelock)) {
    const now = (await ethers.provider.getBlock("latest")).timestamp;
    if (BigInt(now) > BigInt(acceptance.eta) + await timelock.GRACE_PERIOD()) throw new Error("Beacon acceptance operation expired; governed cancellation/requeue must be reviewed before resuming");
  }
  const acceptanceHash = ethers.keccak256(ethers.AbiCoder.defaultAbiCoder().encode(["address", "uint256", "bytes", "uint256"], [acceptance.target, 0, accept, acceptance.eta]));
  await ensure("handoff:beacon-queue", async () => same(await beacon.owner(), addresses.Timelock) || await timelock.queuedTransactions(acceptanceHash), () => timelock.queueTransaction(addresses.PoolBeacon, 0, accept, acceptance.eta));
  await ensure("handoff:proposer", async () => same(await timelock.proposer(), addresses.GovernanceModule_proxy), () => timelock.setProposer(addresses.GovernanceModule_proxy));
  const finalized = same(await beacon.owner(), addresses.Timelock);
  addresses._meta = { ...addresses._meta, guardian: GUARDIAN, timelockDelay: TIMELOCK_DELAY, governanceMode: GOVERNANCE_MODE, governanceAdmin: GOVERNANCE_ADMIN, status: finalized ? "finalized" : "pending-beacon-acceptance" };
  addresses._governance.status = addresses._meta.status;
  checkpoint("complete");
  console.log(`Governance: ${GOVERNANCE_MODE === "admin" ? `administrator ${GOVERNANCE_ADMIN}` : `token voting ${SID}`}; Timelock delay ${TIMELOCK_DELAY} seconds.`);
  console.log(`Deployment wiring verified; status ${addresses._meta.status}. Manifest: ${outPath}`);
  return addresses;
}

module.exports = { main };
if (require.main === module) main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error("❌ Deployment failed:", error);
    process.exit(1);
  });
