/**
 * Sidiora Meta-AG — PriceOracleAdapter unit tests (Phase 2 / Task 2.3)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.3
 *
 * Invariants exercised:
 *   I7 — Adapter never reverts on missing/stale data (confidence == 0 instead).
 *   I8 — Confidence banding: 0 / low(1..3333) / mid(3334..6666) / high(6667..10000).
 *   I9 — timestamp equals source's last update time, NOT block.timestamp.
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const { SOURCE_IDS, CONFIDENCE } = require("../helpers/constants");
const {
  deployPriceOracle,
  deployPriceOracleAdapter,
} = require("../helpers/fixtures");
const { increaseTime, pushPrice } = require("../helpers/oracle");

const ONE = 10n ** 18n;

async function makeToken(label) {
  const ERC20 = await ethers.getContractFactory("MockERC20");
  const t = await ERC20.deploy(label, label.slice(0, 4).toUpperCase(), 18);
  await t.waitForDeployment();
  return t;
}

describe("meta-ag/oracle/PriceOracleAdapter", function () {
  let admin, relayer;
  let oracle, adapter, tokenA, tokenB;

  beforeEach(async function () {
    [admin, relayer] = await ethers.getSigners();
    oracle = await deployPriceOracle({ admin: admin.address });
    adapter = await deployPriceOracleAdapter({ priceOracle: oracle.target });
    tokenA = await makeToken("TokenA");
    tokenB = await makeToken("TokenB");
    await oracle.connect(admin).setRelayer(relayer.address, true);
    // Register tokenA with heartbeat=60, maxStaleness=3600
    await oracle.connect(admin).registerToken(
      tokenA.target,
      60, // heartbeat
      100, // deviationBps
      ONE / 100n, // min
      ONE * 100_000n, // max
      3600 // maxStaleness
    );
  });

  it("sourceId equals keccak256('PaxeerPriceOracle.v1') and adapterName is stable", async function () {
    expect(await adapter.sourceId()).to.equal(SOURCE_IDS.PAXEER_PRICE_ORACLE);
    expect(await adapter.adapterName()).to.equal("PaxeerPriceOracle.v1");
    expect(await adapter.maxStaleness()).to.equal(ethers.MaxUint256);
  });

  it("supportsToken + getSupportedTokens delegate to PriceOracle", async function () {
    expect(await adapter.supportsToken(tokenA.target)).to.equal(true);
    expect(await adapter.supportsToken(tokenB.target)).to.equal(false);
    const registered = await adapter.getSupportedTokens();
    expect(registered).to.deep.equal([tokenA.target]);
  });

  it("getFeedPrice returns FRESH confidence when age <= heartbeat (I8, I9)", async function () {
    await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE * 2n });
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.price).to.equal(ONE * 2n);
    expect(feed.confidence).to.equal(CONFIDENCE.PRICE_ORACLE_FRESH);
    expect(feed.sourceId).to.equal(SOURCE_IDS.PAXEER_PRICE_ORACLE);
    // I9: timestamp is the source's last update, not block.timestamp
    const latest = await oracle.getLatestRound(tokenA.target);
    expect(feed.timestamp).to.equal(latest.timestamp);
  });

  it("getFeedPrice bands to AGING when heartbeat < age <= 2 * heartbeat", async function () {
    await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
    await increaseTime(90); // > 60, <= 120
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.confidence).to.equal(CONFIDENCE.PRICE_ORACLE_AGING);
  });

  it("getFeedPrice bands to NEAR_STALE when 2*heartbeat < age <= maxStaleness", async function () {
    await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
    await increaseTime(500); // > 120, <= 3600
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.confidence).to.equal(CONFIDENCE.PRICE_ORACLE_NEAR_STALE);
  });

  it("getFeedPrice returns zero confidence when age > maxStaleness (I7)", async function () {
    await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
    await increaseTime(3601);
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.confidence).to.equal(0n);
    // Adapter MUST NOT revert
    const batch = await adapter.getFeedPrices([tokenA.target]);
    expect(batch[0].confidence).to.equal(0n);
  });

  it("getFeedPrice on unregistered / never-pushed token returns all zeros (I7)", async function () {
    const feedB = await adapter.getFeedPrice(tokenB.target); // unregistered
    expect(feedB.price).to.equal(0n);
    expect(feedB.confidence).to.equal(0n);
    expect(feedB.timestamp).to.equal(0n);

    // Registered but no price pushed
    const tokenC = await makeToken("TokenC");
    await oracle.connect(admin).registerToken(
      tokenC.target,
      60,
      100,
      ONE / 100n,
      ONE * 100_000n,
      3600
    );
    const feedC = await adapter.getFeedPrice(tokenC.target);
    expect(feedC.price).to.equal(0n);
    expect(feedC.confidence).to.equal(0n);
  });

  it("getFeedPrices (batch) mirrors getFeedPrice for each input token", async function () {
    await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE * 3n });
    const batch = await adapter.getFeedPrices([tokenA.target, tokenB.target]);
    expect(batch.length).to.equal(2);
    expect(batch[0].price).to.equal(ONE * 3n);
    expect(batch[0].confidence).to.equal(CONFIDENCE.PRICE_ORACLE_FRESH);
    expect(batch[1].price).to.equal(0n);
    expect(batch[1].confidence).to.equal(0n);
  });
});
