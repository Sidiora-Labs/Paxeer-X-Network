import { createRequire } from 'node:module';
import { readFileSync, writeFileSync, appendFileSync, statSync } from 'node:fs';
import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { resolve } from 'node:path';
import { afterAll, expect, it } from 'vitest';
import { createPublicClient, createWalletClient, createTestClient, defineChain, erc20Abi, encodeFunctionData, formatUnits, getAddress, decodeFunctionData, keccak256, stringToHex, parseAbi, type Hash, type Hex } from 'viem';
import { ChainTradeProvider, tradeRouterAbi, transactionFingerprint, type TradeDeployment } from './providers';
import { TradeJourney, type JourneyObservation } from './runtime';
import { PendingAttemptStore, ReceiptRecoveryScheduler, type RecoveryObservation, type RecoveryUpdate } from './recovery';
import { contextKey, minimumOutput, parseAmount, QuoteRequest, UINT256_MAX, type PendingAttemptRecord, type TradeContext } from './domain';
import { measuredRpcTransport, operationSummary, recoverySummary, assertCompletionEvidence, type RpcObservation, type OperationObservation, type BrowserObservation } from './measurement';

function boundInput(name:string):string{const value=process.env[name];if(!value)throw new Error('Bound actual phase4 measurement inputs required');return value;}
const configurationPath=boundInput('L26_MEASUREMENT_CONFIG'),configurationDigest=boundInput('L26_MEASUREMENT_CONFIG_SHA256'),output=boundInput('L26_MEASUREMENT_OUTPUT');
const bytes=readFileSync(configurationPath);expect(createHash('sha256').update(bytes).digest('hex')).toBe(configurationDigest);const configuration=JSON.parse(bytes.toString());
const fixtureBytes=readFileSync(configuration.fixture_file);expect(createHash('sha256').update(fixtureBytes).digest('hex')).toBe(configuration.fixture_sha256);const fixture=JSON.parse(fixtureBytes.toString());
const rpcSamples:RpcObservation[]=[],operations:OperationObservation[]=[],recovery:RecoveryObservation[]=[],browserObservations:BrowserObservation[]=[],journeyObservations:JourneyObservation[]=[];
const captured=['rpc.ndjson','operations.ndjson','recovery.ndjson','browser.ndjson','journey.ndjson'];
for(const file of captured)writeFileSync(resolve(output,file),'',{flag:'wx',mode:0o600});
let capturedBytes=0;
function append(file:string,value:unknown){const text=JSON.stringify(value,(_,item)=>typeof item==='bigint'?item.toString():item)+'\n';capturedBytes+=Buffer.byteLength(text);if(capturedBytes>configuration.capture_budget_bytes)throw new Error('Actual raw evidence exceeded admitted capture budget');appendFileSync(resolve(output,file),text);}
const transport=measuredRpcTransport(configuration.rpc_url,sample=>{rpcSamples.push(sample);append('rpc.ndjson',sample);},true);
const chain=defineChain({id:125,name:'L26 measured actual fixture',nativeCurrency:{name:'PAX',symbol:'PAX',decimals:18},rpcUrls:{default:{http:[configuration.rpc_url]}}});
const client=createPublicClient({chain,transport,cacheTime:0}),wallet=createWalletClient({chain,transport}),testClient=createTestClient({chain,transport,mode:'anvil'});
const roles=fixture.manifest.roles;
const deployment:TradeDeployment={revision:fixture.manifest.deploymentVersion,router:roles.Router.address,quoter:roles.Quoter.address,registry:roles.PoolRegistry.address,emitter:roles.EventEmitter.address,usdl:roles.USDL.address,pins:['Router','Quoter','PoolRegistry','EventEmitter'].map(role=>({address:roles[role].address,runtimeHash:roles[role].runtimeHash,implementation:roles[role].implementationAddress,implementationHash:roles[role].implementationHash}))};
const provider=new ChainTradeProvider(client,wallet,deployment);
const wait=(ms:number)=>new Promise<void>(done=>setTimeout(done,ms));
async function until(check:()=>boolean,timeout=30000){const deadline=performance.now()+timeout;while(!check()){if(performance.now()>=deadline)throw new Error('Actual measurement condition timed out');await wait(25);}}
async function baseContext():Promise<TradeContext>{
  expect(await client.getChainId()).toBe(125);expect((await client.getBlock({blockNumber:0n})).hash).toBe(fixture.genesisHash);
  const account=getAddress(fixture.account);expect(await wallet.getAddresses()).toContain(account);const info=await provider.inspectPool(getAddress(fixture.pool));
  expect(info.token.toLowerCase()).toBe(fixture.token.toLowerCase());
  return {chainId:125,manifestRevision:deployment.revision,pool:fixture.pool,token:deployment.usdl,router:deployment.router,sender:account,recipient:account,inputRaw:1000000n,isBuy:true,opticalRevision:info.opticalRevision,opticalData:'0x'};
}
function observeRecovery(value:RecoveryObservation){recovery.push(value);append('recovery.ndjson',value);}
function observeJourney(value:JourneyObservation){journeyObservations.push(value);append('journey.ndjson',value);}
function fault(name:string,txHash?:string){
  const control=configuration.faults?.[name];if(!control)throw new Error('Missing actual bound fault authority: '+name);
  for(const [path,digest]of Object.entries(control.inputs))expect(createHash('sha256').update(readFileSync(path)).digest('hex')).toBe(digest);
  expect(createHash('sha256').update(readFileSync(control.argv[0])).digest('hex')).toBe(control.executable_sha256);
  const result=spawnSync(control.argv[0],control.argv.slice(1),{env:{...process.env,L26_OUTPUT_DIR:output,L26_CONFIG_FILE:configurationPath,...(txHash?{L26_CURRENT_TX_HASH:txHash}:{})},timeout:30000,encoding:'utf8'});
  writeFileSync(resolve(output,name+'.log'),result.stdout+'\n'+result.stderr,{flag:'wx',mode:0o600});expect(result.status).toBe(0);
}
afterAll(()=>{
  writeFileSync(resolve(output,'measurement.json'),JSON.stringify({schema_version:1,binding:configuration.binding,configuration_sha256:configurationDigest,deploymentRevision:deployment.revision,genesis:fixture.genesisHash,rpcSamples:rpcSamples.length,operationSamples:operations.length,recoverySamples:recovery.length,browserSamples:browserObservations.length,shared_capacity_qualified:false,shared_restore_qualified:false,production_qualified:false,shared_acceptance_gate:'9.1'},null,2),{flag:'wx',mode:0o600});
});

