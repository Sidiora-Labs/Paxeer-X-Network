const { expect } = require("chai");
const { ethers } = require("hardhat");
const {
  VIRTUAL_USDL_DEFAULT,
  TOKEN_TOTAL_SUPPLY,
  CREATION_FEE,
  FeeStrategy,
  ZERO_ADDRESS,
} = require("../helpers/constants");

describe("Quoter", function () {
  let quoter, router, routerAddr;
  let factoryProxy, configProxy, treasuryProxy, registryProxy, accumulatorProxy, nftProxy;
  let eventEmitter, beacon, usdl;
  let deployer, alice, bob;

  before(async function () {
    [deployer, alice, bob] = await ethers.getSigners();
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

    // Wire roles
    const FACTORY_ROLE = ethers.keccak256(ethers.toUtf8Bytes("FACTORY_ROLE"));
    const MINTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("MINTER_ROLE"));
    const DEPOSITOR_ROLE = ethers.keccak256(ethers.toUtf8Bytes("DEPOSITOR_ROLE"));
    const ROUTER_ROLE = ethers.keccak256(ethers.toUtf8Bytes("ROUTER_ROLE"));
    await registryProxy.grantRole(FACTORY_ROLE, await factoryProxy.getAddress());
    await accumulatorProxy.grantRole(FACTORY_ROLE, await factoryProxy.getAddress());
    await nftProxy.grantRole(MINTER_ROLE, await factoryProxy.getAddress());
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await accumulatorProxy.getAddress());
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, await factoryProxy.getAddress());
    await factoryProxy.grantRole(ROUTER_ROLE, routerAddr);
    await treasuryProxy.grantRole(DEPOSITOR_ROLE, routerAddr);

    // Fund users
    await usdl.mint(alice.address, ethers.parseUnits("1000000", 6));
    await usdl.mint(bob.address, ethers.parseUnits("1000000", 6));
    await usdl.connect(alice).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(bob).approve(routerAddr, ethers.MaxUint256);
  }

  async function createMarket(creator, name, symbol) {
    const tx = await router.connect(creator).createMarket(name, symbol, 0, ZERO_ADDRESS);
    const receipt = await tx.wait();
    const routerIface = router.interface;
    const event = receipt.logs
      .map(l => { try { return routerIface.parseLog(l); } catch { return null; } })
      .find(e => e && e.name === "MarketCreated");

    const tokenAddr = event.args[0];
    const poolAddr = event.args[1];

    const Pool = await ethers.getContractFactory("SidioraPool");
    const pool = Pool.attach(poolAddr);
    const SidioraERC20 = await ethers.getContractFactory("SidioraERC20");
    const token = SidioraERC20.attach(tokenAddr);

    const POOL_ROLE = ethers.keccak256(ethers.toUtf8Bytes("POOL_ROLE"));
    await accumulatorProxy.grantRole(POOL_ROLE, poolAddr);

    return { pool, token, poolAddr, tokenAddr };
  }

  async function futureDeadline() {
    const block = await ethers.provider.getBlock("latest");
    return block.timestamp + 3600;
  }

  beforeEach(async function () {
    await deployFullStack();
  });

  describe("Initialization", function () {
    it("should set references correctly", async function () {
      expect(await quoter.poolRegistry()).to.equal(await registryProxy.getAddress());
      expect(await quoter.protocolConfig()).to.equal(await configProxy.getAddress());
    });
  });

  describe("quoteExactInput", function () {
    it("should quote buy correctly", async function () {
      const { poolAddr } = await createMarket(alice, "QuoteToken", "QT");
      const amountIn = ethers.parseUnits("500", 6);

      const result = await quoter.quoteExactInput(poolAddr, amountIn, true);

      expect(result.amountOut).to.be.gt(0);
      expect(result.feeAmount).to.be.gt(0);
      expect(result.priceImpactBps).to.be.gt(0);
    });

    it("should quote sell correctly", async function () {
      const { pool, token, poolAddr } = await createMarket(alice, "QuoteSell", "QS");

      // Buy first to have tokens and USDL in pool
      await router.connect(bob).buy(poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());

      const sellAmount = ethers.parseUnits("1000000", 6); // some tokens
      const result = await quoter.quoteExactInput(poolAddr, sellAmount, false);

      expect(result.amountOut).to.be.gt(0);
      expect(result.feeAmount).to.be.gt(0);
    });

    it("should quote match actual swap output (approximately)", async function () {
      const { pool, poolAddr } = await createMarket(alice, "QuoteMatch", "QM");
      const buyAmount = ethers.parseUnits("500", 6);

      // Get quote
      const quote = await quoter.quoteExactInput(poolAddr, buyAmount, true);

      // Execute actual buy
      await router.connect(bob).buy(poolAddr, buyAmount, 0, await futureDeadline());

      expect(await pool.tokenReserve()).to.equal(TOKEN_TOTAL_SUPPLY - quote.amountOut);
      expect(await pool.currentFeeBps()).to.equal((await quoter.getPoolStats(poolAddr)).currentFeeBps);

    });
  });

  describe("getPoolPrice", function () {
    it("should return initial price", async function () {
      const { poolAddr } = await createMarket(alice, "PriceToken", "PT");
      const price = await quoter.getPoolPrice(poolAddr);
      expect(price).to.be.gt(0);
    });

    it("should return higher price after buy", async function () {
      const { poolAddr } = await createMarket(alice, "PriceBuy", "PB");

      const priceBefore = await quoter.getPoolPrice(poolAddr);
      await router.connect(bob).buy(poolAddr, ethers.parseUnits("500", 6), 0, await futureDeadline());
      const priceAfter = await quoter.getPoolPrice(poolAddr);

      expect(priceAfter).to.be.gt(priceBefore);
    });
  });

  describe("getPoolStats", function () {
    it("should return comprehensive pool stats", async function () {
      const { poolAddr } = await createMarket(alice, "StatsToken", "ST");

      await router.connect(bob).buy(poolAddr, ethers.parseUnits("500", 6), 0, await futureDeadline());

      const stats = await quoter.getPoolStats(poolAddr);

      expect(stats.virtualUsdl).to.equal(VIRTUAL_USDL_DEFAULT);
      expect(stats.realUsdl).to.be.gt(0);
      expect(stats.tokenReserve).to.be.lt(TOKEN_TOTAL_SUPPLY);
      expect(stats.cumulativeVolume).to.be.gt(0);
      expect(stats.currentFeeBps).to.be.gt(0);
      expect(stats.poolAge).to.be.gte(0);
      expect(stats.price).to.be.gt(0);
      expect(stats.marketCap).to.be.gt(0);
    });
  });

  describe("getMarketCap", function () {
    it("should return market cap based on price * totalSupply", async function () {
      const { poolAddr } = await createMarket(alice, "CapToken", "CAP");
      const marketCap = await quoter.getMarketCap(poolAddr);
      expect(marketCap).to.be.gt(0);
    });
  });

  describe("getAllPools / getPoolsByCreator", function () {
    it("should return pools from registry", async function () {
      await createMarket(alice, "Pool1", "P1");
      await createMarket(bob, "Pool2", "P2");

      const allPools = await quoter.getAllPools(0, 10);
      expect(allPools.length).to.equal(2);

      const alicePools = await quoter.getPoolsByCreator(alice.address);
      expect(alicePools.length).to.equal(1);
    });
  });

  describe("quoteMultihop", function () {
    let marketA, marketB;
    let tokenA, tokenB, tokenAddrA, tokenAddrB, poolAddrA, poolAddrB;

    beforeEach(async function () {
      marketA = await createMarket(alice, "HopA", "HA");
      marketB = await createMarket(alice, "HopB", "HB");
      tokenA = marketA.token;
      tokenB = marketB.token;
      tokenAddrA = marketA.tokenAddr;
      tokenAddrB = marketB.tokenAddr;
      poolAddrA = marketA.poolAddr;
      poolAddrB = marketB.poolAddr;

      // Add USDL liquidity to both pools via buys
      await router.connect(bob).buy(poolAddrA, ethers.parseUnits("5000", 6), 0, await futureDeadline());
      await router.connect(bob).buy(poolAddrB, ethers.parseUnits("5000", 6), 0, await futureDeadline());
    });

    it("should return valid multihop quote", async function () {
      const amountIn = ethers.parseUnits("1000000", 6); // some TokenA

      const result = await quoter.quoteMultihop(tokenAddrA, tokenAddrB, amountIn);

      expect(result.amountOut).to.be.gt(0);
      expect(result.intermediateUsdl).to.be.gt(0);
      expect(result.sellFeeAmount).to.be.gt(0);
      expect(result.buyFeeAmount).to.be.gt(0);
      expect(result.poolA).to.equal(poolAddrA);
      expect(result.poolB).to.equal(poolAddrB);
    });

    it("should return price impact on both legs", async function () {
      const amountIn = ethers.parseUnits("100000000", 6); // large amount for noticeable impact

      const result = await quoter.quoteMultihop(tokenAddrA, tokenAddrB, amountIn);

      expect(result.sellPriceImpactBps).to.be.gt(0);
      expect(result.buyPriceImpactBps).to.be.gt(0);
      expect(result.combinedPriceImpactBps).to.equal(
        result.sellPriceImpactBps + result.buyPriceImpactBps
      );
    });

    it("should revert with same token", async function () {
      await expect(
        quoter.quoteMultihop(tokenAddrA, tokenAddrA, ethers.parseUnits("100", 6))
      ).to.be.revertedWithCustomError(quoter, "ZeroAddress");
    });

    it("should revert when token has no pool", async function () {
      const MockERC20 = await ethers.getContractFactory("MockERC20");
      const fakeToken = await MockERC20.deploy("Fake", "FAKE", 6);
      await fakeToken.waitForDeployment();

      await expect(
        quoter.quoteMultihop(await fakeToken.getAddress(), tokenAddrB, ethers.parseUnits("100", 6))
      ).to.be.revertedWithCustomError(quoter, "ZeroAddress");
    });

    it("intermediate USDL should be less than a direct sell quote", async function () {
      // The intermediate USDL from quoteMultihop sell leg should match
      // a direct quoteExactInput sell on poolA
      const amountIn = ethers.parseUnits("1000000", 6);

      const multihopResult = await quoter.quoteMultihop(tokenAddrA, tokenAddrB, amountIn);
      const directSell = await quoter.quoteExactInput(poolAddrA, amountIn, false);

      // The sell legs should produce the same intermediate USDL
      expect(multihopResult.intermediateUsdl).to.equal(directSell.amountOut);
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const NewQuoter = await ethers.getContractFactory("Quoter");
      const newImpl = await NewQuoter.deploy();
      await newImpl.waitForDeployment();
      await quoter.upgradeToAndCall(await newImpl.getAddress(), "0x");
    });

    it("should revert upgrade by non-admin", async function () {
      const NewQuoter = await ethers.getContractFactory("Quoter");
      const newImpl = await NewQuoter.deploy();
      await newImpl.waitForDeployment();
      await expect(
        quoter.connect(alice).upgradeToAndCall(await newImpl.getAddress(), "0x")
      ).to.be.revertedWithCustomError(quoter, "MissingRole");
    });
  });
  describe("audit remediation: executable quote boundaries", function () {
    it("rejects sell quotations that would spend virtual backing", async function () {
      const { poolAddr } = await createMarket(alice, "NoBacking", "NB");
      await expect(quoter.quoteExactInput(poolAddr, ethers.parseUnits("100", 6), false))
        .to.be.revertedWithCustomError(quoter, "UnexecutableQuote");
    });
    it("uses full gross sell input in post-trade price impact", async function () {
      const { poolAddr, pool, token } = await createMarket(alice, "SellImpact", "SI");
      await router.connect(bob).buy(poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());
      const amount = (await token.balanceOf(bob.address)) / 2n;
      const beforePrice = await pool.getPrice();
      const quote = await quoter.quoteExactInput(poolAddr, amount, false);
      await token.connect(bob).approve(await router.getAddress(), amount);
      await router.connect(bob).sell(poolAddr, amount, 0, await futureDeadline());
      const afterPrice = await pool.getPrice();
      expect(quote.priceImpactBps).to.equal((beforePrice - afterPrice) * 10000n / beforePrice);
    });
  });

});
