const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("FixedPoint128", function () {
  let fp;
  let Q128;

  before(async function () {
    const Wrapper = await ethers.getContractFactory("FixedPoint128Wrapper");
    fp = await Wrapper.deploy();
    await fp.waitForDeployment();
    Q128 = await fp.Q128();
  });

  describe("Q128 constant", function () {
    it("should equal 2^128", async function () {
      expect(Q128).to.equal(1n << 128n);
    });
  });

  describe("toQ128 / fromQ128", function () {
    it("should convert to Q128 and back", async function () {
      const val = 42n;
      const q = await fp.toQ128(val);
      expect(q).to.equal(val * Q128);
      expect(await fp.fromQ128(q)).to.equal(val);
    });

    it("should truncate fractional part on fromQ128", async function () {
      // A Q128 value representing 1.5 = Q128 + Q128/2
      const onePointFive = Q128 + Q128 / 2n;
      expect(await fp.fromQ128(onePointFive)).to.equal(1n);
    });

    it("should handle zero", async function () {
      expect(await fp.toQ128(0)).to.equal(0);
      expect(await fp.fromQ128(0)).to.equal(0);
    });
  });

  describe("mulQ128", function () {
    it("should multiply by Q128(1) to get same value", async function () {
      const x = ethers.parseEther("1000");
      const oneQ128 = Q128; // represents 1.0
      expect(await fp.mulQ128(x, oneQ128)).to.equal(x);
    });

    it("should multiply by Q128(0.5) to get half", async function () {
      const x = ethers.parseEther("1000");
      const halfQ128 = Q128 / 2n; // represents 0.5
      expect(await fp.mulQ128(x, halfQ128)).to.equal(ethers.parseEther("500"));
    });

    it("should handle zero multiplier", async function () {
      expect(await fp.mulQ128(ethers.parseEther("1000"), 0)).to.equal(0);
    });

    it("should handle large values without overflow", async function () {
      const x = ethers.parseEther("1000000"); // 1e24
      const twoQ128 = Q128 * 2n; // represents 2.0
      expect(await fp.mulQ128(x, twoQ128)).to.equal(ethers.parseEther("2000000"));
    });
  });

  describe("divQ128", function () {
    it("should divide by Q128(1) to get same value", async function () {
      const x = ethers.parseEther("1000");
      expect(await fp.divQ128(x, Q128)).to.equal(x);
    });

    it("should divide by Q128(2) to get half", async function () {
      const x = ethers.parseEther("1000");
      const twoQ128 = Q128 * 2n;
      expect(await fp.divQ128(x, twoQ128)).to.equal(ethers.parseEther("500"));
    });

    it("should revert on division by zero", async function () {
      await expect(fp.divQ128(100, 0)).to.be.revertedWithCustomError(
        fp,
        "DivisionByZero"
      );
    });
  });
});
