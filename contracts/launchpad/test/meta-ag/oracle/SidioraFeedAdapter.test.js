/**
 * Sidiora Meta-AG — SidioraFeedAdapter unit tests (Phase 2 / Task 2.4)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.4
 *
 * Invariants exercised:
 *   I7 — Never reverts on stale/missing data.
 *   I8 — Confidence banding: 0 / low(1500) / medium(4000) / high(7000).
 *   I9 — SidioraFeedAdapter uses block.timestamp (documented exception).
 *   S10 — SidioraFeedAdapter sourceId is `keccak256("SidioraAMM.v1")`.
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const { SOURCE_IDS, CONFIDENCE, STALENESS, ZERO_ADDRESS } = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const {
  deploySidioraFeedAdapter,
} = require("../helpers/fixtures");

const ONE = 10n ** 18n;
const MIN_LIQUIDITY = ONE * 1000n; // 1000 USDL threshold

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

describe("meta-ag/oracle/SidioraFeedAdapter", function () {
  let admin, other;
  let registry, adapter, tokenA, tokenB, pool;

  beforeEach(async function () {
    [admin, other] = await ethers.getSigners();
    registry = await deployRegistry();
    adapter = await deploySidioraFeedAdapter({
      poolRegistry: registry.target,
      minLiquidityThreshold: MIN_LIQUIDITY,
      admin: admin.address,
    });
    const reference = await (await ethers.getContractFactory("MockUsdReference")).deploy();
    await adapter.connect(admin).configureSpotReference(true, reference.target, admin.address);

    tokenA = await makeToken("TokenA");
    tokenB = await makeToken("TokenB");
    pool = await deployPool();
    await registry.setPoolByToken(tokenA.target, pool.target);
  });

  it("sourceId = keccak256('SidioraAMM.v1'); adapterName + maxStaleness stable (S10)", async function () {
    expect(await adapter.sourceId()).to.equal(SOURCE_IDS.SIDIORA_AMM);
    expect(await adapter.adapterName()).to.equal("SidioraAMM.v1");
    expect(await adapter.maxStaleness()).to.equal(BigInt(STALENESS.SIDIORA_SECONDS));
  });

  it("supportsToken reflects pool registry entries; getSupportedTokens echoes registered known tokens", async function () {
    expect(await adapter.supportsToken(tokenA.target)).to.equal(true);
    expect(await adapter.supportsToken(tokenB.target)).to.equal(false);
    await adapter.connect(admin).registerKnownToken(tokenA.target);
    expect(await adapter.getSupportedTokens()).to.deep.equal([tokenA.target]);
  });

  it("getFeedPrice returns HIGH confidence when realUsdl >= minLiquidityThreshold", async function () {
    await pool.setPrice(ONE * 2n);
    await pool.setReserves(ONE * 500n, MIN_LIQUIDITY, ONE * 10_000n);
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.price).to.equal(ONE * 2n);
    expect(feed.confidence).to.equal(CONFIDENCE.SIDIORA_HIGH);
    expect(feed.sourceId).to.equal(SOURCE_IDS.SIDIORA_AMM);
    // I9 documented exception — Sidiora pool uses block.timestamp because AMM updates
    // implicitly on every trade.
    const block = await ethers.provider.getBlock("latest");
    expect(feed.timestamp).to.equal(BigInt(block.timestamp));
  });

  it("getFeedPrice bands to MEDIUM when realUsdl >= threshold/4", async function () {
    await pool.setPrice(ONE);
    await pool.setReserves(ONE * 500n, MIN_LIQUIDITY / 2n, ONE * 10_000n);
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.confidence).to.equal(CONFIDENCE.SIDIORA_MEDIUM);
  });

  it("getFeedPrice bands to LOW when realUsdl < threshold/4", async function () {
    await pool.setPrice(ONE);
    await pool.setReserves(ONE * 500n, MIN_LIQUIDITY / 8n, ONE * 10_000n);
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.confidence).to.equal(CONFIDENCE.SIDIORA_LOW);
  });

  it("getFeedPrice returns zero when no pool registered (I7)", async function () {
    const feedB = await adapter.getFeedPrice(tokenB.target);
    expect(feedB.price).to.equal(0n);
    expect(feedB.confidence).to.equal(0n);
    expect(feedB.timestamp).to.equal(0n);
  });

  it("getFeedPrice does not revert when pool.getPrice reverts (I7)", async function () {
    await pool.setPrice(ONE);
    await pool.setRevertOnGetPrice(true);
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.price).to.equal(0n);
    expect(feed.confidence).to.equal(0n);
  });

  it("getFeedPrice does not revert when registry.getPoolByToken reverts (I7)", async function () {
    await registry.setRevertOnLookup(true);
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.price).to.equal(0n);
    expect(feed.confidence).to.equal(0n);
  });

  it("getFeedPrice falls back to LOW confidence when pool.getReserves reverts but price is present", async function () {
    await pool.setPrice(ONE * 3n);
    await pool.setReserves(ONE * 500n, MIN_LIQUIDITY, ONE * 10_000n);
    await pool.setRevertOnGetReserves(true);
    const feed = await adapter.getFeedPrice(tokenA.target);
    expect(feed.price).to.equal(ONE * 3n);
    expect(feed.confidence).to.equal(CONFIDENCE.SIDIORA_LOW);
  });

  it("getFeedPrices (batch) returns parallel results per token", async function () {
    const tokenC = await makeToken("TokenC");
    const poolC = await deployPool();
    await registry.setPoolByToken(tokenC.target, poolC.target);

    await pool.setPrice(ONE);
    await pool.setReserves(0, MIN_LIQUIDITY, ONE * 10_000n);
    await poolC.setPrice(ONE * 4n);
    await poolC.setReserves(0, MIN_LIQUIDITY / 8n, ONE * 10_000n);

    const batch = await adapter.getFeedPrices([tokenA.target, tokenB.target, tokenC.target]);
    expect(batch.length).to.equal(3);
    expect(batch[0].price).to.equal(ONE);
    expect(batch[0].confidence).to.equal(CONFIDENCE.SIDIORA_HIGH);
    expect(batch[1].price).to.equal(0n);
    expect(batch[1].confidence).to.equal(0n);
    expect(batch[2].price).to.equal(ONE * 4n);
    expect(batch[2].confidence).to.equal(CONFIDENCE.SIDIORA_LOW);
  });

  it("admin can update poolRegistry + minLiquidityThreshold; non-admin reverts", async function () {
    const newRegistry = await deployRegistry();
    await expect(
      adapter.connect(other).setPoolRegistry(newRegistry.target)
    ).to.be.revertedWithCustomError(adapter, ERRORS.common.Unauthorized);
    await adapter.connect(admin).setPoolRegistry(newRegistry.target);
    expect(await adapter.poolRegistry()).to.equal(newRegistry.target);

    await adapter.connect(admin).setMinLiquidityThreshold(MIN_LIQUIDITY * 2n);
    expect(await adapter.minLiquidityThreshold()).to.equal(MIN_LIQUIDITY * 2n);

    await expect(
      adapter.connect(admin).setPoolRegistry(ZERO_ADDRESS)
    ).to.be.revertedWithCustomError(adapter, ERRORS.common.ZeroAddress);
  });

  it("registerKnownToken/registerKnownTokens are admin-only and deduplicate", async function () {
    await expect(
      adapter.connect(other).registerKnownToken(tokenA.target)
    ).to.be.revertedWithCustomError(adapter, ERRORS.common.Unauthorized);
    await adapter.connect(admin).registerKnownToken(tokenA.target);
    await adapter.connect(admin).registerKnownToken(tokenA.target); // dedupe
    const list = await adapter.getSupportedTokens();
    expect(list.length).to.equal(1);

    await adapter.connect(admin).registerKnownTokens([tokenA.target, tokenB.target]);
    expect(await adapter.getSupportedTokens()).to.deep.equal([tokenA.target, tokenB.target]);
  });
});
