/**
 * Sidiora Meta-AG — Phase 2 oracle-layer integration suite (Task 2.5)
 *
 * Wires PriceOracle + PriceOracleAdapter + SidioraFeedAdapter + OracleHub end-to-end,
 * covering the five scenarios mandated by the Phase 2 plan:
 *   (a) Happy path — register token, push price, adapter returns value through hub.
 *   (b) Deviation circuit-breaker filters outliers across adapters.
 *   (c) Confidence gating skips low-confidence adapters.
 *   (d) Staleness: once PriceOracleAdapter is stale, SidioraFeedAdapter still serves.
 *   (e) Pause propagation — hub pause blocks reads; oracle pause blocks pushes.
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const { SOURCE_IDS, CONFIDENCE, BPS } = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const {
  deployPriceOracle,
  deployOracleHub,
  deployPriceOracleAdapter,
  deploySidioraFeedAdapter,
} = require("../helpers/fixtures");
const { increaseTime, pushPrice } = require("../helpers/oracle");

const ONE = 10n ** 18n;
const MIN_LIQUIDITY = ONE * 1000n;

async function makeToken(label) {
  const ERC20 = await ethers.getContractFactory("MockERC20");
  const t = await ERC20.deploy(label, label.slice(0, 4).toUpperCase(), 18);
  await t.waitForDeployment();
  return t;
}

async function deployPool() {
  const F = await ethers.getContractFactory("MockSidioraPool");
  const p = await F.deploy();
  await p.waitForDeployment();
  return p;
}

async function deployRegistry() {
  const F = await ethers.getContractFactory("MockSidioraPoolRegistry");
  const r = await F.deploy();
  await r.waitForDeployment();
  return r;
}

describe("meta-ag/oracle/integration", function () {
  let admin, relayer;
  let priceOracle, hub, pocAdapter, sidAdapter;
  let registry, poolA, tokenA;

  beforeEach(async function () {
    [admin, relayer] = await ethers.getSigners();

    priceOracle = await deployPriceOracle({ admin: admin.address });

    hub = await deployOracleHub({
      admin: admin.address,
      primaryOracle: priceOracle.target,
      deviationBps: BPS.DEFAULT_ORACLE_DEVIATION, // 5%
      minConfidence: 3000n,
    });

    pocAdapter = await deployPriceOracleAdapter({ priceOracle: priceOracle.target });

    registry = await deployRegistry();
    sidAdapter = await deploySidioraFeedAdapter({
      poolRegistry: registry.target,
      minLiquidityThreshold: MIN_LIQUIDITY,
      admin: admin.address,
    });

    const reference = await (await ethers.getContractFactory("MockUsdReference")).deploy();
    await sidAdapter.connect(admin).configureSpotReference(true, reference.target, admin.address);

    // Priority 10 = PriceOracle (preferred), priority 20 = Sidiora (fallback).
    await hub.connect(admin).registerAdapter(pocAdapter.target, 10);
    await hub.connect(admin).registerAdapter(sidAdapter.target, 20);

    tokenA = await makeToken("TokenA");
    poolA = await deployPool();
    await registry.setPoolByToken(tokenA.target, poolA.target);

    await priceOracle.connect(admin).setRelayer(relayer.address, true);
    await priceOracle
      .connect(admin)
      .registerToken(
        tokenA.target,
        60, // heartbeat
        100, // deviationBps
        ONE / 100n, // min
        ONE * 100_000n, // max
        3600 // maxStaleness
      );
  });

  it("(a) end-to-end: price pushed to oracle is routed through hub via PriceOracleAdapter", async function () {
    await pushPrice({ priceOracle, relayer, token: tokenA.target, price: ONE * 2n });
    expect(await hub.getPrice(tokenA.target)).to.equal(ONE * 2n);

    const agg = await hub.getAggregatedPrice(tokenA.target);
    expect(agg.sourceCount).to.equal(1n);
    expect(agg.price).to.equal(ONE * 2n);
    expect(agg.primarySource).to.equal(SOURCE_IDS.PAXEER_PRICE_ORACLE);
    expect(agg.confidence).to.equal(CONFIDENCE.PRICE_ORACLE_FRESH);
  });

  it("(b) deviation circuit-breaker — Sidiora pool price 10% away is filtered out", async function () {
    // Primary (PriceOracle) reports $1; Sidiora reports $1.10 (10% above → > 5% band)
    await pushPrice({ priceOracle, relayer, token: tokenA.target, price: ONE });
    await poolA.setPrice((ONE * 110n) / 100n);
    await poolA.setReserves(0, MIN_LIQUIDITY, ONE * 10_000n); // HIGH confidence

    const agg = await hub.getAggregatedPrice(tokenA.target);
    expect(agg.sourceCount).to.equal(1n);
    expect(agg.price).to.equal(ONE);
    expect(agg.primarySource).to.equal(SOURCE_IDS.PAXEER_PRICE_ORACLE);
  });

  it("(c) confidence gating — Sidiora thin pool below minConfidence is skipped", async function () {
    await poolA.setPrice(ONE);
    // Thin pool → confidence 1500 (LOW) < 3000 minConfidence
    await poolA.setReserves(0, MIN_LIQUIDITY / 8n, ONE * 10_000n);

    // PriceOracle never pushed → adapter returns confidence 0 (also skipped)
    await expect(hub.getPrice(tokenA.target)).to.be.revertedWithCustomError(
      hub,
      ERRORS.oracle.NoActiveAdapters
    );
  });

  it("(d) staleness path — Sidiora serves once PriceOracleAdapter ages past maxStaleness", async function () {
    await pushPrice({ priceOracle, relayer, token: tokenA.target, price: ONE });
    // Sidiora has a healthy pool reporting a compatible price
    await poolA.setPrice(ONE);
    await poolA.setReserves(0, MIN_LIQUIDITY, ONE * 10_000n);

    // First call: PriceOracle is fresh → served by pocAdapter
    let agg = await hub.getAggregatedPrice(tokenA.target);
    expect(agg.primarySource).to.equal(SOURCE_IDS.PAXEER_PRICE_ORACLE);

    // Age past PriceOracleAdapter staleness (3600s) but within Sidiora's mock 120s.
    // Mocks report `block.timestamp`, so Sidiora stays fresh regardless of wall-clock.
    await increaseTime(3700);

    // getPrice must now fall through to Sidiora (priority 20)
    expect(await hub.getPrice(tokenA.target)).to.equal(ONE);
    agg = await hub.getAggregatedPrice(tokenA.target);
    expect(agg.sourceCount).to.equal(1n);
    expect(agg.primarySource).to.equal(SOURCE_IDS.SIDIORA_AMM);
    expect(agg.price).to.equal(ONE);
  });

  it("(e) pause propagation — hub pause blocks reads; oracle pause blocks pushes", async function () {
    await pushPrice({ priceOracle, relayer, token: tokenA.target, price: ONE });

    // Hub pause → reads revert, pushes upstream still work
    await hub.connect(admin).pause();
    await expect(hub.getPrice(tokenA.target)).to.be.revertedWithCustomError(
      hub,
      ERRORS.common.Paused
    );
    await pushPrice({ priceOracle, relayer, token: tokenA.target, price: ONE * 2n });
    await hub.connect(admin).unpause();
    expect(await hub.getPrice(tokenA.target)).to.equal(ONE * 2n);

    // Oracle pause → pushes revert, hub reads still work (stale tolerance permitting)
    await priceOracle.connect(admin).pause();
    await expect(
      priceOracle.connect(relayer).updatePrice(tokenA.target, ONE * 3n)
    ).to.be.revertedWithCustomError(priceOracle, ERRORS.common.Paused);
    expect(await hub.getPrice(tokenA.target)).to.equal(ONE * 2n);
  });
});
