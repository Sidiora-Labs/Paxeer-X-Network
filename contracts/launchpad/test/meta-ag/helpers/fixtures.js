/**
 * Sidiora Meta-AG — Deployment fixtures
 *
 * Composition order (spec §11 build order, mirrored by plan Phases 2–7):
 *   PriceOracle → OracleHub → TransactionTracker → PECORVault v2 →
 *   PECOR → PECOROrders → PriceOracleAdapter → SidioraFeedAdapter →
 *   VaultAdapter → SidioraAdapter → MetaAGRouter → MetaAGQuoter
 *
 * Every helper is an atomic per-contract deploy that:
 *   1. Deploys the UUPS implementation.
 *   2. Deploys `ERC1967Proxy` (or chain equivalent) with the ABI-encoded
 *      `initialize(...)` payload so the proxy is live on first tx.
 *   3. Returns `{ proxy, implementation, abi }` wired as an ethers v6 Contract.
 *
 * Non-UUPS adapters (PriceOracleAdapter, SidioraFeedAdapter, VaultAdapter,
 * SidioraAdapter) are plain `AccessControl` deploys — no proxy.
 *
 * Phase 0 delivers the structural skeleton only. Each helper throws
 * `NotImplementedError` until the matching Phase 2–7 task lands its contract.
 * Tests in later phases MUST import from this module — do not inline fixtures.
 */

const { ethers } = require("hardhat");
const { PECOR_ROLES, LIVE_ADDRESSES } = require("./constants");

class NotImplementedError extends Error {
  constructor(phase, task, subject) {
    super(
      `[meta-ag/fixtures] ${subject} fixture is not yet implemented. ` +
        `Scheduled for Phase ${phase} / Task ${task} per docs/plans/pecor-sidiora-merge-plan.md.`
    );
    this.name = "NotImplementedError";
    this.phase = phase;
    this.task = task;
    this.subject = subject;
  }
}

// --------------------------------------------------------------------------- //
// Generic UUPS deploy helper (used by every proxied contract below once       //
// the matching implementation contract lands).                                //
// --------------------------------------------------------------------------- //
async function deployUUPSProxy({ factoryName, initializer, initArgs, signer }) {
  const deployer = signer ?? (await ethers.getSigners())[0];
  const Factory = await ethers.getContractFactory(factoryName, deployer);
  const impl = await Factory.deploy();
  await impl.waitForDeployment();

  const initData = impl.interface.encodeFunctionData(initializer, initArgs);
  const Proxy = await ethers.getContractFactory("ERC1967Proxy", deployer);
  const proxy = await Proxy.deploy(await impl.getAddress(), initData);
  await proxy.waitForDeployment();

  const instance = Factory.attach(await proxy.getAddress()).connect(deployer);
  // Stash helpers so tests can introspect impl/proxy independently if needed.
  instance.__impl = impl;
  instance.__proxy = proxy;
  return instance;
}

