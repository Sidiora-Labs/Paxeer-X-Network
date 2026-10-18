import { createContext,createElement,useContext,type ReactNode } from 'react';
import { decodeFunctionData, decodeErrorResult, decodeEventLog, erc20Abi, getAddress, keccak256, parseAbi, stringToHex, zeroAddress, type Address, type Hash, type Hex, type PublicClient, type WalletClient } from 'viem';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import type { AccountScope, WalletSessionController } from '@/core/wallet-sdk/session';
import { EvidenceIntegrityError, executableQuote, raw, contextKey, encodeReviewedRouterCalldata, reviewedRouterAbi, quoteFingerprint, type TradeContext, type QuoteEnvelope, type OpticalSimulation, type TxAttempt, type ReceiptEvidence } from './domain';

export const tradeRouterAbi = reviewedRouterAbi;
const quoterAbi = parseAbi(['function quoteExactInput(address pool,uint256 amountIn,bool isBuy) view returns((uint256 amountOut,uint256 feeAmount,uint256 priceImpactBps))']);
const registryAbi = parseAbi(['function getPoolByToken(address token) view returns(address)','function isRegisteredPool(address pool) view returns(bool)']);
const poolAbi = parseAbi(['function tokenAddress() view returns(address)','function opticalAddress() view returns(address)']);
const swapAbi = parseAbi(['event Swap(bytes32 indexed poolId,address indexed sender,bool isBuy,uint256 amountIn,uint256 amountOut,uint256 fee,uint256 price,uint256 timestamp,uint256 blockNumber)']);
const opticalErrors=parseAbi(['error CooldownActive()','error MaxWalletExceeded()','error ProtectionActive()','error CliffActive()','error VestingExceeded()','error OpticalQuoteUnsupported()']);
export class OpticalRejection extends Error {
  constructor(readonly reason:string,readonly retryEligibility:string){super(`${reason}; retry: ${retryEligibility}`);this.name='OpticalRejection';}
}
function opticalRejection(error:unknown):unknown {
  let current:unknown=error;
  for(let index=0;index<8&&current&&typeof current==='object';index++){
    const record=current as {data?:unknown;cause?:unknown};const data=typeof record.data==='string'?record.data:(record.data&&typeof record.data==='object'&&'data' in record.data?record.data.data:undefined);
    if(typeof data==='string'&&/^0x[0-9a-f]+$/i.test(data)){try{const decoded=decodeErrorResult({abi:opticalErrors,data:data as Hex});const retry:Record<string,string>={CooldownActive:'after cooldown',MaxWalletExceeded:'reduce amount or recipient balance',ProtectionActive:'after anti-snipe protection',CliffActive:'after vesting cliff',VestingExceeded:'within vested amount',OpticalQuoteUnsupported:'context-aware optical quote provider required'};return new OpticalRejection(decoded.errorName,retry[decoded.errorName]);}catch{}}
    current=record.cause;
  }
  return error;
}
const implementationSlot = '0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc' as const;
export interface CodePin { address: Address; runtimeHash: Hash; implementation?: Address; implementationHash?: Hash }
export interface TradeDeployment { revision: string; router: Address; quoter: Address; registry: Address; emitter: Address; usdl: Address; pins: readonly CodePin[] }
const proxyHash = '0xb2f39d5c380b45131e836d4beb5254d4e5c410d35935bf270e8492755f7da6f7' as Hash;
export const productionTradeDeployment: TradeDeployment = {
  revision: CORE_DEPLOYMENT.manifestSha256, router: CORE_DEPLOYMENT.router, quoter: CORE_DEPLOYMENT.quoter, registry: CORE_DEPLOYMENT.poolRegistry, emitter: CORE_DEPLOYMENT.eventEmitter, usdl: CORE_DEPLOYMENT.usdl,
  pins: [
    {address: CORE_DEPLOYMENT.router,runtimeHash:proxyHash,implementation:'0xfcBce73f397dF65D5b18364dadDCaE86E95CdB96',implementationHash:'0x3377796c705ad082dfa476c61f25f9cbf64f39fb80dad47de4f5220a4e65190c'},
    {address:CORE_DEPLOYMENT.quoter,runtimeHash:proxyHash,implementation:'0x49Fe29eaAcb16d3D1a8671dD4d6D9070E340B5B8',implementationHash:'0x26be23199e2a93fc8c3b83982ac9e9dca7e065664bc83b028bf5c804d2a5320c'},
    {address:CORE_DEPLOYMENT.poolRegistry,runtimeHash:proxyHash,implementation:'0x0e4d66894321e7e29803eF63E618bF07B9772687',implementationHash:'0xc74db1081c5791cda0336d914cdc44cd31cfb656c8b8aaa0c2e200123503e216'},
    {address:CORE_DEPLOYMENT.eventEmitter,runtimeHash:proxyHash,implementation:'0x6d48a32e313cc4F08acdeE1F39F7e6B56F03a1cB',implementationHash:'0xd0dc6f1aa54811dc2b9ea13e4519b6b6992ba0a082ac55c2d9e312443b289c08'},
  ],
};
const TradeExecutionContext=createContext<TradeDeployment|undefined>(undefined);
export function TradeExecutionProvider({deployment,children}:{deployment?:TradeDeployment;children:ReactNode}) {
  return createElement(TradeExecutionContext.Provider,{value:deployment},children);
}
export function useTradeExecutionDeployment(){return useContext(TradeExecutionContext);}
export function useRequiredTradeExecutionDeployment(){const deployment=useTradeExecutionDeployment();if(!deployment)throw new Error('Attested trade execution authority unavailable');return deployment;}
export interface ActualFill { inputRaw: bigint; outputRaw: bigint; feeRaw: bigint; blockHeight: bigint; isBuy: boolean; pool: Address; token: Address; tokenDecimals: number }
export { EvidenceIntegrityError } from './domain';
function same(a:string,b:string) { return a.toLowerCase()===b.toLowerCase(); }
function alive(signal?:AbortSignal) { if(signal?.aborted) throw new DOMException('Cancelled','AbortError'); }
export function transactionFingerprint(to: string, data: string, value = 0n): string { return keccak256(stringToHex(JSON.stringify([125,to.toLowerCase(),data.toLowerCase(),value.toString()]))); }
const activeContextReads=new Map<string,number>();
const allowanceReadsByClient=new WeakMap<PublicClient,Map<string,Promise<bigint>>>();
export class ChainTradeProvider {
  private readonly activeContextReads=activeContextReads;
  private readonly allowanceReads:Map<string,Promise<bigint>>;
  readonly fills = new Map<string,ActualFill>();
  private readonly simulations = new Map<string,{quote:QuoteEnvelope;minimum:bigint;deadline:bigint;expiresAt:number}>();
  constructor(readonly publicClient: PublicClient, readonly walletClient?: WalletClient, readonly deployment: TradeDeployment = productionTradeDeployment, readonly session?: WalletSessionController) {
    this.allowanceReads=allowanceReadsByClient.get(publicClient)??new Map();allowanceReadsByClient.set(publicClient,this.allowanceReads);
    for (const role of [deployment.router,deployment.quoter,deployment.registry,deployment.emitter]) if(!deployment.pins.some(pin=>same(pin.address,role))) throw new Error('missing_deployment_code_pin');
  }
  async verify(blockNumber:bigint,signal?:AbortSignal) {
    alive(signal); if(await this.publicClient.getChainId()!==125) throw new EvidenceIntegrityError('wrong_chain');
    for (const pin of this.deployment.pins) {
      const code=await this.publicClient.getCode({address:pin.address,blockNumber});
      if(!code||keccak256(code)!==pin.runtimeHash) throw new EvidenceIntegrityError('deployment_code_mismatch');
      if(pin.implementation){
        const slot=await this.publicClient.getStorageAt({address:pin.address,slot:implementationSlot,blockNumber});
        if(!slot||!same(`0x${slot.slice(-40)}`,pin.implementation)) throw new EvidenceIntegrityError('deployment_implementation_mismatch');
        const impl=await this.publicClient.getCode({address:pin.implementation,blockNumber});
        if(!impl||keccak256(impl)!==pin.implementationHash) throw new EvidenceIntegrityError('deployment_implementation_code_mismatch');
      }
    } alive(signal);
  }
  async inspectPool(pool:Address,signal?:AbortSignal) {
    const blockNumber=await this.publicClient.getBlockNumber(); await this.verify(blockNumber,signal);
    const [registered,token,optical]=await Promise.all([
      this.publicClient.readContract({address:this.deployment.registry,abi:registryAbi,functionName:'isRegisteredPool',args:[pool],blockNumber}),
      this.publicClient.readContract({address:pool,abi:poolAbi,functionName:'tokenAddress',blockNumber}),
      this.publicClient.readContract({address:pool,abi:poolAbi,functionName:'opticalAddress',blockNumber}),
    ]);
    const ownerPool=await this.publicClient.readContract({address:this.deployment.registry,abi:registryAbi,functionName:'getPoolByToken',args:[token],blockNumber});
    if(!registered||!same(ownerPool,pool)) throw new Error('unregistered_pool_token');
    const [decimals,usdlDecimals,tokenCode]=await Promise.all([
      this.publicClient.readContract({address:token,abi:erc20Abi,functionName:'decimals',blockNumber}),
      this.publicClient.readContract({address:this.deployment.usdl,abi:erc20Abi,functionName:'decimals',blockNumber}),
      this.publicClient.getCode({address:token,blockNumber}),
    ]);
    alive(signal); if(!tokenCode||usdlDecimals!==6||decimals>77) throw new Error('invalid_asset_identity');
    const restrictions:string[]=[];
    if(same(optical,zeroAddress))restrictions.push('No trading limits');
    else {
      const getters=[['cooldownSeconds','Cooldown (seconds)'],['maxWalletBps','Max wallet (bps)'],['maxBuyBps','Max buy at launch (bps)'],['protectionBlocks','Launch protection (blocks)'],['buyTaxBps','Buy tax (bps)'],['sellTaxBps','Sell tax (bps)']] as const;
      for(const [getter,label] of getters){try{const abi=parseAbi([`function ${getter}() view returns(uint256)`]);const value=await this.publicClient.readContract({address:optical,abi,functionName:getter,blockNumber});restrictions.push(`${label}: ${String(value)}`);}catch{restrictions.push(`${label}: unknown`);}}
    }
    alive(signal);
    return {token,optical,poolId:(`0x${pool.slice(2).toLowerCase().padStart(64,'0')}` as Hash),decimals,usdlDecimals,blockNumber,opticalRevision:optical===zeroAddress?'none':keccak256((await this.publicClient.getCode({address:optical,blockNumber}))??'0x'),restrictions};
  }
  private route(context:TradeContext) {
    contextKey(context);
    if(context.manifestRevision!==this.deployment.revision||!same(context.router,this.deployment.router)) throw new Error('route_deployment_mismatch');
    if(!same(context.sender,context.recipient)) throw new Error('recipient_route_unavailable');
    if(context.opticalData!=='0x') throw new Error('optical_payload_route_unavailable');
  }
  private async contextRead<T>(context:TradeContext,read:()=>Promise<T>):Promise<T>{
    contextKey(context);const owner=context.sender.toLowerCase(),active=this.activeContextReads.get(owner)??0;
    if(active>=2)throw new Error('context_read_capacity_exceeded');
    this.activeContextReads.set(owner,active+1);
    try{return await read();}finally{const remaining=(this.activeContextReads.get(owner)??1)-1;if(remaining)this.activeContextReads.set(owner,remaining);else this.activeContextReads.delete(owner);}
  }
  async readQuote(context:TradeContext,signal?:AbortSignal):Promise<QuoteEnvelope>{return this.contextRead(context,()=>this.readQuoteActual(context,signal));}
  private async readQuoteActual(context:TradeContext,signal?:AbortSignal):Promise<QuoteEnvelope> {
    this.route(context); if(raw(context.inputRaw)===0n)throw new Error('zero_quote_input'); const info=await this.inspectPool(getAddress(context.pool),signal);
    if(!same(context.token,context.isBuy?this.deployment.usdl:info.token)||context.opticalRevision!==info.opticalRevision) throw new Error('quote_token_or_optical_mismatch');
    const result=await this.publicClient.readContract({address:this.deployment.quoter,abi:quoterAbi,functionName:'quoteExactInput',args:[getAddress(context.pool),context.inputRaw,context.isBuy],account:getAddress(context.sender),blockNumber:info.blockNumber}).catch(error=>{throw opticalRejection(error);});
    alive(signal); raw(result.amountOut); raw(result.feeAmount); raw(result.priceImpactBps); const now=Date.now(); const impact=Number(result.priceImpactBps); if(!Number.isSafeInteger(impact)) throw new Error('impact_overflow');
    return {quoteId:keccak256(stringToHex(`${contextKey(context)}:${info.blockNumber}:${result.amountOut}:${now}`)),context:{...context},blockHeight:info.blockNumber,quotedAt:now,expiresAt:now+30000,outputRaw:result.amountOut,feeComponents:[result.feeAmount],impactBps:impact};
  }
  swapData(context:TradeContext,minimum:bigint,deadline:bigint):Hex { this.route(context); return encodeReviewedRouterCalldata(context,{chainId:125,manifestRevision:this.deployment.revision,router:this.deployment.router,pool:context.pool,inputAsset:context.token,recipientMode:'same_sender'},minimum,deadline); }
  async simulate(context:TradeContext,quote:QuoteEnvelope,minimum:bigint,deadline:bigint,signal?:AbortSignal):Promise<OpticalSimulation>{return this.contextRead(context,()=>this.simulateActual(context,quote,minimum,deadline,signal));}
  private async simulateActual(context:TradeContext,quote:QuoteEnvelope,minimum:bigint,deadline:bigint,signal?:AbortSignal):Promise<OpticalSimulation> {
    const reviewKey=contextKey(context);this.simulations.delete(reviewKey);
    if(!executableQuote(quote,context,Date.now()))throw new Error('stale_or_mismatched_quote');
    const data=this.swapData(context,minimum,deadline); const blockHeight=await this.publicClient.getBlockNumber(); await this.verify(blockHeight,signal);
    const info=await this.inspectPool(getAddress(context.pool),signal); if(info.opticalRevision!==context.opticalRevision||!same(context.token,context.isBuy?this.deployment.usdl:info.token)) throw new Error('optical_or_asset_revision_changed');
    await this.publicClient.call({account:getAddress(context.sender),to:this.deployment.router,data,blockNumber:blockHeight}).catch(error=>{throw opticalRejection(error);}); alive(signal);
    const now=Date.now();
    if(this.simulations.size>=2)this.simulations.delete(this.simulations.keys().next().value!);
    this.simulations.set(reviewKey,{quote:Object.freeze({...quote,context:Object.freeze({...quote.context}),feeComponents:Object.freeze([...quote.feeComponents])}),minimum,deadline,expiresAt:Math.min(now+10000,quote.expiresAt)});
    return {contextKey:contextKey(context),quoteFingerprint:quoteFingerprint(quote),blockHeight,simulatedAt:now,expiresAt:Math.min(now+10000,quote.expiresAt),allowed:true,revision:context.opticalRevision,restrictions:info.restrictions};
  }
  async readAllowance(context:TradeContext,blockNumber?:bigint) {
    this.route(context);const key=JSON.stringify([125,this.deployment.revision,context.sender.toLowerCase(),context.token.toLowerCase(),context.router.toLowerCase(),blockNumber?.toString()??'latest']);
    const pending=this.allowanceReads.get(key);if(pending)return pending;
    if(this.allowanceReads.size>=200)throw new Error('allowance_read_capacity_exceeded');
    const read=this.publicClient.readContract({address:getAddress(context.token),abi:erc20Abi,functionName:'allowance',args:[getAddress(context.sender),this.deployment.router],blockNumber}).then(raw);
    this.allowanceReads.set(key,read);
    try{return await read;}finally{if(this.allowanceReads.get(key)===read)this.allowanceReads.delete(key);}
  }
  async readBalance(token:Address,account:Address,blockNumber?:bigint) { return raw(await this.publicClient.readContract({address:token,abi:erc20Abi,functionName:'balanceOf',args:[account],blockNumber})); }
  private async authorize(scope:AccountScope,context:TradeContext) {
    this.session?.assertCurrent(scope,true); alive(scope.signal); if(!this.walletClient||scope.chainId!==125||!same(scope.account,context.sender)) throw new Error('wallet_scope_mismatch');
    if(await this.walletClient.getChainId()!==125||!(await this.walletClient.getAddresses()).some(a=>same(a,scope.account))) throw new Error('wallet_changed');
    this.session?.assertCurrent(scope,true);
  }
  async requiresZeroReset(context:TradeContext,target:bigint) {
    const allowance=await this.readAllowance(context); if(allowance===0n) return false;
    try { await this.publicClient.simulateContract({address:getAddress(context.token),abi:erc20Abi,functionName:'approve',args:[this.deployment.router,target],account:getAddress(context.sender)}); return false; }
    catch { await this.publicClient.simulateContract({address:getAddress(context.token),abi:erc20Abi,functionName:'approve',args:[this.deployment.router,0n],account:getAddress(context.sender)}); return true; }
  }
  async submitApproval(context:TradeContext,amount:bigint,scope:AccountScope) {
    this.route(context); raw(amount); await this.verify(await this.publicClient.getBlockNumber(),scope.signal);
    const simulation=await this.publicClient.simulateContract({address:getAddress(context.token),abi:erc20Abi,functionName:'approve',args:[this.deployment.router,amount],account:getAddress(context.sender)});
    await this.authorize(scope,context); return this.walletClient!.writeContract({...simulation.request,chain:this.walletClient!.chain,account:scope.account});
  }
  async submitSwap(context:TradeContext,minimum:bigint,deadline:bigint,scope:AccountScope) {
    const reviewKey=contextKey(context),review=this.simulations.get(reviewKey);
    if(!review||review.minimum!==minimum||review.deadline!==deadline||Date.now()>=review.expiresAt||!executableQuote(review.quote,context,Date.now()))throw new Error('exact_context_simulation_required');
    const data=this.swapData(context,minimum,deadline);
    const now=BigInt(Math.floor(Date.now()/1000));if(deadline<=now||deadline>now+600n)throw new Error('expired_execution_deadline');
    const info=await this.inspectPool(getAddress(context.pool),scope.signal);
    if(info.opticalRevision!==context.opticalRevision||!same(context.token,context.isBuy?this.deployment.usdl:info.token))throw new Error('execution_context_changed');
    if(await this.readAllowance(context,info.blockNumber)<context.inputRaw)throw new Error('verified_allowance_required');
    await this.publicClient.call({account:getAddress(context.sender),to:this.deployment.router,data,blockNumber:info.blockNumber}).catch(error=>{throw opticalRejection(error);});
    await this.authorize(scope,context);
    if(this.simulations.get(reviewKey)!==review||Date.now()>=review.expiresAt||!executableQuote(review.quote,context,Date.now()))throw new Error('simulation_or_quote_expired');
    this.simulations.delete(reviewKey);
    return this.walletClient!.sendTransaction({account:scope.account,chain:this.walletClient!.chain,to:this.deployment.router,data,value:0n});
  }
  async readReceipt(attempt:TxAttempt,signal?:AbortSignal):Promise<ReceiptEvidence|undefined> {
    alive(signal);if(attempt.chainId!==125||await this.publicClient.getChainId()!==125)throw new EvidenceIntegrityError('receipt_chain_mismatch');
    let receipt; try {receipt=await this.publicClient.getTransactionReceipt({hash:attempt.txHash as Hash});} catch(error) {if(error instanceof Error&&/not found|could not be found/i.test(error.message)) return undefined; throw error;}
    alive(signal);if(!receipt)return undefined; const canonical=await this.publicClient.getBlock({blockNumber:receipt.blockNumber});
    if(canonical.hash!==receipt.blockHash||!same(receipt.transactionHash,attempt.txHash)) throw new EvidenceIntegrityError('receipt_canonical_conflict');
    const evidence:ReceiptEvidence={transactionHash:receipt.transactionHash,status:receipt.status,blockNumber:receipt.blockNumber};
    const tx=await this.publicClient.getTransaction({hash:attempt.txHash as Hash});
    if(!tx.to||!same(tx.hash,attempt.txHash)||!same(tx.from,attempt.from)||(attempt.nonce!==undefined&&tx.nonce!==attempt.nonce)||transactionFingerprint(tx.to,tx.input,tx.value)!==attempt.actionFingerprint) throw new EvidenceIntegrityError('receipt_action_mismatch');
    alive(signal);if(receipt.status==='reverted')return evidence;
    await this.verify(receipt.blockNumber,signal);
    if(attempt.action==='approve') {
      let decoded;try{decoded=decodeFunctionData({abi:erc20Abi,data:tx.input});}catch{throw new EvidenceIntegrityError('malformed_approval_calldata');}
      if(decoded.functionName!=='approve'||!same(decoded.args[0],this.deployment.router)||tx.value!==0n)throw new EvidenceIntegrityError('approval_action_mismatch');
      const code=await this.publicClient.getCode({address:tx.to,blockNumber:receipt.blockNumber});if(!code||code==='0x')throw new EvidenceIntegrityError('approval_target_has_no_code');
      alive(signal);return evidence;
    }
    if(!same(tx.to,this.deployment.router)) throw new EvidenceIntegrityError('swap_target_mismatch');
    let decoded;try{decoded=decodeFunctionData({abi:tradeRouterAbi,data:tx.input});}catch{throw new EvidenceIntegrityError('malformed_swap_calldata');}
    const [pool,input,minimum]=decoded.args;
    if((attempt.action==='buy'&&decoded.functionName!=='buy')||(attempt.action==='sell'&&decoded.functionName!=='sell')||!['buy','sell'].includes(attempt.action)||tx.value!==0n)throw new EvidenceIntegrityError('swap_expected_action_mismatch');
    const registered=await this.publicClient.readContract({address:this.deployment.registry,abi:registryAbi,functionName:'isRegisteredPool',args:[pool],blockNumber:receipt.blockNumber});if(!registered)throw new EvidenceIntegrityError('receipt_pool_unregistered');
    const poolId=`0x${pool.slice(2).toLowerCase().padStart(64,'0')}`;
    const token=await this.publicClient.readContract({address:pool,abi:poolAbi,functionName:'tokenAddress',blockNumber:receipt.blockNumber});
    const swapTopic=keccak256(stringToHex('Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)'));
    const swapLogs=receipt.logs.filter(log=>same(log.address,this.deployment.emitter)&&log.topics[0]?.toLowerCase()===swapTopic);
    if(swapLogs.length!==1)throw new EvidenceIntegrityError('swap_evidence_missing_or_duplicate');
    if(swapLogs.some(log=>log.removed||!log.transactionHash||!same(log.transactionHash,attempt.txHash)||log.blockHash!==receipt.blockHash||log.blockNumber!==receipt.blockNumber))throw new EvidenceIntegrityError('swap_log_provenance_mismatch');
    let fill;
    try{fill=decodeEventLog({abi:swapAbi,data:swapLogs[0].data,topics:swapLogs[0].topics}).args;}
    catch{throw new EvidenceIntegrityError('malformed_canonical_swap_evidence');}
    if(fill.poolId!==poolId||!same(fill.sender,tx.from))throw new EvidenceIntegrityError('swap_pool_or_sender_conflict');
    const tokenDecimals=await this.publicClient.readContract({address:token,abi:erc20Abi,functionName:'decimals',blockNumber:receipt.blockNumber});
    if(!Number.isInteger(tokenDecimals)||tokenDecimals<0||tokenDecimals>77)throw new EvidenceIntegrityError('receipt_asset_decimals_mismatch');
    const isBuy=decoded.functionName==='buy';
    if(fill.isBuy!==isBuy||fill.amountIn!==input||fill.amountOut<minimum||fill.blockNumber!==receipt.blockNumber) throw new EvidenceIntegrityError('swap_evidence_conflict');
    alive(signal);if(this.fills.size>=200&&!this.fills.has(attempt.txHash.toLowerCase()))this.fills.delete(this.fills.keys().next().value!);this.fills.set(attempt.txHash.toLowerCase(),{inputRaw:fill.amountIn,outputRaw:fill.amountOut,feeRaw:fill.fee,blockHeight:receipt.blockNumber,isBuy,pool,token,tokenDecimals}); return evidence;
  }
  async replacement(original:TxAttempt,hash:Hash) {
    if(original.chainId!==125||await this.publicClient.getChainId()!==125)throw new EvidenceIntegrityError('replacement_chain_mismatch');
    const tx=await this.publicClient.getTransaction({hash}); const receipt=await this.publicClient.getTransactionReceipt({hash});
    const block=await this.publicClient.getBlock({blockNumber:receipt.blockNumber});
    if(receipt.status!=='success'||!same(tx.hash,hash)||!same(receipt.transactionHash,hash)||same(hash,original.txHash)||block.hash!==receipt.blockHash||original.nonce===undefined||tx.nonce!==original.nonce||!same(tx.from,original.from)||!tx.to) throw new EvidenceIntegrityError('unverified_replacement');
    return {hash,receipt,changed:transactionFingerprint(tx.to,tx.input,tx.value)!==original.actionFingerprint,cancelled:same(tx.to,tx.from)&&tx.input==='0x'&&tx.value===0n};
  }
}
