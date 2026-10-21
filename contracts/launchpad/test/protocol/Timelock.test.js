const { expect } = require("chai");
const { ethers } = require("hardhat");
const { time } = require("@nomicfoundation/hardhat-toolbox/network-helpers");

describe("Timelock", function () {
  let timelock, mockTarget;
  let deployer, proposer, guardian, alice;
  const TWO_DAYS = 2 * 24 * 60 * 60; // 172800 seconds

  before(async function () {
    [deployer, proposer, guardian, alice] = await ethers.getSigners();
  });

  beforeEach(async function () {
    const Timelock = await ethers.getContractFactory("Timelock");
    timelock = await Timelock.deploy(TWO_DAYS, proposer.address, guardian.address);
    await timelock.waitForDeployment();

    // Deploy a simple target contract for testing execution
    const MockImpl = await ethers.getContractFactory("MockImplementation");
    mockTarget = await MockImpl.deploy();
    await mockTarget.waitForDeployment();
  });

  describe("constructor", function () {
    it("should set minDelay, proposer, guardian", async function () {
      expect(await timelock.minDelay()).to.equal(TWO_DAYS);
      expect(await timelock.proposer()).to.equal(proposer.address);
      expect(await timelock.guardian()).to.equal(guardian.address);
    });

    it("should revert with zero delay", async function () {
      const Timelock = await ethers.getContractFactory("Timelock");
      await expect(
        Timelock.deploy(0, proposer.address, guardian.address)
      ).to.be.revertedWithCustomError(timelock, "InvalidDelay");
    });

    it("should revert with zero proposer", async function () {
      const Timelock = await ethers.getContractFactory("Timelock");
      await expect(
        Timelock.deploy(TWO_DAYS, ethers.ZeroAddress, guardian.address)
      ).to.be.revertedWithCustomError(timelock, "ZeroAddress");
    });

    it("should revert with zero guardian", async function () {
      const Timelock = await ethers.getContractFactory("Timelock");
      await expect(
        Timelock.deploy(TWO_DAYS, proposer.address, ethers.ZeroAddress)
      ).to.be.revertedWithCustomError(timelock, "ZeroAddress");
    });
  });

  describe("queueTransaction", function () {
    it("should queue a transaction by proposer", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await expect(
        timelock.connect(proposer).queueTransaction(target, 0, data, eta)
      ).to.emit(timelock, "TransactionQueued");
    });

    it("should revert queue by non-proposer", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await expect(
        timelock.connect(alice).queueTransaction(target, 0, data, eta)
      ).to.be.revertedWithCustomError(timelock, "Unauthorized");
    });

    it("should revert if eta is too soon", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + 100; // less than minDelay

      await expect(
        timelock.connect(proposer).queueTransaction(target, 0, data, eta)
      ).to.be.revertedWithCustomError(timelock, "DelayNotMet");
    });

    it("should revert if already queued", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await timelock.connect(proposer).queueTransaction(target, 0, data, eta);
      await expect(
        timelock.connect(proposer).queueTransaction(target, 0, data, eta)
      ).to.be.revertedWithCustomError(timelock, "TransactionAlreadyQueued");
    });
  });

  describe("executeTransaction", function () {
    it("should execute after delay", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await timelock.connect(proposer).queueTransaction(target, 0, data, eta);
      await time.increaseTo(eta);

      await expect(
        timelock.executeTransaction(target, 0, data, eta)
      ).to.emit(timelock, "TransactionExecuted");

      expect(await mockTarget.getValue()).to.equal(42);
    });

    it("should revert before delay", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await timelock.connect(proposer).queueTransaction(target, 0, data, eta);

      await expect(
        timelock.executeTransaction(target, 0, data, eta)
      ).to.be.revertedWithCustomError(timelock, "DelayNotMet");
    });

    it("should revert if not queued", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await expect(
        timelock.executeTransaction(target, 0, data, eta)
      ).to.be.revertedWithCustomError(timelock, "TransactionNotQueued");
    });

    it("should remove from queue after execution", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await timelock.connect(proposer).queueTransaction(target, 0, data, eta);
      await time.increaseTo(eta);
      await timelock.executeTransaction(target, 0, data, eta);

      // Second execution should fail
      await expect(
        timelock.executeTransaction(target, 0, data, eta)
      ).to.be.revertedWithCustomError(timelock, "TransactionNotQueued");
    });

    it("should allow anyone to execute after delay", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [99]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await timelock.connect(proposer).queueTransaction(target, 0, data, eta);
      await time.increaseTo(eta);

      // Alice (random user) executes
      await timelock.connect(alice).executeTransaction(target, 0, data, eta);
      expect(await mockTarget.getValue()).to.equal(99);
    });
  });

  describe("cancelTransaction", function () {
    it("should cancel by guardian", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await timelock.connect(proposer).queueTransaction(target, 0, data, eta);
      await expect(
        timelock.connect(guardian).cancelTransaction(target, 0, data, eta)
      ).to.emit(timelock, "TransactionCancelled");

      // Should no longer be executable
      await time.increaseTo(eta);
      await expect(
        timelock.executeTransaction(target, 0, data, eta)
      ).to.be.revertedWithCustomError(timelock, "TransactionNotQueued");
    });

    it("should revert cancel by non-guardian", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await timelock.connect(proposer).queueTransaction(target, 0, data, eta);
      await expect(
        timelock.connect(alice).cancelTransaction(target, 0, data, eta)
      ).to.be.revertedWithCustomError(timelock, "Unauthorized");
    });

    it("should revert cancel of non-queued transaction", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;

      await expect(
        timelock.connect(guardian).cancelTransaction(target, 0, data, eta)
      ).to.be.revertedWithCustomError(timelock, "TransactionNotQueued");
    });
  });

  describe("audit lifecycle regressions", function () {
    it("preserves guardian until nominated address accepts", async function () {
      await timelock.connect(guardian).setGuardian(alice.address);
      expect(await timelock.guardian()).to.equal(guardian.address);
      await expect(timelock.connect(proposer).acceptGuardian()).to.be.revertedWithCustomError(timelock, "Unauthorized");
      await timelock.connect(alice).acceptGuardian();
      expect(await timelock.guardian()).to.equal(alice.address);
    });
    it("expires stale single operations", async function () {
      const target = await mockTarget.getAddress();
      const data = mockTarget.interface.encodeFunctionData("setValue", [42]);
      const eta = (await time.latest()) + TWO_DAYS + 100;
      await timelock.connect(proposer).queueTransaction(target, 0, data, eta);
      await time.increaseTo(eta + Number(await timelock.GRACE_PERIOD()) + 1);
      await expect(timelock.executeTransaction(target, 0, data, eta)).to.be.revertedWithCustomError(timelock, "OperationExpired");
    });
  });

  describe("getMinDelay", function () {
    it("should return correct value", async function () {
      expect(await timelock.getMinDelay()).to.equal(TWO_DAYS);
    });
  });
});
