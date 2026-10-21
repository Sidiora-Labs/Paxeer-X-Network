/**
 * Sidiora Meta-AG + PECOR — Live On-Chain E2E Test
 *
 * Runs a full integration pass against the live localhost deployment produced
 * by:
 *   1. scripts/meta-ag/deploy-tokens.js
 *   2. scripts/deploy.js
 *   3. scripts/meta-ag/deploy-pecor-meta-ag.js
 *
 * Reads addresses from deployments/{NETWORK_TYPE}-addresses.json and
 * deployments/{NETWORK_TYPE}-tokens.json. Tests are sequenced; later tests
 * depend on state from earlier ones (a pool must be created before we can
 * route through SidioraAdapter).
 *
 * Usage:
 *   npx hardhat run scripts/meta-ag/onchain-test-pecor.js --network localhost
 */

const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

const COLORS = {
  reset: "\x1b[0m",
  green: "\x1b[32m",
  red: "\x1b[31m",
  yellow: "\x1b[33m",
  cyan: "\x1b[36m",
  gray: "\x1b[90m",
};
const c = (col, s) => `${COLORS[col]}${s}${COLORS.reset}`;
const step = (s) => console.log(`\n${c("yellow", `── ${s} ──`)}`);
const okm = (s) => console.log(`  ${c("green", "✓")} ${s}`);
const failm = (s) => console.log(`  ${c("red", "✗")} ${s}`);
const info = (s) => console.log(`  ${c("gray", "·")} ${s}`);

// PermitParams zero literal — bypasses permit flow, relies on pre-approval.
const ZERO_PERMIT = {
  value: 0n,
  deadline: 0n,
  v: 0,
  r: "0x" + "00".repeat(32),
  s: "0x" + "00".repeat(32),
};

// Loose helpers — fail the test, don't kill the run.
class TestContext {
  constructor() {
    this.passed = 0;
    this.failed = 0;
    this.errors = [];
  }
  assert(cond, name, detail = "") {
    if (cond) {
      okm(`${name}${detail ? ` — ${c("gray", detail)}` : ""}`);
      this.passed++;
      return true;
    }
    failm(`${name}${detail ? ` — ${c("red", detail)}` : ""}`);
    this.failed++;
    this.errors.push(name);
    return false;
  }
  approxEq(actual, expected, tolerance, name) {
    const diff = actual > expected ? actual - expected : expected - actual;
    return this.assert(
      diff <= tolerance,
      name,
      `got=${actual} expected=${expected} diff=${diff} tol=${tolerance}`
    );
  }
}

