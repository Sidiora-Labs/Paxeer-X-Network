/**
 * Sidiora Meta-AG — PECOROrders engine unit tests (Phase 4 / Task 4.2 — deferred to Session 8)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.7 (FROZEN 2026-04-24)
 * Interface: contracts/meta-ag/interfaces/IPECOROrders.sol
 * Contract:  contracts/meta-ag/engine/PECOROrders.sol
 *
 * Regressions exercised:
 *   - S1   UUPS _authorizeUpgrade gated by DEFAULT_ADMIN_ROLE
 *   - S12  Append-only storage — new fields consume the original gap
 *   - Funds-custody: place pulls from user → cancel pushes back same amount
 *   - KEEPER_ROLE gate on all execution + activation paths
 *   - O(1) active-list removal (removeFromActiveLimitOrders)
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
  deployPECORVault,
  deployPECOROrders,
} = require("../helpers/fixtures");
const { pushPrice, increaseTime } = require("../helpers/oracle");

const ONE = 10n ** 18n;
const ONE_HOUR = 3600;

// Order type / status mirrors the contract enum
const OrderType = {
  LIMIT_BUY: 0,
  LIMIT_SELL: 1,
  STOP_LOSS: 2,
  STOP_LIMIT_BUY: 3,
  STOP_LIMIT_SELL: 4,
};
const OrderStatus = {
  PENDING: 0,
  ACTIVATED: 1,
  EXECUTED: 2,
  CANCELLED: 3,
  EXPIRED: 4,
};

async function makeToken(name, symbol, decimals = 18) {
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

async function setup() {
  const [admin, user, keeper, other] = await ethers.getSigners();

  const weth = await makeWETH();
  const oracle = await deployPriceOracle({ admin: admin.address });
  await oracle.connect(admin).setRelayer(admin.address, true);

  const StubT = await ethers.getContractFactory("MockTxTracker");
  const vaultStub = await StubT.deploy();
  await vaultStub.waitForDeployment();

  const vault = await deployPECORVault({
    weth: weth.target,
    tracker: vaultStub.target,
    admin: admin.address,
  });

  const tokenA = await makeToken("Alpha", "A", 18);
  const usdl = await makeToken("USDL", "USDL", 18);

  await vault.connect(admin).registerToken(tokenA.target, false);
  await vault.connect(admin).registerToken(usdl.target, true);

  // Oracle registration + prices
  for (const t of [tokenA.target, usdl.target]) {
    await oracle
      .connect(admin)
      .registerToken(t, 60, 100n, ONE / 100n, ONE * 1_000_000n, ONE_HOUR);
  }
  // tokenA = $1 baseline, usdl = $1
  await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: ONE });
  await pushPrice({ priceOracle: oracle, relayer: admin, token: usdl.target, price: ONE });

  const orders = await deployPECOROrders({
    priceOracle: oracle.target,
    vault: vault.target,
    tracker: ZERO_ADDRESS, // tracker zero — skip typed-call integration in unit scope
    admin: admin.address,
  });

  await vault.connect(admin).setOperator(orders.target, true);
  await orders.connect(admin).setKeeper(keeper.address, true);

  // Seed vault reserves (so executeLimit* can push tokens out)
  await usdl.mint(admin.address, 1_000_000n * ONE);
  await usdl.connect(admin).approve(vault.target, 1_000_000n * ONE);
  await vault.connect(admin).deposit(usdl.target, 1_000_000n * ONE);

  await tokenA.mint(admin.address, 1_000_000n * ONE);
  await tokenA.connect(admin).approve(vault.target, 1_000_000n * ONE);
  await vault.connect(admin).deposit(tokenA.target, 100_000n * ONE);

  // Fund user
  await usdl.mint(user.address, 10_000n * ONE);
  await usdl.connect(user).approve(vault.target, 10_000n * ONE);
  await tokenA.mint(user.address, 10_000n * ONE);
  await tokenA.connect(user).approve(vault.target, 10_000n * ONE);

  return { admin, user, keeper, other, weth, oracle, vault, orders, tokenA, usdl };
}

async function nowPlus(seconds) {
  const b = await ethers.provider.getBlock("latest");
  return b.timestamp + seconds;
}
async function refreshPrices(oracle, admin, tokens) {
  // push the latest set again to keep oracle fresh across time warps
  for (const { addr, price } of tokens) {
    await pushPrice({ priceOracle: oracle, relayer: admin, token: addr, price });
  }
}

describe("meta-ag/engine/PECOROrders", function () {
  let env;

  beforeEach(async function () {
    env = await setup();
  });

  // ------------------------------------------------------------------- //
  // initialize                                                           //
  // ------------------------------------------------------------------- //
  describe("initialize", function () {
    it("stores refs + grants admin role; nextOrderId starts at 1", async function () {
      const { orders, oracle, vault, admin } = env;
      expect(await orders.priceOracle()).to.equal(oracle.target);
      expect(await orders.vault()).to.equal(vault.target);
      expect(await orders.nextOrderId()).to.equal(1n);
      expect(await orders.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)).to.equal(true);
    });

    it("rejects zero oracle / vault / admin", async function () {
      const { vault } = env;
      const Factory = await ethers.getContractFactory("PECOROrders");
      const impl = await Factory.deploy();
      await impl.waitForDeployment();
      const Proxy = await ethers.getContractFactory("ERC1967Proxy");
      const make = (args) => impl.interface.encodeFunctionData("initialize", args);
      await expect(
        Proxy.deploy(impl.target, make([ethers.ZeroAddress, vault.target, ethers.ZeroAddress, ethers.Wallet.createRandom().address]))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");
      await expect(
        Proxy.deploy(impl.target, make([ethers.Wallet.createRandom().address, ethers.ZeroAddress, ethers.ZeroAddress, ethers.Wallet.createRandom().address]))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");
      await expect(
        Proxy.deploy(impl.target, make([ethers.Wallet.createRandom().address, vault.target, ethers.ZeroAddress, ethers.ZeroAddress]))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");
    });
  });

  // ------------------------------------------------------------------- //
  // setKeeper / pause                                                    //
  // ------------------------------------------------------------------- //
  describe("admin setters", function () {
    it("setKeeper: admin-only; grants/revokes KEEPER_ROLE + mirrors `keepers` flag; emits", async function () {
      const { orders, admin, keeper, other } = env;
      expect(await orders.hasRole(PECOR_ROLES.KEEPER_ROLE, keeper.address)).to.equal(true);
      expect(await orders.keepers(keeper.address)).to.equal(true);

      await expect(orders.connect(admin).setKeeper(keeper.address, false))
        .to.emit(orders, "KeeperUpdated")
        .withArgs(keeper.address, false);
      expect(await orders.hasRole(PECOR_ROLES.KEEPER_ROLE, keeper.address)).to.equal(false);
      expect(await orders.keepers(keeper.address)).to.equal(false);

      await expect(orders.connect(other).setKeeper(keeper.address, true)).to.be.revertedWithCustomError(
        orders,
        ERRORS.common.Unauthorized
      );
      await expect(orders.connect(admin).setKeeper(ethers.ZeroAddress, true)).to.be.revertedWithCustomError(
        orders,
        "ZeroAddress"
      );
    });

    it("pause/unpause: admin-only; blocks placeLimit*", async function () {
      const { orders, admin, user, other, tokenA, usdl } = env;
      await orders.connect(admin).pause();
      const expires = await nowPlus(3600);
      await expect(
        orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, expires)
      ).to.be.revertedWithCustomError(orders, "Paused");
      await expect(orders.connect(other).unpause()).to.be.revertedWithCustomError(
        orders,
        ERRORS.common.Unauthorized
      );
      await orders.connect(admin).unpause();
      await expect(
        orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, expires)
      ).not.to.be.reverted;
    });
  });

  // ------------------------------------------------------------------- //
  // placeLimitBuy — validations + happy path                             //
  // ------------------------------------------------------------------- //
  describe("placeLimitBuy", function () {
    it("validates: NotAStablecoin / TokenIsStablecoin / ZeroAmount / InvalidPrice / InvalidExpiry", async function () {
      const { orders, user, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      // stablecoin is tokenA (not a stablecoin)
      await expect(
        orders.connect(user).placeLimitBuy(tokenA.target, usdl.target, 100n * ONE, ONE, expires)
      ).to.be.revertedWithCustomError(orders, "NotAStablecoin");
      // token is also a stablecoin
      await expect(
        orders.connect(user).placeLimitBuy(usdl.target, usdl.target, 100n * ONE, ONE, expires)
      ).to.be.revertedWithCustomError(orders, "TokenIsStablecoin");
      // Zero amount
      await expect(
        orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 0n, ONE, expires)
      ).to.be.revertedWithCustomError(orders, "ZeroAmount");
      // Zero target price
      await expect(
        orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, 0n, expires)
      ).to.be.revertedWithCustomError(orders, "InvalidPrice");
      // Expiry in the past
      const past = (await ethers.provider.getBlock("latest")).timestamp - 10;
      await expect(
        orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, past)
      ).to.be.revertedWithCustomError(orders, "InvalidExpiry");
    });

    it("happy path: pulls stablecoin from user, records order, emits LimitOrderCreated", async function () {
      const { orders, user, tokenA, usdl, vault } = env;
      const expires = await nowPlus(3600);
      const balBefore = await usdl.balanceOf(user.address);
      const vaultBefore = await usdl.balanceOf(vault.target);

      const tx = await orders
        .connect(user)
        .placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE / 2n, expires);
      await expect(tx)
        .to.emit(orders, "LimitOrderCreated")
        .withArgs(1n, user.address, OrderType.LIMIT_BUY, 100n * ONE, ONE / 2n);

      expect(await usdl.balanceOf(user.address)).to.equal(balBefore - 100n * ONE);
      expect(await usdl.balanceOf(vault.target)).to.equal(vaultBefore + 100n * ONE);

      const order = await orders.getLimitOrder(1n);
      expect(order.user).to.equal(user.address);
      expect(order.amount).to.equal(100n * ONE);
      expect(order.status).to.equal(OrderStatus.PENDING);
      expect(order.orderType).to.equal(OrderType.LIMIT_BUY);

      const userOrders = await orders.getUserLimitOrders(user.address);
      expect(userOrders.length).to.equal(1);
      expect(userOrders[0]).to.equal(1n);
    });
  });

  // ------------------------------------------------------------------- //
  // placeLimitSell / placeStopLoss                                       //
  // ------------------------------------------------------------------- //
  describe("placeLimitSell + placeStopLoss", function () {
    it("placeLimitSell: pulls token from user, emits event with LIMIT_SELL type", async function () {
      const { orders, user, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await expect(
        orders.connect(user).placeLimitSell(tokenA.target, usdl.target, 50n * ONE, 2n * ONE, expires)
      )
        .to.emit(orders, "LimitOrderCreated")
        .withArgs(1n, user.address, OrderType.LIMIT_SELL, 50n * ONE, 2n * ONE);
      const o = await orders.getLimitOrder(1n);
      expect(o.orderType).to.equal(OrderType.LIMIT_SELL);
    });

    it("placeStopLoss: pulls token, stores STOP_LOSS type", async function () {
      const { orders, user, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeStopLoss(tokenA.target, usdl.target, 50n * ONE, ONE / 2n, expires);
      const o = await orders.getLimitOrder(1n);
      expect(o.orderType).to.equal(OrderType.STOP_LOSS);
      expect(o.targetPrice).to.equal(ONE / 2n);
    });
  });

  // ------------------------------------------------------------------- //
  // placeStopLimit* price-range validation                               //
  // ------------------------------------------------------------------- //
  describe("placeStopLimit* price-range validation", function () {
    it("placeStopLimitBuy: InvalidPriceRange when limitPrice < stopPrice", async function () {
      const { orders, user, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await expect(
        orders
          .connect(user)
          .placeStopLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, ONE, expires)
      ).to.be.revertedWithCustomError(orders, "InvalidPriceRange");
    });

    it("placeStopLimitSell: InvalidPriceRange when limitPrice > stopPrice", async function () {
      const { orders, user, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await expect(
        orders
          .connect(user)
          .placeStopLimitSell(tokenA.target, usdl.target, 100n * ONE, ONE, 2n * ONE, expires)
      ).to.be.revertedWithCustomError(orders, "InvalidPriceRange");
    });

    it("happy stop-limit buy: pulls stablecoin, emits StopLimitOrderCreated", async function () {
      const { orders, user, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await expect(
        orders
          .connect(user)
          .placeStopLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, 2n * ONE, expires)
      )
        .to.emit(orders, "StopLimitOrderCreated")
        .withArgs(1n, user.address, OrderType.STOP_LIMIT_BUY, 100n * ONE, ONE, 2n * ONE);
      const o = await orders.getStopLimitOrder(1n);
      expect(o.status).to.equal(OrderStatus.PENDING);
    });
  });

  // ------------------------------------------------------------------- //
  // cancelLimitOrder                                                     //
  // ------------------------------------------------------------------- //
  describe("cancelLimitOrder", function () {
    it("NotOrderOwner from non-owner; refunds exact amount on owner cancel", async function () {
      const { orders, user, other, tokenA, usdl, vault } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE / 2n, expires);

      await expect(orders.connect(other).cancelLimitOrder(1n)).to.be.revertedWithCustomError(
        orders,
        "NotOrderOwner"
      );

      const balBefore = await usdl.balanceOf(user.address);
      await expect(orders.connect(user).cancelLimitOrder(1n))
        .to.emit(orders, "LimitOrderCancelled")
        .withArgs(1n);
      expect(await usdl.balanceOf(user.address)).to.equal(balBefore + 100n * ONE);

      const o = await orders.getLimitOrder(1n);
      expect(o.status).to.equal(OrderStatus.CANCELLED);
    });

    it("OrderNotPending when attempting to cancel a cancelled order", async function () {
      const { orders, user, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE / 2n, expires);
      await orders.connect(user).cancelLimitOrder(1n);
      await expect(orders.connect(user).cancelLimitOrder(1n)).to.be.revertedWithCustomError(
        orders,
        "OrderNotPending"
      );
    });
  });

  // ------------------------------------------------------------------- //
  // cancelStopLimitOrder                                                 //
  // ------------------------------------------------------------------- //
  describe("cancelStopLimitOrder", function () {
    it("refunds stablecoin to buyer on STOP_LIMIT_BUY cancel", async function () {
      const { orders, user, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders
        .connect(user)
        .placeStopLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, 2n * ONE, expires);
      const balBefore = await usdl.balanceOf(user.address);
      await expect(orders.connect(user).cancelStopLimitOrder(1n))
        .to.emit(orders, "StopLimitCancelled")
        .withArgs(1n);
      expect(await usdl.balanceOf(user.address)).to.equal(balBefore + 100n * ONE);
    });

    it("OrderCannotCancel when status is not PENDING or ACTIVATED (e.g., EXECUTED)", async function () {
      const { orders, user, keeper, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      // Place stop-limit-sell with trigger=0.5, limit=0.4
      await orders
        .connect(user)
        .placeStopLimitSell(tokenA.target, usdl.target, 100n * ONE, ONE / 2n, ONE / 4n, expires);
      // Drop price below stop → activate then execute
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: ONE / 3n });
      await orders.connect(keeper).checkStopLimitActivation(1n);
      // current price ONE/3 which is > limitPrice ONE/4 → shouldn't execute
      // Bring price back up so limit passes (for STOP_LIMIT_SELL: cp >= limitPrice)
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: ONE / 3n });
      await orders.connect(keeper).executeStopLimitOrder(1n);
      await expect(orders.connect(user).cancelStopLimitOrder(1n)).to.be.revertedWithCustomError(
        orders,
        "OrderCannotCancel"
      );
    });
  });

  // ------------------------------------------------------------------- //
  // executeLimitOrder                                                    //
  // ------------------------------------------------------------------- //
  describe("executeLimitOrder", function () {
    it("KEEPER_ROLE-gated", async function () {
      const { orders, user, other, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, expires);
      await expect(orders.connect(other).executeLimitOrder(1n)).to.be.revertedWithCustomError(
        orders,
        ERRORS.common.Unauthorized
      );
    });

    it("PriceNotMet when LIMIT_BUY tries to fill above target", async function () {
      const { orders, user, keeper, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE / 2n, expires); // target $0.50
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: ONE }); // market $1.00
      await expect(orders.connect(keeper).executeLimitOrder(1n)).to.be.revertedWithCustomError(
        orders,
        "PriceNotMet"
      );
    });

    it("happy LIMIT_BUY: pushes tokenA to user and emits LimitOrderExecuted", async function () {
      const { orders, user, keeper, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, expires); // target $2.00
      // Keep price $1 (below target) → executable
      const balBefore = await tokenA.balanceOf(user.address);
      await expect(orders.connect(keeper).executeLimitOrder(1n)).to.emit(orders, "LimitOrderExecuted");
      // 100 USDL * $1 / $1 = 100 tokenA
      expect((await tokenA.balanceOf(user.address)) - balBefore).to.equal(100n * ONE);
      expect((await orders.getLimitOrder(1n)).status).to.equal(OrderStatus.EXECUTED);
    });

    it("OrderExpired when deadline passed", async function () {
      const { orders, user, keeper, tokenA, usdl, oracle, admin } = env;
      const expires = await nowPlus(60);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, expires);
      await increaseTime(120);
      // Refresh price so the oracle call inside execute doesn't revert on staleness first
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: ONE });
      await pushPrice({ priceOracle: oracle, relayer: admin, token: usdl.target, price: ONE });
      await expect(orders.connect(keeper).executeLimitOrder(1n)).to.be.revertedWithCustomError(
        orders,
        "OrderExpired"
      );
    });

    it("happy LIMIT_SELL: pushes USDL to user", async function () {
      const { orders, user, keeper, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeLimitSell(tokenA.target, usdl.target, 50n * ONE, ONE / 2n, expires); // target $0.50, market=$1 → fill
      const balBefore = await usdl.balanceOf(user.address);
      await orders.connect(keeper).executeLimitOrder(1n);
      expect((await usdl.balanceOf(user.address)) - balBefore).to.equal(50n * ONE);
    });

    it("happy STOP_LOSS: triggers when price ≤ target, pushes USDL to user", async function () {
      const { orders, user, keeper, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeStopLoss(tokenA.target, usdl.target, 50n * ONE, ONE, expires); // trigger at $1
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: ONE / 2n }); // drop to $0.50
      await pushPrice({ priceOracle: oracle, relayer: admin, token: usdl.target, price: ONE });
      const balBefore = await usdl.balanceOf(user.address);
      await orders.connect(keeper).executeLimitOrder(1n);
      // 50 tokenA * $0.50 / $1 = 25 USDL
      expect((await usdl.balanceOf(user.address)) - balBefore).to.equal(25n * ONE);
    });
  });

  // ------------------------------------------------------------------- //
  // checkStopLimitActivation + executeStopLimitOrder                     //
  // ------------------------------------------------------------------- //
  describe("stop-limit activation + execution", function () {
    it("checkStopLimitActivation: PENDING → ACTIVATED when stopPrice breached; emits StopLimitActivated", async function () {
      const { orders, user, keeper, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders
        .connect(user)
        .placeStopLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, 3n * ONE, expires);
      // price stays $1 → no activation
      await orders.connect(keeper).checkStopLimitActivation(1n);
      expect((await orders.getStopLimitOrder(1n)).status).to.equal(OrderStatus.PENDING);
      // price climbs to $2.5 → activate (≥ stopPrice $2)
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: 2n * ONE + ONE / 2n });
      await expect(orders.connect(keeper).checkStopLimitActivation(1n))
        .to.emit(orders, "StopLimitActivated");
      expect((await orders.getStopLimitOrder(1n)).status).to.equal(OrderStatus.ACTIVATED);
    });

    it("executeStopLimitOrder: reverts OrderNotActivated on PENDING", async function () {
      const { orders, user, keeper, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders
        .connect(user)
        .placeStopLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, 3n * ONE, expires);
      await expect(orders.connect(keeper).executeStopLimitOrder(1n)).to.be.revertedWithCustomError(
        orders,
        "OrderNotActivated"
      );
    });

    it("executeStopLimitOrder: PriceNotMet when STOP_LIMIT_BUY current > limit", async function () {
      const { orders, user, keeper, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders
        .connect(user)
        .placeStopLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, 3n * ONE, expires);
      // Activate at $2.5
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: 2n * ONE + ONE / 2n });
      await orders.connect(keeper).checkStopLimitActivation(1n);
      // Price spikes to $4 (> limit $3) → PriceNotMet
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: 4n * ONE });
      await expect(orders.connect(keeper).executeStopLimitOrder(1n)).to.be.revertedWithCustomError(
        orders,
        "PriceNotMet"
      );
    });

    it("executeStopLimitOrder happy path: pushes token to user + emits StopLimitExecuted", async function () {
      const { orders, user, keeper, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders
        .connect(user)
        .placeStopLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, 3n * ONE, expires);
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: 2n * ONE + ONE / 2n }); // $2.5
      await orders.connect(keeper).checkStopLimitActivation(1n);
      const balBefore = await tokenA.balanceOf(user.address);
      await expect(orders.connect(keeper).executeStopLimitOrder(1n)).to.emit(orders, "StopLimitExecuted");
      expect(await tokenA.balanceOf(user.address)).to.be.gt(balBefore);
      expect((await orders.getStopLimitOrder(1n)).status).to.equal(OrderStatus.EXECUTED);
    });
  });

  // ------------------------------------------------------------------- //
  // batch execution                                                      //
  // ------------------------------------------------------------------- //
  describe("batch execution", function () {
    it("batchExecuteLimitOrders: executes eligible orders, skips others, returns count", async function () {
      const { orders, user, keeper, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      // Order 1: LIMIT_BUY at $2.00 → fillable at $1 market → will fill
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, expires);
      // Order 2: LIMIT_BUY at $0.50 (market still $1) → should SKIP
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE / 2n, expires);

      const tx = await orders.connect(keeper).batchExecuteLimitOrders([1n, 2n]);
      const r = await tx.wait();
      // Can't read return value from event; check status instead
      expect((await orders.getLimitOrder(1n)).status).to.equal(OrderStatus.EXECUTED);
      expect((await orders.getLimitOrder(2n)).status).to.equal(OrderStatus.PENDING);
    });

    it("batchCheckAndExecuteStopLimits: activates + executes in one call", async function () {
      const { orders, user, keeper, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders
        .connect(user)
        .placeStopLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, 3n * ONE, expires);
      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: 2n * ONE + ONE / 2n }); // activate & execute
      await orders.connect(keeper).batchCheckAndExecuteStopLimits([1n]);
      expect((await orders.getStopLimitOrder(1n)).status).to.equal(OrderStatus.EXECUTED);
    });
  });

  // ------------------------------------------------------------------- //
  // view helpers                                                         //
  // ------------------------------------------------------------------- //
  describe("views", function () {
    it("canExecuteLimitOrder returns status reason strings", async function () {
      const { orders, user, oracle, admin, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE / 2n, expires);
      let [ok, reason] = await orders.canExecuteLimitOrder(1n);
      expect(ok).to.equal(false);
      expect(reason).to.equal("Price above target");

      await pushPrice({ priceOracle: oracle, relayer: admin, token: tokenA.target, price: ONE / 4n });
      [ok, reason] = await orders.canExecuteLimitOrder(1n);
      expect(ok).to.equal(true);
      expect(reason).to.equal("Executable");
    });

    it("getExecutableLimitOrders filters by status/price/expiry", async function () {
      const { orders, user, tokenA, usdl } = env;
      const expires = await nowPlus(3600);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, 2n * ONE, expires); // fillable
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE / 2n, expires); // skip
      const ids = await orders.getExecutableLimitOrders(5);
      expect(ids.length).to.equal(1);
      expect(ids[0]).to.equal(1n);
    });
  });

  describe("escrow and settlement regressions", function () {
    it("reserves placement escrow and releases it on cancellation", async function () {
      const { orders, vault, user, tokenA, usdl } = env;
      const available = await vault.getAvailableReserves(usdl.target);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, await nowPlus(3600));
      expect(await vault.getReservedBalance(usdl.target)).to.equal(100n * ONE);
      expect(await vault.getAvailableReserves(usdl.target)).to.equal(available);
      await orders.connect(user).cancelLimitOrder(1n);
      expect(await vault.getReservedBalance(usdl.target)).to.equal(0n);
      expect(await vault.getAvailableReserves(usdl.target)).to.equal(available);
    });

    it("paused settlement preserves escrow while owner cancellation remains available", async function () {
      const { orders, vault, admin, keeper, user, tokenA, usdl } = env;
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, await nowPlus(3600));
      await orders.connect(admin).pause();
      await expect(orders.connect(keeper).executeLimitOrder(1n)).to.be.revertedWithCustomError(orders, "Paused");
      await expect(orders.connect(keeper).batchExecuteLimitOrders([1n])).to.be.revertedWithCustomError(orders, "Paused");
      expect(await vault.getReservedBalance(usdl.target)).to.equal(100n * ONE);
      await orders.connect(user).cancelLimitOrder(1n);
      expect(await vault.getReservedBalance(usdl.target)).to.equal(0n);
    });

    it("atomic placement binds its output floor before any keeper can settle", async function () {
      const { orders, keeper, user, tokenA, usdl } = env;
      await orders.connect(user).placeOrder({
        orderType: OrderType.LIMIT_BUY, stablecoin: usdl.target, token: tokenA.target,
        amount: 100n * ONE, triggerPrice: ONE, limitPrice: 0n,
        expiresAt: await nowPlus(3600), minimumOutput: 101n * ONE,
      });
      expect(await orders.orderMinOutput(1n)).to.equal(101n * ONE);
      await expect(orders.connect(keeper).executeLimitOrder(1n)).to.be.revertedWithCustomError(orders, "InsufficientOutput");
      await orders.connect(user).setOrderMinOutput(1n, 100n * ONE);
      await orders.connect(keeper).executeLimitOrder(1n);
      expect((await orders.getLimitOrder(1n)).status).to.equal(OrderStatus.EXECUTED);
    });

    it("output-floor failure rolls back status, balances and reservations", async function () {
      const { orders, vault, keeper, user, tokenA, usdl } = env;
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, await nowPlus(3600));
      await orders.connect(user).setOrderMinOutput(1n, 101n * ONE);
      const before = await tokenA.balanceOf(user.address);
      await expect(orders.connect(keeper).executeLimitOrder(1n)).to.be.revertedWithCustomError(orders, "InsufficientOutput");
      expect((await orders.getLimitOrder(1n)).status).to.equal(OrderStatus.PENDING);
      expect(await tokenA.balanceOf(user.address)).to.equal(before);
      expect(await vault.getReservedBalance(usdl.target)).to.equal(100n * ONE);
    });

    it("batch isolates an invalid order and a failing output floor from a valid fill", async function () {
      const { orders, vault, keeper, user, tokenA, usdl } = env;
      const expiry = await nowPlus(3600);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, expiry);
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, expiry);
      await orders.connect(user).setOrderMinOutput(1n, 101n * ONE);
      await orders.connect(keeper).batchExecuteLimitOrders([999n, 1n, 2n]);
      expect((await orders.getLimitOrder(1n)).status).to.equal(OrderStatus.PENDING);
      expect((await orders.getLimitOrder(2n)).status).to.equal(OrderStatus.EXECUTED);
      expect(await vault.getReservedBalance(usdl.target)).to.equal(100n * ONE);
    });

    it("retains tier fee and price impact as reserved output, then releases collected fees", async function () {
      const { orders, vault, admin, keeper, user, tokenA, usdl } = env;
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 10_000n * ONE, ONE, await nowPlus(3600));
      const balanceBefore = await tokenA.balanceOf(user.address);
      // $10k tier charges 20bps, then 10bps impact against $100k available output.
      const gross = 10_000n * ONE;
      const afterFee = gross - gross * 20n / 10_000n;
      const expected = afterFee - afterFee * 10n / 10_000n;
      await orders.connect(keeper).executeLimitOrder(1n);
      expect(await tokenA.balanceOf(user.address) - balanceBefore).to.equal(expected);
      expect(await orders.accruedFees(tokenA.target)).to.equal(gross - expected);
      expect(await vault.getReservedBalance(tokenA.target)).to.equal(gross - expected);
      const collectorBefore = await tokenA.balanceOf(admin.address);
      await orders.connect(admin).collectFees(tokenA.target);
      expect(await tokenA.balanceOf(admin.address) - collectorBefore).to.equal(gross - expected);
      expect(await vault.getReservedBalance(tokenA.target)).to.equal(0n);
      expect(await orders.accruedFees(tokenA.target)).to.equal(0n);
    });

    it("any caller can expire an order but refund goes only to its owner", async function () {
      const { orders, vault, user, other, tokenA, usdl } = env;
      await orders.connect(user).placeLimitBuy(usdl.target, tokenA.target, 100n * ONE, ONE, await nowPlus(60));
      const ownerBefore = await usdl.balanceOf(user.address);
      const callerBefore = await usdl.balanceOf(other.address);
      await increaseTime(61);
      await orders.connect(other).expireLimitOrder(1n);
      expect((await orders.getLimitOrder(1n)).status).to.equal(OrderStatus.EXPIRED);
      expect(await usdl.balanceOf(user.address)).to.equal(ownerBefore + 100n * ONE);
      expect(await usdl.balanceOf(other.address)).to.equal(callerBefore);
      expect(await vault.getReservedBalance(usdl.target)).to.equal(0n);
    });
  });

  // ------------------------------------------------------------------- //
  // upgrade authorization (S1)                                           //
  // ------------------------------------------------------------------- //
  describe("upgrade authorization (S1)", function () {
    it("admin-only upgradeToAndCall", async function () {
      const { orders, admin, other } = env;
      const Factory = await ethers.getContractFactory("PECOROrders");
      const newImpl = await Factory.deploy();
      await newImpl.waitForDeployment();
      await expect(
        orders.connect(other).upgradeToAndCall(newImpl.target, "0x")
      ).to.be.revertedWithCustomError(orders, ERRORS.common.Unauthorized);
      await expect(orders.connect(admin).upgradeToAndCall(newImpl.target, "0x")).not.to.be.reverted;
    });
  });

  // ------------------------------------------------------------------- //
  // storage layout (S12)                                                 //
  // ------------------------------------------------------------------- //
  describe("storage layout (S12)", function () {
    it("preserves legacy slots and consumes ten reserved slots", async function () {
      const art = await artifacts.getBuildInfo(
        "contracts/meta-ag/engine/PECOROrders.sol:PECOROrders"
      );
      const layout =
        art.output.contracts["contracts/meta-ag/engine/PECOROrders.sol"].PECOROrders.storageLayout;
      const gap = layout.storage.find((s) => s.label === "__gap");
      expect(gap).to.not.equal(undefined);
      expect(layout.storage.find((s) => s.label === "keepers").slot).to.equal("13");
      expect(layout.storage.find((s) => s.label === "orderMinOutput").slot).to.equal("14");
      expect(gap.slot).to.equal("24");
      expect(gap.type).to.match(/uint256\)40_storage/);
    });
  });
});
