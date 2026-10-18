const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("BeaconProxy + UpgradeableBeacon", function () {
  let deployer, alice;
  let implV1, implV2, beacon;

  before(async function () {
    [deployer, alice] = await ethers.getSigners();
  });

  beforeEach(async function () {
    const V1 = await ethers.getContractFactory("MockImplementation");
    implV1 = await V1.deploy();
    await implV1.waitForDeployment();

    const V2 = await ethers.getContractFactory("MockImplementationV2");
    implV2 = await V2.deploy();
    await implV2.waitForDeployment();

    const Beacon = await ethers.getContractFactory("UpgradeableBeacon");
    beacon = await Beacon.deploy(await implV1.getAddress(), deployer.address);
    await beacon.waitForDeployment();
  });

  describe("UpgradeableBeacon", function () {
    it("should initialize with correct implementation and owner", async function () {
      expect(await beacon.implementation()).to.equal(await implV1.getAddress());
      expect(await beacon.owner()).to.equal(deployer.address);
    });

    it("should allow owner to upgrade", async function () {
      await beacon.upgradeTo(await implV2.getAddress());
      expect(await beacon.implementation()).to.equal(await implV2.getAddress());
    });

    it("should revert upgrade from non-owner", async function () {
      await expect(
        beacon.connect(alice).upgradeTo(await implV2.getAddress())
      ).to.be.revertedWithCustomError(beacon, "Unauthorized");
    });

    it("should emit Upgraded event", async function () {
      await expect(beacon.upgradeTo(await implV2.getAddress()))
        .to.emit(beacon, "Upgraded")
        .withArgs(await implV2.getAddress());
    });

    it("should revert on zero-address implementation", async function () {
      await expect(
        beacon.upgradeTo(deployer.address)
      ).to.be.revertedWithCustomError(beacon, "InvalidImplementation");
    });

    it("should transfer ownership", async function () {
      await beacon.transferOwnership(alice.address);
      expect(await beacon.owner()).to.equal(deployer.address);
      await beacon.connect(alice).acceptOwnership();
      expect(await beacon.owner()).to.equal(alice.address);
    });
  });

  describe("BeaconProxy", function () {
    it("should delegate calls to beacon implementation", async function () {
      const BProxy = await ethers.getContractFactory("BeaconProxy");
      const proxy = await BProxy.deploy(await beacon.getAddress(), "0x");

      const V1 = await ethers.getContractFactory("MockImplementation");
      const proxied = V1.attach(await proxy.getAddress());

      await proxied.setValue(42);
      expect(await proxied.getValue()).to.equal(42);
    });

    it("should support initialization data", async function () {
      const V1 = await ethers.getContractFactory("MockImplementation");
      const initData = V1.interface.encodeFunctionData("setValue", [123]);

      const BProxy = await ethers.getContractFactory("BeaconProxy");
      const proxy = await BProxy.deploy(await beacon.getAddress(), initData);

      const proxied = V1.attach(await proxy.getAddress());
      expect(await proxied.getValue()).to.equal(123);
    });

    it("should update all proxies when beacon is upgraded", async function () {
      const BProxy = await ethers.getContractFactory("BeaconProxy");
      const proxy1 = await BProxy.deploy(await beacon.getAddress(), "0x");
      const proxy2 = await BProxy.deploy(await beacon.getAddress(), "0x");

      const V1 = await ethers.getContractFactory("MockImplementation");
      const p1 = V1.attach(await proxy1.getAddress());
      const p2 = V1.attach(await proxy2.getAddress());

      await p1.setValue(10);
      await p2.setValue(20);

      // Upgrade beacon
      await beacon.upgradeTo(await implV2.getAddress());

      const V2 = await ethers.getContractFactory("MockImplementationV2");
      const p1v2 = V2.attach(await proxy1.getAddress());
      const p2v2 = V2.attach(await proxy2.getAddress());

      // Storage preserved
      expect(await p1v2.getValue()).to.equal(10);
      expect(await p2v2.getValue()).to.equal(20);

      // New function available
      expect(await p1v2.version()).to.equal(2);
      expect(await p2v2.version()).to.equal(2);
    });

    it("should have independent storage per proxy", async function () {
      const BProxy = await ethers.getContractFactory("BeaconProxy");
      const proxy1 = await BProxy.deploy(await beacon.getAddress(), "0x");
      const proxy2 = await BProxy.deploy(await beacon.getAddress(), "0x");

      const V1 = await ethers.getContractFactory("MockImplementation");
      const p1 = V1.attach(await proxy1.getAddress());
      const p2 = V1.attach(await proxy2.getAddress());

      await p1.setValue(111);
      await p2.setValue(222);

      expect(await p1.getValue()).to.equal(111);
      expect(await p2.getValue()).to.equal(222);
    });

    it("should not share storage with implementation", async function () {
      const BProxy = await ethers.getContractFactory("BeaconProxy");
      const proxy = await BProxy.deploy(await beacon.getAddress(), "0x");

      const V1 = await ethers.getContractFactory("MockImplementation");
      const proxied = V1.attach(await proxy.getAddress());
      await proxied.setValue(999);

      expect(await implV1.getValue()).to.equal(0);
    });
  });
});
