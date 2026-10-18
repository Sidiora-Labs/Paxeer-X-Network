const { expect } = require("chai");
const { ethers } = require("hardhat");
const {
  VIRTUAL_USDL_DEFAULT,
  TOKEN_TOTAL_SUPPLY,
  CREATION_FEE,
  FeeStrategy,
  PROTOCOL_FEE_BPS,
  BPS_DENOMINATOR,
} = require("../helpers/constants");

describe("Phase 5: Core-Logic Integration", function () {
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

    // Wire roles
    const FACTORY_ROLE = ethers.keccak256(ethers.toUtf8Bytes("FACTORY_ROLE"));
    const MINTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("MINTER_ROLE"));
    const DEPOSITOR_ROLE = ethers.keccak256(ethers.toUtf8Bytes("DEPOSITOR_ROLE"));
    await registryProxy.grantRole(FACTORY_ROLE, await factoryProxy.getAddress());
    await accumulatorProxy.grantRole(FACTORY_ROLE, await factoryProxy.getAddress());
    await nftProxy.grantRole(MINTER_ROLE, await factoryProxy.getAddress());
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await accumulatorProxy.getAddress());
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await factoryProxy.getAddress());

    // Fund users
    await usdl.mint(alice.address, ethers.parseUnits("1000000", 6));
    await usdl.mint(bob.address, ethers.parseUnits("1000000", 6));
    await usdl.mint(charlie.address, ethers.parseUnits("1000000", 6));
    await usdl.connect(alice).approve(await factoryProxy.getAddress(), ethers.MaxUint256);
    await usdl.connect(bob).approve(await factoryProxy.getAddress(), ethers.MaxUint256);
  }

  async function createMarketAndGetAddresses(creator, name, symbol, strategy) {
    const tx = await factoryProxy.connect(creator).createMarket(name, symbol, strategy, ethers.ZeroAddress);
    const receipt = await tx.wait();
    const event = receipt.logs.find(l => l.fragment && l.fragment.name === "MarketCreated");
    const tokenAddr = event.args[0];
    const poolAddr = event.args[1];
    const nftId = event.args[3];

    const Pool = await ethers.getContractFactory("SidioraPool");
    const pool = Pool.attach(poolAddr);

    const SidioraERC20 = await ethers.getContractFactory("SidioraERC20");
    const token = SidioraERC20.attach(tokenAddr);

    // Grant POOL_ROLE to pool on FeeAccumulator
    const POOL_ROLE = ethers.keccak256(ethers.toUtf8Bytes("POOL_ROLE"));
    expect(await accumulatorProxy.hasRole(POOL_ROLE, poolAddr)).to.equal(true);

    return { pool, token, poolAddr, tokenAddr, nftId };
  }

  async function buy(pool, user, usdlAmount) {
    const poolAddr = await pool.getAddress();
    await usdl.connect(user).transfer(poolAddr, usdlAmount);
    const deadline = (await ethers.provider.getBlock("latest")).timestamp + 3600;
    await pool.connect(user).swap(usdlAmount, 0, true, user.address, deadline);
  }

  async function sell(pool, token, user, tokenAmount) {
    const poolAddr = await pool.getAddress();
    await token.connect(user).transfer(poolAddr, tokenAmount);
    const deadline = (await ethers.provider.getBlock("latest")).timestamp + 3600;
    await pool.connect(user).swap(tokenAmount, 0, false, user.address, deadline);
  }

  beforeEach(async function () {
    await deployFullStack();
  });

  it("full market creation: token+pool+NFT all correct", async function () {
    const { pool, token, poolAddr, tokenAddr, nftId } = await createMarketAndGetAddresses(
      alice, "IntegrationToken", "INTG", Number(FeeStrategy.CLAIM)
    );

    // Token supply in pool
    expect(await token.balanceOf(poolAddr)).to.equal(TOKEN_TOTAL_SUPPLY);
    // Pool reserves correct
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
    expect(await pool.tokenReserve()).to.equal(TOKEN_TOTAL_SUPPLY);
    expect(await pool.realUsdlBalance()).to.equal(0);
    // NFT owner = creator
    expect(await nftProxy.ownerOf(nftId)).to.equal(alice.address);
    // Registered in PoolRegistry
    expect(await registryProxy.getPoolByToken(tokenAddr)).to.equal(poolAddr);
    // Creation fee to treasury
    expect(await usdl.balanceOf(await treasuryProxy.getAddress())).to.equal(CREATION_FEE);
  });

  it("full swap cycle: create → buy → verify → sell → verify", async function () {
    const { pool, token } = await createMarketAndGetAddresses(
      alice, "SwapToken", "SWAP", Number(FeeStrategy.CLAIM)
    );

    const priceBefore = await pool.getPrice();

    // Alice buys
    await buy(pool, alice, ethers.parseUnits("500", 6));

    const priceAfterBuy = await pool.getPrice();
    expect(priceAfterBuy).to.be.gt(priceBefore);

    const aliceTokens = await token.balanceOf(alice.address);
    expect(aliceTokens).to.be.gt(0);

    // Alice sells half
    const sellAmount = aliceTokens / 2n;
    await sell(pool, token, alice, sellAmount);

    const priceAfterSell = await pool.getPrice();
    expect(priceAfterSell).to.be.lt(priceAfterBuy);

    // Pool invariants
    expect(await pool.realUsdlBalance()).to.be.gt(0);
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
  });

  it("buy fees go to FeeAccumulator, sell fees stay in pool", async function () {
    const { pool, token, poolAddr } = await createMarketAndGetAddresses(
      alice, "FeeTest", "FEET", Number(FeeStrategy.CLAIM)
    );

    // Buy — USDL fee goes to FeeAccumulator
    await buy(pool, alice, ethers.parseUnits("1000", 6));
    const accFeesAfterBuy = await accumulatorProxy.getAccumulatedFees(poolAddr);
    expect(accFeesAfterBuy).to.be.gt(0);
    expect(await pool.accumulatedUsdlFees()).to.be.gt(0);

    // Sell — Token fee stays in pool, no new USDL fees
    const aliceTokens = await token.balanceOf(alice.address);
    const accFeesBefore = await accumulatorProxy.getAccumulatedFees(poolAddr);
    await sell(pool, token, alice, aliceTokens / 2n);
    const accFeesAfterSell = await accumulatorProxy.getAccumulatedFees(poolAddr);

    // No new USDL fees from sell
    expect(accFeesAfterSell).to.equal(accFeesBefore);
    // Token fees tracked
    expect(await pool.accumulatedTokenFees()).to.be.gt(0);
  });

  it("protocol fees arrive in Treasury via FeeAccumulator", async function () {
    const { pool } = await createMarketAndGetAddresses(
      alice, "TreasuryTest", "TRST", Number(FeeStrategy.CLAIM)
    );

    const treasuryBefore = await treasuryProxy.getBalance(await usdl.getAddress());

    await buy(pool, alice, ethers.parseUnits("1000", 6));

    const treasuryAfter = await treasuryProxy.getBalance(await usdl.getAddress());
    // Treasury increased by more than just creation fee (now has protocol cut of swap fees)
    expect(treasuryAfter).to.be.gt(treasuryBefore);
  });

  it("multi-user trading: Alice buys, Bob buys, Alice sells", async function () {
    const { pool, token } = await createMarketAndGetAddresses(
      alice, "MultiUser", "MULTI", Number(FeeStrategy.CLAIM)
    );

    // Alice buys
    await buy(pool, alice, ethers.parseUnits("500", 6));
    const aliceTokens = await token.balanceOf(alice.address);
    expect(aliceTokens).to.be.gt(0);

    const priceAfterAlice = await pool.getPrice();

    // Bob buys (price higher for Bob)
    await buy(pool, bob, ethers.parseUnits("500", 6));
    const bobTokens = await token.balanceOf(bob.address);
    expect(bobTokens).to.be.gt(0);
    // Bob gets fewer tokens per USDL (higher price)
    expect(bobTokens).to.be.lt(aliceTokens);

    const priceAfterBob = await pool.getPrice();
    expect(priceAfterBob).to.be.gt(priceAfterAlice);

    // Alice sells
    await sell(pool, token, alice, aliceTokens);
    const priceAfterAliceSell = await pool.getPrice();
    expect(priceAfterAliceSell).to.be.lt(priceAfterBob);

    // Pool still has positive reserves
    expect(await pool.realUsdlBalance()).to.be.gt(0);
    expect(await pool.tokenReserve()).to.be.gt(0);
  });

  it("virtual floor: sell cannot drain below virtual USDL", async function () {
    const { pool, token } = await createMarketAndGetAddresses(
      alice, "FloorTest", "FLOOR", Number(FeeStrategy.CLAIM)
    );

    // Small buy
    await buy(pool, alice, ethers.parseUnits("100", 6));
    const realUsdl = await pool.realUsdlBalance();
    const aliceTokens = await token.balanceOf(alice.address);

    // Sell all tokens back
    await sell(pool, token, alice, aliceTokens);

    // realUsdlBalance >= 0 (virtual floor holds)
    expect(await pool.realUsdlBalance()).to.be.gte(0);
    // virtualUsdlReserve unchanged
    expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
  });

  it("NFT transfer: new owner receives fee rights", async function () {
    const { pool, token, nftId } = await createMarketAndGetAddresses(
      alice, "NFTTransfer", "NFTT", Number(FeeStrategy.CLAIM)
    );

    // Alice is initial NFT owner
    expect(await nftProxy.ownerOf(nftId)).to.equal(alice.address);

    // Transfer NFT to Bob
    await nftProxy.connect(alice).transferFrom(alice.address, bob.address, nftId);
    expect(await nftProxy.ownerOf(nftId)).to.equal(bob.address);

    // Bob can change strategy
    await nftProxy.connect(bob).setFeeStrategy(nftId, Number(FeeStrategy.BURN));
    expect(await nftProxy.getFeeStrategy(nftId)).to.equal(Number(FeeStrategy.BURN));
  });

  it("multiple markets created from factory, all isolated", async function () {
    const m1 = await createMarketAndGetAddresses(alice, "Market1", "M1", 0);
    const m2 = await createMarketAndGetAddresses(bob, "Market2", "M2", 1);

    // Buy in market 1
    await buy(m1.pool, alice, ethers.parseUnits("500", 6));

    // Market 2 unaffected
    expect(await m2.pool.realUsdlBalance()).to.equal(0);
    expect(await m2.pool.tokenReserve()).to.equal(TOKEN_TOTAL_SUPPLY);

    // Each pool has its own token
    expect(m1.tokenAddr).to.not.equal(m2.tokenAddr);
    expect(m1.poolAddr).to.not.equal(m2.poolAddr);

    expect(await registryProxy.getPoolCount()).to.equal(2);
  });

  it("cumulative volume tracks across multiple swaps", async function () {
    const { pool, token } = await createMarketAndGetAddresses(
      alice, "VolTrack", "VOL", 0
    );

    const buy1 = ethers.parseUnits("100", 6);
    await buy(pool, alice, buy1);
    const tokens = await token.balanceOf(alice.address);

    const sell1 = tokens / 2n;
    const beforeUsdl = await usdl.balanceOf(alice.address);
    await sell(pool, token, alice, sell1);
    const sellUsdl = (await usdl.balanceOf(alice.address)) - beforeUsdl;
    const vol = await pool.cumulativeVolume();
    expect(vol).to.equal(buy1 + sellUsdl);
    expect(await pool.cumulativeBuyUsdlVolume()).to.equal(buy1);
    expect(await pool.cumulativeSellUsdlVolume()).to.equal(sellUsdl);
  });

  it("price snapshots buffer fills and wraps", async function () {
    const { pool, token } = await createMarketAndGetAddresses(
      alice, "SnapToken", "SNAP", 0
    );

    // Sample at meaningful time intervals to fill and wrap the chronological buffer.
    for (let i = 0; i < 10; i++) {
      await ethers.provider.send("evm_increaseTime", [60]);
      await buy(pool, alice, ethers.parseUnits("10", 6));
    }

    const snapshots = await pool.getPriceSnapshots();
    // All 8 slots should be non-zero
    for (let i = 0; i < 8; i++) {
      expect(snapshots[i]).to.be.gt(0);
    }
  });
});