it('captures actual bounded parsing, exact minimum arithmetic, debouncing and scoped read admission',async()=>{
  const context=await baseContext();
  for(const decimals of [0,6,18])for(const raw of [0n,1n,UINT256_MAX,...Array.from({length:64},()=>BigInt('0x'+randomBytes(32).toString('hex'))) ]){
    const scheduledMs=performance.now(),startedMs=performance.now();const parsed=parseAmount(formatUnits(raw,decimals),decimals);expect(parsed.parsedRaw).toBe(raw);
    const sample:OperationObservation={kind:'operation',id:randomUUID(),operation:'parse',scheduledMs,startedMs,finishedMs:performance.now(),success:true};operations.push(sample);append('operations.ndjson',sample);
  }
  for(const text of ['1e6','1.0000001',(UINT256_MAX+1n).toString(),'9'.repeat(81)])expect(parseAmount(text,6).errorCode).toBeTruthy();
  expect(parseAmount('1.000001',6).parsedRaw).toBe(1000001n);expect(parseAmount('2',6,1000000n).errorCode).toBe('insufficient_balance');
  const callsBefore=rpcSamples.length;const start=performance.now();const observedMinimum=minimumOutput(1000001n,100);expect(observedMinimum).toBe(990000n);expect(rpcSamples.length).toBe(callsBefore);const minimumRpcCalls=rpcSamples.length-callsBefore;
  const minimum:OperationObservation={kind:'operation',id:randomUUID(),operation:'minimum',scheduledMs:start,startedMs:start,finishedMs:performance.now(),success:true};operations.push(minimum);append('operations.ndjson',minimum);
  const {JSDOM}=createRequire(import.meta.url)('jsdom');const dom=new JSDOM('',{url:'https://measurements.local'});
  const journey=new TradeJourney(provider,context.sender,dom.window.localStorage,undefined,observeJourney);
  try{
    const editedAt=performance.now();journey.setContext(context);journey.setContext({...context,inputRaw:2000000n});
    await until(()=>!!journey.state.quote);const starts=journeyObservations.filter(item=>item.kind==='quote_start');expect(starts).toHaveLength(1);expect(starts[0].monotonicMs-editedAt).toBeGreaterThanOrEqual(250);expect(journey.state.quote?.context.inputRaw).toBe(2000000n);
    const requests=new QuoteRequest();const old=requests.request(context,signal=>provider.readQuote(context,signal));requests.cancel();const changed={...context,inputRaw:2000000n};const fresh=requests.request(changed,signal=>provider.readQuote(changed,signal));
    expect(await old.catch(error=>{expect(error.name).toBe('AbortError');return undefined;})).toBeUndefined();expect((await fresh)?.context.inputRaw).toBe(2000000n);
    const before=rpcSamples.length;await Promise.all(Array.from({length:20},()=>provider.readAllowance(context)));
    const reads=rpcSamples.slice(before).filter(sample=>sample.method==='eth_call');expect(reads).toHaveLength(1);
    const accounts=await wallet.getAddresses();expect(accounts.length).toBeGreaterThan(1);const second={...context,sender:accounts.find(account=>account.toLowerCase()!==context.sender.toLowerCase())!,recipient:accounts.find(account=>account.toLowerCase()!==context.sender.toLowerCase())!};
    const sibling=new ChainTradeProvider(client,wallet,deployment);const sharedBefore=rpcSamples.length;await Promise.all([provider.readAllowance(context),sibling.readAllowance(context)]);expect(rpcSamples.slice(sharedBefore).filter(sample=>sample.method==='eth_call')).toHaveLength(1);
    const scopedBefore=rpcSamples.length;await Promise.all([provider.readAllowance(context),provider.readAllowance(second)]);expect(rpcSamples.slice(scopedBefore).filter(sample=>sample.method==='eth_call')).toHaveLength(2);
    const admission=await Promise.allSettled([provider.readQuote(context),provider.readQuote(changed),sibling.readQuote({...context,inputRaw:3000000n})]);expect(admission.filter(result=>result.status==='rejected')).toHaveLength(1);expect(admission[2].status).toBe('rejected');
    if(admission[2].status==='rejected')expect(admission[2].reason.message).toBe('context_read_capacity_exceeded');
  }finally{journey.close();dom.window.close();}
  writeFileSync(resolve(output,'local-math.json'),JSON.stringify({parsing:operationSummary(operations.filter(sample=>sample.operation==='parse')),minimumOutputRaw:observedMinimum.toString(),minimumRpcCalls,debouncedRequestCount:journeyObservations.filter(item=>item.kind==='quote_start').length}),{flag:'wx',mode:0o600});
},60000);

