/**
 * Sidiora Meta-AG — PECORVault unit tests (Phase 3 / Task 3.1)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.5 (FROZEN 2026-04-24)
 * Plan: docs/plans/pecor-sidiora-merge-plan.md — Phase 3, Task 3.1
 *
 * Regressions exercised:
 *   - S1  — _authorizeUpgrade gated by DEFAULT_ADMIN_ROLE (Timelock)
 *   - S5  — drainage only via OPERATOR_ROLE or Timelock emergencyWithdraw
 *   - S7  — USDL registerable with stablecoin=true (Q2)
 *   - S12 — append-only storage, original reserved range retained after consuming four slots
 */

const { expect } = require("chai");
const { ethers, artifacts } = require("hardhat");

const {
  PECOR_ROLES,
  ZERO_ADDRESS,
} = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const { deployPECORVault } = require("../helpers/fixtures");

const ONE = 10n ** 18n;

async function deployMockStandardERC20(name, symbol, decimals = 18) {
  const F = await ethers.getContractFactory("MockStandardERC20");
  const t = await F.deploy(name, symbol, decimals);
  await t.waitForDeployment();
  return t;
}

async function deployMockFeeOnTransferERC20(name, symbol, decimals, feeBps) {
  const F = await ethers.getContractFactory("MockFeeOnTransferERC20");
  const t = await F.deploy(name, symbol, decimals, feeBps);
  await t.waitForDeployment();
  return t;
}

async function deployMockReentrantERC20(name, symbol, decimals = 18) {
  const F = await ethers.getContractFactory("MockReentrantERC20");
  const t = await F.deploy(name, symbol, decimals);
  await t.waitForDeployment();
  return t;
}

async function deployMockWETH() {
  const F = await ethers.getContractFactory("MockWETH9");
  const w = await F.deploy();
  await w.waitForDeployment();
  return w;
}

async function deployMockTxTracker() {
  const F = await ethers.getContractFactory("MockTxTracker");
  const t = await F.deploy();
  await t.waitForDeployment();
  return t;
}

