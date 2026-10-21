const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("PoolAddress", function () {
  let lib;

  before(async function () {
    const Wrapper = await ethers.getContractFactory("PoolAddressWrapper");
    lib = await Wrapper.deploy();
    await lib.waitForDeployment();
  });

  describe("computeSalt", function () {
    it("should return keccak256 of packed token address", async function () {
      const token = "0x1234567890abcdef1234567890abcdef12345678";
      const salt = await lib.computeSalt(token);
      const expected = ethers.keccak256(ethers.solidityPacked(["address"], [token]));
      expect(salt).to.equal(expected);
    });

    it("should return different salts for different tokens", async function () {
      const salt1 = await lib.computeSalt("0x1111111111111111111111111111111111111111");
      const salt2 = await lib.computeSalt("0x2222222222222222222222222222222222222222");
      expect(salt1).to.not.equal(salt2);
    });
  });

  describe("computeAddress", function () {
    it("should return deterministic address", async function () {
      const factory = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
      const beacon = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
      const token = "0xcccccccccccccccccccccccccccccccccccccccc";
      const creationCode = "0x6080604052"; // dummy bytecode

      const addr = await lib.computeAddress(factory, beacon, token, creationCode, "0x");
      expect(addr).to.not.equal(ethers.ZeroAddress);

      // Same inputs should produce same address
      const addr2 = await lib.computeAddress(factory, beacon, token, creationCode, "0x");
      expect(addr).to.equal(addr2);
    });

    it("should produce different addresses for different tokens", async function () {
      const factory = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
      const beacon = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
      const creationCode = "0x6080604052";

      const addr1 = await lib.computeAddress(
        factory, beacon, "0x1111111111111111111111111111111111111111", creationCode, "0x"
      );
      const addr2 = await lib.computeAddress(
        factory, beacon, "0x2222222222222222222222222222222222222222", creationCode, "0x"
      );
      expect(addr1).to.not.equal(addr2);
    });

    it("should produce different addresses for different factories", async function () {
      const beacon = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
      const token = "0xcccccccccccccccccccccccccccccccccccccccc";
      const creationCode = "0x6080604052";

      const addr1 = await lib.computeAddress(
        "0x1111111111111111111111111111111111111111", beacon, token, creationCode, "0x"
      );
      const addr2 = await lib.computeAddress(
        "0x2222222222222222222222222222222222222222", beacon, token, creationCode, "0x"
      );
      expect(addr1).to.not.equal(addr2);
    });
  });
});