it('records actual peak/surge lane quote and simulation latency without inferring global capacity',async()=>{
  const context=await baseContext();
  const approval=await wallet.writeContract({account:getAddress(context.sender),address:deployment.usdl,abi:erc20Abi,functionName:'approve',args:[deployment.router,context.inputRaw]});expect((await client.waitForTransactionReceipt({hash:approval})).status).toBe('success');
  const summaries=[];
  for(const stage of configuration.measurement_stages){
    const begin=performance.now(),end=begin+stage.duration_ms,pending:Promise<void>[]=[],measured:OperationObservation[]=[];let index=0;
    while(performance.now()<end){
      const scheduledMs=begin+index*1000/stage.rate;if(scheduledMs>=end)break;while(performance.now()<scheduledMs)await wait(Math.ceil(scheduledMs-performance.now()));
      const sequence=index++,operation=sequence%2===0?'quote':'simulation';
      const run=(async()=>{
        const startedMs=performance.now();let success=false,error:string|undefined,quoteCalls=0,simulationCalls=0;
        try{quoteCalls++;const quote=await provider.readQuote(context);if(operation==='simulation'){simulationCalls++;await provider.simulate(context,quote,minimumOutput(quote.outputRaw,100),BigInt(Math.floor(Date.now()/1000)+120));}success=true;}catch(caught){error=caught instanceof Error?caught.message:String(caught);}
        const sample:OperationObservation={kind:'operation',id:randomUUID(),operation,scheduledMs,startedMs,finishedMs:performance.now(),success,error,quoteCalls,simulationCalls,contextKey:contextKey(context)};
        operations.push(sample);append('operations.ndjson',{...sample,profile:stage.profile,warmup:scheduledMs<begin+stage.warmup_ms});if(scheduledMs>=begin+stage.warmup_ms)measured.push(sample);
      })();pending.push(run);
    }
    await Promise.all(pending);const summary=operationSummary(measured);const expectedTotal=Math.ceil(stage.duration_ms*stage.rate/1000),expectedMeasured=expectedTotal-Math.ceil(stage.warmup_ms*stage.rate/1000);summaries.push({profile:stage.profile,allocationRps:stage.allocated_quote_rps,scheduledRps:stage.rate,actuallyStarted:index,expectedTotal,expectedMeasured,generatorSaturated:index!==expectedTotal,quoteCalls:measured.reduce((sum,sample)=>sum+(sample.quoteCalls??0),0),simulationCalls:measured.reduce((sum,sample)=>sum+(sample.simulationCalls??0),0),...summary});
    writeFileSync(resolve(output,'lane-'+stage.profile+'.json'),JSON.stringify(summaries[summaries.length-1]),{flag:'wx',mode:0o600});
    expect.soft(index).toBe(expectedTotal);expect.soft(measured.length).toBe(expectedMeasured);
    expect.soft(summary.samples).toBeGreaterThanOrEqual(100);expect.soft(summary.errorRate).toBeLessThanOrEqual(.001);expect.soft(summary.correctedP95Ms).toBeLessThanOrEqual(250);expect.soft(summary.correctedP99Ms).toBeLessThanOrEqual(750);
  }
  writeFileSync(resolve(output,'lane-capacity.json'),JSON.stringify({summaries,coordinatedOmission:'scheduled-to-finished latency',shared_capacity_qualified:false,shared_acceptance_gate:'9.1'}),{flag:'wx',mode:0o600});
},180000);

