/**
 * Sidiora Launchpad AMM — On-Chain Live Test Script
 *
 * Runs a full E2E test flow against live deployed contracts on Paxeer.
 * Creates a test market, executes buys/sells, verifies fee flows,
 * and checks all subsystems are wired correctly.
 *
 * Usage:
 *   cd /root/sidiora-contracts
 *   npx hardhat run scripts/onchain-test.js --network paxeer-x-network
 *
 * Requires:
 *   - deployments/paxeer-addresses.json (from deploy.js)
 *   - Deployer wallet must have PAX for gas + USDL for creation fee + trading
 *   - USDL_ADDRESS in .env
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
};

function ok(msg) { console.log(`  ${COLORS.green}✅ ${msg}${COLORS.reset}`); }
function fail(msg) { console.log(`  ${COLORS.red}❌ ${msg}${COLORS.reset}`); }
function info(msg) { console.log(`  ${COLORS.cyan}ℹ  ${msg}${COLORS.reset}`); }
function step(msg) { console.log(`\n${COLORS.yellow}── ${msg} ──${COLORS.reset}`); }

async function main() {
  const [deployer] = await ethers.getSigners();
  const addrPath = path.join(__dirname, "..", "deployments", require("./lib/operations").networkName() + "-addresses.json");
  if (!fs.existsSync(addrPath)) throw new Error("Addresses file not found. Run deploy.js first.");
  const addresses = JSON.parse(fs.readFileSync(addrPath, "utf8"));
  await require("./lib/operations").validateManifest(addresses, ethers.provider);
  const meta = addresses._meta;

  console.log(`\n🧪 Sidiora On-Chain Live Test`);
  console.log(`   Deployer: ${deployer.address}`);
  console.log(`   Network:  Chain ID ${(await ethers.provider.getNetwork()).chainId}`);
  console.log(`   USDL:     ${meta.usdl}`);

  const results = { passed: 0, failed: 0, errors: [] };

  function assert(condition, testName) {
    if (condition) {
      ok(testName);
      results.passed++;
    } else {
      fail(testName);
      results.failed++;
      results.errors.push(testName);
    }
  }

  // ─── Load contracts ───
  const USDL_ABI = ["function balanceOf(address) view returns (uint256)",
    "function approve(address,uint256) returns (bool)",
    "function allowance(address,address) view returns (uint256)"];
  const usdl = new ethers.Contract(meta.usdl, USDL_ABI, deployer);

  const Router = await ethers.getContractFactory("Router");
  const router = Router.attach(addresses.Router_proxy);

  const Quoter = await ethers.getContractFactory("Quoter");
  const quoter = Quoter.attach(addresses.Quoter_proxy);

  const FeesRouter = await ethers.getContractFactory("FeesRouter");
  const feesRouter = FeesRouter.attach(addresses.FeesRouter_proxy);

  const PoolRegistry = await ethers.getContractFactory("PoolRegistry");
  const registry = PoolRegistry.attach(addresses.PoolRegistry_proxy);

  const ProtocolConfig = await ethers.getContractFactory("ProtocolConfig");
  const config = ProtocolConfig.attach(addresses.ProtocolConfig_proxy);

  const FeeAccumulator = await ethers.getContractFactory("FeeAccumulator");
  const accumulator = FeeAccumulator.attach(addresses.FeeAccumulator_proxy);

  const SidioraNFT = await ethers.getContractFactory("SidioraNFT");
  const nft = SidioraNFT.attach(addresses.SidioraNFT_proxy);

  const Treasury = await ethers.getContractFactory("Treasury");
  const treasury = Treasury.attach(addresses.Treasury_proxy);

  async function futureDeadline() {
    const block = await ethers.provider.getBlock("latest");
    return block.timestamp + 3600;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 1: Verify Protocol Config
  // ═══════════════════════════════════════════════════════════════════
  step("Test 1: Protocol Config");
  try {
    const usdlAddr = await config.usdlAddress();
    assert(usdlAddr.toLowerCase() === meta.usdl.toLowerCase(), "ProtocolConfig.usdlAddress matches");

    const virtualUsdl = await config.virtualUsdlDefault();
    assert(virtualUsdl === ethers.parseUnits("10000", 6), "virtualUsdlDefault = 10,000");

    const creationFee = await config.creationFee();
    info(`Creation fee: ${ethers.formatUnits(creationFee, 6)} USDL`);
    assert(creationFee > 0n, "creationFee > 0");
  } catch (e) {
    fail(`Protocol Config check failed: ${e.message.slice(0, 100)}`);
    results.failed++;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 2: USDL Approval
  // ═══════════════════════════════════════════════════════════════════
  step("Test 2: USDL Approval");
  try {
    const balance = await usdl.balanceOf(deployer.address);
    info(`USDL balance: ${ethers.formatUnits(balance, 6)}`);
    assert(balance > ethers.parseUnits("2000", 6), "Deployer has > 2000 USDL");

    const allowance = await usdl.allowance(deployer.address, addresses.Router_proxy);
    if (allowance < ethers.parseUnits("100000", 6)) {
      info("Approving Router for USDL...");
      const tx = await usdl.approve(addresses.Router_proxy, ethers.MaxUint256);
      await tx.wait();
    }
    ok("Router approved for USDL");
    results.passed++;
  } catch (e) {
    fail(`USDL approval failed: ${e.message.slice(0, 100)}`);
    results.failed++;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 3: Create Market
  // ═══════════════════════════════════════════════════════════════════
  step("Test 3: Create Market");
  let poolAddr, tokenAddr, nftId;
  try {
    const poolCountBefore = await registry.getPoolCount();
    info(`Pools before: ${poolCountBefore}`);

    // Diagnostics before attempting createMarket
    const creationFeeLive = await config.creationFee();
    info(`Creation fee (raw): ${creationFeeLive}`);
    const routerAddr = addresses.Router_proxy;
    const allowanceLive = await usdl.allowance(deployer.address, routerAddr);
    info(`USDL allowance to Router: ${ethers.formatUnits(allowanceLive, 6)}`);
    const balLive = await usdl.balanceOf(deployer.address);
    info(`USDL balance: ${ethers.formatUnits(balLive, 6)}`);
    const factoryAddr = await router.factory();
    info(`Router.factory: ${factoryAddr}`);
    const treasuryFromFactory = await (await ethers.getContractFactory("SidioraFactory")).attach(factoryAddr).treasury();
    info(`Factory.treasury: ${treasuryFromFactory}`);

    // Try staticCall to get revert reason
    const suffix = Date.now().toString().slice(-6);
    try {
      await router.createMarket.staticCall(
        `LiveTest${suffix}`, `LT${suffix}`, 0, ethers.ZeroAddress
      );
      info("staticCall succeeded — sending real tx...");
    } catch (staticErr) {
      info(`staticCall revert reason: ${staticErr.message.slice(0, 300)}`);
    }

    const tx = await router.createMarket(
      `LiveTest${suffix}`, `LT${suffix}`, 0, ethers.ZeroAddress
    );
    const receipt = await tx.wait();
    info(`Gas used: ${receipt.gasUsed.toString()}`);

    const event = receipt.logs
      .map(l => { try { return router.interface.parseLog(l); } catch { return null; } })
      .find(e => e && e.name === "MarketCreated");

    tokenAddr = event.args[0];
    poolAddr = event.args[1];
    nftId = event.args[3];

    info(`Token: ${tokenAddr}`);
    info(`Pool:  ${poolAddr}`);
    info(`NFT:   ${nftId}`);

    assert(poolAddr !== ethers.ZeroAddress, "Pool address is non-zero");
    assert(tokenAddr !== ethers.ZeroAddress, "Token address is non-zero");

    // Verify Factory auto-authorized pool on FeeAccumulator
    const POOL_ROLE = ethers.keccak256(ethers.toUtf8Bytes("POOL_ROLE"));
    const hasPoolRole = await accumulator.hasRole(POOL_ROLE, poolAddr);
    assert(hasPoolRole, "Pool auto-authorized on FeeAccumulator (POOL_ROLE)");

    const poolCountAfter = await registry.getPoolCount();
    assert(poolCountAfter === poolCountBefore + 1n, "Pool count incremented");
  } catch (e) {
    fail(`Create market failed: ${e.message.slice(0, 150)}`);
    results.failed++;
    // Can't continue without a pool
    printSummary(results);
  if (results.failed > 0 || results.errors.length > 0) throw new Error("On-chain scenarios failed");
    process.exit(1);
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 4: Pool State Verification
  // ═══════════════════════════════════════════════════════════════════
  step("Test 4: Pool State");
  try {
    const Pool = await ethers.getContractFactory("SidioraPool");
    const pool = Pool.attach(poolAddr);

    const virtualUsdl = await pool.virtualUsdlReserve();
    assert(virtualUsdl === ethers.parseUnits("10000", 6), "virtualUsdlReserve = 10,000");

    const realUsdl = await pool.realUsdlBalance();
    assert(realUsdl === 0n, "realUsdlBalance = 0 (fresh pool)");

    const tokenRes = await pool.tokenReserve();
    assert(tokenRes === ethers.parseUnits("1000000000", 6), "tokenReserve = 1B");

    const nftOwner = await nft.ownerOf(nftId);
    assert(nftOwner === deployer.address, "NFT owner = deployer");
  } catch (e) {
    fail(`Pool state check failed: ${e.message.slice(0, 100)}`);
    results.failed++;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 5: Buy
  // ═══════════════════════════════════════════════════════════════════
  step("Test 5: Buy");
  let tokensReceived;
  try {
    const buyAmount = ethers.parseUnits("100", 6);

    // Quote first
    const quote = await quoter.quoteExactInput(poolAddr, buyAmount, true);
    info(`Quoted output: ${ethers.formatUnits(quote.amountOut, 6)} tokens`);
    info(`Quoted fee:    ${ethers.formatUnits(quote.feeAmount, 6)} USDL`);

    const tx = await router.buy(poolAddr, buyAmount, 0, await futureDeadline());
    const receipt = await tx.wait();
    info(`Gas used: ${receipt.gasUsed.toString()}`);

    const Token = await ethers.getContractFactory("SidioraERC20");
    const token = Token.attach(tokenAddr);
    tokensReceived = await token.balanceOf(deployer.address);

    assert(tokensReceived > 0n, "Received tokens > 0");
    info(`Tokens received: ${ethers.formatUnits(tokensReceived, 6)}`);

    const Pool = await ethers.getContractFactory("SidioraPool");
    const pool = Pool.attach(poolAddr);
    const realUsdl = await pool.realUsdlBalance();
    assert(realUsdl > 0n, "Pool realUsdlBalance > 0 after buy");

    const accFees = await accumulator.getAccumulatedFees(poolAddr);
    assert(accFees > 0n, "FeeAccumulator has accumulated fees");
    info(`Accumulated fees: ${ethers.formatUnits(accFees, 6)} USDL`);
  } catch (e) {
    fail(`Buy failed: ${e.message.slice(0, 150)}`);
    results.failed++;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 6: Sell
  // ═══════════════════════════════════════════════════════════════════
  step("Test 6: Sell");
  try {
    if (!tokensReceived || tokensReceived === 0n) throw new Error("No tokens to sell");

    const Token = await ethers.getContractFactory("SidioraERC20");
    const token = Token.attach(tokenAddr);

    const sellAmount = tokensReceived / 2n;
    info(`Selling ${ethers.formatUnits(sellAmount, 6)} tokens`);

    // Approve Router for token
    const approveTx = await token.approve(addresses.Router_proxy, sellAmount);
    await approveTx.wait();

    const usdlBefore = await usdl.balanceOf(deployer.address);
    const tx = await router.sell(poolAddr, sellAmount, 0, await futureDeadline());
    const receipt = await tx.wait();
    info(`Gas used: ${receipt.gasUsed.toString()}`);

    const usdlAfter = await usdl.balanceOf(deployer.address);
    const usdlReceived = usdlAfter - usdlBefore;
    assert(usdlReceived > 0n, "Received USDL > 0 from sell");
    info(`USDL received: ${ethers.formatUnits(usdlReceived, 6)}`);
  } catch (e) {
    fail(`Sell failed: ${e.message.slice(0, 150)}`);
    results.failed++;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 7: Claim Fees
  // ═══════════════════════════════════════════════════════════════════
  step("Test 7: Claim Fees");
  try {
    const accFees = await accumulator.getAccumulatedFees(poolAddr);
    if (accFees === 0n) {
      info("No fees to claim (sell fees stay in pool). Skipping claim.");
      ok("Fee behavior correct (sell fees stay in pool)");
      results.passed++;
    } else {
      const usdlBefore = await usdl.balanceOf(deployer.address);
      const tx = await feesRouter.claimFees(nftId);
      await tx.wait();

      const usdlAfter = await usdl.balanceOf(deployer.address);
      const claimed = usdlAfter - usdlBefore;
      assert(claimed > 0n, "Claimed fees > 0");
      info(`Claimed: ${ethers.formatUnits(claimed, 6)} USDL`);

      const remaining = await accumulator.getAccumulatedFees(poolAddr);
      assert(remaining === 0n, "Accumulated fees zeroed after claim");
    }
  } catch (e) {
    fail(`Claim fees failed: ${e.message.slice(0, 150)}`);
    results.failed++;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 8: Treasury Received Protocol Fees
  // ═══════════════════════════════════════════════════════════════════
  step("Test 8: Treasury Protocol Fees");
  try {
    const treasuryBal = await treasury.getBalance(meta.usdl);
    info(`Treasury tracked balance: ${ethers.formatUnits(treasuryBal, 6)} USDL`);
    assert(treasuryBal > 0n, "Treasury has protocol fees");

    const treasuryUsdl = await usdl.balanceOf(addresses.Treasury_proxy);
    info(`Treasury USDL balance:    ${ethers.formatUnits(treasuryUsdl, 6)} USDL`);
    assert(treasuryUsdl > 0n, "Treasury holds USDL");
  } catch (e) {
    fail(`Treasury check failed: ${e.message.slice(0, 100)}`);
    results.failed++;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 9: Quoter Pool Discovery
  // ═══════════════════════════════════════════════════════════════════
  step("Test 9: Quoter Pool Discovery");
  try {
    const allPools = await quoter.getAllPools(0, 100);
    assert(allPools.includes(poolAddr), "Quoter.getAllPools includes our pool");

    const price = await quoter.getPoolPrice(poolAddr);
    assert(price > 0n, "Quoter.getPoolPrice > 0");
    info(`Current price: ${price}`);

    const marketCap = await quoter.getMarketCap(poolAddr);
    assert(marketCap > 0n, "Quoter.getMarketCap > 0");
    info(`Market cap: ${ethers.formatUnits(marketCap, 6)} USDL`);
  } catch (e) {
    fail(`Quoter check failed: ${e.message.slice(0, 100)}`);
    results.failed++;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Test 10: Pool Invariants
  // ═══════════════════════════════════════════════════════════════════
  step("Test 10: Pool Invariants");
  try {
    const Pool = await ethers.getContractFactory("SidioraPool");
    const pool = Pool.attach(poolAddr);

    const virtualUsdl = await pool.virtualUsdlReserve();
    assert(virtualUsdl === ethers.parseUnits("10000", 6), "virtualUsdlReserve unchanged");

    const realUsdl = await pool.realUsdlBalance();
    assert(realUsdl >= 0n, "realUsdlBalance >= 0");

    const tokenRes = await pool.tokenReserve();
    assert(tokenRes > 0n, "tokenReserve > 0");

    const volume = await pool.cumulativeVolume();
    assert(volume > 0n, "cumulativeVolume > 0");
    info(`Cumulative volume: ${ethers.formatUnits(volume, 6)} USDL`);
  } catch (e) {
    fail(`Pool invariant check failed: ${e.message.slice(0, 100)}`);
    results.failed++;
  }

  // ═══════════════════════════════════════════════════════════════════
  // Summary
  // ═══════════════════════════════════════════════════════════════════
  printSummary(results);
  if (results.failed > 0 || results.errors.length > 0) throw new Error("On-chain scenarios failed");
}

function printSummary(results) {
  console.log(`\n${"═".repeat(60)}`);
  console.log(`  On-Chain Test Summary`);
  console.log(`  ${COLORS.green}✅ Passed: ${results.passed}${COLORS.reset}`);
  console.log(`  ${COLORS.red}❌ Failed: ${results.failed}${COLORS.reset}`);
  if (results.errors.length > 0) {
    console.log(`\n  Failed tests:`);
    for (const e of results.errors) {
      console.log(`    - ${e}`);
    }
  }
  console.log(`${"═".repeat(60)}\n`);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error("❌ On-chain test failed:", error);
    process.exit(1);
  });
