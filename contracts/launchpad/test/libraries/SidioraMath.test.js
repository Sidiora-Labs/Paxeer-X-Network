const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("SidioraMath", function () {
  let mathLib;

  before(async function () {
    // Deploy a wrapper contract to test the library
    const Wrapper = await ethers.getContractFactory("SidioraMathWrapper");
    mathLib = await Wrapper.deploy();
    await mathLib.waitForDeployment();
  });

  describe("sqrt", function () {
    it("should return 0 for input 0", async function () {
      expect(await mathLib.sqrt(0)).to.equal(0);
    });

    it("should return 1 for inputs 1, 2, 3", async function () {
      expect(await mathLib.sqrt(1)).to.equal(1);
      expect(await mathLib.sqrt(2)).to.equal(1);
      expect(await mathLib.sqrt(3)).to.equal(1);
    });

    it("should return exact root for perfect squares", async function () {
      expect(await mathLib.sqrt(4)).to.equal(2);
      expect(await mathLib.sqrt(9)).to.equal(3);
      expect(await mathLib.sqrt(16)).to.equal(4);
      expect(await mathLib.sqrt(100)).to.equal(10);
      expect(await mathLib.sqrt(10000)).to.equal(100);
    });

    it("should return floor for non-perfect squares", async function () {
      expect(await mathLib.sqrt(5)).to.equal(2);
      expect(await mathLib.sqrt(8)).to.equal(2);
      expect(await mathLib.sqrt(10)).to.equal(3);
      expect(await mathLib.sqrt(99)).to.equal(9);
    });

    it("should handle large numbers", async function () {
      // sqrt(1e36) = 1e18
      const large = ethers.parseEther("1000000000000000000"); // 1e36
      expect(await mathLib.sqrt(large)).to.equal(ethers.parseEther("1")); // 1e18

      // sqrt(type(uint256).max)
      const maxUint = ethers.MaxUint256;
      const result = await mathLib.sqrt(maxUint);
      // result^2 <= maxUint < (result+1)^2
      expect(result * result).to.be.lte(maxUint);
    });

    it("should handle 1e18 scale numbers typical in DeFi", async function () {
      // sqrt(4e18) should be 2e9 (since sqrt scales by sqrt of the base)
      const val = ethers.parseEther("4");
      expect(await mathLib.sqrt(val)).to.equal(2000000000n);
    });
  });

  describe("mulDiv", function () {
    it("should calculate simple multiplication and division", async function () {
      expect(await mathLib.mulDiv(10, 20, 5)).to.equal(40);
      expect(await mathLib.mulDiv(100, 200, 100)).to.equal(200);
    });

    it("should handle cases where a*b overflows uint256", async function () {
      const max = ethers.MaxUint256;
      const half = max / 2n;
      // (max * 2) / 2 should equal max (if it didn't overflow internally)
      // But max * 2 overflows, so we need the 512-bit intermediate
      expect(await mathLib.mulDiv(max, 2, 2)).to.equal(max);
    });

    it("should handle large numerator with large denominator", async function () {
      const a = ethers.parseEther("1000000"); // 1e24
      const b = ethers.parseEther("1000000"); // 1e24
      const denom = ethers.parseEther("1"); // 1e18
      // (1e24 * 1e24) / 1e18 = 1e30
      expect(await mathLib.mulDiv(a, b, denom)).to.equal(
        1000000000000000000000000000000n
      );
    });

    it("should revert on division by zero", async function () {
      await expect(mathLib.mulDiv(10, 20, 0)).to.be.revertedWithCustomError(
        mathLib,
        "DivisionByZero"
      );
    });

    it("should revert on result overflow", async function () {
      const max = ethers.MaxUint256;
      // max * max / 1 overflows
      await expect(mathLib.mulDiv(max, max, 1)).to.be.revertedWithCustomError(
        mathLib,
        "Overflow"
      );
    });

    it("should return 0 when a or b is 0", async function () {
      expect(await mathLib.mulDiv(0, 100, 1)).to.equal(0);
      expect(await mathLib.mulDiv(100, 0, 1)).to.equal(0);
    });

    it("should handle denominator equal to a*b", async function () {
      expect(await mathLib.mulDiv(7, 11, 77)).to.equal(1);
    });
  });

  describe("mulDivRoundingUp", function () {
    it("should round up when there is a remainder", async function () {
      // 10 * 3 / 4 = 7.5 → rounds to 8
      expect(await mathLib.mulDivRoundingUp(10, 3, 4)).to.equal(8);
    });

    it("should not round when exact", async function () {
      expect(await mathLib.mulDivRoundingUp(10, 4, 4)).to.equal(10);
    });
  });

  describe("min", function () {
    it("should return the smaller value", async function () {
      expect(await mathLib.min(1, 2)).to.equal(1);
      expect(await mathLib.min(100, 50)).to.equal(50);
    });

    it("should return either when equal", async function () {
      expect(await mathLib.min(42, 42)).to.equal(42);
    });
  });

  describe("max", function () {
    it("should return the larger value", async function () {
      expect(await mathLib.max(1, 2)).to.equal(2);
      expect(await mathLib.max(100, 50)).to.equal(100);
    });

    it("should return either when equal", async function () {
      expect(await mathLib.max(42, 42)).to.equal(42);
    });
  });

  describe("abs", function () {
    it("should return positive value for positive input", async function () {
      expect(await mathLib.abs(42)).to.equal(42);
    });

    it("should return positive value for negative input", async function () {
      expect(await mathLib.abs(-42)).to.equal(42);
    });

    it("should return 0 for 0", async function () {
      expect(await mathLib.abs(0)).to.equal(0);
    });
  });

  describe("safeCastToUint128", function () {
    it("should cast values within range", async function () {
      expect(await mathLib.safeCastToUint128(0)).to.equal(0);
      expect(await mathLib.safeCastToUint128(42)).to.equal(42);
      const max128 = (1n << 128n) - 1n;
      expect(await mathLib.safeCastToUint128(max128)).to.equal(max128);
    });

    it("should revert on overflow", async function () {
      const overMax = 1n << 128n;
      await expect(
        mathLib.safeCastToUint128(overMax)
      ).to.be.revertedWithCustomError(mathLib, "Overflow");
    });
  });

  describe("safeCastToInt256", function () {
    it("should cast values within range", async function () {
      expect(await mathLib.safeCastToInt256(0)).to.equal(0);
      expect(await mathLib.safeCastToInt256(42)).to.equal(42);
      const maxInt256 = (1n << 255n) - 1n;
      expect(await mathLib.safeCastToInt256(maxInt256)).to.equal(maxInt256);
    });

    it("should revert on overflow", async function () {
      const overMax = 1n << 255n;
      await expect(
        mathLib.safeCastToInt256(overMax)
      ).to.be.revertedWithCustomError(mathLib, "Overflow");
    });
  });
});
