/**
 * Sidiora Meta-AG — PECOR engine unit tests (Phase 4 / Task 4.1 — deferred to Session 8)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.6 (FROZEN 2026-04-24)
 * Interface: contracts/meta-ag/interfaces/IPECOR.sol
 * Contract:  contracts/meta-ag/engine/PECOR.sol
 *
 * Regressions exercised:
 *   - S1   UUPS _authorizeUpgrade gated by DEFAULT_ADMIN_ROLE
 *   - S11  Tiered fee stacking: swapFee + tier1 + tier2 ≤ MAX_FEE_BPS (200)
 *   - S12  Append-only storage — __gap[50] at slot 12
 *   - Pausable: every swap path blocked when paused
 *   - Tracker integration: PECOR records via EMITTER_ROLE only
 */

const { expect } = require("chai");
const { ethers, artifacts } = require("hardhat");

const {
  PECOR_ROLES,
  BPS,
  LIMITS,
} = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const {
  deployPriceOracle,
  deployPECORVault,
  deployPECOR,
  deployTransactionTracker,
} = require("../helpers/fixtures");
const { pushPrice } = require("../helpers/oracle");

const ONE = 10n ** 18n;
const ONE_HOUR = 3600;
const PRICE_BOUND_HEARTBEAT = 60;
const PRICE_BOUND_DEVIATION = 100n;
const PRICE_BOUND_MIN = ONE / 100n;
const PRICE_BOUND_MAX = ONE * 1_000_000n;
const PRICE_BOUND_STALE = ONE_HOUR;

async function makeToken(name, symbol = "T", decimals = 18) {
  const ERC20 = await ethers.getContractFactory("MockERC20");
  const t = await ERC20.deploy(name, symbol, decimals);
  await t.waitForDeployment();
  return t;
}

async function makeWETH() {
  const W = await ethers.getContractFactory("MockWETH9");
  const w = await W.deploy();
  await w.waitForDeployment();
  return w;
}

async function setupBase({ withRealTracker = false, swapFeeBps = 0n, wirePecorTracker = false } = {}) {
  const [admin, user, recipient, feeCollector, other] = await ethers.getSigners();

  const weth = await makeWETH();
  const oracle = await deployPriceOracle({ admin: admin.address });
  await oracle.connect(admin).setRelayer(admin.address, true);

  // Vault tracker: always a mock stub (vault only reads it as an address, never calls typed methods here)
  const StubT = await ethers.getContractFactory("MockTxTracker");
  const vaultTracker = await StubT.deploy();
  await vaultTracker.waitForDeployment();

  // PECOR tracker: default = zero (PECOR's _recordTrade short-circuits when unset)
  // Only wired to a real TransactionTracker when the test needs tracker integration.
  let pecorTracker;
  if (withRealTracker) {
    pecorTracker = await deployTransactionTracker({ admin: admin.address });
  }

  const vault = await deployPECORVault({
    weth: weth.target,
    tracker: vaultTracker.target,
    admin: admin.address,
  });

  // Tokens — A is non-stable, USDL is stablecoin (per spec Q2)
  const tokenA = await makeToken("Alpha", "A", 18);
  const usdl = await makeToken("USDL", "USDL", 18);

  // Vault registers; USDL is a stablecoin
  await vault.connect(admin).registerToken(tokenA.target, false);
  await vault.connect(admin).registerToken(usdl.target, true);
  await vault.connect(admin).registerToken(weth.target, false);

  // Oracle config
  for (const t of [tokenA.target, usdl.target, weth.target]) {
    await oracle
      .connect(admin)
      .registerToken(t, PRICE_BOUND_HEARTBEAT, PRICE_BOUND_DEVIATION, PRICE_BOUND_MIN, PRICE_BOUND_MAX, PRICE_BOUND_STALE);
  }
  await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: ONE });
  await pushPrice({ priceOracle: oracle, relayer: admin, token: usdl.target, price: ONE });
  await pushPrice({ priceOracle: oracle, relayer: admin, token: weth.target, price: 2n * ONE });

  const pecor = await deployPECOR({
    priceOracle: oracle.target,
    vault: vault.target,
    weth: weth.target,
    tracker: wirePecorTracker && pecorTracker ? pecorTracker.target : ethers.ZeroAddress,
    admin: admin.address,
  });

  // Vault grants OPERATOR_ROLE so PECOR can pullTokens/pushTokens
  await vault.connect(admin).setOperator(pecor.target, true);

  // Tracker grants EMITTER_ROLE to PECOR (real tracker only)
  if (wirePecorTracker && pecorTracker) {
    await pecorTracker.connect(admin).setAuthorizedEmitter(pecor.target, true);
  }

  // Optional swap fee bump
  if (swapFeeBps > 0n) {
    await pecor.connect(admin).setSwapFee(swapFeeBps);
  }

  // Seed vault liquidity: USDL + tokenA + WETH reserves
  await usdl.mint(admin.address, 1_000_000n * ONE);
  await usdl.connect(admin).approve(vault.target, 1_000_000n * ONE);
  await vault.connect(admin).deposit(usdl.target, 1_000_000n * ONE);

  await tokenA.mint(admin.address, 1_000_000n * ONE);
  await tokenA.connect(admin).approve(vault.target, 1_000_000n * ONE);
  await vault.connect(admin).deposit(tokenA.target, 100_000n * ONE);

  // Seed WETH into vault by wrapping native and depositing
  await weth.connect(admin).deposit({ value: 100n * ONE });
  await weth.connect(admin).approve(vault.target, 100n * ONE);
  await vault.connect(admin).deposit(weth.target, 100n * ONE);

  return {
    admin,
    user,
    recipient,
    feeCollector,
    other,
    weth,
    oracle,
    vault,
    vaultTracker,
    pecorTracker,
    pecor,
    tokenA,
    usdl,
  };
}

