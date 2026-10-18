async function fixture() {
const fs = await import("node:fs");
const path = await import("node:path");
const crypto = await import("node:crypto");
const { default: assert } = await import("node:assert/strict");
const { pathToFileURL } = await import("node:url");
const ethers = await import(pathToFileURL(path.join(process.env.CONTRACT_SOURCE_ROOT, "node_modules/ethers/lib.esm/index.js")).href);
const provider = new ethers.JsonRpcProvider("http://127.0.0.1:22700");
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
 assert.ok(Number.isSafeInteger(Number(process.env.L27_OWNED_ANVIL_PID))&&Number(process.env.L27_OWNED_ANVIL_PID)>0, "Fixture requires the runner-owned fresh Anvil child");
 process.kill(Number(process.env.L27_OWNED_ANVIL_PID), 0);
 assert.equal((await provider.getNetwork()).chainId,125n,'Fixture chain identity must be125; wrong-chain execution rejected before deployment');
 [deployer,alice,bob,charlie]=await Promise.all([0,1,2,3].map(index=>provider.getSigner(index)));
 const artifactFiles=[];
 function walk(dir){for(const e of fs.readdirSync(dir,{withFileTypes:true})){const f=path.join(dir,e.name);if(e.isDirectory())walk(f);else if(e.name.endsWith(".json")&&!e.name.endsWith(".dbg.json"))artifactFiles.push(f);}}
 walk(process.env.CONTRACT_SOURCE_ROOT+"/artifacts/contracts");
 const getContractFactory=async(name)=>{const files=artifactFiles.filter(f=>path.basename(f)===name+".json");assert.equal(files.length,1,`Unique current artifact ${name}`);verifyBuild(files[0]);const bytes=fs.readFileSync(files[0]);const a=JSON.parse(bytes);consumedArtifacts.push({contractName:name,path:files[0],sha256:crypto.createHash("sha256").update(bytes).digest("hex")});return new ethers.ContractFactory(a.abi,a.bytecode,deployer);};
  async function deployFullStack() {
    const Proxy = await getContractFactory("UUPSProxy");
    const Asset = await getContractFactory("SidioraERC20");

    usdl = await Asset.deploy('USD Ledger','USDL',ethers.parseUnits('1000000000',6),deployer.address);
    await usdl.waitForDeployment();

    const EventEmitter = await getContractFactory("EventEmitter");
    const emitterImpl = await EventEmitter.deploy();
    const emitterProxy = await Proxy.deploy(await emitterImpl.getAddress(),
      EventEmitter.interface.encodeFunctionData("initialize", [deployer.address]));
    eventEmitter = EventEmitter.attach(await emitterProxy.getAddress());
    await eventEmitter.reinitializeV2(deployer.address);

    // ProtocolConfig
    const Config = await getContractFactory("ProtocolConfig");
    const configImpl = await Config.deploy();
    let proxy = await Proxy.deploy(await configImpl.getAddress(),
      Config.interface.encodeFunctionData("initialize", [
        await usdl.getAddress(), await eventEmitter.getAddress(), deployer.address,
      ]));
    configProxy = Config.attach(await proxy.getAddress());

    // Treasury
    const Treasury = await getContractFactory("Treasury");
    const treasuryImpl = await Treasury.deploy();
    proxy = await Proxy.deploy(await treasuryImpl.getAddress(),
      Treasury.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ]));
    treasuryProxy = Treasury.attach(await proxy.getAddress());

    // PoolRegistry
    const Registry = await getContractFactory("PoolRegistry");
    const registryImpl = await Registry.deploy();
    proxy = await Proxy.deploy(await registryImpl.getAddress(),
      Registry.interface.encodeFunctionData("initialize", [
        await eventEmitter.getAddress(), deployer.address,
      ]));
    registryProxy = Registry.attach(await proxy.getAddress());

    // FeeAccumulator
    const Acc = await getContractFactory("FeeAccumulator");
    const accImpl = await Acc.deploy();
    proxy = await Proxy.deploy(await accImpl.getAddress(),
      Acc.interface.encodeFunctionData("initialize", [
        await configProxy.getAddress(), await treasuryProxy.getAddress(),
        await registryProxy.getAddress(), await eventEmitter.getAddress(),
        await usdl.getAddress(), deployer.address,
      ]));
    accumulatorProxy = Acc.attach(await proxy.getAddress());

    // SidioraNFT
    const NFT = await getContractFactory("SidioraNFT");
    const nftImpl = await NFT.deploy();
    proxy = await Proxy.deploy(await nftImpl.getAddress(),
      NFT.interface.encodeFunctionData("initialize", [
        "Sidiora Pool NFT", "SIDNFT", await eventEmitter.getAddress(), deployer.address,
      ]));
    nftProxy = NFT.attach(await proxy.getAddress());

    // Pool impl + Beacon
    const Pool = await getContractFactory("SidioraPool");
    const poolImpl = await Pool.deploy();
    const PoolBeacon = await getContractFactory("PoolBeacon");
    beacon = await PoolBeacon.deploy(await poolImpl.getAddress(), deployer.address);
    await beacon.waitForDeployment();

    // Factory
    const Factory = await getContractFactory("SidioraFactory");
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
    const Router = await getContractFactory("Router");
    const routerImpl = await Router.deploy();
    proxy = await Proxy.deploy(await routerImpl.getAddress(),
      Router.interface.encodeFunctionData("initialize", [
        await factoryProxy.getAddress(), await registryProxy.getAddress(),
        await configProxy.getAddress(), await usdl.getAddress(), deployer.address,
      ]));
    router = Router.attach(await proxy.getAddress());
    routerAddr = await router.getAddress();

    // Quoter
    const Quoter = await getContractFactory("Quoter");
    const quoterImpl = await Quoter.deploy();
    proxy = await Proxy.deploy(await quoterImpl.getAddress(),
      Quoter.interface.encodeFunctionData("initialize", [
        await registryProxy.getAddress(), await configProxy.getAddress(), deployer.address,
      ]));
    quoter = Quoter.attach(await proxy.getAddress());

    // FeesRouter
    const FeesRouter = await getContractFactory("FeesRouter");
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
    for (const user of [alice,bob,charlie]) {
      await (await usdl.transfer(user.address,fundAmount)).wait();
    }
    await usdl.connect(alice).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(bob).approve(routerAddr, ethers.MaxUint256);
    await usdl.connect(charlie).approve(routerAddr, ethers.MaxUint256);
  }



 await deployFullStack();

 const domain=await import('./creation-domain.ts');
 const receipts=[];
 async function create(label){const before=await usdl.balanceOf(alice.address);const tx=await router.connect(alice).createMarket(label,label,0,ethers.ZeroAddress);const r=await tx.wait();assert.equal(r.status,1);assert.equal(before-await usdl.balanceOf(alice.address),1000000n);receipts.push(r.toJSON());const events=r.logs.filter(l=>l.address.toLowerCase()===factoryProxy.target.toLowerCase()).map(l=>{try{const e=factoryProxy.interface.parseLog(l);return e.name==='MarketCreated'?{transactionHash:l.transactionHash,emitter:l.address,creator:e.args.creator,pool:e.args.pool,token:e.args.token,logIndex:l.index,blockHeight:String(l.blockNumber),blockHash:l.blockHash}:null}catch{return null}}).filter(Boolean);return {chainId:125,transactionHash:r.hash,status:'success',events,blockHeight:String(r.blockNumber),blockHash:r.blockHash};}
 const a=await create('L27A');const b=await create('L27B');
 await provider.send('anvil_mine',['0x40']);
 const expected={hash:a.transactionHash,creator:alice.address,factory:factoryProxy.target};
 const registry=async(pool,block)=>{const actualBlock=await provider.getBlock(Number(block.height));assert.equal(actualBlock.hash,block.hash);const value=await registryProxy.getPoolMetadata(pool,{blockTag:Number(block.height)});return {token:value.token,creator:value.creator,blockHeight:block.height,blockHash:actualBlock.hash};};
 const resolved=await domain.resolveCreation({...a,events:[...b.events,...a.events]},expected,registry);assert.equal(resolved.poolAddress,a.events[0].pool.toLowerCase());
 assert.equal((await domain.resolveCreation({...a,events:b.events},expected,registry)).state,'resolution_delayed');
 assert.equal((await domain.resolveCreation({...a,events:a.events.map(e=>({...e,emitter:router.target}))},expected,registry)).state,'resolution_delayed');
 await assert.rejects(domain.resolveCreation({...a,events:[...a.events,...a.events]},expected,registry));
 await assert.rejects(domain.resolveCreation(a,expected,async(pool,block)=>({...await registry(pool,block),blockHash:b.blockHash})));
 const d={chainId:125,revision:'actual-L27',usdl:usdl.target,router:router.target,factory:factoryProxy.target};
 const read={revision:d.revision,decimals:Number(await usdl.decimals()),feeRaw:String(await configProxy.creationFee()),gasRaw:'1'};
 assert.equal(domain.verifyCost(d,read).protocolFeeRaw,'1000000');await (await configProxy.setCreationFee(2000000n)).wait();assert.throws(()=>domain.verifyCost(d,{...read,feeRaw:'2000000'}));
 let phase2;
 if(process.env.L27_PHASE2==='1') {
  await (await configProxy.setCreationFee(1000000n)).wait();
  const Proxy=await getContractFactory('UUPSProxy'),Registry=await getContractFactory('OpticalRegistry');
  const impl=await Registry.deploy();const proxy=await Proxy.deploy(impl.target,Registry.interface.encodeFunctionData('initialize',[eventEmitter.target,deployer.address]));
  const revision=crypto.createHash('sha256').update(JSON.stringify(consumedArtifacts)).digest('hex');
  const pins=[];
  for(const contract of [router,quoter,registryProxy,eventEmitter]) {
   const id=contract.target,slot=await provider.getStorage(id,'0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc');
   const implementation=ethers.getAddress('0x'+slot.slice(-40));
   pins.push({address:id,runtimeHash:ethers.keccak256(await provider.getCode(id)),implementation,implementationHash:ethers.keccak256(await provider.getCode(implementation))});
  }
  phase2={ownedFixture:true,rpcUrl:'http://127.0.0.1:22700',creator:alice.address,admin:deployer.address,
   deployment:{...d,revision,protocolConfig:configProxy.target,poolRegistry:registryProxy.target,quoter:quoter.target,opticalRegistry:proxy.target,tradeDeployment:{revision,router:router.target,quoter:quoter.target,registry:registryProxy.target,emitter:eventEmitter.target,usdl:usdl.target,pins}},
   createA:a.transactionHash,createB:b.transactionHash};
 }
 fs.writeFileSync(process.env.FIXTURE_OUTPUT,JSON.stringify({phase2,chainId:125,network:'isolated-anvil-L27-port22700',creationFeeRaw:read.feeRaw,addresses:d,receipts,resolved,consumedArtifacts,scenarios:['actual factory A/B hash selection beyond fifty blocks','missing and foreign emitter delayed','duplicate event integrity rejection','block-pinned registry mismatch rejection','actual one USDL fee and two USDL mismatch']},null,2));
 console.log('L27 actual factory receipt checks passed');
}
try { await main(); } finally { provider.destroy(); }
}
(process.env.L27_PHASE4==='1'?phase4Measurement():fixture()).catch(e=>{console.error(e instanceof Error?e.message:'Creation harness failed');process.exitCode=1});

