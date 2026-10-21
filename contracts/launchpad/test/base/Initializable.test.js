const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("Initializable", function () {
  describe("initializer modifier", function () {
    it("should allow first initialization", async function () {
      const Mock = await ethers.getContractFactory("MockInitializable");
      const mock = await Mock.deploy();
      await mock.waitForDeployment();

      await mock.initialize(42);
      expect(await mock.value()).to.equal(42);
      expect(await mock.initialized()).to.be.true;
    });

    it("should revert on second initialization", async function () {
      const Mock = await ethers.getContractFactory("MockInitializable");
      const mock = await Mock.deploy();
      await mock.initialize(42);

      await expect(mock.initialize(99)).to.be.revertedWithCustomError(
        mock, "AlreadyInitialized"
      );
    });

    it("should emit Initialized event", async function () {
      const Mock = await ethers.getContractFactory("MockInitializable");
      const mock = await Mock.deploy();

      await expect(mock.initialize(42))
        .to.emit(mock, "Initialized")
        .withArgs(1);
    });

    it("should set initialized version to 1", async function () {
      const Mock = await ethers.getContractFactory("MockInitializable");
      const mock = await Mock.deploy();
      await mock.initialize(42);

      expect(await mock.getInitializedVersion()).to.equal(1);
    });
  });

  describe("reinitializer modifier", function () {
    it("should allow reinitialize with higher version", async function () {
      const Mock = await ethers.getContractFactory("MockInitializable");
      const mock = await Mock.deploy();
      await mock.initialize(42);
      await mock.reinitialize2(100);
      expect(await mock.value()).to.equal(100);
      expect(await mock.getInitializedVersion()).to.equal(2);
    });

    it("should revert reinitialize with same version", async function () {
      const Mock = await ethers.getContractFactory("MockInitializable");
      const mock = await Mock.deploy();
      await mock.initialize(42);
      await mock.reinitialize2(100);

      await expect(mock.reinitialize2(200)).to.be.revertedWithCustomError(
        mock, "AlreadyInitialized"
      );
    });

    it("should allow skipping versions", async function () {
      const Mock = await ethers.getContractFactory("MockInitializable");
      const mock = await Mock.deploy();
      await mock.initialize(1);
      await mock.reinitialize3(3);
      expect(await mock.getInitializedVersion()).to.equal(3);
    });
  });

  describe("disableInitializers", function () {
    it("should prevent initialization after disable", async function () {
      const Mock = await ethers.getContractFactory("MockInitializableDisabled");
      const mock = await Mock.deploy();

      await expect(mock.initialize(42)).to.be.revertedWithCustomError(
        mock, "AlreadyInitialized"
      );
    });
  });

  describe("onlyInitializing modifier", function () {
    it("should allow calls during initialization", async function () {
      const Mock = await ethers.getContractFactory("MockInitializableChild");
      const mock = await Mock.deploy();
      await mock.initialize(10, 20);
      expect(await mock.parentValue()).to.equal(10);
      expect(await mock.childValue()).to.equal(20);
    });
  });
});
