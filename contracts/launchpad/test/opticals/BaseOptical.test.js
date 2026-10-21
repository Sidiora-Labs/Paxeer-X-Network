const { registryFixture, attachOptical } = require("./fixture");
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { ZERO_ADDRESS } = require("../helpers/constants");

describe("IOptical + BaseOptical", function () {
  let mockOptical, mockOpticalWithFlags, registry, pool, poolAddr;
  let deployer, alice;

  beforeEach(async function () {
    [deployer, alice] = await ethers.getSigners();

    registry = await registryFixture(deployer);
    pool = await (await ethers.getContractFactory("MockPoolForOptical")).deploy();
    poolAddr = await pool.getAddress();
    await pool.setFeeAccumulator(poolAddr);
    // Deploy base mock (no flags)
    const MockOptical = await ethers.getContractFactory("MockOptical");
    mockOptical = await MockOptical.deploy(await registry.getAddress(), deployer.address);
    await mockOptical.waitForDeployment();
    mockOptical = await attachOptical(registry, pool, mockOptical, deployer);

    // Deploy mock with flags
    const MockOpticalWithFlags = await ethers.getContractFactory("MockOpticalWithFlags");
    mockOpticalWithFlags = await MockOpticalWithFlags.deploy(ZERO_ADDRESS, deployer.address, 0);
    await mockOpticalWithFlags.waitForDeployment();
  });

  describe("Default no-op hooks", function () {
    it("should return getFlags = 0 by default", async function () {
      expect(await mockOptical.getFlags()).to.equal(0);
    });

    it("should return proceed=true, amountDelta=0 from beforeSwap", async function () {
      const result = await mockOptical.beforeSwap.staticCall(
        poolAddr, alice.address, true, ethers.parseUnits("100", 6)
      );
      expect(result.proceed).to.be.true;
      expect(result.amountDelta).to.equal(0);
    });

    it("should return correct selector from afterSwap", async function () {
      const selector = await mockOptical.afterSwap.staticCall(
        poolAddr, alice.address, true, ethers.parseUnits("100", 6), ethers.parseUnits("50", 6)
      );
      const expectedSelector = mockOptical.interface.getFunction("afterSwap").selector;
      expect(selector).to.equal(expectedSelector);
    });

    it("should return unchanged feeAmount from beforeFeeDistribution", async function () {
      const feeAmount = ethers.parseUnits("10", 6);
      const adjusted = await mockOptical.beforeFeeDistribution.staticCall(poolAddr, feeAmount);
      expect(adjusted).to.equal(feeAmount);
    });

    it("should return correct selector from afterFeeDistribution", async function () {
      const selector = await mockOptical.afterFeeDistribution.staticCall(
        poolAddr, ethers.parseUnits("10", 6)
      );
      const expectedSelector = mockOptical.interface.getFunction("afterFeeDistribution").selector;
      expect(selector).to.equal(expectedSelector);
    });
  });

  describe("Immutable state", function () {
    it("should store poolRegistry and owner correctly", async function () {
      expect(await mockOptical.poolRegistry()).to.equal(await registry.getAddress());
      expect(await mockOptical.owner()).to.equal(deployer.address);
    });
  });
});
