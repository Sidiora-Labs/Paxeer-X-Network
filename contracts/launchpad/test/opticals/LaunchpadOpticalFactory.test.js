const { expect } = require("chai");
const { ethers } = require("hardhat");
const {
  ZERO_ADDRESS,
  HookFlags,
  PROTOCOL_FEE_BPS,
  BPS_DENOMINATOR,
  ONE_DAY,
  p6,
} = require("../helpers/constants");

describe("LaunchpadOpticalFactory", function () {
  let factory, factoryProxy, factoryAddr;
  let accumulatorProxy, configProxy, treasuryProxy, registryProxy, eventEmitter;
  let usdl, token;
  let deployer, creator, teamWallet1, teamWallet2, randomUser, teamClaimAddr;

  const CLIFF_DURATION = 30n * ONE_DAY;
  const VESTING_DURATION = 180n * ONE_DAY;
  const CAPITAL_RAISE_BPS = 500n;
  const CAPITAL_RAISE_DURATION = 90n * ONE_DAY;

  before(async function () {
    [deployer, creator, teamWallet1, teamWallet2, randomUser, teamClaimAddr] =
      await ethers.getSigners();
  });

  beforeEach(async function () {
    // Deploy mocks
    const MockERC20 = await ethers.getContractFactory("MockERC20");
    usdl = await MockERC20.deploy("USD Ledger", "USDL", 6);
    await usdl.waitForDeployment();

    token = await MockERC20.deploy("Test Token", "TEST", 6);
    await token.waitForDeployment();

    const MockEE = await ethers.getContractFactory("MockEventEmitter");
    eventEmitter = await MockEE.deploy();
    await eventEmitter.waitForDeployment();

    // ProtocolConfig
    const Config = await ethers.getContractFactory("ProtocolConfig");
    const configImpl = await Config.deploy();
    await configImpl.waitForDeployment();
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    let proxy = await Proxy.deploy(
      await configImpl.getAddress(),
      Config.interface.encodeFunctionData("initialize", [
        await usdl.getAddress(), await eventEmitter.getAddress(), deployer.address,
      ])
    );
    await proxy.waitForDeployment();
    configProxy = Config.attach(await proxy.getAddress());

    // Treasury
    const Treasury = await ethers.getContractFactory("Treasury");
    const treasuryImpl = await Treasury.deploy();
    await treasuryImpl.waitForDeployment();
    proxy = await Proxy.deploy(
      await treasuryImpl.getAddress(),
      Treasury.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ])
    );
    await proxy.waitForDeployment();
    treasuryProxy = Treasury.attach(await proxy.getAddress());

    // PoolRegistry
    const Registry = await ethers.getContractFactory("PoolRegistry");
    const registryImpl = await Registry.deploy();
    await registryImpl.waitForDeployment();
    proxy = await Proxy.deploy(
      await registryImpl.getAddress(),
      Registry.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ])
    );
    await proxy.waitForDeployment();
    registryProxy = Registry.attach(await proxy.getAddress());

    // FeeAccumulator
    const Acc = await ethers.getContractFactory("FeeAccumulator");
    const accImpl = await Acc.deploy();
    await accImpl.waitForDeployment();
    proxy = await Proxy.deploy(
      await accImpl.getAddress(),
      Acc.interface.encodeFunctionData("initialize", [
        await configProxy.getAddress(),
        await treasuryProxy.getAddress(),
        await registryProxy.getAddress(),
        await eventEmitter.getAddress(),
        await usdl.getAddress(),
        deployer.address,
      ])
    );
    await proxy.waitForDeployment();
    accumulatorProxy = Acc.attach(await proxy.getAddress());

    // LaunchpadOpticalFactory (UUPS proxy)
    const LOF = await ethers.getContractFactory("LaunchpadOpticalFactory");
    const lofImpl = await LOF.deploy();
    await lofImpl.waitForDeployment();
    proxy = await Proxy.deploy(
      await lofImpl.getAddress(),
      LOF.interface.encodeFunctionData("initialize", [
        await registryProxy.getAddress(),
        await accumulatorProxy.getAddress(),
        ZERO_ADDRESS, // no optical registry for tests
        deployer.address,
      ])
    );
    await proxy.waitForDeployment();
    factoryProxy = LOF.attach(await proxy.getAddress());
    factoryAddr = await proxy.getAddress();

    // Grant OPTICAL_GRANTER_ROLE to factory on FeeAccumulator
    const OPTICAL_GRANTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("OPTICAL_GRANTER_ROLE"));
    await accumulatorProxy.grantRole(OPTICAL_GRANTER_ROLE, factoryAddr);

    // Grant DEPOSITOR_ROLE to FeeAccumulator on Treasury
    const DEPOSITOR_ROLE = ethers.keccak256(ethers.toUtf8Bytes("DEPOSITOR_ROLE"));
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await accumulatorProxy.getAddress());
  });

  // ============ CREATION ============

  describe("createLaunchpadOptical", function () {
    it("should deploy a new LaunchpadOptical and return its address", async function () {
      const tx = await factoryProxy.connect(creator).createLaunchpadOptical(
        [teamWallet1.address],
        CLIFF_DURATION,
        VESTING_DURATION,
        CAPITAL_RAISE_BPS,
        CAPITAL_RAISE_DURATION,
        teamClaimAddr.address
      );
      const receipt = await tx.wait();

      // Check event
      const event = receipt.logs.find(
        (l) => l.fragment && l.fragment.name === "LaunchpadOpticalCreated"
      );
      expect(event).to.not.be.undefined;
      expect(event.args.creator).to.equal(creator.address);
      expect(event.args.teamClaimAddress).to.equal(teamClaimAddr.address);
      expect(event.args.cliffDuration).to.equal(CLIFF_DURATION);
      expect(event.args.capitalRaiseBps).to.equal(CAPITAL_RAISE_BPS);
    });

    it("should grant OPTICAL_CLAIM_ROLE to the new optical on FeeAccumulator", async function () {
      const tx = await factoryProxy.connect(creator).createLaunchpadOptical(
        [],
        CLIFF_DURATION,
        VESTING_DURATION,
        CAPITAL_RAISE_BPS,
        CAPITAL_RAISE_DURATION,
        teamClaimAddr.address
      );
      const receipt = await tx.wait();

      const event = receipt.logs.find(
        (l) => l.fragment && l.fragment.name === "LaunchpadOpticalCreated"
      );
      const opticalAddr = event.args.optical;

      const OPTICAL_CLAIM_ROLE = ethers.keccak256(ethers.toUtf8Bytes("OPTICAL_CLAIM_ROLE"));
      expect(await accumulatorProxy.hasRole(OPTICAL_CLAIM_ROLE, opticalAddr)).to.be.true;
    });

    it("should set caller as creator (auto-vested) on the deployed optical", async function () {
      const tx = await factoryProxy.connect(creator).createLaunchpadOptical(
        [teamWallet1.address, teamWallet2.address],
        CLIFF_DURATION,
        VESTING_DURATION,
        CAPITAL_RAISE_BPS,
        CAPITAL_RAISE_DURATION,
        teamClaimAddr.address
      );
      const receipt = await tx.wait();

      const event = receipt.logs.find(
        (l) => l.fragment && l.fragment.name === "LaunchpadOpticalCreated"
      );
      const opticalAddr = event.args.optical;

      const LaunchpadOptical = await ethers.getContractFactory("LaunchpadOptical");
      const optical = LaunchpadOptical.attach(opticalAddr);

      expect(await optical.isVested(creator.address)).to.be.true;
      expect(await optical.isVested(teamWallet1.address)).to.be.true;
      expect(await optical.isVested(teamWallet2.address)).to.be.true;
      expect(await optical.creator()).to.equal(creator.address);
      expect(await optical.teamClaimAddress()).to.equal(teamClaimAddr.address);
      expect(await optical.cliffDuration()).to.equal(CLIFF_DURATION);
      expect(await optical.vestingDuration()).to.equal(VESTING_DURATION);
      expect(await optical.capitalRaiseBps()).to.equal(CAPITAL_RAISE_BPS);
    });

    it("should track deployment per creator", async function () {
      await factoryProxy.connect(creator).createLaunchpadOptical(
        [], CLIFF_DURATION, VESTING_DURATION, CAPITAL_RAISE_BPS, CAPITAL_RAISE_DURATION, teamClaimAddr.address
      );
      await factoryProxy.connect(creator).createLaunchpadOptical(
        [], CLIFF_DURATION, VESTING_DURATION, 300n, CAPITAL_RAISE_DURATION, teamClaimAddr.address
      );

      const opticals = await factoryProxy.getOpticalsByCreator(creator.address);
      expect(opticals.length).to.equal(2);
    });

    it("should allow different creators to deploy independently", async function () {
      await factoryProxy.connect(creator).createLaunchpadOptical(
        [], CLIFF_DURATION, VESTING_DURATION, CAPITAL_RAISE_BPS, CAPITAL_RAISE_DURATION, teamClaimAddr.address
      );
      await factoryProxy.connect(randomUser).createLaunchpadOptical(
        [], CLIFF_DURATION, VESTING_DURATION, 200n, CAPITAL_RAISE_DURATION, randomUser.address
      );

      expect((await factoryProxy.getOpticalsByCreator(creator.address)).length).to.equal(1);
      expect((await factoryProxy.getOpticalsByCreator(randomUser.address)).length).to.equal(1);
      expect(await factoryProxy.getDeployedCount()).to.equal(2);
    });

    it("should revert with zero teamClaimAddress", async function () {
      await expect(
        factoryProxy.connect(creator).createLaunchpadOptical(
          [], CLIFF_DURATION, VESTING_DURATION, CAPITAL_RAISE_BPS, CAPITAL_RAISE_DURATION, ZERO_ADDRESS
        )
      ).to.be.revertedWithCustomError(factoryProxy, "ZeroAddress");
    });

    it("should revert with capitalRaiseBps exceeding max", async function () {
      await expect(
        factoryProxy.connect(creator).createLaunchpadOptical(
          [], CLIFF_DURATION, VESTING_DURATION, 1001n, CAPITAL_RAISE_DURATION, teamClaimAddr.address
        )
      ).to.be.reverted; // LaunchpadOptical constructor reverts
    });

    it("should return correct hook flags on deployed optical", async function () {
      const tx = await factoryProxy.connect(creator).createLaunchpadOptical(
        [], CLIFF_DURATION, VESTING_DURATION, CAPITAL_RAISE_BPS, CAPITAL_RAISE_DURATION, teamClaimAddr.address
      );
      const receipt = await tx.wait();
      const event = receipt.logs.find(
        (l) => l.fragment && l.fragment.name === "LaunchpadOpticalCreated"
      );

      const LaunchpadOptical = await ethers.getContractFactory("LaunchpadOptical");
      const optical = LaunchpadOptical.attach(event.args.optical);

      const flags = await optical.getFlags();
      const expected = HookFlags.BEFORE_SWAP | HookFlags.AFTER_SWAP | HookFlags.BEFORE_FEE_DISTRIBUTION | 16n;
      expect(flags).to.equal(expected);
    });
  });

  // ============ VIEWS ============

  describe("Views", function () {
    it("getDeployedCount starts at 0", async function () {
      expect(await factoryProxy.getDeployedCount()).to.equal(0);
    });

    it("getAllOpticals returns paginated results", async function () {
      // Deploy 3
      for (let i = 0; i < 3; i++) {
        await factoryProxy.connect(creator).createLaunchpadOptical(
          [], CLIFF_DURATION, VESTING_DURATION, CAPITAL_RAISE_BPS, CAPITAL_RAISE_DURATION, teamClaimAddr.address
        );
      }

      const all = await factoryProxy.getAllOpticals(0, 10);
      expect(all.length).to.equal(3);

      const page = await factoryProxy.getAllOpticals(1, 1);
      expect(page.length).to.equal(1);
      expect(page[0]).to.equal(all[1]);

      const empty = await factoryProxy.getAllOpticals(10, 5);
      expect(empty.length).to.equal(0);
    });
  });

  // ============ ACCESS CONTROL ============

  describe("Access control", function () {
    it("should set admin on initialize", async function () {
      expect(await factoryProxy.hasRole(ethers.ZeroHash, deployer.address)).to.be.true;
    });

    it("should revert double initialization", async function () {
      await expect(
        factoryProxy.initialize(ZERO_ADDRESS, ZERO_ADDRESS, ZERO_ADDRESS, deployer.address)
      ).to.be.revertedWithCustomError(factoryProxy, "AlreadyInitialized");
    });

    it("should allow UUPS upgrade by admin", async function () {
      const LOF = await ethers.getContractFactory("LaunchpadOpticalFactory");
      const newImpl = await LOF.deploy();
      await factoryProxy.upgradeToAndCall(await newImpl.getAddress(), "0x");
      // Still works after upgrade
      expect(await factoryProxy.getDeployedCount()).to.equal(0);
    });

    it("should revert UUPS upgrade by non-admin", async function () {
      const LOF = await ethers.getContractFactory("LaunchpadOpticalFactory");
      const newImpl = await LOF.deploy();
      await expect(
        factoryProxy.connect(randomUser).upgradeToAndCall(await newImpl.getAddress(), "0x")
      ).to.be.revertedWithCustomError(factoryProxy, "MissingRole");
    });
  });
});
