/**
 * Sidiora Meta-AG — Phase 8 Integration Test 8.3
 *
 * Oracle circuit-breaker scenarios across the dual-adapter pipeline:
 *   - PriceOracleAdapter (priority 10, reads PriceOracle relayer pushes)
 *   - SidioraFeedAdapter (priority 20, reads MockSidioraPool via registry)
 *
 * Scope:
 *   - OracleHub.getAggregatedPrice deviation circuit-breaker (S8): when one
 *     adapter is >deviationThresholdBps away from the highest-priority valid
 *     reference, it's filtered from the median.
 *   - MetaAGRouter._oracleSanityCheck (S4):
 *       (a) skip silently when either price is unavailable.
 *       (b) revert OracleSanityFailed when realized output deviates beyond
 *           maxOracleSanityDeviation.
 *   - Fail-closed: hub.getPrice reverts NoActiveAdapters when no adapter
 *     surfaces a usable price.
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.2, §10.4, §12.
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const { ADAPTER_IDS, BPS } = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const { deployPecorFixture } = require("../helpers/fixtures");

const ONE = 10n ** 18n;

async function makePool(price, realUsdlReserve = 0n) {
  const F = await ethers.getContractFactory("MockSidioraPool");
  const p = await F.deploy();
  await p.waitForDeployment();
  await p.setPrice(price);
  // (virtualUsdl, realUsdl, tokenReserve) — only realUsdl matters for
  // SidioraFeedAdapter confidence banding. Use 0 minLiquidityThreshold in
  // the fixture so any real reserve clears the LOW band.
  await p.setReserves(0n, realUsdlReserve, 0n);
  return p;
}

describe("meta-ag/integration/8.3 — pecor-oracle-circuit-breaker", function () {
  let fx;
  let user;
  let usdl, sidA;
  let router, hub, oracle, sidioraAdapter, vaultAdapter;

  beforeEach(async function () {
    fx = await deployPecorFixture();
    ({ user } = fx.signers);
    ({ usdl, sidA } = fx.tokens);
    ({ router, hub, oracle, sidioraAdapter, vaultAdapter } = fx);
  });

  // =========================================================================
  // S8 — OracleHub deviation circuit-breaker
  // =========================================================================
  it("S8 — within deviation: hub aggregates BOTH PriceOracleAdapter + SidioraFeedAdapter", async function () {
    // sidA = $0.50 on PriceOracle. SidioraPool reports $0.51 (~2% deviation,
    // within 5% bound). Both adapters are valid, both contribute.
    const pool = await makePool(ONE / 2n + ONE / 100n, 1_000n * ONE); // $0.51, real reserves
    await fx.mocks.sidioraRegistry.setPoolByToken(sidA.target, pool.target);

    const agg = await hub.getAggregatedPrice(sidA.target);
    expect(agg.sourceCount).to.equal(2n);
    // primarySource is the PriceOracleAdapter (highest-priority valid)
    expect(agg.primarySource).to.not.equal(ethers.ZeroHash);
  });

  it("S8 — outlier filtered: SidioraFeedAdapter at +10% drops out of the median", async function () {
    // sidA = $0.50 on PriceOracle. SidioraPool reports $0.55 (10% deviation,
    // above 5% bound). Only PriceOracleAdapter remains in the median.
    const pool = await makePool((ONE * 55n) / 100n, 1_000n * ONE); // $0.55
    await fx.mocks.sidioraRegistry.setPoolByToken(sidA.target, pool.target);

    const agg = await hub.getAggregatedPrice(sidA.target);
    expect(agg.sourceCount).to.equal(1n);
    expect(agg.price).to.equal(ONE / 2n); // primary survives
  });

  it("S8 — fail-closed: when no adapter surfaces a valid price, getPrice reverts NoActiveAdapters", async function () {
    // Force PriceOracleAdapter to be stale, leave Sidiora unconfigured (no pool).
    // Push freshness to lower the heartbeat: skip update for an unrelated token
    // so sidA's price ages past 1h staleness.
    await ethers.provider.send("evm_increaseTime", [3601]);
    await ethers.provider.send("evm_mine", []);

    await expect(hub.getPrice(sidA.target)).to.be.revertedWithCustomError(
      hub,
      ERRORS.oracle.NoActiveAdapters
    );
  });

  // =========================================================================
  // S4 — MetaAGRouter sanity check skip-on-unavailable
  // =========================================================================
  it("S4 — sanity check skips when oracle has no price for tokenOut (Sidiora token without PriceOracle entry)", async function () {
    // Deploy a fresh sidToken NOT registered on PriceOracle. Its ABI-valid
    // staged pool reports no reference price. Router._oracleSanityCheck skips
    // silently — swap completes despite oracle having no opinion.
    const ERC20 = await ethers.getContractFactory("MockStandardERC20");
    const sidX = await ERC20.deploy("Sidiora Token X", "sidX", 18);
    await sidX.waitForDeployment();

    // Hardened SidioraAdapter probes pool ABI/optical policy independently
    // from OracleHub price availability, so an EOA is not a valid pool fixture.
    const newPool = await makePool(0n, 1000n * ONE);
    await newPool.setReserves(10000n * ONE, 1000n * ONE, 1000000n * ONE);
    await fx.mocks.sidioraRegistry.setPoolByToken(sidX.target, newPool.target);
    await expect(hub.getPrice(sidX.target)).to.be.revertedWithCustomError(hub, ERRORS.oracle.NoActiveAdapters);

    // Stage a Sidiora buy for USDL → sidX: 100 USDL → 1000 sidX (arbitrary
    // ratio — without an oracle price, no sanity gate to pass).
    const amountIn = 100n * ONE;
    const amountOut = 1_000n * ONE;
    await fx.mocks.sidioraQuoter.setBuyQuote(amountOut, 0n, 50n);
    await fx.mocks.sidioraRouter.setBuyTokenOut(sidX.target);
    await fx.mocks.sidioraRouter.setBuyReturn(amountOut);
    await sidX.mint(fx.mocks.sidioraRouter.target, amountOut);

    await usdl.mint(user.address, amountIn);
    await usdl.connect(user).approve(router.target, amountIn);

    // Should NOT revert with OracleSanityFailed (sidX has no oracle price).
    await router.connect(user).swapBestRoute(usdl.target, sidX.target, amountIn, amountOut, 0);
    expect(await sidX.balanceOf(user.address)).to.equal(amountOut);
  });

  it("S4 — sanity check reverts OracleSanityFailed when realized output deviates >5% from oracle expectation", async function () {
    // Oracle: USDL=$1, sidA=$0.50 → 100 USDL should yield ~200 sidA. Stage
    // Sidiora to overpay 250 sidA (= 25% high). Router must reject.
    const amountIn = 100n * ONE;
    const exaggeratedOut = 250n * ONE; // 25% above oracle-implied 200
    await fx.mocks.sidioraQuoter.setBuyQuote(exaggeratedOut, 0n, 50n);
    await fx.mocks.sidioraRouter.setBuyTokenOut(sidA.target);
    await fx.mocks.sidioraRouter.setBuyReturn(exaggeratedOut);
    await sidA.mint(fx.mocks.sidioraRouter.target, exaggeratedOut);

    await usdl.mint(user.address, amountIn);
    await usdl.connect(user).approve(router.target, amountIn);

    await expect(
      router
        .connect(user)
        .swapBestRoute(usdl.target, sidA.target, amountIn, exaggeratedOut, 0)
    ).to.be.revertedWithCustomError(router, ERRORS.router.OracleSanityFailed);
  });

  it("S4 — admin can disable the sanity check; flagged path no longer reverts", async function () {
    // Same setup as the previous test, but admin disables the gate first.
    await router.connect(fx.signers.admin).setOracleSanityEnabled(false);

    const amountIn = 100n * ONE;
    const exaggeratedOut = 250n * ONE;
    await fx.mocks.sidioraQuoter.setBuyQuote(exaggeratedOut, 0n, 50n);
    await fx.mocks.sidioraRouter.setBuyTokenOut(sidA.target);
    await fx.mocks.sidioraRouter.setBuyReturn(exaggeratedOut);
    await sidA.mint(fx.mocks.sidioraRouter.target, exaggeratedOut);

    await usdl.mint(user.address, amountIn);
    await usdl.connect(user).approve(router.target, amountIn);

    await router
      .connect(user)
      .swapBestRoute(usdl.target, sidA.target, amountIn, exaggeratedOut, 0);
    expect(await sidA.balanceOf(user.address)).to.equal(exaggeratedOut);
  });
});
