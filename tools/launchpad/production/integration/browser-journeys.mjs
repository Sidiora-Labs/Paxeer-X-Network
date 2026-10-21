import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';
import {createRequire} from 'node:module';

const require = createRequire(import.meta.url);
const phase3 = Boolean(process.env.PHASE3_CONFIG_FILE);
const configuration = JSON.parse(fs.readFileSync(phase3 ? process.env.PHASE3_CONFIG_FILE : process.env.PHASE2_CONFIG_FILE));
const {chromium} = require(configuration.playwright_module);
const ethers = require(path.join(configuration.contract_source_root, 'node_modules/ethers'));
const output = path.resolve(phase3 ? process.env.PHASE3_OUTPUT_DIR : process.env.PHASE2_OUTPUT_DIR);
const phase = phase3 ? process.env.PHASE3_BROWSER_PHASE : process.env.PHASE2_BROWSER_PHASE;
const rpcOrigin = new URL(configuration.rpc_url);
assert.ok(['127.0.0.1','[::1]'].includes(rpcOrigin.hostname));
assert.ok(['http:','https:'].includes(rpcOrigin.protocol));
assert.equal(rpcOrigin.username,'');assert.equal(rpcOrigin.password,'');
const provider = new ethers.JsonRpcProvider(configuration.rpc_url,125,{staticNetwork:true,cacheTimeout:-1});
const origin = new URL(configuration.browser_origin);
assert.ok(['127.0.0.1','localhost','[::1]'].includes(origin.hostname));
assert.equal(origin.protocol, 'https:');
assert.equal(configuration.realm, 'private-anvil125');
assert.equal(configuration.allow_fixture_mutation, true);
assert.equal(await provider.send('eth_chainId',[]), '0x7d');
fs.mkdirSync(output,{recursive:true,mode:0o700});
const chain = JSON.parse(fs.readFileSync(configuration.chain_file));
assert.equal(chain.productionActivation,false);
assert.equal(chain.manifest.chainId,125);
assert.equal(chain.genesisHash, (await provider.getBlock(0)).hash);

async function registry() {
 const frontend = JSON.parse(fs.readFileSync(configuration.frontend_registry_file));
 const backend = JSON.parse(fs.readFileSync(configuration.backend_registry_file));
 const manifest = chain.manifest;
 assert.equal(manifest.native.decimals,18);assert.equal(manifest.settlement.decimals,6);
 assert.equal(frontend.deploymentVersion,manifest.deploymentVersion);
 assert.equal(backend.deploymentVersion,manifest.deploymentVersion);
 assert.equal(frontend.activationHeight,manifest.activationHeight);
 assert.equal(backend.activationHeight,manifest.activationHeight);
 const roles=Object.keys(manifest.roles).sort();
 assert.ok(roles.length>0);assert.deepEqual(Object.keys(frontend.roles).sort(),roles);assert.deepEqual(Object.keys(backend.roles).sort(),roles);
 const observed=[];
 for(const role of roles){
  const expected=manifest.roles[role];assert.deepEqual(frontend.roles[role],expected);assert.deepEqual(backend.roles[role],expected);
  const code=await provider.getCode(expected.address);assert.notEqual(code,'0x');assert.equal(ethers.keccak256(code),expected.runtimeHash);
  const row={role,address:expected.address,runtimeHash:ethers.keccak256(code)};
  if(expected.implementationAddress){
   const implementation=await provider.getCode(expected.implementationAddress);assert.notEqual(implementation,'0x');assert.equal(ethers.keccak256(implementation),expected.implementationHash);
   if(expected.kind==='erc1967'){const slot=await provider.getStorage(expected.address,'0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc');assert.equal(ethers.getAddress('0x'+slot.slice(-40)),ethers.getAddress(expected.implementationAddress));}
   if(expected.kind==='beacon'){const beacon=new ethers.Contract(expected.address,['function implementation() view returns(address)'],provider);assert.equal((await beacon.implementation()).toLowerCase(),expected.implementationAddress.toLowerCase());}
   row.implementationHash=ethers.keccak256(implementation);
  }
  observed.push(row);
 }
 return {phase,enabled_contracts:observed,chain_id:125,genesis:chain.genesisHash,production_qualified:false};
}

