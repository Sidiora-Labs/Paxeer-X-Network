const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("TransferHelper", function () {
  let wrapper, standard, nonStandard, reverting;
  let deployer, alice;

  before(async function () {
    [deployer, alice] = await ethers.getSigners();

    const Wrapper = await ethers.getContractFactory("TransferHelperWrapper");
    wrapper = await Wrapper.deploy();
    await wrapper.waitForDeployment();

    const MockERC20 = await ethers.getContractFactory("MockERC20");
    standard = await MockERC20.deploy("Standard", "STD", 18);
    await standard.waitForDeployment();

    const MockNonStandard = await ethers.getContractFactory("MockNonStandardERC20");
    nonStandard = await MockNonStandard.deploy("NonStd", "NSTD", 18);
    await nonStandard.waitForDeployment();

    const MockReverting = await ethers.getContractFactory("MockRevertingERC20");
    reverting = await MockReverting.deploy("Revert", "REV", 18);
    await reverting.waitForDeployment();

    const wrapperAddr = await wrapper.getAddress();
    const amount = ethers.parseEther("10000");

    // Fund wrapper with tokens for safeTransfer tests
    await standard.mint(wrapperAddr, amount);
    await nonStandard.mint(wrapperAddr, amount);
    await reverting.mint(wrapperAddr, amount);

    // Fund deployer for safeTransferFrom tests
    await standard.mint(deployer.address, amount);
    await nonStandard.mint(deployer.address, amount);
    await reverting.mint(deployer.address, amount);
  });

  describe("safeTransfer", function () {
    it("should transfer standard ERC20", async function () {
      const wrapperAddr = await wrapper.getAddress();
      await wrapper.safeTransfer(await standard.getAddress(), alice.address, ethers.parseEther("100"));
      expect(await standard.balanceOf(alice.address)).to.equal(ethers.parseEther("100"));
    });

    it("should transfer non-standard ERC20 (no return value)", async function () {
      await wrapper.safeTransfer(await nonStandard.getAddress(), alice.address, ethers.parseEther("100"));
      expect(await nonStandard.balanceOf(alice.address)).to.equal(ethers.parseEther("100"));
    });

    it("should revert on reverting ERC20", async function () {
      await expect(
        wrapper.safeTransfer(await reverting.getAddress(), alice.address, ethers.parseEther("100"))
      ).to.be.revertedWithCustomError(wrapper, "TransferFailed");
    });
  });

  describe("safeTransferFrom", function () {
    it("should transferFrom standard ERC20", async function () {
      const wrapperAddr = await wrapper.getAddress();
      await standard.approve(wrapperAddr, ethers.parseEther("100"));
      await wrapper.safeTransferFrom(await standard.getAddress(), deployer.address, alice.address, ethers.parseEther("50"));
      expect(await standard.balanceOf(alice.address)).to.equal(ethers.parseEther("150"));
    });

    it("should transferFrom non-standard ERC20", async function () {
      const wrapperAddr = await wrapper.getAddress();
      await nonStandard.approve(wrapperAddr, ethers.parseEther("100"));
      await wrapper.safeTransferFrom(await nonStandard.getAddress(), deployer.address, alice.address, ethers.parseEther("50"));
      expect(await nonStandard.balanceOf(alice.address)).to.equal(ethers.parseEther("150"));
    });

    it("should revert on reverting ERC20", async function () {
      await expect(
        wrapper.safeTransferFrom(await reverting.getAddress(), deployer.address, alice.address, ethers.parseEther("50"))
      ).to.be.revertedWithCustomError(wrapper, "TransferFromFailed");
    });
  });

  describe("safeApprove", function () {
    it("should approve standard ERC20", async function () {
      const wrapperAddr = await wrapper.getAddress();
      await wrapper.safeApprove(await standard.getAddress(), alice.address, ethers.parseEther("500"));
      expect(await standard.allowance(wrapperAddr, alice.address)).to.equal(ethers.parseEther("500"));
    });

    it("should approve non-standard ERC20", async function () {
      const wrapperAddr = await wrapper.getAddress();
      await wrapper.safeApprove(await nonStandard.getAddress(), alice.address, ethers.parseEther("500"));
      expect(await nonStandard.allowance(wrapperAddr, alice.address)).to.equal(ethers.parseEther("500"));
    });

    it("should revert on reverting ERC20", async function () {
      await expect(
        wrapper.safeApprove(await reverting.getAddress(), alice.address, ethers.parseEther("500"))
      ).to.be.revertedWithCustomError(wrapper, "ApproveFailed");
    });
  });
});
