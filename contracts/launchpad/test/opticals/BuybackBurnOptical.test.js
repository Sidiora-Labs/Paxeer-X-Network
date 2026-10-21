const { registryFixture, attachOptical } = require("./fixture");
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { ZERO_ADDRESS, HookFlags } = require("../helpers/constants");

describe("BuybackBurnOptical", function () {
  let buybackBurn, registry, mockPool;
  let deployer, alice;
  let poolAddr;

  const BUYBACK_BPS = 2000n; // 20% of fees go to buyback

  beforeEach(async function () {
    [deployer, alice] = await ethers.getSigners();

    const MockPool = await ethers.getContractFactory("MockPoolForOptical");
    mockPool = await MockPool.deploy();
    await mockPool.waitForDeployment();
    poolAddr = await mockPool.getAddress();

    registry = await registryFixture(deployer);
    await mockPool.setFeeAccumulator(poolAddr);
    const BuybackBurn = await ethers.getContractFactory("BuybackBurnOptical");
    buybackBurn = await BuybackBurn.deploy(await registry.getAddress(), deployer.address, BUYBACK_BPS);
    await buybackBurn.waitForDeployment();
    buybackBurn = await attachOptical(registry, mockPool, buybackBurn, deployer);
  });

  describe("Configuration", function () {
    it("should store immutable config correctly", async function () {
      expect(await buybackBurn.buybackBps()).to.equal(BUYBACK_BPS);
      expect(await buybackBurn.DEAD_ADDRESS()).to.equal("0x000000000000000000000000000000000000dEaD");
    });

    it("should return BEFORE_FEE_DISTRIBUTION flag only", async function () {
      expect(await buybackBurn.getFlags()).to.equal(HookFlags.BEFORE_FEE_DISTRIBUTION);
    });

    it("rejects an unavailable zero executor", async function () {
      const Factory = await ethers.getContractFactory("BuybackBurnOptical");
      await expect(Factory.deploy(await registry.getAddress(), ethers.ZeroAddress, BUYBACK_BPS)).to.be.revertedWith("Zero owner");
    });
    it("should revert if buyback exceeds 50%", async function () {
      const BuybackBurn = await ethers.getContractFactory("BuybackBurnOptical");
      await expect(
        BuybackBurn.deploy(ZERO_ADDRESS, deployer.address, 5001n)
      ).to.be.reverted;
    });
  });

  describe("beforeFeeDistribution", function () {
    it("should redirect buybackBps% of fee and accumulate", async function () {
      const feeAmount = ethers.parseUnits("100", 6);
      const adjustedFee = await buybackBurn.beforeFeeDistribution.staticCall(poolAddr, feeAmount);

      // 20% redirected → 80 USDL adjusted fee
      const expectedBuyback = (feeAmount * BUYBACK_BPS) / 10000n;
      expect(adjustedFee).to.equal(feeAmount - expectedBuyback);
    });

    it("should accumulate USDL across multiple fee distributions", async function () {
      const feeAmount = ethers.parseUnits("100", 6);

      // Two fee distributions
      await buybackBurn.beforeFeeDistribution(poolAddr, feeAmount);
      await buybackBurn.beforeFeeDistribution(poolAddr, feeAmount);

      // 20% of 100 * 2 = 40
      const expectedAccumulated = ((feeAmount * BUYBACK_BPS) / 10000n) * 2n;
      expect(await buybackBurn.getAccumulatedUsdl(poolAddr)).to.equal(expectedAccumulated);
    });

    it("should emit BuybackAccumulated event", async function () {
      const feeAmount = ethers.parseUnits("100", 6);
      const expectedBuyback = (feeAmount * BUYBACK_BPS) / 10000n;

      await expect(buybackBurn.beforeFeeDistribution(poolAddr, feeAmount))
        .to.emit(buybackBurn, "BuybackAccumulated")
        .withArgs(poolAddr, expectedBuyback);
    });

    it("should return full fee when buybackBps is 0", async function () {
      const BuybackBurn = await ethers.getContractFactory("BuybackBurnOptical");
      const r = await registryFixture(deployer);
      let zeroBuyback = await BuybackBurn.deploy(await r.getAddress(), deployer.address, 0n);
      await zeroBuyback.waitForDeployment();
      zeroBuyback = await attachOptical(r, mockPool, zeroBuyback, deployer);

      const feeAmount = ethers.parseUnits("100", 6);
      const adjusted = await zeroBuyback.beforeFeeDistribution.staticCall(poolAddr, feeAmount);
      expect(adjusted).to.equal(feeAmount);
    });
  });

  describe("executeBuybackBurn safety", function () {
    it("rejects zero slippage protection", async function () {
      await expect(buybackBurn.connect(deployer).executeBuybackBurn(poolAddr, 0, 9999999999)).to.be.revertedWith("Slippage required");
    });
    it("rejects execution without accumulated capital", async function () {
      await expect(buybackBurn.connect(deployer).executeBuybackBurn(poolAddr, 1, 9999999999)).to.be.revertedWithCustomError(buybackBurn, "NothingToExecute");
    });
    it("rejects untrusted execution without consuming accumulated capital", async function () {
      await buybackBurn.beforeFeeDistribution(poolAddr, 1000);
      const before = await buybackBurn.accumulatedUsdl(poolAddr);
      await expect(buybackBurn.connect(alice).executeBuybackBurn(poolAddr, 1, 9999999999))
        .to.be.revertedWithCustomError(buybackBurn, "NotOwner");
      expect(await buybackBurn.accumulatedUsdl(poolAddr)).to.equal(before);
    });
    it("does not expose a bookkeeping-only reset", async function () {
      expect(buybackBurn.interface.hasFunction("markBuybackExecuted")).to.equal(false);
    });
  });
});
