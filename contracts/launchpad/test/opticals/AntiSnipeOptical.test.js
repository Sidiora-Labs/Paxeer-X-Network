const { registryFixture, poolSigner, attachOptical } = require("./fixture");
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { ZERO_ADDRESS, HookFlags } = require("../helpers/constants");

describe("AntiSnipeOptical", function () {
  let antiSnipe, antiSnipeAddress;
  let mockPool;
  let deployer, alice;

  // Config: max 1% buy, 10 blocks protection
  const MAX_BUY_BPS = 100n; // 1%
  const PROTECTION_BLOCKS = 10n;

  let registry;
  beforeEach(async function () {
    [deployer, alice] = await ethers.getSigners();
    registry = await registryFixture(deployer);

    // Deploy a minimal mock pool that returns reserves
    const MockPool = await ethers.getContractFactory("MockPoolForOptical");
    mockPool = await MockPool.deploy();
    await mockPool.waitForDeployment();

    // Deploy AntiSnipeOptical
    const AntiSnipe = await ethers.getContractFactory("AntiSnipeOptical");
    antiSnipe = await AntiSnipe.deploy(
      await registry.getAddress(), // authoritative registry
      deployer.address,
      MAX_BUY_BPS,
      PROTECTION_BLOCKS
    );
    await antiSnipe.waitForDeployment();
    antiSnipe = await attachOptical(registry, mockPool, antiSnipe, deployer);
    antiSnipeAddress = await antiSnipe.getAddress();
  });

  describe("Configuration", function () {
    it("should store immutable config correctly", async function () {
      expect(await antiSnipe.maxBuyBps()).to.equal(MAX_BUY_BPS);
      expect(await antiSnipe.protectionBlocks()).to.equal(PROTECTION_BLOCKS);
      expect(await antiSnipe.owner()).to.equal(deployer.address);
    });

    it("should return BEFORE_SWAP flag only", async function () {
      const flags = await antiSnipe.getFlags();
      expect(flags).to.equal(HookFlags.BEFORE_SWAP);
    });
  });

  describe("Pool registration", function () {
    it("should register pool creation block", async function () {
      const poolAddr = await mockPool.getAddress();
      await antiSnipe.registerPool(poolAddr);
      const creationBlock = await antiSnipe.poolCreationBlock(poolAddr);
      expect(creationBlock).to.be.gt(0);
    });

    it("should not overwrite existing registration", async function () {
      const poolAddr = await mockPool.getAddress();
      await antiSnipe.registerPool(poolAddr);
      const firstBlock = await antiSnipe.poolCreationBlock(poolAddr);

      // Mine a few blocks
      await ethers.provider.send("evm_mine", []);
      await ethers.provider.send("evm_mine", []);

      await antiSnipe.registerPool(poolAddr);
      const secondBlock = await antiSnipe.poolCreationBlock(poolAddr);
      expect(secondBlock).to.equal(firstBlock);
    });
  });

  describe("beforeSwap protection", function () {
    it("should allow sells during protection period", async function () {
      const poolAddr = await mockPool.getAddress();
      await antiSnipe.registerPool(poolAddr);

      // Set pool reserves: virtual 10000, real 0, token 1B
      await mockPool.setReserves(
        ethers.parseUnits("10000", 6), 0, ethers.parseUnits("1000000000", 6)
      );

      // Sell should always be allowed
      const result = await antiSnipe.beforeSwap.staticCall(
        poolAddr, alice.address, false, ethers.parseUnits("1000000", 6)
      );
      expect(result.proceed).to.be.true;
    });

    it("should block buys exceeding maxBuyBps during protection", async function () {
      const poolAddr = await mockPool.getAddress();
      await antiSnipe.registerPool(poolAddr);

      // Set pool reserves: virtual 10000, real 0, token 1B
      // Max buy = 1% of 10000 = 100 USDL
      await mockPool.setReserves(
        ethers.parseUnits("10000", 6), 0, ethers.parseUnits("1000000000", 6)
      );

      // Buy 200 USDL (exceeds 1% of 10000 = 100)
      const result = await antiSnipe.beforeSwap.staticCall(
        poolAddr, alice.address, true, ethers.parseUnits("200", 6)
      );
      expect(result.proceed).to.be.false;
    });

    it("should allow buys within maxBuyBps during protection", async function () {
      const poolAddr = await mockPool.getAddress();
      await antiSnipe.registerPool(poolAddr);

      await mockPool.setReserves(
        ethers.parseUnits("10000", 6), 0, ethers.parseUnits("1000000000", 6)
      );

      // Buy 50 USDL (within 1% of 10000 = 100)
      const result = await antiSnipe.beforeSwap.staticCall(
        poolAddr, alice.address, true, ethers.parseUnits("50", 6)
      );
      expect(result.proceed).to.be.true;
    });

    it("should allow any buy after protection period expires", async function () {
      const poolAddr = await mockPool.getAddress();
      await antiSnipe.registerPool(poolAddr);

      await mockPool.setReserves(
        ethers.parseUnits("10000", 6), 0, ethers.parseUnits("1000000000", 6)
      );

      // Mine blocks past protection period
      for (let i = 0; i < 11; i++) {
        await ethers.provider.send("evm_mine", []);
      }

      // Large buy should now be allowed
      const result = await antiSnipe.beforeSwap.staticCall(
        poolAddr, alice.address, true, ethers.parseUnits("5000", 6)
      );
      expect(result.proceed).to.be.true;
    });

    it("rejects an unattached pool", async function () {
      await expect(antiSnipe.beforeSwap.staticCall(alice.address, alice.address, true, 1))
        .to.be.revertedWithCustomError(antiSnipe, "NotPool");
    });
  });
});