describe("meta-ag/vault/PECORVault", function () {
  let admin, operator, user, other, recipient;
  let vault, weth, tracker, tokenA, tokenB;

  beforeEach(async function () {
    [admin, operator, user, other, recipient] = await ethers.getSigners();
    weth = await deployMockWETH();
    tracker = await deployMockTxTracker();
    vault = await deployPECORVault({
      weth: weth.target,
      tracker: tracker.target,
      admin: admin.address,
    });
    tokenA = await deployMockStandardERC20("Token A", "TKA", 18);
    tokenB = await deployMockStandardERC20("Token B", "TKB", 6);
  });

  // ------------------------------------------------------------------- //
  // initialize                                                           //
  // ------------------------------------------------------------------- //
  describe("initialize", function () {
    it("grants DEFAULT_ADMIN_ROLE to the admin argument and stores weth + tracker", async function () {
      expect(await vault.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)).to.equal(true);
      expect(await vault.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, other.address)).to.equal(false);
      expect(await vault.weth()).to.equal(weth.target);
      expect(await vault.transactionTracker()).to.equal(tracker.target);
    });

    it("reverts on re-initialization", async function () {
      await expect(
        vault.initialize(weth.target, tracker.target, admin.address)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.AlreadyInitialized);
    });

    it("rejects zero WETH", async function () {
      const Factory = await ethers.getContractFactory("PECORVault");
      const impl = await Factory.deploy();
      await impl.waitForDeployment();
      const Proxy = await ethers.getContractFactory("ERC1967Proxy");
      const initData = impl.interface.encodeFunctionData("initialize", [
        ZERO_ADDRESS,
        tracker.target,
        admin.address,
      ]);
      await expect(Proxy.deploy(impl.target, initData)).to.be.revertedWithCustomError(
        impl,
        ERRORS.common.ZeroAddress
      );
    });

    it("rejects zero admin; accepts zero tracker (bootstrap path)", async function () {
      const Factory = await ethers.getContractFactory("PECORVault");
      const impl = await Factory.deploy();
      await impl.waitForDeployment();
      const Proxy = await ethers.getContractFactory("ERC1967Proxy");
      const zeroAdmin = impl.interface.encodeFunctionData("initialize", [
        weth.target,
        tracker.target,
        ZERO_ADDRESS,
      ]);
      await expect(Proxy.deploy(impl.target, zeroAdmin)).to.be.revertedWithCustomError(
        impl,
        ERRORS.common.ZeroAddress
      );
      // Zero tracker is allowed at bootstrap per IPECORVault NatSpec.
      const zeroTracker = impl.interface.encodeFunctionData("initialize", [
        weth.target,
        ZERO_ADDRESS,
        admin.address,
      ]);
      const proxy = await Proxy.deploy(impl.target, zeroTracker);
      await proxy.waitForDeployment();
      const v = Factory.attach(proxy.target);
      expect(await v.transactionTracker()).to.equal(ZERO_ADDRESS);
    });
  });

  // ------------------------------------------------------------------- //
  // registerToken                                                        //
  // ------------------------------------------------------------------- //
  describe("registerToken", function () {
    it("stores TokenInfo, pushes to arrays, and emits TokenRegistered", async function () {
      await expect(vault.connect(admin).registerToken(tokenA.target, false))
        .to.emit(vault, "TokenRegistered")
        .withArgs(tokenA.target, 18, false);
      // getTokenInfo returns a positional tuple — destructure in interface order
      // (IPECORVault: isRegistered, isStablecoin, decimals, reserves,
      //  totalDeposited, totalWithdrawn).
      const [isRegistered, isStablecoin, decimals, reserves, totalDeposited, totalWithdrawn] =
        await vault.getTokenInfo(tokenA.target);
      expect(isRegistered).to.equal(true);
      expect(isStablecoin).to.equal(false);
      expect(decimals).to.equal(18);
      expect(reserves).to.equal(0n);
      expect(totalDeposited).to.equal(0n);
      expect(totalWithdrawn).to.equal(0n);
      expect(await vault.getRegisteredTokens()).to.deep.equal([tokenA.target]);
      expect(await vault.getRegisteredStablecoins()).to.deep.equal([]);
      expect(await vault.getRegisteredTokenCount()).to.equal(1n);

      await vault.connect(admin).registerToken(tokenB.target, true);
      expect(await vault.getRegisteredTokens()).to.deep.equal([tokenA.target, tokenB.target]);
      expect(await vault.getRegisteredStablecoins()).to.deep.equal([tokenB.target]);
    });

    it("reverts on duplicate and zero address", async function () {
      await vault.connect(admin).registerToken(tokenA.target, false);
      await expect(
        vault.connect(admin).registerToken(tokenA.target, false)
      ).to.be.revertedWithCustomError(vault, ERRORS.vault.TokenAlreadyRegistered);
      await expect(
        vault.connect(admin).registerToken(ZERO_ADDRESS, false)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.ZeroAddress);
    });

    it("is admin-only", async function () {
      await expect(
        vault.connect(other).registerToken(tokenA.target, false)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);
    });
  });

  // ------------------------------------------------------------------- //
  // setStablecoinStatus                                                  //
  // ------------------------------------------------------------------- //
  describe("setStablecoinStatus", function () {
    it("toggles flag, updates registeredStablecoins, emits, and enforces admin-only + registered", async function () {
      await vault.connect(admin).registerToken(tokenA.target, false);
      // toggle on
      await expect(vault.connect(admin).setStablecoinStatus(tokenA.target, true))
        .to.emit(vault, "StablecoinStatusUpdated")
        .withArgs(tokenA.target, true);
      expect(await vault.isStablecoin(tokenA.target)).to.equal(true);
      expect(await vault.getRegisteredStablecoins()).to.deep.equal([tokenA.target]);
      // toggle off
      await expect(vault.connect(admin).setStablecoinStatus(tokenA.target, false))
        .to.emit(vault, "StablecoinStatusUpdated")
        .withArgs(tokenA.target, false);
      expect(await vault.isStablecoin(tokenA.target)).to.equal(false);
      expect(await vault.getRegisteredStablecoins()).to.deep.equal([]);
      // admin-only
      await expect(
        vault.connect(other).setStablecoinStatus(tokenA.target, true)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);
      // unregistered token revert
      await expect(
        vault.connect(admin).setStablecoinStatus(tokenB.target, true)
      ).to.be.revertedWithCustomError(vault, ERRORS.vault.TokenNotRegistered);
    });
  });

  // ------------------------------------------------------------------- //
  // setOperator                                                          //
  // ------------------------------------------------------------------- //
  describe("setOperator", function () {
    it("grants/revokes OPERATOR_ROLE, mirrors `authorizedOperators`, emits, admin-only, zero-address guarded", async function () {
      await expect(vault.connect(admin).setOperator(operator.address, true))
        .to.emit(vault, "OperatorUpdated")
        .withArgs(operator.address, true);
      expect(await vault.hasRole(PECOR_ROLES.OPERATOR_ROLE, operator.address)).to.equal(true);
      expect(await vault.authorizedOperators(operator.address)).to.equal(true);

      await expect(vault.connect(admin).setOperator(operator.address, false))
        .to.emit(vault, "OperatorUpdated")
        .withArgs(operator.address, false);
      expect(await vault.hasRole(PECOR_ROLES.OPERATOR_ROLE, operator.address)).to.equal(false);
      expect(await vault.authorizedOperators(operator.address)).to.equal(false);

      await expect(
        vault.connect(other).setOperator(operator.address, true)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);
      await expect(
        vault.connect(admin).setOperator(ZERO_ADDRESS, true)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.ZeroAddress);
    });
  });

  // ------------------------------------------------------------------- //
  // setTransactionTracker                                                //
  // ------------------------------------------------------------------- //
  describe("setTransactionTracker", function () {
    it("swaps tracker and emits; admin-only", async function () {
      const newTracker = await deployMockTxTracker();
      await expect(vault.connect(admin).setTransactionTracker(newTracker.target))
        .to.emit(vault, "TransactionTrackerUpdated")
        .withArgs(newTracker.target);
      expect(await vault.transactionTracker()).to.equal(newTracker.target);
      // setting to zero is allowed per spec (tracker may be unset after boot)
      await vault.connect(admin).setTransactionTracker(ZERO_ADDRESS);
      expect(await vault.transactionTracker()).to.equal(ZERO_ADDRESS);
      await expect(
        vault.connect(other).setTransactionTracker(newTracker.target)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);
    });
  });

  // ------------------------------------------------------------------- //
  // deposit / depositBatch / depositNative                               //
  // ------------------------------------------------------------------- //
  describe("deposit", function () {
    it("updates reserves + totalDeposited and emits Deposit", async function () {
      await vault.connect(admin).registerToken(tokenA.target, false);
      await tokenA.mint(user.address, ONE * 10n);
      await tokenA.connect(user).approve(vault.target, ONE * 10n);

      await expect(vault.connect(user).deposit(tokenA.target, ONE * 3n))
        .to.emit(vault, "Deposit")
        .withArgs(tokenA.target, user.address, ONE * 3n, ONE * 3n);
      expect(await vault.getReserves(tokenA.target)).to.equal(ONE * 3n);
      const [, , , reserves, totalDeposited] = await vault.getTokenInfo(tokenA.target);
      expect(totalDeposited).to.equal(ONE * 3n);
      expect(reserves).to.equal(ONE * 3n);

      await expect(
        vault.connect(user).deposit(tokenA.target, 0n)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.ZeroAmount);
      await expect(
        vault.connect(user).deposit(tokenB.target, ONE)
      ).to.be.revertedWithCustomError(vault, ERRORS.vault.TokenNotRegistered);
    });
  });

  describe("depositBatch", function () {
    it("updates all registered tokens atomically; reverts on length mismatch", async function () {
      await vault.connect(admin).registerToken(tokenA.target, false);
      await vault.connect(admin).registerToken(tokenB.target, true);
      await tokenA.mint(user.address, ONE * 10n);
      await tokenB.mint(user.address, 500_000n); // 6 decimals
      await tokenA.connect(user).approve(vault.target, ONE * 10n);
      await tokenB.connect(user).approve(vault.target, 500_000n);

      const tx = await vault
        .connect(user)
        .depositBatch([tokenA.target, tokenB.target], [ONE * 5n, 300_000n]);
      await expect(tx).to.emit(vault, "Deposit").withArgs(tokenA.target, user.address, ONE * 5n, ONE * 5n);
      await expect(tx).to.emit(vault, "Deposit").withArgs(tokenB.target, user.address, 300_000n, 300_000n);
      expect(await vault.getReserves(tokenA.target)).to.equal(ONE * 5n);
      expect(await vault.getReserves(tokenB.target)).to.equal(300_000n);
      const [tokens, reserves] = await vault.getAllReserves();
      expect(tokens).to.deep.equal([tokenA.target, tokenB.target]);
      expect(reserves).to.deep.equal([ONE * 5n, 300_000n]);

      await expect(
        vault.connect(user).depositBatch([tokenA.target], [ONE, ONE])
      ).to.be.revertedWithCustomError(vault, ERRORS.common.InvalidArrayLength);
      await expect(
        vault.connect(user).depositBatch([], [])
      ).to.be.revertedWithCustomError(vault, ERRORS.common.InvalidArrayLength);
    });
  });

  describe("depositNative", function () {
    it("wraps native into WETH, credits reserves, emits NativeDeposit", async function () {
      await vault.connect(admin).registerToken(weth.target, false);
      await expect(vault.connect(user).depositNative({ value: ONE * 2n }))
        .to.emit(vault, "NativeDeposit")
        .withArgs(user.address, ONE * 2n, ONE * 2n);
      expect(await vault.getReserves(weth.target)).to.equal(ONE * 2n);
      expect(await weth.balanceOf(vault.target)).to.equal(ONE * 2n);

      await expect(
        vault.connect(user).depositNative({ value: 0n })
      ).to.be.revertedWithCustomError(vault, ERRORS.common.ZeroAmount);
    });

    it("reverts when WETH is not registered in the vault", async function () {
      await expect(
        vault.connect(user).depositNative({ value: ONE })
      ).to.be.revertedWithCustomError(vault, ERRORS.vault.TokenNotRegistered);
    });
  });

  // ------------------------------------------------------------------- //
  // pullTokens (OPERATOR_ROLE-gated, fee-on-transfer safe)               //
  // ------------------------------------------------------------------- //
  describe("pullTokens", function () {
    it("OPERATOR_ROLE-only; returns actualAmount credited; fee-on-transfer safe", async function () {
      await vault.connect(admin).registerToken(tokenA.target, false);
      await vault.connect(admin).setOperator(operator.address, true);
      await tokenA.mint(user.address, ONE * 10n);
      await tokenA.connect(user).approve(vault.target, ONE * 10n);

      // happy path — standard ERC20: actualAmount == amount
      const preview = await vault
        .connect(operator)
        .pullTokens.staticCall(tokenA.target, user.address, ONE * 4n);
      expect(preview).to.equal(ONE * 4n);
      await vault.connect(operator).pullTokens(tokenA.target, user.address, ONE * 4n);
      expect(await vault.getReserves(tokenA.target)).to.equal(ONE * 4n);
      expect((await vault.getTokenInfo(tokenA.target))[4]).to.equal(ONE * 4n);

      // non-operator
      await expect(
        vault.connect(other).pullTokens(tokenA.target, user.address, ONE)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);

      // fee-on-transfer: 2% fee — pull 1000 net 980
      const fotToken = await deployMockFeeOnTransferERC20("FoT", "FOT", 18, 200);
      await vault.connect(admin).registerToken(fotToken.target, false);
      await fotToken.mint(user.address, ONE * 100n);
      await fotToken.connect(user).approve(vault.target, ONE * 100n);
      const previewFot = await vault
        .connect(operator)
        .pullTokens.staticCall(fotToken.target, user.address, ONE * 10n);
      const expectedNet = (ONE * 10n * 9800n) / 10_000n;
      expect(previewFot).to.equal(expectedNet);
      await vault.connect(operator).pullTokens(fotToken.target, user.address, ONE * 10n);
      expect(await vault.getReserves(fotToken.target)).to.equal(expectedNet);
      expect((await vault.getTokenInfo(fotToken.target))[4]).to.equal(expectedNet);

      // unregistered
      await expect(
        vault.connect(operator).pullTokens(tokenB.target, user.address, ONE)
      ).to.be.revertedWithCustomError(vault, ERRORS.vault.TokenNotRegistered);
    });
  });

  // ------------------------------------------------------------------- //
  // pushTokens (OPERATOR_ROLE-gated)                                     //
  // ------------------------------------------------------------------- //
  describe("pushTokens", function () {
    it("OPERATOR_ROLE-only; debits reserves + totalWithdrawn + emits Withdrawal", async function () {
      await vault.connect(admin).registerToken(tokenA.target, false);
      await vault.connect(admin).setOperator(operator.address, true);
      await tokenA.mint(user.address, ONE * 10n);
      await tokenA.connect(user).approve(vault.target, ONE * 10n);
      await vault.connect(user).deposit(tokenA.target, ONE * 7n);

      await expect(
        vault.connect(operator).pushTokens(tokenA.target, recipient.address, ONE * 3n)
      )
        .to.emit(vault, "Withdrawal")
        .withArgs(tokenA.target, recipient.address, ONE * 3n, ONE * 4n);
      expect(await tokenA.balanceOf(recipient.address)).to.equal(ONE * 3n);
      expect(await vault.getReserves(tokenA.target)).to.equal(ONE * 4n);
      expect((await vault.getTokenInfo(tokenA.target))[5]).to.equal(ONE * 3n);

      await expect(
        vault.connect(other).pushTokens(tokenA.target, recipient.address, ONE)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);

      await expect(
        vault.connect(operator).pushTokens(tokenA.target, recipient.address, ONE * 100n)
      ).to.be.revertedWithCustomError(vault, ERRORS.vault.ReservesMismatch);

      await expect(
        vault.connect(operator).pushTokens(tokenB.target, recipient.address, ONE)
      ).to.be.revertedWithCustomError(vault, ERRORS.vault.TokenNotRegistered);
    });
  });

  // ------------------------------------------------------------------- //
  // withdrawNative (OPERATOR_ROLE-gated)                                 //
  // ------------------------------------------------------------------- //
  describe("withdrawNative", function () {
    it("OPERATOR_ROLE-only; unwraps WETH and forwards native to recipient", async function () {
      await vault.connect(admin).registerToken(weth.target, false);
      await vault.connect(admin).setOperator(operator.address, true);
      await vault.connect(user).depositNative({ value: ONE * 4n });

      const before = await ethers.provider.getBalance(recipient.address);
      await expect(
        vault.connect(operator).withdrawNative(ONE * 2n, recipient.address)
      )
        .to.emit(vault, "NativeWithdrawal")
        .withArgs(recipient.address, ONE * 2n, ONE * 2n);
      const after = await ethers.provider.getBalance(recipient.address);
      expect(after - before).to.equal(ONE * 2n);
      expect(await vault.getReserves(weth.target)).to.equal(ONE * 2n);

      await expect(
        vault.connect(other).withdrawNative(ONE, recipient.address)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);
    });
  });

  // ------------------------------------------------------------------- //
  // updateReserves                                                       //
  // ------------------------------------------------------------------- //
  describe("updateReserves", function () {
    it("rejects deprecated book-only mutations without changing custody or counters", async function () {
      await vault.connect(admin).registerToken(tokenA.target, false);
      await vault.connect(admin).registerToken(tokenB.target, true);
      await vault.connect(admin).setOperator(operator.address, true);
      await tokenA.mint(user.address, ONE * 10n);
      await tokenA.connect(user).approve(vault.target, ONE * 10n);
      await vault.connect(user).deposit(tokenA.target, ONE * 6n);
      const before = await vault.getTokenInfo(tokenA.target);
      await expect(vault.connect(operator).updateReserves(tokenA.target, ONE * 2n, tokenB.target, 1n))
        .to.be.revertedWithCustomError(vault, "DeprecatedBookMutation");
      await expect(vault.connect(other).updateReserves(tokenA.target, ONE, tokenB.target, 1n))
        .to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);
      expect(await vault.getTokenInfo(tokenA.target)).to.deep.equal(before);
      expect(await tokenA.balanceOf(vault.target)).to.equal(ONE * 6n);
    });
  });

  describe("liability and custody regressions", function () {
    beforeEach(async function () {
      await vault.registerToken(tokenA.target, false);
      await vault.setOperator(operator.address, true);
      await vault.setOperator(other.address, true);
      await tokenA.mint(user.address, 10n * ONE);
      await tokenA.connect(user).approve(vault.target, 10n * ONE);
      await vault.connect(user).deposit(tokenA.target, 10n * ONE);
    });

    it("excludes every operator's reservations from trading and emergency withdrawals", async function () {
      await vault.connect(operator).reserveTokens(tokenA.target, 7n * ONE);
      expect(await vault.getAvailableReserves(tokenA.target)).to.equal(3n * ONE);
      expect(await vault.hasLiquidity(tokenA.target, 4n * ONE)).to.equal(false);
      await expect(vault.connect(other).pushTokens(tokenA.target, recipient.address, 4n * ONE))
        .to.be.revertedWithCustomError(vault, "ReservesMismatch");
      await expect(vault.emergencyWithdraw(tokenA.target, 4n * ONE, recipient.address))
        .to.be.revertedWithCustomError(vault, "ReservesMismatch");
      await expect(vault.connect(other).releaseTokens(tokenA.target, ONE))
        .to.be.revertedWithCustomError(vault, "ReservedLiabilities");
      await vault.connect(other).pushTokens(tokenA.target, recipient.address, 3n * ONE);
      expect(await vault.getReservedBalance(tokenA.target)).to.equal(7n * ONE);
      await vault.connect(operator).releaseTokens(tokenA.target, 7n * ONE);
      await vault.connect(operator).pushTokens(tokenA.target, user.address, 7n * ONE);
      expect(await vault.getReserves(tokenA.target)).to.equal(0n);
      expect(await tokenA.balanceOf(vault.target)).to.equal(0n);
    });

    it("quarantine prevents new funding and reserves while preserving exact refunds", async function () {
      await vault.setTokenQuarantine(tokenA.target, true);
      expect(await vault.getAvailableReserves(tokenA.target)).to.equal(0n);
      await expect(vault.connect(operator).reserveTokens(tokenA.target, ONE))
        .to.be.revertedWithCustomError(vault, "TokenQuarantined");
      await tokenA.mint(user.address, ONE);
      await tokenA.connect(user).approve(vault.target, ONE);
      await expect(vault.connect(user).deposit(tokenA.target, ONE))
        .to.be.revertedWithCustomError(vault, "TokenQuarantined");
      await vault.connect(operator).pushTokens(tokenA.target, user.address, ONE);
      expect(await tokenA.balanceOf(user.address)).to.equal(2n * ONE);
    });

    it("generic role grant, revoke and renounce keep the operator mirror consistent", async function () {
      await vault.revokeRole(PECOR_ROLES.OPERATOR_ROLE, operator.address);
      expect(await vault.authorizedOperators(operator.address)).to.equal(false);
      await vault.grantRole(PECOR_ROLES.OPERATOR_ROLE, operator.address);
      expect(await vault.authorizedOperators(operator.address)).to.equal(true);
      await vault.connect(operator).renounceRole(PECOR_ROLES.OPERATOR_ROLE, operator.address);
      expect(await vault.authorizedOperators(operator.address)).to.equal(false);
    });

    it("taxed outgoing delivery reverts atomically after measuring incoming custody", async function () {
      const tax = await deployMockFeeOnTransferERC20("Tax", "TAX", 18, 200);
      await vault.registerToken(tax.target, false);
      await tax.mint(user.address, 10n * ONE);
      await tax.connect(user).approve(vault.target, 10n * ONE);
      await vault.connect(user).deposit(tax.target, 10n * ONE);
      const before = await vault.getTokenInfo(tax.target);
      expect(before[3]).to.equal(98n * ONE / 10n);
      await expect(vault.connect(operator).pushTokens(tax.target, recipient.address, ONE))
        .to.be.revertedWithCustomError(vault, "UnsupportedTransfer");
      expect(await vault.getTokenInfo(tax.target)).to.deep.equal(before);
      expect(await tax.balanceOf(recipient.address)).to.equal(0n);
      expect(await tax.balanceOf(vault.target)).to.equal(before[3]);
    });

    it("reconciles an unreserved custody deficit and refuses to erase reserved liabilities", async function () {
      const F = await ethers.getContractFactory("MockERC20");
      const deficitToken = await F.deploy("Deficit", "DEF", 18);
      await vault.registerToken(deficitToken.target, false);
      await deficitToken.mint(user.address, 10n * ONE);
      await deficitToken.connect(user).approve(vault.target, 10n * ONE);
      await vault.connect(user).deposit(deficitToken.target, 10n * ONE);
      await vault.connect(operator).reserveTokens(deficitToken.target, 7n * ONE);
      await deficitToken.burn(vault.target, 2n * ONE);
      expect(await vault.getAvailableReserves(deficitToken.target)).to.equal(ONE);
      await expect(vault.syncReserves(deficitToken.target))
        .to.emit(vault, "ReserveDeficitReconciled")
        .withArgs(deficitToken.target, 10n * ONE, 8n * ONE);
      await deficitToken.burn(vault.target, 2n * ONE);
      expect(await vault.getAvailableReserves(deficitToken.target)).to.equal(0n);
      await expect(vault.syncReserves(deficitToken.target))
        .to.be.revertedWithCustomError(vault, "ReservedLiabilities");
      expect(await vault.getReserves(deficitToken.target)).to.equal(8n * ONE);
      expect(await vault.getReservedBalance(deficitToken.target)).to.equal(7n * ONE);
      await expect(vault.connect(operator).releaseTokens(deficitToken.target, ONE))
        .to.be.revertedWithCustomError(vault, "ReservedLiabilities");
    });

    it("rejects EOAs and unsupported decimal scales while allowing zero decimals", async function () {
      await expect(vault.registerToken(recipient.address, false))
        .to.be.revertedWithCustomError(vault, "UnsupportedToken");
      const oversized = await deployMockStandardERC20("Large", "L", 37);
      await expect(vault.registerToken(oversized.target, false))
        .to.be.revertedWithCustomError(vault, "UnsupportedToken");
      const integer = await deployMockStandardERC20("Integer", "I", 0);
      await vault.registerToken(integer.target, false);
      expect(await vault.getTokenDecimals(integer.target)).to.equal(0n);
    });
  });

  // ------------------------------------------------------------------- //
  // emergencyWithdraw — S5 regression                                    //
  // ------------------------------------------------------------------- //
  describe("emergencyWithdraw (S5)", function () {
    it("DEFAULT_ADMIN_ROLE only; operator cannot drain; event emitted", async function () {
      await vault.connect(admin).registerToken(tokenA.target, false);
      await vault.connect(admin).setOperator(operator.address, true);
      await tokenA.mint(user.address, ONE * 10n);
      await tokenA.connect(user).approve(vault.target, ONE * 10n);
      await vault.connect(user).deposit(tokenA.target, ONE * 8n);

      // Operator CANNOT drain via emergencyWithdraw — S5.
      await expect(
        vault.connect(operator).emergencyWithdraw(tokenA.target, ONE * 8n, recipient.address)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);

      // Admin (Timelock) can drain.
      await expect(
        vault.connect(admin).emergencyWithdraw(tokenA.target, ONE * 5n, recipient.address)
      )
        .to.emit(vault, "EmergencyWithdraw")
        .withArgs(tokenA.target, recipient.address, ONE * 5n, ONE * 3n);
      expect(await tokenA.balanceOf(recipient.address)).to.equal(ONE * 5n);
      expect(await vault.getReserves(tokenA.target)).to.equal(ONE * 3n);
    });
  });

  // ------------------------------------------------------------------- //
  // syncReserves / syncAllReserves                                       //
  // ------------------------------------------------------------------- //
  describe("syncReserves + syncAllReserves", function () {
    it("reconciles external transfers into reserves and emits ReservesSync; admin-only", async function () {
      await vault.connect(admin).registerToken(tokenA.target, false);
      await vault.connect(admin).registerToken(tokenB.target, true);
      await tokenA.mint(user.address, ONE * 10n);
      await tokenB.mint(user.address, 500_000n);
      // Bypass vault deposits — direct transfer creates untracked funds.
      await tokenA.connect(user).transfer(vault.target, ONE * 3n);
      await tokenB.connect(user).transfer(vault.target, 200_000n);
      expect(await vault.getUntrackedFunds(tokenA.target)).to.equal(ONE * 3n);
      expect(await vault.getUntrackedFunds(tokenB.target)).to.equal(200_000n);

      await expect(vault.connect(admin).syncReserves(tokenA.target))
        .to.emit(vault, "ReservesSync")
        .withArgs(tokenA.target, 0n, ONE * 3n, ONE * 3n);
      expect(await vault.getReserves(tokenA.target)).to.equal(ONE * 3n);
      expect(await vault.getUntrackedFunds(tokenA.target)).to.equal(0n);

      // syncAllReserves clears tokenB too.
      await vault.connect(admin).syncAllReserves();
      expect(await vault.getReserves(tokenB.target)).to.equal(200_000n);
      expect(await vault.getUntrackedFunds(tokenB.target)).to.equal(0n);

      // admin-only
      await expect(
        vault.connect(other).syncReserves(tokenA.target)
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);
      await expect(
        vault.connect(other).syncAllReserves()
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);
    });
  });

  // ------------------------------------------------------------------- //
  // receive()                                                            //
  // ------------------------------------------------------------------- //
  describe("receive()", function () {
    it("rejects direct native transfers from non-WETH callers", async function () {
      await expect(
        user.sendTransaction({ to: vault.target, value: ONE })
      ).to.be.revertedWithCustomError(vault, ERRORS.vault.WethOnly);
    });
  });

  // ------------------------------------------------------------------- //
  // ReentrancyGuard                                                      //
  // ------------------------------------------------------------------- //
  describe("ReentrancyGuard", function () {
    it("blocks cross-function reentry via a malicious ERC20 during emergencyWithdraw", async function () {
      const evil = await deployMockReentrantERC20("Evil", "EVL", 18);
      await vault.connect(admin).registerToken(evil.target, false);
      await evil.mint(user.address, ONE * 10n);
      await evil.connect(user).approve(vault.target, ONE * 10n);
      await vault.connect(admin).setOperator(operator.address, true);
      await vault.connect(user).deposit(evil.target, ONE * 5n);

      // Arm the token to reenter `deposit` on its next transfer. `deposit`
      // is chosen deliberately: it has no access-control modifier, so the
      // `nonReentrant` guard is the FIRST modifier to evaluate and therefore
      // the first to revert. If we reentered `emergencyWithdraw` instead,
      // the `onlyRole(DEFAULT_ADMIN_ROLE)` check would fire first (the evil
      // token contract does not hold the admin role) and we'd be asserting
      // on `MissingRole`, not the reentrancy selector.
      const reentryData = vault.interface.encodeFunctionData("deposit", [
        evil.target,
        ONE,
      ]);
      await evil.armReentry(vault.target, reentryData);

      // Outer call: admin.emergencyWithdraw(evil, ONE, recipient)
      //   -> vault.nonReentrant acquires the guard (status = ENTERED)
      //   -> TransferHelper.safeTransfer -> evil.transfer(recipient, ONE)
      //       -> evil._maybeReenter -> vault.deposit(evil, ONE) [REENTRY]
      //          -> vault.nonReentrant trips -> reverts ReentrancyGuardReentrantCall
      //       -> mock swallows the revert and records `lastReentryOk=false`
      //          + `lastReentryRet = <selector of ReentrancyGuardReentrantCall()>`
      //   -> transfer() returns true, TransferHelper completes, outer tx succeeds.
      // We assert the guard fired by inspecting the mock's captured error —
      // the outer tx cannot surface the inner selector because TransferHelper
      // would rewrap it as `TransferFailed()` if the mock bubbled it up.
      await vault
        .connect(admin)
        .emergencyWithdraw(evil.target, ONE, recipient.address);

      expect(await evil.lastReentryRecorded()).to.equal(true);
      expect(await evil.lastReentryOk()).to.equal(false);
      const reentrantSelector = ethers.id("ReentrancyGuardReentrantCall()").slice(0, 10);
      const capturedRet = await evil.lastReentryRet();
      expect(capturedRet.slice(0, 10)).to.equal(reentrantSelector);

      // Reserves consistent with the single successful outer withdrawal.
      expect(await vault.getReserves(evil.target)).to.equal(ONE * 4n);
      expect(await evil.balanceOf(recipient.address)).to.equal(ONE);
    });
  });

  // ------------------------------------------------------------------- //
  // Storage layout — S12                                                 //
  // ------------------------------------------------------------------- //
  describe("storage layout (S12)", function () {
    it("consumes only the original reserved slots and retains the original gap end", async function () {
      const artifact = await artifacts.readArtifact("PECORVault");
      const buildInfo = await artifacts.getBuildInfo(
        `${artifact.sourceName}:${artifact.contractName}`
      );
      const layout =
        buildInfo.output.contracts[artifact.sourceName][artifact.contractName].storageLayout;
      const last = layout.storage[layout.storage.length - 1];
      expect(last.label).to.equal("__gap");
      const gapType = layout.types[last.type];
      expect(gapType.encoding).to.equal("inplace");
      expect(gapType.numberOfBytes).to.equal("1472");
      expect(BigInt(last.slot) + BigInt(gapType.numberOfBytes) / 32n).to.equal(59n);
    });
  });

  // ------------------------------------------------------------------- //
  // Upgrade authorization — S1                                           //
  // ------------------------------------------------------------------- //
  describe("upgrade authorization (S1)", function () {
    it("upgradeToAndCall reverts for non-admin", async function () {
      const Factory = await ethers.getContractFactory("PECORVault");
      const newImpl = await Factory.deploy();
      await newImpl.waitForDeployment();
      await expect(
        vault.connect(other).upgradeToAndCall(newImpl.target, "0x")
      ).to.be.revertedWithCustomError(vault, ERRORS.common.Unauthorized);
    });

    it("upgradeToAndCall succeeds for admin (Timelock)", async function () {
      const Factory = await ethers.getContractFactory("PECORVault");
      const newImpl = await Factory.deploy();
      await newImpl.waitForDeployment();
      await vault.connect(admin).upgradeToAndCall(newImpl.target, "0x");
      // Post-upgrade the state + view surface remain intact.
      expect(await vault.weth()).to.equal(weth.target);
    });
  });

  // ------------------------------------------------------------------- //
  // USDL stablecoin flag — S7                                            //
  // ------------------------------------------------------------------- //
  describe("USDL stablecoin flag (S7)", function () {
    it("registerToken(USDL, true) stores USDL as a stablecoin (Q2)", async function () {
      // Local metadata-bearing USDL stand-in; live address code is absent on Hardhat.
      const localUSDL = await deployMockStandardERC20("USDL", "USDL", 18);
      const USDL = localUSDL.target;
      await vault.connect(admin).registerToken(USDL, true);
      expect(await vault.isStablecoin(USDL)).to.equal(true);
      const [isRegistered, isStablecoin] = await vault.getTokenInfo(USDL);
      expect(isRegistered).to.equal(true);
      expect(isStablecoin).to.equal(true);
      expect(await vault.getRegisteredStablecoins()).to.deep.equal([USDL]);
    });
  });
});
