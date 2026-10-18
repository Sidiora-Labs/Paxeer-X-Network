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
 assert.equal((await ethers.provider.getNetwork()).chainId,125n,'Fixture chain identity must be125; wrong-chain execution rejected before deployment');
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
 const receipts=[],scenarios=[],negativeCases=[],getters=[],walletEvidence=[],markets=[];
 const plain=(v)=>JSON.parse(JSON.stringify(v,(_,x)=>typeof x==='bigint'?x.toString():x));
 async function capture(label,tx){const r=await tx.wait();assert.equal(r.status,1,`${label}: mined successful receipt`);const txData=await ethers.provider.getTransaction(r.hash);assert.equal(txData.chainId,125n);receipts.push({label,...r.toJSON(),nativeGasFeeRaw:(r.gasUsed*r.gasPrice).toString(),transactionValueRaw:txData.value.toString()});return r;}
 async function negative(id,fn,expected){let failure;try{await fn()}catch(e){failure=e}assert.ok(failure,`${id}: expected rejection`);const data=failure.data||failure.info?.error?.data?.data||failure.info?.error?.data;const selector=typeof data==='string'?data.slice(0,10):null;assert.equal(selector,ethers.id(expected).slice(0,10),`${id}: expected ${expected}, got ${failure.shortMessage}`);negativeCases.push({id,expectedSignature:expected,selector,observedData:data,evidenceType:'actual eth_call semantic rejection'});}
 function decoded(r,contract,name){return r.logs.filter(l=>l.address.toLowerCase()===contract.target.toLowerCase()).map(l=>{try{return contract.interface.parseLog(l)}catch{return null}}).filter(e=>e&&e.name===name);}
 function scenario(id,title,labels,proof){scenarios.push({id,title,receiptLabels:labels,proof:plain(proof),status:'asserted_from_real_contract_execution'});}
 async function snapshot(label,market){const [virtualUsdl,realUsdl,tokenReserve]=await market.pool.getReserves();const value={label,blockNumber:await ethers.provider.getBlockNumber(),pool:market.pool.target,virtualUsdl,realUsdl,tokenReserve,actualUsdl:await usdl.balanceOf(market.pool.target),actualToken:await market.token.balanceOf(market.pool.target),accumulatedUsdlFees:await market.pool.accumulatedUsdlFees(),accumulatedTokenFees:await market.pool.accumulatedTokenFees(),claimableUsdl:await accumulatorProxy.getAccumulatedFees(market.pool.target),totalLiabilities:await accumulatorProxy.totalLiabilities(),treasuryUsdl:await usdl.balanceOf(treasuryProxy.target),owner:await nftProxy.ownerOf(market.nftId),balances:{aliceUsdl:await usdl.balanceOf(alice.address),bobUsdl:await usdl.balanceOf(bob.address),charlieUsdl:await usdl.balanceOf(charlie.address),aliceToken:await market.token.balanceOf(alice.address),bobToken:await market.token.balanceOf(bob.address),charlieToken:await market.token.balanceOf(charlie.address)}};assert.ok(value.actualUsdl>=realUsdl);assert.ok(value.actualToken>=tokenReserve);getters.push(plain(value));return value;}
 async function market(label,strategy=0,optical=ethers.ZeroAddress,direct=false){const before=await usdl.balanceOf(alice.address);if(direct)await capture(label+'-approve',await usdl.connect(alice).approve(factoryProxy.target,1000000n));const r=await capture(label,await (direct?factoryProxy:router).connect(alice).createMarket(label,label.slice(0,5),strategy,optical));assert.equal(before-await usdl.balanceOf(alice.address),1000000n);const e=decoded(r,factoryProxy,'MarketCreated')[0];assert.ok(e,'Factory native creation authoritative');const token=(await ethers.getContractFactory('SidioraERC20')).attach(e.args[0]);const pool=(await ethers.getContractFactory('SidioraPool')).attach(e.args[1]);const nftId=e.args[3];assert.equal(await nftProxy.ownerOf(nftId),alice.address);assert.equal(await registryProxy.isRegisteredPool(pool.target),true);const result={pool,token,nftId,receipt:r};markets.push(result);return result;}
 const deadline=(await ethers.provider.getBlock('latest')).timestamp+86400;
 await capture('fixed-fee-policy',await configProxy.setFeeWeights(0,0,0));
 const a=await market('router-create'); const b=await market('factory-create',0,ethers.ZeroAddress,true);
 scenario('GF01','Router and Factory create charge once',['router-create','factory-create'],{creationFeeRaw:await configProxy.creationFee(),asset:usdl.target,nftIds:[a.nftId,b.nftId],authoritativeEvent:'SidioraFactory.MarketCreated'});
 const before=await snapshot('before-buy',a); const input=5000000000n;
 const buy=await capture('buy',await router.connect(bob).buy(a.pool.target,input,0,deadline));const buySwap=decoded(buy,eventEmitter,'Swap')[0];assert.equal(buySwap.args[5],input*30n/10000n);const after=await snapshot('after-buy',a);assert.equal(after.realUsdl-before.realUsdl,input-buySwap.args[5]);const feeEvent=decoded(buy,accumulatorProxy,'FeeRecorded')[0];assert.equal(feeEvent.args[1],buySwap.args[5]);assert.equal(feeEvent.args[2],buySwap.args[5]*1000n/10000n);assert.equal(after.treasuryUsdl-before.treasuryUsdl,feeEvent.args[2]);
 const sellAmount=(await a.token.balanceOf(bob.address))/10n;await capture('sell-approve',await a.token.connect(bob).approve(routerAddr,sellAmount));const sell=await capture('sell',await router.connect(bob).sell(a.pool.target,sellAmount,0,deadline));const sellSwap=decoded(sell,eventEmitter,'Swap')[0];const afterSell=await snapshot('after-sell',a);assert.equal(sellSwap.args[5],sellAmount*30n/10000n);assert.equal(afterSell.accumulatedTokenFees-after.accumulatedTokenFees,sellSwap.args[5]);assert.equal(decoded(sell,accumulatorProxy,'FeeRecorded').length,0);
 scenario('GF05','Buy USDL fees and sell retained token fees',['buy','sell'],{buyFee:{asset:usdl.target,raw:buySwap.args[5]},sellFee:{asset:a.token.target,raw:sellSwap.args[5]},protocolCut:feeEvent.args[2],realReserveExcludesVirtual:true});
 await capture('direct-prepay',await usdl.connect(bob).transfer(a.pool.target,1000000n));const recipientBefore=await a.token.balanceOf(charlie.address);const direct=await capture('direct-swap',await a.pool.connect(bob).swap(1000000n,0,true,charlie.address,deadline));assert.equal(decoded(direct,router,'Buy').length,0);assert.equal(decoded(direct,eventEmitter,'Swap')[0].args[1],bob.address);assert.ok(await a.token.balanceOf(charlie.address)>recipientBefore);scenario('GF02','Direct swap sends output to third-party recipient',['direct-prepay','direct-swap'],{trader:bob.address,recipient:charlie.address,routerLogs:0});
 await negative('swapFor-unauthorized',()=>a.pool.connect(bob).swapFor.staticCall(1n,0,true,bob.address,bob.address,deadline),'Unauthorized()');
 await capture('rounding-prepay',await usdl.connect(bob).transfer(a.pool.target,1001n));const rounding=await capture('rounding-buy',await a.pool.connect(bob).swap(1001n,0,true,bob.address,deadline));const roundFee=decoded(rounding,eventEmitter,'Swap')[0].args[5];assert.equal(roundFee,3n);scenario('GF06','Integer fee rounds down',['rounding-buy'],{inputRaw:'1001',feeBps:'30',feeRaw:roundFee});
 const multiAmount=(await a.token.balanceOf(bob.address))/20n;await capture('multihop-approve',await a.token.connect(bob).approve(routerAddr,multiAmount));const multi=await capture('multihop',await router.connect(bob).swapTokenForToken(a.token.target,b.token.target,multiAmount,0,deadline));assert.equal(decoded(multi,router,'MultihopSwap').length,1);assert.equal(decoded(multi,eventEmitter,'Swap').length,2);assert.ok(await b.token.balanceOf(bob.address)>0n);scenario('GF03','Multihop has one route and two pool fills',['multihop'],{routeExecutions:1,poolFills:2});
 const snapBefore=await snapshot('before-sync',a);await capture('donation',await usdl.connect(charlie).transfer(a.pool.target,12345n));const sync=await capture('sync',await a.pool.syncReserves());const snapAfter=await snapshot('after-sync',a);assert.equal(snapAfter.realUsdl-snapBefore.realUsdl,12345n);assert.equal(decoded(sync,eventEmitter,'PoolStateUpdated').length,1);assert.equal(decoded(sync,eventEmitter,'Swap').length,0);scenario('GF15','Donation sync is not a trade',['donation','sync'],{donationRaw:'12345',realUsdlDelta:'12345',swapLogs:0});
 const claimable=await accumulatorProxy.getAccumulatedFees(a.pool.target);await capture('nft-transfer',await nftProxy.connect(alice).transferFrom(alice.address,charlie.address,a.nftId));await negative('prior-owner-claim',()=>feesRouter.connect(alice).claimFees.staticCall(a.nftId),'NotNftOwner()');const charlieBefore=await usdl.balanceOf(charlie.address);const claim=await capture('claim',await feesRouter.connect(charlie).claimFees(a.nftId));assert.equal(await usdl.balanceOf(charlie.address)-charlieBefore,claimable);assert.equal(await accumulatorProxy.getAccumulatedFees(a.pool.target),0n);assert.equal(decoded(claim,accumulatorProxy,'FeesClaimed').length,1);scenario('GF11','Fee rights follow NFT ownership',['nft-transfer','claim'],{newOwner:charlie.address,claimedUsdlRaw:claimable,previousOwnerRejected:true});
 for(const [strategy,method,label] of [[1,'executeBurn','burn'],[3,'executeLpRewards','lp']]){const m=await market(label+'-create',strategy);await capture(label+'-buy',await router.connect(bob).buy(m.pool.target,100000000n,0,deadline));const amount=await accumulatorProxy.getAccumulatedFees(m.pool.target);const destination=strategy===1?'0x000000000000000000000000000000000000dEaD':m.pool.target;const balanceBefore=await usdl.balanceOf(destination);const settle=await capture(label+'-settle',await feesRouter.connect(alice)[method](m.nftId));assert.equal(await usdl.balanceOf(destination)-balanceBefore,amount);assert.equal(await accumulatorProxy.getAccumulatedFees(m.pool.target),0n);if(strategy===3)assert.equal((await m.pool.getReserves())[1],await usdl.balanceOf(m.pool.target));scenario('GF12-'+label,'Actual strategy settlement '+label,[label+'-buy',label+'-settle'],{strategy,destination,settledUsdlRaw:amount});}
 const air=await market('airdrop-create',2);await capture('airdrop-buy',await router.connect(bob).buy(air.pool.target,100000000n,0,deadline));assert.equal(await accumulatorProxy.supportsAirdrop(air.pool.target),true);await capture('airdrop-trigger',await feesRouter.connect(alice).executeAirdrop(air.nftId));const airAmount=await accumulatorProxy.getAirdropBalance(air.pool.target),bobAir=await air.token.balanceOf(bob.address);await capture('airdrop-postsnapshot-transfer',await air.token.connect(bob).transfer(charlie.address,bobAir));await negative('airdrop-new-holder',()=>feesRouter.connect(charlie).claimAirdrop.staticCall(air.nftId),'ZeroAmount()');const bobUsdlBefore=await usdl.balanceOf(bob.address);await capture('airdrop-claim',await feesRouter.connect(bob).claimAirdrop(air.nftId));assert.equal(await usdl.balanceOf(bob.address)-bobUsdlBefore,airAmount);await negative('airdrop-repeat',()=>feesRouter.connect(bob).claimAirdrop.staticCall(air.nftId),'AlreadyClaimed()');scenario('GF13','Airdrop uses checkpoint eligibility',['airdrop-trigger','airdrop-postsnapshot-transfer','airdrop-claim'],{snapshotBlock:await accumulatorProxy.airdropSnapshotBlock(air.pool.target,1),snapshotHolder:bob.address,newHolder:charlie.address,claimedUsdlRaw:airAmount});
 const Optical=await ethers.getContractFactory('CooldownOptical');const optical=await Optical.deploy(registryProxy.target,deployer.address,60);await optical.waitForDeployment();const opticalMarket=await market('optical-create',0,optical.target);await negative('optical-quote',()=>quoter.quoteExactInput(opticalMarket.pool.target,1000000n,true),'OpticalQuoteUnsupported()');await capture('optical-buy',await router.connect(bob).buy(opticalMarket.pool.target,1000000n,0,deadline));await negative('optical-cooldown',()=>router.connect(bob).buy.staticCall(opticalMarket.pool.target,1000000n,0,deadline),'InsufficientInput()');scenario('GF09','Actual optical rejects Quoter and applies cooldown',['optical-buy'],{optical:optical.target,quoteSupported:false,cooldownSeconds:'60'});
 const fresh=await market('floor-create');await negative('virtual-floor-quote',()=>quoter.quoteExactInput(fresh.pool.target,1000000000000n,false),'UnexecutableQuote()');scenario('GF07','Virtual reserve is not executable real liquidity',['floor-create'],{reserves:await fresh.pool.getReserves(),quoteRejection:'UnexecutableQuote'});
 await negative('deadline',()=>router.connect(bob).buy.staticCall(a.pool.target,1000000n,0,1),'DeadlineExpired()');await negative('slippage',()=>router.connect(bob).buy.staticCall(a.pool.target,1000000n,ethers.MaxUint256,deadline),'SlippageExceeded()');scenario('GF08','Deadline and slippage are real semantic rejections',[],{timerSuccessAllowed:false});
 const cfgReceipt=await capture('config-change',await configProxy.setCreationFee(1000001n));assert.equal(decoded(cfgReceipt,configProxy,'ConfigUpdated').length,0);assert.equal(decoded(cfgReceipt,eventEmitter,'ConfigUpdated').length,1);assert.equal(await configProxy.creationFee(),1000001n);await capture('config-restore',await configProxy.setCreationFee(1000000n));
 const Config=await ethers.getContractFactory('ProtocolConfig'),Proxy=await ethers.getContractFactory('UUPSProxy');const cfgImpl=await Config.deploy();await cfgImpl.waitForDeployment();const noMirrorProxy=await Proxy.deploy(cfgImpl.target,Config.interface.encodeFunctionData('initialize',[usdl.target,ethers.ZeroAddress,deployer.address]));await noMirrorProxy.waitForDeployment();const noMirror=Config.attach(noMirrorProxy.target);const noMirrorReceipt=await capture('config-no-mirror',await noMirror.setCreationFee(2000000n));assert.equal(noMirrorReceipt.logs.length,0);assert.equal(await noMirror.creationFee(),2000000n);scenario('GF14','Config setter is mirror-only',['config-change','config-no-mirror'],{nativeConfigLogs:0,hubConfigLogs:1,noMirrorLogs:0,requiresGetterReconcile:true});
 const pause=await capture('pause',await a.pool.connect(alice).pause());assert.equal(decoded(pause,a.pool,'PauseToggled')[0].args[0],true);await negative('paused-swap',()=>router.connect(bob).buy.staticCall(a.pool.target,1000000n,0,deadline),'Paused()');await capture('unpause',await a.pool.connect(alice).unpause());scenario('GF18','Pause emits PauseToggled and rejects swaps',['pause','unpause'],{eventSignature:'PauseToggled(bool)'});
 const message='KindleLaunch isolated fixture chain125 signature proof';const signature=await alice.signMessage(message),digest=ethers.hashMessage(message);assert.equal(ethers.verifyMessage(message,signature),alice.address);const solc=require(require('node:module').createRequire(require.resolve(process.env.CONTRACT_SOURCE_ROOT+'/node_modules/hardhat/package.json')).resolve('solc'));const walletSource=fs.readFileSync(path.join(__dirname,'ContractWalletFixture.sol'),'utf8');const compiled=JSON.parse(solc.compile(JSON.stringify({language:'Solidity',sources:{'ContractWalletFixture.sol':{content:walletSource}},settings:{outputSelection:{'*':{'*':['abi','evm.bytecode.object']}}}})));assert.equal((compiled.errors||[]).filter(e=>e.severity==='error').length,0);const wa=compiled.contracts['ContractWalletFixture.sol'].ContractWalletFixture;const wallet=await new ethers.ContractFactory(wa.abi,wa.evm.bytecode.object,deployer).deploy(alice.address);await wallet.waitForDeployment();await capture('erc1271-deploy',wallet.deploymentTransaction());assert.equal(await wallet.isValidSignature(digest,signature),'0x1626ba7e');const invalid=await bob.signMessage(message);assert.equal(await wallet.isValidSignature(digest,invalid),'0xffffffff');assert.equal(await wallet.isValidSignature(digest,'0x12'),'0xffffffff');walletEvidence.push({kind:'EOA',address:alice.address,message,digest,signature,valid:true},{kind:'ERC1271',address:wallet.target,owner:alice.address,message,digest,signature,expectedMagic:'0x1626ba7e',invalidSignature:invalid,invalidExpectedMagic:'0xffffffff',compiler:solc.version(),fixtureSourceSha256:crypto.createHash('sha256').update(walletSource).digest('hex'),deployedCodeHash:ethers.keccak256(await ethers.provider.getCode(wallet.target)),scope:'real isolated contract isValidSignature behavior; not production wallet SDK/backend qualification and not core permit support'});scenario('GF25','EOA and real ERC1271 wallet signature proof',['erc1271-deploy'],{wallet:wallet.target,validMagic:'0x1626ba7e',invalidMagic:'0xffffffff'});

 await ethers.provider.send('evm_setAutomine',[false]);let sameA,sameB;try{const nonce=await alice.getNonce('pending');sameA=await router.connect(alice).createMarket('Same Block A','SBA',0,ethers.ZeroAddress,{nonce,gasLimit:10000000});sameB=await router.connect(alice).createMarket('Same Block B','SBB',0,ethers.ZeroAddress,{nonce:nonce+1,gasLimit:10000000});await ethers.provider.send('evm_mine',[]);}finally{await ethers.provider.send('evm_setAutomine',[true]);}const sameReceiptA=await capture('same-block-create-a',sameA),sameReceiptB=await capture('same-block-create-b',sameB);assert.equal(sameReceiptA.blockNumber,sameReceiptB.blockNumber);const sameEventA=decoded(sameReceiptA,factoryProxy,'MarketCreated')[0],sameEventB=decoded(sameReceiptB,factoryProxy,'MarketCreated')[0];assert.notEqual(sameEventA.args[1],sameEventB.args[1]);assert.notEqual(sameReceiptA.hash,sameReceiptB.hash);scenario('GF04','Same-wallet same-block creation resolves exact receipt',['same-block-create-a','same-block-create-b'],{blockNumber:sameReceiptA.blockNumber,transactions:[sameReceiptA.hash,sameReceiptB.hash],pools:[sameEventA.args[1],sameEventB.args[1]],transactionIndices:[sameReceiptA.index,sameReceiptB.index]});
 const logs=await ethers.provider.getLogs({fromBlock:0,toBlock:'latest'});const replay=await ethers.provider.getLogs({fromBlock:0,toBlock:'latest'});assert.deepEqual(replay.map(l=>[l.transactionHash,l.index,l.data,l.topics]),logs.map(l=>[l.transactionHash,l.index,l.data,l.topics]));scenario('GF16','Immutable raw receipt and getLogs replay',receipts.map(r=>r.label),{rawLogCount:logs.length,repeatReadIdentical:true,backendExactlyOnceNotYetProven:true});
 const allInterfaces=new Map();for(const c of [usdl,eventEmitter,configProxy,treasuryProxy,registryProxy,accumulatorProxy,nftProxy,beacon,factoryProxy,router,quoter,feesRouter,optical,wallet])allInterfaces.set(c.target.toLowerCase(),c.interface);for(const m of markets){allInterfaces.set(m.pool.target.toLowerCase(),m.pool.interface);allInterfaces.set(m.token.target.toLowerCase(),m.token.interface);}
 const decodedLogs=logs.map(l=>{let e;try{e=allInterfaces.get(l.address.toLowerCase())?.parseLog(l)}catch{}return {chainId:125,address:l.address,blockNumber:l.blockNumber,blockHash:l.blockHash,transactionHash:l.transactionHash,transactionIndex:l.transactionIndex,logIndex:l.index,topic0:l.topics[0],signature:e?.signature||null,name:e?.name||null,args:e?plain(e.args.toArray()):null,raw:l.toJSON()};});
 const result={schemaVersion:1,chainId:125,network:'isolated-hardhat',finalityModel:'committed_final_no_normal_reorg',native:{symbol:'PAX',decimals:18},settlement:{symbol:'USDL',decimals:6,address:usdl.target},creationFeeRaw:'1000000',addresses:{usdl:usdl.target,eventEmitter:eventEmitter.target,router:routerAddr,factory:factoryProxy.target,registry:registryProxy.target,accumulator:accumulatorProxy.target,nft:nftProxy.target,quoter:quoter.target,feesRouter:feesRouter.target,pool:a.pool.target,token:a.token.target},artifacts:consumedArtifacts,receipts,decodedLogs,getters,scenarios,negativeCases,walletEvidence,replayVerified:true,capabilityBlockers:[{id:'optional-pecor-fixtures',families:['PECOR','PECOROrders','PECORVault','MetaAG','optional-oracles'],status:'not_executed',reason:'Optional mainnet deployment absent and full isolated optional stack fixtures not qualified; do not advertise these capabilities as release-ready.'},{id:'production-wallet-provider',status:'not_executed',reason:'ERC1271 fixture proves only local signature contract behavior; provider recovery and live browser journey remain separate gates.'}],scope:'Actual current core Solidity artifacts and isolated EVM receipts/getters. No mainnet writes, backend delivery or capacity qualification.'};
 assert.ok(process.env.FIXTURE_OUTPUT);fs.writeFileSync(process.env.FIXTURE_OUTPUT,JSON.stringify(result,(_,v)=>typeof v==='bigint'?v.toString():v,2)+'\n');console.log(JSON.stringify({fixture:process.env.FIXTURE_OUTPUT,chainId:125,receipts:receipts.length,scenarios:scenarios.length,negativeCases:negativeCases.length,rawLogs:logs.length,replayVerified:true}));
}
main().catch(e=>{console.error(e);process.exitCode=1});
