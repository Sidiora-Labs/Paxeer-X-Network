/**
 * Sidiora Meta-AG — MetaAGQuoter unit tests (Phase 7 / Task 7.1 — deferred to Session 8)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.11 (FROZEN 2026-04-24)
 * Interface: contracts/meta-ag/interfaces/IMetaAGQuoter.sol
 * Contract: contracts/meta-ag/quoter/MetaAGQuoter.sol
 *
 * Regressions exercised:
 *   - I1  All view functions never revert (oracle failure → priceStale flags)
 *   - S1  DEFAULT_ADMIN_ROLE = upgrade authority (Timelock at deploy)
 *   - S12 Append-only storage layout — verified separately by check-storage-layout.js
 *
 * Scope:
 *   MetaAGQuoter quotes ONLY vault-side oracle-priced swaps.
 *   Cross-adapter aggregation lives on MetaAGRouter.
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const {
  PECOR_ROLES,
  ZERO_ADDRESS,
} = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const {
  deployPriceOracle,
  deployPECORVault,
  deployPECOR,
  deployMetaAGQuoter,
} = require("../helpers/fixtures");
const { pushPrice, increaseTime } = require("../helpers/oracle");

const ONE = 10n ** 18n;
const ONE_6 = 10n ** 6n;
const ONE_HOUR = 3600;
const BPS_DENOMINATOR = 10_000n;

// --------------------------------------------------------------------------- //
// Helpers                                                                     //
// --------------------------------------------------------------------------- //

async function makeToken(name, symbol, decimals = 18) {
  const ERC20 = await ethers.getContractFactory("MockStandardERC20");
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

async function makeTxTracker() {
  const T = await ethers.getContractFactory("MockTxTracker");
  const t = await T.deploy();
  await t.waitForDeployment();
  return t;
}

function priceBound() {
  return {
    heartbeat: 60,
    deviationBps: 100n, // 1%
    minPrice: ONE / 100n,
    maxPrice: ONE * 100_000n,
    maxStaleness: ONE_HOUR,
  };
}

/**
 * Mock that returns a configurable swapFeeBps via staticcall — emulates the
 * PECOR engine's surface so we can exercise the quoter's fee-aware math
 * without spinning up the full engine.
 */
async function deployFeeBpsBeacon(initialBps) {
  const F = await ethers.getContractFactory("MockSwapFeeBeacon");
  const m = await F.deploy(initialBps);
  await m.waitForDeployment();
  return m;
}

// --------------------------------------------------------------------------- //
// Test suite                                                                  //
// --------------------------------------------------------------------------- //