async function phase4Measurement() {
 const fs=await import('node:fs'),path=await import('node:path'),crypto=await import('node:crypto'),url=await import('node:url');
 const {default:assert}=await import('node:assert/strict');
 const {spawn}=await import('node:child_process');
 const sha=bytes=>crypto.createHash('sha256').update(bytes).digest('hex');
 const required=(value,name)=>{assert.ok(value!==undefined&&value!==null&&value!=='',`Missing actual input: ${name}`);return value;};
 const config=JSON.parse(fs.readFileSync(required(process.env.L27_PHASE4_CONFIG,'L27_PHASE4_CONFIG'),'utf8'));
 const sourceRoot=fs.realpathSync(required(process.env.L27_SOURCE_ROOT,'L27_SOURCE_ROOT'));
 assert.equal(config.schemaVersion,1);assert.equal(config.realm,'private-anvil125');assert.equal(config.allow_fixture_mutation,true);
 const origin=new URL(required(config.browser_origin,'browser_origin')),rpcOrigin=new URL(required(config.rpc_url,'rpc_url'));
 for(const endpoint of [origin,rpcOrigin]){assert.ok(['127.0.0.1','localhost','[::1]'].includes(endpoint.hostname));assert.equal(endpoint.username,'');assert.equal(endpoint.password,'');}
 assert.equal(origin.protocol,'https:');assert.ok(['http:','https:'].includes(rpcOrigin.protocol));
 const sourceManifest=JSON.parse(fs.readFileSync(required(config.source_manifest_file,'source_manifest_file'),'utf8'));
 assert.equal(sourceManifest.revision,required(config.candidate_revision,'candidate_revision'));assert.match(sourceManifest.revision,/^[a-f0-9]{40,64}$/);
 assert.ok(Array.isArray(sourceManifest.files)&&sourceManifest.files.length>0);
 const sourceFiles=sourceManifest.files.map(entry=>{assert.match(entry.sha256,/^[a-f0-9]{64}$/);assert.equal(typeof entry.path,'string');const absolute=fs.realpathSync(path.resolve(sourceRoot,entry.path));assert.ok(absolute.startsWith(sourceRoot+path.sep));assert.equal(sha(fs.readFileSync(absolute)),entry.sha256,`Current source drift: ${entry.path}`);return {path:entry.path,sha256:entry.sha256};}).sort((a,b)=>a.path.localeCompare(b.path));
 const ownedNames=['create-wizard.tsx','creation-adapters.ts','creation-chain.config.cjs','creation-chain.fixture.cjs','creation-controller.ts','creation-domain.test.mjs','creation-domain.ts','creation-journey.integration.test.tsx','creation-storage.ts','run-creation-chain.py'];
 for(const name of ownedNames)assert.ok(sourceFiles.some(entry=>entry.path===`client/apps/web/src/widgets/create/${name}`),`Unbound creation source ${name}`);
 for(const name of ['client/apps/web/src/core/runtime-authority.ts','client/apps/web/src/widgets/create/cap-admission.ts','client/apps/web/src/widgets/trading/transaction-controls/providers.ts'])assert.ok(sourceFiles.some(entry=>entry.path===name),`Unbound actual provider ${name}`);
 const sourceDigest=sha(JSON.stringify(sourceFiles));
 const output=path.resolve(required(process.env.L27_PHASE4_OUTPUT,'L27_PHASE4_OUTPUT'));assert.ok(!fs.existsSync(path.join(output,'result.json')),'Stale result evidence is rejected');fs.mkdirSync(output,{recursive:true,mode:0o700});
 const privateDir=path.join(output,'.private');fs.mkdirSync(privateDir,{recursive:true,mode:0o700});
 const {chromium}=await import(url.pathToFileURL(fs.realpathSync(required(config.playwright_module,'playwright_module'))).href);
 const ethers=await import(url.pathToFileURL(path.join(sourceRoot,'../contracts/node_modules/ethers/lib.esm/index.js')).href);
 const provider=new ethers.JsonRpcProvider(rpcOrigin.href,125,{staticNetwork:true,cacheTimeout:-1});
 const chain=JSON.parse(fs.readFileSync(required(config.chain_file,'chain_file'),'utf8'));
 assert.equal(chain.productionActivation,false);assert.equal(chain.manifest.chainId,125);assert.equal(await provider.send('eth_chainId',[]),'0x7d');assert.equal((await provider.getBlock(0)).hash,chain.genesisHash);
 const identities=[];
 for(const [role,identity] of Object.entries(chain.manifest.roles)){
  const code=await provider.getCode(identity.address);assert.notEqual(code,'0x');assert.equal(ethers.keccak256(code),identity.runtimeHash,`Deployment runtime mismatch ${role}`);
  if(identity.implementationAddress){const impl=await provider.getCode(identity.implementationAddress);assert.equal(ethers.keccak256(impl),identity.implementationHash);if(identity.kind==='erc1967'){const slot=await provider.getStorage(identity.address,'0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc');assert.equal(ethers.getAddress('0x'+slot.slice(-40)),ethers.getAddress(identity.implementationAddress));}}
  identities.push({role,address:identity.address,runtimeHash:identity.runtimeHash,implementationHash:identity.implementationHash??null});
 }
 const role=name=>required(chain.manifest.roles[name]?.address,`deployment role ${name}`);
 const wallet=ethers.getAddress(required(config.wallet,'wallet')),admin=ethers.getAddress(required(config.admin,'admin')),other=ethers.getAddress(required(config.other_trader,'other_trader'));
 assert.notEqual(wallet,other);const signer=await provider.getSigner(wallet),adminSigner=await provider.getSigner(admin),otherSigner=await provider.getSigner(other);
 const erc20Abi=['function balanceOf(address) view returns(uint256)','function transfer(address,uint256) returns(bool)','function approve(address,uint256) returns(bool)'];
 const usdl=new ethers.Contract(role('USDL'),erc20Abi,adminSigner),router=new ethers.Contract(role('Router'),['function createMarket(string,string,uint8,address) returns(address,address,uint256)','function buy(address,uint256,uint256,uint256) returns(uint256)'],signer);
 const protocol=new ethers.Contract(role('ProtocolConfig'),['function creationFee() view returns(uint256)','function setCreationFee(uint256)'],adminSigner);
 const registry=new ethers.Contract(role('PoolRegistry'),['function getPoolMetadata(address) view returns((address creator,address token,address optical,uint256 nftId,uint256 createdAt,uint256 createdBlock))'],provider);
 const factoryInterface=new ethers.Interface(['event MarketCreated(address indexed token,address indexed pool,address indexed creator,uint256 nftId,address optical)']);
 assert.match(await provider.send('web3_clientVersion',[]),/anvil/i,'Faults require an actual private Anvil node');
 const browser=await chromium.launch({headless:true});let context;let savedUsdl,savedPax;let restoreFaults=[];let releaseSigning;let holdSigning=false;let activeScenario='setup';
 const startedAt=new Date().toISOString(),started=performance.now(),requests=[],responses=[],transactions=[],receipts=[],samples=[],failures=[],checkpoints=new Map();
 const createSelector=router.interface.getFunction('createMarket').selector;
 const controls=required(config.fault_controls,'fault_controls');
 async function fault(name){
  const control=required(controls[name],`fault_controls.${name}`);assert.ok(Array.isArray(control.argv)&&control.argv.length>0&&control.argv.length<=32);assert.equal(typeof control.executable,'string');
  const executable=fs.realpathSync(control.executable);assert.equal(control.argv[0],executable);assert.equal(sha(fs.readFileSync(executable)),control.sha256,'Fault control executable drift');
  const begin=performance.now();const exit=await new Promise((resolve,reject)=>{const child=spawn(control.argv[0],control.argv.slice(1),{cwd:sourceRoot,stdio:['ignore','ignore','ignore'],env:process.env});const timer=setTimeout(()=>{child.kill('SIGKILL');reject(new Error(`Fault control deadline: ${name}`));},120000);child.on('error',error=>{clearTimeout(timer);reject(error)});child.on('exit',code=>{clearTimeout(timer);resolve(code)})});samples.push({kind:'fault-control',name,exitCode:exit,durationMs:performance.now()-begin});assert.equal(exit,0,`Fault control failed ${name}`);
 }
 async function receipt(hash){const mined=await provider.waitForTransaction(hash,1,30000);assert.ok(mined,'Actual receipt unavailable');const block=await provider.getBlock(mined.blockNumber);assert.equal(block.hash,mined.blockHash);receipts.push({transactionHash:hash,blockHash:mined.blockHash,blockHeight:String(mined.blockNumber),status:mined.status,gasUsed:String(mined.gasUsed),scenario:activeScenario});return mined;}
 async function resolvePool(hash){const mined=await receipt(hash);assert.equal(mined.status,1);const logs=mined.logs.filter(log=>log.address.toLowerCase()===role('SidioraFactory').toLowerCase()).map(log=>{try{return factoryInterface.parseLog(log)}catch{return null}}).filter(log=>log?.name==='MarketCreated'&&log.args.creator.toLowerCase()===wallet.toLowerCase());assert.equal(logs.length,1);const event=logs[0];const actual=await registry.getPoolMetadata(event.args.pool,{blockTag:mined.blockNumber});assert.equal(actual.creator.toLowerCase(),wallet.toLowerCase());assert.equal(actual.token.toLowerCase(),event.args.token.toLowerCase());return {pool:event.args.pool.toLowerCase(),token:event.args.token.toLowerCase(),hash,blockHash:mined.blockHash,blockHeight:String(mined.blockNumber)};}
 try {
  context=await browser.newContext({viewport:{width:1440,height:1000},storageState:required(config.browser_storage_state,'browser_storage_state')});await context.tracing.start({screenshots:true,snapshots:true,sources:true});
  const page=await context.newPage();const pendingResponses=[];
  await page.exposeFunction('__creationActualWallet',async({method,params=[]})=>{
   const begin=performance.now();const observation={scenario:activeScenario,method,startedMs:begin-started};requests.push(observation);
   if(method==='eth_accounts'||method==='eth_requestAccounts')return [wallet];if(method==='wallet_getPermissions'||method==='wallet_requestPermissions')return [{parentCapability:'eth_accounts'}];
   if(method==='wallet_switchEthereumChain'){assert.equal(params[0].chainId,'0x7d');return null;}
   if(method==='personal_sign'){const message=params[0].length===42?params[1]:params[0],account=params[0]===message?params[1]:params[0];assert.equal(account.toLowerCase(),wallet.toLowerCase());const signature=await signer.signMessage(ethers.getBytes(message));observation.durationMs=performance.now()-begin;return signature;}
   assert.ok(!/^(anvil_|hardhat_|debug_)/.test(method),'Browser cannot issue fixture administration');
   if(method==='eth_sendTransaction'){
    const input=params[0];assert.equal(input.from.toLowerCase(),wallet.toLowerCase());observation.to=input.to?.toLowerCase();observation.selector=input.data?.slice(0,10);observation.walletPending=true;
    if(holdSigning)await new Promise(resolve=>{releaseSigning=resolve});
    const tx=await provider.send(method,params);observation.transactionHash=tx;observation.durationMs=performance.now()-begin;transactions.push({...observation});return tx;
   }
   if(method==='eth_getTransactionReceipt')observation.transactionHash=params[0];
   if(method==='eth_call'||method==='eth_estimateGas'){observation.to=params[0]?.to?.toLowerCase();observation.selector=params[0]?.data?.slice(0,10);observation.block=params[1]??null;}
   if(method==='eth_getLogs'){observation.fromBlock=params[0]?.fromBlock;observation.toBlock=params[0]?.toBlock;observation.to=typeof params[0]?.address==='string'?params[0].address.toLowerCase():null;if(observation.to===role('SidioraFactory').toLowerCase())assert.equal(observation.fromBlock,observation.toBlock,'Unbounded creation fallback scan');}
   const value=await provider.send(method,params);observation.durationMs=performance.now()-begin;return value;
  });
  await page.addInitScript(()=>{
   const listeners=new Map(),wallet={isMetaMask:true,request:request=>window.__creationActualWallet(request),on:(name,callback)=>{if(!listeners.has(name))listeners.set(name,new Set());listeners.get(name).add(callback)},removeListener:(name,callback)=>listeners.get(name)?.delete(callback)};
   Object.defineProperty(window,'ethereum',{value:wallet,configurable:false});const detail=Object.freeze({info:{uuid:'d36a6b07-6ac1-42a9-8d78-1e28625bc6a2',name:'Actual isolated creation wallet',icon:'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg"/>',rdns:'local.creation.wallet'},provider:wallet});window.addEventListener('eip6963:requestProvider',()=>window.dispatchEvent(new CustomEvent('eip6963:announceProvider',{detail})));window.dispatchEvent(new CustomEvent('eip6963:announceProvider',{detail}));
   window.__creationStorageMeasurements=[];const actualSetItem=Storage.prototype.setItem;
   Storage.prototype.setItem=function(key,value){const begin=performance.now(),result=actualSetItem.call(this,key,value);if(key.startsWith('creation-draft-v2:'))window.__creationStorageMeasurements.push({at:begin,bytes:new TextEncoder().encode(value).length});return result;};
  });
  const rpcRequests=new WeakMap();
  page.on('request',request=>{const endpoint=new URL(request.url());if(request.method()!=='POST'||!(endpoint.origin===rpcOrigin.origin||endpoint.origin===origin.origin&&endpoint.pathname===config.browser_rpc_path))return;const text=request.postData();if(!text||Buffer.byteLength(text)>262144)return;let calls;try{const value=JSON.parse(text);calls=Array.isArray(value)?value:[value]}catch{return;}const rows=[];for(const call of calls){if(typeof call.method!=='string'||!Array.isArray(call.params))continue;const row={scenario:activeScenario,method:call.method,startedMs:performance.now()-started,batchSize:calls.length,transport:'actual-browser-http',rpcId:call.id??null};if(['eth_call','eth_estimateGas'].includes(call.method)){row.to=call.params[0]?.to?.toLowerCase();row.selector=call.params[0]?.data?.slice(0,10);row.block=call.params[1]??null;}if(call.method==='eth_getTransactionReceipt')row.transactionHash=call.params[0];if(call.method==='eth_getLogs'){row.to=typeof call.params[0]?.address==='string'?call.params[0].address.toLowerCase():null;row.fromBlock=call.params[0]?.fromBlock;row.toBlock=call.params[0]?.toBlock;}requests.push(row);rows.push(row);}rpcRequests.set(request,rows);});
  page.on('requestfinished',request=>{for(const row of rpcRequests.get(request)??[])row.durationMs=performance.now()-started-row.startedMs;});
  page.on('requestfailed',request=>{for(const row of rpcRequests.get(request)??[]){row.durationMs=performance.now()-started-row.startedMs;row.networkFailed=true;}});
  page.on('response',response=>{const pending=(async()=>{const endpoint=new URL(response.url());if(endpoint.origin!==origin.origin)return;const headers=await response.allHeaders(),sample={scenario:activeScenario,path:endpoint.pathname,method:response.request().method(),status:response.status(),atMs:performance.now()-started,requestId:headers['x-request-id']??null};responses.push(sample);if(endpoint.pathname.includes('/metadata-publications/')&&response.ok()){const value=(await response.json()).data;sample.publication={operationId:value.operationId,contentHash:value.contentHash,revision:value.revision??null,status:value.status,attemptCount:value.attemptCount,acceptedAt:value.acceptedAt,expiresAt:value.expiresAt};}if(response.request().isNavigationRequest())assert.equal(headers[required(config.source_revision_header,'source_revision_header').toLowerCase()],config.candidate_revision,'Served candidate revision mismatch');})();pending.catch(error=>{failures.push({scenario:activeScenario,error:error instanceof Error?error.message:'Actual response observation failed'})});pendingResponses.push(pending);});
  const journal=async()=>page.evaluate(async creator=>{const draftText=localStorage.getItem(`creation-draft-v2:125:${creator}`),draft=draftText?JSON.parse(draftText):null;const attempts=await new Promise((resolve,reject)=>{const request=indexedDB.open('sidiora-creation-v2',1);request.onerror=()=>reject(request.error);request.onsuccess=()=>{const db=request.result;if(!db.objectStoreNames.contains('attempts')){db.close();resolve([]);return;}const tx=db.transaction('attempts','readonly'),get=tx.objectStore('attempts').getAll();get.onerror=()=>reject(get.error);get.onsuccess=()=>{db.close();resolve(get.result.filter(row=>row.key.startsWith(`125:${creator}:`)).map(row=>row.value).sort((a,b)=>b.createdAt-a.createdAt));};};});return {draft,attempts,storageWrites:window.__creationStorageMeasurements??[]};},wallet.toLowerCase());
  const countCreates=()=>requests.filter(row=>row.method==='eth_sendTransaction'&&row.selector===createSelector).length;
  const currentAttempt=rows=>required(rows.find(row=>!row.deliberateNew),'Current actual launch journal');
  let poolA,poolB,quoteBefore,tokenBalanceBefore,createdBeforeMetadata,metadataRevision,submitHash,createCountAtSubmit;
  async function checkpoint(name){
   await Promise.all(pendingResponses);const state=await journal(),body=await page.locator('main').innerText(),attempt=state.attempts.find(row=>!row.deliberateNew);const begin=performance.now();
   const record={scenario:activeScenario,name,atMs:begin-started,bodySha256:sha(body),draftBytes:state.draft?Buffer.byteLength(JSON.stringify(state.draft)):0,attemptCount:state.attempts.length,attempt:attempt?{intentId:attempt.intentId,state:attempt.state,hash:attempt.createTxHash??null,pool:attempt.poolAddress??null,token:attempt.tokenAddress??null,metadataStatus:attempt.metadataStatus,metadataRevision:attempt.metadataRevision??null,metadataOperationId:attempt.metadataOperationId??null,metadataAttemptCount:attempt.metadataAttemptCount,purchaseState:attempt.purchaseState,purchaseHash:attempt.purchaseTxHash??null}:null,createWalletRequests:countCreates()};samples.push(record);checkpoints.set(`${activeScenario}:${name}`,record);
   assert.ok(record.draftBytes<=65536);assert.ok(state.attempts.length<=100);assert.ok(!state.draft||!Object.keys(state.draft).some(key=>/signature|token|secret|approval/i.test(key)));
   if(name==='feeMismatch'){assert.equal(await protocol.creationFee(),2000000n);assert.match(body,/configuration_blocked|creation requires 1.00 USDL/i);assert.equal(await page.getByRole('button',{name:'Create token',exact:true}).count(),0);}
   if(name==='feeRestored'){assert.equal(await protocol.creationFee(),1000000n);assert.match(body,/Protocol fee: 1.00 USDL/);assert.match(body,/gas estimate: [0-9.]+ PAX/);}
   if(name==='approved'){assert.ok(transactions.some(row=>row.selector==='0x095ea7b3'));assert.equal(await page.getByRole('heading',{name:'Reviewed creation intent'}).count(),0);}
   if(name==='symbolEdited'){assert.equal(state.draft.symbol,config.token.symbol_after);assert.ok(body.includes(`Symbol: ${config.token.symbol_after}`));assert.ok(!body.includes(`Symbol: ${config.token.symbol_before}`));}
   if(name==='draftReloaded'){assert.equal(state.draft.symbol,config.token.symbol_after);assert.equal(await page.getByRole('heading',{name:'Reviewed creation intent'}).count(),0);}
   if(name==='poolA'){const row=currentAttempt(state.attempts);poolA=await resolvePool(required(row.createTxHash,'pool A signed hash'));assert.equal(row.poolAddress,poolA.pool);}
   if(name==='poolB'){poolB=await resolvePool(required(config.pool_b_hash,'actual pool B hash'));assert.notEqual(poolA.pool,poolB.pool);assert.ok(BigInt(poolB.blockHeight)>BigInt(poolA.blockHeight));const logs=await provider.getLogs({address:role('SidioraFactory'),fromBlock:Number(poolA.blockHeight),toBlock:Number(poolB.blockHeight),topics:[factoryInterface.getEvent('MarketCreated').topicHash]});assert.equal(logs[logs.length-1].transactionHash,config.pool_b_hash);}
   if(name==='recoveredA'){const row=currentAttempt(state.attempts);assert.equal(row.createTxHash,poolA.hash);assert.equal(row.poolAddress,poolA.pool);assert.equal((await page.getByRole('link',{name:'Open live pool'}).getAttribute('href')),`/token/${poolA.pool}`);}
   if(name==='metadataCreated'){const row=currentAttempt(state.attempts);await resolvePool(row.createTxHash);assert.equal(row.metadataStatus,'metadata_pending');createdBeforeMetadata=countCreates();}
   if(name==='metadataUnavailable'){const row=currentAttempt(state.attempts);assert.equal(row.state,'resolved');assert.equal(row.metadataStatus,'metadata_pending');assert.match(body,/Token created|metadata pending/i);assert.equal(countCreates(),createdBeforeMetadata);assert.ok(responses.some(row=>row.scenario===activeScenario&&row.status>=500)||await page.getByRole('alert').count()>0);}
   if(name==='metadataPublished'||name==='metadataRetryPublished'){const row=currentAttempt(state.attempts);assert.equal(row.metadataStatus,'published');assert.ok(BigInt(row.metadataRevision)>0n);assert.ok(row.metadataOperationId);assert.ok(row.metadataAttemptCount<=5);assert.equal(countCreates(),createdBeforeMetadata);if(name==='metadataPublished')metadataRevision=row.metadataRevision;else assert.equal(row.metadataRevision,metadataRevision);}
   if(name==='purchaseQuote'){const row=currentAttempt(state.attempts);assert.equal(row.state,'resolved');const min=body.match(/Minimum output: (\d+) token raw/),out=body.match(/Expected output: (\d+) token raw/);assert.ok(min&&out);quoteBefore={minOut:BigInt(min[1]),amountOut:BigInt(out[1]),pool:row.poolAddress,token:row.tokenAddress};assert.ok(quoteBefore.minOut>1n);const token=new ethers.Contract(quoteBefore.token,erc20Abi,provider);tokenBalanceBefore=await token.balanceOf(wallet);}
   if(name==='reservesChanged'){assert.ok(receipts.some(row=>row.scenario===activeScenario&&row.status===1&&row.transactionHash===config.other_trade_hash),'Missing actual other trader receipt');}
   if(name==='purchaseOutcome'){const row=currentAttempt(state.attempts);assert.equal(row.state,'resolved');if(row.purchaseTxHash){const mined=await receipt(row.purchaseTxHash);if(mined.status===1){const token=new ethers.Contract(quoteBefore.token,erc20Abi,provider);assert.ok((await token.balanceOf(wallet))-tokenBalanceBefore>=quoteBefore.minOut);}else assert.match(body,/Token created|purchase.*revert/i);}else assert.ok(await page.getByRole('alert').count()>0,'Stale quote must be rejected explicitly');}
   if(name==='invalidOptical'||name==='registryChanged'){assert.ok(await page.locator('[aria-invalid="true"]').count()>0);assert.equal(await page.getByRole('button',{name:/Approve exact/}).count(),0);assert.equal(await page.getByRole('button',{name:'Create token',exact:true}).count(),0);}
   if(name==='standardReview'){assert.match(body,/Standard AMM/);assert.ok(!/bonding.curve|graduation/i.test(body));}
   if(name==='insufficientUsdl'){assert.equal(await usdl.balanceOf(wallet),999999n);assert.ok(await provider.getBalance(wallet)>0n);assert.match(body,/insufficient_usdl/);assert.equal(await page.getByRole('button',{name:'Create token',exact:true}).count(),0);}
   if(name==='insufficientPax'){assert.equal(await usdl.balanceOf(wallet),1000000n);assert.equal(await provider.getBalance(wallet),0n);assert.match(body,/insufficient_gas/);assert.equal(await page.getByRole('button',{name:'Create token',exact:true}).count(),0);}
   if(name==='walletPending'){assert.equal(requests.filter(row=>row.scenario===activeScenario&&row.walletPending&&row.selector===createSelector).length,1);assert.equal(currentAttempt(state.attempts).state,'signing_locked');}
   if(name==='creationSubmitted'){const row=currentAttempt(state.attempts);submitHash=required(row.createTxHash,'durable creation hash');await resolvePool(submitHash);createCountAtSubmit=countCreates();}
   if(name==='launchReloaded'){const row=currentAttempt(state.attempts);assert.equal(row.createTxHash,submitHash);assert.equal(countCreates(),createCountAtSubmit);assert.ok(['submitted','resolved'].includes(row.state));}
   if(name==='settledPurchaseInput'){const quoterSelector=new ethers.Interface(['function quoteExactInput(address,uint256,bool) view returns((uint256,uint256,uint256))']).getFunction('quoteExactInput').selector;record.quoteCalls=requests.filter(row=>row.scenario==='capacity'&&row.to===role('Quoter').toLowerCase()&&row.selector===quoterSelector).length;}
   await page.screenshot({path:path.join(privateDir,`${activeScenario}-${name}.png`),fullPage:true});
  }
  async function actions(steps){
   assert.ok(Array.isArray(steps)&&steps.length>0&&steps.length<=100);
   for(const step of steps){
    if(step.op==='checkpoint'){await checkpoint(step.name);continue;}
    if(step.op==='control'){
     if(step.name==='feeMismatch'){await receipt((await protocol.setCreationFee(2000000n)).hash);continue;}if(step.name==='feeRestore'){await receipt((await protocol.setCreationFee(1000000n)).hash);continue;}
     if(step.name==='holdWallet'){holdSigning=true;continue;}if(step.name==='releaseWallet'){holdSigning=false;required(releaseSigning,'actual pending wallet request')();releaseSigning=undefined;continue;}
     if(step.name==='workerPause'){await fault('workerPause');restoreFaults.push('workerResume');continue;}if(step.name==='workerResume'){await fault('workerResume');restoreFaults=restoreFaults.filter(name=>name!=='workerResume');continue;}
     if(step.name==='metadataDown'){await fault('metadataDown');restoreFaults.push('metadataUp');continue;}if(step.name==='metadataUp'){await fault('metadataUp');restoreFaults=restoreFaults.filter(name=>name!=='metadataUp');continue;}
     if(step.name==='registryChange'){await fault('registryChange');restoreFaults.push('registryRestore');continue;}if(step.name==='registryRestore'){await fault('registryRestore');restoreFaults=restoreFaults.filter(name=>name!=='registryRestore');continue;}
     if(step.name==='createPoolB'){await receipt((await usdl.connect(signer).approve(role('Router'),1000000n)).hash);const tx=await router.createMarket(required(config.token.name,'token.name'),required(config.token.symbol_after,'token.symbol_after'),0,ethers.ZeroAddress);config.pool_b_hash=tx.hash;await receipt(tx.hash);continue;}
     if(step.name==='otherTraderBuy'){assert.ok(quoteBefore,'Review the actual creator quote before changing reserves');const input=BigInt(required(config.other_buy_raw,'other_buy_raw'));assert.ok(input>0n);const quoter=new ethers.Contract(role('Quoter'),['function quoteExactInput(address,uint256,bool) view returns((uint256 amountOut,uint256 feeAmount,uint256 priceImpactBps))'],provider);const quote=await quoter.quoteExactInput(quoteBefore.pool,input,true);const minimum=quote.amountOut*9900n/10000n;assert.ok(minimum>1n);await receipt((await usdl.connect(otherSigner).approve(role('Router'),input)).hash);const trade=await router.connect(otherSigner).buy(quoteBefore.pool,input,minimum,Math.floor(Date.now()/1000)+120);config.other_trade_hash=trade.hash;await receipt(trade.hash);continue;}
     if(step.name==='insufficientUsdl'||step.name==='insufficientPax'||step.name==='restoreFunding'){if(savedUsdl===undefined){savedUsdl=await usdl.balanceOf(wallet);savedPax=await provider.getBalance(wallet);}const target=step.name==='insufficientUsdl'?999999n:step.name==='insufficientPax'?1000000n:savedUsdl;await provider.send('anvil_setBalance',[wallet,ethers.toQuantity(savedPax)]);const current=await usdl.balanceOf(wallet);if(current>target)await receipt((await usdl.connect(signer).transfer(admin,current-target)).hash);if(current<target)await receipt((await usdl.transfer(wallet,target-current)).hash);if(step.name==='insufficientPax')await provider.send('anvil_setBalance',[wallet,'0x0']);if(step.name==='restoreFunding')await provider.send('anvil_setBalance',[wallet,ethers.toQuantity(savedPax)]);continue;}
     if(step.name==='otherTradeReceipt'){await receipt(required(config.other_trade_hash,'actual other_trade_hash'));continue;}
     throw new Error(`Unrecognized actual control ${step.name}`);
    }
    if(step.op==='goto'){const destination=new URL(step.path,origin);assert.equal(destination.origin,origin.origin);await page.goto(destination.href,{waitUntil:'domcontentloaded'});continue;}
    if(step.op==='reload'){await page.reload({waitUntil:'domcontentloaded'});continue;}
    if(step.op==='wait'){assert.ok(Number.isInteger(step.ms)&&step.ms>=1&&step.ms<=30000);await page.waitForTimeout(step.ms);continue;}
    assert.ok(['fill','click','doubleClick','press','visible','text','file'].includes(step.op));assert.equal(typeof step.selector,'string');assert.ok(step.selector.length<=512);const element=page.locator(step.selector);assert.equal(await element.count(),1,'Actual unique UI element is required');
    if(step.op==='fill')await element.fill(step.value);if(step.op==='click')await element.click();if(step.op==='doubleClick')await element.evaluate(node=>{node.click();node.click();});if(step.op==='press')await element.press(step.key);if(step.op==='visible')await element.waitFor({state:'visible',timeout:30000});if(step.op==='text')assert.equal((await element.innerText()).trim(),step.value);if(step.op==='file'){const asset=required(config.approved_image_file,'approved_image_file');assert.equal(sha(fs.readFileSync(asset)),required(config.approved_image_sha256,'approved_image_sha256'));await element.setInputFiles(asset);}
   }
  }
  const requiredPoints={1:['feeMismatch','feeRestored'],2:['approved','symbolEdited','draftReloaded'],3:['poolA','poolB','recoveredA'],4:['metadataCreated','metadataUnavailable','metadataPublished','metadataRetryPublished'],5:['purchaseQuote','reservesChanged','purchaseOutcome'],6:['invalidOptical','registryChanged','standardReview'],7:['insufficientUsdl','insufficientPax'],8:['walletPending','creationSubmitted','launchReloaded']};
  const originalFee=await protocol.creationFee();assert.equal(originalFee,1000000n);
  for(const number of required(config.scenario_order,'scenario_order')){
   assert.ok(Number.isInteger(number)&&number>=1&&number<=8);activeScenario=`scenario-${number}`;const begin=performance.now();
   try{await actions(required(config.scenario_actions[String(number)],`scenario_actions.${number}`));for(const point of requiredPoints[number])assert.ok(checkpoints.has(`${activeScenario}:${point}`),`Missing actual checkpoint ${point}`);samples.push({kind:'scenario',scenario:activeScenario,durationMs:performance.now()-begin,outcome:'passed'});}catch(error){failures.push({scenario:activeScenario,error:error instanceof Error?error.message:'Unknown actual scenario failure'});samples.push({kind:'scenario',scenario:activeScenario,durationMs:performance.now()-begin,outcome:'failed'});throw error;}
  }
  assert.equal(config.scenario_order.length,8);assert.equal(new Set(config.scenario_order).size,8);
  activeScenario='capacity';await actions(required(config.capacity_actions,'capacity_actions'));
  const opticalCalls=requests.filter(row=>row.scenario==='capacity'&&row.method==='eth_call'&&row.to===role('OpticalRegistry').toLowerCase());assert.ok(opticalCalls.length>0,'No actual bounded registry reads');const presetOptions=await page.getByLabel('Optical preset').locator('option').evaluateAll(options=>options.map(option=>option.value));assert.ok(presetOptions.length-1<=50,'Actual registry preset cache exceeded50');assert.equal(requests.filter(row=>row.scenario==='capacity'&&row.method==='eth_estimateGas'&&presetOptions.includes(row.to)).length,0,'Render simulated optical presets');const opticalKeys=new Set();for(const row of opticalCalls){const key=`${required(config.registry_revision,'registry_revision')}:${row.block}:${row.selector}`;assert.ok(!opticalKeys.has(key),'Registry revision render did not reuse bounded cached definitions');opticalKeys.add(key);}
  const observed=await journal();assert.ok(observed.storageWrites.length>=2,'No actual autosave timing samples');for(const write of observed.storageWrites)assert.ok(write.bytes<=65536);
  for(let index=1;index<observed.storageWrites.length;index++)assert.ok(observed.storageWrites[index].at-observed.storageWrites[index-1].at>=1000,'Autosave debounce below1000ms');
  const mutations=responses.filter(row=>['POST','PUT','PATCH','DELETE'].includes(row.method));assert.ok(!mutations.some(row=>row.scenario==='capacity'&&/metadata|assets/.test(row.path)),'Draft typing caused server mutation');
  const feeSelector=new ethers.Interface(['function creationFee() view returns(uint256)']).getFunction('creationFee').selector;
  const configReads=requests.filter(row=>row.method==='eth_call'&&row.to===role('ProtocolConfig').toLowerCase()&&row.selector===feeSelector);
  const readKeys=new Set();for(const row of configReads){const key=`${config.candidate_revision}:${row.block}`;assert.ok(!readKeys.has(key),'Fee configuration was not cached by unchanged deployment/block identity');readKeys.add(key);}
  assert.ok(configReads.length>0,'No actual fee dependency calls observed');
  const balances=requests.filter(row=>row.scenario==='capacity'&&row.method==='eth_call'&&['0x70a08231','0xdd62ed3e'].includes(row.selector));assert.ok(balances.length>0,'No actual balance/allowance samples');assert.ok(balances.every(row=>row.batchSize>1),'Balance/allowance review did not use a batched transport read');
  const estimates=requests.filter(row=>row.scenario==='capacity'&&row.method==='eth_estimateGas');assert.ok(estimates.length>0,'No actual on-demand gas estimate');
  const gasBudget=JSON.parse(fs.readFileSync(required(config.gas_budget_file,'gas_budget_file'),'utf8'));assert.ok(sourceFiles.some(entry=>entry.path===gasBudget.sourcePath&&entry.sha256===gasBudget.sourceSha256),'Gas rate policy is not bound to current source');assert.ok(Number.isSafeInteger(gasBudget.minimumIntervalMs)&&gasBudget.minimumIntervalMs>0);for(let index=1;index<estimates.length;index++)assert.ok(estimates[index].startedMs-estimates[index-1].startedMs>=gasBudget.minimumIntervalMs,'On-demand gas estimate exceeded declared rate');
  const settled=samples.filter(row=>row.name==='settledPurchaseInput');assert.ok(settled.length>=2,'Missing real settled-input quote samples');for(let index=1;index<settled.length;index++)assert.equal(settled[index].quoteCalls-settled[index-1].quoteCalls,1,'Settled input caused more than one quote request');
  for(const row of requests.filter(row=>row.method==='eth_getLogs'&&row.to===role('SidioraFactory').toLowerCase()))assert.equal(row.fromBlock,row.toBlock,'Creation fallback scanned more than the receipt block');
  const receiptReads=requests.filter(row=>row.method==='eth_getTransactionReceipt');for(let index=1;index<receiptReads.length;index++){const current=receiptReads[index],prior=receiptReads[index-1];if(current.transactionHash===prior.transactionHash&&current.scenario===prior.scenario){assert.ok(current.startedMs>prior.startedMs,'Duplicate receipt lookup in one retry');assert.ok(current.startedMs-prior.startedMs<=10000,'Receipt retry interval exceeded10s');}}
  for(const row of responses.filter(row=>row.publication)){assert.ok(row.publication.attemptCount<=5);assert.equal(Date.parse(row.publication.expiresAt)-Date.parse(row.publication.acceptedAt),1800000);}
  const polls=responses.filter(row=>row.method==='GET'&&row.publication);assert.ok(polls.length>=2,'Insufficient actual automatic publication polling samples');for(let index=1;index<polls.length;index++){if(polls[index].publication.operationId===polls[index-1].publication.operationId){const delay=polls[index].atMs-polls[index-1].atMs;assert.ok(delay>=1000&&delay<=30000,'Publication polling interval outside1s..30s');}}
  await actions(required(config.fairness_actions,'fairness_actions'));
  const fairness=JSON.parse(fs.readFileSync(required(config.quote_budget_evidence_file,'quote_budget_evidence_file'),'utf8'));assert.equal(fairness.sourceRevision,config.candidate_revision);assert.equal(fairness.genesisHash,chain.genesisHash);assert.ok(sourceFiles.some(entry=>entry.path===fairness.providerSourcePath&&entry.sha256===fairness.providerSourceSha256),'Shared quote provider evidence is not current-source bound');assert.ok(Array.isArray(fairness.admissions)&&fairness.admissions.length>=2);const priorities=new Set();for(const admission of fairness.admissions){assert.ok([wallet.toLowerCase(),other.toLowerCase()].includes(admission.actor.toLowerCase()));assert.ok(responses.some(row=>row.requestId===admission.requestId&&row.status>=200&&row.status<300),'Shared quote budget sample lacks an actual successful correlated response');assert.ok(Number.isFinite(admission.acceptedAtMs)&&admission.acceptedAtMs>=Date.parse(startedAt)&&admission.acceptedAtMs<=Date.now());assert.equal(admission.bypass,false);priorities.add(admission.priority);}assert.equal(priorities.size,1,'Creation first purchase used a preferred shared quote priority');assert.ok(new Set(fairness.admissions.map(row=>row.actor.toLowerCase())).size===2,'Missing normal trader fairness sample');
  const axeSource=fs.readFileSync(required(config.axe_browser_script,'axe_browser_script'),'utf8');assert.equal(sha(axeSource),required(config.axe_browser_script_sha256,'axe_browser_script_sha256'));
  for(const viewport of [{width:1440,height:1000},{width:768,height:1024},{width:390,height:844}]){
   await page.setViewportSize(viewport);await page.addScriptTag({content:axeSource});const axe=await page.evaluate(async()=>window.axe.run(document.querySelector('main'),{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}}));fs.writeFileSync(path.join(privateDir,`axe-${viewport.width}.json`),JSON.stringify(axe,null,2),{mode:0o600});assert.equal(axe.violations.length,0,'Actual creation accessibility violations');
   const geometry=await page.evaluate(()=>({scroll:document.documentElement.scrollWidth,width:window.innerWidth,main:document.querySelectorAll('main').length}));assert.equal(geometry.main,1);assert.ok(geometry.scroll<=geometry.width,'Creation page horizontal overflow');await page.keyboard.press('Tab');const focus=await page.evaluate(()=>{const node=document.activeElement;return {tag:node?.tagName,outline:node?getComputedStyle(node).outlineStyle:null}});assert.ok(focus.tag&&focus.tag!=='BODY','Keyboard focus did not reach a real control');samples.push({kind:'accessibility',viewport,geometry,focus,violations:axe.violations.length});await page.screenshot({path:path.join(privateDir,`viewport-${viewport.width}.png`),fullPage:true});
  }
  await Promise.all(pendingResponses);
 } catch(error){if(!failures.length)failures.push({scenario:activeScenario,error:error instanceof Error?error.message:'Actual harness failure'});throw error;}
 finally {
  holdSigning=false;if(releaseSigning)releaseSigning();
  for(const restore of restoreFaults.reverse()){try{await fault(restore)}catch{failures.push({scenario:'cleanup',error:`Actual fault restoration failed: ${restore}`})}}
  if(savedPax!==undefined){try{await provider.send('anvil_setBalance',[wallet,ethers.toQuantity(savedPax)]);const current=await usdl.balanceOf(wallet);if(current>savedUsdl)await receipt((await usdl.connect(signer).transfer(admin,current-savedUsdl)).hash);if(current<savedUsdl)await receipt((await usdl.transfer(wallet,savedUsdl-current)).hash);}catch{failures.push({scenario:'cleanup',error:'Actual wallet funding restoration failed'})}}
  try{if(await protocol.creationFee()!==1000000n)await receipt((await protocol.setCreationFee(1000000n)).hash);}catch{failures.push({scenario:'cleanup',error:'Actual protocol fee restoration failed'})}
  if(context){try{await context.tracing.stop({path:path.join(privateDir,'browser-trace.zip')})}catch{failures.push({scenario:'cleanup',error:'Actual browser trace capture failed'})}try{await context.close()}catch{failures.push({scenario:'cleanup',error:'Actual browser context cleanup failed'})}}try{await browser.close()}catch{failures.push({scenario:'cleanup',error:'Actual browser cleanup failed'})}provider.destroy();
  const durationMs=performance.now()-started,latencies=requests.map(row=>row.durationMs).filter(value=>Number.isFinite(value)).sort((a,b)=>a-b),percentile=percentage=>latencies.length?latencies[Math.ceil(latencies.length*percentage)-1]:null;
  fs.writeFileSync(path.join(output,'requests.json'),JSON.stringify(requests,null,2),{mode:0o600});fs.writeFileSync(path.join(output,'responses.json'),JSON.stringify(responses,null,2),{mode:0o600});fs.writeFileSync(path.join(output,'receipts.json'),JSON.stringify(receipts,null,2),{mode:0o600});fs.writeFileSync(path.join(output,'samples.json'),JSON.stringify(samples,null,2),{mode:0o600});
  const artifacts=[];function inventory(dir){for(const entry of fs.readdirSync(dir,{withFileTypes:true})){const file=path.join(dir,entry.name);if(entry.isDirectory())inventory(file);else if(entry.name!=='runner.log')artifacts.push({path:path.relative(output,file),sha256:sha(fs.readFileSync(file)),bytes:fs.statSync(file).size});}}inventory(output);
  const result={schemaVersion:1,lane:'L27',phase:4,sourceRevision:config.candidate_revision,sourceManifestSha256:sourceDigest,sourceFiles,chainId:125,genesisHash:chain.genesisHash,deployment:identities,startedAt,finishedAt:new Date().toISOString(),durationMs,observations:{rpcSamples:requests.length,receiptCount:receipts.length,p50Ms:percentile(.5),p95Ms:percentile(.95),p99Ms:percentile(.99)},scenarioCount:samples.filter(row=>row.kind==='scenario'&&row.outcome==='passed').length,failures,artifacts,status:failures.length?'failed':'observed_lane_checks_passed',productionQualified:false,sharedLoadRestore:'deferred_to_serial9.1',manualAssistiveTechnology:'not_claimed'};
  fs.writeFileSync(path.join(output,'result.json'),JSON.stringify(result,null,2),{mode:0o600});if(failures.length)process.exitCode=1;
 }
}
