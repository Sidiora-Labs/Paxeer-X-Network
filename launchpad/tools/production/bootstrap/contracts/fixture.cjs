// Fixture transactions use current compiled sibling artifacts on an in-process chain125 only.
// Core wiring adapted from sibling test/integration/e2e-happy-path.test.js deployFullStack; actual artifacts and receipts are required.
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const hre = require(process.env.CONTRACT_SOURCE_ROOT + "/node_modules/hardhat");
const {ethers}=hre;
let usdl,eventEmitter,configProxy,treasuryProxy,registryProxy,accumulatorProxy,nftProxy,beacon,factoryProxy,router,routerAddr,quoter,feesRouter,feesRouterAddr;
let deployer,alice,bob,charlie;
const consumedArtifacts=[];
const checkedBuilds=new Set();
function verifyBuild(artifactFile){
 const artifact=JSON.parse(fs.readFileSync(artifactFile));
 const dbg=JSON.parse(fs.readFileSync(artifactFile.replace(/\.json$/,".dbg.json")));
 const buildPath=path.resolve(path.dirname(artifactFile),dbg.buildInfo);
 const build=JSON.parse(fs.readFileSync(buildPath));
 function check(source){const key=buildPath+":"+source;if(checkedBuilds.has(key))return;checkedBuilds.add(key);const entry=build.input.sources[source];assert.ok(entry,`Missing compiled dependency ${source}`);if(source.startsWith("contracts/")){const actual=fs.readFileSync(path.join(process.env.CONTRACT_SOURCE_ROOT,source),"utf8");assert.ok(actual===entry.content,`Compiled artifact source drift: ${source}`);}for(const m of entry.content.matchAll(/import\s+(?:[^;]*?from\s*)?["']([^"']+)["']\s*;/g)){const dep=m[1].startsWith(".")?path.posix.normalize(path.posix.join(path.posix.dirname(source),m[1])):m[1];check(dep);}}
 check(artifact.sourceName);
}

async function main(){
 assert.equal(hre.network.name,"hardhat", "Fixture only permits isolated in-process hardhat network");
 assert.equal((await ethers.provider.getNetwork()).chainId,125n);
 [deployer,alice,bob,charlie]=await ethers.getSigners();
 const artifactFiles=[];
 function walk(dir){for(const e of fs.readdirSync(dir,{withFileTypes:true})){const f=path.join(dir,e.name);if(e.isDirectory())walk(f);else if(e.name.endsWith(".json")&&!e.name.endsWith(".dbg.json"))artifactFiles.push(f);}}
 walk(process.env.CONTRACT_SOURCE_ROOT+"/artifacts/contracts");
 ethers.getContractFactory=async(name)=>{const files=artifactFiles.filter(f=>path.basename(f)===name+".json");assert.equal(files.length,1,`Unique current artifact ${name}`);verifyBuild(files[0]);const bytes=fs.readFileSync(files[0]);const a=JSON.parse(bytes);consumedArtifacts.push({contractName:name,path:files[0],sha256:crypto.createHash("sha256").update(bytes).digest("hex")});return new ethers.ContractFactory(a.abi,a.bytecode,deployer);};
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


 await deployFullStack();
 const receipts=[];
 async function capture(label,tx){const r=await tx.wait();assert.equal(r.status,1);receipts.push({label,...r.toJSON()});return r;}
 const before=await usdl.balanceOf(alice.address);
 const created=await capture("create",await router.connect(alice).createMarket("Local Fixture","LFX",0,ethers.ZeroAddress));
 assert.equal(before-await usdl.balanceOf(alice.address),1000000n,"Creation fee exactly one USDL");
 const event=created.logs.map(l=>{try{return router.interface.parseLog(l)}catch{return null}}).find(e=>e&&e.name==="MarketCreated");
 assert.ok(event,"Real MarketCreated receipt required");
 const poolAddress=event.args[1],tokenAddress=event.args[0];
 const pool=(await ethers.getContractFactory("SidioraPool")).attach(poolAddress);
 const token=(await ethers.getContractFactory("SidioraERC20")).attach(tokenAddress);
 const deadline=(await ethers.provider.getBlock("latest")).timestamp+3600;
 const buy=await capture("buy",await router.connect(bob).buy(poolAddress,5000000000n,0,deadline));
 const amount=await token.balanceOf(bob.address);assert.ok(amount>0n);
 await (await token.connect(bob).approve(routerAddr,amount)).wait();
 const feesBefore=await pool.accumulatedTokenFees();
 const sell=await capture("sell",await router.connect(bob).sell(poolAddress,amount,0,deadline));
 assert.ok(await pool.accumulatedTokenFees()>feesBefore,"Sell fee retained as token");
 const hubLogs=await ethers.provider.getLogs({address:await eventEmitter.getAddress(),fromBlock:created.blockNumber,toBlock:sell.blockNumber});
 const swaps=hubLogs.map(l=>{try{return eventEmitter.interface.parseLog(l)}catch{return null}}).filter(e=>e&&e.name==="Swap");
 assert.equal(swaps.length,2,"Canonical hub only has actual buy/sell");
 const replay=await ethers.provider.getLogs({address:await eventEmitter.getAddress(),fromBlock:created.blockNumber,toBlock:sell.blockNumber});
 assert.deepEqual(replay.map(l=>[l.transactionHash,l.index,l.data,l.topics]),hubLogs.map(l=>[l.transactionHash,l.index,l.data,l.topics]),"Immutable local history replay");
 const addresses={usdl:await usdl.getAddress(),eventEmitter:await eventEmitter.getAddress(),router:routerAddr,pool:poolAddress,token:tokenAddress};
 const result={schemaVersion:1,chainId:125,network:"isolated-hardhat",creationFeeRaw:"1000000",addresses,artifacts:consumedArtifacts,receipts,canonicalHubLogs:hubLogs.map(l=>l.toJSON()),replayVerified:true,scope:"real EVM transactions and RPC log replay; no backend ingestion capacity or integration qualification"};
 const out=process.env.FIXTURE_OUTPUT;assert.ok(out,"FIXTURE_OUTPUT required");fs.writeFileSync(out,JSON.stringify(result,(_,v)=>typeof v==="bigint"?v.toString():v,2)+"\n");
 console.log(JSON.stringify({fixture:out,chainId:125,receipts:receipts.length,swapLogs:swaps.length,replayVerified:true}));
}
main().catch(e=>{console.error(e);process.exitCode=1});
