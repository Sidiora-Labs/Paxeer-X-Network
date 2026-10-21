const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("UUPSUpgradeable", function () {
  let deployer, alice;
  let implV1, implV2, proxy, proxied;

  before(async function () {
    [deployer, alice] = await ethers.getSigners();
  });

  beforeEach(async function () {
    const V1 = await ethers.getContractFactory("MockUUPS");
    implV1 = await V1.deploy();
    await implV1.waitForDeployment();

    const V2 = await ethers.getContractFactory("MockUUPSV2");
    implV2 = await V2.deploy();
    await implV2.waitForDeployment();

    const initData = implV1.interface.encodeFunctionData("initialize", [deployer.address]);
    const ProxyFactory = await ethers.getContractFactory("UUPSProxy");
    proxy = await ProxyFactory.deploy(await implV1.getAddress(), initData);
    await proxy.waitForDeployment();

    proxied = V1.attach(await proxy.getAddress());
  });

  describe("initialization", function () {
    it("should initialize through proxy", async function () {
      expect(await proxied.upgrader()).to.equal(deployer.address);
    });

    it("should set version to 1", async function () {
      expect(await proxied.version()).to.equal(1);
    });
  });

  describe("upgradeToAndCall", function () {
    it("should upgrade to V2", async function () {
      await proxied.upgradeToAndCall(await implV2.getAddress(), "0x");
      const V2 = await ethers.getContractFactory("MockUUPSV2");
      const proxiedV2 = V2.attach(await proxy.getAddress());
      expect(await proxiedV2.version()).to.equal(2);
    });

    it("should preserve storage after upgrade", async function () {
      await proxied.setValue(42);
      expect(await proxied.value()).to.equal(42);

      await proxied.upgradeToAndCall(await implV2.getAddress(), "0x");
      const V2 = await ethers.getContractFactory("MockUUPSV2");
      const proxiedV2 = V2.attach(await proxy.getAddress());

      expect(await proxiedV2.value()).to.equal(42);
      expect(await proxiedV2.upgrader()).to.equal(deployer.address);
    });

    it("should revert for unauthorized caller", async function () {
      await expect(
        proxied.connect(alice).upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(proxied, "UnauthorizedUpgrade");
    });

    it("should revert for non-UUPS implementation", async function () {
      const NonUUPS = await ethers.getContractFactory("MockImplementation");
      const nonUups = await NonUUPS.deploy();

      await expect(
        proxied.upgradeToAndCall(await nonUups.getAddress(), "0x")
      ).to.be.revertedWithCustomError(proxied, "InvalidImplementation");
    });

    it("should execute initialization data during upgrade", async function () {
      const V2 = await ethers.getContractFactory("MockUUPSV2");
      const data = V2.interface.encodeFunctionData("setExtra", [999]);

      await proxied.upgradeToAndCall(await implV2.getAddress(), data);
      const proxiedV2 = V2.attach(await proxy.getAddress());
      expect(await proxiedV2.extra()).to.equal(999);
    });
  });

  describe("proxiableUUID", function () {
    it("should return implementation slot when called on implementation directly", async function () {
      const implSlot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc";
      expect(await implV1.proxiableUUID()).to.equal(implSlot);
    });

    it("should revert when called through proxy", async function () {
      await expect(proxied.proxiableUUID()).to.be.revertedWithCustomError(
        proxied, "UUPSNotThroughActiveProxy"
      );
    });
  });

  describe("direct call protection", function () {
    it("should revert upgradeToAndCall on implementation directly", async function () {
      await expect(
        implV1.upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(implV1, "UUPSNotThroughProxy");
    });
  });
});
