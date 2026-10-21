const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("FeeLib", function () {
  let lib;

  // Protocol defaults
  const BASE_FEE = 30n; // 0.30%
  const MIN_FEE = 10n; // 0.10%
  const MAX_FEE = 300n; // 3.00%
  const FEE_DECAY_RATE = 500n;
  const VOLATILITY_WEIGHT = 100n;
  const CONCENTRATION_WEIGHT = 100n;

  before(async function () {
    const Wrapper = await ethers.getContractFactory("FeeLibWrapper");
    lib = await Wrapper.deploy();
    await lib.waitForDeployment();
  });

  describe("calculateAgeFactor", function () {
    it("should return full decay rate for brand new pool (0 seconds)", async function () {
      // ageFactor = 500 / (1 + 0) = 500
      expect(await lib.calculateAgeFactor(FEE_DECAY_RATE, 0)).to.equal(500n);
    });

    it("should decay over time", async function () {
      // After 1 hour: 500 / (1 + 1) = 250
      expect(await lib.calculateAgeFactor(FEE_DECAY_RATE, 3600)).to.equal(250n);
      // After 24 hours: 500 / (1 + 24) = 20
      expect(await lib.calculateAgeFactor(FEE_DECAY_RATE, 86400)).to.equal(20n);
      // After 499 hours: 500 / (1 + 499) = 1
      expect(await lib.calculateAgeFactor(FEE_DECAY_RATE, 499 * 3600)).to.equal(1n);
    });

    it("should approach zero for very old pools", async function () {
      // After 1000 hours: 500 / 1001 = 0
      expect(await lib.calculateAgeFactor(FEE_DECAY_RATE, 1000 * 3600)).to.equal(0n);
    });

    it("should handle zero decay rate", async function () {
      expect(await lib.calculateAgeFactor(0, 3600)).to.equal(0n);
    });
  });

  describe("calculateVolatilityFactor", function () {
    it("should return 0 for zero volatility", async function () {
      expect(await lib.calculateVolatilityFactor(VOLATILITY_WEIGHT, 0)).to.equal(0n);
    });

    it("should scale with volatility", async function () {
      // volatility = 0.5e6 (50% std dev) → factor = 100 * 0.5e6 / 1e6 = 50
      const vol = 500000n; // 0.5e6
      expect(await lib.calculateVolatilityFactor(VOLATILITY_WEIGHT, vol)).to.equal(50n);
    });

    it("should handle high volatility", async function () {
      // volatility = 2.0e6 (200% std dev) → factor = 100 * 2e6 / 1e6 = 200
      const vol = 2000000n; // 2e6
      expect(await lib.calculateVolatilityFactor(VOLATILITY_WEIGHT, vol)).to.equal(200n);
    });

    it("should handle zero weight", async function () {
      const vol = 1000000n; // 1e6
      expect(await lib.calculateVolatilityFactor(0, vol)).to.equal(0n);
    });
  });

  describe("calculateConcentrationFactor", function () {
    it("should return 0 for zero concentration", async function () {
      expect(await lib.calculateConcentrationFactor(CONCENTRATION_WEIGHT, 0)).to.equal(0n);
    });

    it("should scale with holder percentage", async function () {
      // topHolder owns 50% = 5000 bps → factor = 100 * 5000 / 10000 = 50
      expect(await lib.calculateConcentrationFactor(CONCENTRATION_WEIGHT, 5000)).to.equal(50n);
    });

    it("should handle max concentration (100%)", async function () {
      // topHolder owns 100% = 10000 bps → factor = 100 * 10000 / 10000 = 100
      expect(await lib.calculateConcentrationFactor(CONCENTRATION_WEIGHT, 10000)).to.equal(100n);
    });
  });

  describe("calculateDynamicFee", function () {
    it("should return baseFee for mature pool with no volatility/concentration", async function () {
      // Old pool (1000 hours), no volatility, no concentration
      const fee = await lib.calculateDynamicFee(
        BASE_FEE, MIN_FEE, MAX_FEE, FEE_DECAY_RATE,
        VOLATILITY_WEIGHT, CONCENTRATION_WEIGHT,
        1000 * 3600, 0, 0
      );
      // baseFee=30, ageFactor=0, vol=0, conc=0 → 30
      expect(fee).to.equal(30n);
    });

    it("should be high for brand new pool", async function () {
      // New pool (0 seconds), no volatility, no concentration
      const fee = await lib.calculateDynamicFee(
        BASE_FEE, MIN_FEE, MAX_FEE, FEE_DECAY_RATE,
        VOLATILITY_WEIGHT, CONCENTRATION_WEIGHT,
        0, 0, 0
      );
      // baseFee=30 + ageFactor=500 = 530, clamped to max=300
      expect(fee).to.equal(MAX_FEE);
    });

    it("should clamp to minFee", async function () {
      // Use baseFee=0, old pool, no vol/conc → should clamp to minFee
      const fee = await lib.calculateDynamicFee(
        0, MIN_FEE, MAX_FEE, 0,
        0, 0,
        86400 * 365, 0, 0
      );
      expect(fee).to.equal(MIN_FEE);
    });

    it("should clamp to maxFee", async function () {
      // Everything maxed: new pool + high vol + high concentration
      const highVol = 3000000n; // 3e6
      const fee = await lib.calculateDynamicFee(
        BASE_FEE, MIN_FEE, MAX_FEE, FEE_DECAY_RATE,
        VOLATILITY_WEIGHT, CONCENTRATION_WEIGHT,
        0, highVol, 10000
      );
      expect(fee).to.equal(MAX_FEE);
    });

    it("should combine all factors correctly", async function () {
      // Pool age 24h: ageFactor = 500/25 = 20
      // Volatility 0.1e6 = 100000: volFactor = 100 * 100000 / 1e6 = 10
      // Concentration 20% = 2000 bps: concFactor = 100 * 2000/10000 = 20
      // Total: 30 + 20 + 10 + 20 = 80
      const fee = await lib.calculateDynamicFee(
        BASE_FEE, MIN_FEE, MAX_FEE, FEE_DECAY_RATE,
        VOLATILITY_WEIGHT, CONCENTRATION_WEIGHT,
        86400, 100000n, 2000
      );
      expect(fee).to.equal(80n);
    });

    it("should handle moderate pool age with some volatility", async function () {
      // Pool age 1h: ageFactor = 500/2 = 250
      // Volatility 0.3e6 = 300000: volFactor = 100 * 300000 / 1e6 = 30
      // No concentration
      // Total: 30 + 250 + 30 + 0 = 310, clamped to 300
      const fee = await lib.calculateDynamicFee(
        BASE_FEE, MIN_FEE, MAX_FEE, FEE_DECAY_RATE,
        VOLATILITY_WEIGHT, CONCENTRATION_WEIGHT,
        3600, 300000n, 0
      );
      expect(fee).to.equal(MAX_FEE);
    });
  });

  describe("calculateVolatility", function () {
    it("should return 0 for fewer than 2 snapshots", async function () {
      const snapshots = [ethers.parseUnits("100", 6), 0n, 0n, 0n, 0n, 0n, 0n, 0n];
      expect(await lib.calculateVolatility(snapshots, 1)).to.equal(0n);
      expect(await lib.calculateVolatility(snapshots, 0)).to.equal(0n);
    });

    it("should return 0 for constant prices", async function () {
      // Price ~0.00001 USDL per token = 10 raw (in 6-dec)
      const p = 10n;
      const snapshots = [p, p, p, p, 0n, 0n, 0n, 0n];
      expect(await lib.calculateVolatility(snapshots, 4)).to.equal(0n);
    });

    it("should detect volatility from price changes", async function () {
      // Prices with meaningful deltas (raw 6-dec values)
      const snapshots = [
        100000n,
        120000n,
        90000n,
        150000n,
        0n, 0n, 0n, 0n,
      ];
      const vol = await lib.calculateVolatility(snapshots, 4);
      expect(vol).to.be.gt(0);
    });

    it("should return higher volatility for bigger swings", async function () {
      // Small swings (raw 6-dec values)
      const small = [
        100000n,
        101000n,
        99000n,
        102000n,
        0n, 0n, 0n, 0n,
      ];
      // Big swings
      const big = [
        100000n,
        200000n,
        50000n,
        300000n,
        0n, 0n, 0n, 0n,
      ];
      const volSmall = await lib.calculateVolatility(small, 4);
      const volBig = await lib.calculateVolatility(big, 4);
      expect(volBig).to.be.gt(volSmall);
    });
  });
  describe("audit remediation: bounded dimensionless volatility", function () {
    it("produces identical volatility for equal percentage moves at different prices", async function () {
      const series = [100n, 120n, 90n, 150n, 0n, 0n, 0n, 0n];
      const scaled = series.map(x => x * 10n ** 18n);
      expect(await lib.calculateVolatility(series, 4)).to.equal(await lib.calculateVolatility(scaled, 4));
    });
    it("detects one movement and repeated equal-sized oscillations", async function () {
      expect(await lib.calculateVolatility([100n,120n,0n,0n,0n,0n,0n,0n], 2)).to.equal(200000n);
      expect(await lib.calculateVolatility([100n,120n,100n,120n,100n,120n,100n,120n], 8)).to.be.gt(180000n);
    });
    it("caps extreme movement and fee components without intermediate overflow", async function () {
      expect(await lib.calculateVolatility([1n,ethers.MaxUint256,0n,0n,0n,0n,0n,0n], 2)).to.equal(1000000n);
      expect(await lib.calculateDynamicFee(30,10,300,0,ethers.MaxUint256,ethers.MaxUint256,0,1000000,10000)).to.equal(300n);
      expect(await lib.calculateDynamicFee(ethers.MaxUint256,10,300,ethers.MaxUint256,ethers.MaxUint256,ethers.MaxUint256,0,1000000,10000)).to.equal(300n);
    });
  });

});
