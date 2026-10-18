import { address, hash, raw, validateDraft, type Draft, type Cost } from './creation-domain';
import { validateStagedMediaDescriptor, type StagedMediaDescriptor } from './media-validation';

/** `stagedMedia` is a fingerprint of the locally selected image (name, size, type, sha256, dimensions); file bytes are never persisted. */
export interface CreationDraftV2 extends Draft { version: 2; feeStrategy: 0 | 1 | 2 | 3; stagedMedia?: StagedMediaDescriptor }
export interface LaunchAttempt {
 intentId: string; creator: string; chainId: 125; draftId: string; reviewedSnapshot: string;
 draft: CreationDraftV2; cost: Cost; deploymentRevision: string; createdAt: number;
 state: 'signing_locked' | 'outcome_unknown' | 'submitted' | 'resolved' | 'reverted';
 createTxHash?: string; poolAddress?: string; tokenAddress?: string;
 metadataStatus: 'metadata_pending' | 'published'; metadataContentHash?: string;
 metadataPublicationBody?:string;metadataEnqueuedAt?:number;metadataLastError?:string;metadataAssetId?:string;metadataUploadId?:string;metadataUploadHash?:string;metadataOperationId?: string; metadataRevision?: string; metadataPatch?: string; metadataAttemptCount: number;
 purchaseState: 'not_requested' | 'review_required' | 'signing_locked' | 'outcome_unknown' | 'submitted' | 'confirmed' | 'failed' | 'skipped';
 purchaseTxHash?: string; purchaseReview?: string; deliberateNew?: boolean;
}
export function validateCreationDraft(draft: CreationDraftV2, now = Date.now()) {
 if(!draft||typeof draft!=='object'){const errors:Record<string,string>={draft:'Invalid draft.'};return errors;}
 const errors = validateDraft({...draft,description:''}, now);
 if(draft.opticalSelection!=='standard')errors.opticalSelection='This option isn\'t available yet. Pick Standard, your other details are kept.';
 if (!draft || draft.version !== 2 || !Number.isInteger(draft.feeStrategy) || draft.feeStrategy < 0 || draft.feeStrategy > 3) errors.feeStrategy = 'Pick where creator rewards go.';
 if (typeof draft.description !== 'string' || [...draft.description].length > 4096 || /[\p{Cc}\p{Cf}]/u.test(draft.description)) errors.description = 'Keep the description under 4,096 characters';
 if(!Array.isArray(draft.socialLinks)||draft.socialLinks.length>5)errors.socialLinks='Add up to 5 links, each starting with https://';
 if(!Array.isArray(draft.uploadRefs)||draft.uploadRefs.length>1||draft.uploadRefs.some(value=>!/^([a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})$/.test(value)))errors.uploadRefs='Pick one image.';
 if(draft.stagedMedia!==undefined){const reason=validateStagedMediaDescriptor(draft.stagedMedia);if(reason)errors.stagedMedia=reason;}
 return errors;
}
function validStoredDraft(draft:CreationDraftV2,creator:string) {
 if(!draft||draft.version!==2||draft.chainId!==125||address(draft.creator)!==address(creator)||!draft.draftId||!Number.isSafeInteger(draft.updatedAt)||draft.updatedAt>Date.now()||Date.now()-draft.updatedAt>30*86400000||!Number.isInteger(draft.feeStrategy)||draft.feeStrategy<0||draft.feeStrategy>3)throw new Error('Saved draft expired or is invalid.');
 for(const value of [draft.name,draft.symbol,draft.description,draft.opticalSelection])if(typeof value!=='string'||value.length>8192)throw new Error('Saved draft text is invalid.');
 for(const values of [draft.tags,draft.socialLinks,draft.uploadRefs])if(!Array.isArray(values)||values.length>10||values.some(value=>typeof value!=='string'||value.length>8192))throw new Error('Saved draft list is invalid.');
 if(draft.stagedMedia!==undefined&&validateStagedMediaDescriptor(draft.stagedMedia))throw new Error('Saved draft media fingerprint is invalid.');
 raw(draft.firstBuyRaw);
}
function checkedAttempt(value: LaunchAttempt, creator: string): LaunchAttempt {
 if (!value || value.chainId !== 125 || address(value.creator) !== address(creator) || !value.intentId || !value.draftId || value.draftId !== value.draft?.draftId || address(value.draft.creator) !== address(creator) || value.draft.chainId !== 125 || !value.reviewedSnapshot || value.reviewedSnapshot.length > 131072 || !value.deploymentRevision || !Number.isSafeInteger(value.createdAt)) throw new Error('Invalid saved launch attempt.');
 if (!['signing_locked','outcome_unknown','submitted','resolved','reverted'].includes(value.state) || !['metadata_pending','published'].includes(value.metadataStatus) || !['not_requested','review_required','signing_locked','outcome_unknown','submitted','confirmed','failed','skipped'].includes(value.purchaseState)) throw new Error('Invalid saved launch state.');
 validStoredDraft(value.draft,creator);
 if(Object.keys(validateCreationDraft(value.draft)).length)throw new Error('Saved launch fields are invalid.');
 const snapshot=JSON.parse(value.reviewedSnapshot) as {intentId:string;draft:CreationDraftV2;cost:Cost;optical:{presetId:string;registryRevision:string;opticalAddress:string;initializationData:string}};
 if(snapshot.intentId!==value.intentId||JSON.stringify(snapshot.draft)!==JSON.stringify(value.draft)||snapshot.cost.manifestRevision!==value.deploymentRevision||snapshot.optical.registryRevision!==value.deploymentRevision||snapshot.optical.presetId!==value.draft.opticalSelection||snapshot.optical.opticalAddress!=='0x0000000000000000000000000000000000000000'||snapshot.optical.initializationData!=='0x'||value.cost.manifestRevision!==value.deploymentRevision||value.cost.optionalInitialBuyRaw!==value.draft.firstBuyRaw||snapshot.cost.optionalInitialBuyRaw!==value.draft.firstBuyRaw)throw new Error('Saved reviewed creation identity changed.');
 for(const cost of [snapshot.cost,value.cost]){if(cost.protocolFeeRaw!=='1000000')throw new Error('Saved creation fee changed.');address(cost.protocolFeeAssetRef);address(cost.approvalSpender);address(cost.factory);raw(cost.gasEstimatePaxRaw);}
 if(snapshot.cost.protocolFeeAssetRef!==value.cost.protocolFeeAssetRef||snapshot.cost.approvalSpender!==value.cost.approvalSpender||snapshot.cost.factory!==value.cost.factory)throw new Error('Saved creation deployment changed.');
 if (value.createTxHash) hash(value.createTxHash);
 if (['submitted','resolved','reverted'].includes(value.state) && !value.createTxHash) throw new Error('Saved launch hash is missing.');
 if (value.state === 'resolved') { address(value.poolAddress!); address(value.tokenAddress!); }
 if (value.purchaseTxHash) hash(value.purchaseTxHash);
 if(value.purchaseReview){
  if(value.purchaseReview.length>16384)throw new Error('Saved purchase review exceeds its limit.');
  const purchase=JSON.parse(value.purchaseReview) as {pool:string;creator:string;chainId:number;amountInRaw:string;amountOutRaw:string;minOutRaw:string;slippageBps:number;deadline:number;deploymentRevision:string;accountEpoch:number;blockHash:string;blockHeight:string};
  if(address(purchase.pool)!==address(value.poolAddress!)||address(purchase.creator)!==address(value.creator)||purchase.chainId!==125||purchase.amountInRaw!==value.draft.firstBuyRaw||purchase.deploymentRevision!==value.deploymentRevision||!Number.isInteger(purchase.slippageBps)||purchase.slippageBps<1||purchase.slippageBps>5000||!Number.isSafeInteger(purchase.deadline)||!Number.isSafeInteger(purchase.accountEpoch)||purchase.accountEpoch<0||raw(purchase.minOutRaw)!==raw(purchase.amountOutRaw)*BigInt(10000-purchase.slippageBps)/10000n||raw(purchase.minOutRaw)===0n)throw new Error('Saved purchase identity or protection changed.');hash(purchase.blockHash);raw(purchase.blockHeight);
 }
 if(['submitted','confirmed','failed'].includes(value.purchaseState)&&(!value.purchaseTxHash||!value.purchaseReview))throw new Error('Saved purchase correlation is missing.');
 if(value.metadataStatus==='published'&&(!value.metadataRevision||!value.metadataOperationId||!value.metadataContentHash||!value.metadataPublicationBody))throw new Error('Saved metadata publication correlation is missing.');
 if(value.deliberateNew!==undefined&&typeof value.deliberateNew!=='boolean')throw new Error('Invalid saved recovery decision.');
 if (value.metadataContentHash) hash(value.metadataContentHash);
 if (!Number.isInteger(value.metadataAttemptCount) || value.metadataAttemptCount < 0 || value.metadataAttemptCount > 5) throw new Error('Invalid metadata retry budget.');
 if (value.metadataRevision) raw(value.metadataRevision);
 if(value.metadataAssetId&&!/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(value.metadataAssetId))throw new Error('Invalid saved approved asset.');
 if(value.metadataUploadHash)hash(value.metadataUploadHash);
 if(value.metadataPatch){
  if(new TextEncoder().encode(value.metadataPatch).length>32768)throw new Error('Saved metadata patch exceeds its limit.');
  const patch=JSON.parse(value.metadataPatch) as {expectedRevision:string;description:string;displayName:string;displaySymbol:string;tags:string[];socialLinks:{label:string;url:string}[];logoAssetId?:string};
  raw(patch.expectedRevision);if(patch.description!==value.draft.description||patch.displayName!==value.draft.name||patch.displaySymbol!==value.draft.symbol||JSON.stringify(patch.tags)!==JSON.stringify(value.draft.tags)||JSON.stringify(patch.socialLinks)!==JSON.stringify(value.draft.socialLinks.map((url,index)=>({label:`Link ${index+1}`,url})))||patch.logoAssetId!==(value.metadataAssetId??value.draft.uploadRefs[0]))throw new Error('Saved metadata differs from the reviewed draft.');
 }
 if(value.metadataPublicationBody){
  if(new TextEncoder().encode(value.metadataPublicationBody).length>36864)throw new Error('Saved publication exceeds its limit.');
  const body=JSON.parse(value.metadataPublicationBody) as {draftId:string;operationId:string;creationTxHash:string;metadata:unknown;uploadRefs:string[]};
  if(body.draftId!==value.draftId||body.operationId!==value.metadataOperationId||body.creationTxHash!==value.createTxHash||JSON.stringify(body.metadata)!==value.metadataPatch||!Array.isArray(body.uploadRefs)||body.uploadRefs.length>1||body.uploadRefs.some(id=>id!==(value.metadataAssetId??value.draft.uploadRefs[0])))throw new Error('Saved publication scope changed.');
 }
 if(value.metadataEnqueuedAt&&(!Number.isSafeInteger(value.metadataEnqueuedAt)||value.metadataEnqueuedAt>Date.now()+1000))throw new Error('Invalid publication acceptance time.');
 return value;
}
export class CreationStorage {
 private db: Promise<IDBDatabase>;
 constructor(private creator: string, database = 'sidiora-creation-v2') {
  this.creator = address(creator);
  this.db = new Promise((resolve,reject) => {
   const request = indexedDB.open(database,1);
   request.onupgradeneeded = () => request.result.createObjectStore('attempts',{keyPath:'key'});
   request.onsuccess = () => resolve(request.result);
   request.onerror = () => reject(request.error ?? new Error('Recovery storage is unavailable.'));
   request.onblocked = () => reject(new Error('Close another launch tab before upgrading recovery storage.'));
  });
 }
 private key(intentId: string) { return `125:${this.creator}:${intentId}`; }
 async history(): Promise<LaunchAttempt[]> {
  const db = await this.db;
  return new Promise((resolve,reject) => {
   const tx = db.transaction('attempts','readonly'); const request = tx.objectStore('attempts').getAll();
   request.onsuccess = () => {
    try { const rows = (request.result as {key:string;value:LaunchAttempt}[]).filter(row => row.key.startsWith(`125:${this.creator}:`)); if(rows.length>100)throw new Error('Launch history exceeds its limit.'); resolve(rows.map(row=>checkedAttempt(row.value,this.creator)).sort((a,b)=>b.createdAt-a.createdAt)); } catch(error){reject(error)}
   }; request.onerror=()=>reject(request.error);
  });
 }
 async put(attempt: LaunchAttempt): Promise<void> {
  checkedAttempt(attempt,this.creator); const db=await this.db;
  await new Promise<void>((resolve,reject)=>{
   const tx=db.transaction('attempts','readwrite'),store=tx.objectStore('attempts');
   const request=store.get(this.key(attempt.intentId));
   request.onsuccess=()=>{try{
    const prior=request.result?.value as LaunchAttempt|undefined;
    if(prior){checkedAttempt(prior,this.creator);if(prior.reviewedSnapshot!==attempt.reviewedSnapshot||JSON.stringify(prior.draft)!==JSON.stringify(attempt.draft)||prior.draftId!==attempt.draftId||prior.deploymentRevision!==attempt.deploymentRevision||prior.createdAt!==attempt.createdAt||prior.createTxHash&&prior.createTxHash!==attempt.createTxHash||prior.purchaseTxHash&&prior.purchaseTxHash!==attempt.purchaseTxHash||prior.state==='resolved'&&(attempt.state!=='resolved'||prior.poolAddress!==attempt.poolAddress||prior.tokenAddress!==attempt.tokenAddress)||prior.metadataContentHash&&prior.metadataContentHash!==attempt.metadataContentHash||prior.metadataPatch&&prior.metadataPatch!==attempt.metadataPatch||prior.metadataPublicationBody&&prior.metadataPublicationBody!==attempt.metadataPublicationBody||prior.metadataStatus==='published'&&attempt.metadataStatus!=='published'||prior.metadataRevision&&prior.metadataRevision!==attempt.metadataRevision||prior.metadataOperationId&&prior.metadataOperationId!==attempt.metadataOperationId||attempt.metadataAttemptCount<prior.metadataAttemptCount)throw new Error('Saved launch identity or signing fence changed.');}
    store.put({key:this.key(attempt.intentId),value:attempt});
   }catch(error){reject(error);tx.abort();}};
   tx.oncomplete=()=>resolve();tx.onabort=()=>reject(tx.error??new Error('Launch recovery could not be saved.'));tx.onerror=()=>reject(tx.error);
  });
 }
 async releaseUnsent(attempt:LaunchAttempt):Promise<void>{
  const db=await this.db;
  await new Promise<void>((resolve,reject)=>{
   const tx=db.transaction('attempts','readwrite'),store=tx.objectStore('attempts'),request=store.get(this.key(attempt.intentId));
   request.onsuccess=()=>{const prior=request.result?.value as LaunchAttempt|undefined;if(!prior||prior.state!=='signing_locked'||prior.createTxHash||prior.reviewedSnapshot!==attempt.reviewedSnapshot){reject(new Error('Cannot release a submitted or changed signing fence.'));tx.abort();return;}store.delete(this.key(attempt.intentId));};
   tx.oncomplete=()=>resolve();tx.onabort=()=>reject(tx.error??new Error('Signing fence release failed.'));tx.onerror=()=>reject(tx.error);
  });
 }
 async exclusively<T>(action:()=>Promise<T>):Promise<T> {
  if(!navigator.locks)throw new Error('This browser cannot safely lock a launch across tabs. Use a browser with Web Locks.');
  return navigator.locks.request(`creation:125:${this.creator}`,{mode:'exclusive',ifAvailable:true},lock=>{
   if(!lock)throw new Error('Another tab is already handling this creator launch.'); return action();
  });
 }
 async admit(attempt:LaunchAttempt):Promise<void> {
  checkedAttempt(attempt,this.creator);if(attempt.state!=='signing_locked'||attempt.createTxHash)throw new Error('Only an unsent reviewed intent may enter signing.');
  const db=await this.db;
  await new Promise<void>((resolve,reject)=>{
   const tx=db.transaction('attempts','readwrite'),store=tx.objectStore('attempts'),request=store.getAll();
   request.onsuccess=()=>{try{
    const history=(request.result as {key:string;value:LaunchAttempt}[]).filter(row=>row.key.startsWith(`125:${this.creator}:`)).map(row=>checkedAttempt(row.value,this.creator));
    if(history.length>=100)throw new Error('Launch history is full. Export and resolve existing attempts before starting another.');
    if(history.some(row=>row.intentId===attempt.intentId||(row.draftId===attempt.draftId&&!row.deliberateNew)))throw new Error('Recover the existing attempt before creating another token.');
    store.add({key:this.key(attempt.intentId),value:attempt});
   }catch(error){reject(error);tx.abort();}};
   tx.oncomplete=()=>resolve();tx.onabort=()=>reject(tx.error??new Error('Launch admission failed.'));tx.onerror=()=>reject(tx.error);
  });
 }
 saveDraft(draft:CreationDraftV2) {
  validStoredDraft(draft,this.creator);
  const {draftId,version,creator,chainId,name,symbol,description,tags,socialLinks,uploadRefs,firstBuyRaw,opticalSelection,updatedAt,feeStrategy,stagedMedia}=draft;
  const body=JSON.stringify({draftId,version,creator,chainId,name,symbol,description,tags,socialLinks,uploadRefs,firstBuyRaw,opticalSelection,updatedAt,feeStrategy,...(stagedMedia?{stagedMedia}:{})});
  if(new TextEncoder().encode(body).length>65536)throw new Error('Draft exceeds 64 KiB.');
  localStorage.setItem(`creation-draft-v2:125:${this.creator}`,body);
 }
 restoreDraft():CreationDraftV2|null {
  const body=localStorage.getItem(`creation-draft-v2:125:${this.creator}`);if(!body)return null;
  if(new TextEncoder().encode(body).length>65536)throw new Error('Saved draft exceeds 64 KiB.');
  const draft=JSON.parse(body) as CreationDraftV2;
  validStoredDraft(draft,this.creator);
  const {draftId,version,creator,chainId,name,symbol,description,tags,socialLinks,uploadRefs,firstBuyRaw,opticalSelection,updatedAt,feeStrategy,stagedMedia}=draft;
  return {draftId,version,creator,chainId,name,symbol,description,tags,socialLinks,uploadRefs,firstBuyRaw,opticalSelection,updatedAt,feeStrategy,...(stagedMedia?{stagedMedia}:{})};
 }
}
