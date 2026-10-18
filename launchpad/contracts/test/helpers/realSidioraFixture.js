const { ethers } = require("hardhat");

async function deployRealSidioraFixture({ authorizeFactory = true } = {}) {
    const [deployer, alice, bob, charlie, guardian] = await ethers.getSigners();
    let router, routerAddr, quoter, feesRouter, feesRouterAddr;
    let factoryProxy, configProxy, treasuryProxy, registryProxy, accumulatorProxy, nftProxy;
    let eventEmitter, beacon, usdl;
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
    if (authorizeFactory) await accumulatorProxy.grantRole(FACTORY_ROLE, await factoryProxy.getAddress());
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
    return { deployer, alice, bob, charlie, guardian, router, routerAddr, quoter,
      feesRouter, feesRouterAddr, factory: factoryProxy, config: configProxy,
      treasury: treasuryProxy, registry: registryProxy, accumulator: accumulatorProxy,
      nft: nftProxy, eventEmitter, beacon, usdl };
  }

module.exports = { deployRealSidioraFixture };
