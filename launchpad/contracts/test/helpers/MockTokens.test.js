const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("Mock Tokens", function () {
  let deployer, alice;

  before(async function () {
    [deployer, alice] = await ethers.getSigners();
  });

  describe("MockERC20", function () {
    it("should mint and transfer", async function () {
      const Mock = await ethers.getContractFactory("MockERC20");
      const token = await Mock.deploy("Test Token", "TEST", 18);
      await token.waitForDeployment();

      await token.mint(deployer.address, ethers.parseEther("1000"));
      expect(await token.balanceOf(deployer.address)).to.equal(ethers.parseEther("1000"));

      await token.transfer(alice.address, ethers.parseEther("100"));
      expect(await token.balanceOf(alice.address)).to.equal(ethers.parseEther("100"));
      expect(await token.balanceOf(deployer.address)).to.equal(ethers.parseEther("900"));
    });
  });

  describe("MockNonStandardERC20", function () {
    it("should transfer without return value", async function () {
      const Mock = await ethers.getContractFactory("MockNonStandardERC20");
      const token = await Mock.deploy("NonStd", "NSTD", 18);
      await token.waitForDeployment();

      await token.mint(deployer.address, ethers.parseEther("1000"));
      await token.transfer(alice.address, ethers.parseEther("100"));
      expect(await token.balanceOf(alice.address)).to.equal(ethers.parseEther("100"));
    });
  });

  describe("MockRevertingERC20", function () {
    it("should revert on transfer when enabled", async function () {
      const Mock = await ethers.getContractFactory("MockRevertingERC20");
      const token = await Mock.deploy("Revert", "REV", 18);
      await token.waitForDeployment();

      await token.mint(deployer.address, ethers.parseEther("1000"));
      await expect(token.transfer(alice.address, ethers.parseEther("100")))
        .to.be.revertedWith("MockRevertingERC20: forced revert");
    });

    it("should transfer when revert disabled", async function () {
      const Mock = await ethers.getContractFactory("MockRevertingERC20");
      const token = await Mock.deploy("Revert", "REV", 18);
      await token.waitForDeployment();

      await token.mint(deployer.address, ethers.parseEther("1000"));
      await token.setShouldRevert(false);
      await token.transfer(alice.address, ethers.parseEther("100"));
      expect(await token.balanceOf(alice.address)).to.equal(ethers.parseEther("100"));
    });
  });
});
