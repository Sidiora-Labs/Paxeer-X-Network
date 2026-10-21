const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("ProtocolConfig", function () {
  let config, configProxy, eventEmitter, usdl;
  let deployer, alice;

  before(async function () {
    [deployer, alice] = await ethers.getSigners();
  });

  beforeEach(async function () {
    // Deploy mock USDL
    const MockERC20 = await ethers.getContractFactory("MockERC20");
    usdl = await MockERC20.deploy("USD Ledger", "USDL", 6);
    await usdl.waitForDeployment();

    // Deploy mock EventEmitter
    const MockEE = await ethers.getContractFactory("MockEventEmitter");
    eventEmitter = await MockEE.deploy();
    await eventEmitter.waitForDeployment();

    // Deploy ProtocolConfig implementation
    const Config = await ethers.getContractFactory("ProtocolConfig");
    const impl = await Config.deploy();
    await impl.waitForDeployment();

    // Deploy proxy
    const initData = Config.interface.encodeFunctionData("initialize", [
      await usdl.getAddress(),
      await eventEmitter.getAddress(),
      deployer.address,
    ]);
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxy = await Proxy.deploy(await impl.getAddress(), initData);
    await proxy.waitForDeployment();

    configProxy = Config.attach(await proxy.getAddress());
  });

  describe("initialization", function () {
    it("should set correct defaults", async function () {
      expect(await configProxy.usdlAddress()).to.equal(await usdl.getAddress());
      expect(await configProxy.virtualUsdlDefault()).to.equal(ethers.parseUnits("10000", 6));
      expect(await configProxy.virtualTokenDefault()).to.equal(ethers.parseUnits("1000000000", 6));
      expect(await configProxy.minFeeBps()).to.equal(10);
      expect(await configProxy.maxFeeBps()).to.equal(300);
      expect(await configProxy.baseFeeBps()).to.equal(30);
      expect(await configProxy.protocolFeeBps()).to.equal(1000);
      expect(await configProxy.feeDecayRate()).to.equal(500);
      expect(await configProxy.volatilityWeight()).to.equal(100);
      expect(await configProxy.concentrationWeight()).to.equal(0);
      expect(await configProxy.creationFee()).to.equal(ethers.parseUnits("1", 6));
    });

    it("should grant DEFAULT_ADMIN_ROLE to deployer", async function () {
      expect(await configProxy.hasRole(ethers.ZeroHash, deployer.address)).to.be.true;
    });

    it("should revert on double initialization", async function () {
      await expect(
        configProxy.initialize(await usdl.getAddress(), await eventEmitter.getAddress(), deployer.address)
      ).to.be.revertedWithCustomError(configProxy, "AlreadyInitialized");
    });

    it("should revert with zero USDL address", async function () {
      const Config = await ethers.getContractFactory("ProtocolConfig");
      const impl2 = await Config.deploy();
      const Proxy = await ethers.getContractFactory("UUPSProxy");
      const initData = Config.interface.encodeFunctionData("initialize", [
        ethers.ZeroAddress, await eventEmitter.getAddress(), deployer.address,
      ]);
      await expect(Proxy.deploy(await impl2.getAddress(), initData)).to.be.reverted;
    });
  });

  describe("setBaseFeeBps", function () {
    it("should update base fee", async function () {
      await configProxy.setBaseFeeBps(50);
      expect(await configProxy.baseFeeBps()).to.equal(50);
    });

    it("should revert if above maxFeeBps", async function () {
      await expect(configProxy.setBaseFeeBps(301)).to.be.revertedWithCustomError(
        configProxy, "FeeOutOfRange"
      );
    });

    it("should revert for non-admin", async function () {
      await expect(
        configProxy.connect(alice).setBaseFeeBps(50)
      ).to.be.revertedWithCustomError(configProxy, "MissingRole");
    });

    it("should emit via EventEmitter", async function () {
      await configProxy.setBaseFeeBps(50);
      expect(await eventEmitter.configUpdateCount()).to.equal(1);
      expect(await eventEmitter.lastConfigNewValue()).to.equal(50);
    });
  });

  describe("setProtocolFeeBps", function () {
    it("should update protocol fee", async function () {
      await configProxy.setProtocolFeeBps(2000);
      expect(await configProxy.protocolFeeBps()).to.equal(2000);
    });

    it("should revert if above 50%", async function () {
      await expect(configProxy.setProtocolFeeBps(5001)).to.be.revertedWithCustomError(
        configProxy, "FeeOutOfRange"
      );
    });

    it("should revert for non-admin", async function () {
      await expect(
        configProxy.connect(alice).setProtocolFeeBps(2000)
      ).to.be.revertedWithCustomError(configProxy, "MissingRole");
    });
  });

  describe("setCreationFee", function () {
    it("should update creation fee", async function () {
      await configProxy.setCreationFee(ethers.parseUnits("200", 6));
      expect(await configProxy.creationFee()).to.equal(ethers.parseUnits("200", 6));
    });

    it("should revert for non-admin", async function () {
      await expect(
        configProxy.connect(alice).setCreationFee(ethers.parseUnits("200", 6))
      ).to.be.revertedWithCustomError(configProxy, "MissingRole");
    });
  });

  describe("setFeeWeights", function () {
    it("should update all fee weights", async function () {
      await configProxy.setFeeWeights(200, 150, 0);
      expect(await configProxy.feeDecayRate()).to.equal(200);
      expect(await configProxy.volatilityWeight()).to.equal(150);
      expect(await configProxy.concentrationWeight()).to.equal(0);
    });

    it("should emit three config updates", async function () {
      await configProxy.setFeeWeights(200, 150, 0);
      expect(await eventEmitter.configUpdateCount()).to.equal(3);
    });
  });

  describe("setVirtualDefaults", function () {
    it("should update virtual defaults", async function () {
      await configProxy.setVirtualDefaults(ethers.parseUnits("20000", 6), ethers.parseUnits("500000000", 6));
      expect(await configProxy.virtualUsdlDefault()).to.equal(ethers.parseUnits("20000", 6));
      expect(await configProxy.virtualTokenDefault()).to.equal(ethers.parseUnits("500000000", 6));
    });
  });

  describe("audit parameter boundaries", function () {
    it("rejects overflowing weights and accepts bounded maxima", async function () {
      await expect(configProxy.setFeeWeights(ethers.MaxUint256, 0, 0)).to.be.revertedWithCustomError(configProxy, "FeeOutOfRange");
      await configProxy.setFeeWeights(10000, 10000, 0);
      await expect(configProxy.setFeeWeights(0, 0, 1)).to.be.revertedWithCustomError(configProxy, "FeeOutOfRange");
    });
    it("rejects zero and unsafe reserve defaults", async function () {
      await expect(configProxy.setVirtualDefaults(0, 1)).to.be.revertedWithCustomError(configProxy, "FeeOutOfRange");
      await expect(configProxy.setVirtualDefaults(1, 1n << 112n)).to.be.revertedWithCustomError(configProxy, "FeeOutOfRange");
    });
    it("rejects fees below configured minimum", async function () {
      await expect(configProxy.setBaseFeeBps(9)).to.be.revertedWithCustomError(configProxy, "FeeOutOfRange");
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const V2 = await ethers.getContractFactory("ProtocolConfig");
      const implV2 = await V2.deploy();
      await configProxy.upgradeToAndCall(await implV2.getAddress(), "0x");
      // Storage should be preserved
      expect(await configProxy.baseFeeBps()).to.equal(30);
    });

    it("should revert upgrade by non-admin", async function () {
      const V2 = await ethers.getContractFactory("ProtocolConfig");
      const implV2 = await V2.deploy();
      await expect(
        configProxy.connect(alice).upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(configProxy, "MissingRole");
    });
  });
});
