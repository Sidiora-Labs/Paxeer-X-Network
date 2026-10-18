/**
 * Sidiora Meta-AG — TransactionTracker unit tests (Phase 7 / Task 7.2 — deferred to Session 8)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.12 (FROZEN 2026-04-24)
 * Interface: contracts/meta-ag/interfaces/ITransactionTracker.sol
 * Contract: contracts/meta-ag/analytics/TransactionTracker.sol
 *
 * Regressions exercised:
 *   - S1   UUPS _authorizeUpgrade gated by DEFAULT_ADMIN_ROLE
 *   - S10  Recording functions callable only by EMITTER_ROLE
 *   - S12  Append-only storage — __gap[50] at slot 11
 *
 * Note: errors.js registers `analytics.EmitterNotAuthorized` but the shipped
 *       contract gates via AccessControl's generic MissingRole(address, bytes32)
 *       inherited error. Tests pin to the actual behavior.
 */

const { expect } = require("chai");
const { ethers, artifacts, network } = require("hardhat");

const { PECOR_ROLES } = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const { deployTransactionTracker } = require("../helpers/fixtures");

const ONE_DAY = 86_400;

/**
 * Compute `floor(ts / 1 days) * 1 days` in JS to mirror the contract's
 * `_getDayTimestamp` internal helper.
 */
function floorDay(ts) {
  return Math.floor(Number(ts) / ONE_DAY) * ONE_DAY;
}

