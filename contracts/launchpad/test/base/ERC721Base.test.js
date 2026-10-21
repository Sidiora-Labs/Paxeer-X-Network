const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("ERC721Base", function () {
  let nft, deployer, alice, bob;

  before(async function () {
    [deployer, alice, bob] = await ethers.getSigners();
  });

  beforeEach(async function () {
    const Mock = await ethers.getContractFactory("MockERC721Base");
    nft = await Mock.deploy();
    await nft.waitForDeployment();
  });

  describe("metadata", function () {
    it("should have correct name and symbol", async function () {
      expect(await nft.name()).to.equal("TestNFT");
      expect(await nft.symbol()).to.equal("TNFT");
    });
  });

  describe("supportsInterface (ERC165)", function () {
    it("should support ERC721 interface", async function () {
      expect(await nft.supportsInterface("0x80ac58cd")).to.be.true;
    });

    it("should support ERC165 interface", async function () {
      expect(await nft.supportsInterface("0x01ffc9a7")).to.be.true;
    });

    it("should not support random interface", async function () {
      expect(await nft.supportsInterface("0xdeadbeef")).to.be.false;
    });
  });

  describe("mint", function () {
    it("should mint token to address", async function () {
      await nft.mint(alice.address, 1);
      expect(await nft.ownerOf(1)).to.equal(alice.address);
      expect(await nft.balanceOf(alice.address)).to.equal(1);
    });

    it("should emit Transfer event", async function () {
      await expect(nft.mint(alice.address, 1))
        .to.emit(nft, "Transfer")
        .withArgs(ethers.ZeroAddress, alice.address, 1);
    });

    it("should revert on zero address", async function () {
      await expect(nft.mint(ethers.ZeroAddress, 1)).to.be.revertedWithCustomError(nft, "ZeroAddress");
    });

    it("should revert on already minted token", async function () {
      await nft.mint(alice.address, 1);
      await expect(nft.mint(bob.address, 1)).to.be.revertedWithCustomError(nft, "AlreadyMinted");
    });
  });

  describe("burn", function () {
    it("should burn token", async function () {
      await nft.mint(alice.address, 1);
      await nft.burn(1);
      expect(await nft.exists(1)).to.be.false;
      expect(await nft.balanceOf(alice.address)).to.equal(0);
    });
  });

  describe("transfer", function () {
    beforeEach(async function () {
      await nft.mint(deployer.address, 1);
    });

    it("should transfer by owner", async function () {
      await nft.transferFrom(deployer.address, alice.address, 1);
      expect(await nft.ownerOf(1)).to.equal(alice.address);
    });

    it("should emit Transfer event", async function () {
      await expect(nft.transferFrom(deployer.address, alice.address, 1))
        .to.emit(nft, "Transfer")
        .withArgs(deployer.address, alice.address, 1);
    });

    it("should revert transfer by non-owner/non-approved", async function () {
      await expect(
        nft.connect(alice).transferFrom(deployer.address, bob.address, 1)
      ).to.be.revertedWithCustomError(nft, "NotApproved");
    });

    it("should revert transfer to zero address", async function () {
      await expect(
        nft.transferFrom(deployer.address, ethers.ZeroAddress, 1)
      ).to.be.revertedWithCustomError(nft, "ZeroAddress");
    });
  });

  describe("approve", function () {
    beforeEach(async function () {
      await nft.mint(deployer.address, 1);
    });

    it("should approve and allow transferFrom", async function () {
      await nft.approve(alice.address, 1);
      expect(await nft.getApproved(1)).to.equal(alice.address);
      await nft.connect(alice).transferFrom(deployer.address, bob.address, 1);
      expect(await nft.ownerOf(1)).to.equal(bob.address);
    });

    it("should emit Approval event", async function () {
      await expect(nft.approve(alice.address, 1))
        .to.emit(nft, "Approval")
        .withArgs(deployer.address, alice.address, 1);
    });

    it("should clear approval after transfer", async function () {
      await nft.approve(alice.address, 1);
      await nft.transferFrom(deployer.address, bob.address, 1);
      expect(await nft.getApproved(1)).to.equal(ethers.ZeroAddress);
    });

    it("should revert approve by non-owner", async function () {
      await expect(
        nft.connect(alice).approve(bob.address, 1)
      ).to.be.revertedWithCustomError(nft, "NotApproved");
    });
  });

  describe("setApprovalForAll", function () {
    beforeEach(async function () {
      await nft.mint(deployer.address, 1);
      await nft.mint(deployer.address, 2);
    });

    it("should set operator approval", async function () {
      await nft.setApprovalForAll(alice.address, true);
      expect(await nft.isApprovedForAll(deployer.address, alice.address)).to.be.true;
    });

    it("should allow operator to transfer any token", async function () {
      await nft.setApprovalForAll(alice.address, true);
      await nft.connect(alice).transferFrom(deployer.address, bob.address, 1);
      await nft.connect(alice).transferFrom(deployer.address, bob.address, 2);
      expect(await nft.ownerOf(1)).to.equal(bob.address);
      expect(await nft.ownerOf(2)).to.equal(bob.address);
    });

    it("should revoke operator approval", async function () {
      await nft.setApprovalForAll(alice.address, true);
      await nft.setApprovalForAll(alice.address, false);
      await expect(
        nft.connect(alice).transferFrom(deployer.address, bob.address, 1)
      ).to.be.revertedWithCustomError(nft, "NotApproved");
    });
  });

  describe("safeTransferFrom", function () {
    it("should transfer to EOA", async function () {
      await nft.mint(deployer.address, 1);
      await nft["safeTransferFrom(address,address,uint256)"](deployer.address, alice.address, 1);
      expect(await nft.ownerOf(1)).to.equal(alice.address);
    });

    it("should transfer to valid receiver contract", async function () {
      const Receiver = await ethers.getContractFactory("MockERC721Receiver");
      const receiver = await Receiver.deploy();

      await nft.mint(deployer.address, 1);
      await nft["safeTransferFrom(address,address,uint256)"](
        deployer.address, await receiver.getAddress(), 1
      );
      expect(await nft.ownerOf(1)).to.equal(await receiver.getAddress());
    });

    it("should revert transfer to bad receiver contract", async function () {
      const Bad = await ethers.getContractFactory("MockBadERC721Receiver");
      const bad = await Bad.deploy();

      await nft.mint(deployer.address, 1);
      await expect(
        nft["safeTransferFrom(address,address,uint256)"](
          deployer.address, await bad.getAddress(), 1
        )
      ).to.be.revertedWithCustomError(nft, "NonERC721Receiver");
    });
  });
});
