const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("EventEmitter", function () {
  let eventEmitter, emitterProxy;
  let deployer, alice, authorizedContract, unauthorizedAddr;

  before(async function () {
    [deployer, alice, authorizedContract, unauthorizedAddr] = await ethers.getSigners();
  });

  beforeEach(async function () {
    // Deploy EventEmitter implementation
    const EventEmitter = await ethers.getContractFactory("EventEmitter");
    const impl = await EventEmitter.deploy();
    await impl.waitForDeployment();

    // Deploy proxy
    const initData = EventEmitter.interface.encodeFunctionData("initialize", [
      deployer.address,
    ]);
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxy = await Proxy.deploy(await impl.getAddress(), initData);
    await proxy.waitForDeployment();

    emitterProxy = EventEmitter.attach(await proxy.getAddress());

    // Authorize a contract for emitting
    await emitterProxy.setAuthorizedEmitter(authorizedContract.address, true);
    // This fixture signer deliberately represents each canonical typed source.
    for (const fragment of emitterProxy.interface.fragments) {
      if (fragment.type === "function" && fragment.name.startsWith("emit") && !fragment.name.startsWith("emitEventLog")) {
        await emitterProxy.setTypedEmitter(authorizedContract.address, fragment.selector, true);
      }
    }
  });

  describe("initialization", function () {
    it("should grant DEFAULT_ADMIN_ROLE to admin", async function () {
      expect(await emitterProxy.hasRole(ethers.ZeroHash, deployer.address)).to.be.true;
    });

    it("should revert on double initialization", async function () {
      await expect(
        emitterProxy.initialize(deployer.address)
      ).to.be.revertedWithCustomError(emitterProxy, "AlreadyInitialized");
    });

    it("should revert with zero address admin", async function () {
      const EventEmitter = await ethers.getContractFactory("EventEmitter");
      const impl2 = await EventEmitter.deploy();
      const Proxy = await ethers.getContractFactory("UUPSProxy");
      const initData = EventEmitter.interface.encodeFunctionData("initialize", [
        ethers.ZeroAddress,
      ]);
      await expect(Proxy.deploy(await impl2.getAddress(), initData)).to.be.reverted;
    });
  });

  describe("emitter management", function () {
    it("should register authorized emitter", async function () {
      expect(await emitterProxy.isAuthorizedEmitter(authorizedContract.address)).to.be.true;
    });

    it("should deregister emitter", async function () {
      await emitterProxy.setAuthorizedEmitter(authorizedContract.address, false);
      expect(await emitterProxy.isAuthorizedEmitter(authorizedContract.address)).to.be.false;
    });

    it("should revert registration by non-admin", async function () {
      await expect(
        emitterProxy.connect(alice).setAuthorizedEmitter(alice.address, true)
      ).to.be.revertedWithCustomError(emitterProxy, "MissingRole");
    });

    it("should revert registration of zero address", async function () {
      await expect(
        emitterProxy.setAuthorizedEmitter(ethers.ZeroAddress, true)
      ).to.be.revertedWithCustomError(emitterProxy, "Unauthorized");
    });
  });

  describe("emitMarketCreated", function () {
    it("should emit MarketCreated event from authorized emitter", async function () {
      const poolId = ethers.id("pool1");
      const token = alice.address;
      const creator = deployer.address;
      const pool = authorizedContract.address;
      const optical = ethers.ZeroAddress;

      await expect(
        emitterProxy.connect(authorizedContract).emitMarketCreated(poolId, token, creator, pool, optical)
      ).to.emit(emitterProxy, "MarketCreated")
        .withArgs(poolId, token, creator, pool, optical, anyUint, anyUint);
    });

    it("should revert from unauthorized address", async function () {
      const poolId = ethers.id("pool1");
      await expect(
        emitterProxy.connect(unauthorizedAddr).emitMarketCreated(poolId, alice.address, deployer.address, authorizedContract.address, ethers.ZeroAddress)
      ).to.be.revertedWithCustomError(emitterProxy, "Unauthorized");
    });
  });

  describe("emitSwap", function () {
    it("should emit Swap event with correct args", async function () {
      const poolId = ethers.id("pool1");
      const sender = alice.address;
      const amountIn = ethers.parseUnits("100", 6);
      const amountOut = ethers.parseUnits("9900", 6);
      const fee = ethers.parseUnits("0.3", 6);
      const price = ethers.parseUnits("0.00001", 6);

      await expect(
        emitterProxy.connect(authorizedContract).emitSwap(poolId, sender, true, amountIn, amountOut, fee, price)
      ).to.emit(emitterProxy, "Swap")
        .withArgs(poolId, sender, true, amountIn, amountOut, fee, price, anyUint, anyUint);
    });
  });

  describe("emitFeeRecorded", function () {
    it("should emit FeeRecorded event", async function () {
      const poolId = ethers.id("pool1");
      const feeAmount = ethers.parseUnits("1", 6);
      const protocolCut = ethers.parseUnits("0.1", 6);
      const poolCut = ethers.parseUnits("0.9", 6);

      await expect(
        emitterProxy.connect(authorizedContract).emitFeeRecorded(poolId, feeAmount, protocolCut, poolCut)
      ).to.emit(emitterProxy, "FeeRecorded")
        .withArgs(poolId, feeAmount, protocolCut, poolCut, anyUint, anyUint);
    });
  });

  describe("emitFeeDistributed", function () {
    it("should emit FeeDistributed event", async function () {
      const poolId = ethers.id("pool1");
      await expect(
        emitterProxy.connect(authorizedContract).emitFeeDistributed(poolId, 1, 0, ethers.parseUnits("5", 6), alice.address)
      ).to.emit(emitterProxy, "FeeDistributed")
        .withArgs(poolId, 1, 0, ethers.parseUnits("5", 6), alice.address, anyUint, anyUint);
    });
  });

  describe("emitFeeStrategyChanged", function () {
    it("should emit FeeStrategyChanged event", async function () {
      const poolId = ethers.id("pool1");
      await expect(
        emitterProxy.connect(authorizedContract).emitFeeStrategyChanged(poolId, 1, 0, 2)
      ).to.emit(emitterProxy, "FeeStrategyChanged")
        .withArgs(poolId, 1, 0, 2, anyUint, anyUint);
    });
  });

  describe("emitPoolStateUpdated", function () {
    it("should emit PoolStateUpdated event", async function () {
      const poolId = ethers.id("pool1");
      const virtualR = ethers.parseUnits("10000", 6);
      const realR = ethers.parseUnits("500", 6);
      const tokenR = ethers.parseUnits("900000000", 6);
      const price = ethers.parseUnits("0.000012", 6);

      await expect(
        emitterProxy.connect(authorizedContract).emitPoolStateUpdated(poolId, virtualR, realR, tokenR, price)
      ).to.emit(emitterProxy, "PoolStateUpdated")
        .withArgs(poolId, virtualR, realR, tokenR, price, anyUint, anyUint);
    });
  });

  describe("emitOpticalExecuted", function () {
    it("should emit OpticalExecuted event", async function () {
      const poolId = ethers.id("pool1");
      const optical = alice.address;
      const hookName = "beforeSwap";
      const data = "0x1234";

      await expect(
        emitterProxy.connect(authorizedContract).emitOpticalExecuted(poolId, optical, hookName, data)
      ).to.emit(emitterProxy, "OpticalExecuted");
    });
  });

  describe("emitConfigUpdated", function () {
    it("should emit ConfigUpdated event", async function () {
      const key = ethers.id("baseFeeBps");
      await expect(
        emitterProxy.connect(authorizedContract).emitConfigUpdated(key, 30, 50)
      ).to.emit(emitterProxy, "ConfigUpdated")
        .withArgs(key, 30, 50, anyUint, anyUint);
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const V2 = await ethers.getContractFactory("EventEmitter");
      const implV2 = await V2.deploy();
      await emitterProxy.upgradeToAndCall(await implV2.getAddress(), "0x");
      // Verify emitter authorization persists
      expect(await emitterProxy.isAuthorizedEmitter(authorizedContract.address)).to.be.true;
    });

    it("should revert upgrade by non-admin", async function () {
      const V2 = await ethers.getContractFactory("EventEmitter");
      const implV2 = await V2.deploy();
      await expect(
        emitterProxy.connect(alice).upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(emitterProxy, "MissingRole");
    });
  });
});

// Helper for matching any uint in event args
const anyUint = () => true;