it('measures four-reader recovery, real null receipts for sixty seconds and approval reload without writes',async()=>{
  const context=await baseContext();const artifact=JSON.parse(readFileSync(process.env.L26_ZERO_RESET_ARTIFACT!,'utf8'));
  const deploymentHash=await wallet.deployContract({account:getAddress(context.sender),abi:artifact.abi,bytecode:artifact.bytecode as Hex,args:[100000000n]});const tokenReceipt=await client.waitForTransactionReceipt({hash:deploymentHash});expect(tokenReceipt.status).toBe('success');const token=tokenReceipt.contractAddress!;
  await client.waitForTransactionReceipt({hash:await wallet.writeContract({account:getAddress(context.sender),address:token,abi:erc20Abi,functionName:'approve',args:[deployment.router,1n]})});
  const {JSDOM}=createRequire(import.meta.url)('jsdom');const dom=new JSDOM('',{url:'https://pending-recovery.local'});const records:PendingAttemptRecord[]=[];const updates:RecoveryUpdate[]=[];
  const scheduler=new ReceiptRecoveryScheduler((attempt,signal)=>provider.readReceipt(attempt,signal),observeRecovery);let restored:TradeJourney|undefined;let successes=0;
  await testClient.setAutomine(false);
  try{
    const data=encodeFunctionData({abi:erc20Abi,functionName:'approve',args:[deployment.router,2n]});
    for(let index=0;index<8;index++){
      const hash=await wallet.sendTransaction({account:getAddress(context.sender),to:token,data,gas:100000n});const actual=await client.getTransaction({hash});
      const attempt:PendingAttemptRecord={version:1,intentId:randomUUID(),chainId:125,from:context.sender,txHash:hash,submittedAt:Date.now(),nonce:actual.nonce,action:'approve',actionFingerprint:transactionFingerprint(token,data)};records.push(attempt);scheduler.watch(attempt,update=>{updates.push(update);if(update.state==='confirmed_success')successes++;});
    }
    const store=new PendingAttemptStore(dom.window.localStorage,context.sender);for(const record of records)store.save(record);
    expect(store.page(0).records).toHaveLength(8);expect(store.page(25).records).toHaveLength(0);const sends=rpcSamples.filter(sample=>sample.method==='eth_sendTransaction').length;
    restored=new TradeJourney(provider,context.sender,dom.window.localStorage,()=>successes++);const withheldAt=performance.now();await wait(61000);
    expect(performance.now()-withheldAt).toBeGreaterThanOrEqual(60000);expect(successes).toBe(0);expect(restored.state.attempts.every(update=>update.state==='delayed'&&!update.receipt)).toBe(true);
    expect(rpcSamples.filter(sample=>sample.method==='eth_sendTransaction')).toHaveLength(sends);expect(rpcSamples.some(sample=>sample.method==='eth_getTransactionReceipt'&&sample.response&&JSON.parse(sample.response).result===null)).toBe(true);
    const observedWithheldMs=performance.now()-withheldAt;
    await testClient.mine({blocks:1});for(const record of records){scheduler.wake(record.txHash);restored.retry(record.txHash);}
    await until(()=>restored!.state.attempts.every(update=>update.state==='confirmed_reverted'));await until(()=>records.every(record=>updates.some(update=>update.attempt.txHash===record.txHash&&update.state==='confirmed_reverted')));expect(successes).toBe(0);
    writeFileSync(resolve(output,'recovery-summary.json'),JSON.stringify({...recoverySummary(recovery),withheldMs:observedWithheldMs,submittedHashes:records.map(record=>record.txHash),successCounter:successes,additionalApprovalWrites:rpcSamples.filter(sample=>sample.method==='eth_sendTransaction').length-sends,terminalStates:restored.state.attempts.map(update=>update.state)}),{flag:'wx',mode:0o600});
  }finally{scheduler.close();restored?.close();await testClient.setAutomine(true);dom.window.close();}
},100000);

