const { registryFixture, attachOptical } = require("./fixture");
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { ZERO_ADDRESS, HookFlags } = require("../helpers/constants");

describe("TaxOptical", function () {
  let taxOptical, registry, pool, poolAddr;
  let deployer, alice;

  const BUY_TAX_BPS = 200n; // 2%
  const SELL_TAX_BPS = 300n; // 3%

  beforeEach(async function () {
    [deployer, alice] = await ethers.getSigners();

    registry = await registryFixture(deployer);
    pool = await (await ethers.getContractFactory("MockPoolForOptical")).deploy();
    poolAddr = await pool.getAddress();
    const TaxOptical = await ethers.getContractFactory("TaxOptical");
    taxOptical = await TaxOptical.deploy(
      await registry.getAddress(), deployer.address, BUY_TAX_BPS, SELL_TAX_BPS
    );
    await taxOptical.waitForDeployment();
    taxOptical = await attachOptical(registry, pool, taxOptical, deployer);
  });

  describe("Configuration", function () {
    it("should store immutable config correctly", async function () {
      expect(await taxOptical.buyTaxBps()).to.equal(BUY_TAX_BPS);
      expect(await taxOptical.sellTaxBps()).to.equal(SELL_TAX_BPS);
      expect(await taxOptical.MAX_TAX_BPS()).to.equal(1000n);
    });

    it("should return BEFORE_SWAP flag only", async function () {
      expect(await taxOptical.getFlags()).to.equal(HookFlags.BEFORE_SWAP);
    });

    it("should revert if buy tax exceeds 10%", async function () {
      const TaxOptical = await ethers.getContractFactory("TaxOptical");
      await expect(
        TaxOptical.deploy(ZERO_ADDRESS, deployer.address, 1001n, 0n)
      ).to.be.revertedWithCustomError(taxOptical, "TaxTooHigh");
    });

    it("should revert if sell tax exceeds 10%", async function () {
      const TaxOptical = await ethers.getContractFactory("TaxOptical");
      await expect(
        TaxOptical.deploy(ZERO_ADDRESS, deployer.address, 0n, 1001n)
      ).to.be.revertedWithCustomError(taxOptical, "TaxTooHigh");
    });
  });

  describe("beforeSwap tax", function () {
    it("should apply buy tax as negative amountDelta", async function () {
      const amountIn = ethers.parseUnits("1000", 6);
      const result = await taxOptical.beforeSwap.staticCall(
        poolAddr, alice.address, true, amountIn
      );
      expect(result.proceed).to.be.true;
      // 2% of 1000 = 20, so delta = -20
      const expectedDelta = -((amountIn * BUY_TAX_BPS) / 10000n);
      expect(result.amountDelta).to.equal(expectedDelta);
    });

    it("should apply sell tax as negative amountDelta", async function () {
      const amountIn = ethers.parseUnits("500", 6);
      const result = await taxOptical.beforeSwap.staticCall(
        poolAddr, alice.address, false, amountIn
      );
      expect(result.proceed).to.be.true;
      // 3% of 500 = 15, so delta = -15
      const expectedDelta = -((amountIn * SELL_TAX_BPS) / 10000n);
      expect(result.amountDelta).to.equal(expectedDelta);
    });

    it("should return zero delta when tax is zero", async function () {
      const TaxOptical = await ethers.getContractFactory("TaxOptical");
      const zeroTaxRegistry = await registryFixture(deployer);
      let zeroTax = await TaxOptical.deploy(await zeroTaxRegistry.getAddress(), deployer.address, 0n, 0n);
      await zeroTax.waitForDeployment();
      zeroTax = await attachOptical(zeroTaxRegistry, pool, zeroTax, deployer);

      const result = await zeroTax.beforeSwap.staticCall(
        poolAddr, alice.address, true, ethers.parseUnits("1000", 6)
      );
      expect(result.proceed).to.be.true;
      expect(result.amountDelta).to.equal(0);
    });

    it("should allow max 10% tax deployment", async function () {
      const TaxOptical = await ethers.getContractFactory("TaxOptical");
      const maxTaxRegistry = await registryFixture(deployer);
      let maxTax = await TaxOptical.deploy(await maxTaxRegistry.getAddress(), deployer.address, 1000n, 1000n);
      await maxTax.waitForDeployment();
      maxTax = await attachOptical(maxTaxRegistry, pool, maxTax, deployer);

      const amountIn = ethers.parseUnits("1000", 6);
      const result = await maxTax.beforeSwap.staticCall(
        poolAddr, alice.address, true, amountIn
      );
      // 10% of 1000 = 100
      expect(result.amountDelta).to.equal(-(amountIn / 10n));
    });
  });
});