describe("meta-ag/quoter/MetaAGQuoter", function () {
  let admin, user, other;
  let priceOracle, vault, weth, tracker;
  let tokenA, tokenB, tokenC; // 18, 18, 6 decimals
  let quoter;

  beforeEach(async function () {
    [admin, user, other] = await ethers.getSigners();

    priceOracle = await deployPriceOracle({ admin: admin.address });
    await priceOracle.connect(admin).setRelayer(admin.address, true);

    weth = await makeWETH();
    tracker = await makeTxTracker();
    vault = await deployPECORVault({
      weth: weth.target,
      tracker: tracker.target,
      admin: admin.address,
    });

    tokenA = await makeToken("Alpha", "A", 18);
    tokenB = await makeToken("Beta", "B", 18);
    tokenC = await makeToken("Gamma", "C", 6); // mixed-decimals coverage

    // Register every token on the vault.
    await vault.connect(admin).registerToken(tokenA.target, false);
    await vault.connect(admin).registerToken(tokenB.target, false);
    await vault.connect(admin).registerToken(tokenC.target, false);
    await vault.connect(admin).registerToken(weth.target, false);

    // Register every token on the oracle and seed prices:
    //   tokenA = $1, tokenB = $2, tokenC = $5, weth = $10
    const b = priceBound();
    for (const t of [tokenA, tokenB, tokenC, weth]) {
      await priceOracle
        .connect(admin)
        .registerToken(
          t.target,
          b.heartbeat,
          b.deviationBps,
          b.minPrice,
          b.maxPrice,
          b.maxStaleness
        );
    }
    await pushPrice({ priceOracle, relayer: admin, token: tokenA.target, price: ONE });
    await pushPrice({ priceOracle, relayer: admin, token: tokenB.target, price: 2n * ONE });
    await pushPrice({ priceOracle, relayer: admin, token: tokenC.target, price: 5n * ONE });
    await pushPrice({ priceOracle, relayer: admin, token: weth.target, price: 10n * ONE });

    // Quoter — pecor=address(0) so feeBps defaults to 0 (covers the bootstrap path).
    quoter = await deployMetaAGQuoter({
      priceOracle: priceOracle.target,
      vault: vault.target,
      weth: weth.target,
      pecor: ZERO_ADDRESS,
      admin: admin.address,
    });

    // Seed reserves so liquidity flags can flip.
    //   tokenA: 100k, tokenB: 1k, tokenC: 5k (in own decimals)
    await tokenA.mint(admin.address, 100_000n * ONE);
    await tokenB.mint(admin.address, 1_000n * ONE);
    await tokenC.mint(admin.address, 5_000n * ONE_6);

    await tokenA.connect(admin).approve(vault.target, ethers.MaxUint256);
    await tokenB.connect(admin).approve(vault.target, ethers.MaxUint256);
    await tokenC.connect(admin).approve(vault.target, ethers.MaxUint256);

    await vault.connect(admin).deposit(tokenA.target, 100_000n * ONE);
    await vault.connect(admin).deposit(tokenB.target, 1_000n * ONE);
    await vault.connect(admin).deposit(tokenC.target, 5_000n * ONE_6);
  });

  // ===================================================================== //
  // Initializer                                                            //
  // ===================================================================== //
  describe("initializer", function () {
    it("stores priceOracle / vault / weth / pecor and grants admin DEFAULT_ADMIN_ROLE", async function () {
      expect(await quoter.priceOracle()).to.equal(priceOracle.target);
      expect(await quoter.vault()).to.equal(vault.target);
      expect(await quoter.weth()).to.equal(weth.target);
      expect(await quoter.pecor()).to.equal(ZERO_ADDRESS);
      expect(
        await quoter.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)
      ).to.equal(true);
      expect(await quoter.BPS_DENOMINATOR()).to.equal(BPS_DENOMINATOR);
    });

    it("rejects zero priceOracle / vault / weth / admin", async function () {
      const Impl = await ethers.getContractFactory("MetaAGQuoter");
      const impl = await Impl.deploy();
      await impl.waitForDeployment();
      const Proxy = await ethers.getContractFactory("ERC1967Proxy");

      // Helper to encode init data with one arg zeroed out.
      const enc = (po, v, w, p, a) =>
        impl.interface.encodeFunctionData("initialize", [po, v, w, p, a]);

      await expect(
        Proxy.deploy(impl.target, enc(ZERO_ADDRESS, vault.target, weth.target, ZERO_ADDRESS, admin.address))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");

      await expect(
        Proxy.deploy(impl.target, enc(priceOracle.target, ZERO_ADDRESS, weth.target, ZERO_ADDRESS, admin.address))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");

      await expect(
        Proxy.deploy(impl.target, enc(priceOracle.target, vault.target, ZERO_ADDRESS, ZERO_ADDRESS, admin.address))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");

      await expect(
        Proxy.deploy(impl.target, enc(priceOracle.target, vault.target, weth.target, ZERO_ADDRESS, ZERO_ADDRESS))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");
    });

    it("accepts pecor==address(0) at bootstrap (feeBps falls through to 0)", async function () {
      // Already deployed with pecor=0 in beforeEach. Hitting an exactIn path
      // should yield feeBps==0 (verified later, but assert here for clarity).
      const r = await quoter.quoteExactIn(tokenA.target, tokenB.target, ONE);
      expect(r.feeBps).to.equal(0n);
      expect(r.feeAmount).to.equal(0n);
    });

    it("cannot be re-initialized (Initializable guard)", async function () {
      await expect(
        quoter.initialize(
          priceOracle.target,
          vault.target,
          weth.target,
          ZERO_ADDRESS,
          admin.address
        )
      ).to.be.revertedWithCustomError(quoter, ERRORS.common.AlreadyInitialized);
    });
  });

  // ===================================================================== //
  // quoteExactIn                                                          //
  // ===================================================================== //
  describe("quoteExactIn", function () {
    it("happy path 18→18 same decimals: 100 A @ $1 → 50 B @ $2 (no fee)", async function () {
      const r = await quoter.quoteExactIn(tokenA.target, tokenB.target, 100n * ONE);

      expect(r.amountIn).to.equal(100n * ONE);
      expect(r.grossAmountOut).to.equal(50n * ONE);
      expect(r.amountOut).to.equal(50n * ONE);
      expect(r.feeAmount).to.equal(0n);
      expect(r.feeBps).to.equal(0n);
      expect(r.spotPriceIn).to.equal(ONE);
      expect(r.spotPriceOut).to.equal(2n * ONE);
      expect(r.priceStaleIn).to.equal(false);
      expect(r.priceStaleOut).to.equal(false);
      expect(r.sufficientLiquidity).to.equal(true);
      expect(r.availableLiquidity).to.equal(1_000n * ONE);
      // executionPrice = amountOut * 10^decIn / amountIn = 50e18 * 1e18 / 100e18 = 0.5e18
      expect(r.executionPrice).to.equal(ONE / 2n);
    });

    it("decimal asymmetry 18→6: 10 A @ $1 → 2 C @ $5 (output in 6 decimals)", async function () {
      const r = await quoter.quoteExactIn(tokenA.target, tokenC.target, 10n * ONE);
      expect(r.amountOut).to.equal(2n * ONE_6);
      expect(r.sufficientLiquidity).to.equal(true);
    });

    it("decimal asymmetry 6→18: 10 C @ $5 → 25 B @ $2 (input in 6 decimals)", async function () {
      const r = await quoter.quoteExactIn(tokenC.target, tokenB.target, 10n * ONE_6);
      expect(r.amountOut).to.equal(25n * ONE);
    });

    it("insufficient liquidity flag flips when output > available reserves", async function () {
      // tokenB reserves seeded at 1_000 — request 1M A → 500k B (way over).
      const r = await quoter.quoteExactIn(tokenA.target, tokenB.target, 1_000_000n * ONE);
      expect(r.amountOut).to.equal(500_000n * ONE);
      expect(r.availableLiquidity).to.equal(1_000n * ONE);
      expect(r.sufficientLiquidity).to.equal(false);
    });

    it("oracle revert (stale price) returns priceStale flags + zero amounts; never reverts (I1)", async function () {
      const b = priceBound();
      // Fast-forward past staleness for tokenA only.
      await increaseTime(b.maxStaleness + 60);

      const r = await quoter.quoteExactIn(tokenA.target, tokenB.target, 100n * ONE);
      expect(r.priceStaleIn).to.equal(true);
      expect(r.spotPriceIn).to.equal(0n);
      expect(r.amountOut).to.equal(0n);
      expect(r.grossAmountOut).to.equal(0n);
    });

    it("zero amountIn returns zero amountOut without reverting", async function () {
      const r = await quoter.quoteExactIn(tokenA.target, tokenB.target, 0n);
      expect(r.amountIn).to.equal(0n);
      expect(r.amountOut).to.equal(0n);
      expect(r.grossAmountOut).to.equal(0n);
      // With amount==0, executionPrice stays at 0 (guarded by `if (amount > 0)`)
      expect(r.executionPrice).to.equal(0n);
    });

    it("applies feeBps when pecor surfaces a non-zero swap fee", async function () {
      // Re-deploy quoter with a pecor mock that returns 30 bps.
      const beacon = await deployPECOR({ priceOracle: priceOracle.target, vault: vault.target, weth: weth.target, admin: admin.address });
      await beacon.setSwapFee(30n);
      await beacon.setPriceImpact(false, 0);
      const q2 = await deployMetaAGQuoter({
        priceOracle: priceOracle.target,
        vault: vault.target,
        weth: weth.target,
        pecor: beacon.target,
        admin: admin.address,
      });
      const r = await q2.quoteExactIn(tokenA.target, tokenB.target, 100n * ONE);
      // grossAmountOut = 50e18; fee = 50e18 * 30 / 10000 = 0.15e18; amountOut = 49.85e18
      expect(r.feeBps).to.equal(30n);
      expect(r.grossAmountOut).to.equal(50n * ONE);
      expect(r.feeAmount).to.equal((50n * ONE * 30n) / BPS_DENOMINATOR);
      expect(r.amountOut).to.equal(50n * ONE - (50n * ONE * 30n) / BPS_DENOMINATOR);
    });
  });

  // ===================================================================== //
  // quoteExactOut                                                         //
  // ===================================================================== //
  describe("configured engine parity", function () {
    it("delegates effective tier fees and inventory impact across thresholds", async function () {
      const engine = await deployPECOR({ priceOracle: priceOracle.target, vault: vault.target, weth: weth.target, admin: admin.address });
      await engine.setSwapFee(10n);
      await engine.setTieredFees(20n, 30n);
      await engine.setPriceImpact(true, 50n);
      const q = await deployMetaAGQuoter({ priceOracle: priceOracle.target, vault: vault.target, weth: weth.target, pecor: engine.target, admin: admin.address });
      for (const amount of [ONE, 10000n * ONE - 1n, 10000n * ONE, 10000n * ONE + 1n, 100000n * ONE - 1n, 100000n * ONE, 100000n * ONE + 1n]) {
        const expected = await engine.getDetailedQuote(tokenA.target, tokenB.target, amount);
        const actual = await q.quoteExactIn(tokenA.target, tokenB.target, amount);
        expect(actual.grossAmountOut).to.equal(expected[0]);
        expect(actual.amountOut).to.equal(expected[1]);
        expect(actual.feeBps).to.equal(expected[3]);
        expect(actual.feeAmount).to.equal(expected[4]);
      }
    });
  });

  describe("quoteExactOut", function () {
    it("happy path with feeBps=0: required input mirrors the inverse of exactIn", async function () {
      // Want 50 B → expect 100 A in (no fee).
      const r = await quoter.quoteExactOut(tokenA.target, tokenB.target, 50n * ONE);
      expect(r.grossAmountOut).to.equal(50n * ONE);
      expect(r.amountIn).to.equal(100n * ONE);
      expect(r.amountOut).to.equal(50n * ONE);
      expect(r.feeAmount).to.equal(0n);
      // executionPrice = amountOut * 10^decIn / amountIn = 50e18 * 1e18 / 100e18 = 0.5e18
      expect(r.executionPrice).to.equal(ONE / 2n);
    });

    it("with non-zero fee, amountIn is grossed up so net output equals requested amountOut", async function () {
      const beacon = await deployPECOR({ priceOracle: priceOracle.target, vault: vault.target, weth: weth.target, admin: admin.address });
      await beacon.setSwapFee(30n);
      await beacon.setPriceImpact(false, 0); // 30 bps
      const q2 = await deployMetaAGQuoter({
        priceOracle: priceOracle.target,
        vault: vault.target,
        weth: weth.target,
        pecor: beacon.target,
        admin: admin.address,
      });
      // Request 50 B out — required input goes UP by ~30 bps to cover fee.
      const r = await q2.quoteExactOut(tokenA.target, tokenB.target, 50n * ONE);
      // amountIn = grossInput + grossInput * 30 / (10000-30) + 1
      const grossInput = 100n * ONE;
      const expectedFee = (await beacon.getQuoteExactOut(tokenA.target, tokenB.target, 50n * ONE)) - grossInput;
      expect(r.amountIn).to.equal(grossInput + expectedFee);
      expect(r.feeAmount).to.equal(expectedFee);
      expect(r.amountOut).to.equal(50n * ONE);
    });

    it("oracle revert returns staleness flags + empty amounts (I1)", async function () {
      const b = priceBound();
      await increaseTime(b.maxStaleness + 60);
      const r = await quoter.quoteExactOut(tokenA.target, tokenB.target, 50n * ONE);
      expect(r.priceStaleIn).to.equal(true);
      expect(r.amountIn).to.equal(0n);
    });
  });

  // ===================================================================== //
  // Native shorthand                                                      //
  // ===================================================================== //
  describe("native shorthand", function () {
    it("quoteExactInNative: 1 PAX @ $10 → 5 B @ $2", async function () {
      const r = await quoter.quoteExactInNative(tokenB.target, ONE);
      expect(r.amountOut).to.equal(5n * ONE);
      expect(r.spotPriceIn).to.equal(10n * ONE); // weth price
    });

    it("quoteExactInToNative: 50 B @ $2 → 10 PAX @ $10", async function () {
      const r = await quoter.quoteExactInToNative(tokenB.target, 50n * ONE);
      expect(r.amountOut).to.equal(10n * ONE);
      expect(r.spotPriceOut).to.equal(10n * ONE);
    });
  });

  // ===================================================================== //
  // Market helpers (alias paths through _buildQuote)                      //
  // ===================================================================== //
  describe("market helpers", function () {
    it("quoteMarketBuy: stable→token alias of quoteExactIn", async function () {
      const a = await quoter.quoteExactIn(tokenA.target, tokenB.target, 100n * ONE);
      const m = await quoter.quoteMarketBuy(tokenA.target, tokenB.target, 100n * ONE);
      expect(m.amountOut).to.equal(a.amountOut);
    });

    it("quoteMarketSell: token→stable alias of quoteExactIn", async function () {
      const a = await quoter.quoteExactIn(tokenB.target, tokenA.target, 50n * ONE);
      const m = await quoter.quoteMarketSell(tokenB.target, tokenA.target, 50n * ONE);
      expect(m.amountOut).to.equal(a.amountOut);
    });
  });

  // ===================================================================== //
  // batchQuote                                                            //
  // ===================================================================== //
  describe("batchQuote", function () {
    it("returns empty array for empty input", async function () {
      const r = await quoter.batchQuote([]);
      expect(r.length).to.equal(0);
    });

    it("preserves order and per-request isExactIn flag (mixed exactIn/exactOut)", async function () {
      const reqs = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, amount: 100n * ONE, isExactIn: true },
        { tokenIn: tokenA.target, tokenOut: tokenB.target, amount: 50n * ONE, isExactIn: false },
      ];
      const out = await quoter.batchQuote(reqs);
      expect(out.length).to.equal(2);
      expect(out[0].amountOut).to.equal(50n * ONE); // exactIn
      expect(out[1].amountIn).to.equal(100n * ONE); // exactOut
    });

    it("address(0) is shorthand for native (substitutes weth at the boundary)", async function () {
      const reqs = [
        { tokenIn: ZERO_ADDRESS, tokenOut: tokenB.target, amount: ONE, isExactIn: true },
        { tokenIn: tokenB.target, tokenOut: ZERO_ADDRESS, amount: 50n * ONE, isExactIn: true },
      ];
      const out = await quoter.batchQuote(reqs);
      expect(out[0].amountOut).to.equal(5n * ONE); // 1 weth=$10 → 5 B
      expect(out[1].amountOut).to.equal(10n * ONE); // 50 B=$2 → 10 weth=$10
    });
  });

  // ===================================================================== //
  // Liquidity & price info                                                //
  // ===================================================================== //
  describe("liquidity & price info", function () {
    it("getLiquidityInfo: registered + fresh → (reserves, price, false)", async function () {
      const [available, price, stale] = await quoter.getLiquidityInfo(tokenA.target);
      expect(available).to.equal(100_000n * ONE);
      expect(price).to.equal(ONE);
      expect(stale).to.equal(false);
    });

    it("getLiquidityInfo: stale price → (reserves, 0, true) — never reverts (I1)", async function () {
      const b = priceBound();
      await increaseTime(b.maxStaleness + 60);
      const [available, price, stale] = await quoter.getLiquidityInfo(tokenA.target);
      expect(available).to.equal(100_000n * ONE);
      expect(price).to.equal(0n);
      expect(stale).to.equal(true);
    });

    it("getAllLiquidityInfo: returns all four parallel arrays of equal length", async function () {
      const [tokens, reserves, prices, stale] = await quoter.getAllLiquidityInfo();
      expect(tokens.length).to.equal(4); // A, B, C, weth
      expect(reserves.length).to.equal(tokens.length);
      expect(prices.length).to.equal(tokens.length);
      expect(stale.length).to.equal(tokens.length);
      // every price was just pushed → no staleness
      for (const s of stale) {
        expect(s).to.equal(false);
      }
    });

    it("getTokenPrice: fresh → (price, ts, false); stale → (0, 0, true)", async function () {
      const [p, t, s] = await quoter.getTokenPrice(tokenA.target);
      expect(p).to.equal(ONE);
      expect(t).to.be.gt(0n);
      expect(s).to.equal(false);

      await increaseTime(priceBound().maxStaleness + 60);
      const [p2, t2, s2] = await quoter.getTokenPrice(tokenA.target);
      expect(p2).to.equal(0n);
      expect(t2).to.equal(0n);
      expect(s2).to.equal(true);
    });

    it("getTokenPrices is a pass-through to oracle.getPrices", async function () {
      const tokens = [tokenA.target, tokenB.target, tokenC.target];
      const [prices, timestamps, stale] = await quoter.getTokenPrices(tokens);
      expect(prices[0]).to.equal(ONE);
      expect(prices[1]).to.equal(2n * ONE);
      expect(prices[2]).to.equal(5n * ONE);
      expect(timestamps.length).to.equal(3);
      expect(stale.every((s) => s === false)).to.equal(true);
    });
  });

  // ===================================================================== //
  // _getFeeBps via low-level staticcall                                   //
  // ===================================================================== //
  describe("_getFeeBps surface", function () {
    it("pecor==address(0) → feeBps=0 (covered above; sanity asserted here)", async function () {
      const r = await quoter.quoteExactIn(tokenA.target, tokenB.target, ONE);
      expect(r.feeBps).to.equal(0n);
    });

    it("pecor returns a value → quoter reads it through staticcall", async function () {
      const beacon = await deployPECOR({ priceOracle: priceOracle.target, vault: vault.target, weth: weth.target, admin: admin.address });
      await beacon.setSwapFee(75n);
      const q2 = await deployMetaAGQuoter({
        priceOracle: priceOracle.target,
        vault: vault.target,
        weth: weth.target,
        pecor: beacon.target,
        admin: admin.address,
      });
      const r = await q2.quoteExactIn(tokenA.target, tokenB.target, ONE);
      expect(r.feeBps).to.equal(75n);
    });

    it("pecor reverts → feeBps falls back to 0 (no propagation)", async function () {
      const beacon = await deployFeeBpsBeacon(50n);
      await beacon.setRevert(true);
      const q2 = await deployMetaAGQuoter({
        priceOracle: priceOracle.target,
        vault: vault.target,
        weth: weth.target,
        pecor: beacon.target,
        admin: admin.address,
      });
      const r = await q2.quoteExactIn(tokenA.target, tokenB.target, ONE);
      expect(r.feeBps).to.equal(0n);
    });
  });

  // ===================================================================== //
  // UUPS upgrade authorization (S1)                                       //
  // ===================================================================== //
  describe("UUPS upgrade authorization", function () {
    it("admin can upgrade to a new MetaAGQuoter implementation", async function () {
      const Impl = await ethers.getContractFactory("MetaAGQuoter");
      const newImpl = await Impl.deploy();
      await newImpl.waitForDeployment();
      // upgradeToAndCall with empty data is the canonical UUPS upgrade path.
      await expect(quoter.connect(admin).upgradeToAndCall(newImpl.target, "0x")).to.not
        .be.reverted;
    });

    it("non-admin cannot upgrade (DEFAULT_ADMIN_ROLE gate)", async function () {
      const Impl = await ethers.getContractFactory("MetaAGQuoter");
      const newImpl = await Impl.deploy();
      await newImpl.waitForDeployment();
      await expect(
        quoter.connect(other).upgradeToAndCall(newImpl.target, "0x")
      ).to.be.revertedWithCustomError(quoter, ERRORS.common.Unauthorized);
    });
  });
});