async function main() {
  const [deployer] = await ethers.getSigners();
  const net = await ethers.provider.getNetwork();
  const chainId = Number(net.chainId);
  const NETWORK_TYPE =
    require("../lib/operations").networkName();

  const baseDir = path.join(__dirname, "..", "..", "deployments");
  const A = JSON.parse(fs.readFileSync(path.join(baseDir, `${NETWORK_TYPE}-addresses.json`), "utf8"));
  await require("../lib/operations").validateManifest(A, ethers.provider);
  const T = JSON.parse(fs.readFileSync(path.join(baseDir, `${NETWORK_TYPE}-tokens.json`), "utf8")).tokens;

  console.log(`\n${c("yellow", "╔════════════════════════════════════════════════════════════╗")}`);
  console.log(`${c("yellow", "║ Sidiora Meta-AG + PECOR — Live On-Chain E2E Test           ║")}`);
  console.log(`${c("yellow", "╚════════════════════════════════════════════════════════════╝")}`);
  info(`Deployer: ${deployer.address}`);
  info(`Network: ${NETWORK_TYPE} (chainId ${chainId})`);
  info(`Balance: ${ethers.formatEther(await ethers.provider.getBalance(deployer.address))} PAX`);

  const ctx = new TestContext();

  // Token contracts
  const erc20Abi = [
    "function balanceOf(address) view returns (uint256)",
    "function transfer(address,uint256) returns (bool)",
    "function approve(address,uint256) returns (bool)",
    "function allowance(address,address) view returns (uint256)",
    "function decimals() view returns (uint8)",
    "function symbol() view returns (string)",
    "function totalSupply() view returns (uint256)",
  ];
  const usdl = new ethers.Contract(T.USDL.address, erc20Abi, deployer);
  const usdc = new ethers.Contract(T.USDC.address, erc20Abi, deployer);
  const wpax = new ethers.Contract(A.WPAX, [...erc20Abi, "function deposit() payable", "function withdraw(uint256)"], deployer);

  // Sidiora contracts
  const sidRouter = await ethers.getContractAt("Router", A.Router_proxy);
  const sidQuoter = await ethers.getContractAt("Quoter", A.Quoter_proxy);
  const sidReg = await ethers.getContractAt("PoolRegistry", A.PoolRegistry_proxy);
  const sidConfig = await ethers.getContractAt("ProtocolConfig", A.ProtocolConfig_proxy);

  // Meta-AG / PECOR contracts
  const metaRouter = await ethers.getContractAt("MetaAGRouter", A.MetaAGRouter_proxy);
  const vault = await ethers.getContractAt("PECORVault", A.PECORVault_proxy);
  const oracle = await ethers.getContractAt("PriceOracle", A.PriceOracle_proxy);
  const hub = await ethers.getContractAt("OracleHub", A.OracleHub_proxy);
  const vaultAdapter = await ethers.getContractAt("VaultAdapter", A.VaultAdapter);
  const sidioraAdapter = await ethers.getContractAt("SidioraAdapter", A.SidioraAdapter);
  const priceOracleAdapter = await ethers.getContractAt("PriceOracleAdapter", A.PriceOracleAdapter);
  const sidioraFeedAdapter = await ethers.getContractAt("SidioraFeedAdapter", A.SidioraFeedAdapter);

  const usdlDec = await usdl.decimals();
  const usdcDec = await usdc.decimals();
  info(`USDL decimals: ${usdlDec}, USDC decimals: ${usdcDec}`);
  const ONE_USDL = 10n ** BigInt(usdlDec);
  const ONE_USDC = 10n ** BigInt(usdcDec);
  const ONE18 = 10n ** 18n;

  // Cache expected adapter IDs (keccak256 of the names)
  const ADAPTER_ID_VAULT = ethers.keccak256(ethers.toUtf8Bytes("PECORVault.v1"));
  const ADAPTER_ID_SIDIORA = ethers.keccak256(ethers.toUtf8Bytes("SidioraAMM.v1"));

  // =========================================================================
  // Test 1 — Pre-flight: all contracts reachable, adapters wired correctly.
  // =========================================================================
  step("Test 1 — Pre-flight (topology check)");
  try {
    ctx.assert((await usdl.balanceOf(deployer.address)) > 0n, "Deployer holds USDL");
    ctx.assert((await usdc.balanceOf(deployer.address)) > 0n, "Deployer holds USDC");
    ctx.assert(
      (await metaRouter.adapterCount()) === 2n,
      "MetaAGRouter has 2 adapters registered"
    );
    ctx.assert(
      (await vaultAdapter.adapterId()) === ADAPTER_ID_VAULT,
      "VaultAdapter.adapterId == keccak256('PECORVault.v1')"
    );
    ctx.assert(
      (await sidioraAdapter.adapterId()) === ADAPTER_ID_SIDIORA,
      "SidioraAdapter.adapterId == keccak256('SidioraAMM.v1')"
    );
    const usdlPrice = await oracle.getPrice(T.USDL.address);
    ctx.assert(usdlPrice === ONE18, "Oracle USDL price = $1", `got=${usdlPrice}`);
    const wpaxPrice = await oracle.getPrice(A.WPAX);
    // WPAX is the wrapped native — its USD price is whatever the live relayer
    // pushed, not a canonical $1. Assert non-zero (relayer is alive) and within
    // the registered bounds [$0.01, $1,000,000].
    const minPrice = ONE18 / 100n;
    const maxPrice = ONE18 * 1_000_000n;
    ctx.assert(
      wpaxPrice > 0n && wpaxPrice >= minPrice && wpaxPrice <= maxPrice,
      "Oracle WPAX price set & in registered bounds",
      `got=${wpaxPrice} ($${Number(wpaxPrice) / 1e18})`
    );
  } catch (e) {
    failm(`Pre-flight error: ${e.shortMessage || e.message}`);
    ctx.failed++;
  }


  // =========================================================================
  // Test 3 — Meta-AG vault-only swap (USDL → USDC via VaultAdapter)
  //          Witnesses the Session-9 VaultAdapter port fix end-to-end on
  //          the live deployment.
  // =========================================================================
  step("Test 3 — Meta-AG swapBestRoute: USDL → USDC via VaultAdapter");
  try {
    const amountIn = 1_000n * ONE_USDL; // 1,000 USDL
    // At $1:$1 with matching decimals, grossOut == amountIn (rebased to USDC unit).
    // Fetch live feeBps so the test tracks adapter config changes.
    const feeBps = await vaultAdapter.feeBps();
    const BPS_DENOM = 10_000n;
    const grossOut = 1_000n * ONE_USDC; // 1,000 USDC
    const feeAmount = (grossOut * feeBps) / BPS_DENOM;
    const netOut = grossOut - feeAmount; // what the recipient actually receives

    await (await usdl.approve(metaRouter.target, amountIn)).wait();
    const usdlBefore = await usdl.balanceOf(deployer.address);
    const usdcBefore = await usdc.balanceOf(deployer.address);
    const usdlReservesBefore = await vault.getReserves(T.USDL.address);
    const usdcReservesBefore = await vault.getReserves(T.USDC.address);

    const tx = await metaRouter.swapBestRoute(
      T.USDL.address,
      T.USDC.address,
      amountIn,
      netOut, // minAmountOut must match what the recipient gets, not gross
      0
    );
    const receipt = await tx.wait();
    okm(`Swap landed in block ${receipt.blockNumber} (gas=${receipt.gasUsed}, feeBps=${feeBps})`);

    const usdlAfter = await usdl.balanceOf(deployer.address);
    const usdcAfter = await usdc.balanceOf(deployer.address);

    ctx.assert(usdlBefore - usdlAfter === amountIn, "USDL debited by amountIn");
    ctx.assert(usdcAfter - usdcBefore === netOut, "USDC credited by netOut", `got=${usdcAfter - usdcBefore} expected=${netOut}`);

    // Vault accounting: USDL reserves ↑ by amountIn (deposit funnel),
    // USDC reserves ↓ by grossOut (netOut to recipient + feeAmount to feeCollector).
    ctx.assert(
      (await vault.getReserves(T.USDL.address)) - usdlReservesBefore === amountIn,
      "Vault USDL reserves increased by amountIn"
    );
    ctx.assert(
      usdcReservesBefore - (await vault.getReserves(T.USDC.address)) === grossOut,
      "Vault USDC reserves decreased by grossOut (netOut + fee)"
    );

    // S9 invariants — router + adapter clean
    ctx.assert(
      (await usdl.balanceOf(metaRouter.target)) === 0n,
      "Router holds no USDL dust"
    );
    ctx.assert(
      (await usdc.balanceOf(metaRouter.target)) === 0n,
      "Router holds no USDC dust"
    );
    ctx.assert(
      (await usdl.balanceOf(vaultAdapter.target)) === 0n,
      "VaultAdapter holds no USDL dust"
    );
    ctx.assert(
      (await usdc.balanceOf(vaultAdapter.target)) === 0n,
      "VaultAdapter holds no USDC dust"
    );
    ctx.assert(
      (await usdl.allowance(metaRouter.target, vaultAdapter.target)) === 0n,
      "Router→VaultAdapter USDL allowance = 0 post-swap"
    );
    ctx.assert(
      (await usdl.allowance(vaultAdapter.target, vault.target)) === 0n,
      "VaultAdapter→Vault USDL allowance = 0 post-swap"
    );

    // Verify the BestRouteSwap event selected the VaultAdapter
    const iface = metaRouter.interface;
    const parsed = receipt.logs
      .map((log) => {
        try {
          return iface.parseLog({ topics: [...log.topics], data: log.data });
        } catch {
          return null;
        }
      })
      .find((l) => l && l.name === "BestRouteSwap");
    ctx.assert(
      parsed && parsed.args.adapterId === ADAPTER_ID_VAULT,
      "BestRouteSwap event selected VaultAdapter"
    );
  } catch (e) {
    failm(`Vault swap error: ${e.shortMessage || e.message}`);
    ctx.failed++;
  }

  // =========================================================================
  // Test 4 — Create a Sidiora market (sidToken + pool).
  //          Uses the live Router.createMarket (4-arg, no permit).
  // =========================================================================
  step("Test 4 — Create Sidiora market via live Router.createMarket");
  let sidToken, sidPool, sidTokenContract;
  try {
    const creationFee = await sidConfig.creationFee();
    info(`Creation fee: ${ethers.formatUnits(creationFee, usdlDec)} USDL`);
    await (await usdl.approve(sidRouter.target, creationFee)).wait();

    const suffix = Date.now().toString().slice(-6);
    const name = `PECORLive${suffix}`;
    const symbol = `PLT${suffix}`;
    const tx = await sidRouter["createMarket(string,string,uint8,address)"](
      name,
      symbol,
      0, // feeStrategy
      ethers.ZeroAddress // optical
    );
    const receipt = await tx.wait();
    okm(`createMarket tx landed (gas=${receipt.gasUsed})`);

    // Parse the MarketCreated event via the Factory
    const factory = await ethers.getContractAt("SidioraFactory", A.SidioraFactory_proxy);
    const parsed = receipt.logs
      .map((log) => {
        try {
          return factory.interface.parseLog({ topics: [...log.topics], data: log.data });
        } catch {
          return null;
        }
      })
      .find((l) => l && l.name === "MarketCreated");
    ctx.assert(!!parsed, "MarketCreated event emitted");
    if (parsed) {
      sidToken = parsed.args.token;
      sidPool = parsed.args.pool;
      info(`  sidToken: ${sidToken}`);
      info(`  sidPool:  ${sidPool}`);
    }

    // PoolRegistry should know the pool now.
    const count = await sidReg.getPoolCount();
    ctx.assert(count >= 1n, `PoolRegistry.getPoolCount() = ${count}`);
    const byToken = await sidReg.getPoolByToken(sidToken);
    ctx.assert(byToken.toLowerCase() === sidPool.toLowerCase(), "PoolRegistry.getPoolByToken matches");

    // Bind token contract for later tests
    sidTokenContract = new ethers.Contract(sidToken, erc20Abi, deployer);
    const tokenDec = await sidTokenContract.decimals();
    const tokenSupply = await sidTokenContract.totalSupply();
    info(`  sidToken decimals: ${tokenDec}, totalSupply: ${tokenSupply}`);
  } catch (e) {
    failm(`createMarket error: ${e.shortMessage || e.message}`);
    ctx.failed++;
  }

  // =========================================================================
  // Test 5 — SidioraFeedAdapter sees the new pool and returns a price.
  //          (Exercised via OracleHub.getAggregatedPrice for sidToken.)
  // =========================================================================
  step("Test 5 — SidioraFeedAdapter picks up the new pool");
  try {
    if (!sidToken) throw new Error("sidToken not created in Test 4");

    // SidioraFeedAdapter.getFeedPrice returns FeedPrice { price, timestamp, confidence, sourceId }
    // Availability convention: confidence > 0 (per IDataFeedAdapter spec — MUST NOT revert).
    const feed = await sidioraFeedAdapter.getFeedPrice(sidToken);
    ctx.assert(feed.price > 0n, "SidioraFeedAdapter: non-zero price", `price=${feed.price}`);
    ctx.assert(feed.confidence > 0n, "SidioraFeedAdapter: confidence > 0 (adapter surfaces the feed)", `conf=${feed.confidence}`);
    ctx.assert(feed.timestamp > 0n, "SidioraFeedAdapter: timestamp stamped");
    info(`  SidioraFeed: price=${feed.price} confidence=${feed.confidence} ts=${feed.timestamp}`);
  } catch (e) {
    failm(`SidioraFeed error: ${e.shortMessage || e.message}`);
    ctx.failed++;
  }

  // =========================================================================
  // Test 6 — Direct Sidiora BUY via live Router (baseline — no Meta-AG).
  //          Validates the live Sidiora stack itself works end-to-end.
  // =========================================================================
  step("Test 6 — Direct Sidiora buy (USDL → sidToken via Sidiora Router)");
  try {
    if (!sidPool) throw new Error("sidPool not created in Test 4");

    const amountIn = 10n * ONE_USDL; // 10 USDL
    await (await usdl.approve(sidRouter.target, amountIn)).wait();

    const balBefore = await sidTokenContract.balanceOf(deployer.address);
    // Sidiora Router requires a real deadline (unlike Meta-AG which treats
    // 0 as "no deadline"). One hour forward is plenty for a local tx.
    const block = await ethers.provider.getBlock("latest");
    const deadline = BigInt(block.timestamp) + 3600n;
    const tx = await sidRouter.buy(
      sidPool,
      amountIn,
      0n,
      deadline,
      ZERO_PERMIT
    );
    await tx.wait();
    const balAfter = await sidTokenContract.balanceOf(deployer.address);
    const received = balAfter - balBefore;
    ctx.assert(received > 0n, "Direct Sidiora buy credited sidToken", `received=${received}`);
    info(`  received: ${received}`);
  } catch (e) {
    failm(`Direct buy error: ${e.shortMessage || e.message}`);
    ctx.failed++;
  }

  // =========================================================================
  // Test 7 — Meta-AG → SidioraAdapter BUY (USDL → sidToken)
  //          Witnesses the SidioraAdapter against the LIVE Sidiora Router +
  //          Quoter + PoolRegistry (no mocks).
  // =========================================================================
  step("Test 7 — Meta-AG swapBestRoute: USDL → sidToken via SidioraAdapter");
  try {
    if (!sidToken) throw new Error("sidToken not created in Test 4");

    const amountIn = 10n * ONE_USDL;
    await (await usdl.approve(metaRouter.target, amountIn)).wait();

    const balBefore = await sidTokenContract.balanceOf(deployer.address);
    const usdlBefore = await usdl.balanceOf(deployer.address);

    const tx = await metaRouter.swapBestRoute(
      T.USDL.address,
      sidToken,
      amountIn,
      0n, // accept any output — we trust the pool math for this sanity check
      0
    );
    const receipt = await tx.wait();
    okm(`Swap landed (gas=${receipt.gasUsed})`);

    const balAfter = await sidTokenContract.balanceOf(deployer.address);
    const received = balAfter - balBefore;
    ctx.assert(received > 0n, "Meta-AG Sidiora buy credited sidToken", `received=${received}`);
    ctx.assert(
      usdlBefore - (await usdl.balanceOf(deployer.address)) === amountIn,
      "USDL debited by amountIn"
    );

    // Verify the BestRouteSwap event selected the SidioraAdapter
    const parsed = receipt.logs
      .map((log) => {
        try {
          return metaRouter.interface.parseLog({ topics: [...log.topics], data: log.data });
        } catch {
          return null;
        }
      })
      .find((l) => l && l.name === "BestRouteSwap");
    ctx.assert(
      parsed && parsed.args.adapterId === ADAPTER_ID_SIDIORA,
      "BestRouteSwap event selected SidioraAdapter"
    );

    // S9 invariants
    ctx.assert(
      (await usdl.balanceOf(metaRouter.target)) === 0n,
      "Router holds no USDL dust"
    );
    ctx.assert(
      (await usdl.balanceOf(sidioraAdapter.target)) === 0n,
      "SidioraAdapter holds no USDL dust"
    );
    ctx.assert(
      (await usdl.allowance(metaRouter.target, sidioraAdapter.target)) === 0n,
      "Router→SidioraAdapter USDL allowance = 0 post-swap"
    );
    ctx.assert(
      (await usdl.allowance(sidioraAdapter.target, sidRouter.target)) === 0n,
      "SidioraAdapter→SidioraRouter USDL allowance = 0 post-swap"
    );
  } catch (e) {
    failm(`Meta-AG Sidiora BUY error: ${e.shortMessage || e.message}`);
    ctx.failed++;
  }

  // =========================================================================
  // Test 8 — Meta-AG → SidioraAdapter SELL (sidToken → USDL)
  // =========================================================================
  step("Test 8 — Meta-AG swapBestRoute: sidToken → USDL via SidioraAdapter");
  try {
    if (!sidToken) throw new Error("sidToken not created in Test 4");

    const balAvail = await sidTokenContract.balanceOf(deployer.address);
    const amountIn = balAvail / 10n; // sell 10% of holdings
    ctx.assert(amountIn > 0n, "Deployer has sidToken to sell", `bal=${balAvail}`);
    await (await sidTokenContract.approve(metaRouter.target, amountIn)).wait();

    const usdlBefore = await usdl.balanceOf(deployer.address);

    const tx = await metaRouter.swapBestRoute(
      sidToken,
      T.USDL.address,
      amountIn,
      0n,
      0
    );
    await tx.wait();

    const usdlAfter = await usdl.balanceOf(deployer.address);
    const received = usdlAfter - usdlBefore;
    ctx.assert(received > 0n, "Meta-AG Sidiora sell credited USDL", `received=${received}`);

    // S9 post-swap
    ctx.assert(
      (await sidTokenContract.balanceOf(metaRouter.target)) === 0n,
      "Router holds no sidToken dust"
    );
    ctx.assert(
      (await sidTokenContract.balanceOf(sidioraAdapter.target)) === 0n,
      "SidioraAdapter holds no sidToken dust"
    );
  } catch (e) {
    failm(`Meta-AG Sidiora SELL error: ${e.shortMessage || e.message}`);
    ctx.failed++;
  }

  // =========================================================================
  // Test 9 — Meta-AG multi-hop: USDC → USDL → sidToken via [VAULT, SIDIORA]
  //          Chains the VaultAdapter port-fix and SidioraAdapter in a single
  //          atomic transaction.
  // =========================================================================
  step("Test 9 — Meta-AG swapMultiHop: USDC → USDL → sidToken");
  try {
    if (!sidToken) throw new Error("sidToken not created in Test 4");

    const amountIn = 50n * ONE_USDC;
    await (await usdc.approve(metaRouter.target, amountIn)).wait();

    const sidBalBefore = await sidTokenContract.balanceOf(deployer.address);
    const usdcBefore = await usdc.balanceOf(deployer.address);

    // Vault hop applies feeBps; compute expected net USDL output for minAmountOut.
    const vaultFeeBps = await vaultAdapter.feeBps();
    const vaultGrossUsdl = 50n * ONE_USDL; // 1:1 at $1 each, same decimals
    const vaultNetUsdl = vaultGrossUsdl - (vaultGrossUsdl * vaultFeeBps) / 10_000n;

    const hops = [
      {
        adapterId: ADAPTER_ID_VAULT,
        tokenIn: T.USDC.address,
        tokenOut: T.USDL.address,
        minAmountOut: vaultNetUsdl, // accounts for live feeBps
      },
      {
        adapterId: ADAPTER_ID_SIDIORA,
        tokenIn: T.USDL.address,
        tokenOut: sidToken,
        minAmountOut: 0n, // accept any output; pool math is dynamic
      },
    ];

    const tx = await metaRouter.swapMultiHop(hops, amountIn, 0n, 0);
    const receipt = await tx.wait();
    okm(`Multi-hop landed (gas=${receipt.gasUsed})`);

    const sidBalAfter = await sidTokenContract.balanceOf(deployer.address);
    const received = sidBalAfter - sidBalBefore;
    ctx.assert(received > 0n, "Multi-hop credited sidToken", `received=${received}`);
    ctx.assert(
      usdcBefore - (await usdc.balanceOf(deployer.address)) === amountIn,
      "USDC debited by amountIn"
    );

    // Verify MultiHopSwap event
    const parsed = receipt.logs
      .map((log) => {
        try {
          return metaRouter.interface.parseLog({ topics: [...log.topics], data: log.data });
        } catch {
          return null;
        }
      })
      .find((l) => l && l.name === "MultiHopSwap");
    ctx.assert(parsed, "MultiHopSwap event emitted");
    if (parsed) {
      ctx.assert(parsed.args.hops === 2n, "MultiHopSwap.hops = 2");
    }

    // S9 across every hop
    ctx.assert((await usdc.balanceOf(metaRouter.target)) === 0n, "Router USDC dust = 0");
    ctx.assert((await usdl.balanceOf(metaRouter.target)) === 0n, "Router USDL dust = 0");
    ctx.assert((await sidTokenContract.balanceOf(metaRouter.target)) === 0n, "Router sidToken dust = 0");
    ctx.assert(
      (await usdc.allowance(metaRouter.target, vaultAdapter.target)) === 0n,
      "Router→VaultAdapter USDC allowance = 0"
    );
    ctx.assert(
      (await usdl.allowance(metaRouter.target, sidioraAdapter.target)) === 0n,
      "Router→SidioraAdapter USDL allowance = 0"
    );
    ctx.assert(
      (await usdc.allowance(vaultAdapter.target, vault.target)) === 0n,
      "VaultAdapter→Vault USDC allowance = 0"
    );
  } catch (e) {
    failm(`Multi-hop error: ${e.shortMessage || e.message}`);
    ctx.failed++;
  }

  // =========================================================================
  // Test 10 — Governance topology: deployer still holds DEFAULT_ADMIN_ROLE
  //           on every proxy; no other EOA does. (In production Andrew
  //           would transfer these to the Timelock — this test asserts the
  //           baseline before that handoff.)
  // =========================================================================
  step("Test 10 — Governance baseline (DEFAULT_ADMIN_ROLE audit)");
  try {
    const ADMIN_ROLE = "0x" + "00".repeat(32);
    const adminables = [
      { name: "PriceOracle", c: oracle },
      { name: "OracleHub", c: hub },
      { name: "PECORVault", c: vault },
      { name: "MetaAGRouter", c: metaRouter },
      { name: "VaultAdapter", c: vaultAdapter },
      { name: "SidioraAdapter", c: sidioraAdapter },
    ];
    for (const { name, c: ct } of adminables) {
      const has = await ct.hasRole(ADMIN_ROLE, deployer.address);
      ctx.assert(!has, `${name}: deployer bootstrap DEFAULT_ADMIN_ROLE removed`);
      ctx.assert(await ct.hasRole(ADMIN_ROLE, A.Timelock), `${name}: Timelock holds DEFAULT_ADMIN_ROLE`);
    }
    // Random EOA shouldn't
    const randomEOA = ethers.getAddress("0x" + "ab".repeat(20));
    for (const { name, c: ct } of adminables) {
      const has = await ct.hasRole(ADMIN_ROLE, randomEOA);
      ctx.assert(!has, `${name}: random EOA does NOT hold DEFAULT_ADMIN_ROLE`);
    }
  } catch (e) {
    failm(`Governance audit error: ${e.shortMessage || e.message}`);
    ctx.failed++;
  }

  // =========================================================================
  // Summary
  // =========================================================================
  step("Summary");
  const total = ctx.passed + ctx.failed;
  console.log(
    `\n  Total: ${total}    ${c("green", "Passed: " + ctx.passed)}    ${
      ctx.failed > 0 ? c("red", "Failed: " + ctx.failed) : c("gray", "Failed: 0")
    }\n`
  );
  if (ctx.failed > 0 || ctx.errors.length) {
    console.log(`  ${c("red", "Failed assertions:")}`);
    for (const e of ctx.errors) console.log(`    - ${e}`);
    process.exit(1);
  }
  console.log(`  ${c("green", "✓ All on-chain scenarios passed.")}\n`);
}

main()
  .then(() => process.exit(0))
  .catch((e) => {
    console.error(c("reset", "\n❌ Test run crashed:"), e);
    process.exit(1);
  });
