const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("PoolRegistry", function () {
  let registry, registryProxy, eventEmitter;
  let deployer, alice, bob, factory, pool1, pool2, token1, token2;

  before(async function () {
    [deployer, alice, bob, factory, pool1, pool2, token1, token2] = await ethers.getSigners();
  });

  beforeEach(async function () {
    // Deploy mock EventEmitter
    const MockEE = await ethers.getContractFactory("MockEventEmitter");
    eventEmitter = await MockEE.deploy();
    await eventEmitter.waitForDeployment();

    // Deploy PoolRegistry implementation
    const Registry = await ethers.getContractFactory("PoolRegistry");
    const impl = await Registry.deploy();
    await impl.waitForDeployment();

    // Deploy proxy
    const initData = Registry.interface.encodeFunctionData("initialize", [
      await eventEmitter.getAddress(),
      deployer.address,
    ]);
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const proxy = await Proxy.deploy(await impl.getAddress(), initData);
    await proxy.waitForDeployment();

    registryProxy = Registry.attach(await proxy.getAddress());

    // Grant FACTORY_ROLE to factory signer
    const FACTORY_ROLE = ethers.keccak256(ethers.toUtf8Bytes("FACTORY_ROLE"));
    await registryProxy.grantRole(FACTORY_ROLE, factory.address);
  });

  describe("initialization", function () {
    it("should grant DEFAULT_ADMIN_ROLE to admin", async function () {
      expect(await registryProxy.hasRole(ethers.ZeroHash, deployer.address)).to.be.true;
    });

    it("should revert on double initialization", async function () {
      await expect(
        registryProxy.initialize(await eventEmitter.getAddress(), deployer.address)
      ).to.be.revertedWithCustomError(registryProxy, "AlreadyInitialized");
    });

    it("should start with zero pool count", async function () {
      expect(await registryProxy.getPoolCount()).to.equal(0);
    });
  });

  describe("register", function () {
    it("should register a pool from factory", async function () {
      await registryProxy.connect(factory).register(
        pool1.address, token1.address, alice.address, ethers.ZeroAddress, 1
      );
      expect(await registryProxy.getPoolByToken(token1.address)).to.equal(pool1.address);
      expect(await registryProxy.getPoolCount()).to.equal(1);
    });

    it("should emit PoolRegistered event", async function () {
      await expect(
        registryProxy.connect(factory).register(
          pool1.address, token1.address, alice.address, ethers.ZeroAddress, 1
        )
      ).to.emit(registryProxy, "PoolRegistered")
        .withArgs(pool1.address, token1.address, alice.address, ethers.ZeroAddress, 1, anyUint);
    });

    it("should store correct metadata", async function () {
      await registryProxy.connect(factory).register(
        pool1.address, token1.address, alice.address, bob.address, 1
      );
      const meta = await registryProxy.getPoolMetadata(pool1.address);
      expect(meta.creator).to.equal(alice.address);
      expect(meta.token).to.equal(token1.address);
      expect(meta.optical).to.equal(bob.address);
      expect(meta.nftId).to.equal(1);
      expect(meta.createdAt).to.be.gt(0);
      expect(meta.createdBlock).to.be.gt(0);
    });

    it("should track pools by creator", async function () {
      await registryProxy.connect(factory).register(
        pool1.address, token1.address, alice.address, ethers.ZeroAddress, 1
      );
      await registryProxy.connect(factory).register(
        pool2.address, token2.address, alice.address, ethers.ZeroAddress, 2
      );
      const pools = await registryProxy.getPoolsByCreator(alice.address);
      expect(pools.length).to.equal(2);
      expect(pools[0]).to.equal(pool1.address);
      expect(pools[1]).to.equal(pool2.address);
    });

    it("should set nftId correctly", async function () {
      await registryProxy.connect(factory).register(
        pool1.address, token1.address, alice.address, ethers.ZeroAddress, 42
      );
      expect(await registryProxy.getNftIdByPool(pool1.address)).to.equal(42);
    });

    it("should mark pool as registered", async function () {
      await registryProxy.connect(factory).register(
        pool1.address, token1.address, alice.address, ethers.ZeroAddress, 1
      );
      expect(await registryProxy.isRegisteredPool(pool1.address)).to.be.true;
      expect(await registryProxy.isRegisteredPool(bob.address)).to.be.false;
    });

    it("should revert duplicate token registration", async function () {
      await registryProxy.connect(factory).register(
        pool1.address, token1.address, alice.address, ethers.ZeroAddress, 1
      );
      await expect(
        registryProxy.connect(factory).register(
          pool2.address, token1.address, bob.address, ethers.ZeroAddress, 2
        )
      ).to.be.revertedWithCustomError(registryProxy, "DuplicateToken");
    });

    it("should revert from non-factory", async function () {
      await expect(
        registryProxy.connect(alice).register(
          pool1.address, token1.address, alice.address, ethers.ZeroAddress, 1
        )
      ).to.be.revertedWithCustomError(registryProxy, "MissingRole");
    });

    it("should revert with zero pool address", async function () {
      await expect(
        registryProxy.connect(factory).register(
          ethers.ZeroAddress, token1.address, alice.address, ethers.ZeroAddress, 1
        )
      ).to.be.revertedWithCustomError(registryProxy, "ZeroAddress");
    });
  });

  describe("queries", function () {
    it("should return zero address for unregistered token", async function () {
      expect(await registryProxy.getPoolByToken(token1.address)).to.equal(ethers.ZeroAddress);
    });

    it("should return empty array for creator with no pools", async function () {
      const pools = await registryProxy.getPoolsByCreator(alice.address);
      expect(pools.length).to.equal(0);
    });

    it("should paginate getAllPools correctly", async function () {
      await registryProxy.connect(factory).register(
        pool1.address, token1.address, alice.address, ethers.ZeroAddress, 1
      );
      await registryProxy.connect(factory).register(
        pool2.address, token2.address, bob.address, ethers.ZeroAddress, 2
      );

      // Get first page
      const page1 = await registryProxy.getAllPools(0, 1);
      expect(page1.length).to.equal(1);
      expect(page1[0]).to.equal(pool1.address);

      // Get second page
      const page2 = await registryProxy.getAllPools(1, 1);
      expect(page2.length).to.equal(1);
      expect(page2[0]).to.equal(pool2.address);

      // Get all
      const all = await registryProxy.getAllPools(0, 10);
      expect(all.length).to.equal(2);

      // Offset beyond length
      const empty = await registryProxy.getAllPools(5, 10);
      expect(empty.length).to.equal(0);
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const V2 = await ethers.getContractFactory("PoolRegistry");
      const implV2 = await V2.deploy();
      await registryProxy.upgradeToAndCall(await implV2.getAddress(), "0x");
      // Pool count should be preserved
      expect(await registryProxy.getPoolCount()).to.equal(0);
    });

    it("should revert upgrade by non-admin", async function () {
      const V2 = await ethers.getContractFactory("PoolRegistry");
      const implV2 = await V2.deploy();
      await expect(
        registryProxy.connect(alice).upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(registryProxy, "MissingRole");
    });
  });
});

const anyUint = () => true;
