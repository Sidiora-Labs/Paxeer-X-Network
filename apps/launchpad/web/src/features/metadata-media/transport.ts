import {metadataApiUrl} from '@/core/sdk-config';
import {fetchMetadataV2,prepareMetadataV2Operation,sendMetadataV2Operation,type MetadataV2Operation} from '@/core/clients/metadata';
import {projectV2Image,type FrozenAsset} from './frozen-adapter';
import type {ImageOutcome} from './editor';
import {revisionValid,approvedOutcome} from './outcome';

export type CreatorImageAuthority={tokenAddress:string;wallet:string;audience:string;revision:string;authenticationHeaders:()=>Promise<Record<string,string>>;signMessage:(message:string)=>Promise<string>};
type PendingUpload={sha256:string;metadata?:MetadataV2Operation;signature?:string};
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
export async function fingerprintCreatorImage(file:File):Promise<string>{
 if(file.size<1||file.size>6291456||!['image/png','image/jpeg','image/webp'].includes(file.type))throw new Error('Choose JPEG, PNG or WebP up to 6 MiB.');
 return Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',await file.arrayBuffer())),b=>b.toString(16).padStart(2,'0')).join('');
}
async function assetResponse(response:Response,token:string):Promise<FrozenAsset>{
 if(response.status===403)throw new Error('Creator authorization was rejected.');
 if(response.status===409)throw new Error('This upload operation belongs to different image bytes.');
 if(response.status===415)throw new Error('Choose a supported JPEG, PNG or WebP image.');
 if(response.status===413||response.status===422)throw new Error('Image was rejected. Check its size, format and dimensions.');
 if(!response.ok)throw new Error('Image service unavailable. Your previous image remains active.');
 if(!response.body)throw new Error('Image service returned an incomplete response.');
 const reader=response.body.getReader();let size=0;const chunks:Uint8Array[]=[];
 try{for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>1048576)throw new Error('Image response exceeded its limit.');chunks.push(value)}}finally{await reader.cancel()}
 const bytes=new Uint8Array(size);let offset=0;for(const part of chunks){bytes.set(part,offset);offset+=part.byteLength}
 const {decodeV2Contract}=await import('@kindle/sdk');
 const envelope=decodeV2Contract('AssetGetResponse',JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(bytes)));
 if(!/^0x[a-f0-9]{40}$/.test(token))throw new Error('Invalid token identity.');
 return envelope.data.asset;
}
function pause(signal:AbortSignal,milliseconds:number):Promise<void>{return new Promise((resolve,reject)=>{if(signal.aborted){reject(new DOMException('Aborted','AbortError'));return}const abort=()=>{clearTimeout(timer);reject(new DOMException('Aborted','AbortError'))};const timer=setTimeout(()=>{signal.removeEventListener('abort',abort);resolve()},milliseconds);signal.addEventListener('abort',abort,{once:true})})}
export function createMetadataImageTransport(authority:CreatorImageAuthority){
 const pending=new Map<string,PendingUpload>();
 const token=authority.tokenAddress.toLowerCase();
 if(!/^0x[a-f0-9]{40}$/.test(token)||!/^0x[a-f0-9]{40}$/.test(authority.wallet.toLowerCase())||!revisionValid(authority.revision))throw new TypeError('Invalid creator identity.');
 return async(file:File,operationId:string,signal:AbortSignal):Promise<ImageOutcome>=>{
  signal=AbortSignal.any([signal,AbortSignal.timeout(75000)]);signal.throwIfAborted();
  if(!uuid.test(operationId)||file.size<1||file.size>6291456||!['image/png','image/jpeg','image/webp'].includes(file.type))throw new Error('Choose JPEG, PNG or WebP up to 6 MiB.');
  const hash=await fingerprintCreatorImage(file);
  let operation=pending.get(operationId);
  if(operation&&operation.sha256!==hash)throw new Error('Select the image again to begin a new operation.');
  if(!operation){operation={sha256:hash};if(pending.size>=32)throw new Error('Too many unfinished image operations. Reload before starting another image.');pending.set(operationId,operation)}
  const headers=await authority.authenticationHeaders();
  const endpoint=metadataApiUrl(`/v2/tokens/${token}/assets/${operationId}`);
  let asset=await assetResponse(await fetch(endpoint,{method:'PUT',body:file,signal,credentials:'include',redirect:'error',headers:{...headers,'Content-Type':file.type,'Idempotency-Key':operationId}}),token);
  if(asset.assetId!==operationId||asset.ownerWallet.toLowerCase()!==authority.wallet.toLowerCase()||asset.sha256.toLowerCase()!=='0x'+hash)throw new Error('Image response did not match this operation.');
  const deadline=Date.now()+60000;
  while(asset.state!=='approved'&&asset.state!=='rejected'&&Date.now()<deadline){await pause(signal,1000);asset=await assetResponse(await fetch(endpoint,{signal,credentials:'include',cache:'no-store',redirect:'error',headers:{...headers,'Idempotency-Key':operationId}}),token);if(asset.assetId!==operationId||asset.ownerWallet.toLowerCase()!==authority.wallet.toLowerCase()||asset.sha256.toLowerCase()!=='0x'+hash)throw new Error('Image status changed identity.');}
  if(asset.state!=='approved')return projectV2Image(asset,operationId,authority.revision,null);
  let metadata=await fetchMetadataV2(token,signal);
  if(metadata?.token.logoAssetId===asset.assetId&&BigInt(metadata.revision)>BigInt(authority.revision)){const outcome=projectV2Image(asset,operationId,authority.revision,metadata);pending.delete(operationId);return outcome;}
  if(!operation.metadata||Number(operation.metadata.expiresAt)<=Math.floor(Date.now()/1000)){const metadataOperation=await prepareMetadataV2Operation(token,authority.wallet,authority.audience,{expectedRevision:authority.revision,logoAssetId:asset.assetId});const signature=await authority.signMessage(metadataOperation.statement);signal.throwIfAborted();operation.metadata=metadataOperation;operation.signature=signature}
  if(!operation.signature)throw new Error('Wallet authorization is required.');
  metadata=await sendMetadataV2Operation(operation.metadata,operation.signature,signal,headers);
  const outcome=projectV2Image(asset,operationId,authority.revision,metadata);
  if(approvedOutcome(outcome,operationId,authority.revision))pending.delete(operationId);
  return outcome;
 };
}
