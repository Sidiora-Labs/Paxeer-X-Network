const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("PoolBeacon", function () {
  let beacon, mockImpl, mockImplV2;
  let deployer, alice;

  before(async function () {
    [deployer, alice] = await ethers.getSigners();
  });

  beforeEach(async function () {
    // Deploy a mock implementation (any contract with code)
    const MockEE = await ethers.getContractFactory("MockEventEmitter");
    mockImpl = await MockEE.deploy();
    await mockImpl.waitForDeployment();

    mockImplV2 = await MockEE.deploy();
    await mockImplV2.waitForDeployment();

    // Deploy PoolBeacon
    const PoolBeacon = await ethers.getContractFactory("PoolBeacon");
    beacon = await PoolBeacon.deploy(await mockImpl.getAddress(), deployer.address);
    await beacon.waitForDeployment();
  });

  it("should store initial implementation", async function () {
    expect(await beacon.implementation()).to.equal(await mockImpl.getAddress());
  });

  it("should set owner correctly", async function () {
    expect(await beacon.owner()).to.equal(deployer.address);
  });

  it("should allow owner to upgrade implementation", async function () {
    await beacon.upgradeTo(await mockImplV2.getAddress());
    expect(await beacon.implementation()).to.equal(await mockImplV2.getAddress());
  });

  it("should emit Upgraded event on upgrade", async function () {
    await expect(beacon.upgradeTo(await mockImplV2.getAddress()))
      .to.emit(beacon, "Upgraded")
      .withArgs(await mockImplV2.getAddress());
  });

  it("should revert upgrade by non-owner", async function () {
    await expect(
      beacon.connect(alice).upgradeTo(await mockImplV2.getAddress())
    ).to.be.revertedWithCustomError(beacon, "Unauthorized");
  });

  it("should revert with zero-address implementation", async function () {
    // EOA addresses have no code, so they fail the code.length check
    await expect(
      beacon.upgradeTo(alice.address)
    ).to.be.revertedWithCustomError(beacon, "InvalidImplementation");
  });
});
