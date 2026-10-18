const { expect } = require("chai");
const { ethers } = require("hardhat");
const {
  VIRTUAL_USDL_DEFAULT,
  VIRTUAL_TOKEN_DEFAULT,
  TOKEN_TOTAL_SUPPLY,
  CREATION_FEE,
  FeeStrategy,
} = require("../helpers/constants");

describe("SidioraFactory", function () {
  let factory, factoryProxy;
  let configProxy, treasuryProxy, registryProxy, accumulatorProxy, nftProxy;
  let eventEmitter, beacon, usdl;
  let deployer, alice, bob;

  before(async function () {
    [deployer, alice, bob] = await ethers.getSigners();
  });

  async function deployFullStack(legacyPool = false) {
    const Proxy = await ethers.getContractFactory("UUPSProxy");

    // Deploy mock USDL
    const MockERC20 = await ethers.getContractFactory("MockERC20");
    usdl = await MockERC20.deploy("USD Ledger", "USDL", 6);
    await usdl.waitForDeployment();

    // Deploy mock EventEmitter
    const MockEE = await ethers.getContractFactory("MockEventEmitter");
    eventEmitter = await MockEE.deploy();
    await eventEmitter.waitForDeployment();

    // Deploy ProtocolConfig
    const Config = await ethers.getContractFactory("ProtocolConfig");
    const configImpl = await Config.deploy();
    let proxy = await Proxy.deploy(
      await configImpl.getAddress(),
      Config.interface.encodeFunctionData("initialize", [
        await usdl.getAddress(), await eventEmitter.getAddress(), deployer.address,
      ])
    );
    configProxy = Config.attach(await proxy.getAddress());

    // Deploy Treasury
    const Treasury = await ethers.getContractFactory("Treasury");
    const treasuryImpl = await Treasury.deploy();
    proxy = await Proxy.deploy(
      await treasuryImpl.getAddress(),
      Treasury.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ])
    );
    treasuryProxy = Treasury.attach(await proxy.getAddress());

    // Deploy PoolRegistry
    const Registry = await ethers.getContractFactory("PoolRegistry");
    const registryImpl = await Registry.deploy();
    proxy = await Proxy.deploy(
      await registryImpl.getAddress(),
      Registry.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ])
    );
    registryProxy = Registry.attach(await proxy.getAddress());

    // Deploy FeeAccumulator
    const Acc = await ethers.getContractFactory("FeeAccumulator");
    const accImpl = await Acc.deploy();
    proxy = await Proxy.deploy(
      await accImpl.getAddress(),
      Acc.interface.encodeFunctionData("initialize", [
        await configProxy.getAddress(), await treasuryProxy.getAddress(),
        await registryProxy.getAddress(), await eventEmitter.getAddress(),
        await usdl.getAddress(), deployer.address,
      ])
    );
    accumulatorProxy = Acc.attach(await proxy.getAddress());

    // Deploy SidioraNFT
    const NFT = await ethers.getContractFactory("SidioraNFT");
    const nftImpl = await NFT.deploy();
    proxy = await Proxy.deploy(
      await nftImpl.getAddress(),
      NFT.interface.encodeFunctionData("initialize", [
        "Sidiora Pool NFT", "SIDNFT", await eventEmitter.getAddress(), deployer.address,
      ])
    );
    nftProxy = NFT.attach(await proxy.getAddress());

    // Deploy SidioraPool impl + PoolBeacon
    const Pool = await ethers.getContractFactory("SidioraPool");
    const PoolDeployment = legacyPool
      ? new ethers.ContractFactory(require("../../../audit/2026-10-09/remediation/evidence/original-artifacts/SidioraPool.json").abi,
          require("../../../audit/2026-10-09/remediation/evidence/original-artifacts/SidioraPool.json").bytecode, deployer)
      : Pool;
    const poolImpl = await PoolDeployment.deploy();
    await poolImpl.waitForDeployment();
    const PoolBeacon = await ethers.getContractFactory("PoolBeacon");
    beacon = await PoolBeacon.deploy(await poolImpl.getAddress(), deployer.address);
    await beacon.waitForDeployment();

    // Deploy SidioraFactory
    const Factory = await ethers.getContractFactory("SidioraFactory");
    const factoryImpl = await Factory.deploy();
    proxy = await Proxy.deploy(
      await factoryImpl.getAddress(),
      Factory.interface.encodeFunctionData("initialize", [
        await beacon.getAddress(),
        await nftProxy.getAddress(),
        await registryProxy.getAddress(),
        await eventEmitter.getAddress(),
        await configProxy.getAddress(),
        await treasuryProxy.getAddress(),
        await accumulatorProxy.getAddress(),
        await usdl.getAddress(),
        deployer.address,
      ])
    );
    factoryProxy = Factory.attach(await proxy.getAddress());

    // Wire roles
    const FACTORY_ROLE = ethers.keccak256(ethers.toUtf8Bytes("FACTORY_ROLE"));
    const MINTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("MINTER_ROLE"));
    const DEPOSITOR_ROLE = ethers.keccak256(ethers.toUtf8Bytes("DEPOSITOR_ROLE"));

    await registryProxy.grantRole(FACTORY_ROLE, await factoryProxy.getAddress());
    await accumulatorProxy.grantRole(FACTORY_ROLE, await factoryProxy.getAddress());
    await nftProxy.grantRole(MINTER_ROLE, await factoryProxy.getAddress());
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await accumulatorProxy.getAddress());
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await factoryProxy.getAddress());

    // Mint USDL to users for creation fees
    await usdl.mint(alice.address, ethers.parseUnits("100000", 6));
    await usdl.mint(bob.address, ethers.parseUnits("100000", 6));
    // Approve factory to pull USDL for creation fee
    await usdl.connect(alice).approve(await factoryProxy.getAddress(), ethers.MaxUint256);
    await usdl.connect(bob).approve(await factoryProxy.getAddress(), ethers.MaxUint256);
  }

  beforeEach(async function () {
    await deployFullStack();
  });

  describe("initialization", function () {
    it("should set all addresses correctly", async function () {
      expect(await factoryProxy.poolBeacon()).to.equal(await beacon.getAddress());
      expect(await factoryProxy.nftContract()).to.equal(await nftProxy.getAddress());
      expect(await factoryProxy.poolRegistry()).to.equal(await registryProxy.getAddress());
      expect(await factoryProxy.protocolConfig()).to.equal(await configProxy.getAddress());
      expect(await factoryProxy.treasury()).to.equal(await treasuryProxy.getAddress());
    });

    it("should grant DEFAULT_ADMIN_ROLE to admin", async function () {
      expect(await factoryProxy.hasRole(ethers.ZeroHash, deployer.address)).to.be.true;
    });

    it("should revert on double initialization", async function () {
      await expect(
        factoryProxy.initialize(
          await beacon.getAddress(), await nftProxy.getAddress(),
          await registryProxy.getAddress(), await eventEmitter.getAddress(),
          await configProxy.getAddress(), await treasuryProxy.getAddress(),
          await accumulatorProxy.getAddress(), await usdl.getAddress(), deployer.address,
        )
      ).to.be.revertedWithCustomError(factoryProxy, "AlreadyInitialized");
    });
  });

  describe("createMarket", function () {
    it("should deploy token, pool, mint NFT, and register", async function () {
      const tx = await factoryProxy.connect(alice).createMarket(
        "TestToken", "TEST", Number(FeeStrategy.CLAIM), ethers.ZeroAddress
      );
      const receipt = await tx.wait();

      // Find MarketCreated event
      const event = receipt.logs.find(
        l => l.fragment && l.fragment.name === "MarketCreated"
      );
      expect(event).to.not.be.undefined;

      const tokenAddr = event.args[0];
      const poolAddr = event.args[1];
      const nftId = event.args[3];

      // Token exists with correct supply
      const SidioraERC20 = await ethers.getContractFactory("SidioraERC20");
      const token = SidioraERC20.attach(tokenAddr);
      expect(await token.name()).to.equal("TestToken");
      expect(await token.symbol()).to.equal("TEST");
      expect(await token.totalSupply()).to.equal(TOKEN_TOTAL_SUPPLY);

      // Pool has all tokens
      expect(await token.balanceOf(poolAddr)).to.equal(TOKEN_TOTAL_SUPPLY);

      // Pool is initialized
      const Pool = await ethers.getContractFactory("SidioraPool");
      const pool = Pool.attach(poolAddr);
      expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
      expect(await pool.tokenReserve()).to.equal(TOKEN_TOTAL_SUPPLY);
      expect(await pool.tokenAddress()).to.equal(tokenAddr);

      // NFT minted to alice
      expect(await nftProxy.ownerOf(nftId)).to.equal(alice.address);
      expect(await nftProxy.getPoolAddress(nftId)).to.equal(poolAddr);
      expect(await nftProxy.getFeeStrategy(nftId)).to.equal(Number(FeeStrategy.CLAIM));

      // Registered in PoolRegistry
      expect(await registryProxy.getPoolByToken(tokenAddr)).to.equal(poolAddr);
      expect(await registryProxy.isRegisteredPool(poolAddr)).to.be.true;
    });

    it("should charge creation fee to treasury", async function () {
      const treasuryBefore = await usdl.balanceOf(await treasuryProxy.getAddress());

      await factoryProxy.connect(alice).createMarket(
        "FeeToken", "FEE", Number(FeeStrategy.CLAIM), ethers.ZeroAddress
      );

      const treasuryAfter = await usdl.balanceOf(await treasuryProxy.getAddress());
      expect(treasuryAfter - treasuryBefore).to.equal(CREATION_FEE);
    });

    it("should emit MarketCreated event", async function () {
      await expect(
        factoryProxy.connect(alice).createMarket(
          "EmitToken", "EMIT", Number(FeeStrategy.BURN), ethers.ZeroAddress
        )
      ).to.emit(factoryProxy, "MarketCreated");
    });

    it("should increment nonce per creator", async function () {
      expect(await factoryProxy.getNonce(alice.address)).to.equal(0);

      await factoryProxy.connect(alice).createMarket(
        "Token1", "T1", Number(FeeStrategy.CLAIM), ethers.ZeroAddress
      );
      expect(await factoryProxy.getNonce(alice.address)).to.equal(1);

      await factoryProxy.connect(alice).createMarket(
        "Token2", "T2", Number(FeeStrategy.BURN), ethers.ZeroAddress
      );
      expect(await factoryProxy.getNonce(alice.address)).to.equal(2);
    });

    it("should support multiple creators", async function () {
      await factoryProxy.connect(alice).createMarket(
        "AliceToken", "ALICE", Number(FeeStrategy.CLAIM), ethers.ZeroAddress
      );
      await factoryProxy.connect(bob).createMarket(
        "BobToken", "BOB", Number(FeeStrategy.BURN), ethers.ZeroAddress
      );

      expect(await registryProxy.getPoolCount()).to.equal(2);
      const alicePools = await registryProxy.getPoolsByCreator(alice.address);
      const bobPools = await registryProxy.getPoolsByCreator(bob.address);
      expect(alicePools.length).to.equal(1);
      expect(bobPools.length).to.equal(1);
    });

    it("should create market with optical", async function () {
      const optical = await (await ethers.getContractFactory("CoreAuditOptical")).deploy();
      const tx = await factoryProxy.connect(alice).createMarket(
        "OptToken", "OPT", Number(FeeStrategy.CLAIM), await optical.getAddress()
      );
      const receipt = await tx.wait();
      const event = receipt.logs.find(
        l => l.fragment && l.fragment.name === "MarketCreated"
      );
      const poolAddr = event.args[1];

      const Pool = await ethers.getContractFactory("SidioraPool");
      const pool = Pool.attach(poolAddr);
      expect(await pool.opticalAddress()).to.equal(await optical.getAddress());
    });

    it("should create market with address(0) optical", async function () {
      const tx = await factoryProxy.connect(alice).createMarket(
        "NoOpt", "NOPT", Number(FeeStrategy.CLAIM), ethers.ZeroAddress
      );
      const receipt = await tx.wait();
      const event = receipt.logs.find(
        l => l.fragment && l.fragment.name === "MarketCreated"
      );
      const poolAddr = event.args[1];

      const Pool = await ethers.getContractFactory("SidioraPool");
      const pool = Pool.attach(poolAddr);
      expect(await pool.opticalAddress()).to.equal(ethers.ZeroAddress);
    });

    it("should deploy deterministic token addresses (different names = different addresses)", async function () {
      const tx1 = await factoryProxy.connect(alice).createMarket(
        "Token1", "T1", 0, ethers.ZeroAddress
      );
      const r1 = await tx1.wait();
      const e1 = r1.logs.find(l => l.fragment && l.fragment.name === "MarketCreated");

      const tx2 = await factoryProxy.connect(alice).createMarket(
        "Token2", "T2", 0, ethers.ZeroAddress
      );
      const r2 = await tx2.wait();
      const e2 = r2.logs.find(l => l.fragment && l.fragment.name === "MarketCreated");

      expect(e1.args[0]).to.not.equal(e2.args[0]); // different token addresses
      expect(e1.args[1]).to.not.equal(e2.args[1]); // different pool addresses
    });

    it("should support all 4 fee strategies", async function () {
      for (let i = 0; i < 4; i++) {
        await factoryProxy.connect(alice).createMarket(
          `Strat${i}`, `S${i}`, i, ethers.ZeroAddress
        );
      }
      expect(await registryProxy.getPoolCount()).to.equal(4);
      expect(await nftProxy.getFeeStrategy(1)).to.equal(0);
      expect(await nftProxy.getFeeStrategy(2)).to.equal(1);
      expect(await nftProxy.getFeeStrategy(3)).to.equal(2);
      expect(await nftProxy.getFeeStrategy(4)).to.equal(3);
    });

    it("should revert with insufficient USDL for creation fee", async function () {
      // Deploy a user with no USDL
      const [,,, noFundsUser] = await ethers.getSigners();
      await usdl.connect(noFundsUser).approve(await factoryProxy.getAddress(), ethers.MaxUint256);

      await expect(
        factoryProxy.connect(noFundsUser).createMarket(
          "Broke", "BRK", 0, ethers.ZeroAddress
        )
      ).to.be.reverted;
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const V2 = await ethers.getContractFactory("SidioraFactory");
      const implV2 = await V2.deploy();
      await factoryProxy.upgradeToAndCall(await implV2.getAddress(), "0x");
      expect(await factoryProxy.poolBeacon()).to.equal(await beacon.getAddress());
    });

    it("should revert upgrade by non-admin", async function () {
      const V2 = await ethers.getContractFactory("SidioraFactory");
      const implV2 = await V2.deploy();
      await expect(
        factoryProxy.connect(alice).upgradeToAndCall(await implV2.getAddress(), "0x")
      ).to.be.revertedWithCustomError(factoryProxy, "MissingRole");
    });
  });
  describe("audit remediation: real CREATE2 pool prediction", function () {
    it("matches actual deployed pool with exact initialization calldata", async function () {
      const predictedMarket = await factoryProxy.connect(alice).createMarket.staticCall("Predict", "PRD", 0, ethers.ZeroAddress);
      const predictedPool = await factoryProxy.predictPoolAddress(predictedMarket[0], alice.address, ethers.ZeroAddress);
      expect(predictedPool).to.equal(predictedMarket[1]);
      await factoryProxy.connect(alice).createMarket("Predict", "PRD", 0, ethers.ZeroAddress);
      expect(await ethers.provider.getCode(predictedPool)).to.not.equal("0x");
      expect(await registryProxy.getPoolByToken(predictedMarket[0])).to.equal(predictedPool);
    });
  });

  describe("audit remediation: registered launchpad bootstrap", function () {
    it("creates a launchpad pool and enforces vesting on the bound token", async function () {
      const launchpad = await (await ethers.getContractFactory("LaunchpadOptical")).deploy(
        await registryProxy.getAddress(), deployer.address, alice.address, [],
        3600, 3600, 0, 0, alice.address, await accumulatorProxy.getAddress());
      const market = await factoryProxy.connect(alice).createMarket.staticCall("Vested", "VST", 0, await launchpad.getAddress());
      await factoryProxy.connect(alice).createMarket("Vested", "VST", 0, await launchpad.getAddress());
      const pool = await ethers.getContractAt("SidioraPool", market[1]);
      const token = await ethers.getContractAt("SidioraERC20", market[0]);
      expect(await token.pool()).to.equal(market[1]);
      expect(await token.balanceOf(market[1])).to.equal(TOKEN_TOTAL_SUPPLY);
      const input = ethers.parseUnits("100", 6);
      await usdl.connect(alice).transfer(market[1], input);
      const deadline = (await ethers.provider.getBlock("latest")).timestamp + 3600;
      await pool.connect(alice).swap(input, 0, true, alice.address, deadline);
      await expect(token.connect(alice).transfer(bob.address, 1))
        .to.be.revertedWithCustomError(launchpad, "CliffActive");
      await expect(token.connect(alice).transfer(market[1], 1))
        .to.be.revertedWithCustomError(launchpad, "CliffActive");
    });
  });

  describe("audit remediation: populated baseline pool migration", function () {
    it("preserves pre-upgrade reserves and restores authenticated routing only after administrator migration", async function () {
      await deployFullStack(true);
      const market = await factoryProxy.connect(alice).createMarket.staticCall("Legacy", "LEG", 0, ethers.ZeroAddress);
      await factoryProxy.connect(alice).createMarket("Legacy", "LEG", 0, ethers.ZeroAddress);
      const pool = await ethers.getContractAt("SidioraPool", market[1]);
      const input = ethers.parseUnits("100", 6);
      await usdl.connect(alice).transfer(market[1], input);
      let deadline = (await ethers.provider.getBlock("latest")).timestamp + 3600;
      await pool.connect(alice).swap(input, 0, true, alice.address, deadline);
      const reserves = await pool.getReserves();
      const legacyVolume = await pool.cumulativeVolume();
      const nextImpl = await (await ethers.getContractFactory("SidioraPool")).deploy();
      await beacon.upgradeTo(await nextImpl.getAddress());
      expect(await pool.factory()).to.equal(ethers.ZeroAddress);
      expect(await pool.getReserves()).to.deep.equal(reserves);
      expect(await pool.cumulativeVolume()).to.equal(legacyVolume);
      await expect(pool.connect(alice).migrateFactory(await factoryProxy.getAddress()))
        .to.be.revertedWithCustomError(pool, "Unauthorized");
      await expect(pool.migrateFactory(await tokenAddressForInvalidMigration()))
        .to.be.reverted;
      await pool.migrateFactory(await factoryProxy.getAddress());
      expect(await pool.factory()).to.equal(await factoryProxy.getAddress());
      await expect(pool.migrateFactory(await factoryProxy.getAddress()))
        .to.be.revertedWithCustomError(pool, "InvalidConfiguration");
      const role = ethers.keccak256(ethers.toUtf8Bytes("ROUTER_ROLE"));
      await factoryProxy.grantRole(role, bob.address);
      await usdl.connect(bob).transfer(market[1], input);
      deadline = (await ethers.provider.getBlock("latest")).timestamp + 3600;
      await pool.connect(bob).swapFor(input, 0, true, alice.address, alice.address, deadline);
      expect(await pool.realUsdlBalance()).to.equal(await usdl.balanceOf(market[1]));
      expect(await pool.tokenReserve()).to.equal(await (await ethers.getContractAt("SidioraERC20", market[0])).balanceOf(market[1]));
      async function tokenAddressForInvalidMigration() { return await usdl.getAddress(); }
    });
  });

  describe("audit remediation: fresh checkpoint markets support AIRDROP", function () {
    it("creates AIRDROP fee rights and permits direct owner selection for checkpoint tokens", async function () {
      const market = await factoryProxy.connect(alice).createMarket.staticCall("Snapshot", "SNP", 2, ethers.ZeroAddress);
      await factoryProxy.connect(alice).createMarket("Snapshot", "SNP", 2, ethers.ZeroAddress);
      expect(await nftProxy.getFeeStrategy(market[2])).to.equal(2);
      await nftProxy.connect(alice).setFeeStrategy(market[2], 0);
      await nftProxy.connect(alice).setFeeStrategy(market[2], 2);
      expect(await nftProxy.getFeeStrategy(market[2])).to.equal(2);
    });
  });

});
