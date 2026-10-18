import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { it,expect } from 'vitest';
import { createPublicClient,createWalletClient,createTestClient,defineChain,http,erc20Abi,encodeFunctionData,getAddress,type Hash,type Hex } from 'viem';
import { ChainTradeProvider,EvidenceIntegrityError,transactionFingerprint,type TradeDeployment } from './providers';
import { ReceiptRecoveryScheduler, type RecoveryUpdate } from './recovery';
import { TradeJourney } from './runtime';
import { WalletSessionController } from '@/core/wallet-sdk/session';
import { contextKey,minimumOutput,type TradeContext } from './domain';
import { TransactionStatus } from './transaction-status';
import { useTransactionAttempt } from '@/hooks/tx/use-transaction-attempt';
import { AmountInput } from '../trade-panel-parts';
const wait=(ms:number)=>new Promise(resolve=>setTimeout(resolve,ms));
async function until(check:()=>boolean,timeout=30000){const deadline=Date.now()+timeout;while(!check()){if(Date.now()>=deadline)throw new Error('Actual journey timed out');await wait(50);}}
function fixture(){const path=process.env.L26_FIXTURE_JSON,expected=process.env.L26_FIXTURE_SHA256;if(!path||!expected)throw new Error('Bound actual chain125 deployment fixture required');const bytes=readFileSync(path);expect(createHash('sha256').update(bytes).digest('hex')).toBe(expected);return JSON.parse(bytes.toString());}
function deployConfig(data:ReturnType<typeof fixture>):TradeDeployment{const roles=data.manifest.roles;return {revision:data.manifest.deploymentVersion,router:roles.Router.address,quoter:roles.Quoter.address,registry:roles.PoolRegistry.address,emitter:roles.EventEmitter.address,usdl:roles.USDL.address,pins:['Router','Quoter','PoolRegistry','EventEmitter'].map(name=>({address:roles[name].address,runtimeHash:roles[name].runtimeHash,implementation:roles[name].implementationAddress,implementationHash:roles[name].implementationHash}))};}
it('funded real chain125 quote, zero-reset approvals, receipt recovery, actual swap completion and changed-action cancellation',async()=>{
  const data=fixture();const rpc=process.env.L26_RPC_URL;if(!rpc||!/^http:\/\/127\.0\.0\.1:\d+$/.test(rpc))throw new Error('Owned loopback RPC required');
  const chain=defineChain({id:125,name:'L26 actual private fixture',nativeCurrency:{name:'PAX',symbol:'PAX',decimals:18},rpcUrls:{default:{http:[rpc]}}});
  const publicClient=createPublicClient({chain,transport:http(rpc,{retryCount:0})});expect(await publicClient.getChainId()).toBe(125);
  const wallet=createWalletClient({chain,transport:http(rpc,{retryCount:0})});const accounts=await wallet.getAddresses();const account=getAddress(data.account);expect(accounts).toContain(account);
  const session=new WalletSessionController(()=>{});session.select('owned-anvil','injected',account,125,['owned-anvil']);
  const provider=new ChainTradeProvider(publicClient,wallet,deployConfig(data),session);const info=await provider.inspectPool(getAddress(data.pool));
  expect(info.token.toLowerCase()).toBe(data.token.toLowerCase());const base:TradeContext={chainId:125,manifestRevision:provider.deployment.revision,pool:data.pool,token:provider.deployment.usdl,router:provider.deployment.router,sender:account,recipient:account,inputRaw:1000001n,isBuy:true,opticalRevision:info.opticalRevision,opticalData:'0x'};
  const {JSDOM}=createRequire(import.meta.url)('jsdom');const dom=new JSDOM('<div id="root"></div>',{url:'https://fixture.local'});const prior={window:globalThis.window,document:globalThis.document,navigator:globalThis.navigator};Object.assign(globalThis,{window:dom.window,document:dom.window.document,IS_REACT_ACT_ENVIRONMENT:true});
  let root:ReturnType<typeof createRoot>|undefined;let completed=0;let tradeCompleted=0;let journey:TradeJourney|undefined;let restored:TradeJourney|undefined;
  try{
    journey=new TradeJourney(provider,account,dom.window.localStorage,attempt=>{completed++;if(attempt.action==='buy'||attempt.action==='sell')tradeCompleted++;});
    const Render=()=>{const state=useTransactionAttempt(journey!);return <><AmountInput amount="1.000001" inputDecimals={6} onChange={()=>{}}/><TransactionStatus journey={journey!} state={state}/></>;};
    root=createRoot(dom.window.document.getElementById('root')!);await act(async()=>root!.render(<Render/>));
    expect(dom.window.document.querySelector('input')?.getAttribute('type')).toBe('text');
    journey.setContext(base);journey.setContext({...base,inputRaw:2000001n});await until(()=>!!journey!.state.quote);expect(journey.state.quote?.context.inputRaw).toBe(2000001n);
    journey.setContext(base);await until(()=>!!journey!.state.quote);const quote=journey.state.quote!;expect(contextKey(quote.context)).toBe(contextKey(base));expect(minimumOutput(1000001n,100)).toBe(990000n);
    const balance=await provider.readBalance(provider.deployment.usdl,account);expect(balance>=base.inputRaw).toBe(true);
    const approvalHash=await wallet.writeContract({account,address:provider.deployment.usdl,abi:erc20Abi,functionName:'approve',args:[provider.deployment.router,base.inputRaw]});expect((await publicClient.waitForTransactionReceipt({hash:approvalHash})).status).toBe('success');
    await provider.simulate(base,quote,minimumOutput(quote.outputRaw,100),BigInt(Math.floor(Date.now()/1000)+120));
    await expect(provider.simulate(base,quote,quote.outputRaw+1n,BigInt(Math.floor(Date.now()/1000)+120))).rejects.toThrow();
    await expect(provider.readQuote({...base,recipient:accounts.find(value=>value!==account)!})).rejects.toThrow('recipient_route_unavailable');
    const artifactPath=process.env.L26_ZERO_RESET_ARTIFACT;if(!artifactPath)throw new Error('Compiled actual zero-reset token artifact required');const artifact=JSON.parse(readFileSync(artifactPath,'utf8'));
    const deploymentHash=await wallet.deployContract({account,abi:artifact.abi,bytecode:artifact.bytecode as Hex,args:[100000000n]});const zeroReceipt=await publicClient.waitForTransactionReceipt({hash:deploymentHash});expect(zeroReceipt.status).toBe('success');const zero=zeroReceipt.contractAddress!;
    const first=await wallet.writeContract({account,address:zero,abi:erc20Abi,functionName:'approve',args:[provider.deployment.router,1n]});await publicClient.waitForTransactionReceipt({hash:first});
    const zeroContext={...base,token:zero};expect(await provider.requiresZeroReset(zeroContext,base.inputRaw)).toBe(true);
    journey.setContext(zeroContext);const approvalsBefore=await publicClient.getTransactionCount({address:account});await journey.approve(session.capture());const approvalsAfter=await publicClient.getTransactionCount({address:account});expect(approvalsAfter-approvalsBefore).toBe(2);expect(await provider.readAllowance(zeroContext)).toBe(base.inputRaw);
    const secondDeploy=await wallet.deployContract({account,abi:artifact.abi,bytecode:artifact.bytecode as Hex,args:[100000000n]});const secondToken=(await publicClient.waitForTransactionReceipt({hash:secondDeploy})).contractAddress!;
    await publicClient.waitForTransactionReceipt({hash:await wallet.writeContract({account,address:secondToken,abi:erc20Abi,functionName:'approve',args:[provider.deployment.router,1n]})});
    const beforeSwitch=await publicClient.getTransactionCount({address:account});
    const switched=new TradeJourney(provider,account,dom.window.sessionStorage,attempt=>{if(attempt.action==='approve')session.select('owned-anvil','injected',accounts.find(value=>value!==account)!,125,['owned-anvil']);});
    try{switched.setContext({...base,token:secondToken});await expect(switched.approve(session.capture())).rejects.toThrow();expect(await publicClient.getTransactionCount({address:account})-beforeSwitch).toBe(1);expect(await provider.readAllowance({...base,token:secondToken})).toBe(0n);}finally{switched.close();session.select('owned-anvil','injected',account,125,['owned-anvil']);}
    const approvals=journey.state.attempts.filter(update=>update.attempt.action==='approve');expect(approvals).toHaveLength(2);expect(approvals.every(update=>update.receipt?.status==='success')).toBe(true);
    const sendsBeforeRestore=await publicClient.getTransactionCount({address:account});restored=new TradeJourney(provider,account,dom.window.localStorage);await until(()=>restored!.state.attempts.every(update=>!!update.receipt));expect(await publicClient.getTransactionCount({address:account})).toBe(sendsBeforeRestore);
    journey.setContext(base);await until(()=>!!journey!.state.quote);const submitted=await journey.swap(session.capture());await until(()=>journey!.state.attempts.some(update=>update.attempt.txHash===submitted.txHash&&update.state==='confirmed_success'));expect(provider.fills.get(submitted.txHash.toLowerCase())?.inputRaw).toBe(base.inputRaw);expect(tradeCompleted).toBe(1);const once=completed;journey.retry(submitted.txHash);journey.retry(submitted.txHash);await wait(100);expect(completed).toBe(once);expect(tradeCompleted).toBe(1);
    const quotaDom=new JSDOM('',{url:'https://recovery-quota.local',storageQuota:0});
    const quotaJourney=new TradeJourney(provider,account,quotaDom.window.localStorage);
    const beforeExplorerRecovery=await publicClient.getTransactionCount({address:account});
    try{
      await quotaJourney.recoverHash(submitted.txHash as Hash,'buy');
      await until(()=>quotaJourney.state.attempts[0]?.state==='confirmed_success');
      expect(quotaJourney.state.storageError).toBeTruthy();expect(quotaJourney.state.completion?.attempt.txHash).toBe(submitted.txHash);
      expect(await publicClient.getTransactionCount({address:account})).toBe(beforeExplorerRecovery);
    }finally{quotaJourney.close();quotaDom.window.close();}
    const wrongAction={...submitted,action:'sell'};
    await expect(provider.readReceipt(wrongAction)).rejects.toThrow('swap_expected_action_mismatch');
    await expect(provider.readReceipt({...submitted,from:accounts.find(value=>value!==account)!})).rejects.toThrow('receipt_action_mismatch');
    const wrongEmitter=new ChainTradeProvider(publicClient,wallet,{...deployConfig(data),emitter:provider.deployment.router},session);
    await expect(wrongEmitter.readReceipt(submitted)).rejects.toThrow('swap_evidence_missing_or_duplicate');
    const quarantined=new ReceiptRecoveryScheduler((attempt,signal)=>provider.readReceipt(attempt,signal));
    let quarantinedDeliveries=0;
    try{
      const update=await new Promise<RecoveryUpdate>(resolve=>quarantined.watch(wrongAction,value=>{quarantinedDeliveries++;resolve(value);}));
      expect(update.state).toBe('quarantined');expect(update.error).toBeInstanceOf(EvidenceIntegrityError);expect(update.receipt).toBeUndefined();
      quarantined.wake(submitted.txHash);await wait(600);expect(quarantinedDeliveries).toBe(1);expect(tradeCompleted).toBe(1);
    }finally{quarantined.close();}
    const nonceBeforeSimulationFailure=await publicClient.getTransactionCount({address:account});
    journey.requote();await until(()=>!!journey!.state.quote);
    await expect(journey.swap(session.capture())).rejects.toThrow();
    expect(journey.state.quote).toBeUndefined();expect(journey.state.staleQuote).toBeDefined();expect(journey.state.quoteStatus).toBe('stale');
    expect(await publicClient.getTransactionCount({address:account})).toBe(nonceBeforeSimulationFailure);
    expect(tradeCompleted).toBe(1);
    await publicClient.waitForTransactionReceipt({hash:await wallet.writeContract({account,address:provider.deployment.usdl,abi:erc20Abi,functionName:'approve',args:[provider.deployment.router,base.inputRaw]})});
    journey.requote();await until(()=>!!journey!.state.quote);
    const test=createTestClient({chain,transport:http(rpc),mode:'anvil'});await test.setAutomine(false);
    try{
      const duplicate=await journey.swap(session.capture());
      await expect(journey.swap(session.capture())).rejects.toThrow('unresolved transaction');
      expect(journey.state.attempts.find(update=>update.attempt.txHash===duplicate.txHash)?.receipt).toBeUndefined();
      await test.mine({blocks:1});journey.retry(duplicate.txHash);
      await until(()=>journey!.state.attempts.find(update=>update.attempt.txHash===duplicate.txHash)?.state==='confirmed_success');
      expect(tradeCompleted).toBe(2);
      const revertingData=encodeFunctionData({abi:erc20Abi,functionName:'approve',args:[provider.deployment.router,2n]});
      const delayedHash=await wallet.sendTransaction({account,to:zero,data:revertingData,gas:100000n});
      const delayedRecord={version:1 as const,intentId:'actual-delayed-revert',chainId:125 as const,from:account,txHash:delayedHash,submittedAt:Date.now(),action:'approve',actionFingerprint:transactionFingerprint(zero,revertingData)};
      dom.window.sessionStorage.setItem(`sidiora:pending:1:125:${account.toLowerCase()}`,JSON.stringify([delayedRecord]));let delayedSuccess=0;
      const delayed=new TradeJourney(provider,account,dom.window.sessionStorage,()=>delayedSuccess++);
      try{await until(()=>delayed.state.attempts[0]?.state==='checking',10000);expect(delayed.state.attempts[0].receipt).toBeUndefined();await wait(61000);expect(delayed.state.attempts[0].state).toBe('delayed');expect(delayedSuccess).toBe(0);await test.mine({blocks:1});delayed.retry(delayedHash);await until(()=>delayed.state.attempts[0]?.state==='confirmed_reverted');expect(delayedSuccess).toBe(0);}finally{delayed.close();}
      const nonce=await publicClient.getTransactionCount({address:account});const gasPrice=await publicClient.getGasPrice();const pendingData=provider.swapData(base,1n,BigInt(Math.floor(Date.now()/1000)+120));const original=await wallet.sendTransaction({account,to:provider.deployment.router,data:pendingData,nonce,gas:500000n,gasPrice});
      const pending={version:1 as const,intentId:'actual-pending',chainId:125 as const,from:account,txHash:original,submittedAt:Date.now(),nonce,action:'buy',actionFingerprint:transactionFingerprint(provider.deployment.router,pendingData)};
      dom.window.localStorage.setItem(`sidiora:pending:1:125:${account.toLowerCase()}`,JSON.stringify([pending]));restored.close();restored=new TradeJourney(provider,account,dom.window.localStorage);await until(()=>restored!.state.attempts[0]?.state==='checking',10000);expect(restored.state.attempts[0].receipt).toBeUndefined();
      const cancel=await wallet.sendTransaction({account,to:account,value:0n,nonce,gas:21000n,gasPrice:gasPrice*2n+1n});await test.mine({blocks:1});await restored.inspectReplacement(original,cancel);expect(restored.state.replacement?.state).toBe('cancelled');expect(restored.state.replacement?.original).toBe(original);expect(restored.state.attempts.find(update=>update.attempt.txHash===original)?.state).toBe('cancelled');expect(restored.state.replacement?.changed?.to.toLowerCase()).toBe(account.toLowerCase());
    }finally{await test.setAutomine(true);}
    session.select('owned-anvil','injected',accounts.find(value=>value!==account)!,125,['owned-anvil']);await expect(journey.approve(session.capture())).rejects.toThrow();
  }finally{await act(async()=>root?.unmount());journey?.close();restored?.close();session.dispose();dom.window.close();Object.assign(globalThis,prior);}
},180000);