describe("meta-ag/analytics/TransactionTracker", function () {
  let admin, emitter, other, user, user2;
  let tracker;
  const EMITTER_ROLE = PECOR_ROLES.EMITTER_ROLE;

  beforeEach(async function () {
    [admin, emitter, other, user, user2] = await ethers.getSigners();
    tracker = await deployTransactionTracker({ admin: admin.address });
  });

  // ------------------------------------------------------------------- //
  // initialize                                                           //
  // ------------------------------------------------------------------- //
  describe("initialize", function () {
    it("grants DEFAULT_ADMIN_ROLE to the admin argument", async function () {
      expect(
        await tracker.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)
      ).to.equal(true);
      expect(
        await tracker.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, other.address)
      ).to.equal(false);
    });

    it("seeds currentDayTimestamp to floor(block.timestamp / 1 day) * 1 day", async function () {
      const latest = await ethers.provider.getBlock("latest");
      const expected = BigInt(floorDay(latest.timestamp));
      expect(await tracker.currentDayTimestamp()).to.equal(expected);
    });

    it("reverts on re-initialization", async function () {
      await expect(tracker.initialize(admin.address)).to.be.revertedWithCustomError(
        tracker,
        ERRORS.common.AlreadyInitialized
      );
    });

    it("rejects zero admin", async function () {
      const Factory = await ethers.getContractFactory("TransactionTracker");
      const impl = await Factory.deploy();
      await impl.waitForDeployment();
      const Proxy = await ethers.getContractFactory("ERC1967Proxy");
      const initData = impl.interface.encodeFunctionData("initialize", [
        ethers.ZeroAddress,
      ]);
      await expect(
        Proxy.deploy(await impl.getAddress(), initData)
      ).to.be.revertedWithCustomError(impl, ERRORS.common.ZeroAddress);
    });
  });

  // ------------------------------------------------------------------- //
  // setAuthorizedEmitter                                                 //
  // ------------------------------------------------------------------- //
  describe("setAuthorizedEmitter", function () {
    it("grants EMITTER_ROLE + mirror bool + emits AuthorizedEmitterUpdated", async function () {
      await expect(
        tracker.connect(admin).setAuthorizedEmitter(emitter.address, true)
      )
        .to.emit(tracker, "AuthorizedEmitterUpdated")
        .withArgs(emitter.address, true);
      expect(await tracker.hasRole(EMITTER_ROLE, emitter.address)).to.equal(true);
      expect(await tracker.authorizedEmitters(emitter.address)).to.equal(true);
    });

    it("revokes EMITTER_ROLE + mirror bool", async function () {
      await tracker.connect(admin).setAuthorizedEmitter(emitter.address, true);
      await expect(
        tracker.connect(admin).setAuthorizedEmitter(emitter.address, false)
      )
        .to.emit(tracker, "AuthorizedEmitterUpdated")
        .withArgs(emitter.address, false);
      expect(await tracker.hasRole(EMITTER_ROLE, emitter.address)).to.equal(false);
      expect(await tracker.authorizedEmitters(emitter.address)).to.equal(false);
    });

    it("is admin-only", async function () {
      await expect(
        tracker.connect(other).setAuthorizedEmitter(emitter.address, true)
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
    });

    it("rejects the zero address", async function () {
      await expect(
        tracker.connect(admin).setAuthorizedEmitter(ethers.ZeroAddress, true)
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.ZeroAddress);
    });
  });

  // ------------------------------------------------------------------- //
  // forceDaySnapshot                                                     //
  // ------------------------------------------------------------------- //
  describe("forceDaySnapshot", function () {
    it("is admin-only", async function () {
      await expect(
        tracker.connect(other).forceDaySnapshot()
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
    });

    it("rolls the day pointer forward and emits DailyStatsSnapshot on closing", async function () {
      await tracker.connect(admin).setAuthorizedEmitter(emitter.address, true);
      // Record a trade so the closing-day aggregate has non-zero stats.
      await tracker
        .connect(emitter)
        .recordTrade(
          user.address,
          ethers.ZeroAddress,
          ethers.ZeroAddress,
          1000n,
          1000n,
          10n ** 18n // 1 USD
        );
      const priorDay = await tracker.currentDayTimestamp();

      // Advance > 1 day so _updateDayIfNeeded rolls forward.
      await network.provider.send("evm_increaseTime", [ONE_DAY + 60]);
      await network.provider.send("evm_mine", []);

      await expect(tracker.connect(admin).forceDaySnapshot())
        .to.emit(tracker, "DailyStatsSnapshot")
        .withArgs(priorDay, 10n ** 18n, 1n, 1n);

      // Current day advanced; previous day archived with the snapshot values.
      expect(await tracker.currentDayTimestamp()).to.be.gt(priorDay);
      const archived = await tracker.getDailyStats(priorDay);
      expect(archived.totalVolume).to.equal(10n ** 18n);
      expect(archived.totalTrades).to.equal(1n);
      expect(archived.uniqueTraders).to.equal(1n);
    });

    it("is a no-op within the same day", async function () {
      const before = await tracker.currentDayTimestamp();
      await tracker.connect(admin).forceDaySnapshot();
      expect(await tracker.currentDayTimestamp()).to.equal(before);
    });
  });

  // ------------------------------------------------------------------- //
  // recordTrade                                                          //
  // ------------------------------------------------------------------- //
  describe("recordTrade", function () {
    beforeEach(async function () {
      await tracker.connect(admin).setAuthorizedEmitter(emitter.address, true);
    });

    it("reverts without EMITTER_ROLE (S10)", async function () {
      await expect(
        tracker
          .connect(other)
          .recordTrade(
            user.address,
            ethers.ZeroAddress,
            ethers.ZeroAddress,
            1n,
            1n,
            1n
          )
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
    });

    it("aggregates daily + user + token stats and emits TradeExecuted + UserTradeMetrics", async function () {
      const tokenIn = other.address;
      const tokenOut = user2.address;
      const amountIn = 100n * 10n ** 18n;
      const amountOut = 200n * 10n ** 18n;
      const volumeUSD = 50n * 10n ** 18n;

      await expect(
        tracker
          .connect(emitter)
          .recordTrade(user.address, tokenIn, tokenOut, amountIn, amountOut, volumeUSD)
      )
        .to.emit(tracker, "TradeExecuted")
        .withArgs(user.address, tokenIn, tokenOut, amountIn, amountOut, volumeUSD, anyUint())
        .to.emit(tracker, "UserTradeMetrics")
        .withArgs(user.address, volumeUSD, 1n, anyUint());

      const daily = await tracker.getCurrentDayStats();
      expect(daily.totalVolume).to.equal(volumeUSD);
      expect(daily.totalTrades).to.equal(1n);
      expect(daily.uniqueTraders).to.equal(1n);

      const sIn = await tracker.getTokenStats(tokenIn);
      expect(sIn.totalVolume).to.equal(volumeUSD / 2n);
      expect(sIn.tradeCount).to.equal(1n);

      const sOut = await tracker.getTokenStats(tokenOut);
      expect(sOut.totalVolume).to.equal(volumeUSD / 2n);
      expect(sOut.tradeCount).to.equal(1n);

      const u = await tracker.getUserStats(user.address);
      expect(u.totalVolume).to.equal(volumeUSD);
      expect(u.tradeCount).to.equal(1n);
      expect(u.firstTradeTimestamp).to.be.gt(0n);
      expect(u.lastTradeTimestamp).to.equal(u.firstTradeTimestamp);
    });

    it("increments uniqueTraders only on first trade per day per user", async function () {
      // user trades twice; uniqueTraders should stay 1
      await tracker
        .connect(emitter)
        .recordTrade(user.address, other.address, user2.address, 1n, 1n, 10n ** 18n);
      await tracker
        .connect(emitter)
        .recordTrade(user.address, other.address, user2.address, 1n, 1n, 10n ** 18n);
      let daily = await tracker.getCurrentDayStats();
      expect(daily.uniqueTraders).to.equal(1n);
      expect(daily.totalTrades).to.equal(2n);

      // user2 trades -> uniqueTraders -> 2
      await tracker
        .connect(emitter)
        .recordTrade(user2.address, other.address, user.address, 1n, 1n, 10n ** 18n);
      daily = await tracker.getCurrentDayStats();
      expect(daily.uniqueTraders).to.equal(2n);
      expect(daily.totalTrades).to.equal(3n);
    });

    it("preserves firstTradeTimestamp across multiple trades", async function () {
      await tracker
        .connect(emitter)
        .recordTrade(user.address, other.address, user2.address, 1n, 1n, 10n ** 18n);
      const first = (await tracker.getUserStats(user.address)).firstTradeTimestamp;

      await network.provider.send("evm_increaseTime", [60]);
      await network.provider.send("evm_mine", []);

      await tracker
        .connect(emitter)
        .recordTrade(user.address, other.address, user2.address, 1n, 1n, 10n ** 18n);
      const u = await tracker.getUserStats(user.address);
      expect(u.firstTradeTimestamp).to.equal(first);
      expect(u.lastTradeTimestamp).to.be.gt(first);
      expect(u.tradeCount).to.equal(2n);
    });

    it("auto-rolls the day forward on first trade after 1 day (archives + resets)", async function () {
      await tracker
        .connect(emitter)
        .recordTrade(user.address, other.address, user2.address, 1n, 1n, 10n ** 18n);
      const priorDay = await tracker.currentDayTimestamp();

      await network.provider.send("evm_increaseTime", [ONE_DAY + 60]);
      await network.provider.send("evm_mine", []);

      await expect(
        tracker
          .connect(emitter)
          .recordTrade(user.address, other.address, user2.address, 1n, 1n, 2n * 10n ** 18n)
      )
        .to.emit(tracker, "DailyStatsSnapshot")
        .withArgs(priorDay, 10n ** 18n, 1n, 1n);

      const newDay = await tracker.currentDayTimestamp();
      expect(newDay).to.be.gt(priorDay);
      const today = await tracker.getCurrentDayStats();
      expect(today.totalVolume).to.equal(2n * 10n ** 18n);
      expect(today.totalTrades).to.equal(1n);
      expect(today.uniqueTraders).to.equal(1n);
    });
  });

  // ------------------------------------------------------------------- //
  // recordMarketTrade                                                    //
  // ------------------------------------------------------------------- //
  describe("recordMarketTrade", function () {
    beforeEach(async function () {
      await tracker.connect(admin).setAuthorizedEmitter(emitter.address, true);
    });

    it("reverts without EMITTER_ROLE", async function () {
      await expect(
        tracker
          .connect(other)
          .recordMarketTrade(
            user.address,
            other.address,
            user2.address,
            1n,
            1n,
            true,
            10n ** 18n
          )
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
    });

    it("emits MarketTrade with all args", async function () {
      await expect(
        tracker
          .connect(emitter)
          .recordMarketTrade(
            user.address,
            other.address,
            user2.address,
            1_000n * 10n ** 18n,
            500n * 10n ** 18n,
            true,
            2n * 10n ** 18n
          )
      )
        .to.emit(tracker, "MarketTrade")
        .withArgs(
          user.address,
          other.address,
          user2.address,
          1_000n * 10n ** 18n,
          500n * 10n ** 18n,
          true,
          2n * 10n ** 18n,
          anyUint()
        );
    });
  });

  // ------------------------------------------------------------------- //
  // recordLimitOrder* / recordStopLoss* / recordStopLimit*               //
  // ------------------------------------------------------------------- //
  describe("order-lifecycle event recorders", function () {
    beforeEach(async function () {
      await tracker.connect(admin).setAuthorizedEmitter(emitter.address, true);
    });

    it("recordLimitOrderPlaced: EMITTER gate + emits LimitOrderPlaced", async function () {
      await expect(
        tracker.connect(other).recordLimitOrderPlaced(1n, user.address, other.address, user2.address, 1n, 1n, true)
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);

      await expect(
        tracker
          .connect(emitter)
          .recordLimitOrderPlaced(42n, user.address, other.address, user2.address, 100n, 5n, true)
      )
        .to.emit(tracker, "LimitOrderPlaced")
        .withArgs(42n, user.address, other.address, user2.address, 100n, 5n, true, anyUint());
    });

    it("recordLimitOrderExecuted + Cancelled: EMITTER gate + events", async function () {
      await expect(
        tracker.connect(other).recordLimitOrderExecuted(1n, user.address, 1n)
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
      await expect(
        tracker.connect(emitter).recordLimitOrderExecuted(42n, user.address, 7n)
      )
        .to.emit(tracker, "LimitOrderExecuted")
        .withArgs(42n, user.address, 7n, anyUint());

      await expect(
        tracker.connect(other).recordLimitOrderCancelled(1n, user.address)
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
      await expect(tracker.connect(emitter).recordLimitOrderCancelled(42n, user.address))
        .to.emit(tracker, "LimitOrderCancelled")
        .withArgs(42n, user.address, anyUint());
    });

    it("recordStopLossPlaced + Triggered: EMITTER gate + events", async function () {
      await expect(
        tracker.connect(emitter).recordStopLossPlaced(1n, user.address, other.address, 10n, 100n)
      )
        .to.emit(tracker, "StopLossPlaced")
        .withArgs(1n, user.address, other.address, 10n, 100n, anyUint());

      await expect(
        tracker.connect(emitter).recordStopLossTriggered(1n, user.address, 100n, 99n, 9n)
      )
        .to.emit(tracker, "StopLossTriggered")
        .withArgs(1n, user.address, 100n, 99n, 9n, anyUint());

      await expect(
        tracker.connect(other).recordStopLossPlaced(1n, user.address, other.address, 10n, 100n)
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
    });

    it("recordStopLimit* (Placed / Activated / Executed): EMITTER gate + events", async function () {
      await expect(
        tracker
          .connect(emitter)
          .recordStopLimitPlaced(1n, user.address, other.address, 10n, 100n, 95n, false)
      )
        .to.emit(tracker, "StopLimitPlaced")
        .withArgs(1n, user.address, other.address, 10n, 100n, 95n, false, anyUint());

      await expect(tracker.connect(emitter).recordStopLimitActivated(1n, 100n))
        .to.emit(tracker, "StopLimitActivated")
        .withArgs(1n, 100n, anyUint());

      await expect(
        tracker.connect(emitter).recordStopLimitExecuted(1n, user.address, 95n)
      )
        .to.emit(tracker, "StopLimitExecuted")
        .withArgs(1n, user.address, 95n, anyUint());

      await expect(
        tracker.connect(other).recordStopLimitActivated(1n, 100n)
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
    });
  });

  // ------------------------------------------------------------------- //
  // recordLiquidityAdded / recordLiquidityRemoved                        //
  // ------------------------------------------------------------------- //
  describe("liquidity recorders", function () {
    beforeEach(async function () {
      await tracker.connect(admin).setAuthorizedEmitter(emitter.address, true);
    });

    it("recordLiquidityAdded: EMITTER gate + emits LiquidityAdded AND VaultLiquidityUpdate", async function () {
      await expect(
        tracker.connect(emitter).recordLiquidityAdded(other.address, user.address, 50n, 500n)
      )
        .to.emit(tracker, "LiquidityAdded")
        .withArgs(other.address, user.address, 50n, 500n, anyUint())
        .to.emit(tracker, "VaultLiquidityUpdate")
        .withArgs(other.address, 500n, anyUint());

      await expect(
        tracker.connect(other).recordLiquidityAdded(other.address, user.address, 50n, 500n)
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
    });

    it("recordLiquidityRemoved: EMITTER gate + emits LiquidityRemoved AND VaultLiquidityUpdate", async function () {
      await expect(
        tracker.connect(emitter).recordLiquidityRemoved(other.address, user.address, 20n, 480n)
      )
        .to.emit(tracker, "LiquidityRemoved")
        .withArgs(other.address, user.address, 20n, 480n, anyUint())
        .to.emit(tracker, "VaultLiquidityUpdate")
        .withArgs(other.address, 480n, anyUint());

      await expect(
        tracker.connect(other).recordLiquidityRemoved(other.address, user.address, 20n, 480n)
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
    });
  });

  // ------------------------------------------------------------------- //
  // view: getDailyStats / getCurrentDayStats                             //
  // ------------------------------------------------------------------- //
  describe("views", function () {
    beforeEach(async function () {
      await tracker.connect(admin).setAuthorizedEmitter(emitter.address, true);
    });

    it("getDailyStats returns currentDayStats when day == currentDayTimestamp", async function () {
      await tracker
        .connect(emitter)
        .recordTrade(user.address, other.address, user2.address, 1n, 1n, 10n ** 18n);
      const day = await tracker.currentDayTimestamp();
      const viaDaily = await tracker.getDailyStats(day);
      const viaCurrent = await tracker.getCurrentDayStats();
      expect(viaDaily.totalVolume).to.equal(viaCurrent.totalVolume);
      expect(viaDaily.totalTrades).to.equal(viaCurrent.totalTrades);
      expect(viaDaily.uniqueTraders).to.equal(viaCurrent.uniqueTraders);
    });

    it("getDailyStats returns zeros for an unknown day", async function () {
      const zero = await tracker.getDailyStats(1n);
      expect(zero.totalVolume).to.equal(0n);
      expect(zero.totalTrades).to.equal(0n);
      expect(zero.uniqueTraders).to.equal(0n);
    });
  });

  // ------------------------------------------------------------------- //
  // UUPS upgrade authorization (S1)                                      //
  // ------------------------------------------------------------------- //
  describe("upgrade authorization (S1)", function () {
    it("upgradeToAndCall reverts for non-admin", async function () {
      const Factory = await ethers.getContractFactory("TransactionTracker");
      const newImpl = await Factory.deploy();
      await newImpl.waitForDeployment();
      await expect(
        tracker.connect(other).upgradeToAndCall(await newImpl.getAddress(), "0x")
      ).to.be.revertedWithCustomError(tracker, ERRORS.common.Unauthorized);
    });

    it("upgradeToAndCall succeeds for DEFAULT_ADMIN_ROLE holder", async function () {
      const Factory = await ethers.getContractFactory("TransactionTracker");
      const newImpl = await Factory.deploy();
      await newImpl.waitForDeployment();
      await expect(
        tracker.connect(admin).upgradeToAndCall(await newImpl.getAddress(), "0x")
      ).not.to.be.reverted;
    });
  });

  // ------------------------------------------------------------------- //
  // storage layout (S12)                                                 //
  // ------------------------------------------------------------------- //
  describe("storage layout (S12)", function () {
    it("__gap[50] trails every declared storage variable (slot 11)", async function () {
      const art = await artifacts.getBuildInfo(
        "contracts/meta-ag/analytics/TransactionTracker.sol:TransactionTracker"
      );
      const layout =
        art.output.contracts[
          "contracts/meta-ag/analytics/TransactionTracker.sol"
        ].TransactionTracker.storageLayout;
      const gap = layout.storage.find((s) => s.label === "__gap");
      expect(gap).to.not.equal(undefined);
      expect(gap.slot).to.equal("11");
      // gap type must be a 50-length uint256 array
      expect(gap.type).to.match(/uint256\)50_storage/);
    });
  });
});

// ------------------------------------------------------------------- //
// helpers                                                              //
// ------------------------------------------------------------------- //

/**
 * Chai `withArgs` matcher for a non-deterministic uint (e.g., block.timestamp).
 * Returns a predicate that matches any BigInt greater than zero.
 */
function anyUint() {
  return (v) => {
    if (typeof v === "bigint") return v > 0n;
    if (typeof v === "number") return v > 0;
    return false;
  };
}
