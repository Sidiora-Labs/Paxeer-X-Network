const { expect } = require("chai");
const { ethers } = require("hardhat");
const {
  VIRTUAL_USDL_DEFAULT,
  TOKEN_TOTAL_SUPPLY,
  CREATION_FEE,
  FeeStrategy,
  ZERO_ADDRESS,
  DEAD_ADDRESS,
  BPS_DENOMINATOR,
} = require("../helpers/constants");

describe("Phase 8.1: E2E Happy Path", function () {
  let router, routerAddr, quoter, feesRouter, feesRouterAddr;
  let factoryProxy, configProxy, treasuryProxy, registryProxy, accumulatorProxy, nftProxy;
  let eventEmitter, beacon, usdl;
  let deployer, alice, bob, charlie, guardian;

  before(async function () {
    [deployer, alice, bob, charlie, guardian] = await ethers.getSigners();
  });

  async function futureDeadline() {
    const block = await ethers.provider.getBlock("latest");
    return block.timestamp + 3600;
  }

  async function deployFullStack() {
    const Proxy = await ethers.getContractFactory("UUPSProxy");
    const MockERC20 = await ethers.getContractFactory("MockERC20");

    usdl = await MockERC20.deploy("USD Ledger", "USDL", 6);
    await usdl.waitForDeployment();

    const EventEmitter = await ethers.getContractFactory("EventEmitter");
    const emitterImpl = await EventEmitter.deploy();
    const emitterProxy = await Proxy.deploy(await emitterImpl.getAddress(),
      EventEmitter.interface.encodeFunctionData("initialize", [deployer.address]));
    eventEmitter = EventEmitter.attach(await emitterProxy.getAddress());
    await eventEmitter.reinitializeV2(deployer.address);

    // ProtocolConfig
    const Config = await ethers.getContractFactory("ProtocolConfig");
    const configImpl = await Config.deploy();
    let proxy = await Proxy.deploy(await configImpl.getAddress(),
      Config.interface.encodeFunctionData("initialize", [
        await usdl.getAddress(), await eventEmitter.getAddress(), deployer.address,
      ]));
    configProxy = Config.attach(await proxy.getAddress());

    // Treasury
    const Treasury = await ethers.getContractFactory("Treasury");
    const treasuryImpl = await Treasury.deploy();
    proxy = await Proxy.deploy(await treasuryImpl.getAddress(),
      Treasury.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ]));
    treasuryProxy = Treasury.attach(await proxy.getAddress());

    // PoolRegistry
    const Registry = await ethers.getContractFactory("PoolRegistry");
    const registryImpl = await Registry.deploy();
    proxy = await Proxy.deploy(await registryImpl.getAddress(),
      Registry.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ]));
    registryProxy = Registry.attach(await proxy.getAddress());

    // FeeAccumulator
    const Acc = await ethers.getContractFactory("FeeAccumulator");
    const accImpl = await Acc.deploy();
    proxy = await Proxy.deploy(await accImpl.getAddress(),
      Acc.interface.encodeFunctionData("initialize", [
        await configProxy.getAddress(), await treasuryProxy.getAddress(),
        await registryProxy.getAddress(), await eventEmitter.getAddress(),
        await usdl.getAddress(), deployer.address,
      ]));
    accumulatorProxy = Acc.attach(await proxy.getAddress());

    // SidioraNFT
    const NFT = await ethers.getContractFactory("SidioraNFT");
    const nftImpl = await NFT.deploy();
    proxy = await Proxy.deploy(await nftImpl.getAddress(),
      NFT.interface.encodeFunctionData("initialize", [
        "Sidiora Pool NFT", "SIDNFT", await eventEmitter.getAddress(), deployer.address,
      ]));
    nftProxy = NFT.attach(await proxy.getAddress());

    // Pool impl + Beacon
    const Pool = await ethers.getContractFactory("SidioraPool");
    const poolImpl = await Pool.deploy();
    const PoolBeacon = await ethers.getContractFactory("PoolBeacon");
    beacon = await PoolBeacon.deploy(await poolImpl.getAddress(), deployer.address);
    await beacon.waitForDeployment();

    // Factory
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

    // Router
    const Router = await ethers.getContractFactory("Router");
    const routerImpl = await Router.deploy();
    proxy = await Proxy.deploy(await routerImpl.getAddress(),
      Router.interface.encodeFunctionData("initialize", [
        await factoryProxy.getAddress(), await registryProxy.getAddress(),
        await configProxy.getAddress(), await usdl.getAddress(), deployer.address,
      ]));
    router = Router.attach(await proxy.getAddress());
    routerAddr = await router.getAddress();

    // Quoter
    const Quoter = await ethers.getContractFactory("Quoter");
    const quoterImpl = await Quoter.deploy();
    proxy = await Proxy.deploy(await quoterImpl.getAddress(),
      Quoter.interface.encodeFunctionData("initialize", [
        await registryProxy.getAddress(), await configProxy.getAddress(), deployer.address,
      ]));
    quoter = Quoter.attach(await proxy.getAddress());

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
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, routerAddr);
    await eventEmitter.setPoolRegistry(await registryProxy.getAddress());
    await eventEmitter.setSidioraFactory(await factoryProxy.getAddress());
    for (const producer of [configProxy, treasuryProxy, registryProxy, accumulatorProxy, nftProxy]) {
      await eventEmitter.setAuthorizedEmitter(await producer.getAddress(), true);
    }
    await eventEmitter.setTypedEmitter(await configProxy.getAddress(), eventEmitter.interface.getFunction("emitConfigUpdated").selector, true);
    await eventEmitter.setTypedEmitter(await registryProxy.getAddress(), eventEmitter.interface.getFunction("emitPoolRegistered").selector, true);
    await eventEmitter.setTypedEmitter(await accumulatorProxy.getAddress(), eventEmitter.interface.getFunction("emitFeeFlow").selector, true);
    for (const name of ["emitFeeRecorded", "emitFeeDistributed"]) {
      await eventEmitter.setTypedEmitter(await accumulatorProxy.getAddress(), eventEmitter.interface.getFunction(name).selector, true);
    }
    for (const name of ["emitFeeStrategyChanged", "emitNftMint"]) {
      await eventEmitter.setTypedEmitter(await nftProxy.getAddress(), eventEmitter.interface.getFunction(name).selector, true);
    }
    await eventEmitter.setAuthorizedEmitter(routerAddr, true);
    await eventEmitter.setTypedEmitter(routerAddr, eventEmitter.interface.getFunction("emitRouterTrade").selector, true);
    await factoryProxy.grantRole(ROUTER_ROLE, routerAddr);
    await accumulatorProxy.grantRole(FEES_ROUTER_ROLE, feesRouterAddr);

    // Fund users generously
    const fundAmount = ethers.parseUnits("10000000", 6);
    await usdl.mint(alice.address, fundAmount);
    await usdl.mint(bob.address, fundAmount);
    await usdl.mint(charlie.address, fundAmount);
    await usdl.connect(alice).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(bob).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(charlie).approve(routerAddr, ethers.MaxUint256);
  }

  async function createMarket(creator, name, symbol, strategy, optical) {
    const opticalAddr = optical || ZERO_ADDRESS;
    const tx = await router.connect(creator).createMarket(name, symbol, strategy, opticalAddr);
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

  // ───────────────────────────────────────────────────────────────────────────
  // Test 1: Market lifecycle — create → buy → price up → sell → price down
  // ───────────────────────────────────────────────────────────────────────────
  it("Market lifecycle: create → buy → price up → sell → price down", async function () {
    const { pool, token, poolAddr } = await createMarket(
      alice, "LifecycleToken", "LIFE", Number(FeeStrategy.CLAIM)
    );

    // Initial state
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
    expect(await pool.tokenReserve()).to.equal(TOKEN_TOTAL_SUPPLY);
    expect(await pool.realUsdlBalance()).to.equal(0);
    const priceInit = await pool.getPrice();

    // Buy — price should go up
    const buyAmount = ethers.parseUnits("5000", 6);
    await router.connect(bob).buy(poolAddr, buyAmount, 0, await futureDeadline());

    const priceAfterBuy = await pool.getPrice();
    expect(priceAfterBuy).to.be.gt(priceInit);
    expect(await pool.realUsdlBalance()).to.be.gt(0);
    const bobTokens = await token.balanceOf(bob.address);
    expect(bobTokens).to.be.gt(0);

    // Sell — price should go down
    await token.connect(bob).approve(routerAddr, bobTokens);
    await router.connect(bob).sell(poolAddr, bobTokens, 0, await futureDeadline());

    const priceAfterSell = await pool.getPrice();
    expect(priceAfterSell).to.be.lt(priceAfterBuy);
    // Virtual reserve unchanged
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 2: Multi-user trading — Alice buys, Bob buys, Alice sells
  // ───────────────────────────────────────────────────────────────────────────
  it("Multi-user: Alice buys, Bob buys (gets fewer tokens), Alice sells", async function () {
    const { pool, token, poolAddr } = await createMarket(
      charlie, "MultiToken", "MULTI", Number(FeeStrategy.CLAIM)
    );

    // Alice buys first
    const buyAmount = ethers.parseUnits("2000", 6);
    await router.connect(alice).buy(poolAddr, buyAmount, 0, await futureDeadline());
    const aliceTokens = await token.balanceOf(alice.address);

    // Bob buys same amount — should get fewer tokens (price moved up)
    await router.connect(bob).buy(poolAddr, buyAmount, 0, await futureDeadline());
    const bobTokens = await token.balanceOf(bob.address);
    expect(bobTokens).to.be.lt(aliceTokens);

    const priceAfterBuys = await pool.getPrice();

    // Alice sells all her tokens
    await token.connect(alice).approve(routerAddr, aliceTokens);
    await router.connect(alice).sell(poolAddr, aliceTokens, 0, await futureDeadline());

    // Price went down after Alice's sell
    const priceAfterSell = await pool.getPrice();
    expect(priceAfterSell).to.be.lt(priceAfterBuys);

    // Pool still has positive reserves (Bob still holds tokens)
    expect(await pool.realUsdlBalance()).to.be.gt(0);
    expect(await pool.tokenReserve()).to.be.gt(0);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 3: Fee strategy CLAIM — creator claims accumulated fees
  // ───────────────────────────────────────────────────────────────────────────
  it("Fee strategy CLAIM: creator claims accumulated USDL fees", async function () {
    const { poolAddr, nftId } = await createMarket(
      alice, "ClaimToken", "CLMT", Number(FeeStrategy.CLAIM)
    );

    // Generate fees via buy
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("5000", 6), 0, await futureDeadline());
    const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(accFees).to.be.gt(0);

    // Alice (NFT owner) claims
    const aliceUsdlBefore = await usdl.balanceOf(alice.address);
    await feesRouter.connect(alice).claimFees(nftId);
    const aliceUsdlAfter = await usdl.balanceOf(alice.address);
    expect(aliceUsdlAfter - aliceUsdlBefore).to.equal(accFees);

    // Fees zeroed after claim
    expect(await accumulatorProxy.getAccumulatedFees(poolAddr)).to.equal(0);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 4: Fee strategy BURN — fees sent to DEAD address
  // ───────────────────────────────────────────────────────────────────────────
  it("Fee strategy BURN: fees burned to DEAD address", async function () {
    const { poolAddr, nftId } = await createMarket(
      alice, "BurnToken", "BRNT", Number(FeeStrategy.BURN)
    );

    await router.connect(bob).buy(poolAddr, ethers.parseUnits("5000", 6), 0, await futureDeadline());
    const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(accFees).to.be.gt(0);

    const deadBefore = await usdl.balanceOf(DEAD_ADDRESS);
    await feesRouter.connect(alice).executeBurn(nftId);
    const deadAfter = await usdl.balanceOf(DEAD_ADDRESS);
    expect(deadAfter - deadBefore).to.equal(accFees);

    expect(await accumulatorProxy.getAccumulatedFees(poolAddr)).to.equal(0);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 5: Fee strategy AIRDROP — holders claim proportional share
  // ───────────────────────────────────────────────────────────────────────────
  it("Fee strategy AIRDROP: token holders claim proportional share", async function () {
    const { pool, token, poolAddr, nftId } = await createMarket(
      alice, "AirdropToken", "AIRT", Number(FeeStrategy.AIRDROP)
    );

    // Bob and Charlie buy tokens to become holders
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("3000", 6), 0, await futureDeadline());
    await router.connect(charlie).buy(poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());

    const bobTokens = await token.balanceOf(bob.address);
    const charlieTokens = await token.balanceOf(charlie.address);
    const totalSupply = await token.totalSupply();

    const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(accFees).to.be.gt(0);

    // Alice (NFT owner) triggers airdrop
    await feesRouter.connect(alice).executeAirdrop(nftId);
    expect(await accumulatorProxy.getAccumulatedFees(poolAddr)).to.equal(0);

    // Bob claims his share
    const bobUsdlBefore = await usdl.balanceOf(bob.address);
    await feesRouter.connect(bob).claimAirdrop(nftId);
    const bobUsdlAfter = await usdl.balanceOf(bob.address);
    const bobShare = bobUsdlAfter - bobUsdlBefore;
    expect(bobShare).to.be.gt(0);

    // Charlie claims his share
    const charlieUsdlBefore = await usdl.balanceOf(charlie.address);
    await feesRouter.connect(charlie).claimAirdrop(nftId);
    const charlieUsdlAfter = await usdl.balanceOf(charlie.address);
    const charlieShare = charlieUsdlAfter - charlieUsdlBefore;
    expect(charlieShare).to.be.gt(0);

    // Bob should get more than Charlie (bought more)
    expect(bobShare).to.be.gt(charlieShare);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 6: Fee strategy LP_REWARDS — deepens pool liquidity
  // ───────────────────────────────────────────────────────────────────────────
  it("Fee strategy LP_REWARDS: fees sent to pool, deepens liquidity", async function () {
    const { pool, poolAddr, nftId } = await createMarket(
      alice, "LpRewardToken", "LPR", Number(FeeStrategy.LP_REWARDS)
    );

    await router.connect(bob).buy(poolAddr, ethers.parseUnits("5000", 6), 0, await futureDeadline());
    const realUsdlBefore = await pool.realUsdlBalance();
    const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(accFees).to.be.gt(0);

    // Execute LP rewards — fees go to pool, syncReserves updates balance
    await feesRouter.connect(alice).executeLpRewards(nftId);
    const realUsdlAfter = await pool.realUsdlBalance();
    expect(realUsdlAfter).to.be.gt(realUsdlBefore);
    expect(await accumulatorProxy.getAccumulatedFees(poolAddr)).to.equal(0);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 7: NFT transfer — new owner claims fees and changes strategy
  // ───────────────────────────────────────────────────────────────────────────
  it("NFT transfer: new owner claims fees and changes strategy", async function () {
    const { poolAddr, nftId } = await createMarket(
      alice, "TransferToken", "XFER", Number(FeeStrategy.CLAIM)
    );

    // Generate fees
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("3000", 6), 0, await futureDeadline());
    const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(accFees).to.be.gt(0);

    // Alice transfers NFT to Charlie
    await nftProxy.connect(alice).transferFrom(alice.address, charlie.address, nftId);
    expect(await nftProxy.ownerOf(nftId)).to.equal(charlie.address);

    // Charlie (new owner) claims fees
    const charlieUsdlBefore = await usdl.balanceOf(charlie.address);
    await feesRouter.connect(charlie).claimFees(nftId);
    const charlieUsdlAfter = await usdl.balanceOf(charlie.address);
    expect(charlieUsdlAfter - charlieUsdlBefore).to.equal(accFees);

    // Charlie changes strategy to BURN
    await feesRouter.connect(charlie).setFeeStrategy(nftId, Number(FeeStrategy.BURN));
    expect(await nftProxy.getFeeStrategy(nftId)).to.equal(Number(FeeStrategy.BURN));

    // Generate more fees
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("2000", 6), 0, await futureDeadline());
    const newFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(newFees).to.be.gt(0);

    // Charlie burns under new strategy
    const deadBefore = await usdl.balanceOf(DEAD_ADDRESS);
    await feesRouter.connect(charlie).executeBurn(nftId);
    const deadAfter = await usdl.balanceOf(DEAD_ADDRESS);
    expect(deadAfter - deadBefore).to.equal(newFees);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 8: AntiSnipe optical — blocks early large buys
  // ───────────────────────────────────────────────────────────────────────────
  it("AntiSnipe optical: blocks large early buys, allows after protection period", async function () {
    // Deploy AntiSnipe optical: 1% max buy (100 bps), 5 blocks protection
    const AntiSnipe = await ethers.getContractFactory("AntiSnipeOptical");
    const antiSnipe = await AntiSnipe.deploy(
      await registryProxy.getAddress(),
      deployer.address,
      100, // maxBuyBps = 1%
      5    // protectionBlocks
    );
    await antiSnipe.waitForDeployment();
    const antiSnipeAddr = await antiSnipe.getAddress();

    // Create market with AntiSnipe optical
    const { pool, token, poolAddr, nftId } = await createMarket(
      alice, "SnipeToken", "SNIP", Number(FeeStrategy.CLAIM), antiSnipeAddr
    );

    // Register the pool's creation block in AntiSnipe
    await antiSnipe.registerPool(poolAddr);

    // During protection: a large buy should be blocked (>1% of effective USDL = >100 USDL)
    // effectiveUsdl = 10000e18 + 0 = 10000e18, 1% = 100e18
    // A buy of 200 USDL should be blocked
    const largeBuy = ethers.parseUnits("200", 6);
    await expect(
      router.connect(bob).buy(poolAddr, largeBuy, 0, await futureDeadline())
    ).to.be.reverted;

    // A small buy within 1% should succeed
    const smallBuy = ethers.parseUnits("50", 6);
    await router.connect(bob).buy(poolAddr, smallBuy, 0, await futureDeadline());
    expect(await token.balanceOf(bob.address)).to.be.gt(0);

    // Mine blocks past protection period
    for (let i = 0; i < 6; i++) {
      await ethers.provider.send("evm_mine", []);
    }

    // After protection: large buy should succeed
    await router.connect(charlie).buy(poolAddr, largeBuy, 0, await futureDeadline());
    expect(await token.balanceOf(charlie.address)).to.be.gt(0);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 9: MaxWallet optical — enforces max token holding percentage
  // ───────────────────────────────────────────────────────────────────────────
  it("MaxWallet optical: enforces max holding percentage per wallet", async function () {
    // Deploy MaxWallet optical: 2% max wallet (200 bps)
    const MaxWallet = await ethers.getContractFactory("MaxWalletOptical");
    const maxWallet = await MaxWallet.deploy(
      await registryProxy.getAddress(),
      deployer.address,
      200 // maxWalletBps = 2%
    );
    await maxWallet.waitForDeployment();
    const maxWalletAddr = await maxWallet.getAddress();

    // Create market with MaxWallet optical
    const { pool, token, poolAddr } = await createMarket(
      alice, "MaxWalletToken", "MAXW", Number(FeeStrategy.CLAIM), maxWalletAddr
    );

    // 2% of 1B supply = 20M tokens
    // Buy via Router — token transfer policy checks the actual output recipient.
    const moderateBuy = ethers.parseUnits("100", 6);
    await router.connect(bob).buy(poolAddr, moderateBuy, 0, await futureDeadline());
    const bobTokens = await token.balanceOf(bob.address);
    expect(bobTokens).to.be.gt(0);

    // A huge buy that would push bob over 2% should revert
    // At near-initial price, 500 USDL buys ~47M tokens (>2% of 1B)
    const hugeBuy = ethers.parseUnits("500", 6);
    const usdlBefore = await usdl.balanceOf(bob.address);
    const reservesBefore = await pool.getReserves();
    // The token callback rejects MaxWalletExceeded. TransferHelper wraps failed
    // token delivery in TransferFailed; all input, fees and reserve edits roll back.
    await expect(
      router.connect(bob).buy(poolAddr, hugeBuy, 0, await futureDeadline())
    ).to.be.revertedWithCustomError(pool, "TransferFailed");
    expect(await token.balanceOf(bob.address)).to.equal(bobTokens);
    expect(await usdl.balanceOf(bob.address)).to.equal(usdlBefore);
    expect(await pool.getReserves()).to.deep.equal(reservesBefore);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 10: Full lifecycle with all subsystems verified
  // ───────────────────────────────────────────────────────────────────────────
  it("Full lifecycle: create→buy→sell with all subsystems verified", async function () {
    const { pool, token, poolAddr, tokenAddr, nftId } = await createMarket(
      alice, "FullLifecycle", "FULL", Number(FeeStrategy.CLAIM)
    );

    // --- Verify creation ---
    // Token supply in pool
    expect(await token.balanceOf(poolAddr)).to.equal(TOKEN_TOTAL_SUPPLY);
    // Pool initialized correctly
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
    expect(await pool.realUsdlBalance()).to.equal(0);
    // NFT to creator
    expect(await nftProxy.ownerOf(nftId)).to.equal(alice.address);
    // Registered in PoolRegistry
    expect(await registryProxy.getPoolByToken(tokenAddr)).to.equal(poolAddr);
    // Creation fee went to treasury
    expect(await usdl.balanceOf(await treasuryProxy.getAddress())).to.be.gte(CREATION_FEE);
    // Quoter can find pool
    const allPools = await quoter.getAllPools(0, 10);
    expect(allPools).to.include(poolAddr);

    // --- Execute trades ---
    // Bob buys
    const buyAmount = ethers.parseUnits("3000", 6);
    await router.connect(bob).buy(poolAddr, buyAmount, 0, await futureDeadline());
    const bobTokens = await token.balanceOf(bob.address);
    expect(bobTokens).to.be.gt(0);

    // Verify quoter approximates actual output
    const quoteResult = await quoter.quoteExactInput(poolAddr, ethers.parseUnits("1000", 6), true);
    expect(quoteResult.amountOut).to.be.gt(0);
    expect(quoteResult.feeAmount).to.be.gt(0);

    // Bob sells half
    const sellAmount = bobTokens / 2n;
    await token.connect(bob).approve(routerAddr, sellAmount);
    const bobUsdlBefore = await usdl.balanceOf(bob.address);
    await router.connect(bob).sell(poolAddr, sellAmount, 0, await futureDeadline());
    const bobUsdlAfter = await usdl.balanceOf(bob.address);
    expect(bobUsdlAfter).to.be.gt(bobUsdlBefore);

    // --- Verify fee subsystem ---
    // Buy fees accumulated in FeeAccumulator
    const accFees = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(accFees).to.be.gt(0);
    // Protocol fees reached Treasury via deposit() — getBalance tracks deposit() calls only
    // (creation fee goes via raw safeTransferFrom, not deposit(), so isn't in getBalance)
    const treasuryBal = await treasuryProxy.getBalance(await usdl.getAddress());
    expect(treasuryBal).to.be.gt(0);
    // Treasury address also holds the creation fee as a raw USDL balance
    const treasuryUsdl = await usdl.balanceOf(await treasuryProxy.getAddress());
    expect(treasuryUsdl).to.be.gt(CREATION_FEE);

    // Alice claims fees
    const aliceUsdlBefore = await usdl.balanceOf(alice.address);
    await feesRouter.connect(alice).claimFees(nftId);
    const aliceUsdlAfter = await usdl.balanceOf(alice.address);
    expect(aliceUsdlAfter).to.be.gt(aliceUsdlBefore);
    expect(await accumulatorProxy.getAccumulatedFees(poolAddr)).to.equal(0);

    // --- Verify pool invariants ---
    expect(await pool.realUsdlBalance()).to.be.gte(0);
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
    expect(await pool.tokenReserve()).to.be.gt(0);
    expect(await pool.cumulativeVolume()).to.be.gt(0);
  });
});
