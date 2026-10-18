const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("Pausable", function () {
  let mock;

  beforeEach(async function () {
    const Mock = await ethers.getContractFactory("MockPausable");
    mock = await Mock.deploy();
    await mock.waitForDeployment();
  });

  it("should start unpaused", async function () {
    expect(await mock.paused()).to.be.false;
  });

  it("should allow protected action when not paused", async function () {
    await mock.protectedAction(42);
    expect(await mock.value()).to.equal(42);
  });

  it("should pause", async function () {
    await mock.pause();
    expect(await mock.paused()).to.be.true;
  });

  it("should emit PauseToggled on pause", async function () {
    await expect(mock.pause()).to.emit(mock, "PauseToggled").withArgs(true);
  });

  it("should revert protected action when paused", async function () {
    await mock.pause();
    await expect(mock.protectedAction(42)).to.be.revertedWithCustomError(mock, "Paused");
  });

  it("should allow emergency action when paused", async function () {
    await mock.pause();
    // Should not revert — that's all we need to verify
    await mock.emergencyAction();
  });

  it("should unpause", async function () {
    await mock.pause();
    await mock.unpause();
    expect(await mock.paused()).to.be.false;
    await mock.protectedAction(99);
    expect(await mock.value()).to.equal(99);
  });

  it("should emit PauseToggled on unpause", async function () {
    await mock.pause();
    await expect(mock.unpause()).to.emit(mock, "PauseToggled").withArgs(false);
  });

  it("should revert double pause", async function () {
    await mock.pause();
    await expect(mock.pause()).to.be.revertedWithCustomError(mock, "Paused");
  });

  it("should revert unpause when not paused", async function () {
    await expect(mock.unpause()).to.be.revertedWithCustomError(mock, "NotPaused");
  });
});
