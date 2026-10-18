const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("ReentrancyGuard", function () {
  let guard;

  beforeEach(async function () {
    const Mock = await ethers.getContractFactory("MockReentrancyGuard");
    guard = await Mock.deploy();
    await guard.waitForDeployment();
  });

  it("should allow single call to protected function", async function () {
    await guard.protectedIncrement();
    expect(await guard.counter()).to.equal(1);
  });

  it("should allow sequential calls to protected function", async function () {
    await guard.protectedIncrement();
    await guard.protectedIncrement();
    expect(await guard.counter()).to.equal(2);
  });

  it("should revert on reentrant call to same function", async function () {
    await expect(guard.reentrantCall()).to.be.revertedWithCustomError(
      guard, "ReentrancyGuardReentrantCall"
    );
  });

  it("should revert on cross-function reentrancy", async function () {
    await expect(guard.crossFunctionReentrantCall()).to.be.revertedWithCustomError(
      guard, "ReentrancyGuardReentrantCall"
    );
  });

  it("should not block unprotected functions", async function () {
    await guard.unprotectedIncrement();
    expect(await guard.counter()).to.equal(1);
  });
});
