import { readFileSync } from 'node:fs';
import { expect, it } from 'vitest';
import { createPublicClient, createWalletClient, defineChain, http, parseAbi, type Address, type PublicClient, type WalletClient } from 'viem';
import { ChainCreationAdapter, creationFunding, type LaunchDeployment } from './creation-adapters';
import { validateCreationDraft, type CreationDraftV2 } from './creation-storage';
import { runtimeCreationAdmission } from './cap-admission';
import { loadRuntimeAuthority, parsePrivateFixtureAuthorityProfileV1 } from '@/core/runtime-authority';

it('executes creation fee, exact approval, native hash resolution and protected first purchase against real compiled chain125 contracts',async()=>{
 const file=process.env.L27_PHASE2_FIXTURE;if(!file)throw new Error('Run the owned chain fixture with --phase2; no synthetic provider is accepted.');
 const fixture=JSON.parse(readFileSync(file,'utf8')).phase2 as {ownedFixture:boolean;rpcUrl:string;creator:Address;admin:Address;deployment:LaunchDeployment;createA:string;createB:string};
 expect(fixture.ownedFixture).toBe(true);const origin=new URL(fixture.rpcUrl);expect(origin.hostname).toBe('127.0.0.1');
 const chain=defineChain({id:125,name:'L27 owned chain',nativeCurrency:{name:'PAX',symbol:'PAX',decimals:18},rpcUrls:{default:{http:[fixture.rpcUrl]}}});
 const client=createPublicClient({chain,transport:http(fixture.rpcUrl)}),wallet=createWalletClient({account:fixture.creator,chain,transport:http(fixture.rpcUrl)}),admin=createWalletClient({account:fixture.admin,chain,transport:http(fixture.rpcUrl)});
 const scope={creator:fixture.creator.toLowerCase(),chainId:125,epoch:1};
 const profileFile=process.env.L27_AUTHORITY_PROFILE;if(!profileFile)throw new Error('Set L27_AUTHORITY_PROFILE to the owned private-fixture authority profile; creation admission is never bypassed.');
 const authority=await loadRuntimeAuthority(AbortSignal.timeout(30000),parsePrivateFixtureAuthorityProfileV1(JSON.parse(readFileSync(profileFile,'utf8'))));
 const adapter=new ChainCreationAdapter(client as PublicClient,wallet as WalletClient,fixture.deployment,()=>scope,runtimeCreationAdmission(authority,fixture.deployment));
 const draft:CreationDraftV2={draftId:crypto.randomUUID(),version:2,creator:fixture.creator.toLowerCase(),chainId:125,name:'L27 phase two',symbol:'l27',description:'',tags:[],socialLinks:[],uploadRefs:[],firstBuyRaw:'1000000',opticalSelection:'standard',updatedAt:Date.now(),feeStrategy:0};
 expect(validateCreationDraft(draft)).toEqual({});
 const first=await adapter.readFunding(draft,scope);expect(first.cost.protocolFeeRaw).toBe('1000000');expect(first.cost.protocolFeeAssetRef).toBe(fixture.deployment.usdl.toLowerCase());expect(BigInt(first.cost.gasEstimatePaxRaw)).toBeGreaterThan(0n);
 expect(creationFunding(first).state).toBe('approval_required');
 const approval=await adapter.approve('1000000',scope);await adapter.approvalProof(approval,'1000000',scope);expect(creationFunding(await adapter.readFunding(draft,scope)).state).toBe('signable');
 const configAbi=parseAbi(['function setCreationFee(uint256)']);
 try {const change=await admin.writeContract({address:fixture.deployment.protocolConfig as Address,abi:configAbi,functionName:'setCreationFee',args:[2000000n]});await client.waitForTransactionReceipt({hash:change});await expect(adapter.readFunding(draft,scope)).rejects.toThrow('configuration_blocked');}
 finally {const restore=await admin.writeContract({address:fixture.deployment.protocolConfig as Address,abi:configAbi,functionName:'setCreationFee',args:[1000000n]});await client.waitForTransactionReceipt({hash:restore});}
 const a=await adapter.resolve(fixture.createA,fixture.creator),b=await adapter.resolve(fixture.createB,fixture.creator);expect(a.state).toBe('resolved');expect(b.state).toBe('resolved');
 if(a.state!=='resolved'||b.state!=='resolved')throw new Error('Actual factory resolution is required.');expect(a.createTxHash).toBe(fixture.createA.toLowerCase());expect(a.poolAddress).not.toBe(b.poolAddress);
 const tx=await adapter.sendCreation(draft,scope);await client.waitForTransactionReceipt({hash:tx as `0x${string}`});const created=await adapter.resolve(tx,fixture.creator);if(created.state!=='resolved')throw new Error('Native creation event did not resolve.');
 expect(created.expectedCreator).toBe(fixture.creator.toLowerCase());expect(created.registryVerified).toBe(true);expect((await adapter.createdPools(fixture.creator)).includes(created.poolAddress)).toBe(true);
 const buyApproval=await adapter.approve(draft.firstBuyRaw,scope);await adapter.approvalProof(buyApproval,draft.firstBuyRaw,scope);
 const quote=await adapter.quotePurchase(created.poolAddress,draft.firstBuyRaw,100,scope);expect(BigInt(quote.minOutRaw)).toBe(BigInt(quote.amountOutRaw)*9900n/10000n);expect(BigInt(quote.minOutRaw)).toBeGreaterThan(1n);
 const buy=await adapter.sendPurchase(quote,scope);expect((await adapter.purchaseReceipt(buy,quote)).status).toBe('success');
 scope.epoch++;await expect(adapter.sendPurchase(quote,{...scope,epoch:scope.epoch-1})).rejects.toThrow('Wallet changed');
},180000);

