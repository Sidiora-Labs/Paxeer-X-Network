import { fetchMetadataV2, type MetadataV2Patch } from '@/core/clients/metadata';
import { metadataApiUrl } from '@/core/sdk-config';
import { signOperation } from '@/core/operation-envelope';
import { KnownCapabilityAdmissionError } from './cap-admission';
import { requireAccount, assertAccount } from '../profile/account-transport';
import { address, hash, raw, review, selectOptical } from './creation-domain';
import { ChainCreationAdapter, creationFunding, type AccountScope, type FirstPurchaseQuote, type FundingRead } from './creation-adapters';
import { CreationStorage, validateCreationDraft, type CreationDraftV2, type LaunchAttempt } from './creation-storage';

function reviewedDraft(intentId:string,draft:CreationDraftV2,cost:FundingRead['cost'],optical:ReturnType<typeof selectOptical>,now:number){const base=review(intentId,{...draft,description:''},cost,optical,now);return {...base,snapshot:JSON.stringify({intentId,draft,cost,optical})};}
export interface ReviewedLaunch { intentId: string; snapshot: string; draft: Readonly<CreationDraftV2>; scope: AccountScope; read: FundingRead; reviewedAt: number }
export interface LaunchState { phase:'draft'|'review'|'approving'|'signing'|'submitted'|'resolution_delayed'|'resolved'|'metadata_pending'|'published'|'purchase_review'|'purchase_submitted'|'purchase_failed'|'purchase_confirmed'; busy:boolean; message:string; fieldErrors?:Record<string,string>; resolutionVerified?:boolean; publicationVerified?:boolean; reviewed?:ReviewedLaunch; attempt?:LaunchAttempt; quote?:FirstPurchaseQuote }
async function digest(text:string) {const bytes=new Uint8Array(await crypto.subtle.digest('SHA-256',new TextEncoder().encode(text)));return '0x'+Array.from(bytes,n=>n.toString(16).padStart(2,'0')).join('');}
function publicationSignal(signal:AbortSignal){return AbortSignal.any([signal,AbortSignal.timeout(15000)]);}
function operationUUID(digestValue:string) {const bytes=hash(digestValue).slice(2,34).split('');bytes[12]='5';bytes[16]=((parseInt(bytes[16],16)&3)|8).toString(16);const value=bytes.join('');return `${value.slice(0,8)}-${value.slice(8,12)}-${value.slice(12,16)}-${value.slice(16,20)}-${value.slice(20)}`;}
export class CreationController {
 state:LaunchState={phase:'draft',busy:false,message:''}; private publicationTimer?:ReturnType<typeof setTimeout>;private publicationDelay=1000;private publicationUntil=0;private disposed=false; private listeners=new Set<()=>void>();private running=false;private epoch=0;
 constructor(readonly chain:ChainCreationAdapter,readonly storage:CreationStorage,private signMessage:(message:string)=>Promise<string>) {}
 subscribe=(listener:()=>void)=>{this.disposed=false;this.listeners.add(listener);return()=>{this.listeners.delete(listener)}};
 private set(patch:Partial<LaunchState>){if(this.disposed)return;this.state={...this.state,...patch};for(const listener of this.listeners)listener();}
 dispose(){this.disposed=true;clearTimeout(this.publicationTimer);this.publicationTimer=undefined;this.epoch++;this.listeners.clear();}
 invalidate(message='Launch parameters changed. Review them again.') {this.epoch++;this.set({reviewed:undefined,quote:undefined,fieldErrors:undefined,phase:this.state.attempt?.state==='resolved'?'resolved':'draft',message});}
 private async single<T>(action:()=>Promise<T>) {if(this.running)throw new Error('A launch action is already waiting for your wallet or network.');this.running=true;this.set({busy:true,message:''});try{return await action()}catch(error){if(this.state.reviewed)this.invalidate('Launch dependency is unavailable. Review again before signing.');this.set({message:error instanceof Error?error.message:'Launch action failed.'});throw error}finally{this.running=false;this.set({busy:false})}}
 async reviewDraft(draft:CreationDraftV2) {
  return this.single(async()=>{
   this.invalidate();this.set({fieldErrors:undefined});
   const errors=validateCreationDraft(draft);if(Object.keys(errors).length)this.set({fieldErrors:errors});if(Object.keys(errors).length)throw new Error(Object.entries(errors).map(([field,error])=>`${field}: ${error}`).join('; '));
   const scope=this.chain.scope();if(address(draft.creator)!==scope.creator||draft.chainId!==scope.chainId)throw new Error('Review the draft with its creator wallet on chain 125.');
   const currentEpoch=this.epoch;const copy=JSON.parse(JSON.stringify(draft)) as CreationDraftV2;
   const read=await this.chain.readFunding(copy,scope);this.chain.scope(scope);if(currentEpoch!==this.epoch)throw new Error('Draft changed while reads were pending. Review again.');
   const optical=selectOptical(copy.opticalSelection,this.chain.deployment.revision,[]);
   const intentId=crypto.randomUUID();const reviewed=reviewedDraft(intentId,copy,read.cost,optical,Date.now());
   Object.freeze(copy.tags);Object.freeze(copy.socialLinks);Object.freeze(copy.uploadRefs);
   const snapshot:ReviewedLaunch={intentId,snapshot:reviewed.snapshot,draft:Object.freeze(copy),scope,read,reviewedAt:Date.now()};
   this.storage.saveDraft(copy);Object.freeze(read.balances);Object.freeze(read);Object.freeze(scope);Object.freeze(snapshot);this.set({phase:'review',reviewed:snapshot,message:''});return snapshot;
  });
 }
 private assertReviewed(value:ReviewedLaunch,epoch:number) {
  this.chain.scope(value.scope);
  if(this.state.reviewed!==value||epoch!==this.epoch||Date.now()-value.reviewedAt>30000)throw new Error('Review expired or the account changed. Review this launch again.');
  if(reviewedDraft(value.intentId,value.draft,value.read.cost,selectOptical(value.draft.opticalSelection,this.chain.deployment.revision,[]),Date.now()).snapshot!==value.snapshot)throw new Error('Launch review changed.');
 }
 async approve() {
  return this.single(async()=>this.storage.exclusively(async()=>{
   const reviewed=this.state.reviewed;if(!reviewed)throw new Error('Review this launch before approving.');const epoch=this.epoch;this.assertReviewed(reviewed,epoch);
   const fresh=await this.chain.readFunding(reviewed.draft,reviewed.scope);this.assertReviewed(reviewed,epoch);const plan=creationFunding(fresh);
   if(plan.state!=='approval_required')throw new Error(plan.state==='signable'?'Allowance is already sufficient. Review creation gas before submitting.':plan.state);
   await this.chain.admitCreation(reviewed.draft,reviewed.scope);this.assertReviewed(reviewed,epoch);
   this.set({phase:'approving',message:'Approve the exact USDL amount. Creation is a separate transaction.'});
   const tx=await this.chain.approve(plan.approvalTargetRaw!,reviewed.scope);this.set({message:`Approval submitted: ${tx}`});
   await this.chain.approvalProof(tx,plan.approvalTargetRaw!,reviewed.scope);
   this.invalidate('Approval confirmed. Review the fresh creation gas and launch details before creating.');
  }));
 }
 async create() {
  return this.single(async()=>this.storage.exclusively(async()=>{
   const reviewed=this.state.reviewed;if(!reviewed)throw new Error('Review this launch before creating.');const epoch=this.epoch;this.assertReviewed(reviewed,epoch);
   const fresh=await this.chain.readFunding(reviewed.draft,reviewed.scope);this.assertReviewed(reviewed,epoch);
   const plan=creationFunding(fresh);if(plan.state!=='signable')throw new Error(plan.state);
   const attempt:LaunchAttempt={intentId:reviewed.intentId,creator:reviewed.scope.creator,chainId:125,draftId:reviewed.draft.draftId,reviewedSnapshot:reviewed.snapshot,draft:JSON.parse(JSON.stringify(reviewed.draft)),cost:fresh.cost,deploymentRevision:this.chain.deployment.revision,createdAt:Date.now(),state:'signing_locked',metadataStatus:'metadata_pending',metadataAttemptCount:0,purchaseState:raw(reviewed.draft.firstBuyRaw)>0n?'review_required':'not_requested'};
   await this.chain.admitCreation(reviewed.draft,reviewed.scope);this.assertReviewed(reviewed,epoch);
   await this.storage.admit(attempt);this.set({phase:'signing',attempt,resolutionVerified:false,publicationVerified:false,message:'Confirm one createMarket transaction in your wallet.'});
   let tx:string;
   try{try{this.assertReviewed(reviewed,epoch)}catch(error){throw new KnownCapabilityAdmissionError(error instanceof Error?error.message:'Creation review expired.',error);}tx=hash(await this.chain.sendCreation(reviewed.draft,reviewed.scope,reviewed.reviewedAt+30000))}catch(error){if(error instanceof KnownCapabilityAdmissionError){await this.storage.releaseUnsent(attempt);this.set({attempt:undefined,phase:'review',message:error.message});throw error;}attempt.state='outcome_unknown';await this.storage.put(attempt);this.set({attempt,phase:'resolution_delayed',message:'Submission outcome is unknown. Recover this attempt before starting another.'});throw error}
   attempt.createTxHash=tx;attempt.state='submitted';
   try{await this.storage.put(attempt)}catch{this.set({attempt,phase:'submitted',reviewed:undefined,message:`Recovery storage failed. Save this transaction hash: ${tx}. Do not create again.`});throw new Error(`Recovery storage failed. Save transaction hash ${tx}; do not resubmit.`)}
   this.set({phase:'submitted',attempt,reviewed:undefined,message:`Creation submitted: ${tx}`});
   return attempt;
  }));
 }
 async resume(intentId:string) {
  return this.single(async()=>{
   const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);if(!attempt)throw new Error('Saved attempt was not found.');
   if(address(attempt.creator)!==this.chain.scope().creator||attempt.deploymentRevision!==this.chain.deployment.revision)throw new Error('Recover using the original creator and deployment.');
   if(!attempt.createTxHash){this.set({attempt,resolutionVerified:false,publicationVerified:false,quote:undefined,phase:'resolution_delayed',reviewed:undefined,message:'Submission outcome is unknown. Check your wallet transaction history and attach the exact hash.'});return attempt;}
   const recoveryScope=this.chain.scope();
   this.set({attempt,resolutionVerified:false,publicationVerified:false,quote:undefined,phase:'submitted',reviewed:undefined,message:'Checking the saved transaction. No new transaction will be sent.'});
   let resolved;
   try{await this.chain.client.waitForTransactionReceipt({hash:attempt.createTxHash as `0x${string}`,timeout:30000,pollingInterval:1500});resolved=await this.chain.resolve(attempt.createTxHash,attempt.creator,undefined,attempt.draft)}catch(error){if(error instanceof Error&&error.message==='creation_reverted'){attempt.state='reverted';await this.storage.put(attempt)}this.set({attempt,phase:'resolution_delayed'});throw error}
   if(resolved.state!=='resolved'){this.set({phase:'resolution_delayed',message:'Creation is submitted; pool resolution is delayed. Resume this same attempt.'});return attempt;}
   this.chain.scope(recoveryScope);
   attempt.state='resolved';attempt.poolAddress=resolved.poolAddress;attempt.tokenAddress=resolved.tokenAddress;
   await this.storage.put(attempt);this.set({attempt,resolutionVerified:true,publicationVerified:false,phase:'metadata_pending',message:'Token created. Checking metadata publication status.'});if(attempt.metadataOperationId)await this.refreshPublication(attempt.intentId);if(attempt.metadataEnqueuedAt&&attempt.metadataStatus!=='published'&&Date.now()<attempt.metadataEnqueuedAt+1800000){this.publicationUntil=attempt.metadataEnqueuedAt+1800000;this.schedulePublication(attempt.intentId);}return attempt;
  });
 }
 async attachRecoveredHash(intentId:string,txHash:string) {
  const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);if(!attempt)throw new Error('Unknown attempt.');
  const scope=this.chain.scope();if(scope.creator!==address(attempt.creator)||attempt.deploymentRevision!==this.chain.deployment.revision)throw new Error('Recover with the original creator and deployment.');
  const resolved=await this.chain.resolve(hash(txHash),attempt.creator,undefined,attempt.draft);this.chain.scope(scope);if(resolved.state!=='resolved')throw new Error('Recovered transaction has not resolved. Keep the same attempt.');
  attempt.createTxHash=hash(txHash);attempt.state='submitted';await this.storage.put(attempt);return this.resume(intentId);
 }
 async uploadLogo(intentId:string,file:File) {
  return this.single(async()=>this.storage.exclusively(async()=>{
   const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);
   if(!attempt||attempt.state!=='resolved'||!attempt.tokenAddress||!attempt.createTxHash||attempt.metadataPatch||attempt.metadataStatus==='published')throw new Error('Select the pool image after creation resolves and before metadata publication.');
   if(file.size<1||file.size>6291456||!['image/jpeg','image/png','image/webp'].includes(file.type))throw new Error('Choose JPEG, PNG or WebP up to 6 MiB.');
   const scope=this.chain.scope(),account=requireAccount(attempt.creator);if(scope.creator!==address(attempt.creator))throw new Error('Reconnect the original creator to upload.');
   const resolution=await this.chain.resolve(attempt.createTxHash,attempt.creator,undefined,attempt.draft);if(resolution.state!=='resolved'||resolution.tokenAddress!==attempt.tokenAddress)throw new Error('Pool image identity changed.');
   const bytes=new Uint8Array(await crypto.subtle.digest('SHA-256',await file.arrayBuffer())),contentHash='0x'+Array.from(bytes,b=>b.toString(16).padStart(2,'0')).join('');
   if(attempt.metadataUploadHash!==contentHash){attempt.metadataUploadId=crypto.randomUUID();attempt.metadataUploadHash=contentHash;attempt.metadataAssetId=undefined;await this.storage.put(attempt);}
   const id=attempt.metadataUploadId!;const endpoint=metadataApiUrl(`/v2/tokens/${attempt.tokenAddress}/assets/${id}`);
   const {decodeV2Contract}=await import('@kindle/sdk');
   const decode=async(response:Response)=>{if(!response.ok)throw new Error(`Image service unavailable or image rejected (${response.status}). Token remains created.`);const asset=decodeV2Contract('AssetGetResponse',await this.responseJSON(response)).data.asset;if(asset.assetId!==id||address(asset.ownerWallet)!==scope.creator||asset.sha256.toLowerCase()!==contentHash||asset.bytes!==file.size||asset.mediaType!==file.type)throw new Error('Image operation scope changed.');assertAccount(account);this.chain.scope(scope);return asset;};
   const headers={Authorization:`Bearer ${account.accessToken}`,'Idempotency-Key':id};
   let asset=await decode(await fetch(endpoint,{method:'PUT',body:file,signal:publicationSignal(account.signal),credentials:'omit',redirect:'error',headers:{...headers,'Content-Type':file.type}}));
   const until=Date.now()+60000;
   while(asset.state==='uploading'||asset.state==='quarantined'){
    if(Date.now()>=until){this.set({attempt,phase:'metadata_pending',message:'Token created. Image validation is pending; retry keeps the same image operation.'});return attempt;}
    await new Promise<void>((resolve,reject)=>{const abort=()=>{clearTimeout(timer);reject(new Error('Image authorization changed.'));};const timer=setTimeout(()=>{account.signal.removeEventListener('abort',abort);resolve();},1500);account.signal.addEventListener('abort',abort,{once:true});if(account.signal.aborted)abort();});
    asset=await decode(await fetch(endpoint,{signal:publicationSignal(account.signal),cache:'no-store',credentials:'omit',redirect:'error',headers}));
   }
   if(asset.state!=='approved'||!asset.publicUrl)throw new Error('Image was rejected. Token remains created and metadata pending.');
   attempt.metadataAssetId=id;await this.storage.put(attempt);this.set({attempt,phase:'metadata_pending',message:'Pool image approved. Review and publish metadata with this image.'});return attempt;
  }));
 }
 private async responseJSON(response:Response,limit=65536):Promise<unknown>{
  if(!response.headers.get('Content-Type')?.toLowerCase().startsWith('application/json')||!response.body)throw new Error('Publication service returned an invalid JSON response.');
  const reader=response.body.getReader(),chunks:Uint8Array[]=[];let length=0;
  try{for(;;){const next=await reader.read();if(next.done)break;length+=next.value.byteLength;if(length>limit)throw new Error('Publication response exceeded its limit.');chunks.push(next.value);}}finally{await reader.cancel();reader.releaseLock();}
  const bytes=new Uint8Array(length);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length;}return JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(bytes)) as unknown;
 }
 private async publicationResponse(response:Response,attempt:LaunchAttempt){
  if(!response.ok)throw new Error(`Metadata publication service unavailable or authorization rejected (${response.status}). Token remains created.`);
  const outer=await this.responseJSON(response) as {data?:Record<string,unknown>},value=outer.data;
  if(!value||value.schemaVersion!==3||value.operationId!==attempt.metadataOperationId||value.draftId!==attempt.draftId||value.tokenAddress!==attempt.tokenAddress||value.creator!==attempt.creator||value.contentHash!==attempt.metadataContentHash?.slice(2)||!['metadata_pending','publishing','published','rejected','authorization_required'].includes(String(value.status))||!Number.isInteger(value.attemptCount)||(value.attemptCount as number)<0||(value.attemptCount as number)>5||typeof value.lastErrorCode!=='string'||value.lastErrorCode.length>128||!Array.isArray(value.uploadRefs)||JSON.stringify(value.uploadRefs)!==JSON.stringify((JSON.parse(attempt.metadataPublicationBody!) as {uploadRefs:string[]}).uploadRefs))throw new Error('Metadata publication response differs from the saved creator intent.');
  const accepted=Date.parse(String(value.acceptedAt)),expires=Date.parse(String(value.expiresAt));if(!Number.isFinite(accepted)||accepted>Date.now()+1000||expires-accepted!==1800000)throw new Error('Metadata publication lifetime is invalid.');
  if(attempt.metadataEnqueuedAt!==undefined&&accepted!==attempt.metadataEnqueuedAt||(value.attemptCount as number)<attempt.metadataAttemptCount)throw new Error('Metadata publication history moved backwards.');
  if(value.status!=='published'&&attempt.metadataStatus==='published')throw new Error('Saved publication success cannot be confirmed by the service.');
  if(value.status==='published'&&attempt.metadataPatch&&raw(typeof value.revision==='string'?value.revision:'invalid')<=raw((JSON.parse(attempt.metadataPatch) as MetadataV2Patch).expectedRevision))throw new Error('Published metadata revision did not advance.');
  this.publicationUntil=expires;attempt.metadataEnqueuedAt=accepted;attempt.metadataAttemptCount=value.attemptCount as number;attempt.metadataLastError=value.lastErrorCode;
  if(value.status==='published'){if(typeof value.revision!=='string'||raw(value.revision)===0n)throw new Error('Published metadata revision is invalid.');attempt.metadataStatus='published';attempt.metadataRevision=value.revision;}
  await this.storage.put(attempt);this.set({attempt,publicationVerified:value.status==='published',phase:attempt.metadataStatus==='published'?'published':'metadata_pending',message:value.status==='published'?`Token created. Metadata published at revision ${value.revision}.`:value.status==='authorization_required'?'Token created. Metadata needs fresh original creator authorization.':value.status==='rejected'?`Token created. Metadata publication stopped: ${value.lastErrorCode}.`:'Token created. Metadata queued for durable publication.'});
  return String(value.status);
 }
 private schedulePublication(intentId:string){
  if(this.disposed||Date.now()>=this.publicationUntil)return;clearTimeout(this.publicationTimer);this.publicationTimer=setTimeout(()=>{void this.refreshPublication(intentId).catch(error=>{this.set({message:error instanceof Error?error.message:'Metadata service unavailable; token remains created.'});try{requireAccount(this.chain.scope().creator);this.schedulePublication(intentId)}catch{this.set({message:'Token created. Reconnect and authenticate the original creator to resume metadata status.'});}});},this.publicationDelay);this.publicationDelay=Math.min(30000,this.publicationDelay*2);
 }
 async refreshPublication(intentId:string){
  const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);if(!attempt?.metadataOperationId||!attempt.tokenAddress||!attempt.metadataPublicationBody)throw new Error('No saved publication operation.');
  const account=requireAccount(attempt.creator),scope=this.chain.scope();if(scope.creator!==attempt.creator)throw new Error('Reconnect the original creator to check metadata.');
  if(!attempt.createTxHash||!attempt.metadataPatch||await digest(attempt.metadataPatch)!==attempt.metadataContentHash)throw new Error('Saved metadata content cannot be verified.');
  const resolved=await this.chain.resolve(attempt.createTxHash,attempt.creator,account.signal,attempt.draft);if(resolved.state!=='resolved'||resolved.tokenAddress!==attempt.tokenAddress||resolved.poolAddress!==attempt.poolAddress)throw new Error('Saved metadata token identity changed.');
  const response=await fetch(metadataApiUrl(`/v3/tokens/${attempt.tokenAddress}/metadata-publications/${attempt.metadataOperationId}`),{headers:{Authorization:`Bearer ${account.accessToken}`},signal:publicationSignal(account.signal),credentials:'omit',cache:'no-store',redirect:'error'});assertAccount(account);this.chain.scope(scope);
  const state=await this.publicationResponse(response,attempt);
  if(['metadata_pending','publishing'].includes(state)&&attempt.metadataEnqueuedAt&&Date.now()<attempt.metadataEnqueuedAt+1800000)this.schedulePublication(intentId);else clearTimeout(this.publicationTimer);
  return attempt;
 }
 async publish(intentId:string) {
  return this.single(async()=>this.storage.exclusively(async()=>{
   const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);if(!attempt||attempt.state!=='resolved'||!attempt.tokenAddress||!attempt.createTxHash)throw new Error('Resolve the exact creation receipt before publishing.');
   const scope=this.chain.scope(),account=requireAccount(attempt.creator);if(scope.creator!==address(attempt.creator))throw new Error('Reconnect the original creator wallet to publish metadata.');
   if(attempt.metadataStatus==='published')return this.refreshPublication(intentId);
   const resolved=await this.chain.resolve(attempt.createTxHash,attempt.creator,undefined,attempt.draft);if(resolved.state!=='resolved'||resolved.tokenAddress!==attempt.tokenAddress)throw new Error('Creation identity changed. Metadata is blocked.');
   if(!attempt.metadataPatch){
    const current=await fetchMetadataV2(attempt.tokenAddress,account.signal);if(!current)throw new Error('Canonical metadata is not indexed yet. Token remains created, metadata pending.');
    if(address(current.token.address)!==address(attempt.tokenAddress)||current.token.sourcePosition?.chainId!=='125')throw new Error('Canonical metadata token identity does not match this launch.');
    const logo=attempt.metadataAssetId??attempt.draft.uploadRefs[0];
    const patch:MetadataV2Patch={expectedRevision:current.revision,description:attempt.draft.description,displayName:attempt.draft.name,displaySymbol:attempt.draft.symbol,tags:attempt.draft.tags,socialLinks:attempt.draft.socialLinks.map((url,index)=>({label:`Link ${index+1}`,url})),...(logo?{logoAssetId:logo}:{})};
    const {decodeV2Contract}=await import('@kindle/sdk');decodeV2Contract('MetadataPatchBody',patch);
    attempt.metadataPatch=JSON.stringify(patch);attempt.metadataContentHash=await digest(attempt.metadataPatch);attempt.metadataOperationId=operationUUID(await digest(`${attempt.draftId}:${attempt.tokenAddress}:${attempt.metadataContentHash}`));
    attempt.metadataPublicationBody=JSON.stringify({draftId:attempt.draftId,operationId:attempt.metadataOperationId,creationTxHash:attempt.createTxHash,metadata:patch,uploadRefs:logo?[logo]:[]});await this.storage.put(attempt);
   }
   if(await digest(attempt.metadataPatch)!==attempt.metadataContentHash||!attempt.metadataPublicationBody)throw new Error('Saved metadata content changed.');
   const headers={Authorization:`Bearer ${account.accessToken}`};
   const path=`/v3/tokens/${attempt.tokenAddress}/metadata-publications`;
   const prior=await fetch(metadataApiUrl(`${path}/${attempt.metadataOperationId}`),{headers,signal:publicationSignal(account.signal),credentials:'omit',cache:'no-store',redirect:'error'});
   if(prior.ok){const state=await this.publicationResponse(prior,attempt);if(['metadata_pending','publishing'].includes(state)){this.publicationDelay=1000;this.schedulePublication(intentId);return attempt;}if(state==='published')return attempt;if(state!=='authorization_required'||!attempt.metadataEnqueuedAt||Date.now()>=attempt.metadataEnqueuedAt+1800000||attempt.metadataAttemptCount>=5)throw new Error('Publication stopped or its retry budget expired. Keep the live token and inspect the service outcome.');}
   if(!prior.ok&&prior.status!==404)throw new Error(`Publication status is unavailable (${prior.status}); no duplicate enqueue was sent.`);
   const authorityResponse=await fetch(metadataApiUrl('/v3/metadata-publication-authority'),{headers,signal:publicationSignal(account.signal),credentials:'omit',cache:'no-store',redirect:'error'});if(!authorityResponse.ok)throw new Error('Metadata publication signing authority unavailable.');
   const authority=(await this.responseJSON(authorityResponse,8192) as {data?:Record<string,unknown>}).data;
   if(!authority||!Number.isFinite(Date.parse(String(authority.expiresAt)))||authority.schemaVersion!==3||authority.action!=='metadata.publish'||typeof authority.environment!=='string'||!(/^[a-zA-Z0-9_.:-]{1,128}$/).test(authority.environment)||typeof authority.envelopeHeader!=='string'||typeof authority.signatureHeader!=='string'||!(/^[A-Za-z][A-Za-z0-9-]{0,63}$/).test(authority.envelopeHeader)||!(/^[A-Za-z][A-Za-z0-9-]{0,63}$/).test(authority.signatureHeader)||authority.envelopeHeader.toLowerCase()===authority.signatureHeader.toLowerCase()||[authority.envelopeHeader,authority.signatureHeader].some(value=>['authorization','content-type','idempotency-key','x-request-id'].includes(value.toLowerCase()))||Date.parse(String(authority.expiresAt))<=Date.now()||Date.parse(String(authority.expiresAt))>Date.now()+61000)throw new Error('Metadata publication signing authority is invalid.');
   // The shared operation envelope signer (core/operation-envelope.ts) with the headers and environment the publication authority names.
   const signed=await signOperation({actor:attempt.creator,action:'metadata.publish',method:'POST',path,body:attempt.metadataPublicationBody,version:2,environment:authority.environment},{signMessage:message=>this.signMessage(message),assertCurrent:()=>{assertAccount(account);this.chain.scope(scope);}});
   const encoded=signed.encoded,signature=signed.signature;
   const response=await fetch(metadataApiUrl(path),{method:'POST',body:attempt.metadataPublicationBody,headers:{...headers,'Content-Type':'application/json','Idempotency-Key':attempt.metadataOperationId!,'X-Request-ID':crypto.randomUUID(),[authority.envelopeHeader]:encoded,[authority.signatureHeader]:signature},signal:publicationSignal(account.signal),credentials:'omit',redirect:'error'});assertAccount(account);this.chain.scope(scope);
   await this.publicationResponse(response,attempt);return this.refreshPublication(intentId);
  }));
 }
 async approvePurchase(intentId:string) {
  return this.single(async()=>this.storage.exclusively(async()=>{
   const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);if(!attempt||attempt.state!=='resolved'||attempt.purchaseTxHash||attempt.draft.firstBuyRaw==='0')throw new Error('Resolve creation before approving a first purchase.');
   const scope=this.chain.scope();if(scope.creator!==address(attempt.creator))throw new Error('Reconnect the original creator.');
   const tx=await this.chain.approve(attempt.draft.firstBuyRaw,scope);this.set({message:`Purchase approval submitted: ${tx}`});await this.chain.approvalProof(tx,attempt.draft.firstBuyRaw,scope);this.set({quote:undefined,message:'First purchase approval confirmed. Request and review a fresh quote.'});
  }));
 }
 async reviewPurchase(intentId:string,slippageBps:number) {
  return this.single(async()=>{
   const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);if(!attempt||attempt.state!=='resolved'||!attempt.poolAddress||!attempt.createTxHash)throw new Error('Resolve creation before purchasing.');
   if(['submitted','confirmed','signing_locked','outcome_unknown'].includes(attempt.purchaseState))throw new Error('Recover the existing purchase before sending another.');
   const scope=this.chain.scope();if(scope.creator!==address(attempt.creator))throw new Error('The first purchase requires the reviewed creator wallet.');
   const resolution=await this.chain.resolve(attempt.createTxHash,attempt.creator,undefined,attempt.draft);if(resolution.state!=='resolved'||resolution.poolAddress!==attempt.poolAddress)throw new Error('Pool identity changed.');
   if(raw(await this.chain.purchaseAllowance(attempt.creator))<raw(attempt.draft.firstBuyRaw))throw new Error('Approve the separate first purchase amount before requesting its executable quote.');
   this.set({quote:undefined});const epoch=this.epoch;
   const quote=await this.chain.quotePurchase(attempt.poolAddress,attempt.draft.firstBuyRaw,slippageBps,scope);this.chain.scope(scope);if(epoch!==this.epoch)throw new Error('Purchase parameters changed. Review again.');Object.freeze(quote);this.set({attempt,quote,phase:'purchase_review',message:'Review this separate first purchase. Creation is already confirmed.'});return quote;
  });
 }
 async buy(intentId:string,highImpactAcknowledged:boolean,highSlippageAcknowledged:boolean) {
  return this.single(async()=>this.storage.exclusively(async()=>{
   const attempt=(await this.storage.history()).find(row=>row.intentId===intentId),quote=this.state.quote;
   if(!attempt||!quote||quote.pool!==attempt.poolAddress||quote.amountInRaw!==attempt.draft.firstBuyRaw||attempt.purchaseTxHash||['signing_locked','outcome_unknown','confirmed'].includes(attempt.purchaseState))throw new Error('A fresh purchase review is required.');
   if((quote.priceImpactBps>=500&&!highImpactAcknowledged)||(quote.slippageBps>=500&&!highSlippageAcknowledged))throw new Error('Acknowledge high price impact and high slippage separately.');
   const scope=this.chain.scope();if(scope.creator!==address(attempt.creator)||scope.epoch!==quote.accountEpoch)throw new Error('Purchase creator or wallet epoch changed.');
   if(!attempt.createTxHash)throw new Error('Exact creation hash is required before purchase.');
   const resolved=await this.chain.resolve(attempt.createTxHash,attempt.creator,undefined,attempt.draft);this.chain.scope(scope);if(this.state.quote!==quote||resolved.state!=='resolved'||resolved.poolAddress!==attempt.poolAddress||resolved.tokenAddress!==attempt.tokenAddress)throw new Error('Purchase pool identity or review changed.');
   attempt.purchaseState='signing_locked';attempt.purchaseReview=JSON.stringify(quote);await this.storage.put(attempt);
   let tx:string;try{tx=hash(await this.chain.sendPurchase(quote,scope))}catch(error){attempt.purchaseState='outcome_unknown';await this.storage.put(attempt);this.set({attempt,phase:'purchase_failed',message:'Token created. Purchase outcome is unknown; recover it before retrying.'});throw error}
   attempt.purchaseTxHash=tx;attempt.purchaseState='submitted';try{await this.storage.put(attempt)}catch{this.set({attempt,quote:undefined,phase:'purchase_submitted',message:`Token created. Purchase recovery storage failed. Save transaction hash ${tx}; do not buy again.`});throw new Error(`Purchase recovery requires transaction hash ${tx}; do not resubmit.`)}this.set({attempt,quote:undefined,phase:'purchase_submitted',message:`Token created. Purchase submitted: ${tx}`});return attempt;
  }));
 }
 async resumePurchase(intentId:string) {
  return this.single(async()=>{
   const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);if(!attempt?.purchaseTxHash)throw new Error('No saved purchase hash. Inspect wallet history before retrying.');
   const scope=this.chain.scope();if(scope.creator!==address(attempt.creator)||attempt.deploymentRevision!==this.chain.deployment.revision||!attempt.createTxHash)throw new Error('Recover purchase with the original creator and deployment.');
   const resolved=await this.chain.resolve(attempt.createTxHash,attempt.creator,undefined,attempt.draft);if(resolved.state!=='resolved'||resolved.poolAddress!==attempt.poolAddress||resolved.tokenAddress!==attempt.tokenAddress)throw new Error('Purchase recovery creation identity changed.');
   if(!attempt.purchaseReview)throw new Error('Saved purchase review is missing.');const receipt=await this.chain.purchaseReceipt(attempt.purchaseTxHash,JSON.parse(attempt.purchaseReview));this.chain.scope(scope);attempt.purchaseState=receipt.status==='success'?'confirmed':'failed';await this.storage.put(attempt);this.set({attempt,phase:receipt.status==='success'?'purchase_confirmed':'purchase_failed',message:receipt.status==='success'?'Token created. First purchase confirmed.':'Token created. First purchase reverted.'});
  });
 }
 async skipPurchase(intentId:string) {return this.single(async()=>this.storage.exclusively(async()=>{const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);if(!attempt||attempt.state!=='resolved'||attempt.purchaseTxHash||['signing_locked','outcome_unknown'].includes(attempt.purchaseState))throw new Error('Resolve any pending purchase first.');attempt.purchaseState='skipped';await this.storage.put(attempt);this.set({attempt,quote:undefined,phase:'resolved',message:'Token created. First purchase skipped by your choice.'});}));}
 async deliberateNew(intentId:string,recoveryChecked:boolean,consent:boolean) {return this.single(async()=>this.storage.exclusively(async()=>{if(!recoveryChecked||!consent)throw new Error('Check recovery and explicitly confirm a separate launch.');const attempt=(await this.storage.history()).find(row=>row.intentId===intentId);if(!attempt)throw new Error('Unknown attempt.');attempt.deliberateNew=true;await this.storage.put(attempt);this.invalidate('A separate new launch is allowed. Review its new intent before signing.');}));}
}
