const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("BitFlag", function () {
  let bf;

  before(async function () {
    const Wrapper = await ethers.getContractFactory("BitFlagWrapper");
    bf = await Wrapper.deploy();
    await bf.waitForDeployment();
  });

  describe("constants", function () {
    it("should have correct flag values", async function () {
      expect(await bf.BEFORE_SWAP()).to.equal(1);
      expect(await bf.AFTER_SWAP()).to.equal(2);
      expect(await bf.BEFORE_FEE_DISTRIBUTION()).to.equal(4);
      expect(await bf.AFTER_FEE_DISTRIBUTION()).to.equal(8);
    });
  });

  describe("hasFlag", function () {
    it("should detect set flags", async function () {
      const flags = 0b0101; // BEFORE_SWAP + BEFORE_FEE_DISTRIBUTION
      expect(await bf.hasFlag(flags, 1)).to.be.true; // BEFORE_SWAP
      expect(await bf.hasFlag(flags, 4)).to.be.true; // BEFORE_FEE_DISTRIBUTION
    });

    it("should detect unset flags", async function () {
      const flags = 0b0101;
      expect(await bf.hasFlag(flags, 2)).to.be.false; // AFTER_SWAP
      expect(await bf.hasFlag(flags, 8)).to.be.false; // AFTER_FEE_DISTRIBUTION
    });

    it("should return false for empty flags", async function () {
      expect(await bf.hasFlag(0, 1)).to.be.false;
    });
  });

  describe("setFlag", function () {
    it("should set a flag", async function () {
      const result = await bf.setFlag(0, 1); // set BEFORE_SWAP
      expect(result).to.equal(1);
    });

    it("should not change already-set flags", async function () {
      const result = await bf.setFlag(1, 1); // BEFORE_SWAP already set
      expect(result).to.equal(1);
    });

    it("should combine multiple flags", async function () {
      let flags = await bf.setFlag(0, 1); // BEFORE_SWAP
      flags = await bf.setFlag(flags, 2); // + AFTER_SWAP
      flags = await bf.setFlag(flags, 8); // + AFTER_FEE_DISTRIBUTION
      expect(flags).to.equal(0b1011);
    });
  });

  describe("clearFlag", function () {
    it("should clear a set flag", async function () {
      const flags = 0b1111; // all set
      const result = await bf.clearFlag(flags, 1); // clear BEFORE_SWAP
      expect(result).to.equal(0b1110);
    });

    it("should not change already-unset flags", async function () {
      const flags = 0b1110; // BEFORE_SWAP not set
      const result = await bf.clearFlag(flags, 1);
      expect(result).to.equal(0b1110);
    });
  });
});