it('rejects hostile creation and purchase context against the actual deployment without changing balances or registry ownership',async()=>{
 const file=process.env.L27_PHASE2_FIXTURE;if(!file)throw new Error('The real owned creation fixture is required.');
 const fixture=JSON.parse(readFileSync(file,'utf8')).phase2 as {ownedFixture:boolean;rpcUrl:string;creator:Address;admin:Address;deployment:LaunchDeployment;createA:string;createB:string};
 expect(fixture.ownedFixture).toBe(true);expect(new URL(fixture.rpcUrl).hostname).toBe('127.0.0.1');
 const chain=defineChain({id:125,name:'L27 adversarial chain',nativeCurrency:{name:'PAX',symbol:'PAX',decimals:18},rpcUrls:{default:{http:[fixture.rpcUrl]}}});
 const client=createPublicClient({chain,transport:http(fixture.rpcUrl)}),wallet=createWalletClient({account:fixture.creator,chain,transport:http(fixture.rpcUrl)});
 const scope={creator:fixture.creator.toLowerCase(),chainId:125,epoch:7},adapter=new ChainCreationAdapter(client as PublicClient,wallet as WalletClient,fixture.deployment,()=>scope);
 const draft:CreationDraftV2={draftId:crypto.randomUUID(),version:2,creator:scope.creator,chainId:125,name:'Exact e\u0301',symbol:'Exact',description:'',tags:[],socialLinks:[],uploadRefs:[],firstBuyRaw:'0',opticalSelection:'standard',updatedAt:Date.now(),feeStrategy:0};
 const balanceAbi=parseAbi(['function balanceOf(address) view returns (uint256)']);
 const before=await client.readContract({address:fixture.deployment.usdl as Address,abi:balanceAbi,functionName:'balanceOf',args:[fixture.creator]});
 const pools=await adapter.createdPools(fixture.creator);
 await expect(adapter.resolve(fixture.createA,fixture.admin)).rejects.toThrow('scope');
 await expect(adapter.resolve(fixture.createA,fixture.creator,undefined,draft)).rejects.toThrow('reviewed draft');
 for(const fields of [{name:'unsafe\u202Ename'},{socialLinks:['javascript:alert(1)']},{opticalSelection:fixture.deployment.router},{firstBuyRaw:'1e6'}]){
  const hostile={...draft,...fields};expect(Object.keys(validateCreationDraft(hostile)).length).toBeGreaterThan(0);
  await expect(adapter.sendCreation(hostile,scope)).rejects.toThrow('Invalid creation fields');
 }
 const encoded=adapter.calldata(draft);expect(encoded.args[0]).toBe(draft.name);expect(encoded.args[1]).toBe(draft.symbol);
 const recovered=await adapter.resolve(fixture.createA,fixture.creator);expect(recovered.state).toBe('resolved');
 if(recovered.state!=='resolved')throw new Error('Real canonical factory receipt is required.');
 await expect(adapter.quotePurchase(recovered.poolAddress,'1000000',100,{...scope,creator:fixture.admin.toLowerCase()})).rejects.toThrow('Wallet changed');
 await expect(adapter.verifyPurchasePool(recovered.poolAddress,fixture.admin)).rejects.toThrow('reviewed creator');
 const old=await adapter.resolve(fixture.createA,fixture.creator);expect(old.state).toBe('resolved');if(old.state==='resolved')expect(old.poolAddress).toBe(recovered.poolAddress);
 expect(await client.readContract({address:fixture.deployment.usdl as Address,abi:balanceAbi,functionName:'balanceOf',args:[fixture.creator]})).toBe(before);
 expect(await adapter.createdPools(fixture.creator)).toEqual(pools);
 const unavailable=createPublicClient({chain,transport:http('http://127.0.0.1:1',{retryCount:0,timeout:1000})});
 const offline=new ChainCreationAdapter(unavailable as PublicClient,wallet as WalletClient,fixture.deployment,()=>scope);
 await expect(offline.readFunding(draft,scope)).rejects.toThrow();
 scope.epoch++;expect(()=>adapter.scope({...scope,epoch:scope.epoch-1})).toThrow('Wallet changed');
},180000);
