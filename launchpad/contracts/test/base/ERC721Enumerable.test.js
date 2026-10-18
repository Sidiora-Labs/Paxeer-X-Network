const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("ERC721Enumerable", function () {
  let nft, deployer, alice, bob;

  before(async function () {
    [deployer, alice, bob] = await ethers.getSigners();
  });

  beforeEach(async function () {
    const Mock = await ethers.getContractFactory("MockERC721Enumerable");
    nft = await Mock.deploy();
    await nft.waitForDeployment();
  });

  describe("supportsInterface", function () {
    it("should support ERC721Enumerable interface", async function () {
      expect(await nft.supportsInterface("0x780e9d63")).to.be.true;
    });

    it("should still support ERC721 and ERC165", async function () {
      expect(await nft.supportsInterface("0x80ac58cd")).to.be.true;
      expect(await nft.supportsInterface("0x01ffc9a7")).to.be.true;
    });
  });

  describe("totalSupply", function () {
    it("should start at 0", async function () {
      expect(await nft.totalSupply()).to.equal(0);
    });

    it("should increase on mint", async function () {
      await nft.mint(alice.address, 1);
      expect(await nft.totalSupply()).to.equal(1);
      await nft.mint(alice.address, 2);
      expect(await nft.totalSupply()).to.equal(2);
    });

    it("should decrease on burn", async function () {
      await nft.mint(alice.address, 1);
      await nft.mint(alice.address, 2);
      await nft.burn(1);
      expect(await nft.totalSupply()).to.equal(1);
    });
  });

  describe("tokenByIndex", function () {
    it("should return correct token IDs", async function () {
      await nft.mint(alice.address, 10);
      await nft.mint(bob.address, 20);
      await nft.mint(alice.address, 30);

      expect(await nft.tokenByIndex(0)).to.equal(10);
      expect(await nft.tokenByIndex(1)).to.equal(20);
      expect(await nft.tokenByIndex(2)).to.equal(30);
    });

    it("should revert on out of bounds", async function () {
      await nft.mint(alice.address, 1);
      await expect(nft.tokenByIndex(1)).to.be.revertedWithCustomError(nft, "IndexOutOfBounds");
    });
  });

  describe("tokenOfOwnerByIndex", function () {
    it("should return correct tokens per owner", async function () {
      await nft.mint(alice.address, 10);
      await nft.mint(alice.address, 20);
      await nft.mint(bob.address, 30);

      expect(await nft.tokenOfOwnerByIndex(alice.address, 0)).to.equal(10);
      expect(await nft.tokenOfOwnerByIndex(alice.address, 1)).to.equal(20);
      expect(await nft.tokenOfOwnerByIndex(bob.address, 0)).to.equal(30);
    });

    it("should revert on out of bounds", async function () {
      await nft.mint(alice.address, 1);
      await expect(
        nft.tokenOfOwnerByIndex(alice.address, 1)
      ).to.be.revertedWithCustomError(nft, "IndexOutOfBounds");
    });
  });

  describe("enumeration updates after transfer", function () {
    it("should update owner enumeration on transfer", async function () {
      await nft.mint(deployer.address, 1);
      await nft.mint(deployer.address, 2);

      await nft.transferFrom(deployer.address, alice.address, 1);

      expect(await nft.tokenOfOwnerByIndex(alice.address, 0)).to.equal(1);
      expect(await nft.balanceOf(deployer.address)).to.equal(1);
      expect(await nft.balanceOf(alice.address)).to.equal(1);
    });
  });

  describe("enumeration updates after burn", function () {
    it("should remove from enumeration on burn", async function () {
      await nft.mint(alice.address, 1);
      await nft.mint(alice.address, 2);
      await nft.mint(alice.address, 3);

      await nft.burn(2);

      expect(await nft.totalSupply()).to.equal(2);
      // Token 3 should have moved to index 1 (swap-and-pop)
      expect(await nft.tokenByIndex(0)).to.equal(1);
      expect(await nft.tokenByIndex(1)).to.equal(3);
    });
  });
});
