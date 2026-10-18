/**
 * Sidiora Meta-AG — PriceOracle unit tests (Phase 2 / Task 2.1)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.1
 * Invariants exercised: S1 (Timelock-only upgrades), S12 (storage append-only gap at tail).
 */

const { expect } = require("chai");
const { ethers, artifacts } = require("hardhat");

const {
  PECOR_ROLES,
  ZERO_ADDRESS,
} = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const {
  deployPriceOracle,
} = require("../helpers/fixtures");
const {
  increaseTime,
  pushPrice,
  batchPushPrices,
  buildTwapSeries,
  driveTwapSeries,
} = require("../helpers/oracle");

const ONE = 10n ** 18n;

async function makeToken(label) {
  const ERC20 = await ethers.getContractFactory("MockERC20");
  const t = await ERC20.deploy(label, label.slice(0, 4).toUpperCase(), 18);
  await t.waitForDeployment();
  return t;
}

function priceBound() {
  return {
    heartbeat: 60, // seconds
    deviationBps: 100n, // 1%
    minPrice: ONE / 100n, // $0.01
    maxPrice: ONE * 100_000n, // $100k
    maxStaleness: 3600, // 1h
  };
}

describe("meta-ag/oracle/PriceOracle", function () {
  let admin, relayer, other, alice;
  let oracle;
  let tokenA, tokenB;

  beforeEach(async function () {
    [admin, relayer, other, alice] = await ethers.getSigners();
    oracle = await deployPriceOracle({ admin: admin.address });
    tokenA = await makeToken("TokenA");
    tokenB = await makeToken("TokenB");
  });

  // ------------------------------------------------------------------- //
  // Initialization                                                       //
  // ------------------------------------------------------------------- //
  describe("initialize", function () {
    it("grants DEFAULT_ADMIN_ROLE to the admin argument", async function () {
      expect(await oracle.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)).to.equal(true);
    });

    it("does not grant DEFAULT_ADMIN_ROLE to the deployer or any other EOA", async function () {
      expect(await oracle.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, other.address)).to.equal(false);
      expect(await oracle.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, alice.address)).to.equal(false);
    });

    it("reverts on re-initialization", async function () {
      await expect(oracle.initialize(admin.address)).to.be.revertedWithCustomError(
        oracle,
        "AlreadyInitialized"
      );
    });

    it("exposes PRICE_DECIMALS = 18", async function () {
      expect(await oracle.PRICE_DECIMALS()).to.equal(18);
    });
  });

  // ------------------------------------------------------------------- //
  // Role management                                                      //
  // ------------------------------------------------------------------- //
  describe("relayer role gate", function () {
    it("setRelayer is admin-only", async function () {
      await expect(
        oracle.connect(other).setRelayer(relayer.address, true)
      ).to.be.revertedWithCustomError(oracle, "MissingRole");
    });

    it("admin can grant RELAYER_ROLE and emits RelayerUpdated", async function () {
      await expect(oracle.connect(admin).setRelayer(relayer.address, true))
        .to.emit(oracle, "RelayerUpdated")
        .withArgs(relayer.address, true);
      expect(await oracle.isAuthorizedRelayer(relayer.address)).to.equal(true);
      expect(await oracle.hasRole(PECOR_ROLES.RELAYER_ROLE, relayer.address)).to.equal(true);
    });

    it("admin can revoke RELAYER_ROLE", async function () {
      await oracle.connect(admin).setRelayer(relayer.address, true);
      await oracle.connect(admin).setRelayer(relayer.address, false);
      expect(await oracle.isAuthorizedRelayer(relayer.address)).to.equal(false);
      expect(await oracle.hasRole(PECOR_ROLES.RELAYER_ROLE, relayer.address)).to.equal(false);
    });

    it("setRelayer rejects the zero address", async function () {
      await expect(
        oracle.connect(admin).setRelayer(ZERO_ADDRESS, true)
      ).to.be.revertedWithCustomError(oracle, ERRORS.common.ZeroAddress);
    });

    it("updatePrice reverts without RELAYER_ROLE", async function () {
      const b = priceBound();
      await oracle.connect(admin).registerToken(
        tokenA.target,
        b.heartbeat,
        b.deviationBps,
        b.minPrice,
        b.maxPrice,
        b.maxStaleness
      );
      await expect(
        oracle.connect(other).updatePrice(tokenA.target, ONE * 2n)
      ).to.be.revertedWithCustomError(oracle, "MissingRole");
    });
  });

  // ------------------------------------------------------------------- //
  // Token registration / configuration                                   //
  // ------------------------------------------------------------------- //
  describe("token registration", function () {
    it("registerToken stores config and emits TokenRegistered", async function () {
      const b = priceBound();
      await expect(
        oracle
          .connect(admin)
          .registerToken(
            tokenA.target,
            b.heartbeat,
            b.deviationBps,
            b.minPrice,
            b.maxPrice,
            b.maxStaleness
          )
      )
        .to.emit(oracle, "TokenRegistered")
        .withArgs(
          tokenA.target,
          b.heartbeat,
          b.deviationBps,
          b.minPrice,
          b.maxPrice,
          b.maxStaleness
        );
      const cfg = await oracle.getTokenConfig(tokenA.target);
      expect(cfg.isRegistered).to.equal(true);
      expect(cfg.heartbeatInterval).to.equal(BigInt(b.heartbeat));
      expect(cfg.deviationThresholdBps).to.equal(b.deviationBps);
      expect(cfg.minPriceBound).to.equal(b.minPrice);
      expect(cfg.maxPriceBound).to.equal(b.maxPrice);
      expect(cfg.maxStaleness).to.equal(BigInt(b.maxStaleness));
      const tokens = await oracle.getRegisteredTokens();
      expect(tokens).to.deep.equal([tokenA.target]);
    });

    it("registerToken reverts on duplicate", async function () {
      const b = priceBound();
      await oracle
        .connect(admin)
        .registerToken(
          tokenA.target,
          b.heartbeat,
          b.deviationBps,
          b.minPrice,
          b.maxPrice,
          b.maxStaleness
        );
      await expect(
        oracle
          .connect(admin)
          .registerToken(
            tokenA.target,
            b.heartbeat,
            b.deviationBps,
            b.minPrice,
            b.maxPrice,
            b.maxStaleness
          )
      ).to.be.revertedWithCustomError(oracle, "TokenAlreadyRegistered");
    });

    it("registerToken rejects zero address / invalid bounds", async function () {
      const b = priceBound();
      await expect(
        oracle
          .connect(admin)
          .registerToken(ZERO_ADDRESS, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness)
      ).to.be.revertedWithCustomError(oracle, ERRORS.common.ZeroAddress);
      await expect(
        oracle
          .connect(admin)
          .registerToken(tokenA.target, 0, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness)
      ).to.be.revertedWithCustomError(oracle, "InvalidConfig");
      await expect(
        oracle
          .connect(admin)
          .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.maxPrice, b.minPrice, b.maxStaleness)
      ).to.be.revertedWithCustomError(oracle, "InvalidConfig");
    });

    it("updateTokenConfig rejects invalid bounds / heartbeat / staleness / deviation", async function () {
      const b = priceBound();
      await oracle
        .connect(admin)
        .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
      const bad1 = {
        isRegistered: true,
        heartbeatInterval: 0,
        deviationThresholdBps: 200,
        maxPriceBound: ONE * 50_000n,
        minPriceBound: ONE,
        maxStaleness: 3600,
      };
      await expect(
        oracle.connect(admin).updateTokenConfig(tokenA.target, bad1)
      ).to.be.revertedWithCustomError(oracle, "InvalidConfig");
      await expect(
        oracle.connect(admin).updateTokenConfig(tokenA.target, { ...bad1, heartbeatInterval: 60, maxStaleness: 0 })
      ).to.be.revertedWithCustomError(oracle, "InvalidConfig");
      await expect(
        oracle.connect(admin).updateTokenConfig(tokenA.target, {
          ...bad1,
          heartbeatInterval: 60,
          maxStaleness: 3600,
          maxPriceBound: ONE,
          minPriceBound: ONE * 2n,
        })
      ).to.be.revertedWithCustomError(oracle, "InvalidConfig");
      await expect(
        oracle.connect(admin).updateTokenConfig(tokenA.target, {
          ...bad1,
          heartbeatInterval: 60,
          maxStaleness: 3600,
          deviationThresholdBps: 10_001,
        })
      ).to.be.revertedWithCustomError(oracle, "InvalidConfig");
    });

    it("updateTokenConfig reverts when token is unregistered", async function () {
      const next = {
        isRegistered: true,
        heartbeatInterval: 60,
        deviationThresholdBps: 100,
        maxPriceBound: ONE * 1_000n,
        minPriceBound: ONE,
        maxStaleness: 3600,
      };
      await expect(
        oracle.connect(admin).updateTokenConfig(tokenB.target, next)
      ).to.be.revertedWithCustomError(oracle, "TokenNotConfigured");
    });

    it("updateTokenConfig preserves registration and emits TokenConfigUpdated", async function () {
      const b = priceBound();
      await oracle
        .connect(admin)
        .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
      const next = {
        isRegistered: true,
        heartbeatInterval: 120,
        deviationThresholdBps: 200,
        maxPriceBound: ONE * 50_000n,
        minPriceBound: ONE / 10n,
        maxStaleness: 1800,
      };
      await expect(oracle.connect(admin).updateTokenConfig(tokenA.target, next))
        .to.emit(oracle, "TokenConfigUpdated")
        .withArgs(
          tokenA.target,
          next.heartbeatInterval,
          next.deviationThresholdBps,
          next.minPriceBound,
          next.maxPriceBound,
          next.maxStaleness
        );
      const cfg = await oracle.getTokenConfig(tokenA.target);
      expect(cfg.isRegistered).to.equal(true);
      expect(cfg.heartbeatInterval).to.equal(120n);
      expect(cfg.maxStaleness).to.equal(1800n);
    });

    it("registerToken is admin-only", async function () {
      const b = priceBound();
      await expect(
        oracle
          .connect(other)
          .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness)
      ).to.be.revertedWithCustomError(oracle, "MissingRole");
    });
  });

  // ------------------------------------------------------------------- //
  // Price pushes (single + batch)                                        //
  // ------------------------------------------------------------------- //
  describe("price pushes", function () {
    beforeEach(async function () {
      const b = priceBound();
      await oracle.connect(admin).setRelayer(relayer.address, true);
      await oracle
        .connect(admin)
        .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
      await oracle
        .connect(admin)
        .registerToken(tokenB.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
    });

    it("updatePrice emits PriceUpdated with incremented round", async function () {
      const price = ONE * 2n;
      const tx = await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price });
      const rec = await tx.wait();
      const block = await ethers.provider.getBlock(rec.blockNumber);
      await expect(tx)
        .to.emit(oracle, "PriceUpdated")
        .withArgs(tokenA.target, price, 1n, relayer.address, BigInt(block.timestamp));
      expect(await oracle.getPrice(tokenA.target)).to.equal(price);
    });

    it("updatePrice rejects price out of bounds", async function () {
      await expect(
        pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: 0n })
      ).to.be.revertedWithCustomError(oracle, "PriceOutOfBounds");
      const tooHigh = ONE * 1_000_000n;
      await expect(
        pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: tooHigh })
      ).to.be.revertedWithCustomError(oracle, "PriceOutOfBounds");
    });

    it("updatePrice reverts when token is not registered", async function () {
      const tokenC = await makeToken("TokenC");
      await expect(
        pushPrice({ priceOracle: oracle, relayer, token: tokenC.target, price: ONE })
      ).to.be.revertedWithCustomError(oracle, "TokenNotConfigured");
    });

    it("batchUpdatePrices updates all registered tokens and emits BatchPriceUpdate", async function () {
      const tokens = [tokenA.target, tokenB.target];
      const prices = [ONE * 2n, ONE * 3n];
      const tx = await batchPushPrices({ priceOracle: oracle, relayer, tokens, prices });
      const rec = await tx.wait();
      const block = await ethers.provider.getBlock(rec.blockNumber);
      await expect(tx)
        .to.emit(oracle, "BatchPriceUpdate")
        .withArgs(relayer.address, 2, BigInt(block.timestamp));
      expect(await oracle.getPrice(tokenA.target)).to.equal(prices[0]);
      expect(await oracle.getPrice(tokenB.target)).to.equal(prices[1]);
    });

    it("batchUpdatePrices reverts on length mismatch / empty arrays", async function () {
      await expect(
        oracle.connect(relayer).batchUpdatePrices([tokenA.target], [ONE, ONE * 2n])
      ).to.be.revertedWithCustomError(oracle, ERRORS.common.InvalidArrayLength);
      await expect(
        oracle.connect(relayer).batchUpdatePrices([], [])
      ).to.be.revertedWithCustomError(oracle, ERRORS.common.InvalidArrayLength);
    });

    it("batchUpdatePrices silently skips unregistered tokens inside the batch", async function () {
      const tokenC = await makeToken("TokenC");
      const tokens = [tokenA.target, tokenC.target, tokenB.target];
      const prices = [ONE * 2n, ONE * 5n, ONE * 3n];
      await batchPushPrices({ priceOracle: oracle, relayer, tokens, prices });
      // Registered tokens updated; unregistered ignored (does not revert).
      expect(await oracle.getPrice(tokenA.target)).to.equal(prices[0]);
      expect(await oracle.getPrice(tokenB.target)).to.equal(prices[2]);
      const cfgC = await oracle.getTokenConfig(tokenC.target);
      expect(cfgC.isRegistered).to.equal(false);
    });
  });

  // ------------------------------------------------------------------- //
  // Staleness                                                            //
  // ------------------------------------------------------------------- //
  describe("staleness", function () {
    beforeEach(async function () {
      const b = priceBound();
      await oracle.connect(admin).setRelayer(relayer.address, true);
      await oracle
        .connect(admin)
        .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
    });

    it("getPrice reverts once block.timestamp - lastUpdate > maxStaleness", async function () {
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      await increaseTime(3601);
      await expect(oracle.getPrice(tokenA.target)).to.be.revertedWithCustomError(
        oracle,
        ERRORS.oracle.StalePrice
      );
      expect(await oracle.isPriceStale(tokenA.target)).to.equal(true);
    });

    it("getPrices (batch) reports stale flag without reverting", async function () {
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      await increaseTime(3601);
      const [prices, timestamps, stale] = await oracle.getPrices([tokenA.target]);
      expect(prices[0]).to.equal(ONE);
      expect(timestamps[0]).to.be.greaterThan(0n);
      expect(stale[0]).to.equal(true);
    });
  });

  // ------------------------------------------------------------------- //
  // TWAP accumulator                                                     //
  // ------------------------------------------------------------------- //
  describe("TWAP accumulator", function () {
    beforeEach(async function () {
      const b = { ...priceBound(), maxStaleness: 86_400 };
      await oracle.connect(admin).setRelayer(relayer.address, true);
      await oracle
        .connect(admin)
        .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
    });

    it("getTWAP tracks a sinusoidal series within 2% of the base price", async function () {
      const base = ONE * 100n;
      const series = buildTwapSeries({ basePrice: base, samples: 8, periodSec: 60, amplitudeBps: 100 });
      await driveTwapSeries({ priceOracle: oracle, relayer, token: tokenA.target, series });
      const twap = await oracle.getTWAP(tokenA.target, 60 * 8);
      const tol = (base * 200n) / 10_000n; // 2%
      expect(twap).to.be.greaterThan(base - tol);
      expect(twap).to.be.lessThan(base + tol);
    });

    it("getTWAP reverts with TwapWindowInvalid when period = 0", async function () {
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      await expect(oracle.getTWAP(tokenA.target, 0)).to.be.revertedWithCustomError(
        oracle,
        ERRORS.oracle.TwapWindowInvalid
      );
    });
  });

  // ------------------------------------------------------------------- //
  // History                                                              //
  // ------------------------------------------------------------------- //
  describe("history", function () {
    beforeEach(async function () {
      const b = { ...priceBound(), maxStaleness: 86_400 };
      await oracle.connect(admin).setRelayer(relayer.address, true);
      await oracle
        .connect(admin)
        .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
    });

    it("getPriceHistory returns newest first", async function () {
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      await increaseTime(10);
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE * 2n });
      await increaseTime(10);
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE * 3n });
      const history = await oracle.getPriceHistory(tokenA.target, 3);
      expect(history.length).to.equal(3);
      expect(history[0].price).to.equal(ONE * 3n);
      expect(history[2].price).to.equal(ONE);
    });

    it("getPriceHistory clamps count when count > currentRound", async function () {
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      const history = await oracle.getPriceHistory(tokenA.target, 10);
      expect(history.length).to.equal(1);
      expect(history[0].price).to.equal(ONE);
    });

    it("getLatestRound + getRoundData return consistent data", async function () {
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      const latest = await oracle.getLatestRound(tokenA.target);
      const byRound = await oracle.getRoundData(tokenA.target, latest.roundId);
      expect(byRound.price).to.equal(latest.price);
      expect(byRound.timestamp).to.equal(latest.timestamp);
      expect(byRound.relayer).to.equal(latest.relayer);
    });

    it("getRoundData reverts on round=0 or round > currentRound", async function () {
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      await expect(
        oracle.getRoundData(tokenA.target, 0)
      ).to.be.revertedWithCustomError(oracle, "TokenNotConfigured");
      await expect(
        oracle.getRoundData(tokenA.target, 999)
      ).to.be.revertedWithCustomError(oracle, "TokenNotConfigured");
    });

    it("getCurrentRound exposes the monotonically-incrementing round counter", async function () {
      expect(await oracle.getCurrentRound(tokenA.target)).to.equal(0n);
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      expect(await oracle.getCurrentRound(tokenA.target)).to.equal(1n);
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE * 2n });
      expect(await oracle.getCurrentRound(tokenA.target)).to.equal(2n);
    });

    it("getPriceWithTimestamp reverts with StalePrice after staleness window", async function () {
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      await increaseTime(86_400 + 1);
      await expect(
        oracle.getPriceWithTimestamp(tokenA.target)
      ).to.be.revertedWithCustomError(oracle, ERRORS.oracle.StalePrice);
    });

    it("getPriceWithTimestamp returns fresh data", async function () {
      await pushPrice({ priceOracle: oracle, relayer, token: tokenA.target, price: ONE });
      const [p, ts, rid] = await oracle.getPriceWithTimestamp(tokenA.target);
      expect(p).to.equal(ONE);
      expect(rid).to.equal(1n);
      expect(ts).to.be.greaterThan(0n);
    });

    it("getPriceWithTimestamp reverts when no price has been pushed yet", async function () {
      await expect(
        oracle.getPriceWithTimestamp(tokenA.target)
      ).to.be.revertedWithCustomError(oracle, ERRORS.oracle.StalePrice);
    });

    it("getPrices (batch) reports stale flag for unregistered tokens without reverting", async function () {
      const tokenC = await makeToken("TokenC");
      const [prices, timestamps, stale] = await oracle.getPrices([tokenA.target, tokenC.target]);
      expect(prices[0]).to.equal(0n);
      expect(prices[1]).to.equal(0n);
      expect(stale[0]).to.equal(true);
      expect(stale[1]).to.equal(true);
      expect(timestamps[0]).to.equal(0n);
      expect(timestamps[1]).to.equal(0n);
    });
  });

  // ------------------------------------------------------------------- //
  // Pause                                                                //
  // ------------------------------------------------------------------- //
  describe("pause", function () {
    beforeEach(async function () {
      const b = priceBound();
      await oracle.connect(admin).setRelayer(relayer.address, true);
      await oracle
        .connect(admin)
        .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
    });

    it("pause blocks single + batch pushes", async function () {
      await oracle.connect(admin).pause();
      await expect(
        oracle.connect(relayer).updatePrice(tokenA.target, ONE)
      ).to.be.revertedWithCustomError(oracle, "Paused");
      await expect(
        oracle.connect(relayer).batchUpdatePrices([tokenA.target], [ONE])
      ).to.be.revertedWithCustomError(oracle, "Paused");
      await oracle.connect(admin).unpause();
      await oracle.connect(relayer).updatePrice(tokenA.target, ONE);
      expect(await oracle.getPrice(tokenA.target)).to.equal(ONE);
    });

    it("pause/unpause is admin-only", async function () {
      await expect(oracle.connect(other).pause()).to.be.revertedWithCustomError(oracle, "MissingRole");
    });
  });

  // ------------------------------------------------------------------- //
  // Upgrade authorization (S1)                                           //
  // ------------------------------------------------------------------- //
  describe("upgrade authorization (S1)", function () {
    it("upgradeToAndCall reverts for non-admin", async function () {
      const Factory = await ethers.getContractFactory("PriceOracle");
      const newImpl = await Factory.deploy();
      await newImpl.waitForDeployment();
      await expect(
        oracle.connect(other).upgradeToAndCall(newImpl.target, "0x")
      ).to.be.revertedWithCustomError(oracle, "MissingRole");
    });

    it("upgradeToAndCall succeeds for admin (Timelock)", async function () {
      const Factory = await ethers.getContractFactory("PriceOracle");
      const newImpl = await Factory.deploy();
      await newImpl.waitForDeployment();
      await oracle.connect(admin).upgradeToAndCall(newImpl.target, "0x");
      // Post-upgrade call still resolves cleanly.
      expect(await oracle.PRICE_DECIMALS()).to.equal(18);
    });
  });

  // ------------------------------------------------------------------- //
  // Storage layout — S12                                                 //
  // ------------------------------------------------------------------- //
  describe("storage layout (S12)", function () {
    it("preserves every legacy slot and appends bounded history after the legacy gap", async function () {
      // Read the on-disk build-info to find the storage layout for PriceOracle.
      const artifact = await artifacts.readArtifact("PriceOracle");
      const buildInfo = await artifacts.getBuildInfo(
        `${artifact.sourceName}:${artifact.contractName}`
      );
      const layout =
        buildInfo.output.contracts[artifact.sourceName][artifact.contractName].storageLayout;
      const expected = ["_roles", "authorizedRelayers", "_tokenConfigs", "_latestPrices", "_priceHistory", "_currentRounds", "_priceCumulativeLast", "_lastCumulativeTimestamp", "_twapSnapshots", "_twapSnapshotIndex", "_registeredTokens", "_tokenIndex", "__gap"];
      expected.forEach((label, slot) => {
        const entry = layout.storage.find(item => item.label === label);
        expect(entry, label).not.to.equal(undefined);
        expect(entry.slot, label).to.equal(String(slot));
      });
      const gap = layout.storage.find(item => item.label === "__gap");
      const gapType = layout.types[gap.type];
      ["_observations", "_observationCount", "_firstBoundedRound"].forEach((label, index) => {
        expect(layout.storage.find(item => item.label === label).slot).to.equal(String(62 + index));
      });
      // Expect fixed-size uint256[50]
      expect(gapType.encoding).to.equal("inplace");
      expect(gapType.numberOfBytes).to.equal("1600"); // 50 * 32
    });
  });
});
