const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("TokenAddress", function () {
  let lib;

  before(async function () {
    const Wrapper = await ethers.getContractFactory("TokenAddressWrapper");
    lib = await Wrapper.deploy();
    await lib.waitForDeployment();
  });

  describe("computeSalt", function () {
    it("should return keccak256 of packed creator+name+symbol+nonce", async function () {
      const creator = "0x1234567890abcdef1234567890abcdef12345678";
      const salt = await lib.computeSalt(creator, "TestToken", "TT", 0);
      const expected = ethers.keccak256(
        ethers.solidityPacked(
          ["address", "string", "string", "uint256"],
          [creator, "TestToken", "TT", 0]
        )
      );
      expect(salt).to.equal(expected);
    });

    it("should return different salts for different nonces", async function () {
      const creator = "0x1234567890abcdef1234567890abcdef12345678";
      const salt0 = await lib.computeSalt(creator, "TestToken", "TT", 0);
      const salt1 = await lib.computeSalt(creator, "TestToken", "TT", 1);
      expect(salt0).to.not.equal(salt1);
    });

    it("should return different salts for different creators", async function () {
      const salt1 = await lib.computeSalt(
        "0x1111111111111111111111111111111111111111", "Token", "TKN", 0
      );
      const salt2 = await lib.computeSalt(
        "0x2222222222222222222222222222222222222222", "Token", "TKN", 0
      );
      expect(salt1).to.not.equal(salt2);
    });
  });

  describe("computeAddress", function () {
    it("should return deterministic address", async function () {
      const factory = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
      const creator = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
      const recipient = "0xcccccccccccccccccccccccccccccccccccccccc";
      const creationCode = "0x6080604052";

      const addr = await lib.computeAddress(
        factory, creator, "Token", "TKN", 0, creationCode,
        ethers.parseUnits("1000000000", 6), recipient
      );
      expect(addr).to.not.equal(ethers.ZeroAddress);

      // Same inputs = same address
      const addr2 = await lib.computeAddress(
        factory, creator, "Token", "TKN", 0, creationCode,
        ethers.parseUnits("1000000000", 6), recipient
      );
      expect(addr).to.equal(addr2);
    });

    it("should produce different addresses for different names", async function () {
      const factory = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
      const creator = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
      const recipient = "0xcccccccccccccccccccccccccccccccccccccccc";
      const creationCode = "0x6080604052";
      const supply = ethers.parseUnits("1000000000", 6);

      const addr1 = await lib.computeAddress(
        factory, creator, "Alpha", "ALPHA", 0, creationCode, supply, recipient
      );
      const addr2 = await lib.computeAddress(
        factory, creator, "Beta", "BETA", 0, creationCode, supply, recipient
      );
      expect(addr1).to.not.equal(addr2);
    });
  });
});