// --------------------------------------------------------------------------- //
// Phase 2 — Oracle layer                                                      //
// --------------------------------------------------------------------------- //
async function deployPriceOracle({ admin, signer } = {}) {
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  return deployUUPSProxy({
    factoryName: "PriceOracle",
    initializer: "initialize",
    initArgs: [adminAddr],
    signer: deployer,
  });
}
async function deployOracleHub({
  admin,
  primaryOracle,
  deviationBps,
  minConfidence,
  signer,
} = {}) {
  if (!primaryOracle) {
    throw new Error("[meta-ag/fixtures] deployOracleHub requires `primaryOracle`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  return deployUUPSProxy({
    factoryName: "OracleHub",
    initializer: "initialize",
    initArgs: [primaryOracle, deviationBps ?? 500, minConfidence ?? 3000, adminAddr],
    signer: deployer,
  });
}
async function deployPriceOracleAdapter({ priceOracle, signer } = {}) {
  if (!priceOracle) {
    throw new Error("[meta-ag/fixtures] deployPriceOracleAdapter requires `priceOracle`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const Factory = await ethers.getContractFactory("PriceOracleAdapter", deployer);
  const adapter = await Factory.deploy(priceOracle);
  await adapter.waitForDeployment();
  return adapter;
}
async function deploySidioraFeedAdapter({
  poolRegistry,
  minLiquidityThreshold,
  admin,
  signer,
} = {}) {
  if (!poolRegistry) {
    throw new Error("[meta-ag/fixtures] deploySidioraFeedAdapter requires `poolRegistry`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  const threshold = minLiquidityThreshold ?? 10n ** 18n;
  const Factory = await ethers.getContractFactory("SidioraFeedAdapter", deployer);
  const adapter = await Factory.deploy(poolRegistry, threshold, adminAddr);
  await adapter.waitForDeployment();
  return adapter;
}

// --------------------------------------------------------------------------- //
// Phase 3 — Vault v2                                                          //
// --------------------------------------------------------------------------- //
async function deployPECORVault({ weth, tracker, admin, signer } = {}) {
  if (!weth) {
    throw new Error("[meta-ag/fixtures] deployPECORVault requires `weth`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  const trackerAddr = tracker ?? ethers.ZeroAddress;
  return deployUUPSProxy({
    factoryName: "PECORVault",
    initializer: "initialize",
    initArgs: [weth, trackerAddr, adminAddr],
    signer: deployer,
  });
}

// --------------------------------------------------------------------------- //
// Phase 4 — Engine                                                            //
// --------------------------------------------------------------------------- //
async function deployPECOR({
  priceOracle,
  vault,
  weth,
  tracker,
  admin,
  signer,
} = {}) {
  if (!priceOracle) {
    throw new Error("[meta-ag/fixtures] deployPECOR requires `priceOracle`");
  }
  if (!vault) {
    throw new Error("[meta-ag/fixtures] deployPECOR requires `vault`");
  }
  if (!weth) {
    throw new Error("[meta-ag/fixtures] deployPECOR requires `weth`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  const trackerAddr = tracker ?? ethers.ZeroAddress;
  return deployUUPSProxy({
    factoryName: "PECOR",
    initializer: "initialize",
    initArgs: [priceOracle, vault, weth, trackerAddr, adminAddr],
    signer: deployer,
  });
}
async function deployPECOROrders({
  priceOracle,
  vault,
  tracker,
  admin,
  signer,
} = {}) {
  if (!priceOracle) {
    throw new Error("[meta-ag/fixtures] deployPECOROrders requires `priceOracle`");
  }
  if (!vault) {
    throw new Error("[meta-ag/fixtures] deployPECOROrders requires `vault`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  const trackerAddr = tracker ?? ethers.ZeroAddress;
  return deployUUPSProxy({
    factoryName: "PECOROrders",
    initializer: "initialize",
    initArgs: [priceOracle, vault, trackerAddr, adminAddr],
    signer: deployer,
  });
}

// --------------------------------------------------------------------------- //
// Phase 5 — Router adapters                                                   //
// --------------------------------------------------------------------------- //
async function deployVaultAdapter({
  vault,
  priceOracle,
  feeBps,
  feeCollector,
  admin,
  signer,
} = {}) {
  if (!vault) {
    throw new Error("[meta-ag/fixtures] deployVaultAdapter requires `vault`");
  }
  if (!priceOracle) {
    throw new Error("[meta-ag/fixtures] deployVaultAdapter requires `priceOracle`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  const collectorAddr = feeCollector ?? adminAddr;
  const Factory = await ethers.getContractFactory("VaultAdapter", deployer);
  const adapter = await Factory.deploy(
    vault,
    priceOracle,
    feeBps ?? 20n,
    collectorAddr,
    adminAddr
  );
  await adapter.waitForDeployment();
  return adapter;
}
async function deploySidioraAdapter({
  poolRegistry,
  quoter,
  sidioraRouter,
  usdl,
  admin,
  signer,
} = {}) {
  if (!poolRegistry) {
    throw new Error("[meta-ag/fixtures] deploySidioraAdapter requires `poolRegistry`");
  }
  if (!quoter) {
    throw new Error("[meta-ag/fixtures] deploySidioraAdapter requires `quoter`");
  }
  if (!sidioraRouter) {
    throw new Error("[meta-ag/fixtures] deploySidioraAdapter requires `sidioraRouter`");
  }
  if (!usdl) {
    throw new Error("[meta-ag/fixtures] deploySidioraAdapter requires `usdl`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  const Factory = await ethers.getContractFactory("SidioraAdapter", deployer);
  const adapter = await Factory.deploy(
    poolRegistry,
    quoter,
    sidioraRouter,
    usdl,
    adminAddr
  );
  await adapter.waitForDeployment();
  return adapter;
}

// --------------------------------------------------------------------------- //
// Phase 6 — MetaAGRouter                                                      //
// --------------------------------------------------------------------------- //
async function deployMetaAGRouter({
  oracleHub,
  maxOracleSanityDeviation,
  admin,
  signer,
} = {}) {
  if (!oracleHub) {
    throw new Error("[meta-ag/fixtures] deployMetaAGRouter requires `oracleHub`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  return deployUUPSProxy({
    factoryName: "MetaAGRouter",
    initializer: "initialize",
    initArgs: [oracleHub, maxOracleSanityDeviation ?? 500n, adminAddr],
    signer: deployer,
  });
}

// --------------------------------------------------------------------------- //
// Phase 7 — Quoter + Analytics                                                //
// --------------------------------------------------------------------------- //
async function deployMetaAGQuoter({
  priceOracle,
  vault,
  weth,
  pecor,
  admin,
  signer,
} = {}) {
  if (!priceOracle) {
    throw new Error("[meta-ag/fixtures] deployMetaAGQuoter requires `priceOracle`");
  }
  if (!vault) {
    throw new Error("[meta-ag/fixtures] deployMetaAGQuoter requires `vault`");
  }
  if (!weth) {
    throw new Error("[meta-ag/fixtures] deployMetaAGQuoter requires `weth`");
  }
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  const pecorAddr = pecor ?? ethers.ZeroAddress;
  return deployUUPSProxy({
    factoryName: "MetaAGQuoter",
    initializer: "initialize",
    initArgs: [priceOracle, vault, weth, pecorAddr, adminAddr],
    signer: deployer,
  });
}
async function deployTransactionTracker({ admin, signer } = {}) {
  const deployer = signer ?? (await ethers.getSigners())[0];
  const adminAddr = admin ?? deployer.address;
  return deployUUPSProxy({
    factoryName: "TransactionTracker",
    initializer: "initialize",
    initArgs: [adminAddr],
    signer: deployer,
  });
}

// --------------------------------------------------------------------------- //
// Top-level composition: deployPecorFixture                                   //
//                                                                             //
// Returns the entire wired stack for integration tests (Phase 8+).            //
//                                                                             //
// Composition order (mirrors spec §11 build order):                           //
//   PriceOracle → OracleHub → TransactionTracker → PECORVault →               //
//   PECOR → PECOROrders → PriceOracleAdapter → SidioraFeedAdapter →           //
//   VaultAdapter → SidioraAdapter (over Sidiora-API mocks) →                  //
//   MetaAGRouter → MetaAGQuoter                                               //
//                                                                             //
// Wires every role required for end-to-end swaps:                             //
//   - PriceOracle.RELAYER_ROLE             → relayer signer                   //
//   - PECORVault.OPERATOR_ROLE             → PECOR + PECOROrders + VaultAdapter //
//   - TransactionTracker.EMITTER_ROLE      → PECOR + PECOROrders + MetaAGRouter //
//   - PECOROrders.KEEPER_ROLE              → keeper signer                    //
//   - OracleHub: registers PriceOracleAdapter (priority 10) + SidioraFeedAdapter (priority 20) //
//   - MetaAGRouter: registers VaultAdapter + SidioraAdapter                   //
//                                                                             //
// Tokens, prices, reserves, and Sidiora pool keys are deterministic so each   //
// integration test starts from the same canonical state. Per-test overrides   //
// can still be layered on top of the returned wiring.                         //
// --------------------------------------------------------------------------- //
async function deployPecorFixture({
  signer,
  oracleSanityDeviation,
} = {}) {
  const ethersHelpers = ethers;
  const ZERO = ethersHelpers.ZeroAddress;
  const [deployer, user, recipient, relayer, keeper, feeCollector, other] =
    await ethersHelpers.getSigners();
  const adminSigner = signer ?? deployer;
  const admin = adminSigner.address;

  // Top admin's native balance back to 10k ETH. Each fixture call wraps
  // VAULT_WPAX via weth.deposit, which depletes the signer across repeated
  // beforeEach invocations in long test files. This keeps the fixture
  // idempotent without forcing callers to use snapshot/revert.
  const { network } = require("hardhat");
  await network.provider.send("hardhat_setBalance", [
    admin,
    "0x21E19E0C9BAB2400000", // 10,000 ether
  ]);

  // ------------------------------------------------------------------------- //
  // 1) Deploy mocks: WETH + 6 ERC20 tokens + Sidiora-API surface              //
  // ------------------------------------------------------------------------- //
  const WETH = await ethersHelpers.getContractFactory("MockWETH9", adminSigner);
  const weth = await WETH.deploy();
  await weth.waitForDeployment();

  async function deployERC20(name, symbol, decimals = 18) {
    const ERC20 = await ethersHelpers.getContractFactory(
      "MockStandardERC20",
      adminSigner
    );
    const t = await ERC20.deploy(name, symbol, decimals);
    await t.waitForDeployment();
    return t;
  }

  const usdl = await deployERC20("Sidiora USDL", "USDL", 18);
  const usdc = await deployERC20("Mock USD Coin", "USDC", 18);
  const usdt = await deployERC20("Mock USD Tether", "USDT", 18);
  const wpax = weth; // WPAX is the wrapped-native rail; same contract as WETH for tests
  const sidA = await deployERC20("Sidiora Token A", "sidA", 18);
  const sidB = await deployERC20("Sidiora Token B", "sidB", 18);

  const SidioraRouterMock = await ethersHelpers.getContractFactory(
    "MockSidioraRouter",
    adminSigner
  );
  const sidioraRouterMock = await SidioraRouterMock.deploy();
  await sidioraRouterMock.waitForDeployment();
  await sidioraRouterMock.setUsdl(usdl.target);

  const SidioraQuoterMock = await ethersHelpers.getContractFactory(
    "MockSidioraQuoter",
    adminSigner
  );
  const sidioraQuoterMock = await SidioraQuoterMock.deploy();
  await sidioraQuoterMock.waitForDeployment();

  const SidioraRegistryMock = await ethersHelpers.getContractFactory(
    "MockSidioraPoolRegistry",
    adminSigner
  );
  const sidioraRegistryMock = await SidioraRegistryMock.deploy();
  await sidioraRegistryMock.waitForDeployment();

  // Configurable pool contracts provide real ABI/reserve/optical probes for
  // focused staged adapter tests. Production execution is covered separately
  // by remediation-real-sidiora.test.js using the actual launchpad stack.
  const SidioraPoolMock = await ethersHelpers.getContractFactory("MockSidioraPool", adminSigner);
  const poolAMock = await SidioraPoolMock.deploy();
  const poolBMock = await SidioraPoolMock.deploy();
  const poolA = poolAMock.target;
  const poolB = poolBMock.target;
  await poolAMock.setPrice(10n ** 18n / 2n);
  await poolBMock.setPrice(3n * 10n ** 18n / 2n);
  for (const pool of [poolAMock, poolBMock]) {
    await pool.setReserves(10000n * 10n ** 18n, 10000000n * 10n ** 18n, 10000000n * 10n ** 18n);
  }
  await sidioraRegistryMock.setPoolByToken(sidA.target, poolA);
  await sidioraRegistryMock.setPoolByToken(sidB.target, poolB);

  // ------------------------------------------------------------------------- //
  // 2) Oracle stack                                                           //
  // ------------------------------------------------------------------------- //
  const priceOracle = await deployPriceOracle({ admin, signer: adminSigner });
  await priceOracle.connect(adminSigner).setRelayer(relayer.address, true);

  const oracleHub = await deployOracleHub({
    primaryOracle: priceOracle.target,
    deviationBps: oracleSanityDeviation ?? 500n,
    minConfidence: 3000n,
    admin,
    signer: adminSigner,
  });

  const priceOracleAdapter = await deployPriceOracleAdapter({
    priceOracle: priceOracle.target,
    signer: adminSigner,
  });

  const sidioraFeedAdapter = await deploySidioraFeedAdapter({
    poolRegistry: sidioraRegistryMock.target,
    minLiquidityThreshold: 10n ** 18n,
    admin,
    signer: adminSigner,
  });

  await oracleHub
    .connect(adminSigner)
    .registerAdapter(priceOracleAdapter.target, 10);
  await oracleHub
    .connect(adminSigner)
    .registerAdapter(sidioraFeedAdapter.target, 20);

  // Register every token on PriceOracle (heartbeat 60s, deviation 100 bps,
  // wide bounds, 1h staleness). Then push canonical USD prices.
  const ONE = 10n ** 18n;
  const oracleConfig = [60, 100n, ONE / 100n, ONE * 1_000_000n, 3600];
  const oracleTokens = [
    [usdl.target, ONE], // USDL = $1
    [usdc.target, ONE], // USDC = $1
    [usdt.target, ONE], // USDT = $1
    [wpax.target, 2n * ONE], // WPAX = $2
    [sidA.target, ONE / 2n], // sidA = $0.50
    [sidB.target, (3n * ONE) / 2n], // sidB = $1.50
  ];
  for (const [t] of oracleTokens) {
    await priceOracle.connect(adminSigner).registerToken(t, ...oracleConfig);
  }
  await priceOracle
    .connect(relayer)
    .batchUpdatePrices(
      oracleTokens.map(([t]) => t),
      oracleTokens.map(([, p]) => p)
    );

  // Explicit test-only risk opt-in; the independent relayer oracle normalizes USDL.
  await sidioraFeedAdapter.connect(adminSigner).configureSpotReference(true, priceOracle.target, usdl.target);

  // ------------------------------------------------------------------------- //
  // 3) Analytics + vault                                                      //
  // ------------------------------------------------------------------------- //
  const tracker = await deployTransactionTracker({ admin, signer: adminSigner });

  const vault = await deployPECORVault({
    weth: weth.target,
    tracker: tracker.target,
    admin,
    signer: adminSigner,
  });

  // Vault token registry: USDL, USDC, USDT (stablecoins) + WPAX (non-stable).
  // sidA/sidB are NOT registered — they live on Sidiora pools only.
  await vault.connect(adminSigner).registerToken(usdl.target, true);
  await vault.connect(adminSigner).registerToken(usdc.target, true);
  await vault.connect(adminSigner).registerToken(usdt.target, true);
  await vault.connect(adminSigner).registerToken(wpax.target, false);

  // ------------------------------------------------------------------------- //
  // 4) Engine + orders                                                        //
  // ------------------------------------------------------------------------- //
  const pecor = await deployPECOR({
    priceOracle: priceOracle.target,
    vault: vault.target,
    weth: weth.target,
    tracker: tracker.target,
    admin,
    signer: adminSigner,
  });

  const pecorOrders = await deployPECOROrders({
    priceOracle: priceOracle.target,
    vault: vault.target,
    tracker: tracker.target,
    admin,
    signer: adminSigner,
  });

  await vault.connect(adminSigner).setOperator(pecor.target, true);
  await vault.connect(adminSigner).setOperator(pecorOrders.target, true);
  await pecorOrders.connect(adminSigner).setKeeper(keeper.address, true);

  // ------------------------------------------------------------------------- //
  // 5) Adapters                                                               //
  // ------------------------------------------------------------------------- //
  const vaultAdapter = await deployVaultAdapter({
    vault: vault.target,
    priceOracle: priceOracle.target,
    feeBps: 0n, // spec Q6: 0 bps at deploy; integration tests override per-scenario
    feeCollector: feeCollector.address,
    admin,
    signer: adminSigner,
  });
  await vault.connect(adminSigner).setOperator(vaultAdapter.target, true);

  const sidioraAdapter = await deploySidioraAdapter({
    poolRegistry: sidioraRegistryMock.target,
    quoter: sidioraQuoterMock.target,
    sidioraRouter: sidioraRouterMock.target,
    usdl: usdl.target,
    admin,
    signer: adminSigner,
  });

  // ------------------------------------------------------------------------- //
  // 6) Router + quoter                                                        //
  // ------------------------------------------------------------------------- //
  const router = await deployMetaAGRouter({
    oracleHub: oracleHub.target,
    maxOracleSanityDeviation: oracleSanityDeviation ?? 500n,
    admin,
    signer: adminSigner,
  });

  const quoter = await deployMetaAGQuoter({
    priceOracle: priceOracle.target,
    vault: vault.target,
    weth: weth.target,
    pecor: pecor.target,
    admin,
    signer: adminSigner,
  });

  // Adapter registry — order matters for getBestQuote tie-breaks (first wins).
  await router.connect(adminSigner).registerAdapter(vaultAdapter.target);
  await router.connect(adminSigner).registerAdapter(sidioraAdapter.target);

  // ------------------------------------------------------------------------- //
  // 7) Tracker emitter wiring                                                 //
  // ------------------------------------------------------------------------- //
  await tracker.connect(adminSigner).setAuthorizedEmitter(pecor.target, true);
  await tracker
    .connect(adminSigner)
    .setAuthorizedEmitter(pecorOrders.target, true);
  await tracker.connect(adminSigner).setAuthorizedEmitter(router.target, true);

  // ------------------------------------------------------------------------- //
  // 8) Liquidity seeding                                                      //
  // ------------------------------------------------------------------------- //
  // Vault reserves: 1M USDL/USDC/USDT, 1k WPAX. Big enough that no
  // integration scenario hits InsufficientLiquidity by accident, while
  // staying inside hardhat's default 10k ETH signer balance for WPAX wrap.
  const VAULT_STABLE = 1_000_000n * ONE;
  const VAULT_WPAX = 1_000n * ONE;

  for (const stable of [usdl, usdc, usdt]) {
    await stable.mint(adminSigner.address, VAULT_STABLE);
    await stable.connect(adminSigner).approve(vault.target, VAULT_STABLE);
    await vault.connect(adminSigner).deposit(stable.target, VAULT_STABLE);
  }

  // WPAX into vault: wrap native then deposit.
  await weth.connect(adminSigner).deposit({ value: VAULT_WPAX });
  await weth.connect(adminSigner).approve(vault.target, VAULT_WPAX);
  await vault.connect(adminSigner).deposit(wpax.target, VAULT_WPAX);

  // Sidiora router (mock) pre-funded with each token so any buy/sell/multihop
  // payout path can settle. The mock honors `transferFrom` from the adapter
  // and `transfer` outbound — same wire as the live IRouter.
  const SIDIORA_ROUTER_LIQ = 1_000_000n * ONE;
  for (const t of [usdl, sidA, sidB]) {
    await t.mint(sidioraRouterMock.target, SIDIORA_ROUTER_LIQ);
  }

  // ------------------------------------------------------------------------- //
  // 9) Return wiring                                                          //
  // ------------------------------------------------------------------------- //
  return {
    signers: { admin: adminSigner, user, recipient, relayer, keeper, feeCollector, other },
    tokens: { usdl, usdc, usdt, wpax, weth, sidA, sidB },
    pools: { poolA, poolB },
    mocks: {
      sidioraRouter: sidioraRouterMock,
      sidioraQuoter: sidioraQuoterMock,
      sidioraRegistry: sidioraRegistryMock,
    },
    oracle: priceOracle,
    hub: oracleHub,
    priceOracleAdapter,
    sidioraFeedAdapter,
    tracker,
    vault,
    pecor,
    pecorOrders,
    vaultAdapter,
    sidioraAdapter,
    router,
    quoter,
    constants: {
      ONE,
      VAULT_STABLE,
      VAULT_WPAX,
      SIDIORA_ROUTER_LIQ,
      ZERO,
    },
  };
}

module.exports = {
  NotImplementedError,
  deployUUPSProxy,
  deployPriceOracle,
  deployOracleHub,
  deployPriceOracleAdapter,
  deploySidioraFeedAdapter,
  deployPECORVault,
  deployPECOR,
  deployPECOROrders,
  deployVaultAdapter,
  deploySidioraAdapter,
  deployMetaAGRouter,
  deployMetaAGQuoter,
  deployTransactionTracker,
  deployPecorFixture,
  PECOR_ROLES,
  LIVE_ADDRESSES,
};
