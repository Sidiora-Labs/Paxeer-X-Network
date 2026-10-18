const { expect } = require("chai");
const { ethers } = require("hardhat");
const { ZERO_ADDRESS } = require("../helpers/constants");

describe("OpticalRegistry", function () {
  let registry, registryAddress;
  let deployer, alice, opticalAddr1, opticalAddr2;

  beforeEach(async function () {
    [deployer, alice, opticalAddr1, opticalAddr2] = await ethers.getSigners();

    const Optical = await ethers.getContractFactory("MockOptical");
    opticalAddr1 = { address: await (await Optical.deploy(ZERO_ADDRESS, deployer.address)).getAddress() };
    opticalAddr2 = { address: await (await Optical.deploy(ZERO_ADDRESS, deployer.address)).getAddress() };

    // Deploy OpticalRegistry behind UUPS proxy
    const UUPSProxy = await ethers.getContractFactory("UUPSProxy");
    const OpticalRegistry = await ethers.getContractFactory("OpticalRegistry");

    const impl = await OpticalRegistry.deploy();
    await impl.waitForDeployment();

    const initData = OpticalRegistry.interface.encodeFunctionData("initialize", [
      ZERO_ADDRESS, // eventEmitter (not needed for unit tests)
      deployer.address,
    ]);

    const proxy = await UUPSProxy.deploy(await impl.getAddress(), initData);
    await proxy.waitForDeployment();

    registry = OpticalRegistry.attach(await proxy.getAddress());
    registryAddress = await registry.getAddress();
  });

  describe("Initialization", function () {
    it("should set admin correctly", async function () {
      const DEFAULT_ADMIN_ROLE = ethers.ZeroHash;
      expect(await registry.hasRole(DEFAULT_ADMIN_ROLE, deployer.address)).to.be.true;
    });

    it("should revert double initialization", async function () {
      await expect(
        registry.initialize(ZERO_ADDRESS, deployer.address)
      ).to.be.revertedWithCustomError(registry, "AlreadyInitialized");
    });
  });

  describe("Register optical", function () {
    it("should register an optical with metadata", async function () {
      const tx = await registry.registerOptical(
        opticalAddr1.address, "AntiSnipe", "Blocks large early buys", 2, "Sidiora Team"
      );
      await expect(tx).to.emit(registry, "OpticalRegistered");

      expect(await registry.isRegistered(opticalAddr1.address)).to.be.true;
      expect(await registry.isApproved(opticalAddr1.address)).to.be.true;

      const meta = await registry.getOpticalMetadata(opticalAddr1.address);
      expect(meta.name).to.equal("AntiSnipe");
      expect(meta.description).to.equal("Blocks large early buys");
      expect(meta.riskLevel).to.equal(2);
      expect(meta.auditor).to.equal("Sidiora Team");
    });

    it("should revert registering zero address", async function () {
      await expect(
        registry.registerOptical(ZERO_ADDRESS, "Test", "desc", 1, "none")
      ).to.be.revertedWithCustomError(registry, "ZeroAddress");
    });

    it("should revert duplicate registration", async function () {
      await registry.registerOptical(opticalAddr1.address, "AntiSnipe", "desc", 2, "team");
      await expect(
        registry.registerOptical(opticalAddr1.address, "AntiSnipe2", "desc2", 3, "team2")
      ).to.be.revertedWithCustomError(registry, "AlreadyRegistered");
    });

    it("should revert non-admin registration", async function () {
      await expect(
        registry.connect(alice).registerOptical(opticalAddr1.address, "Test", "desc", 1, "none")
      ).to.be.revertedWithCustomError(registry, "MissingRole");
    });
  });

  describe("Deregister optical", function () {
    it("should deregister an optical", async function () {
      await registry.registerOptical(opticalAddr1.address, "AntiSnipe", "desc", 2, "team");
      expect(await registry.isRegistered(opticalAddr1.address)).to.be.true;

      await expect(
        registry.deregisterOptical(opticalAddr1.address)
      ).to.emit(registry, "OpticalDeregistered");

      expect(await registry.isRegistered(opticalAddr1.address)).to.be.false;
      expect(await registry.isApproved(opticalAddr1.address)).to.be.false;
    });

    it("should revert deregistering unregistered optical", async function () {
      await expect(
        registry.deregisterOptical(opticalAddr1.address)
      ).to.be.revertedWithCustomError(registry, "NotRegistered");
    });
  });

  describe("Queries", function () {
    it("should return false for unregistered optical", async function () {
      expect(await registry.isRegistered(opticalAddr1.address)).to.be.false;
    });

    it("should paginate getAllOpticals correctly", async function () {
      await registry.registerOptical(opticalAddr1.address, "A", "desc", 1, "t");
      await registry.registerOptical(opticalAddr2.address, "B", "desc", 2, "t");

      expect(await registry.getOpticalCount()).to.equal(2);

      const page1 = await registry.getAllOpticals(0, 1);
      expect(page1.length).to.equal(1);
      expect(page1[0]).to.equal(opticalAddr1.address);

      const page2 = await registry.getAllOpticals(1, 1);
      expect(page2.length).to.equal(1);
      expect(page2[0]).to.equal(opticalAddr2.address);

      const beyondEnd = await registry.getAllOpticals(5, 10);
      expect(beyondEnd.length).to.equal(0);
    });
  });

  describe("Audit remediation", function () {
    it("does not duplicate an optical when approval is renewed", async function () {
      await registry.registerOptical(opticalAddr1.address, "A", "desc", 1, "t");
      await registry.deregisterOptical(opticalAddr1.address);
      await registry.registerOptical(opticalAddr1.address, "A2", "desc", 2, "t");
      expect(await registry.getOpticalCount()).to.equal(1);
      expect(await registry.getAllOpticals(0, ethers.MaxUint256)).to.deep.equal([opticalAddr1.address]);
      expect(await registry.approvedCodeHash(opticalAddr1.address)).to.equal(ethers.keccak256(await ethers.provider.getCode(opticalAddr1.address)));
    });
    it("rejects EOA and invalid risk review registrations", async function () {
      await expect(registry.registerOptical(alice.address, "EOA", "desc", 1, "t")).to.be.revertedWithCustomError(registry, "InvalidOptical");
      await expect(registry.registerOptical(opticalAddr1.address, "A", "desc", 0, "t")).to.be.revertedWithCustomError(registry, "InvalidOptical");
      await expect(registry.registerOptical(opticalAddr1.address, "A", "desc", 6, "t")).to.be.revertedWithCustomError(registry, "InvalidOptical");
    });
  });

  // Helper to get current block timestamp
  async function getTimestamp() {
    const block = await ethers.provider.getBlock("latest");
    return block.timestamp;
  }
});
