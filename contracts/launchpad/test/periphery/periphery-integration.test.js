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

describe("Phase 7: Periphery Integration", function () {
  let router, routerAddr, quoter, feesRouter, feesRouterAddr;
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

    const Quoter = await ethers.getContractFactory("Quoter");
    const quoterImpl = await Quoter.deploy();
    proxy = await Proxy.deploy(await quoterImpl.getAddress(),
      Quoter.interface.encodeFunctionData("initialize", [
        await registryProxy.getAddress(), await configProxy.getAddress(), deployer.address,
      ]));
    quoter = Quoter.attach(await proxy.getAddress());

    const FeesRouter = await ethers.getContractFactory("FeesRouter");
    const feesRouterImpl = await FeesRouter.deploy();
    proxy = await Proxy.deploy(await feesRouterImpl.getAddress(),
      FeesRouter.interface.encodeFunctionData("initialize", [
        await nftProxy.getAddress(), await accumulatorProxy.getAddress(),
        await registryProxy.getAddress(), deployer.address,
      ]));
    feesRouter = FeesRouter.attach(await proxy.getAddress());
    feesRouterAddr = await feesRouter.getAddress();

    // Wire all roles
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
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await factoryProxy.getAddress());
    await factoryProxy.grantRole(ROUTER_ROLE, routerAddr);
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, routerAddr);
    await accumulatorProxy.grantRole(FEES_ROUTER_ROLE, feesRouterAddr);

    // Fund users
    await usdl.mint(alice.address, ethers.parseUnits("1000000", 6));
    await usdl.mint(bob.address, ethers.parseUnits("1000000", 6));
    await usdl.mint(charlie.address, ethers.parseUnits("1000000", 6));
    await usdl.connect(alice).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(bob).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(charlie).approve(routerAddr, ethers.MaxUint256);
  }

  async function futureDeadline() {
    const block = await ethers.provider.getBlock("latest");
    return block.timestamp + 3600;
  }

  async function createMarket(creator, name, symbol, strategy) {
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
    expect(await accumulatorProxy.hasRole(POOL_ROLE, poolAddr)).to.equal(true);

    return { pool, token, poolAddr, tokenAddr, nftId };
  }

  beforeEach(async function () {
    await deployFullStack();
  });

  it("Router.createMarket full flow: token+pool+NFT+registry", async function () {
    const { pool, token, poolAddr, tokenAddr, nftId } = await createMarket(
      alice, "IntegToken", "INTG", Number(FeeStrategy.CLAIM)
    );

    // Token supply in pool
    expect(await token.balanceOf(poolAddr)).to.equal(TOKEN_TOTAL_SUPPLY);
    // Pool initialized
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
    // NFT to creator
    expect(await nftProxy.ownerOf(nftId)).to.equal(alice.address);
    // Registered
    expect(await registryProxy.getPoolByToken(tokenAddr)).to.equal(poolAddr);
    // Creation fee in treasury
    expect(await usdl.balanceOf(await treasuryProxy.getAddress())).to.be.gte(CREATION_FEE);
    // Quoter can find it
    const allPools = await quoter.getAllPools(0, 10);
    expect(allPools).to.include(poolAddr);
  });

  it("Router.buy + Router.sell full cycle with fee recording", async function () {
    const { pool, token, poolAddr } = await createMarket(
      alice, "CycleToken", "CYC", Number(FeeStrategy.CLAIM)
    );

    // Bob buys
    const buyAmount = ethers.parseUnits("1000", 6);
    await router.connect(bob).buy(poolAddr, buyAmount, 0, await futureDeadline());

    const bobTokens = await token.balanceOf(bob.address);
    expect(bobTokens).to.be.gt(0);
    expect(await pool.realUsdlBalance()).to.be.gt(0);

    // Fee accumulated
    const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(accFees).to.be.gt(0);

    // Bob sells half
    const sellAmount = bobTokens / 2n;
    await token.connect(bob).approve(routerAddr, sellAmount);
    const bobUsdlBefore = await usdl.balanceOf(bob.address);
    await router.connect(bob).sell(poolAddr, sellAmount, 0, await futureDeadline());
    const bobUsdlAfter = await usdl.balanceOf(bob.address);
    expect(bobUsdlAfter).to.be.gt(bobUsdlBefore);
  });

  it("Quoter.quoteExactInput matches actual Router.buy output", async function () {
    const { pool, token, poolAddr } = await createMarket(
      alice, "QuoteToken", "QOT", Number(FeeStrategy.CLAIM)
    );

    const buyAmount = ethers.parseUnits("500", 6);
    const quote = await quoter.quoteExactInput(poolAddr, buyAmount, true);
    expect(quote.amountOut).to.be.gt(0);
    expect(quote.feeAmount).to.be.gt(0);

    // Execute actual buy
    await router.connect(bob).buy(poolAddr, buyAmount, 0, await futureDeadline());
    const bobTokens = await token.balanceOf(bob.address);

    // Quote should be close to actual (within 2% — fee calc timing can differ slightly)
    const diff = quote.amountOut > bobTokens
      ? quote.amountOut - bobTokens
      : bobTokens - quote.amountOut;
    const tolerance = bobTokens / 50n; // 2%
    expect(diff).to.be.lte(tolerance);
  });

  it("FeesRouter.claimFees: full create→buy→claim flow", async function () {
    const { poolAddr, nftId } = await createMarket(
      alice, "ClaimFlow", "CLF", Number(FeeStrategy.CLAIM)
    );

    // Bob buys to generate fees
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("2000", 6), 0, await futureDeadline());

    const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(accFees).to.be.gt(0);

    // Alice (NFT owner) claims
    const aliceUsdlBefore = await usdl.balanceOf(alice.address);
    await feesRouter.connect(alice).claimFees(nftId);
    const aliceUsdlAfter = await usdl.balanceOf(alice.address);
    expect(aliceUsdlAfter - aliceUsdlBefore).to.equal(accFees);

    // Fees zeroed
    expect(await accumulatorProxy.getAccumulatedFees(poolAddr)).to.equal(0);
  });

  it("FeesRouter.executeBurn: fees sent to DEAD address", async function () {
    const { poolAddr, nftId } = await createMarket(
      alice, "BurnFlow", "BRF", Number(FeeStrategy.BURN)
    );

    await router.connect(bob).buy(poolAddr, ethers.parseUnits("2000", 6), 0, await futureDeadline());
    const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);

    const deadBefore = await usdl.balanceOf(DEAD_ADDRESS);
    await feesRouter.connect(alice).executeBurn(nftId);
    const deadAfter = await usdl.balanceOf(DEAD_ADDRESS);
    expect(deadAfter - deadBefore).to.equal(accFees);
  });

  it("FeesRouter.executeLpRewards: USDL sent to pool, reserves updated", async function () {
    const { pool, poolAddr, nftId } = await createMarket(
      alice, "LpFlow", "LPF", Number(FeeStrategy.LP_REWARDS)
    );

    await router.connect(bob).buy(poolAddr, ethers.parseUnits("2000", 6), 0, await futureDeadline());
    const realUsdlBefore = await pool.realUsdlBalance();

    await feesRouter.connect(alice).executeLpRewards(nftId);
    const realUsdlAfter = await pool.realUsdlBalance();
    expect(realUsdlAfter).to.be.gt(realUsdlBefore);
  });

  it("NFT transfer changes fee claim recipient", async function () {
    const { poolAddr, nftId } = await createMarket(
      alice, "TransferNFT", "TNFT", Number(FeeStrategy.CLAIM)
    );

    await router.connect(bob).buy(poolAddr, ethers.parseUnits("2000", 6), 0, await futureDeadline());

    // Transfer NFT to charlie
    await nftProxy.connect(alice).transferFrom(alice.address, charlie.address, nftId);
    expect(await nftProxy.ownerOf(nftId)).to.equal(charlie.address);

    // Charlie claims (not Alice)
    const charlieUsdlBefore = await usdl.balanceOf(charlie.address);
    await feesRouter.connect(charlie).claimFees(nftId);
    const charlieUsdlAfter = await usdl.balanceOf(charlie.address);
    expect(charlieUsdlAfter).to.be.gt(charlieUsdlBefore);

    // Alice cannot claim
    // (fees already claimed, so this would revert with NoFeesAccumulated anyway)
  });

  it("Multi-market: two markets, independent fees and claims", async function () {
    const m1 = await createMarket(alice, "Market1", "M1", Number(FeeStrategy.CLAIM));
    const m2 = await createMarket(bob, "Market2", "M2", Number(FeeStrategy.BURN));

    // Buy in both
    await router.connect(charlie).buy(m1.poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());
    await router.connect(charlie).buy(m2.poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());

    const fees1 = await accumulatorProxy.getAccumulatedFees(m1.poolAddr);
    const fees2 = await accumulatorProxy.getAccumulatedFees(m2.poolAddr);
    expect(fees1).to.be.gt(0);
    expect(fees2).to.be.gt(0);

    // Alice claims M1 fees
    await feesRouter.connect(alice).claimFees(m1.nftId);
    expect(await accumulatorProxy.getAccumulatedFees(m1.poolAddr)).to.equal(0);
    // M2 fees unaffected
    expect(await accumulatorProxy.getAccumulatedFees(m2.poolAddr)).to.equal(fees2);

    // Bob burns M2 fees
    await feesRouter.connect(bob).executeBurn(m2.nftId);
    expect(await accumulatorProxy.getAccumulatedFees(m2.poolAddr)).to.equal(0);
  });
});
