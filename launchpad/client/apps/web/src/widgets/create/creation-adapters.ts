import { decodeEventLog, decodeFunctionData, encodeFunctionData, parseAbi, type Address, type Hex, type PublicClient, type WalletClient } from 'viem';
import { address, hash, raw, resolveCreation, verifyCost, type Deployment, type Receipt } from './creation-domain';
import { validateCreationDraft, type CreationDraftV2 } from './creation-storage';
import { KnownCapabilityAdmissionError, type CreationCapabilityAdmission } from './cap-admission';
import { ChainTradeProvider, productionTradeDeployment, transactionFingerprint, type TradeDeployment } from '../trading/transaction-controls/providers';
import { minimumOutput, type TradeContext } from '../trading/transaction-controls/domain';

const erc20 = parseAbi(['function decimals() view returns (uint8)','function balanceOf(address) view returns (uint256)','function allowance(address,address) view returns (uint256)','function approve(address,uint256) returns (bool)']);
const routerAbi = parseAbi(['function createMarket(string name,string symbol,uint8 feeStrategy,address optical) returns (address tokenAddr,address poolAddr,uint256 nftId)','function buy(address pool,uint256 usdlAmountIn,uint256 minTokensOut,uint256 deadline) returns (uint256)','function factory() view returns (address)','function poolRegistry() view returns (address)','function protocolConfig() view returns (address)','function usdlAddress() view returns (address)']);
const configAbi = parseAbi(['function creationFee() view returns (uint256)','function usdlAddress() view returns (address)']);
const factoryAbi = parseAbi(['function treasury() view returns (address)','function usdlAddress() view returns (address)','function protocolConfig() view returns (address)','function poolRegistry() view returns (address)']);
const registryAbi = parseAbi(['function getPoolMetadata(address pool) view returns ((address creator,address token,address optical,uint256 nftId,uint256 createdAt,uint256 createdBlock))','function getPoolsByCreatorPage(address creator,uint256 offset,uint256 limit) view returns (address[])']);
const factoryEvent = parseAbi(['event MarketCreated(address indexed token,address indexed pool,address indexed creator,uint256 nftId,address optical)']);
export interface LaunchDeployment extends Deployment { protocolConfig: string; poolRegistry: string; quoter: string; opticalRegistry: string; tradeDeployment?: TradeDeployment }
export interface AccountScope { creator: string; chainId: number; epoch: number }
export interface FundingRead { cost: ReturnType<typeof verifyCost>; balances: { usdl: string; pax: string; allowance: string }; blockHeight: string; blockHash: string; gasStage: 'approval' | 'creation'; observedAt: number }
export interface FirstPurchaseQuote { pool: string; creator: string; amountInRaw: string; amountOutRaw: string; expiresAt: number; chainId: 125; priceImpactBps: number; blockHeight: string; blockHash: string; deploymentRevision: string; opticalRevision: string; slippageBps: number; minOutRaw: string; deadline: number; quoteId: string; accountEpoch: number }
export function creationFunding(read:FundingRead) {
 if(!read||!read.balances)return {state:'balance_loading',approvalTargetRaw:undefined};
 if(read.cost.protocolFeeRaw!=='1000000'||!Number.isSafeInteger(read.observedAt)||read.observedAt>Date.now()||Date.now()-read.observedAt>30000)return {state:'funding_unavailable',approvalTargetRaw:undefined};
 const needed=raw(read.cost.protocolFeeRaw)+raw(read.cost.optionalInitialBuyRaw);
 const state=raw(read.balances.usdl)<needed?'insufficient_usdl':raw(read.balances.pax)<raw(read.cost.gasEstimatePaxRaw)?'insufficient_gas':raw(read.balances.allowance)!==raw(read.cost.protocolFeeRaw)?'approval_required':'signable';
 return {state,approvalTargetRaw:read.cost.protocolFeeRaw};
}
export class ChainCreationAdapter {
 readonly trade: ChainTradeProvider;
 constructor(readonly client: PublicClient, private wallet: WalletClient, readonly deployment: LaunchDeployment, private currentScope: () => AccountScope, private admission?:CreationCapabilityAdmission) {
  this.deployment=Object.freeze({...deployment});
  const authority=deployment.tradeDeployment??productionTradeDeployment;
  const tradeDeployment=Object.freeze({...authority,pins:Object.freeze(authority.pins.map(pin=>Object.freeze({...pin})))});
  if(tradeDeployment.revision!==deployment.revision||address(tradeDeployment.router)!==address(deployment.router)||address(tradeDeployment.usdl)!==address(deployment.usdl)||address(tradeDeployment.registry)!==address(deployment.poolRegistry))throw new Error('Creation and trade deployment identities differ.');
  this.trade=new ChainTradeProvider(client,wallet,tradeDeployment);
 }
 scope(expected?:AccountScope) {
  const value=this.currentScope();
  if(value.chainId!==125 || address(value.creator)!==address(this.wallet.account?.address??value.creator) || (expected&&(value.epoch!==expected.epoch||address(value.creator)!==address(expected.creator)||value.chainId!==expected.chainId)))throw new Error('Wallet changed. Review this launch again.');
  return {...value,creator:address(value.creator)};
 }
 async assertWallet(expected:AccountScope) {
  const accounts=await this.wallet.getAddresses();if(await this.wallet.getChainId()!==125||!accounts.some(value=>address(value)===address(expected.creator)))throw new Error('Wallet account or chain changed. Review again.');this.scope(expected);
 }
 async assertChain(signal?:AbortSignal) {
  signal?.throwIfAborted(); await this.trade.verify(await this.client.getBlockNumber(),signal); if(await this.client.getChainId()!==125 || await this.wallet.getChainId()!==125)throw new Error('Switch to chain 125.');
  const d=this.deployment;
  for(const value of [d.router,d.factory,d.usdl,d.protocolConfig,d.poolRegistry,d.quoter,d.opticalRegistry])address(value);
  const block=await this.client.getBlock({blockTag:'latest'});if(block.number===null||!block.hash)throw new Error('Deployment block is unavailable.');
  const at={blockNumber:block.number};
  const [factory,registry,config,usdl,factoryUsdl,factoryConfig,factoryRegistry,treasury,configUsdl]=await Promise.all([
   this.client.readContract({address:d.router as Address,abi:routerAbi,functionName:'factory',...at}),
   this.client.readContract({address:d.router as Address,abi:routerAbi,functionName:'poolRegistry',...at}),
   this.client.readContract({address:d.router as Address,abi:routerAbi,functionName:'protocolConfig',...at}),
   this.client.readContract({address:d.router as Address,abi:routerAbi,functionName:'usdlAddress',...at}),
   this.client.readContract({address:d.factory as Address,abi:factoryAbi,functionName:'usdlAddress',...at}),
   this.client.readContract({address:d.factory as Address,abi:factoryAbi,functionName:'protocolConfig',...at}),
   this.client.readContract({address:d.factory as Address,abi:factoryAbi,functionName:'poolRegistry',...at}),
   this.client.readContract({address:d.factory as Address,abi:factoryAbi,functionName:'treasury',...at}),
   this.client.readContract({address:d.protocolConfig as Address,abi:configAbi,functionName:'usdlAddress',...at})]);
  if(address(factory)!==address(d.factory)||address(registry)!==address(d.poolRegistry)||address(config)!==address(d.protocolConfig)||address(usdl)!==address(d.usdl)||address(factoryUsdl)!==address(d.usdl)||address(configUsdl)!==address(d.usdl)||address(factoryConfig)!==address(d.protocolConfig)||address(factoryRegistry)!==address(d.poolRegistry))throw new Error('Verified deployment wiring changed. Creation is blocked.');
  address(treasury);
  if((await this.client.getBlock({blockNumber:block.number})).hash!==block.hash)throw new Error('Deployment block was reorganized.');
  signal?.throwIfAborted();
 }
 calldata(draft:CreationDraftV2) {
  const errors=validateCreationDraft(draft);if(Object.keys(errors).length)throw new Error('Invalid creation fields: '+Object.keys(errors).join(', '));
  if(draft.opticalSelection!=='standard')throw new Error('This optical preset requires an admitted parameter schema before creation.');
  return {address:this.deployment.router as Address,abi:routerAbi,functionName:'createMarket' as const,args:[draft.name,draft.symbol,draft.feeStrategy,'0x0000000000000000000000000000000000000000'] as readonly [string,string,number,Address],account:address(draft.creator) as Address};
 }
 async readFunding(draft:CreationDraftV2, expected:AccountScope, signal?:AbortSignal):Promise<FundingRead> {
  this.calldata(draft);if(address(draft.creator)!==address(expected.creator))throw new Error('Draft creator changed.');
  await this.assertChain(signal);this.scope(expected); const d=this.deployment;const block=await this.client.getBlock({blockTag:'latest'});if(block.number===null||!block.hash)throw new Error('Latest canonical block is unavailable.');
  const [decimals,fee,usdl,pax,allowance,gasPrice]=await Promise.all([
   this.client.readContract({address:d.usdl as Address,abi:erc20,functionName:'decimals',blockNumber:block.number}),
   this.client.readContract({address:d.protocolConfig as Address,abi:configAbi,functionName:'creationFee',blockNumber:block.number}),
   this.client.readContract({address:d.usdl as Address,abi:erc20,functionName:'balanceOf',args:[expected.creator as Address],blockNumber:block.number}),
   this.client.getBalance({address:expected.creator as Address,blockNumber:block.number}),
   this.client.readContract({address:d.usdl as Address,abi:erc20,functionName:'allowance',args:[expected.creator as Address,d.router as Address],blockNumber:block.number}),this.client.getGasPrice()]);
  verifyCost(d,{revision:d.revision,decimals:Number(decimals),feeRaw:String(fee),gasRaw:'0'},draft.firstBuyRaw);
  if(usdl<fee+raw(draft.firstBuyRaw))throw new Error('insufficient_usdl: USDL must cover the one USDL fee plus the selected first purchase.');
  if(pax===0n)throw new Error('insufficient_gas: Your USDL balance is sufficient, but PAX is required separately for network gas.');
  const target=fee,gasStage=allowance!==target?'approval':'creation';
  const gas=gasStage==='approval'?await this.client.estimateContractGas({address:d.usdl as Address,abi:erc20,functionName:'approve',args:[d.router as Address,target],account:expected.creator as Address}):await this.client.estimateContractGas(this.calldata(draft));
  const cost=verifyCost(d,{revision:d.revision,decimals:Number(decimals),feeRaw:String(fee),gasRaw:String(gas*gasPrice)},draft.firstBuyRaw);
  if((await this.client.getBlock({blockNumber:block.number})).hash!==block.hash)throw new Error('Funding block was reorganized.');
  signal?.throwIfAborted();this.scope(expected);
  return {cost,balances:{usdl:String(usdl),pax:String(pax),allowance:String(allowance)},blockHeight:String(block.number),blockHash:block.hash,gasStage,observedAt:Date.now()};
 }
 async approve(targetRaw:string,expected:AccountScope):Promise<string> {
  this.scope(expected);await this.assertChain();const d=this.deployment;
  const request=await this.client.simulateContract({address:d.usdl as Address,abi:erc20,functionName:'approve',args:[d.router as Address,raw(targetRaw)],account:expected.creator as Address});
  await this.assertWallet(expected);return this.wallet.writeContract({...request.request,chain:this.wallet.chain,account:expected.creator as Address});
 }
 async approvalProof(txHash:string,targetRaw:string,expected:AccountScope) {
  const receipt=await this.client.waitForTransactionReceipt({hash:hash(txHash) as Hex,timeout:30000});this.scope(expected);
  if(receipt.status!=='success')throw new Error('Approval reverted. Creation has not been submitted.');
  const block=await this.client.getBlock({blockNumber:receipt.blockNumber});if(block.hash!==receipt.blockHash)throw new Error('Approval is no longer canonical.');
  const tx=await this.client.getTransaction({hash:receipt.transactionHash});
  if(address(tx.from)!==address(expected.creator)||!tx.to||address(tx.to)!==address(this.deployment.usdl)||tx.input!==encodeFunctionData({abi:erc20,functionName:'approve',args:[this.deployment.router as Address,raw(targetRaw)]}))throw new Error('Approval transaction does not match the reviewed amount and spender.');
  const allowance=await this.client.readContract({address:this.deployment.usdl as Address,abi:erc20,functionName:'allowance',args:[expected.creator as Address,this.deployment.router as Address],blockNumber:receipt.blockNumber});
  if(receipt.transactionHash.toLowerCase()!==hash(txHash))throw new Error('Approval hash changed.');
  if(allowance!==raw(targetRaw))throw new Error('Mined approval allowance is insufficient.');
  return {receipt,allowanceRaw:String(allowance)};
 }
 async admitCreation(draft:CreationDraftV2,expected:AccountScope) {
  try{this.scope(expected);if(!this.admission)throw new Error('Runtime creation authority is unavailable. No wallet request was sent.');await this.admission(draft,expected);this.scope(expected);}
  catch(error){if(error instanceof KnownCapabilityAdmissionError)throw error;throw new KnownCapabilityAdmissionError(error instanceof Error?error.message:'Creation admission failed before wallet dispatch.',error);}
 }
 async sendCreation(draft:CreationDraftV2,expected:AccountScope,reviewExpiresAt?:number):Promise<string> {
  const request=await (async()=>{
   try{
    this.scope(expected);await this.assertChain();
    const fresh=await this.readFunding(draft,expected);if(creationFunding(fresh).state!=='signable')throw new Error('Creation funding changed. Review again.');
    const simulation=await this.client.simulateContract(this.calldata(draft));await this.assertWallet(expected);
    await this.admitCreation(draft,expected);
    if(reviewExpiresAt!==undefined&&Date.now()>=reviewExpiresAt)throw new Error('Creation review expired before wallet dispatch.');
    return {...simulation.request,chain:this.wallet.chain,account:expected.creator as Address};
   }catch(error){if(error instanceof KnownCapabilityAdmissionError)throw error;throw new KnownCapabilityAdmissionError(error instanceof Error?error.message:'Creation was blocked before wallet dispatch.',error);}
  })();
  try{return await this.wallet.writeContract(request)}catch(error){let cause:unknown=error;for(let depth=0;depth<8&&cause&&typeof cause==='object';depth++){const known=cause as {code?:unknown;name?:unknown;cause?:unknown};if(known.code===4001||known.name==='UserRejectedRequestError')throw new KnownCapabilityAdmissionError('Wallet rejected creation. No transaction was submitted.',error);cause=known.cause;}throw error;}
 }
 async resolve(txHash:string,creator:string,signal?:AbortSignal,expectedDraft?:CreationDraftV2) {
  await this.assertChain(signal);const tx=hash(txHash) as Hex;
  const receipt=await this.client.getTransactionReceipt({hash:tx});
  const canonical=await this.client.getBlock({blockNumber:receipt.blockNumber});
  if(canonical.hash!==receipt.blockHash)throw new Error('Creation receipt is not canonical.');
  const transaction=await this.client.getTransaction({hash:tx});
  if(!transaction.to||address(transaction.to)!==address(this.deployment.router)||address(transaction.from)!==address(creator))throw new Error('Creation transaction scope does not match.');
  if(transaction.chainId!==125||receipt.transactionHash.toLowerCase()!==tx)throw new Error('Creation transaction chain or hash changed.');
  const input=decodeFunctionData({abi:routerAbi,data:transaction.input});
  if(input.functionName!=='createMarket')throw new Error('Receipt is not a createMarket transaction.');
  if(input.args[3]!=='0x0000000000000000000000000000000000000000'||Number(input.args[2])>3)throw new Error('Creation optical or fee strategy has not been admitted.');
  if(expectedDraft&&transaction.input!==encodeFunctionData(this.calldata(expectedDraft)))throw new Error('Creation receipt does not match the saved reviewed draft.');
  const events:Receipt['events']=[];
  for(const log of receipt.logs) {
   if(address(log.address)!==address(this.deployment.factory))continue;
   let decoded;try{decoded=decodeEventLog({abi:factoryEvent,data:log.data,topics:log.topics,strict:true})}catch{continue}
   if(decoded.eventName==='MarketCreated')events.push({transactionHash:log.transactionHash,emitter:log.address,creator:decoded.args.creator,pool:decoded.args.pool,token:decoded.args.token,logIndex:log.logIndex,blockHeight:String(log.blockNumber),blockHash:log.blockHash});
  }
  if(events.length===0&&receipt.status==='success') {
   const fallback=await this.client.getLogs({address:this.deployment.factory as Address,fromBlock:receipt.blockNumber,toBlock:receipt.blockNumber});
   for(const log of fallback) {
    if(!log.transactionHash||!log.blockHash||log.blockNumber===null||log.logIndex===null||log.transactionHash.toLowerCase()!==tx||log.blockHash!==receipt.blockHash)continue;
    let decoded;try{decoded=decodeEventLog({abi:factoryEvent,data:log.data,topics:log.topics,strict:true})}catch{continue}
    if(decoded.eventName==='MarketCreated'&&address(decoded.args.creator)===address(creator))events.push({transactionHash:log.transactionHash,emitter:log.address,creator:decoded.args.creator,pool:decoded.args.pool,token:decoded.args.token,logIndex:log.logIndex,blockHeight:String(log.blockNumber),blockHash:log.blockHash});
   }
  }
  const data:Receipt={chainId:125,transactionHash:receipt.transactionHash,status:receipt.status,events,blockHeight:String(receipt.blockNumber),blockHash:receipt.blockHash};
  return resolveCreation(data,{hash:tx,creator,factory:this.deployment.factory},async(pool,block)=>{
   const before=await this.client.getBlock({blockNumber:raw(block.height)});if(before.hash!==block.hash)throw new Error('Registry block was reorganized.');
   const entry=await this.client.readContract({address:this.deployment.poolRegistry as Address,abi:registryAbi,functionName:'getPoolMetadata',args:[pool as Address],blockNumber:raw(block.height)});
   const after=await this.client.getBlock({blockNumber:raw(block.height)});if(after.hash!==block.hash)throw new Error('Registry block was reorganized.');
   return {token:entry.token,creator:entry.creator,blockHeight:block.height,blockHash:block.hash};
  },signal);
 }
 async verifyPurchasePool(pool:string,creator:string,signal?:AbortSignal) {
  await this.assertChain(signal);const block=await this.client.getBlock();if(block.number===null||!block.hash)throw new Error('Purchase registry block unavailable.');
  const entry=await this.client.readContract({address:this.deployment.poolRegistry as Address,abi:registryAbi,functionName:'getPoolMetadata',args:[address(pool) as Address],blockNumber:block.number});
  if(address(entry.creator)!==address(creator))throw new Error('Purchase pool does not belong to the reviewed creator.');address(entry.token);
  if((await this.client.getBlock({blockNumber:block.number})).hash!==block.hash)throw new Error('Purchase registry block was reorganized.');signal?.throwIfAborted();return entry;
 }
 async purchaseAllowance(creator:string) {return String(await this.client.readContract({address:this.deployment.usdl as Address,abi:erc20,functionName:'allowance',args:[address(creator) as Address,this.deployment.router as Address]}));}
 async quotePurchase(pool:string,amountInRaw:string,slippageBps:number,expected:AccountScope,signal?:AbortSignal):Promise<FirstPurchaseQuote> {
  this.scope(expected);await this.verifyPurchasePool(pool,expected.creator,signal);
  if(!Number.isInteger(slippageBps)||slippageBps<1||slippageBps>5000||raw(amountInRaw)===0n)throw new Error('Invalid purchase amount or slippage.');
  const block=await this.client.getBlock();if(!block.hash||block.number===null)throw new Error('Quote block is unavailable.');
  const info=await this.trade.inspectPool(pool as Address,signal);
  const context:TradeContext={chainId:125,manifestRevision:this.deployment.revision,pool:address(pool),token:address(this.deployment.usdl),router:address(this.deployment.router),sender:address(expected.creator),recipient:address(expected.creator),inputRaw:raw(amountInRaw),isBuy:true,opticalRevision:info.opticalRevision,opticalData:'0x'};
  const quoted=await this.trade.readQuote(context,signal),minOut=minimumOutput(quoted.outputRaw,slippageBps);if(minOut<=0n)throw new Error('Quote has no safe minimum output.');
  const deadline=Date.now()+120000;
  const simulation=await this.trade.simulate(context,quoted,minOut,BigInt(Math.floor(deadline/1000)),signal);
  signal?.throwIfAborted();this.scope(expected);
  if(!simulation.allowed||simulation.expiresAt<=Date.now())throw new Error('Purchase simulation expired.');
  const quoteBlock=await this.client.getBlock({blockNumber:quoted.blockHeight});if(!quoteBlock.hash)throw new Error('Quote block is unavailable.');
  return {pool:address(pool),creator:address(expected.creator),amountInRaw,amountOutRaw:String(quoted.outputRaw),expiresAt:Math.min(quoted.expiresAt,simulation.expiresAt),chainId:125,priceImpactBps:quoted.impactBps,blockHeight:String(quoted.blockHeight),blockHash:quoteBlock.hash,deploymentRevision:this.deployment.revision,opticalRevision:info.opticalRevision,slippageBps,minOutRaw:String(minOut),deadline,quoteId:quoted.quoteId,accountEpoch:expected.epoch};
 }
 async sendPurchase(quote:FirstPurchaseQuote,expected:AccountScope) {
  this.scope(expected);
  if(quote.chainId!==125||!Number.isSafeInteger(quote.expiresAt)||!Number.isSafeInteger(quote.deadline)||quote.deadline<Date.now()+30000||quote.deadline>Date.now()+600000||raw(quote.minOutRaw)!==minimumOutput(raw(quote.amountOutRaw),quote.slippageBps)||raw(quote.minOutRaw)===0n)throw new Error('Purchase quote protections changed.');
  if(quote.expiresAt<=Date.now()||quote.accountEpoch!==expected.epoch||quote.deploymentRevision!==this.deployment.revision||address(quote.creator)!==address(expected.creator))throw new Error('Purchase quote expired. Request a new quote.');
  const block=await this.client.getBlock({blockNumber:raw(quote.blockHeight)});if(block.hash!==quote.blockHash)throw new Error('Quote block was reorganized.');
  const fresh=await this.quotePurchase(quote.pool,quote.amountInRaw,quote.slippageBps,expected);
  if(fresh.opticalRevision!==quote.opticalRevision||fresh.pool!==quote.pool||fresh.creator!==quote.creator)throw new Error('Purchase pool context changed. Review a fresh quote.');
  if(raw(fresh.amountOutRaw)<raw(quote.minOutRaw))throw new Error('Pool reserves changed beyond your minimum. Review a new quote.');
  const simulation=await this.client.simulateContract({address:this.deployment.router as Address,abi:routerAbi,functionName:'buy',args:[quote.pool as Address,raw(quote.amountInRaw),raw(quote.minOutRaw),BigInt(Math.floor(quote.deadline/1000))],account:expected.creator as Address});
  await this.verifyPurchasePool(quote.pool,expected.creator);await this.assertWallet(expected);if(quote.expiresAt<=Date.now())throw new Error('Purchase review expired. Request a new quote.');return this.wallet.writeContract({...simulation.request,chain:this.wallet.chain,account:expected.creator as Address});
 }
 async purchaseReceipt(txHash:string,quote:FirstPurchaseQuote) {
  const receipt=await this.client.waitForTransactionReceipt({hash:hash(txHash) as Hex,timeout:30000});
  const data=encodeFunctionData({abi:routerAbi,functionName:'buy',args:[quote.pool as Address,raw(quote.amountInRaw),raw(quote.minOutRaw),BigInt(Math.floor(quote.deadline/1000))]});
  const proof=await this.trade.readReceipt({intentId:quote.quoteId,chainId:125,from:quote.creator,txHash,submittedAt:0,action:'buy',actionFingerprint:transactionFingerprint(this.deployment.router,data)});
  if(!proof)throw new Error('Purchase receipt is unavailable.');return receipt;
 }
 async createdPools(creator:string,offset=0n) {
  await this.assertChain();const pools=await this.client.readContract({address:this.deployment.poolRegistry as Address,abi:registryAbi,functionName:'getPoolsByCreatorPage',args:[address(creator) as Address,offset,25n]});
  if(offset<0n)throw new Error('Invalid history cursor.');
  if(pools.length>25||new Set(pools.map(pool=>address(pool))).size!==pools.length)throw new Error('Invalid registry history page.');
  await Promise.all(pools.map(pool=>this.verifyPurchasePool(pool,creator)));return pools.map(pool=>address(pool));
 }
}
