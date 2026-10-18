/** Phase-one domain primitives. All chain/service inputs must come from verified adapters. */
export const CREATION_FEE_RAW = 1_000_000n;
export const CHAIN_ID = 125;
const MAX = (1n << 256n) - 1n;
export function raw(value: string): bigint {
  if (typeof value !== 'string' || !/^(0|[1-9][0-9]{0,77})$/.test(value) || BigInt(value) > MAX) throw new Error('invalid_raw_amount');
  return BigInt(value);
}
export function address(value: string): string {
  if (typeof value !== 'string' || !/^0x[0-9a-fA-F]{40}$/.test(value) || /^0x0{40}$/i.test(value)) throw new Error('invalid_address');
  return value.toLowerCase();
}
export function hash(value: string): string {
  if (typeof value !== 'string' || !/^0x[0-9a-fA-F]{64}$/.test(value)) throw new Error('invalid_hash');
  return value.toLowerCase();
}
export interface Deployment { revision: string; chainId: number; usdl: string; router: string; factory: string }
export interface Cost { protocolFeeRaw: string; protocolFeeAssetRef: string; gasEstimatePaxRaw: string; optionalInitialBuyRaw: string; manifestRevision: string; approvalSpender:string; factory:string }
export function verifyCost(d: Deployment, read: { revision: string; decimals: number; feeRaw: string; gasRaw: string }, firstBuyRaw = '0'): Readonly<Cost> {
  if (d.chainId !== CHAIN_ID || !d.revision || read.revision !== d.revision) throw new Error('deployment_mismatch');
  address(d.router); address(d.factory);
  if (read.decimals !== 6 || raw(read.feeRaw) !== CREATION_FEE_RAW) throw new Error('configuration_blocked: creation requires 1.00 USDL (6 decimals)');
  raw(read.gasRaw); raw(firstBuyRaw);
  return Object.freeze({protocolFeeRaw:'1000000', protocolFeeAssetRef:address(d.usdl),gasEstimatePaxRaw:read.gasRaw,optionalInitialBuyRaw:firstBuyRaw,manifestRevision:d.revision,approvalSpender:address(d.router),factory:address(d.factory)});
}
export function funding(cost: Cost, d: Deployment, balances?: {usdl: string; pax: string; allowance: string}) {
  if (d.chainId!==125) throw new Error('wrong_chain');
  if (raw(cost.protocolFeeRaw)!==CREATION_FEE_RAW) throw new Error('fee_changed_review_required');
  if (!balances) return {state:'balance_loading' as const};
  const target = raw(cost.protocolFeeRaw) + raw(cost.optionalInitialBuyRaw);
  if (target > MAX || cost.manifestRevision !== d.revision || cost.approvalSpender!==address(d.router) || cost.factory!==address(d.factory) || address(cost.protocolFeeAssetRef) !== address(d.usdl)) throw new Error('review_required');
  const state = raw(balances.usdl) < target ? 'insufficient_usdl' : raw(balances.pax) < raw(cost.gasEstimatePaxRaw) ? 'insufficient_gas' : raw(balances.allowance) < target ? 'approval_required' : 'signable';
  return {state, approvalSpender:address(d.router), approvalTargetRaw:target.toString(), feeLabel:'1.00 USDL', gasAsset:'PAX'};
}
export interface Draft { draftId: string; version: number; creator: string; chainId: number; name: string; symbol: string; description: string; tags: string[]; socialLinks: string[]; uploadRefs: string[]; firstBuyRaw: string; opticalSelection: string; updatedAt: number }
export function validateDraft(d: Draft, now: number): Record<string,string> {
  const errors: Record<string,string> = {};
  if (!d || typeof d !== 'object') return {draft:'invalid_draft'};
  for (const [key,max] of [['name',50],['symbol',10],['description',5000]] as const) {
    const s = d[key];
    if (typeof s !== 'string' || (key !== 'description' && !s.length) || new TextEncoder().encode(s).length > max || /[\p{Cc}\p{Cf}]/u.test(s)) errors[key]='invalid_text';
  }
  try { address(d.creator); } catch { errors.creator='invalid_creator'; }
  if (d.chainId !== CHAIN_ID) errors.chainId='wrong_chain';
  if (typeof d.draftId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(d.draftId) || !Number.isSafeInteger(d.version) || d.version < 1) errors.draftId='invalid_identity';
  if (!Number.isSafeInteger(now) || !Number.isSafeInteger(d.updatedAt) || now < d.updatedAt || now-d.updatedAt > 30*86400000) errors.updatedAt='draft_expired';
  if (!Array.isArray(d.tags) || d.tags.length > 10 || new Set(d.tags).size !== d.tags.length || d.tags.some(t=>typeof t!=='string' || !t.length || t.length>256 || /[\p{Cc}\p{Cf}]/u.test(t))) errors.tags='invalid_tags';
  if (!Array.isArray(d.socialLinks) || d.socialLinks.length > 10 || d.socialLinks.some(s=>{if(typeof s!=='string'||/[\p{Cc}\p{Cf}]/u.test(s))return true;try { const u = new URL(s); return u.protocol !== 'https:' || !!u.username || !!u.password || s.length>2048; } catch {return true;}})) errors.socialLinks='unsafe_url';
  if (!Array.isArray(d.uploadRefs) || d.uploadRefs.length>2 || d.uploadRefs.some(s=>typeof s!=='string'||!/^[-a-zA-Z0-9_]{1,256}$/.test(s))) errors.uploadRefs='invalid_asset_reference';
  if(typeof d.opticalSelection!=='string'||!d.opticalSelection||d.opticalSelection.length>128)errors.opticalSelection='invalid_optical';
  try {raw(d.firstBuyRaw);} catch {errors.firstBuyRaw='invalid_amount';}
  if (new TextEncoder().encode(JSON.stringify(d)).length>65536) errors.draft='payload_too_large';
  return errors;
}
export interface Preset { id: string; address: string; initializationData: string; restrictions: string[]; supported: boolean }
export function selectOptical(id: string, revision: string, presets: readonly Preset[]) {
  if (!revision || presets.length>50) throw new Error('invalid_registry');
  if (id==='standard') return Object.freeze({registryRevision:revision,presetId:id,opticalAddress:'0x0000000000000000000000000000000000000000',initializationData:'0x',restrictionsSummary:['Standard AMM']});
  const p = presets.filter(p=>p.id===id);
  if (p.length!==1 || !p[0].supported || !/^0x(?:[0-9a-fA-F]{2})*$/.test(p[0].initializationData) || p[0].initializationData.length>8194) throw new Error('unsupported_optical');
  return Object.freeze({registryRevision:revision,presetId:id,opticalAddress:address(p[0].address),initializationData:p[0].initializationData,restrictionsSummary:Object.freeze([...p[0].restrictions])});
}
export function review(intentId: string, draft: Draft, cost: Cost, optical: ReturnType<typeof selectOptical>, now: number) {
  const errors=validateDraft(draft,now); if(Object.keys(errors).length) throw new Error(JSON.stringify(errors));
  if (!intentId || draft.opticalSelection!==optical.presetId || draft.firstBuyRaw!==cost.optionalInitialBuyRaw) throw new Error('review_context_mismatch');
  const snapshot=JSON.stringify({intentId,draft,cost,optical});
  // Serialized snapshot prevents callers mutating nested reviewed calldata.
  return Object.freeze({intentId,snapshot,creator:address(draft.creator),chainId:CHAIN_ID,manifestRevision:cost.manifestRevision});
}
export function assertReview(r: ReturnType<typeof review>, draft: Draft, cost: Cost, optical: ReturnType<typeof selectOptical>, now: number) {
  if (review(r.intentId,draft,cost,optical,now).snapshot!==r.snapshot) throw new Error('review_invalidated');
}
export interface CreationEvent {transactionHash:string; emitter:string; creator:string; pool:string; token:string; logIndex:number; blockHeight:string; blockHash:string}
export interface Receipt {chainId:number; transactionHash:string; status:'success'|'reverted'; events:CreationEvent[];blockHeight:string;blockHash:string}
export async function resolveCreation(receipt: Receipt, expected: {hash:string; creator:string; factory:string}, registry: (pool:string, block:{height:string;hash:string})=>Promise<{token:string;creator:string;blockHeight:string;blockHash:string}>, signal?: AbortSignal) {
  signal?.throwIfAborted();
  if (receipt.chainId!==CHAIN_ID || hash(receipt.transactionHash)!==hash(expected.hash)) throw new Error('receipt_context_mismatch');
  if (receipt.status!=='success') throw new Error('creation_reverted');
  const matching=receipt.events.filter(e=>hash(e.transactionHash)===hash(expected.hash) && address(e.emitter)===address(expected.factory) && address(e.creator)===address(expected.creator));
  if (matching.length===0) return {state:'resolution_delayed' as const,createTxHash:hash(expected.hash)};
  if (matching.length!==1) throw new Error('integrity_blocked: ambiguous_creation');
  const e=matching[0]; if(e.blockHeight!==receipt.blockHeight || hash(e.blockHash)!==hash(receipt.blockHash))throw new Error('event_block_mismatch'); address(e.token); address(e.pool); raw(e.blockHeight);
  if (!Number.isSafeInteger(e.logIndex)||e.logIndex<0) throw new Error('invalid_log_index');
  const registered=await registry(e.pool,{height:receipt.blockHeight,hash:receipt.blockHash}); signal?.throwIfAborted();
  if (registered.blockHeight!==receipt.blockHeight || hash(registered.blockHash)!==hash(receipt.blockHash))throw new Error('registry_block_mismatch');
  if (address(registered.token)!==address(e.token)||address(registered.creator)!==address(expected.creator)) throw new Error('integrity_blocked: registry_conflict');
  return {state:'resolved' as const,chainId:CHAIN_ID,createTxHash:hash(expected.hash),expectedCreator:address(expected.creator),emitter:address(expected.factory),poolAddress:address(e.pool),tokenAddress:address(e.token),logIndex:e.logIndex,blockHeight:e.blockHeight,registryVerified:true as const};
}
function firstPurchase(resolution: Awaited<ReturnType<typeof resolveCreation>>, quote: {pool:string;creator:string;amountInRaw:string;amountOutRaw:string;expiresAt:number;chainId:number}, expectedCreator:string, amountInRaw:string, slippageBps:number, now:number) {
  if (!Number.isSafeInteger(now)||!Number.isSafeInteger(quote.expiresAt))throw new Error('invalid_quote_time');
  if (resolution.state!=='resolved' || quote.chainId!==CHAIN_ID || address(quote.pool)!==resolution.poolAddress || address(quote.creator)!==address(expectedCreator) || resolution.expectedCreator!==address(expectedCreator) || quote.amountInRaw!==amountInRaw || quote.expiresAt<=now || quote.expiresAt>now+30000) throw new Error('fresh_quote_required');
  if (!Number.isInteger(slippageBps)||slippageBps<1||slippageBps>5000 || raw(amountInRaw)===0n) throw new Error('invalid_purchase');
  const minOut=raw(quote.amountOutRaw)*BigInt(10000-slippageBps)/10000n;
  if (minOut<=0n) throw new Error('unsafe_minimum_output');
  return {pool:resolution.poolAddress,amountInRaw,minOutRaw:minOut.toString(),requiresExplicitReview:true};
}
export interface Storage {getItem(key:string):string|null;setItem(key:string,value:string):void}
export interface History {intentId:string;creator:string;chainId:number;draftId:string;state:'signing_locked'|'outcome_unknown'|'submitted';submittedHash?:string;recoveryDecision?:'deliberate_new'}
export class CreationRecovery {
  private locked=new Set<string>();
  private storage:Storage; private creator:string;
  constructor(storage:Storage, creator:string) {address(creator); this.storage=storage;this.creator=creator;}
  private key(){return `creation:125:${address(this.creator)}`;}
  load(): History[] {
    const records: unknown=JSON.parse(this.storage.getItem(this.key())??'[]');
    if (!Array.isArray(records)||records.length>100) throw new Error('corrupt_recovery_history');
    return records.map((r:History)=>{if(!r || address(r.creator)!==address(this.creator)||r.chainId!==125||!r.intentId||!r.draftId||!['signing_locked','outcome_unknown','submitted'].includes(r.state)) throw new Error('corrupt_recovery_history');if(r.submittedHash)hash(r.submittedHash);if(r.state==='submitted'&&!r.submittedHash)throw new Error('missing_hash');return {...r,state:r.state==='signing_locked'?'outcome_unknown':r.state};});
  }
  private save(record:History){const records=this.load().filter(r=>r.intentId!==record.intentId);if(records.length>=100)throw new Error('recovery_history_full');this.storage.setItem(this.key(),JSON.stringify([...records,record]));}
  deliberateNewLaunch(draftId:string, priorIntentId:string, decision:{recoveryChecked:boolean;explicitUserConsent:boolean}) {
    const prior=this.load().find(x=>x.intentId===priorIntentId && x.draftId===draftId);
    if(!prior||!decision.recoveryChecked||!decision.explicitUserConsent)throw new Error('recovery_decision_required');
    this.save({...prior,recoveryDecision:'deliberate_new'});
  }
  async submit(r:ReturnType<typeof review>, draftId:string, send:()=>Promise<string>) {
    const reviewed=JSON.parse(r.snapshot);
    if(reviewed.draft.draftId!==draftId)throw new Error('reviewed_draft_mismatch');
    if(address(r.creator)!==address(this.creator)||this.locked.has(r.intentId)||this.load().some(x=>x.intentId===r.intentId || (x.draftId===draftId && x.recoveryDecision!=='deliberate_new')))throw new Error('recover_existing_attempt');
    this.locked.add(r.intentId);
    const entry:History={intentId:r.intentId,creator:r.creator,chainId:125,draftId,state:'signing_locked'};
    this.save(entry); // Persist lock before wallet dispatch; storage failure prevents signing.
    let tx:string;
    try {tx=hash(await send());} catch(error) {this.save({...entry,state:'outcome_unknown'});throw error;}
    try {this.save({...entry,state:'submitted',submittedHash:tx});} catch {throw new Error(`recovery_storage_failed:${tx}`);}
    return tx;
  }
}
export interface Publication {key:string;draftId:string;tokenAddress:string;creator:string;contentHash:string;expectedRevision:string;attemptCount:number;createdAt:number;status:'metadata_pending'|'published'|'rejected';lastErrorCode?:string}
export function publication(draftId:string, resolution:Awaited<ReturnType<typeof resolveCreation>>, contentHash:string, expectedRevision:string, now:number): Publication {
  if(resolution.state!=='resolved'||!draftId)throw new Error('verified_creation_required');raw(expectedRevision);hash(contentHash);
  return {key:`${draftId}:${resolution.tokenAddress}:${hash(contentHash)}`,draftId,tokenAddress:resolution.tokenAddress,creator:resolution.expectedCreator,contentHash:hash(contentHash),expectedRevision,attemptCount:0,createdAt:now,status:'metadata_pending'};
}
export async function publishMetadata(p:Publication, creator:string, now:number, persist:(p:Publication)=>void, publish:(p:Readonly<Publication>)=>Promise<{revision:string}>, signal?:AbortSignal):Promise<Publication> {
  signal?.throwIfAborted();
  if(address(creator)!==address(p.creator))throw new Error('original_creator_authorization_required');
  if(p.status!=='metadata_pending')return p;
  if(p.attemptCount>=5||now-p.createdAt>1800000)throw new Error('automatic_retry_budget_exhausted');
  const attempted={...p,attemptCount:p.attemptCount+1};persist(attempted);
  try {const response=await publish(Object.freeze(attempted));raw(response.revision);if(raw(response.revision)<=raw(p.expectedRevision))throw new Error('stale_metadata_revision');const result={...attempted,status:'published' as const};persist(result);return result;} catch {const result={...attempted,lastErrorCode:'publication_failed'};persist(result);return result;}
}
function verifyApproval(receipt: {chainId:number;transactionHash:string;status:'success'|'reverted'}, expectedHash:string, actualAllowanceRaw:string, targetRaw:string) {
  if(raw(targetRaw)===0n)throw new Error('invalid_approval_target');
  if(receipt.chainId!==125 || hash(receipt.transactionHash)!==hash(expectedHash) || receipt.status!=='success' || raw(actualAllowanceRaw)<raw(targetRaw)) throw new Error('approval_not_verified');
  return true;
}
/** Store an explicit allowlist, never a wallet/auth object. Reload discards approval state. */
export function saveDraft(storage:Storage, draft:Draft, now:number) {
  if(Object.keys(validateDraft(draft,now)).length)throw new Error('invalid_draft');
  const {draftId,version,creator,chainId,name,symbol,description,tags,socialLinks,uploadRefs,firstBuyRaw,opticalSelection,updatedAt}=draft;
  storage.setItem(`creation-draft:125:${address(creator)}:${draftId}`,JSON.stringify({draftId,version,creator,chainId,name,symbol,description,tags,socialLinks,uploadRefs,firstBuyRaw,opticalSelection,updatedAt}));
}
export function restoreDraft(storage:Storage, creator:string, draftId:string, now:number):Draft|null {
  const text=storage.getItem(`creation-draft:125:${address(creator)}:${draftId}`);if(!text)return null;
  if(new TextEncoder().encode(text).length>65536)throw new Error('draft_payload_too_large');
  const d:Draft=JSON.parse(text);
  if(Object.keys(validateDraft(d,now)).length || address(d.creator)!==address(creator)||d.draftId!==draftId)throw new Error('untrusted_draft');
  return d;
}
/** Browser-owned durable outbox; the service must enforce key/revision and creator auth. */
export class MetadataOutbox {
  private storage:Storage;private creator:string;
  constructor(storage:Storage,creator:string){this.storage=storage;this.creator=address(creator);}
  private key(){return `creation-metadata:125:${this.creator}`;}
  load():Publication[]{
    const rows:Publication[]=JSON.parse(this.storage.getItem(this.key())??'[]');
    if(!Array.isArray(rows)||rows.length>100)throw new Error('invalid_outbox');
    for(const p of rows){
      if(address(p.creator)!==this.creator||!p.draftId||p.key!==`${p.draftId}:${address(p.tokenAddress)}:${hash(p.contentHash)}`||!Number.isInteger(p.attemptCount)||p.attemptCount<0||p.attemptCount>5||!Number.isSafeInteger(p.createdAt)||!['metadata_pending','published','rejected'].includes(p.status))throw new Error('invalid_outbox_entry');raw(p.expectedRevision);
    }
    return rows;
  }
  persist(p:Publication){
    if(address(p.creator)!==this.creator)throw new Error('creator_mismatch');
    const rows=this.load().filter(x=>x.key!==p.key);if(rows.length>=100)throw new Error('metadata_outbox_full');
    this.storage.setItem(this.key(),JSON.stringify([...rows,p]));
  }
}
export interface PurchaseContext {reviewedContent:string;simulationStatus:'success'|'failed';priceImpactBps:number;highImpactAcknowledged:boolean;highSlippageAcknowledged:boolean;manifestRevision:string;router:string;sender:string;recipient:string;opticalRevision:string;quoteId:string;simulationId:string;simulatedAt:number;deadline:number;consentIntentId:string}
/** Context supplied by authenticated quote/simulation adapters, then bound to explicit user review. */
export function reviewFirstPurchase(resolution:Awaited<ReturnType<typeof resolveCreation>>, quote:Parameters<typeof firstPurchase>[1] & PurchaseContext, expected:PurchaseContext, amountInRaw:string, slippageBps:number, now:number) {
  const content=JSON.stringify({pool:quote.pool,creator:quote.creator,amountInRaw,amountOutRaw:quote.amountOutRaw,expiresAt:quote.expiresAt,chainId:quote.chainId,slippageBps,router:quote.router,sender:quote.sender,recipient:quote.recipient,deadline:quote.deadline,manifestRevision:quote.manifestRevision,opticalRevision:quote.opticalRevision,quoteId:quote.quoteId,simulationId:quote.simulationId,simulatedAt:quote.simulatedAt,priceImpactBps:quote.priceImpactBps});
  if(expected.reviewedContent!==content || quote.reviewedContent!==content || quote.simulationStatus!=='success'||expected.simulationStatus!=='success'||!Number.isInteger(quote.priceImpactBps)||quote.priceImpactBps<0||quote.priceImpactBps>10000||quote.priceImpactBps!==expected.priceImpactBps||(quote.priceImpactBps>=500 && (!quote.highImpactAcknowledged||!expected.highImpactAcknowledged))||(slippageBps>=500 && (!quote.highSlippageAcknowledged||!expected.highSlippageAcknowledged)))throw new Error('purchase_review_or_simulation_required');
  for(const key of ['manifestRevision','opticalRevision','quoteId','simulationId','consentIntentId'] as const)if(!expected[key]||quote[key]!==expected[key])throw new Error('purchase_context_mismatch');
  for(const key of ['router','sender','recipient'] as const)if(address(quote[key])!==address(expected[key]))throw new Error('purchase_identity_mismatch');
  if(!Number.isSafeInteger(quote.simulatedAt)||quote.simulatedAt>now||now-quote.simulatedAt>30000||quote.simulatedAt!==expected.simulatedAt||!Number.isSafeInteger(quote.deadline)||quote.deadline!==expected.deadline||quote.deadline<now+30000||quote.deadline>now+600000)throw new Error('purchase_simulation_or_deadline_invalid');
  return Object.freeze({...firstPurchase(resolution,quote,expected.sender,amountInRaw,slippageBps,now),...expected});
}
export interface ApprovalContext {chainId:number;token:string;spender:string;account:string;manifestRevision:string;accountEpoch:number;targetRaw:string;transactionHash:string;blockHeight:string;blockHash:string}
export function verifyCreationApproval(expected:ApprovalContext, receipt:ApprovalContext & {status:'success'|'reverted'}, allowance:ApprovalContext & {allowanceRaw:string;observedAt:number}, deployment:Deployment, now:number, currentAccountEpoch:number) {
  if(!Number.isSafeInteger(now)||!Number.isSafeInteger(allowance.observedAt)||allowance.observedAt>now||now-allowance.observedAt>10000||currentAccountEpoch!==expected.accountEpoch)throw new Error('stale_approval_proof');
  if(deployment.chainId!==125||expected.chainId!==125||address(expected.token)!==address(deployment.usdl)||address(expected.spender)!==address(deployment.router)||expected.manifestRevision!==deployment.revision||!Number.isSafeInteger(expected.accountEpoch)||expected.accountEpoch<0)throw new Error('approval_context_invalid');
  address(expected.account);raw(expected.blockHeight);hash(expected.blockHash);
  for(const actual of [receipt,allowance])for(const key of Object.keys(expected) as (keyof ApprovalContext)[])if(actual[key]!==expected[key])throw new Error('approval_context_mismatch');
  return verifyApproval(receipt,expected.transactionHash,allowance.allowanceRaw,expected.targetRaw);
}
