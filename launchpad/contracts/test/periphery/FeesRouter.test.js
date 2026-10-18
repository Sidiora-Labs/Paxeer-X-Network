const { expect } = require("chai");
const { ethers } = require("hardhat");
const {
  VIRTUAL_USDL_DEFAULT,
  TOKEN_TOTAL_SUPPLY,
  CREATION_FEE,
  FeeStrategy,
  ZERO_ADDRESS,
  DEAD_ADDRESS,
} = require("../helpers/constants");

describe("FeesRouter", function () {
  let feesRouter, feesRouterAddr;
  let router, routerAddr;
  let factoryProxy, configProxy, treasuryProxy, registryProxy, accumulatorProxy, nftProxy;
  let eventEmitter, beacon, usdl;
  let deployer, alice, bob, charlie;

  before(async function () {
    [deployer, alice, bob, charlie] = await ethers.getSigners();
  });

  async function deployFullStack() {
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const MockERC20 = await ethers.getContractFactory("MockERC20");

    usdl = await MockERC20.deploy("USD Ledger", "USDL", 6);
    await usdl.waitForDeployment();

    const MockEE = await ethers.getContractFactory("MockEventEmitter");
    eventEmitter = await MockEE.deploy();
    await eventEmitter.waitForDeployment();

    const Config = await ethers.getContractFactory("ProtocolConfig");
    const configImpl = await Config.deploy();
    let proxy = await Proxy.deploy(await configImpl.getAddress(),
      Config.interface.encodeFunctionData("initialize", [
        await usdl.getAddress(), await eventEmitter.getAddress(), deployer.address,
      ]));
    configProxy = Config.attach(await proxy.getAddress());

    const Treasury = await ethers.getContractFactory("Treasury");
    const treasuryImpl = await Treasury.deploy();
    proxy = await Proxy.deploy(await treasuryImpl.getAddress(),
      Treasury.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ]));
    treasuryProxy = Treasury.attach(await proxy.getAddress());

    const Registry = await ethers.getContractFactory("PoolRegistry");
    const registryImpl = await Registry.deploy();
    proxy = await Proxy.deploy(await registryImpl.getAddress(),
      Registry.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ]));
    registryProxy = Registry.attach(await proxy.getAddress());

    const Acc = await ethers.getContractFactory("FeeAccumulator");
    const accImpl = await Acc.deploy();
    proxy = await Proxy.deploy(await accImpl.getAddress(),
      Acc.interface.encodeFunctionData("initialize", [
        await configProxy.getAddress(), await treasuryProxy.getAddress(),
        await registryProxy.getAddress(), await eventEmitter.getAddress(),
        await usdl.getAddress(), deployer.address,
      ]));
    accumulatorProxy = Acc.attach(await proxy.getAddress());

    const NFT = await ethers.getContractFactory("SidioraNFT");
    const nftImpl = await NFT.deploy();
    proxy = await Proxy.deploy(await nftImpl.getAddress(),
      NFT.interface.encodeFunctionData("initialize", [
        "Sidiora Pool NFT", "SIDNFT", await eventEmitter.getAddress(), deployer.address,
      ]));
    nftProxy = NFT.attach(await proxy.getAddress());

    const Pool = await ethers.getContractFactory("SidioraPool");
    const poolImpl = await Pool.deploy();
    const PoolBeacon = await ethers.getContractFactory("PoolBeacon");
    beacon = await PoolBeacon.deploy(await poolImpl.getAddress(), deployer.address);
    await beacon.waitForDeployment();

    const Factory = await ethers.getContractFactory("SidioraFactory");
    const factoryImpl = await Factory.deploy();
    proxy = await Proxy.deploy(await factoryImpl.getAddress(),
      Factory.interface.encodeFunctionData("initialize", [
        await beacon.getAddress(), await nftProxy.getAddress(),
        await registryProxy.getAddress(), await eventEmitter.getAddress(),
        await configProxy.getAddress(), await treasuryProxy.getAddress(),
        await accumulatorProxy.getAddress(), await usdl.getAddress(), deployer.address,
      ]));
    factoryProxy = Factory.attach(await proxy.getAddress());

    const Router = await ethers.getContractFactory("Router");
    const routerImpl = await Router.deploy();
    proxy = await Proxy.deploy(await routerImpl.getAddress(),
      Router.interface.encodeFunctionData("initialize", [
        await factoryProxy.getAddress(), await registryProxy.getAddress(),
        await configProxy.getAddress(), await usdl.getAddress(), deployer.address,
      ]));
    router = Router.attach(await proxy.getAddress());
    routerAddr = await router.getAddress();

    // FeesRouter
    const FeesRouter = await ethers.getContractFactory("FeesRouter");
    const feesRouterImpl = await FeesRouter.deploy();
    proxy = await Proxy.deploy(await feesRouterImpl.getAddress(),
      FeesRouter.interface.encodeFunctionData("initialize", [
        await nftProxy.getAddress(), await accumulatorProxy.getAddress(),
        await registryProxy.getAddress(), deployer.address,
      ]));
    feesRouter = FeesRouter.attach(await proxy.getAddress());
    feesRouterAddr = await feesRouter.getAddress();

    // Wire roles
    const FACTORY_ROLE = ethers.keccak256(ethers.toUtf8Bytes("FACTORY_ROLE"));
    const MINTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("MINTER_ROLE"));
    const DEPOSITOR_ROLE = ethers.keccak256(ethers.toUtf8Bytes("DEPOSITOR_ROLE"));
    const ROUTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("ROUTER_ROLE"));
    const FEES_ROUTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("FEES_ROUTER_ROLE"));
    const STRATEGY_SETTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("STRATEGY_SETTER_ROLE"));
    await registryProxy.grantRole(FACTORY_ROLE, await factoryProxy.getAddress());
    await accumulatorProxy.grantRole(FACTORY_ROLE, await factoryProxy.getAddress());
    await nftProxy.grantRole(MINTER_ROLE, await factoryProxy.getAddress());
    await nftProxy.grantRole(STRATEGY_SETTER_ROLE, feesRouterAddr);
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await accumulatorProxy.getAddress());
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, routerAddr);
    await factoryProxy.grantRole(ROUTER_ROLE, routerAddr);
    await accumulatorProxy.grantRole(FEES_ROUTER_ROLE, feesRouterAddr);

    // Fund users
    await usdl.mint(alice.address, ethers.parseUnits("1000000", 6));
    await usdl.mint(bob.address, ethers.parseUnits("1000000", 6));
    await usdl.mint(charlie.address, ethers.parseUnits("1000000", 6));
    await usdl.connect(alice).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(bob).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(charlie).approve(routerAddr, ethers.MaxUint256);
  }

  async function createMarketWithStrategy(creator, name, symbol, strategy) {
    const tx = await router.connect(creator).createMarket(name, symbol, strategy, ZERO_ADDRESS);
    const receipt = await tx.wait();
    const routerIface = router.interface;
    const event = receipt.logs
      .map(l => { try { return routerIface.parseLog(l); } catch { return null; } })
      .find(e => e && e.name === "MarketCreated");

    const tokenAddr = event.args[0];
    const poolAddr = event.args[1];
    const nftId = event.args[3];

    const Pool = await ethers.getContractFactory("SidioraPool");
    const pool = Pool.attach(poolAddr);
    const SidioraERC20 = await ethers.getContractFactory("SidioraERC20");
    const token = SidioraERC20.attach(tokenAddr);

    const POOL_ROLE = ethers.keccak256(ethers.toUtf8Bytes("POOL_ROLE"));
    await accumulatorProxy.grantRole(POOL_ROLE, poolAddr);

    return { pool, token, poolAddr, tokenAddr, nftId };
  }

  async function futureDeadline() {
    const block = await ethers.provider.getBlock("latest");
    return block.timestamp + 3600;
  }

  async function buyAndAccumulateFees(poolAddr, buyer, amount) {
    await router.connect(buyer).buy(poolAddr, amount, 0, await futureDeadline());
  }

  beforeEach(async function () {
    await deployFullStack();
  });

  describe("Initialization", function () {
    it("should set references correctly", async function () {
      expect(await feesRouter.nftContract()).to.equal(await nftProxy.getAddress());
      expect(await feesRouter.feeAccumulator()).to.equal(await accumulatorProxy.getAddress());
      expect(await feesRouter.poolRegistry()).to.equal(await registryProxy.getAddress());
    });
  });

  describe("setFeeStrategy", function () {
    it("should allow NFT owner to change strategy", async function () {
      const { nftId } = await createMarketWithStrategy(alice, "StratToken", "STRAT", Number(FeeStrategy.CLAIM));
      await expect(feesRouter.connect(alice).setFeeStrategy(nftId, Number(FeeStrategy.BURN)))
        .to.emit(feesRouter, "FeeStrategyChanged").withArgs(nftId, Number(FeeStrategy.CLAIM), Number(FeeStrategy.BURN));
      expect(await nftProxy.getFeeStrategy(nftId)).to.equal(Number(FeeStrategy.BURN));
    });

    it("should revert for non-NFT owner", async function () {
      const { nftId } = await createMarketWithStrategy(alice, "StratToken2", "ST2", Number(FeeStrategy.CLAIM));
      await expect(
        feesRouter.connect(bob).setFeeStrategy(nftId, Number(FeeStrategy.BURN))
      ).to.be.revertedWithCustomError(feesRouter, "NotNftOwner");
    });
  });

  describe("claimFees (CLAIM strategy)", function () {
    it("should claim accumulated fees to NFT owner", async function () {
      const { poolAddr, nftId } = await createMarketWithStrategy(
        alice, "ClaimToken", "CLM", Number(FeeStrategy.CLAIM)
      );

      // Generate fees via buy
      await buyAndAccumulateFees(poolAddr, bob, ethers.parseUnits("1000", 6));

      const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
      expect(accFees).to.be.gt(0);

      const aliceUsdlBefore = await usdl.balanceOf(alice.address);
      const tx = await feesRouter.connect(alice).claimFees(nftId);
      await expect(tx).to.emit(feesRouter, "FeesClaimed");

      const aliceUsdlAfter = await usdl.balanceOf(alice.address);
      expect(aliceUsdlAfter - aliceUsdlBefore).to.equal(accFees);

      // Fees should be zeroed out
      expect(await accumulatorProxy.getAccumulatedFees(poolAddr)).to.equal(0);
    });

    it("should revert claim with wrong strategy", async function () {
      const { poolAddr, nftId } = await createMarketWithStrategy(
        alice, "WrongStrat", "WS", Number(FeeStrategy.BURN)
      );

      await buyAndAccumulateFees(poolAddr, bob, ethers.parseUnits("1000", 6));

      await expect(
        feesRouter.connect(alice).claimFees(nftId)
      ).to.be.revertedWithCustomError(feesRouter, "WrongStrategy");
    });

    it("should revert claim for non-owner", async function () {
      const { poolAddr, nftId } = await createMarketWithStrategy(
        alice, "NonOwner", "NO", Number(FeeStrategy.CLAIM)
      );

      await buyAndAccumulateFees(poolAddr, bob, ethers.parseUnits("1000", 6));

      await expect(
        feesRouter.connect(bob).claimFees(nftId)
      ).to.be.revertedWithCustomError(feesRouter, "NotNftOwner");
    });
  });

  describe("executeBurn (BURN strategy)", function () {
    it("should burn accumulated fees to DEAD address", async function () {
      const { poolAddr, nftId } = await createMarketWithStrategy(
        alice, "BurnToken", "BRN", Number(FeeStrategy.BURN)
      );

      await buyAndAccumulateFees(poolAddr, bob, ethers.parseUnits("1000", 6));

      const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
      const deadBefore = await usdl.balanceOf(DEAD_ADDRESS);

      const tx = await feesRouter.connect(alice).executeBurn(nftId);
      await expect(tx).to.emit(feesRouter, "FeesBurned");

      const deadAfter = await usdl.balanceOf(DEAD_ADDRESS);
      expect(deadAfter - deadBefore).to.equal(accFees);
      expect(await accumulatorProxy.getAccumulatedFees(poolAddr)).to.equal(0);
    });

    it("should revert burn with wrong strategy", async function () {
      const { poolAddr, nftId } = await createMarketWithStrategy(
        alice, "BurnWrong", "BW", Number(FeeStrategy.CLAIM)
      );

      await buyAndAccumulateFees(poolAddr, bob, ethers.parseUnits("1000", 6));

      await expect(
        feesRouter.connect(alice).executeBurn(nftId)
      ).to.be.revertedWithCustomError(feesRouter, "WrongStrategy");
    });
  });

  describe("executeAirdrop (AIRDROP strategy)", function () {
    it("should trigger airdrop and allow token holder to claim", async function () {
      const { pool, token, poolAddr, nftId } = await createMarketWithStrategy(
        alice, "AirdropToken", "AIR", Number(FeeStrategy.AIRDROP)
      );

      // Bob buys tokens (becomes a holder)
      await buyAndAccumulateFees(poolAddr, bob, ethers.parseUnits("1000", 6));
      const bobTokens = await token.balanceOf(bob.address);
      expect(bobTokens).to.be.gt(0);

      // Alice triggers airdrop
      const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
      const tx = await feesRouter.connect(alice).executeAirdrop(nftId);
      await expect(tx).to.emit(feesRouter, "AirdropExecuted");

      // Bob claims airdrop
      const bobUsdlBefore = await usdl.balanceOf(bob.address);
      const claimTx = await feesRouter.connect(bob).claimAirdrop(nftId);
      await expect(claimTx).to.emit(feesRouter, "AirdropClaimed");

      const bobUsdlAfter = await usdl.balanceOf(bob.address);
      expect(bobUsdlAfter - bobUsdlBefore).to.equal(accFees);
    });
  });

  describe("executeLpRewards (LP_REWARDS strategy)", function () {
    it("should send fees to pool and sync reserves", async function () {
      const { pool, poolAddr, nftId } = await createMarketWithStrategy(
        alice, "LpToken", "LP", Number(FeeStrategy.LP_REWARDS)
      );

      await buyAndAccumulateFees(poolAddr, bob, ethers.parseUnits("1000", 6));

      const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
      const realUsdlBefore = await pool.realUsdlBalance();

      const tx = await feesRouter.connect(alice).executeLpRewards(nftId);
      await expect(tx).to.emit(feesRouter, "LpRewardsExecuted");

      // Pool's real USDL should increase by the LP rewards amount
      const realUsdlAfter = await pool.realUsdlBalance();
      expect(realUsdlAfter).to.be.gt(realUsdlBefore);
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const New = await ethers.getContractFactory("FeesRouter");
      const newImpl = await New.deploy();
      await newImpl.waitForDeployment();
      await feesRouter.upgradeToAndCall(await newImpl.getAddress(), "0x");
    });

    it("should revert upgrade by non-admin", async function () {
      const New = await ethers.getContractFactory("FeesRouter");
      const newImpl = await New.deploy();
      await newImpl.waitForDeployment();
      await expect(
        feesRouter.connect(alice).upgradeToAndCall(await newImpl.getAddress(), "0x")
      ).to.be.revertedWithCustomError(feesRouter, "MissingRole");
    });
  });
});