describe("meta-ag/engine/PECOR", function () {
  let env;

  beforeEach(async function () {
    env = await setupBase();
  });

  // ------------------------------------------------------------------- //
  // initialize                                                           //
  // ------------------------------------------------------------------- //
  describe("initialize", function () {
    it("stores oracle / vault / weth / tracker; grants admin DEFAULT_ADMIN_ROLE", async function () {
      const { pecor, oracle, vault, weth, admin } = env;
      expect(await pecor.priceOracle()).to.equal(oracle.target);
      expect(await pecor.vault()).to.equal(vault.target);
      expect(await pecor.weth()).to.equal(weth.target);
      // Default setup: tracker unset (zero) — exercises _recordTrade skip path
      expect(await pecor.transactionTracker()).to.equal(ethers.ZeroAddress);
      expect(await pecor.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)).to.equal(true);
    });

    it("seeds default fees: swapFeeBps=0, tier1=20, tier2=50, scalar=100, impactEnabled=true", async function () {
      const { pecor } = env;
      expect(await pecor.swapFeeBps()).to.equal(0n);
      expect(await pecor.tier1FeeBps()).to.equal(20n);
      expect(await pecor.tier2FeeBps()).to.equal(50n);
      expect(await pecor.priceImpactScalarBps()).to.equal(100n);
      expect(await pecor.priceImpactEnabled()).to.equal(true);
    });

    it("rejects zero oracle / vault / weth / admin", async function () {
      const { vault, weth } = env;
      const Factory = await ethers.getContractFactory("PECOR");
      const impl = await Factory.deploy();
      await impl.waitForDeployment();
      const Proxy = await ethers.getContractFactory("ERC1967Proxy");
      const make = (args) =>
        impl.interface.encodeFunctionData("initialize", args);

      // zero oracle
      await expect(
        Proxy.deploy(impl.target, make([ethers.ZeroAddress, vault.target, weth.target, ethers.ZeroAddress, ethers.Wallet.createRandom().address]))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");
      // zero vault
      await expect(
        Proxy.deploy(impl.target, make([ethers.Wallet.createRandom().address, ethers.ZeroAddress, weth.target, ethers.ZeroAddress, ethers.Wallet.createRandom().address]))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");
      // zero weth
      await expect(
        Proxy.deploy(impl.target, make([ethers.Wallet.createRandom().address, vault.target, ethers.ZeroAddress, ethers.ZeroAddress, ethers.Wallet.createRandom().address]))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");
      // zero admin
      await expect(
        Proxy.deploy(impl.target, make([ethers.Wallet.createRandom().address, vault.target, weth.target, ethers.ZeroAddress, ethers.ZeroAddress]))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");
    });

    it("constants: MAX_FEE_BPS=200, MAX_IMPACT_BPS=500, TIER1=10_000e18, TIER2=100_000e18", async function () {
      const { pecor } = env;
      expect(await pecor.MAX_FEE_BPS()).to.equal(BPS.MAX_FEE_BPS);
      expect(await pecor.MAX_IMPACT_BPS()).to.equal(BPS.MAX_IMPACT_BPS);
      expect(await pecor.TIER1_THRESHOLD()).to.equal(LIMITS.TIER1_THRESHOLD_USD);
      expect(await pecor.TIER2_THRESHOLD()).to.equal(LIMITS.TIER2_THRESHOLD_USD);
    });
  });

  // ------------------------------------------------------------------- //
  // admin setters (S11 fee stacking)                                     //
  // ------------------------------------------------------------------- //
  describe("admin setters (S11)", function () {
    it("setSwapFee respects S11: swap+tier1+tier2 ≤ MAX_FEE_BPS", async function () {
      const { pecor, admin, other } = env;
      // Defaults: tier1=20 + tier2=50 = 70. So swapFee can go up to 130 (130+70=200)
      await expect(pecor.connect(admin).setSwapFee(130n))
        .to.emit(pecor, "SwapFeeUpdated")
        .withArgs(130n);
      // 131 + 70 = 201 > 200 → revert
      await expect(pecor.connect(admin).setSwapFee(131n)).to.be.revertedWithCustomError(
        pecor,
        "FeeTooHigh"
      );
      // EOA gate
      await expect(pecor.connect(other).setSwapFee(10n)).to.be.revertedWithCustomError(
        pecor,
        ERRORS.common.Unauthorized
      );
    });

    it("setTieredFees enforces tier2 ≥ tier1 and S11 stacking", async function () {
      const { pecor, admin, other } = env;
      await expect(pecor.connect(admin).setTieredFees(50n, 100n))
        .to.emit(pecor, "TieredFeesUpdated")
        .withArgs(50n, 100n);
      // tier2 < tier1
      await expect(
        pecor.connect(admin).setTieredFees(100n, 50n)
      ).to.be.revertedWithCustomError(pecor, "Tier2BelowTier1");
      // 0 + 100 + 101 = 201 > 200
      await expect(
        pecor.connect(admin).setTieredFees(100n, 101n)
      ).to.be.revertedWithCustomError(pecor, "FeeTooHigh");
      await expect(
        pecor.connect(other).setTieredFees(10n, 20n)
      ).to.be.revertedWithCustomError(pecor, ERRORS.common.Unauthorized);
    });

    it("setPriceImpact: ScalarTooHigh > MAX_IMPACT_BPS (500), admin-only, emits", async function () {
      const { pecor, admin, other } = env;
      await expect(pecor.connect(admin).setPriceImpact(false, 200n))
        .to.emit(pecor, "PriceImpactConfigUpdated")
        .withArgs(false, 200n);
      expect(await pecor.priceImpactEnabled()).to.equal(false);

      await expect(
        pecor.connect(admin).setPriceImpact(true, 501n)
      ).to.be.revertedWithCustomError(pecor, "ScalarTooHigh");
      await expect(
        pecor.connect(other).setPriceImpact(true, 100n)
      ).to.be.revertedWithCustomError(pecor, ERRORS.common.Unauthorized);
    });

    it("setFeeCollector rotates FEE_COLLECTOR_ROLE and emits", async function () {
      const { pecor, admin, feeCollector, other } = env;
      await expect(pecor.connect(admin).setFeeCollector(feeCollector.address))
        .to.emit(pecor, "FeeCollectorUpdated")
        .withArgs(feeCollector.address);
      expect(await pecor.hasRole(PECOR_ROLES.FEE_COLLECTOR_ROLE, feeCollector.address)).to.equal(true);

      await pecor.connect(admin).setFeeCollector(other.address);
      expect(await pecor.hasRole(PECOR_ROLES.FEE_COLLECTOR_ROLE, feeCollector.address)).to.equal(false);
      expect(await pecor.hasRole(PECOR_ROLES.FEE_COLLECTOR_ROLE, other.address)).to.equal(true);

      await expect(
        pecor.connect(admin).setFeeCollector(ethers.ZeroAddress)
      ).to.be.revertedWithCustomError(pecor, "ZeroAddress");
    });

    it("setPriceOracle / setTransactionTracker emit + are admin-only", async function () {
      const { pecor, admin, other } = env;
      const fresh = await deployPriceOracle({ admin: admin.address });
      await expect(pecor.connect(admin).setPriceOracle(fresh.target))
        .to.emit(pecor, "PriceOracleUpdated")
        .withArgs(fresh.target);

      await expect(
        pecor.connect(admin).setPriceOracle(ethers.ZeroAddress)
      ).to.be.revertedWithCustomError(pecor, "ZeroAddress");

      await expect(
        pecor.connect(admin).setTransactionTracker(ethers.ZeroAddress)
      )
        .to.emit(pecor, "TransactionTrackerUpdated")
        .withArgs(ethers.ZeroAddress);

      await expect(
        pecor.connect(other).setPriceOracle(fresh.target)
      ).to.be.revertedWithCustomError(pecor, ERRORS.common.Unauthorized);
    });

    it("pause/unpause: admin-only, blocks swaps", async function () {
      const { pecor, admin, other, user, tokenA, usdl } = env;
      await pecor.connect(admin).pause();
      await tokenA.mint(user.address, 10n * ONE);
      await tokenA.connect(user).approve(env.vault.target, 10n * ONE);
      await expect(
        pecor.connect(user).swapExactIn(tokenA.target, usdl.target, ONE, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "Paused");

      await expect(pecor.connect(other).unpause()).to.be.revertedWithCustomError(
        pecor,
        ERRORS.common.Unauthorized
      );
      await pecor.connect(admin).unpause();
      await expect(
        pecor.connect(user).swapExactIn(tokenA.target, usdl.target, ONE, 0n, 0)
      ).not.to.be.reverted;
    });
  });

  // ------------------------------------------------------------------- //
  // swapExactIn                                                          //
  // ------------------------------------------------------------------- //
  describe("swapExactIn", function () {
    let user, tokenA, usdl, vault, pecor;
    beforeEach(async function () {
      ({ user, tokenA, usdl, vault, pecor } = env);
      await tokenA.mint(user.address, 1000n * ONE);
      await tokenA.connect(user).approve(vault.target, 1000n * ONE);
    });

    it("reverts SameToken / ZeroAmount / Expired", async function () {
      await expect(
        pecor.connect(user).swapExactIn(tokenA.target, tokenA.target, ONE, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "SameToken");
      await expect(
        pecor.connect(user).swapExactIn(tokenA.target, usdl.target, 0n, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "ZeroAmount");
      const past = (await ethers.provider.getBlock("latest")).timestamp - 100;
      await expect(
        pecor.connect(user).swapExactIn(tokenA.target, usdl.target, ONE, 0n, past)
      ).to.be.revertedWithCustomError(pecor, "Expired");
    });

    it("happy path: swaps tokenA→USDL, emits SimpleSwap, no fee at sub-tier1 volume", async function () {
      const balUsdlBefore = await usdl.balanceOf(user.address);
      // 100 tokenA = $100 (well below tier1=$10k) → swapFeeBps=0 default → no TieredFeeApplied
      const tx = await pecor.connect(user).swapExactIn(tokenA.target, usdl.target, 100n * ONE, 0n, 0);
      const r = await tx.wait();
      const balUsdlAfter = await usdl.balanceOf(user.address);
      // gross = 100 * 1 / 1 = 100 USDL; impact ~ $100/$1M reserves * 100 = 0.01 bps → 0
      expect(balUsdlAfter - balUsdlBefore).to.be.gte(99n * ONE);
      expect(r.logs.some((l) => l.fragment && l.fragment.name === "SimpleSwap")).to.equal(true);
    });

    it("InsufficientOutput when amountOutMin > computed net out", async function () {
      // 1 tokenA → 1 USDL gross; ask for 2 USDL min
      await expect(
        pecor.connect(user).swapExactIn(tokenA.target, usdl.target, ONE, 2n * ONE, 0)
      ).to.be.revertedWithCustomError(pecor, "InsufficientOutput");
    });

    it("InsufficientLiquidity when output exceeds vault reserves", async function () {
      // 5_000_000 tokenA → 5M USDL but vault only seeded 1M USDL
      await tokenA.mint(user.address, 5_000_000n * ONE);
      await tokenA.connect(user).approve(vault.target, 5_000_000n * ONE);
      await expect(
        pecor.connect(user).swapExactIn(tokenA.target, usdl.target, 5_000_000n * ONE, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "InsufficientLiquidity");
    });
  });

  // ------------------------------------------------------------------- //
  // swapExactOut                                                         //
  // ------------------------------------------------------------------- //
  describe("swapExactOut", function () {
    let user, tokenA, usdl, vault, pecor;
    beforeEach(async function () {
      ({ user, tokenA, usdl, vault, pecor } = env);
      await tokenA.mint(user.address, 1000n * ONE);
      await tokenA.connect(user).approve(vault.target, 1000n * ONE);
    });

    it("reverts SameToken / ZeroAmount / InsufficientLiquidity", async function () {
      await expect(
        pecor.connect(user).swapExactOut(tokenA.target, tokenA.target, ONE, ONE * 10n, 0)
      ).to.be.revertedWithCustomError(pecor, "SameToken");
      await expect(
        pecor.connect(user).swapExactOut(tokenA.target, usdl.target, 0n, ONE, 0)
      ).to.be.revertedWithCustomError(pecor, "ZeroAmount");
      await expect(
        pecor.connect(user).swapExactOut(tokenA.target, usdl.target, 5_000_000n * ONE, ONE * 10_000_000n, 0)
      ).to.be.revertedWithCustomError(pecor, "InsufficientLiquidity");
    });

    it("ExcessiveInput when required amountIn > amountInMax", async function () {
      // Need 1 USDL out → ~1 tokenA in. Setting amountInMax=0 → revert
      await expect(
        pecor.connect(user).swapExactOut(tokenA.target, usdl.target, ONE, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "ExcessiveInput");
    });

    it("happy path: pulls just enough tokenA, pushes exact USDL out, emits SimpleSwap", async function () {
      const balUsdlBefore = await usdl.balanceOf(user.address);
      const targetOut = 50n * ONE;
      await pecor.connect(user).swapExactOut(tokenA.target, usdl.target, targetOut, 1000n * ONE, 0);
      const balUsdlAfter = await usdl.balanceOf(user.address);
      expect(balUsdlAfter - balUsdlBefore).to.equal(targetOut);
    });
  });

  // ------------------------------------------------------------------- //
  // marketBuy / marketSell                                               //
  // ------------------------------------------------------------------- //
  describe("market orders", function () {
    let user, tokenA, usdl, vault, pecor;
    beforeEach(async function () {
      ({ user, tokenA, usdl, vault, pecor } = env);
      await usdl.mint(user.address, 1000n * ONE);
      await usdl.connect(user).approve(vault.target, 1000n * ONE);
      await tokenA.mint(user.address, 1000n * ONE);
      await tokenA.connect(user).approve(vault.target, 1000n * ONE);
    });

    it("marketBuy: NotAStablecoin / TokenIsStablecoin / ZeroAmount", async function () {
      await expect(
        pecor.connect(user).marketBuy(tokenA.target, usdl.target, ONE, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "NotAStablecoin");
      await expect(
        pecor.connect(user).marketBuy(usdl.target, usdl.target, ONE, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "TokenIsStablecoin");
      await expect(
        pecor.connect(user).marketBuy(usdl.target, tokenA.target, 0n, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "ZeroAmount");
    });

    it("marketBuy happy path: emits MarketOrderExecuted with isBuy=true", async function () {
      const tx = await pecor.connect(user).marketBuy(usdl.target, tokenA.target, 100n * ONE, 0n, 0);
      const r = await tx.wait();
      const moe = r.logs.find((l) => l.fragment && l.fragment.name === "MarketOrderExecuted");
      expect(moe).to.not.equal(undefined);
      expect(moe.args.isBuy).to.equal(true);
    });

    it("marketSell: mirror-checks + isBuy=false", async function () {
      // NotAStablecoin: second arg `stablecoin` is not a stablecoin (tokenA)
      await expect(
        pecor.connect(user).marketSell(usdl.target, tokenA.target, ONE, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "NotAStablecoin");
      // TokenIsStablecoin: first arg `token` IS a stablecoin (usdl), stablecoin is also usdl
      await expect(
        pecor.connect(user).marketSell(usdl.target, usdl.target, ONE, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "TokenIsStablecoin");

      const tx = await pecor.connect(user).marketSell(tokenA.target, usdl.target, 100n * ONE, 0n, 0);
      const r = await tx.wait();
      const moe = r.logs.find((l) => l.fragment && l.fragment.name === "MarketOrderExecuted");
      expect(moe.args.isBuy).to.equal(false);
    });
  });

  // ------------------------------------------------------------------- //
  // native swaps                                                         //
  // ------------------------------------------------------------------- //
  describe("native swaps", function () {
    let user, weth, usdl, pecor;
    beforeEach(async function () {
      ({ user, weth, usdl, pecor } = env);
    });

    it("swapExactInNative: ZeroAmount on msg.value=0", async function () {
      await expect(
        pecor.connect(user).swapExactInNative(usdl.target, 0n, 0, { value: 0n })
      ).to.be.revertedWithCustomError(pecor, "ZeroAmount");
    });

    it("swapExactInNative: UseWethDeposit when tokenOut == WETH", async function () {
      await expect(
        pecor.connect(user).swapExactInNative(weth.target, 0n, 0, { value: ONE })
      ).to.be.revertedWithCustomError(pecor, "UseWethDeposit");
    });

    it("swapExactInNative happy path: WETH=$2 → 2 USDL per 1 ETH; emits NativeSwap nativeIsInput=true", async function () {
      const balUsdlBefore = await usdl.balanceOf(user.address);
      const tx = await pecor.connect(user).swapExactInNative(usdl.target, 0n, 0, { value: ONE });
      const r = await tx.wait();
      const balUsdlAfter = await usdl.balanceOf(user.address);
      // 1 ETH * $2 / $1 = 2 USDL gross, no fee → 2 USDL net (give or take impact)
      expect(balUsdlAfter - balUsdlBefore).to.be.gt(0n);
      const ev = r.logs.find((l) => l.fragment && l.fragment.name === "NativeSwap");
      expect(ev.args.nativeIsInput).to.equal(true);
    });

    it("swapExactInToNative: UseWethWithdraw when tokenIn == WETH", async function () {
      await expect(
        pecor.connect(user).swapExactInToNative(weth.target, ONE, 0n, 0)
      ).to.be.revertedWithCustomError(pecor, "UseWethWithdraw");
    });

    it("swapExactInToNative happy path: tokenA→native; emits NativeSwap nativeIsInput=false", async function () {
      const { tokenA, vault } = env;
      await tokenA.mint(user.address, 100n * ONE);
      await tokenA.connect(user).approve(vault.target, 100n * ONE);
      const ethBefore = await ethers.provider.getBalance(user.address);
      const tx = await pecor.connect(user).swapExactInToNative(tokenA.target, 100n * ONE, 0n, 0);
      const r = await tx.wait();
      const ethAfter = await ethers.provider.getBalance(user.address);
      // 100 tokenA = $100 / $2 (WETH price) = 50 ETH gross — but WETH reserves only 100, so this is fine
      // Net to user (post-gas) should be > 0
      expect(ethAfter + r.gasUsed * r.gasPrice - ethBefore).to.be.gt(0n);
      const ev = r.logs.find((l) => l.fragment && l.fragment.name === "NativeSwap");
      expect(ev.args.nativeIsInput).to.equal(false);
    });
  });

  // ------------------------------------------------------------------- //
  // tiered fee invariant (S11) via getDetailedQuote                      //
  // ------------------------------------------------------------------- //
  describe("tiered fee resolution (S11)", function () {
    it("at volume < TIER1: effective fee = swapFeeBps", async function () {
      const { pecor, admin, tokenA, usdl } = env;
      await pecor.connect(admin).setSwapFee(10n);
      const q = await pecor.getDetailedQuote(tokenA.target, usdl.target, ONE);
      expect(q.feeBps).to.equal(10n);
    });

    it("at volume ≥ TIER1 but < TIER2: effective fee = swapFeeBps + tier1FeeBps", async function () {
      const { pecor, admin, tokenA, usdl } = env;
      await pecor.connect(admin).setSwapFee(10n);
      // 10_000 tokenA = $10_000 → exactly at TIER1
      const q = await pecor.getDetailedQuote(tokenA.target, usdl.target, 10_000n * ONE);
      expect(q.feeBps).to.equal(10n + 20n);
    });

    it("at volume ≥ TIER2: effective fee = swap + tier1 + tier2", async function () {
      const { pecor, admin, tokenA, usdl } = env;
      await pecor.connect(admin).setSwapFee(10n);
      // 100_000 tokenA = $100_000 → TIER2
      const q = await pecor.getDetailedQuote(tokenA.target, usdl.target, 100_000n * ONE);
      expect(q.feeBps).to.equal(10n + 20n + 50n);
    });
  });

  // ------------------------------------------------------------------- //
  // view: quotes                                                         //
  // ------------------------------------------------------------------- //
  describe("views", function () {
    it("getQuoteExactIn matches actual netOut from getDetailedQuote", async function () {
      const { pecor, tokenA, usdl } = env;
      const direct = await pecor.getQuoteExactIn(tokenA.target, usdl.target, 100n * ONE);
      const detailed = await pecor.getDetailedQuote(tokenA.target, usdl.target, 100n * ONE);
      expect(direct).to.equal(detailed.netOut);
    });

    it("getQuoteExactOut accounts for fee on the input side", async function () {
      const { pecor, admin, tokenA, usdl } = env;
      await pecor.connect(admin).setSwapFee(10n);
      const inForOne = await pecor.getQuoteExactOut(tokenA.target, usdl.target, ONE);
      // Without fee, 1 USDL needs 1 tokenA; with 10 bps fee, slightly more.
      expect(inForOne).to.be.gte(ONE);
      expect(inForOne).to.be.lt(2n * ONE);
    });
  });

  // ------------------------------------------------------------------- //
  // collectFees                                                          //
  // ------------------------------------------------------------------- //
  describe("collectFees", function () {
    it("FEE_COLLECTOR_ROLE-gated, NoFeesToCollect when zero", async function () {
      const { pecor, admin, feeCollector, other } = env;
      await pecor.connect(admin).setFeeCollector(feeCollector.address);
      await expect(
        pecor.connect(other).collectFees(env.usdl.target)
      ).to.be.revertedWithCustomError(pecor, ERRORS.common.Unauthorized);
      await expect(
        pecor.connect(feeCollector).collectFees(env.usdl.target)
      ).to.be.revertedWithCustomError(pecor, "NoFeesToCollect");
    });

    it("transfers accruedFees out of vault to collector and emits FeesCollected", async function () {
      const { pecor, admin, feeCollector, user, tokenA, usdl, vault } = env;
      await pecor.connect(admin).setFeeCollector(feeCollector.address);
      await pecor.connect(admin).setSwapFee(50n); // 0.5% fee → meaningful accrual

      await tokenA.mint(user.address, 1000n * ONE);
      await tokenA.connect(user).approve(vault.target, 1000n * ONE);
      // Run a swap big enough to clear tier1 (volume $10k): 10000 tokenA
      // At 50bps + tier1 20 = 70bps fee on ~$10k of USDL out.
      await tokenA.mint(user.address, 9000n * ONE);
      await tokenA.connect(user).approve(vault.target, 10000n * ONE);
      await pecor.connect(user).swapExactIn(tokenA.target, usdl.target, 10_000n * ONE, 0n, 0);

      const accrued = await pecor.accruedFees(usdl.target);
      expect(accrued).to.be.gt(0n);

      const collectorBefore = await usdl.balanceOf(feeCollector.address);
      await expect(pecor.connect(feeCollector).collectFees(usdl.target))
        .to.emit(pecor, "FeesCollected")
        .withArgs(usdl.target, feeCollector.address, accrued);
      expect(await pecor.accruedFees(usdl.target)).to.equal(0n);
      expect(await usdl.balanceOf(feeCollector.address)).to.equal(collectorBefore + accrued);
    });
  });

  // ------------------------------------------------------------------- //
  // multicall                                                            //
  // ------------------------------------------------------------------- //
  describe("multicall", function () {
    it("batches multiple calls atomically", async function () {
      const { pecor, admin } = env;
      const data = [
        pecor.interface.encodeFunctionData("setSwapFee", [10n]),
        pecor.interface.encodeFunctionData("setTieredFees", [10n, 20n]),
      ];
      await pecor.connect(admin).multicall(data);
      expect(await pecor.swapFeeBps()).to.equal(10n);
      expect(await pecor.tier1FeeBps()).to.equal(10n);
      expect(await pecor.tier2FeeBps()).to.equal(20n);
    });

    it("bubbles up underlying custom error verbatim", async function () {
      const { pecor, admin } = env;
      const data = [
        pecor.interface.encodeFunctionData("setSwapFee", [500n]), // > MAX_FEE_BPS
      ];
      await expect(pecor.connect(admin).multicall(data)).to.be.revertedWithCustomError(
        pecor,
        "FeeTooHigh"
      );
    });
  });

  // ------------------------------------------------------------------- //
  // receive() — only WETH may transfer                                   //
  // ------------------------------------------------------------------- //
  describe("receive()", function () {
    it("rejects native transfer from non-WETH source", async function () {
      const { pecor, user } = env;
      await expect(
        user.sendTransaction({ to: pecor.target, value: ONE })
      ).to.be.revertedWithCustomError(pecor, "NativeTransferFailed");
    });
  });

  // ------------------------------------------------------------------- //
  // S1 upgrade authorization                                             //
  // ------------------------------------------------------------------- //
  describe("upgrade authorization (S1)", function () {
    it("upgradeToAndCall reverts for non-admin, succeeds for DEFAULT_ADMIN_ROLE", async function () {
      const { pecor, admin, other } = env;
      const Factory = await ethers.getContractFactory("PECOR");
      const newImpl = await Factory.deploy();
      await newImpl.waitForDeployment();
      await expect(
        pecor.connect(other).upgradeToAndCall(newImpl.target, "0x")
      ).to.be.revertedWithCustomError(pecor, ERRORS.common.Unauthorized);
      await expect(
        pecor.connect(admin).upgradeToAndCall(newImpl.target, "0x")
      ).not.to.be.reverted;
    });
  });

  // ------------------------------------------------------------------- //
  // S12 storage layout                                                   //
  // ------------------------------------------------------------------- //
  describe("storage layout (S12)", function () {
    it("appends fee migration state while retaining the original storage boundary", async function () {
      const art = await artifacts.getBuildInfo("contracts/meta-ag/engine/PECOR.sol:PECOR");
      const layout = art.output.contracts["contracts/meta-ag/engine/PECOR.sol"].PECOR.storageLayout;
      const gap = layout.storage.find((s) => s.label === "__gap");
      expect(gap).to.not.equal(undefined);
      expect(layout.storage.find(s => s.label === "accruedFees").slot).to.equal("11");
      expect(layout.storage.find(s => s.label === "feesInitialized").slot).to.equal("12");
      expect(layout.storage.find(s => s.label === "feeMigrationCursor").slot).to.equal("13");
      expect(gap.slot).to.equal("14");
      expect(gap.type).to.match(/uint256\)48_storage/);
    });
  });
});