async function browserJourney(){
 const browser=await chromium.launch({headless:true});
 const sessionPath=path.join(path.dirname(output),'.private','browser-session.json');
 const useSession=phase3&&fs.existsSync(sessionPath);
 const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1440,height:1000},...(useSession?{storageState:sessionPath}:{})});
 await context.tracing.start({screenshots:true,snapshots:true,sources:true});
 const page=await context.newPage();
 const transactions=[];const receipts=[];const responses=[];const frames=[];let signedMessages=0;
 let miningDisabled=false;let currentJourney=phase;
 const wallet=ethers.getAddress(configuration.wallet);
 await page.exposeFunction('__phase2WalletRequest',async({method,params=[]})=>{
  if(method==='eth_accounts'||method==='eth_requestAccounts')return[wallet];
  if(method==='wallet_requestPermissions'||method==='wallet_getPermissions')return[{parentCapability:'eth_accounts'}];
  if(method==='wallet_switchEthereumChain'){assert.equal(params[0].chainId,'0x7d');return null;}
  if(method==='personal_sign'){
   const message=params[0].length===42?params[1]:params[0];const account=params[0]===message?params[1]:params[0];assert.equal(account.toLowerCase(),wallet.toLowerCase());signedMessages++;
   return (await provider.getSigner(wallet)).signMessage(ethers.getBytes(message));
  }
  if(method==='eth_sendTransaction'){
   assert.equal(params[0].from.toLowerCase(),wallet.toLowerCase());
   const hash=await provider.send(method,params);transactions.push({transactionHash:hash,submittedAt:performance.now(),journey:currentJourney});return hash;
  }
  assert.ok(!method.startsWith('anvil_')&&!method.startsWith('hardhat_')&&!method.startsWith('debug_'));
  return provider.send(method,params);
 });
 await page.addInitScript(()=>{
  const listeners=new Map();const wallet={isMetaMask:true,request:request=>window.__phase2WalletRequest(request),on:(name,callback)=>{if(!listeners.has(name))listeners.set(name,new Set());listeners.get(name).add(callback);},removeListener:(name,callback)=>listeners.get(name)?.delete(callback)};
  Object.defineProperty(window,'ethereum',{value:wallet,configurable:false});
  const detail=Object.freeze({info:{uuid:'429a01fc-bf54-4c4d-944e-b9820eb85098',name:'Phase two actual Anvil wallet',icon:'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg"/>',rdns:'local.phase2.wallet'},provider:wallet});
  window.addEventListener('eip6963:requestProvider',()=>window.dispatchEvent(new CustomEvent('eip6963:announceProvider',{detail})));
  window.dispatchEvent(new CustomEvent('eip6963:announceProvider',{detail}));
 });
 const pendingResponses=[];
 page.on('response',response=>{pendingResponses.push((async()=>{const u=new URL(response.url());if(u.origin!==origin.origin)return;const headers=await response.allHeaders();responses.push({path:u.pathname,method:response.request().method(),status:response.status(),requestId:headers['x-request-id']??headers['x-trace-id']??null});})());});
 page.on('websocket',socket=>socket.on('framereceived',frame=>{try{const value=JSON.parse(String(frame.payload));if(value.op==='message'||value.type==='pool_message'||value.type==='message_persisted'||value.op==='checkpoint')frames.push(value);}catch{}}));
 async function steps(actions){
  assert.ok(Array.isArray(actions)&&actions.length>0&&actions.length<=40);
  for(const action of actions){
   assert.ok(['goto','fill','click','press','visible','text','reload'].includes(action.op));
   if(action.op==='goto'){const destination=new URL(action.path,origin);assert.equal(destination.origin,origin.origin);await page.goto(destination.href,{waitUntil:'domcontentloaded'});continue;}
   if(action.op==='reload'){await page.reload({waitUntil:'domcontentloaded'});continue;}
   assert.equal(typeof action.selector,'string');assert.ok(action.selector.length<=512);const locator=page.locator(action.selector);assert.equal(await locator.count(),1,'journey selector must identify exactly one real element');
   if(action.op==='fill')await locator.fill(action.value.replaceAll('{marker}',configuration.marker));
   if(action.op==='click')await locator.click();
   if(action.op==='press')await locator.press(action.key);
   if(action.op==='visible')await locator.waitFor({state:'visible',timeout:15000});
   if(action.op==='text')assert.equal((await locator.innerText()).trim(),action.value.replaceAll('{marker}',configuration.marker));
  }
 }

 async function gatewayRequest(api, specimen, authenticated) {
  assert.match(specimen.id,/^[a-z0-9-]{1,80}$/);
  const endpoint=new URL(specimen.path,origin);
  assert.equal(endpoint.origin,origin.origin);assert.equal(endpoint.username,'');assert.equal(endpoint.password,'');
  assert.ok(['GET','POST','PUT','PATCH','DELETE'].includes(specimen.method));
  const options={method:specimen.method,failOnStatusCode:false,maxRedirects:0,timeout:configuration.detection_threshold_seconds*1000};
  let input=null;
  if(specimen.body_file){
   input=fs.readFileSync(specimen.body_file);assert.ok(input.length<=40*1024*1024);
   assert.equal(crypto.createHash('sha256').update(input).digest('hex'),specimen.body_sha256);
   options.data=input;options.headers={'Content-Type':specimen.content_type};
  }
  if(specimen.multipart){
   assert.ok(authenticated,'upload must traverse actual authenticated context');
   const asset=specimen.multipart;
   const file=fs.readFileSync(asset.file);assert.ok(file.length<=40*1024*1024);
   assert.equal(crypto.createHash('sha256').update(file).digest('hex'),asset.sha256);
   const message=configuration.marker;
   const signature=await (await provider.getSigner(wallet)).signMessage(message);
   options.multipart={message,signature,[asset.field]:{name:asset.name,mimeType:asset.mime_type,buffer:file}};
   input=file;
  }
  if(specimen.provider_signature){assert.equal(authenticated,false);assert.ok(specimen.provider_signature.length<=1024);options.headers={...(options.headers??{}),'Livepeer-Signature':specimen.provider_signature};}
  const start=performance.now();
  const response=await api.fetch(endpoint.href,options);
  const elapsedMs=performance.now()-start;
  assert.ok(elapsedMs<=configuration.detection_threshold_seconds*1000,'dependency detection exceeded declared bound');
  const body=await response.body();assert.ok(body.length<=1048576);assert.ok(response.status()<300||response.status()>=400,'redirect cannot establish denial');
  const headers=response.headers();
  const record={id:specimen.id,method:specimen.method,path:endpoint.pathname,status:response.status(),elapsed_ms:elapsedMs,input_sha256:input?crypto.createHash('sha256').update(input).digest('hex'):null,output_sha256:crypto.createHash('sha256').update(body).digest('hex'),request_id:headers['x-request-id']??headers['x-trace-id']??null};
  fs.writeFileSync(path.join(output,specimen.id+'-response.bin'),body,{flag:'wx',mode:0o600});
  if(input)fs.writeFileSync(path.join(output,specimen.id+'-request.bin'),input,{flag:'wx',mode:0o600});
  assert.ok(record.request_id,'actual gateway request identity required');
  return {record,text:body.toString('utf8')};
 }
 async function securityJourney(){
  const requests=[];
  if(phase==='scanner-baseline'||phase==='scanner-outage'){
   const result=await gatewayRequest(context.request,configuration.security.scanner_upload,true);requests.push(result.record);
   if(phase==='scanner-baseline')assert.ok(result.record.status>=200&&result.record.status<300,'same genuine image must pass with live scanner');
   else{assert.ok(result.record.status>=400,'scanner unavailable upload was accepted');assert.match(result.text,/scanner|scan.unavailable|quarantin/i,'rejection must be caused by scanner');}
   return {phase,requests,scanner_rejected:phase==='scanner-outage',production_qualified:false};
  }
  if(phase==='webhook-valid'||phase==='webhook-replay'){
   const anonymous=await browser.newContext({ignoreHTTPSErrors:true});
   try{const result=await gatewayRequest(anonymous.request,{...configuration.security.valid_signed_webhook,id:phase},false);assert.ok(result.record.status>=200&&result.record.status<300,'actual valid callback/replay was not accepted');return {phase,requests:[result.record],accepted:true,production_qualified:false};}finally{await anonymous.close();}
  }
  const anonymous=await browser.newContext({ignoreHTTPSErrors:true});
  try{
   const webhook=await gatewayRequest(anonymous.request,configuration.security.unauthenticated_webhook,false);
   assert.ok([401,403].includes(webhook.record.status),'missing provider signature must deny');requests.push(webhook.record);
   const auth=await gatewayRequest(anonymous.request,configuration.security.unauthenticated_mutation,false);
   assert.ok([401,403].includes(auth.record.status),'absent user authorization must deny');requests.push(auth.record);
   for(const key of ['oversized_multipart','polyglot','unsafe_svg']){
    const result=await gatewayRequest(context.request,configuration.security[key],true);
    assert.ok(result.record.status>=400,'unsafe upload was accepted: '+key);requests.push(result.record);
   }
   const replay=await gatewayRequest(anonymous.request,configuration.security.expired_signed_webhook,false);
   assert.ok([401,403].includes(replay.record.status),'expired actual provider signature replay must deny');requests.push(replay.record);
  }finally{await anonymous.close();}
  return {phase,requests,unauthenticated_webhook_denied:true,authorization_denied:true,unsafe_uploads_denied:true,production_qualified:false};
 }
 async function boundedFailure(){
  const spec=configuration.failure_requests[phase];assert.ok(spec,'frozen failure request absent');
  await steps(configuration.browser_steps[phase]);
  const result=await gatewayRequest(context.request,spec,true);
  assert.ok([429,500,502,503,504].includes(result.record.status),'unavailable durable mutation was acknowledged');
  assert.match(result.text,/unavailable|exhaust|quarantin|integrity|retry|limit|dependency/i,'dependency failure must remain explicit');
  const stateSelector=configuration.failure_state_selectors[phase];
  assert.equal(typeof stateSelector,'string');
  await page.locator(stateSelector).waitFor({state:'visible',timeout:configuration.detection_threshold_seconds*1000});
  return {phase,requests:[result.record],mutation_rejected:true,explicit_unavailable:true,production_qualified:false};
 }
 try{
  if(!useSession){await steps(configuration.browser_steps.login);assert.ok(signedMessages>0,'actual wallet signature required');}
  if(phase3&&!useSession){fs.mkdirSync(path.dirname(sessionPath),{recursive:true,mode:0o700});await context.storageState({path:sessionPath});fs.chmodSync(sessionPath,0o600);}
  if(phase3&&['scanner-baseline','scanner-outage','media-auth-adversarial','webhook-valid','webhook-replay'].includes(phase)){
   const result=await securityJourney();await page.screenshot({path:path.join(output,'browser.png'),fullPage:true});return result;
  }
  if(phase3&&['redis-outage','nats-quorum-loss','object-storage-failure','database-pool-exhaustion','rpc-quarantine'].includes(phase)){
   const result=await boundedFailure();await page.screenshot({path:path.join(output,'browser.png'),fullPage:true});return result;
  }
  if(phase3&&['az-surviving','application-recovered'].includes(phase)){
   await steps(configuration.browser_steps.persisted);await page.screenshot({path:path.join(output,'browser.png'),fullPage:true});return {phase,persisted_visible:true,production_qualified:false};
  }
  if(phase==='journeys'){
   currentJourney='create';await steps(configuration.browser_steps.create);
   currentJourney='trade';await steps(configuration.browser_steps.trade);
   await steps(configuration.browser_steps.watchlist);
   await steps(configuration.browser_steps.chat);
   await page.reload({waitUntil:'domcontentloaded'});
   await steps(configuration.browser_steps.persisted);
   assert.ok(transactions.length>=2,'actual browser create and trade submissions required');
   assert.ok(responses.some(r=>r.path.includes('/watchlist')&&r.method==='PUT'&&r.status>=200&&r.status<300),'local storage cannot substitute actual watchlist write');
  }else if(phase==='receipt-withheld-reverted'){
   await provider.send('anvil_setAutomine',[false]);miningDisabled=true;
   await steps(configuration.browser_steps.reverted_trade);
   assert.equal(transactions.length,1,'one deliberately pending actual trade required');
   const tx=transactions[0];const start=performance.now();
   while(performance.now()-start<2500){
    assert.equal(await provider.send('eth_getTransactionReceipt',[tx.transactionHash]),null);
    const state=(await page.locator(configuration.transaction_state_selector).innerText()).trim();
    assert.ok(/pending|submitted|confirming/i.test(state));assert.ok(!/success|confirmed|complete/i.test(state));
    await new Promise(resolve=>setTimeout(resolve,100));
   }
   const pendingElapsed=performance.now()-start;assert.ok(pendingElapsed>2000);
   await provider.send('evm_increaseTime',[3600]);await provider.send('evm_mine',[]);
   const receipt=await provider.send('eth_getTransactionReceipt',[tx.transactionHash]);assert.ok(receipt);assert.equal(receipt.status,'0x0');
   await page.locator(configuration.transaction_reverted_selector).waitFor({state:'visible',timeout:30000});
   tx.pendingElapsedMs=pendingElapsed;
  }else if(phase==='concurrent-candle-fills'){
   await provider.send('anvil_setAutomine',[false]);miningDisabled=true;
   await steps(configuration.browser_steps.concurrent_fill_one);
   await steps(configuration.browser_steps.concurrent_fill_two);
   assert.equal(transactions.length,2);
   await provider.send('evm_mine',[]);
  }else throw new Error('unknown browser scenario');
  for(const tx of transactions){
   const receipt=await provider.send('eth_getTransactionReceipt',[tx.transactionHash]);assert.ok(receipt);assert.equal(receipt.transactionHash,tx.transactionHash);
   const block=await provider.getBlock(receipt.blockHash);assert.ok(block);assert.equal(block.hash,receipt.blockHash);
   receipts.push(receipt);tx.status=receipt.status;tx.blockHash=receipt.blockHash;tx.blockNumber=receipt.blockNumber;
   if(phase!=='receipt-withheld-reverted')assert.equal(tx.status,'0x1');
  }
  if(phase==='concurrent-candle-fills'){assert.equal(receipts[0].blockHash,receipts[1].blockHash);assert.equal(receipts[0].blockNumber,receipts[1].blockNumber);}
  await Promise.all(pendingResponses);
  const ids=responses.filter(r=>r.requestId).map(r=>r.requestId);
  if(phase==='journeys')assert.ok(ids.length>0,'actual HTTP request IDs required');
  await page.screenshot({path:path.join(output,'browser.png'),fullPage:true});
  return{phase,transactions,receipts,http_request_ids:ids,http:responses,frames,candle_fill_hashes:phase==='concurrent-candle-fills'?transactions.map(t=>t.transactionHash.toLowerCase()):[],production_qualified:false};
 }finally{
  if(miningDisabled){await provider.send('anvil_setAutomine',[true]);await provider.send('evm_mine',[]);}
  await context.tracing.stop({path:path.join(output,'browser-trace.zip')});await context.close();await browser.close();
 }
}
const result=phase==='registry'?await registry():await browserJourney();
fs.writeFileSync(path.join(output,'result.json'),JSON.stringify(result,null,2)+'\n',{flag:'wx',mode:0o600});
console.log(JSON.stringify({phase,result:path.join(output,'result.json'),checks:phase==='registry'?result.enabled_contracts.length:result.transactions.length,production_qualified:false}));