it('captures accessible actual application trade, redelivery, history alignment and optical recipient boundaries',async()=>{
  const {chromium}=createRequire(import.meta.url)(configuration.playwright_entry);const browser=await chromium.launch({headless:true,executablePath:configuration.browser_executable});
  const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1280,height:900}});await context.tracing.start({screenshots:true,snapshots:true,sources:true});const page=await context.newPage();
  const submitted:Hash[]=[];const bundle=JSON.parse(readFileSync(configuration.browser_bundle_file,'utf8'));const assets:Record<string,{sha256:string;status:number}>={},assetReads:Promise<void>[]=[];
  page.on('response',(response:{url():string;status():number;body():Promise<Buffer>})=>{const target=response.url();if(!Object.hasOwn(bundle.assets,target))return;assetReads.push((async()=>{assets[target]={sha256:createHash('sha256').update(await response.body()).digest('hex'),status:response.status()};})());});
  await page.exposeFunction('__l26RealWalletRequest',async({method,params=[]}:{method:string;params:unknown[]})=>{
    if(method==='eth_accounts'||method==='eth_requestAccounts'){const actual=await wallet.getAddresses();expect(actual).toContain(getAddress(fixture.account));return actual.filter(account=>account.toLowerCase()===fixture.account.toLowerCase());}
    if(method==='wallet_requestPermissions'||method==='wallet_getPermissions')return [{parentCapability:'eth_accounts'}];
    if(method==='wallet_switchEthereumChain'){expect((params[0]as {chainId:string}).chainId).toBe('0x7d');return null;}
    if(method==='personal_sign'){const first=String(params[0]),second=String(params[1]);const message=first.length===42?second:first,account=first.length===42?first:second;expect(account.toLowerCase()).toBe(fixture.account.toLowerCase());return wallet.signMessage({account:getAddress(account),message:{raw:message as Hex}});}
    if(/^(anvil_|evm_|hardhat_|debug_)/.test(method))throw new Error('Wallet cannot invoke fixture fault methods');
    if(method==='eth_sendTransaction')expect((params[0] as {from:string}).from.toLowerCase()).toBe(fixture.account.toLowerCase());
    const result=await client.request({method,params}as Parameters<typeof client.request>[0]);if(method==='eth_sendTransaction')submitted.push(result as Hash);return result;
  });
  await page.addInitScript(()=>{
    const w=window as unknown as {__L26_MEASUREMENT_ACTIVE__:boolean;__l26Observations:unknown[];__l26RealWalletRequest:(request:unknown)=>Promise<unknown>;ethereum:unknown};w.__L26_MEASUREMENT_ACTIVE__=true;w.__l26Observations=[];window.addEventListener('sidiora:trade-measurement',event=>w.__l26Observations.push((event as CustomEvent).detail));
    const listeners=new Map<string,Set<(...args:unknown[])=>void>>();const wallet={request:(request:unknown)=>w.__l26RealWalletRequest(request),on:(name:string,listener:(...args:unknown[])=>void)=>{const set=listeners.get(name)??new Set();set.add(listener);listeners.set(name,set);},removeListener:(name:string,listener:(...args:unknown[])=>void)=>listeners.get(name)?.delete(listener)};
    Object.defineProperty(window,'ethereum',{value:wallet,configurable:false});const detail=Object.freeze({info:{uuid:'429a01fc-bf54-4c4d-944e-b9820eb85098',name:'Actual measurement wallet',icon:'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg"/>',rdns:'local.l26.wallet'},provider:wallet});window.addEventListener('eip6963:requestProvider',()=>window.dispatchEvent(new CustomEvent('eip6963:announceProvider',{detail})));window.dispatchEvent(new CustomEvent('eip6963:announceProvider',{detail}));
  });
  try{
    for(const action of configuration.browser_steps){
      if(action.op==='goto'){const url=new URL(action.path,configuration.browser_origin);expect(url.origin).toBe(new URL(configuration.browser_origin).origin);await page.goto(url.href,{waitUntil:'domcontentloaded'});continue;}
      if(action.op==='redeliver'){const observed=await page.evaluate(()=>(window as unknown as {__l26Observations:BrowserObservation[]}).__l26Observations);const confirmed=observed.filter((item:BrowserObservation)=>item.kind==='trade_confirmed').at(-1);expect(confirmed?.txHash).toBeTruthy();fault('redeliver_three',confirmed!.txHash);continue;}
      if(action.op==='reload'){await page.reload({waitUntil:'domcontentloaded'});continue;}
      expect(['fill','click','press','text','visible']).toContain(action.op);const element=page.locator(action.selector);expect(await element.count()).toBe(1);
      if(action.op==='fill')await element.fill(action.value);if(action.op==='click')await element.click();if(action.op==='press')await element.press(action.key);if(action.op==='visible')await element.waitFor({state:'visible',timeout:30000});if(action.op==='text')expect((await element.innerText()).trim()).toBe(action.value);
    }
    await Promise.all(assetReads);for(const [url,digest]of Object.entries(bundle.assets)){expect(assets[url]?.status).toBe(200);expect(assets[url]?.sha256).toBe(digest);}writeFileSync(resolve(output,'browser-assets.json'),JSON.stringify({candidate_sha256:configuration.binding.candidate,assets}),{flag:'wx',mode:0o600});
    const amount=page.getByRole('textbox',{name:'Trade amount',exact:true});expect(await amount.count()).toBe(1);expect(await amount.getAttribute('inputmode')).toBe('decimal');expect(await amount.getAttribute('maxlength')).toBe('80');await amount.focus();expect(await amount.evaluate((element:Element)=>element===document.activeElement)).toBe(true);
    const observed=await page.evaluate(()=>(window as unknown as {__l26Observations:BrowserObservation[]}).__l26Observations);for(const item of observed){browserObservations.push(item);append('browser.ndjson',item);}
    const successful=browserObservations.filter(item=>item.kind==='trade_confirmed');expect(successful.length).toBeGreaterThan(0);const completions=[];
    for(const item of successful){expect(submitted.map(hash=>hash.toLowerCase())).toContain(item.txHash!.toLowerCase());const tx=await client.getTransaction({hash:item.txHash as Hash});const decoded=decodeFunctionData({abi:tradeRouterAbi,data:tx.input});expect(tx.from.toLowerCase()).toBe(fixture.account.toLowerCase());expect(tx.to?.toLowerCase()).toBe(deployment.router.toLowerCase());const evidence=await provider.readReceipt({chainId:125,intentId:item.txHash!,from:fixture.account,txHash:item.txHash!,nonce:tx.nonce,submittedAt:item.at,action:decoded.functionName,actionFingerprint:transactionFingerprint(tx.to!,tx.input,tx.value)});expect(evidence?.status).toBe('success');expect(browserObservations.filter(event=>event.kind==='invalidate_batch'&&event.txHash===item.txHash)).toHaveLength(1);completions.push(assertCompletionEvidence(browserObservations,item.txHash!));}
    await page.screenshot({path:resolve(output,'trade-desktop.png'),fullPage:true});await page.setViewportSize({width:390,height:844});const launcher=page.locator(configuration.mobile_drawer_selector);expect(await launcher.count()).toBe(1);await launcher.click();const drawer=page.getByRole('dialog',{name:'Trade',exact:true});await drawer.waitFor({state:'visible'});expect(await drawer.getAttribute('aria-modal')).toBe('true');await page.screenshot({path:resolve(output,'trade-mobile.png'),fullPage:true});await page.keyboard.press('Escape');await drawer.waitFor({state:'hidden'});
    writeFileSync(resolve(output,'browser-completion.json'),JSON.stringify({completions,submitted,analyticsInvocationMeasured:successful.length>0,analyticsDeliveryQualified:false}),{flag:'wx',mode:0o600});
    const analyticsAuthority=configuration.faults?.capture_analytics;if(!analyticsAuthority?.result_file)throw new Error('Missing actual analytics delivery capture authority; callback invocation is insufficient');const captureStarted=Date.now();fault('capture_analytics');const analyticsPath=resolve(analyticsAuthority.result_file);expect(analyticsPath.startsWith(resolve(output)+'/')).toBe(true);expect(statSync(analyticsPath).mtimeMs).toBeGreaterThanOrEqual(captureStarted);const analyticsBytes=readFileSync(analyticsPath);expect(analyticsBytes.length).toBeGreaterThan(0);expect(analyticsBytes.length).toBeLessThanOrEqual(1048576);const analytics=JSON.parse(analyticsBytes.toString());expect(analytics.binding.candidate).toBe(configuration.binding.candidate);expect(analytics.deploymentRevision).toBe(deployment.revision);
    for(const completed of successful){const events=analytics.events.filter((event:{event:string;chainId:number;txHash:string})=>event.event==='trade_confirmed'&&event.chainId===125&&event.txHash.toLowerCase()===completed.txHash!.toLowerCase());expect(events).toHaveLength(1);expect(events[0].providerAcknowledgmentId).toMatch(/^.{1,256}$/);expect(events[0].providerStatus).toBeGreaterThanOrEqual(200);expect(events[0].providerStatus).toBeLessThan(300);expect(events[0].observedAt).toBeGreaterThanOrEqual(completed.at);}
    writeFileSync(resolve(output,'analytics-binding.json'),JSON.stringify({sha256:createHash('sha256').update(analyticsBytes).digest('hex'),bytes:analyticsBytes.length,events:analytics.events}),{flag:'wx',mode:0o600});
    const minBoundary=configuration.minimum_boundary;if(!minBoundary)throw new Error('Missing real minimum-output boundary fixture authority (1000001 quote / 989999 actual output)');
    expect(minBoundary.context.chainId).toBe(125);expect(typeof minBoundary.context.inputRaw).toBe('string');expect(minBoundary.context.inputRaw).toMatch(/^[0-9]{1,78}$/);const boundaryContext={...minBoundary.context,inputRaw:BigInt(minBoundary.context.inputRaw)} as TradeContext;contextKey(boundaryContext);expect(boundaryContext.sender.toLowerCase()).toBe(fixture.account.toLowerCase());
    const quote=await provider.readQuote(boundaryContext);expect(quote.outputRaw).toBe(1000001n);fault('minimum_output_change');const changed=await provider.readQuote(boundaryContext);expect(changed.outputRaw).toBe(989999n);expect(await provider.readAllowance(boundaryContext)).toBeGreaterThanOrEqual(boundaryContext.inputRaw);expect(await provider.readBalance(getAddress(boundaryContext.token),getAddress(boundaryContext.sender))).toBeGreaterThanOrEqual(boundaryContext.inputRaw);
    let revertedData:string|undefined;try{await provider.simulate(boundaryContext,quote,990000n,BigInt(Math.floor(Date.now()/1000)+120));throw new Error('Actual below-minimum trade unexpectedly simulated');}catch(error){let current:unknown=error;for(let depth=0;depth<8&&current&&typeof current==='object';depth++){const record=current as {data?:unknown;cause?:unknown};if(typeof record.data==='string')revertedData=record.data;else if(record.data&&typeof record.data==='object'&&'data'in record.data&&typeof record.data.data==='string')revertedData=record.data.data;current=record.cause;}}
    expect(revertedData?.slice(0,10)).toBe(keccak256(stringToHex('SlippageExceeded()')).slice(0,10));
    expect(typeof minBoundary.gas_raw).toBe('string');expect(minBoundary.gas_raw).toMatch(/^[0-9]{1,78}$/);const gas=BigInt(minBoundary.gas_raw);expect(gas).toBeGreaterThan(0n);expect(gas).toBeLessThanOrEqual(UINT256_MAX);
    const beforeBalance=await provider.readBalance(getAddress(boundaryContext.token),getAddress(boundaryContext.sender));const data=provider.swapData(boundaryContext,990000n,BigInt(Math.floor(Date.now()/1000)+120));const boundaryHash=await wallet.sendTransaction({account:getAddress(boundaryContext.sender),to:deployment.router,data,gas});const actualReceipt=await client.waitForTransactionReceipt({hash:boundaryHash});expect(actualReceipt.status).toBe('reverted');const afterBalance=await provider.readBalance(getAddress(boundaryContext.token),getAddress(boundaryContext.sender));expect(afterBalance).toBe(beforeBalance);
    writeFileSync(resolve(output,'minimum-boundary.json'),JSON.stringify({quotedOutputRaw:quote.outputRaw,observedAfterChangeRaw:changed.outputRaw,reviewedMinimumRaw:minimumOutput(quote.outputRaw,100),txHash:boundaryHash,receipt:actualReceipt,beforeBalance,afterBalance},(_,value)=>typeof value==='bigint'?value.toString():value),{flag:'wx',mode:0o600});
    if(!configuration.optical_boundary)throw new Error('Missing admitted alternate-recipient max-wallet route and exact-context optical quote authority');fault('optical_recipient_boundary');
    const optical=configuration.optical_boundary;const opticalPool=getAddress(optical.pool),recipient=getAddress(optical.recipient),sender=getAddress(fixture.account);expect(recipient.toLowerCase()).not.toBe(sender.toLowerCase());
    const info=await provider.inspectPool(opticalPool);expect(keccak256((await client.getCode({address:info.optical}))!)).toBe(optical.runtime_hash);
    expect(typeof optical.output_raw).toBe('string');expect(optical.output_raw).toMatch(/^[0-9]{1,78}$/);const amountOut=BigInt(optical.output_raw);expect(amountOut).toBeGreaterThan(0n);expect(amountOut).toBeLessThanOrEqual(UINT256_MAX);
    const blockNumber=await client.getBlockNumber();const hookAbi=parseAbi(['function maxWalletBps() view returns(uint256)','function beforeTokenTransfer(address pool,address from,address to,uint256 amount) view']);
    const [supply,bps,senderBalance,recipientBalance]=await Promise.all([client.readContract({address:info.token,abi:erc20Abi,functionName:'totalSupply',blockNumber}),client.readContract({address:info.optical,abi:hookAbi,functionName:'maxWalletBps',blockNumber}),provider.readBalance(info.token,sender,blockNumber),provider.readBalance(info.token,recipient,blockNumber)]);const cap=supply*bps/10000n;expect(senderBalance+amountOut).toBeLessThanOrEqual(cap);expect(recipientBalance+amountOut).toBeGreaterThan(cap);
    const senderData=encodeFunctionData({abi:hookAbi,functionName:'beforeTokenTransfer',args:[opticalPool,opticalPool,sender,amountOut]}),recipientData=encodeFunctionData({abi:hookAbi,functionName:'beforeTokenTransfer',args:[opticalPool,opticalPool,recipient,amountOut]});const signedBefore=submitted.length;
    const senderResult=await client.call({account:info.token,to:info.optical,data:senderData,blockNumber});let recipientRejection:string|undefined;
    try{await client.call({account:info.token,to:info.optical,data:recipientData,blockNumber});}catch(error){let current:unknown=error;for(let depth=0;depth<8&&current&&typeof current==='object';depth++){const record=current as {data?:unknown;cause?:unknown};if(typeof record.data==='string')recipientRejection=record.data;else if(record.data&&typeof record.data==='object'&&'data'in record.data&&typeof record.data.data==='string')recipientRejection=record.data.data;current=record.cause;}}
    expect(recipientRejection?.slice(0,10)).toBe(keccak256(stringToHex('MaxWalletExceeded()')).slice(0,10));expect(submitted.length-signedBefore).toBe(0);
    writeFileSync(resolve(output,'optical-boundary.json'),JSON.stringify({pool:opticalPool,optical:info.optical,blockNumber,sender,recipient,amountOut,supply,bps,cap,senderBalance,recipientBalance,senderResult,recipientRejection,additionalSignatures:submitted.length-signedBefore},(_,value)=>typeof value==='bigint'?value.toString():value),{flag:'wx',mode:0o600});
    const recipientContext:TradeContext={chainId:125,manifestRevision:deployment.revision,pool:opticalPool,token:deployment.usdl,router:deployment.router,sender,recipient,inputRaw:1000000n,isBuy:true,opticalRevision:info.opticalRevision,opticalData:'0x'};
    await expect(provider.readQuote(recipientContext)).rejects.toThrow('MaxWalletExceeded');
  }finally{await context.tracing.stop({path:resolve(output,'browser-trace.zip')});await browser.close();}
},240000);
