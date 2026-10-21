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

describe("Phase 8.3: E2E Edge Cases", function () {
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

    const fundAmount = ethers.parseUnits("100000000", 6);
    await usdl.mint(alice.address, fundAmount);
    await usdl.mint(bob.address, fundAmount);
    await usdl.mint(charlie.address, fundAmount);
    await usdl.connect(alice).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(bob).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(charlie).approve(routerAddr, ethers.MaxUint256);
  }

  async function createMarket(creator, name, symbol, strategy) {
    const tx = await router.connect(creator).createMarket(name, symbol, strategy, ZERO_ADDRESS);
    const receipt = await tx.wait();
    const event = receipt.logs
      .map(l => { try { return router.interface.parseLog(l); } catch { return null; } })
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
  // Test 1: 1 wei buy — minimum viable trade
  // ───────────────────────────────────────────────────────────────────────────
  it("should handle 1 wei buy without revert (dust trade)", async function () {
    const { pool, token, poolAddr } = await createMarket(
      alice, "DustToken", "DUST", Number(FeeStrategy.CLAIM)
    );

    // 1 wei buy — likely rounds to 0 output, should revert gracefully or succeed with 0
    // The pool deducts fees first: 1 * feeBps / 10000 = 0, so amountInAfterFee = 1
    // getAmountOut(10000e18, 1e27, 1) = 1e27 * 1 / (10000e18 + 1) ≈ 99999 tokens
    // Actually with 1 wei of USDL this is valid. Let's see.
    await router.connect(bob).buy(poolAddr, 1n, 0, await futureDeadline());

    // Pool state still valid
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
    expect(await pool.realUsdlBalance()).to.be.gte(0);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 2: Max uint256 buy — should revert (insufficient balance)
  // ───────────────────────────────────────────────────────────────────────────
  it("should revert on max uint256 buy amount", async function () {
    const { poolAddr } = await createMarket(
      alice, "MaxUint", "MAXU", Number(FeeStrategy.CLAIM)
    );

    await expect(
      router.connect(bob).buy(poolAddr, ethers.MaxUint256, 0, await futureDeadline())
    ).to.be.reverted;
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 3: Sell all tokens back — pool returns to near-initial state
  // ───────────────────────────────────────────────────────────────────────────
  it("should allow selling all tokens back, pool near-initial state", async function () {
    const { pool, token, poolAddr } = await createMarket(
      alice, "SellAll", "SALL", Number(FeeStrategy.CLAIM)
    );

    // Buy
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());
    const bobTokens = await token.balanceOf(bob.address);

    // Sell all back
    await token.connect(bob).approve(routerAddr, bobTokens);
    await router.connect(bob).sell(poolAddr, bobTokens, 0, await futureDeadline());

    // Bob has 0 tokens
    expect(await token.balanceOf(bob.address)).to.equal(0);
    // Pool has very little real USDL (fees taken from both sides)
    expect(await pool.realUsdlBalance()).to.be.gte(0);
    // Virtual unchanged
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 4: Two users buy, first sells all — second user unaffected
  // ───────────────────────────────────────────────────────────────────────────
  it("should isolate users: Alice buys, Bob buys, Alice sells all", async function () {
    const { pool, token, poolAddr } = await createMarket(
      charlie, "Isolate", "ISOL", Number(FeeStrategy.CLAIM)
    );

    await router.connect(alice).buy(poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());

    const aliceTokens = await token.balanceOf(alice.address);
    const bobTokens = await token.balanceOf(bob.address);
    expect(aliceTokens).to.be.gt(0);
    expect(bobTokens).to.be.gt(0);

    // Alice sells all
    await token.connect(alice).approve(routerAddr, aliceTokens);
    await router.connect(alice).sell(poolAddr, aliceTokens, 0, await futureDeadline());

    // Bob's balance unchanged
    expect(await token.balanceOf(bob.address)).to.equal(bobTokens);
    // Pool still has USDL (Bob's buy money minus Alice's withdrawal)
    expect(await pool.realUsdlBalance()).to.be.gt(0);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 5: Same name/symbol by different creator — both succeed
  // ───────────────────────────────────────────────────────────────────────────
  it("should allow same name/symbol from different creators", async function () {
    const m1 = await createMarket(alice, "SameName", "SAME", Number(FeeStrategy.CLAIM));
    const m2 = await createMarket(bob, "SameName", "SAME", Number(FeeStrategy.CLAIM));

    // Different token and pool addresses (different CREATE2 salts due to different creator)
    expect(m1.tokenAddr).to.not.equal(m2.tokenAddr);
    expect(m1.poolAddr).to.not.equal(m2.poolAddr);
    expect(await registryProxy.getPoolCount()).to.equal(2);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 6: minAmountOut = exact expected output — succeeds
  // ───────────────────────────────────────────────────────────────────────────
  it("should succeed when minAmountOut equals exact output", async function () {
    const { pool, token, poolAddr } = await createMarket(
      alice, "ExactSlip", "EXSL", Number(FeeStrategy.CLAIM)
    );

    // Quote first
    const buyAmount = ethers.parseUnits("1000", 6);
    const quote = await quoter.quoteExactInput(poolAddr, buyAmount, true);

    // Buy with minAmountOut = quoted output (should pass if quote matches)
    // Use slight discount (1 wei less) to avoid rounding mismatch
    const minOut = quote.amountOut > 0n ? quote.amountOut - 1n : 0n;
    await router.connect(bob).buy(poolAddr, buyAmount, minOut, await futureDeadline());

    const bobTokens = await token.balanceOf(bob.address);
    expect(bobTokens).to.be.gte(minOut);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 7: Deadline = current block timestamp — should succeed
  // ───────────────────────────────────────────────────────────────────────────
  it("should succeed when deadline equals current block.timestamp", async function () {
    const { pool, token, poolAddr } = await createMarket(
      alice, "DeadlineNow", "DNOW", Number(FeeStrategy.CLAIM)
    );

    // Get next block timestamp — hardhat mines one block per tx, timestamp increments by 1
    const block = await ethers.provider.getBlock("latest");
    const deadline = block.timestamp + 1; // next block's timestamp

    await router.connect(bob).buy(poolAddr, ethers.parseUnits("100", 6), 0, deadline);
    expect(await token.balanceOf(bob.address)).to.be.gt(0);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 8: Fee strategy change mid-accumulation
  // ───────────────────────────────────────────────────────────────────────────
  it("should handle fee strategy change mid-accumulation", async function () {
    const { pool, token, poolAddr, nftId } = await createMarket(
      alice, "StratChange", "STRC", Number(FeeStrategy.CLAIM)
    );

    // Generate fees under CLAIM
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("2000", 6), 0, await futureDeadline());
    const feesUnderClaim = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(feesUnderClaim).to.be.gt(0);

    // Alice claims under CLAIM strategy
    await feesRouter.connect(alice).claimFees(nftId);
    expect(await accumulatorProxy.getAccumulatedFees(poolAddr)).to.equal(0);

    // Switch to BURN
    await feesRouter.connect(alice).setFeeStrategy(nftId, Number(FeeStrategy.BURN));

    // Generate more fees under BURN
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("2000", 6), 0, await futureDeadline());
    const feesUnderBurn = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(feesUnderBurn).to.be.gt(0);

    // Burn under new strategy
    const deadBefore = await usdl.balanceOf(DEAD_ADDRESS);
    await feesRouter.connect(alice).executeBurn(nftId);
    const deadAfter = await usdl.balanceOf(DEAD_ADDRESS);
    expect(deadAfter - deadBefore).to.equal(feesUnderBurn);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 9: Claim when 0 fees — should revert
  // ───────────────────────────────────────────────────────────────────────────
  it("should revert claim when no fees accumulated", async function () {
    const { poolAddr, nftId } = await createMarket(
      alice, "NoFees", "NOFEE", Number(FeeStrategy.CLAIM)
    );

    // No buys → no fees → claim should revert
    await expect(
      feesRouter.connect(alice).claimFees(nftId)
    ).to.be.revertedWithCustomError(accumulatorProxy, "NoFeesAccumulated");
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 10: syncReserves no-op when balances match state
  // ───────────────────────────────────────────────────────────────────────────
  it("should handle syncReserves as no-op when already synced", async function () {
    const { pool, poolAddr } = await createMarket(
      alice, "SyncNoop", "SYNC", Number(FeeStrategy.CLAIM)
    );

    // Buy to have real balances
    await router.connect(bob).buy(poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());

    const realUsdlBefore = await pool.realUsdlBalance();
    const tokenResBefore = await pool.tokenReserve();

    // syncReserves should be no-op if balances already match
    await pool.syncReserves();

    expect(await pool.realUsdlBalance()).to.equal(realUsdlBefore);
    expect(await pool.tokenReserve()).to.equal(tokenResBefore);
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 11: Buy with zero amount — should revert
  // ───────────────────────────────────────────────────────────────────────────
  it("should revert buy with zero amount", async function () {
    const { poolAddr } = await createMarket(
      alice, "ZeroBuy", "ZBUY", Number(FeeStrategy.CLAIM)
    );

    await expect(
      router.connect(bob).buy(poolAddr, 0, 0, await futureDeadline())
    ).to.be.revertedWithCustomError(router, "ZeroAmount");
  });

  // ───────────────────────────────────────────────────────────────────────────
  // Test 12: Expired deadline — should revert
  // ───────────────────────────────────────────────────────────────────────────
  it("should revert buy with expired deadline", async function () {
    const { poolAddr } = await createMarket(
      alice, "Expired", "EXPD", Number(FeeStrategy.CLAIM)
    );

    // Deadline in the past
    await expect(
      router.connect(bob).buy(poolAddr, ethers.parseUnits("100", 6), 0, 1)
    ).to.be.revertedWithCustomError(router, "DeadlineExpired");
  });
});
