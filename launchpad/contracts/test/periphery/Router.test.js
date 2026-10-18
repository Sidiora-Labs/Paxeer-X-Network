const { expect } = require("chai");
const { ethers } = require("hardhat");
const {
  VIRTUAL_USDL_DEFAULT,
  TOKEN_TOTAL_SUPPLY,
  CREATION_FEE,
  FeeStrategy,
  ZERO_ADDRESS,
} = require("../helpers/constants");

describe("Router", function () {
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
    await usdl.mint(charlie.address, ethers.parseUnits("1000000", 6));

    // Approve Router for USDL
    await usdl.connect(alice).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(bob).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(charlie).approve(routerAddr, ethers.MaxUint256);
  }

  async function createMarketViaRouter(creator, name, symbol, strategy) {
    const tx = await router.connect(creator).createMarket(name, symbol, strategy, ZERO_ADDRESS);
    const receipt = await tx.wait();

    // Find MarketCreated event from Router
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

    // Grant POOL_ROLE to pool
    const POOL_ROLE = ethers.keccak256(ethers.toUtf8Bytes("POOL_ROLE"));
    await accumulatorProxy.grantRole(POOL_ROLE, poolAddr);

    return { pool, token, poolAddr, tokenAddr, nftId };
  }

  async function futureDeadline() {
    const block = await ethers.provider.getBlock("latest");
    return block.timestamp + 3600;
  }

  beforeEach(async function () {
    await deployFullStack();
  });

  describe("Initialization", function () {
    it("should set all references correctly", async function () {
      expect(await router.factory()).to.equal(await factoryProxy.getAddress());
      expect(await router.poolRegistry()).to.equal(await registryProxy.getAddress());
      expect(await router.protocolConfig()).to.equal(await configProxy.getAddress());
      expect(await router.usdlAddress()).to.equal(await usdl.getAddress());
    });

    it("should revert double initialization", async function () {
      await expect(
        router.initialize(ZERO_ADDRESS, ZERO_ADDRESS, ZERO_ADDRESS, ZERO_ADDRESS, deployer.address)
      ).to.be.revertedWithCustomError(router, "AlreadyInitialized");
    });
  });

  describe("createMarket", function () {
    it("should create market via factory", async function () {
      const { pool, token, poolAddr, tokenAddr, nftId } = await createMarketViaRouter(
        alice, "RouterToken", "RTOK", Number(FeeStrategy.CLAIM)
      );

      expect(await token.balanceOf(poolAddr)).to.equal(TOKEN_TOTAL_SUPPLY);
      expect(await pool.virtualUsdlReserve()).to.equal(VIRTUAL_USDL_DEFAULT);
      expect(await nftProxy.ownerOf(nftId)).to.equal(alice.address);
      expect(await registryProxy.getPoolByToken(tokenAddr)).to.equal(poolAddr);
    });

    it("should charge creation fee", async function () {
      const aliceBefore = await usdl.balanceOf(alice.address);
      await createMarketViaRouter(alice, "FeeToken", "FTOK", 0);
      const aliceAfter = await usdl.balanceOf(alice.address);
      expect(aliceBefore - aliceAfter).to.equal(CREATION_FEE);
    });

    it("should emit MarketCreated event", async function () {
      const tx = await router.connect(alice).createMarket("EvtToken", "ETOK", 0, ZERO_ADDRESS);
      await expect(tx).to.emit(router, "MarketCreated");
    });
  });

  describe("buy", function () {
    let pool, token, poolAddr;

    beforeEach(async function () {
      const market = await createMarketViaRouter(alice, "BuyToken", "BUY", 0);
      pool = market.pool;
      token = market.token;
      poolAddr = market.poolAddr;
    });

    it("should execute buy and transfer tokens to buyer", async function () {
      const buyAmount = ethers.parseUnits("500", 6);
      const tx = await router.connect(bob).buy(poolAddr, buyAmount, 0, await futureDeadline());
      await expect(tx).to.emit(router, "Buy");

      const bobTokens = await token.balanceOf(bob.address);
      expect(bobTokens).to.be.gt(0);
    });

    it("should increase pool real USDL balance after buy", async function () {
      const buyAmount = ethers.parseUnits("500", 6);
      await router.connect(bob).buy(poolAddr, buyAmount, 0, await futureDeadline());

      // realUsdlBalance should increase (amountIn - fee goes to reserves)
      expect(await pool.realUsdlBalance()).to.be.gt(0);
    });

    it("should revert buy with expired deadline", async function () {
      const block = await ethers.provider.getBlock("latest");
      const pastDeadline = block.timestamp - 3600;
      await expect(
        router.connect(bob).buy(poolAddr, ethers.parseUnits("100", 6), 0, pastDeadline)
      ).to.be.revertedWithCustomError(router, "DeadlineExpired");
    });

    it("should revert buy with zero amount", async function () {
      await expect(
        router.connect(bob).buy(poolAddr, 0, 0, await futureDeadline())
      ).to.be.revertedWithCustomError(router, "ZeroAmount");
    });

    it("should revert buy on unregistered pool", async function () {
      await expect(
        router.connect(bob).buy(charlie.address, ethers.parseUnits("100", 6), 0, await futureDeadline())
      ).to.be.revertedWithCustomError(router, "PoolNotFound");
    });

    it("should revert buy with zero pool address", async function () {
      await expect(
        router.connect(bob).buy(ZERO_ADDRESS, ethers.parseUnits("100", 6), 0, await futureDeadline())
      ).to.be.revertedWithCustomError(router, "ZeroAddress");
    });
  });

  describe("sell", function () {
    let pool, token, poolAddr;

    beforeEach(async function () {
      const market = await createMarketViaRouter(alice, "SellToken", "SELL", 0);
      pool = market.pool;
      token = market.token;
      poolAddr = market.poolAddr;

      // Bob buys tokens first so he has some to sell
      await router.connect(bob).buy(poolAddr, ethers.parseUnits("1000", 6), 0, await futureDeadline());
    });

    it("should execute sell and transfer USDL to seller", async function () {
      const bobTokens = await token.balanceOf(bob.address);
      const sellAmount = bobTokens / 2n;

      // Approve router for tokens
      await token.connect(bob).approve(routerAddr, sellAmount);

      const bobUsdlBefore = await usdl.balanceOf(bob.address);
      const tx = await router.connect(bob).sell(poolAddr, sellAmount, 0, await futureDeadline());
      await expect(tx).to.emit(router, "Sell");

      const bobUsdlAfter = await usdl.balanceOf(bob.address);
      expect(bobUsdlAfter).to.be.gt(bobUsdlBefore);
    });

    it("should decrease pool real USDL balance after sell", async function () {
      const bobTokens = await token.balanceOf(bob.address);
      const sellAmount = bobTokens / 4n;
      await token.connect(bob).approve(routerAddr, sellAmount);

      const realUsdlBefore = await pool.realUsdlBalance();
      await router.connect(bob).sell(poolAddr, sellAmount, 0, await futureDeadline());
      const realUsdlAfter = await pool.realUsdlBalance();

      expect(realUsdlAfter).to.be.lt(realUsdlBefore);
    });

    it("should revert sell with expired deadline", async function () {
      const block = await ethers.provider.getBlock("latest");
      const pastDeadline = block.timestamp - 3600;
      await expect(
        router.connect(bob).sell(poolAddr, ethers.parseUnits("100", 6), 0, pastDeadline)
      ).to.be.revertedWithCustomError(router, "DeadlineExpired");
    });

    it("should revert sell with zero amount", async function () {
      await expect(
        router.connect(bob).sell(poolAddr, 0, 0, await futureDeadline())
      ).to.be.revertedWithCustomError(router, "ZeroAmount");
    });

    it("should revert sell on unregistered pool", async function () {
      await expect(
        router.connect(bob).sell(charlie.address, ethers.parseUnits("100", 6), 0, await futureDeadline())
      ).to.be.revertedWithCustomError(router, "PoolNotFound");
    });
  });

  describe("multicall", function () {
    it("should batch multiple buys", async function () {
      const market = await createMarketViaRouter(alice, "MultiToken", "MULTI", 0);
      const poolAddr = market.poolAddr;

      const dl = await futureDeadline();
      const buyData1 = router.interface.encodeFunctionData("buy", [
        poolAddr, ethers.parseUnits("100", 6), 0, dl,
      ]);
      const buyData2 = router.interface.encodeFunctionData("buy", [
        poolAddr, ethers.parseUnits("200", 6), 0, dl,
      ]);

      await router.connect(bob).multicall([buyData1, buyData2]);

      const bobTokens = await market.token.balanceOf(bob.address);
      expect(bobTokens).to.be.gt(0);
    });
  });

  describe("swapTokenForToken (multihop)", function () {
    let marketA, marketB;
    let tokenA, tokenB, poolAddrA, poolAddrB;

    beforeEach(async function () {
      // Create two markets
      marketA = await createMarketViaRouter(alice, "TokenA", "TOKA", 0);
      marketB = await createMarketViaRouter(alice, "TokenB", "TOKB", 0);
      tokenA = marketA.token;
      tokenB = marketB.token;
      poolAddrA = marketA.poolAddr;
      poolAddrB = marketB.poolAddr;

      // Bob buys TokenA so he has some to swap
      await router.connect(bob).buy(poolAddrA, ethers.parseUnits("5000", 6), 0, await futureDeadline());
      // Also put some USDL liquidity in poolB so the buy leg works
      await router.connect(charlie).buy(poolAddrB, ethers.parseUnits("5000", 6), 0, await futureDeadline());
    });

    it("should swap TokenA → USDL → TokenB in one transaction", async function () {
      const bobTokenA = await tokenA.balanceOf(bob.address);
      const swapAmount = bobTokenA / 4n;

      // Approve Router for TokenA
      await tokenA.connect(bob).approve(routerAddr, swapAmount);

      const bobTokenBBefore = await tokenB.balanceOf(bob.address);
      expect(bobTokenBBefore).to.equal(0);

      const dl = await futureDeadline();
      const tx = await router.connect(bob).swapTokenForToken(
        await tokenA.getAddress(),
        await tokenB.getAddress(),
        swapAmount,
        0,
        dl
      );

      const bobTokenBAfter = await tokenB.balanceOf(bob.address);
      expect(bobTokenBAfter).to.be.gt(0);

      // Bob's TokenA should decrease
      const bobTokenAAfter = await tokenA.balanceOf(bob.address);
      expect(bobTokenAAfter).to.equal(bobTokenA - swapAmount);
    });

    it("should emit MultihopSwap event", async function () {
      const bobTokenA = await tokenA.balanceOf(bob.address);
      const swapAmount = bobTokenA / 4n;
      await tokenA.connect(bob).approve(routerAddr, swapAmount);

      const tx = await router.connect(bob).swapTokenForToken(
        await tokenA.getAddress(),
        await tokenB.getAddress(),
        swapAmount,
        0,
        await futureDeadline()
      );

      await expect(tx).to.emit(router, "MultihopSwap");
    });

    it("should return intermediate USDL amount", async function () {
      const bobTokenA = await tokenA.balanceOf(bob.address);
      const swapAmount = bobTokenA / 4n;
      await tokenA.connect(bob).approve(routerAddr, swapAmount);

      const result = await router.connect(bob).swapTokenForToken.staticCall(
        await tokenA.getAddress(),
        await tokenB.getAddress(),
        swapAmount,
        0,
        await futureDeadline()
      );

      expect(result.amountOut).to.be.gt(0);
      expect(result.intermediateUsdl).to.be.gt(0);
    });

    it("should revert with same token", async function () {
      const tokenAAddr = await tokenA.getAddress();
      await expect(
        router.connect(bob).swapTokenForToken(tokenAAddr, tokenAAddr, 100n, 0, await futureDeadline())
      ).to.be.revertedWithCustomError(router, "SameToken");
    });

    it("should revert with zero amount", async function () {
      await expect(
        router.connect(bob).swapTokenForToken(
          await tokenA.getAddress(), await tokenB.getAddress(), 0, 0, await futureDeadline()
        )
      ).to.be.revertedWithCustomError(router, "ZeroAmount");
    });

    it("should revert with expired deadline", async function () {
      const block = await ethers.provider.getBlock("latest");
      const pastDeadline = block.timestamp - 3600;
      await expect(
        router.connect(bob).swapTokenForToken(
          await tokenA.getAddress(), await tokenB.getAddress(), 100n, 0, pastDeadline
        )
      ).to.be.revertedWithCustomError(router, "DeadlineExpired");
    });

    it("should revert when tokenIn has no pool", async function () {
      // Deploy a random token not registered in PoolRegistry
      const MockERC20 = await ethers.getContractFactory("MockERC20");
      const fakeToken = await MockERC20.deploy("Fake", "FAKE", 6);
      await fakeToken.waitForDeployment();

      await expect(
        router.connect(bob).swapTokenForToken(
          await fakeToken.getAddress(), await tokenB.getAddress(), 100n, 0, await futureDeadline()
        )
      ).to.be.revertedWithCustomError(router, "PoolNotFound");
    });

    it("should enforce slippage protection (minAmountOut)", async function () {
      const bobTokenA = await tokenA.balanceOf(bob.address);
      const swapAmount = bobTokenA / 4n;
      await tokenA.connect(bob).approve(routerAddr, swapAmount);

      // Set unreasonably high minAmountOut
      const unreasonableMin = ethers.parseUnits("999999999", 6);
      await expect(
        router.connect(bob).swapTokenForToken(
          await tokenA.getAddress(), await tokenB.getAddress(), swapAmount, unreasonableMin, await futureDeadline()
        )
      ).to.be.reverted; // SlippageExceeded from pool
    });

    it("should not leave dust USDL in Router", async function () {
      const bobTokenA = await tokenA.balanceOf(bob.address);
      const swapAmount = bobTokenA / 4n;
      await tokenA.connect(bob).approve(routerAddr, swapAmount);

      await router.connect(bob).swapTokenForToken(
        await tokenA.getAddress(), await tokenB.getAddress(), swapAmount, 0, await futureDeadline()
      );

      // Router should hold zero USDL
      const routerUsdl = await usdl.balanceOf(routerAddr);
      expect(routerUsdl).to.equal(0);
    });
  });

  describe("permit variants", function () {
    let pool, token, poolAddr, tokenAddr;

    beforeEach(async function () {
      const market = await createMarketViaRouter(alice, "PermitToken", "PERM", 0);
      pool = market.pool;
      token = market.token;
      poolAddr = market.poolAddr;
      tokenAddr = market.tokenAddr;

      // Bob buys tokens first so he has some to sell
      await router.connect(bob).buy(poolAddr, ethers.parseUnits("5000", 6), 0, await futureDeadline());
    });

    async function signPermit(signer, tokenContract, spender, value, deadline) {
      const tokenAddress = await tokenContract.getAddress();
      const ownerAddress = signer.address;
      const nonce = await tokenContract.nonces(ownerAddress);

      const domain = {
        name: await tokenContract.name(),
        version: "1",
        chainId: (await ethers.provider.getNetwork()).chainId,
        verifyingContract: tokenAddress,
      };

      const types = {
        Permit: [
          { name: "owner", type: "address" },
          { name: "spender", type: "address" },
          { name: "value", type: "uint256" },
          { name: "nonce", type: "uint256" },
          { name: "deadline", type: "uint256" },
        ],
      };

      const message = {
        owner: ownerAddress,
        spender: spender,
        value: value,
        nonce: nonce,
        deadline: deadline,
      };

      const sig = await signer.signTypedData(domain, types, message);
      const { v, r, s } = ethers.Signature.from(sig);
      return { value, deadline, v, r, s };
    }

    it("sellWithPermit should sell without prior approve tx", async function () {
      const bobTokens = await token.balanceOf(bob.address);
      const sellAmount = bobTokens / 4n;
      const dl = await futureDeadline();

      // No approve() call — use permit instead
      const permit = await signPermit(bob, token, routerAddr, sellAmount, dl);

      const bobUsdlBefore = await usdl.balanceOf(bob.address);
      await router.connect(bob).sellWithPermit(poolAddr, sellAmount, 0, dl, permit);
      const bobUsdlAfter = await usdl.balanceOf(bob.address);

      expect(bobUsdlAfter).to.be.gt(bobUsdlBefore);
    });

    it("swapTokenForTokenWithPermit should multihop without prior approve tx", async function () {
      // Create a second market to swap into
      const marketB = await createMarketViaRouter(alice, "PermitB", "PRMB", 0);
      await router.connect(charlie).buy(marketB.poolAddr, ethers.parseUnits("5000", 6), 0, await futureDeadline());

      const bobTokens = await token.balanceOf(bob.address);
      const swapAmount = bobTokens / 4n;
      const dl = await futureDeadline();

      // No approve() — permit only
      const permit = await signPermit(bob, token, routerAddr, swapAmount, dl);

      const bobTokenBBefore = await marketB.token.balanceOf(bob.address);
      await router.connect(bob).swapTokenForTokenWithPermit(
        tokenAddr, marketB.tokenAddr, swapAmount, 0, dl, permit
      );
      const bobTokenBAfter = await marketB.token.balanceOf(bob.address);

      expect(bobTokenBAfter).to.be.gt(bobTokenBBefore);
    });

    it("buyWithPermit should work when permit fails silently (existing allowance)", async function () {
      // MockERC20 USDL has no permit — permit call fails silently,
      // falls back to existing allowance (already approved in setup)
      const buyAmount = ethers.parseUnits("100", 6);
      const dl = await futureDeadline();

      // Dummy permit params (will fail silently)
      const dummyPermit = {
        value: buyAmount,
        deadline: dl,
        v: 27,
        r: ethers.zeroPadValue("0x01", 32),
        s: ethers.zeroPadValue("0x02", 32),
      };

      const bobTokensBefore = await token.balanceOf(bob.address);
      await router.connect(bob).buyWithPermit(poolAddr, buyAmount, 0, dl, dummyPermit);
      const bobTokensAfter = await token.balanceOf(bob.address);

      expect(bobTokensAfter).to.be.gt(bobTokensBefore);
    });
  });

  describe("UUPS upgrade", function () {
    it("should upgrade by admin", async function () {
      const NewRouter = await ethers.getContractFactory("Router");
      const newImpl = await NewRouter.deploy();
      await newImpl.waitForDeployment();

      await router.upgradeToAndCall(await newImpl.getAddress(), "0x");
    });

    it("should revert upgrade by non-admin", async function () {
      const NewRouter = await ethers.getContractFactory("Router");
      const newImpl = await NewRouter.deploy();
      await newImpl.waitForDeployment();

      await expect(
        router.connect(alice).upgradeToAndCall(await newImpl.getAddress(), "0x")
      ).to.be.revertedWithCustomError(router, "MissingRole");
    });
  });
});
