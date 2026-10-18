import { encodeFunctionData, erc20Abi, type Hash } from 'viem';
import type { AccountScope } from '@/core/wallet-sdk/session';
import { approvalMayContinue, approvalPlan, approvalStepFingerprint, contextKey, executableQuote, minimumOutput, QuoteRequest, simulationAllowed, tradeLimits, type PendingAttemptRecord, type QuoteEnvelope, type RiskConsent, type TradeContext, type ReceiptEvidence } from './domain';
import { PendingAttemptStore, ReceiptRecoveryScheduler, type PendingStorage, type RecoveryUpdate } from './recovery';
import { ChainTradeProvider, EvidenceIntegrityError, transactionFingerprint, type ActualFill } from './providers';

const signingAccounts=new Set<string>();
const unresolvedAccounts=new Map<string,Set<string>>();

export type JourneyObservation = { kind: 'quote_start' | 'quote_end' | 'completion' | 'duplicate_completion' | 'approval_submitted' | 'allowance_verified' | 'swap_submitted'; at: number; monotonicMs: number; contextKey?: string; txHash?: string; action?: string; quoteId?: string; blockHeight?: string };

export interface JourneyState { quote?:QuoteEnvelope; staleQuote?:QuoteEnvelope; quoteStatus?:'idle'|'requesting'|'executable'|'stale'|'unavailable'; quoteError?:string; signing:boolean; attempts:readonly RecoveryUpdate[]; integrityError?:string; storageError?:string; completion?:{attempt:PendingAttemptRecord;receipt:ReceiptEvidence;fill:ActualFill}; replacement?:{original:string;replacement:string;changed?:{to:string;data:string;value:string};state:'cancelled'|'changed_action'|'replacement_validated'} }
export class TradeJourney {
  private readonly requests=new QuoteRequest();
  private readonly recovery:ReceiptRecoveryScheduler;
  private readonly store:PendingAttemptStore;
  private readonly updates=new Map<string,RecoveryUpdate>();
  private readonly watchStops=new Map<string,()=>void>();
  private readonly completed=new Set<string>();
  private readonly listeners=new Set<()=>void>();
  private context?:TradeContext;
  private quoteTimer?:ReturnType<typeof setTimeout>;
  private closed=false;
  private readonly closing=new AbortController();
  private _state:JourneyState={signing:false,attempts:[]};
  get state(){return this._state;}
  constructor(readonly provider:ChainTradeProvider,readonly account:string,private readonly storage:PendingStorage,private readonly onComplete?:(attempt:PendingAttemptRecord,receipt:ReceiptEvidence,fill?:ActualFill)=>void,private readonly observe?:(observation:JourneyObservation)=>void){
    this.store=new PendingAttemptStore(storage,account);
    this.recovery=new ReceiptRecoveryScheduler((attempt,signal)=>provider.readReceipt(attempt,signal));
    const restored=this.store.load();
    if(restored.corrupt)this.patch({storageError:'Some stored attempts are corrupt; valid attempts remain visible. Clear local recovery history before submitting.'});
    for(const attempt of restored.records.filter(record=>['approve','buy','sell'].includes(record.action))){this.watch(attempt);if(attempt.replacementHash)void this.inspectReplacement(attempt.txHash,attempt.replacementHash as Hash).catch(error=>this.patch(error instanceof EvidenceIntegrityError?{integrityError:error.message}:{quoteError:failureReason(error)}));}
  }
  subscribe(listener:()=>void){this.listeners.add(listener);return()=>{this.listeners.delete(listener);};}
  private patch(change:Partial<JourneyState>){if(this.closed)return;this._state={...this._state,...change};for(const listener of this.listeners)listener();}
  setContext(context?:TradeContext){
    const next=context?contextKey(context):undefined;const previous=this.context?contextKey(this.context):undefined;
    if(next===previous)return;
    this.context=context?Object.freeze({...context}):undefined;clearTimeout(this.quoteTimer);this.requests.cancel();this.patch({quote:undefined,staleQuote:this._state.quote??this._state.staleQuote,quoteStatus:context?'requesting':'idle',quoteError:undefined});
    if(!context||context.inputRaw<=0n)return;
    this.quoteTimer=setTimeout(()=>{void this.requests.request(context,signal=>{this.observe?.({kind:'quote_start',at:Date.now(),monotonicMs:performance.now(),contextKey:next});return this.provider.readQuote(context,signal);}).then(quote=>{this.observe?.({kind:'quote_end',at:Date.now(),monotonicMs:performance.now(),contextKey:next,quoteId:quote?.quoteId,blockHeight:quote?.blockHeight.toString()});if(!this.closed&&this.context&&contextKey(this.context)===next)this.patch({quote,quoteStatus:quote?'executable':'stale'});}).catch(error=>{if(!this.closed&&this.context&&contextKey(this.context)===next)this.patch({quote:undefined,quoteStatus:'unavailable',quoteError:failureReason(error)});});},250);
  }
  requote(){const current=this.context;this.context=undefined;this.setContext(current);}
  private assertContext(context:TradeContext,scope:AccountScope){
    if(this.closed||!this.context||contextKey(this.context)!==contextKey(context)||scope.signal.aborted||scope.account.toLowerCase()!==this.account.toLowerCase())throw new Error('Context changed; review again');
    this.provider.session?.assertCurrent(scope,true);
  }
  private assertNoUnresolved(){if(signingAccounts.has(this.account.toLowerCase())||(unresolvedAccounts.get(this.account.toLowerCase())?.size??0)>0||[...this.updates.values()].some(update=>!update.receipt&&!['cancelled','replaced','changed_action'].includes(update.state)))throw new Error('An unresolved transaction requires receipt recovery before another submission');}
  private ensureCapacity(){if(this.updates.size>=200&&[...this.updates.values()].every(update=>!update.receipt))throw new Error('Recovery history contains 200 unresolved attempts; resolve an existing attempt first');}
  private async register(hash:Hash,action:string,fingerprint:string):Promise<PendingAttemptRecord>{
    const attempt:PendingAttemptRecord={version:1,intentId:crypto.randomUUID(),chainId:125,from:this.account,txHash:hash,submittedAt:Date.now(),action,actionFingerprint:fingerprint};
    if(this.closed){const owner=this.account.toLowerCase(),unresolved=unresolvedAccounts.get(owner)??new Set<string>();unresolved.add(hash.toLowerCase());unresolvedAccounts.set(owner,unresolved);this.store.save(attempt);return attempt;}
    this.watch(attempt);
    try{this.store.save(attempt);}catch(error){this.patch({storageError:`Signed transaction ${hash} retained in this session; local persistence unavailable: ${failureReason(error)}`});}
    try{const tx=await this.provider.publicClient.getTransaction({hash});attempt.nonce=tx.nonce;this.store.save(attempt);const previous=this.updates.get(hash.toLowerCase());if(previous)this.updates.set(hash.toLowerCase(),{...previous,attempt:Object.freeze({...attempt})});}catch{}
    return attempt;
  }
  private watch(attempt:PendingAttemptRecord){
    if(this.updates.size>=200){const oldest=[...this.updates.values()].filter(update=>!!update.receipt).sort((a,b)=>a.attempt.submittedAt-b.attempt.submittedAt)[0];if(oldest){const key=oldest.attempt.txHash.toLowerCase();this.updates.delete(key);this.watchStops.delete(key);}}
    const owner=this.account.toLowerCase();const unresolved=unresolvedAccounts.get(owner)??new Set<string>();unresolved.add(attempt.txHash.toLowerCase());unresolvedAccounts.set(owner,unresolved);
    this.updates.set(attempt.txHash.toLowerCase(),{attempt,state:'submitted'});
    const stop=this.recovery.watch(attempt,update=>{
      const key=attempt.txHash.toLowerCase();this.updates.set(key,update);
      if(update.receipt){const unresolved=unresolvedAccounts.get(owner);unresolved?.delete(key);if(!unresolved?.size)unresolvedAccounts.delete(owner);}
      if(update.error instanceof EvidenceIntegrityError)this.patch({integrityError:update.error.message});
      this.patch({attempts:[...this.updates.values()]});
      if(update.state==='confirmed_success'&&update.receipt){
        const fill=this.provider.fills.get(key);
        if(fill)this.patch({completion:{attempt:update.attempt,receipt:update.receipt,fill}});
        if(this.completed.has(key)){this.observe?.({kind:'duplicate_completion',at:Date.now(),monotonicMs:performance.now(),txHash:key,action:attempt.action});return;}
        const completionKey=`sidiora:completion:125:${this.account.toLowerCase()}`;
        try{
          const text=this.storage.getItem(completionKey);const claims:unknown=text?JSON.parse(text):[];
          if(!Array.isArray(claims)||claims.length>200||!claims.every(c=>c&&typeof c.hash==='string'&&/^0x[0-9a-fA-F]{64}$/.test(c.hash)&&typeof c.action==='string'&&c.action.length<=64&&Number.isSafeInteger(c.at)&&c.at>=0&&c.at<=Date.now()))throw new Error('corrupt_completion_storage');
          const recent=claims.filter(c=>Date.now()-c.at<=30*86400000);
          if(recent.some(c=>c.hash.toLowerCase()===key)){this.observe?.({kind:'duplicate_completion',at:Date.now(),monotonicMs:performance.now(),txHash:key,action:attempt.action});if(this.completed.size>=200)this.completed.delete(this.completed.values().next().value!);this.completed.add(key);return;}
          this.storage.setItem(completionKey,JSON.stringify([...recent,{hash:key,action:attempt.action,at:Date.now()}].slice(-200)));
        }catch(error){this.patch({storageError:`Confirmed chain result retained; local completion storage unavailable: ${failureReason(error)}`});return;}
        if(this.completed.size>=200)this.completed.delete(this.completed.values().next().value!);this.completed.add(key);
        this.observe?.({kind:'completion',at:Date.now(),monotonicMs:performance.now(),txHash:key,action:attempt.action,blockHeight:update.receipt.blockNumber.toString()});
        try{this.onComplete?.(update.attempt,update.receipt,fill);}catch(error){this.patch({quoteError:`Confirmed chain result retained; completion refresh failed: ${failureReason(error)}`});}
      }
    });
    this.watchStops.set(attempt.txHash.toLowerCase(),stop);
    this.patch({attempts:[...this.updates.values()]});
  }
  private wait(attempt:PendingAttemptRecord,scope:AccountScope):Promise<ReceiptEvidence>{
    return new Promise((resolve,reject)=>{
      const inspect=()=>{if(scope.signal.aborted||this.closed){finish();reject(new Error('Wallet changed; review again'));return;}const update=this.updates.get(attempt.txHash.toLowerCase());if(update?.error instanceof EvidenceIntegrityError){finish();reject(update.error);}else if(update?.receipt){finish();if(update.receipt.status==='success')resolve(update.receipt);else reject(new Error('Approval reverted on chain'));}};
      const finish=()=>{this.listeners.delete(inspect);scope.signal.removeEventListener('abort',inspect);this.closing.signal.removeEventListener('abort',inspect);};
      this.listeners.add(inspect);scope.signal.addEventListener('abort',inspect,{once:true});this.closing.signal.addEventListener('abort',inspect,{once:true});inspect();
    });
  }
  async approve(scope:AccountScope,unlimited=false){
    const context=this.context;if(!context||this._state.signing)throw new Error('No reviewed context');if(this._state.storageError||this._state.integrityError)throw new Error('Recovery integrity requires review');this.assertContext(context,scope);this.assertNoUnresolved();this.ensureCapacity();signingAccounts.add(this.account.toLowerCase());this.patch({signing:true});
    try{
      const allowance=await this.provider.readAllowance(context);this.assertContext(context,scope);
      const target=unlimited?(1n<<256n)-1n:context.inputRaw;
      const reset=allowance<target&&await this.provider.requiresZeroReset(context,target);this.assertContext(context,scope);
      const plan=approvalPlan(context,this.provider.deployment.router,allowance,reset,unlimited);
      for(let index=0;index<plan.steps.length;index++){
        this.assertContext(context,scope);
        const data=encodeFunctionData({abi:erc20Abi,functionName:'approve',args:[this.provider.deployment.router,plan.steps[index]]});
        const hash=await this.provider.submitApproval(context,plan.steps[index],scope);
        const attempt=await this.register(hash,'approve',transactionFingerprint(context.token,data));
        this.observe?.({kind:'approval_submitted',at:Date.now(),monotonicMs:performance.now(),txHash:hash,contextKey:contextKey(context),action:'approve'});
        const receipt=await this.wait(attempt,scope);this.assertContext(context,scope);
        const blockHeight=await this.provider.publicClient.getBlockNumber();const allowanceRaw=await this.provider.readAllowance(context,blockHeight);this.assertContext(context,scope);
        const stepFingerprint=approvalStepFingerprint(plan,index);
        if(!approvalMayContinue(plan,context,{...attempt,actionFingerprint:stepFingerprint},receipt,{chainId:125,account:this.account,token:context.token,spender:context.router,allowanceRaw,blockHeight,observedAt:Date.now(),approvalTxHash:hash,stepFingerprint},index))throw new EvidenceIntegrityError('Refreshed allowance does not match approved amount');
        this.observe?.({kind:'allowance_verified',at:Date.now(),monotonicMs:performance.now(),txHash:hash,contextKey:contextKey(context),blockHeight:blockHeight.toString()});
      }
      this.requote();
    }finally{signingAccounts.delete(this.account.toLowerCase());this.patch({signing:false});}
  }
  async swap(scope:AccountScope,bps=100,consent?:RiskConsent){
    const context=this.context,quote=this._state.quote;if(!context||!quote||this._state.signing)throw new Error('Fresh quote required');this.assertContext(context,scope);
    if(this._state.storageError||this._state.integrityError)throw new Error('Recovery integrity requires review');
    this.assertNoUnresolved();
    const latestApproval=[...this.updates.values()].filter(update=>update.attempt.action==='approve').sort((a,b)=>b.attempt.submittedAt-a.attempt.submittedAt)[0];
    if(latestApproval?.state==='confirmed_reverted')throw new Error('Approval reverted; verify a new approval before spending');
    this.ensureCapacity();tradeLimits(quote,context,bps,Math.floor(Date.now()/1000),120,consent);
    signingAccounts.add(this.account.toLowerCase());this.patch({signing:true});
    try{
      const reviewed=tradeLimits(quote,context,bps,Math.floor(Date.now()/1000),120,consent);
      let simulation;
      try{simulation=await this.provider.simulate(context,quote,reviewed.minimumOutputRaw,BigInt(reviewed.deadlineUnix),scope.signal);}catch(error){this.patch({quote:undefined,staleQuote:quote,quoteStatus:'stale',quoteError:`${failureReason(error)}. Refresh quote before signing.`});throw error;}
      this.assertContext(context,scope);
      if(!simulationAllowed(context,simulation,quote)||!executableQuote(quote,context,Date.now())){this.patch({quote:undefined,staleQuote:quote,quoteStatus:'stale',quoteError:'Simulation expired; refresh quote before signing'});throw new Error('Simulation expired; review again');}
      const data=this.provider.swapData(context,minimumOutput(quote.outputRaw,bps),BigInt(reviewed.deadlineUnix));
      let hash:Hash;
      try{hash=await this.provider.submitSwap(context,reviewed.minimumOutputRaw,BigInt(reviewed.deadlineUnix),scope);}catch(error){this.patch({quote:undefined,staleQuote:quote,quoteStatus:'stale',quoteError:`${failureReason(error)}. Refresh quote before signing.`});throw error;}
      const attempt=await this.register(hash,context.isBuy?'buy':'sell',transactionFingerprint(context.router,data));
      this.observe?.({kind:'swap_submitted',at:Date.now(),monotonicMs:performance.now(),txHash:hash,contextKey:contextKey(context),action:attempt.action});return attempt;
    }finally{signingAccounts.delete(this.account.toLowerCase());this.patch({signing:false});}
  }
  async inspectReplacement(originalHash:string,replacementHash:Hash){
    let original=this.store.load().records.find(record=>record.txHash.toLowerCase()===originalHash.toLowerCase());if(!original)throw new Error('Unknown original attempt');
    if(original.nonce===undefined){const actual=await this.provider.publicClient.getTransaction({hash:originalHash as Hash});original={...original,nonce:actual.nonce};}
    const verified=await this.provider.replacement(original,replacementHash);
    if(this.closed)return;
    this.store.save({...original,replacementHash});
    const unresolved=unresolvedAccounts.get(this.account.toLowerCase());unresolved?.delete(originalHash.toLowerCase());if(!unresolved?.size)unresolvedAccounts.delete(this.account.toLowerCase());
    const tx=await this.provider.publicClient.getTransaction({hash:replacementHash});
    this.watchStops.get(originalHash.toLowerCase())?.();this.watchStops.delete(originalHash.toLowerCase());
    const prior=this.updates.get(originalHash.toLowerCase());if(prior)this.updates.set(originalHash.toLowerCase(),{...prior,state:verified.cancelled?'cancelled':verified.changed?'changed_action':'replaced'});
    this.patch({attempts:[...this.updates.values()],replacement:{original:originalHash,replacement:replacementHash,state:verified.cancelled?'cancelled':verified.changed?'changed_action':'replacement_validated',changed:verified.changed?{to:tx.to!,data:tx.input,value:tx.value.toString()}:undefined}});
    if(!verified.changed){if(this.updates.has(replacementHash.toLowerCase()))this.retry(replacementHash);else await this.register(replacementHash,original.action,transactionFingerprint(tx.to!,tx.input,tx.value));}
  }
  async recoverHash(hash:Hash,action:'approve'|'buy'|'sell'){
    if(this.closed||!/^0x[0-9a-fA-F]{64}$/.test(hash))throw new Error('invalid_recovery_hash');
    const tx=await this.provider.publicClient.getTransaction({hash});
    if(!tx.to||tx.from.toLowerCase()!==this.account.toLowerCase()||await this.provider.publicClient.getChainId()!==125)throw new EvidenceIntegrityError('recovery_account_or_chain_mismatch');
    if(this.closed)return;
    if(this.updates.has(hash.toLowerCase())){this.retry(hash);return;}
    this.ensureCapacity();
    await this.register(hash,action,transactionFingerprint(tx.to,tx.input,tx.value));
  }
  repairLocalHistory(){
    const loaded=this.store.load();this.store.clear();
    for(const record of loaded.records)this.store.save(record);
    this.patch({storageError:undefined});
  }
  retry(hash:string){this.recovery.wake(hash);}
  page(offset=0){return this.store.page(offset);}
  close(){this.closed=true;this.closing.abort();clearTimeout(this.quoteTimer);this.requests.cancel();this.recovery.close();this.watchStops.clear();this.listeners.clear();}
}
export function failureReason(error:unknown):string {
  if(error instanceof Error){const detail=('shortMessage' in error&&typeof error.shortMessage==='string')?error.shortMessage:error.message;return detail.slice(0,600);}
  return 'Transaction provider unavailable';
}
