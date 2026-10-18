// media/livestream serves `/v2/streams/*` at its root. The gateway has no
// `/livestream` mount, so there is no fallback host: without
// NEXT_PUBLIC_LIVESTREAM_API_URL every call answers as not served (501).
import {plainCopy} from '@/core/user-facing-error';
import {LIVESTREAM_SIGNATURE_HEADER,operationHeaders,signOperation,type OperationMethod} from '@/core/operation-envelope';
import {livestreamApiBase} from '@/core/backend-origins';
const API=livestreamApiBase()??'';
export type StreamViewerQuality="verified"|"unavailable"|"stale";
export type StreamStatus='created'|'starting'|'live'|'ending'|'ended'|'failed'|'suspended';
export interface StreamPublic{id:string;poolAddress:string;creatorWallet:string;title:string;playbackUrl:string|null;playbackId:string|null;thumbnailUrl:string|null;isLive:boolean;viewerCount:number|null;startedAt:number|null;endedAt:number|null;createdAt:number|null;revision?:string;generatedAt?:string;viewerQuality?:StreamViewerQuality;status?:StreamStatus}
export interface StreamDirectoryEntry extends StreamPublic{revision:string;generatedAt:string;viewerQuality:StreamViewerQuality;status:StreamStatus}
export interface CreateStreamResponse{id:string;streamKey:string;rtmpUrl:string;playbackUrl:string;playbackId:string}
interface AuthBody{wallet:string;signature:string;message:string;nonce?:string;expiresAt?:string}
const address=/^0x[0-9a-fA-F]{40}$/;
const uuid=/^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
/**
 * A failed livestream request. `code` is the top-level `code` of the error
 * body: UPPER_SNAKE from media/livestream (relayed verbatim for 4xx, kept as the
 * envelope code by the gateway for upstream 5xx) or lower_snake from the
 * gateway itself (`upstream_unavailable` for a dead upstream); null when the
 * body carried none.
 */
export class LivestreamApiError extends Error {
 constructor(readonly status:number,readonly retryable:boolean|null=null,readonly retryAfterMs:number|null=null,readonly code:string|null=null){super(livestreamErrorMessage(status,code))}
}
const codeShape=/^(?:[A-Z][A-Z0-9_]{0,63}|[a-z][a-z0-9_]{0,63})$/;
/** Plain wording for a livestream failure: the code decides first, the status only when the code is absent or unknown. */
export function livestreamErrorMessage(status:number,code:string|null):string{
 switch(code){
  case 'REVISION_CONFLICT':return 'This stream just changed, refresh and try again';
  case 'BROADCAST_NOT_PROVISIONED':return plainCopy.broadcastNotProvisioned;
  case 'STATE_CONFLICT':return plainCopy.stateConflict;
  case 'AUTH_EXPIRED':return plainCopy.sessionExpired;
  case 'upstream_unavailable':return plainCopy.upstreamUnavailable;
  case 'FIELD_INVALID':return plainCopy.invalid;
 }
 return status===409?plainCopy.conflict:status===401?plainCopy.sessionExpired:status===403?"This is not allowed on this stream":status===429?"Too many tries, wait a moment and try again":status===501?"Live streams are not available yet":"Live is not loading right now, try again";
}
/** Retry-After as delta-seconds, in milliseconds; null when absent, an HTTP date, or not exposed to this origin. */
function retryAfterHeaderMs(res:Response):number|null{const raw=res.headers.get('Retry-After')?.trim();return raw&&/^\d{1,6}$/.test(raw)?Number(raw)*1000:null}
async function failure(res:Response):Promise<LivestreamApiError>{
 const body:unknown=await res.json().catch(()=>null);
 if(!body||typeof body!=='object'||Array.isArray(body))return new LivestreamApiError(res.status,null,retryAfterHeaderMs(res));
 const row=body as {code?:unknown;retryable?:unknown;retryAfterMs?:unknown};
 const retryAfterMs=typeof row.retryAfterMs==='number'&&Number.isFinite(row.retryAfterMs)&&row.retryAfterMs>=0?row.retryAfterMs:retryAfterHeaderMs(res);
 return new LivestreamApiError(res.status,typeof row.retryable==='boolean'?row.retryable:null,retryAfterMs,typeof row.code==='string'&&codeShape.test(row.code)?row.code:null);
}
async function read(path:string,options:RequestInit={}):Promise<unknown>{
 if(!API)throw new LivestreamApiError(501);
 const controller=new AbortController();const timeout=setTimeout(()=>controller.abort(),10000);
 try{const res=await fetch(API+path,{...options,signal:controller.signal,credentials:'omit',cache:'no-store'});if(!res.ok)throw await failure(res);
 if(!res.body)throw new Error('Livestream response unavailable');const reader=res.body.getReader();let size=0;const chunks:Uint8Array[]=[];
 try{for(;;){const next=await reader.read();if(next.done)break;size+=next.value.byteLength;if(size>1048576){await reader.cancel();throw new Error('Livestream response exceeds limit')};chunks.push(next.value)}}finally{reader.releaseLock()}
 const bytes=new Uint8Array(size);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length};const contentType=res.headers.get('Content-Type')?.split(';')[0].trim().toLowerCase();if(contentType!=='application/json')throw new Error('Invalid livestream response content type');return JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(bytes));
 }finally{clearTimeout(timeout)}
}
export function approvedPlaybackURL(raw:string):URL {
 const u=new URL(raw);if(raw.length>2048||u.protocol!=='https:'||u.hostname!=='livepeercdn.studio'||u.port||u.username||u.password||u.search||u.hash||!/^\/hls\/[A-Za-z0-9_-]{1,128}\/index\.m3u8$/.test(u.pathname))throw new Error('Unapproved playback CDN');return u;
}
function thumbnailURL(raw:unknown):string|null{
 if(raw===undefined||raw===null)return null;if(typeof raw!=='string'||!raw||raw.length>2048)throw new Error('Invalid stream thumbnail');
 let u:URL;try{u=new URL(raw)}catch{throw new Error('Invalid stream thumbnail')}
 if(u.protocol!=='https:'||!u.hostname||u.username||u.password||u.hash||u.pathname==='/'&&!u.search)throw new Error('Invalid stream thumbnail');return raw;
}
const streamStates:readonly StreamStatus[]=['created','starting','live','ending','ended','failed','suspended'];
export function parseStream(raw:unknown):StreamPublic{
 if(!raw||typeof raw!=='object')throw new Error('Invalid livestream response');const v=raw as Record<string,unknown>;
 if ('streamId' in v) {
 const states:readonly string[]=streamStates;
 if(typeof v.revision!=='string'||!/^([1-9][0-9]{0,18})$/.test(v.revision)||BigInt(v.revision)>9223372036854775807n||typeof v.generatedAt!=='string'||!Number.isFinite(Date.parse(v.generatedAt))||typeof v.streamId!=='string'||!uuid.test(v.streamId)||typeof v.poolAddress!=='string'||!address.test(v.poolAddress)||typeof v.creatorWallet!=='string'||!address.test(v.creatorWallet)||typeof v.title!=='string'||[...v.title].length<1||[...v.title].length>120||typeof v.status!=='string'||!states.includes(v.status)||!['verified','unavailable','stale'].includes(String(v.viewerQuality))||(v.viewerCount!==null&&(!Number.isSafeInteger(v.viewerCount)||Number(v.viewerCount)<0))||(v.viewerQuality==='unavailable')!==(v.viewerCount===null))throw new Error('Invalid livestream response');
 if(v.status==='live') {if(typeof v.playbackUrl!=='string')throw new Error('Invalid playback URL');approvedPlaybackURL(v.playbackUrl)}
 else if(v.playbackUrl!==null)throw new Error('Inactive livestream has playback');
 return {revision:v.revision,generatedAt:v.generatedAt,id:v.streamId,poolAddress:v.poolAddress,creatorWallet:v.creatorWallet,title:v.title,status:v.status as StreamStatus,playbackUrl:v.playbackUrl as string|null,playbackId:null,thumbnailUrl:thumbnailURL(v.thumbnailUrl),isLive:v.status==='live',viewerCount:v.viewerCount as number|null,viewerQuality:v.viewerQuality as StreamViewerQuality,startedAt:null,endedAt:null,createdAt:null};
 }
 const time=(n:unknown):n is number|null=>n===null||(Number.isSafeInteger(n)&&Number(n)>=0);
 if(typeof v.id!=='string'||!uuid.test(v.id)||typeof v.poolAddress!=='string'||!address.test(v.poolAddress)||typeof v.creatorWallet!=='string'||!address.test(v.creatorWallet)||typeof v.title!=='string'||[...v.title].length>120||typeof v.isLive!=='boolean'||!Number.isSafeInteger(v.viewerCount)||Number(v.viewerCount)<0||!time(v.startedAt)||!time(v.endedAt)||!time(v.createdAt)||v.createdAt===null||typeof v.playbackId!=='string'||v.playbackId.length>128||typeof v.playbackUrl!=='string'||v.playbackUrl.length>2048)throw new Error('Invalid livestream response');
 if(v.playbackUrl)approvedPlaybackURL(v.playbackUrl);
 // Explicit public projection prevents an unexpected upstream ingest secret from
 // being retained in client state or telemetry.
 return {id:v.id,poolAddress:v.poolAddress,creatorWallet:v.creatorWallet,title:v.title,playbackUrl:v.playbackUrl,playbackId:v.playbackId,thumbnailUrl:thumbnailURL(v.thumbnailUrl),isLive:v.isLive,viewerCount:Number(v.viewerCount),startedAt:v.startedAt,endedAt:v.endedAt,createdAt:v.createdAt};
}
export function isDirectoryEntry(stream:StreamPublic):stream is StreamDirectoryEntry{return typeof stream.revision==='string'&&typeof stream.generatedAt==='string'&&typeof stream.viewerQuality==='string'&&typeof stream.status==='string'}
function parseList(value:unknown):StreamPublic[]{if(!value||typeof value!=='object')throw new Error('Invalid livestream list');const raw=(value as {streams?:unknown}).streams;if(raw===null)return [];if(!Array.isArray(raw))throw new Error('Invalid livestream list');const rows=raw;if(rows.length>100)throw new Error('Livestream list exceeds limit');return rows.map(parseStream)}
export async function getLiveStreams():Promise<StreamPublic[]>{return parseList(await read('/v2/streams/live'))}
export async function getPoolStreams(poolAddress:string,liveOnly=false):Promise<StreamPublic[]>{if(!address.test(poolAddress))throw new Error('Invalid pool address');return parseList(await read(`/v2/streams/pool/${poolAddress}${liveOnly?'?live=true':''}`))}
export async function getStream(streamId:string):Promise<StreamPublic|null>{if(!uuid.test(streamId))throw new Error('Invalid stream identity');return parseStream(await read(`/v2/streams/${streamId}`))}
// Kept as source-compatible signatures for legacy callers. Arbitrary reusable
// signed messages cannot authorize provider allocation or lifecycle writes.
export async function createStream(_poolAddress:string,_title:string,_auth:AuthBody):Promise<CreateStreamResponse|null>{throw new Error('Verified livestream action authorization unavailable')}
export async function goLive(_streamId:string,_auth:AuthBody):Promise<boolean>{throw new Error('Verified livestream action authorization unavailable')}
export async function endStream(_streamId:string,_auth:AuthBody):Promise<boolean>{throw new Error('Verified livestream action authorization unavailable')}

export interface StreamOperation {operationId:string;streamId:string;status:'requested'|'provisioning'|'ready'|'live'|'ending'|'ended'|'failed'|'suspended';revision:string;retryAt:string|null}
export interface IngestCredential {providerId:string;ingestEndpoint:string;token:string;expiresAt:string}
function operation(value:unknown):StreamOperation {
 if(!value||typeof value!=='object')throw new Error('Invalid stream operation');const o=(value as {operation?:unknown}).operation;
 if(!o||typeof o!=='object')throw new Error('Invalid stream operation');const v=o as Record<string,unknown>;
 if(typeof v.operationId!=='string'||!uuid.test(v.operationId)||v.streamId!==v.operationId||!['requested','provisioning','ready','live','ending','ended','failed','suspended'].includes(String(v.status))||typeof v.revision!=='string'||!/^([1-9][0-9]{0,18})$/.test(v.revision)||BigInt(v.revision)>9223372036854775807n||(v.retryAt!==null&&(typeof v.retryAt!=='string'||!Number.isFinite(Date.parse(v.retryAt)))))throw new Error('Invalid stream operation');
 return {operationId:v.operationId,streamId:v.streamId as string,status:v.status as StreamOperation['status'],revision:v.revision,retryAt:v.retryAt as string|null};
}
function sessionHeaders(accessToken:string):Record<string,string> {if(!accessToken||accessToken.length>16384||/[\r\n]/.test(accessToken))throw new Error('Verified livestream authorization required');return {'Authorization':`Bearer ${accessToken}`,'Content-Type':'application/json'}}
export async function requestStream(poolAddress:string,title:string,idempotencyKey:string,accessToken:string,signing?:StreamSigningContext):Promise<StreamOperation> {
 if(!address.test(poolAddress)||![...title].length||[...title].length>120||!/^[A-Za-z0-9_-]{1,128}$/.test(idempotencyKey))throw new Error('Invalid livestream request');
 const body=JSON.stringify({poolAddress,title});return operation(await read('/v2/streams',{method:'POST',headers:{...sessionHeaders(accessToken),...await actionHeaders(signing,'stream.create','POST','/v2/streams',body),'Idempotency-Key':idempotencyKey},body}));
}
export async function getStreamOperation(operationId:string,accessToken:string):Promise<StreamOperation> {if(!uuid.test(operationId))throw new Error('Invalid stream identity');return operation(await read(`/v2/streams/operations/${operationId}`,{headers:sessionHeaders(accessToken)}))}
export async function controlStream(streamId:string,revision:string,action:'start'|'end'|'rotate'|'restore',accessToken:string,signing?:StreamSigningContext):Promise<StreamOperation> {
 if(!uuid.test(streamId)||!/^([1-9][0-9]{0,18})$/.test(revision))throw new Error('Invalid stream revision');const path=`/v2/streams/${streamId}/${action==='start'?'go-live':action==='rotate'?'rotate-key':action}`;const body=JSON.stringify({revision});return operation(await read(path,{method:'POST',headers:{...sessionHeaders(accessToken),...await actionHeaders(signing,`stream.${action}`,'POST',path,body)},body}));
}
export async function getIngestCredential(streamId:string,accessToken:string,signing?:StreamSigningContext):Promise<IngestCredential> {
 if(!uuid.test(streamId))throw new Error('Invalid stream identity');const path=`/v2/streams/${streamId}/ingest-credential`;const raw=await read(path,{method:'POST',headers:{...sessionHeaders(accessToken),...await actionHeaders(signing,'stream.credential','POST',path,'{}')},body:'{}'});
 if(!raw||typeof raw!=='object')throw new Error('Credential unavailable');const result=raw as {streamId?:unknown;ingestCredential?:unknown};if(result.streamId!==streamId||!result.ingestCredential||typeof result.ingestCredential!=='object')throw new Error('Credential unavailable');const c=result.ingestCredential as Record<string,unknown>;
 if(typeof c.providerId!=='string'||!/^[A-Za-z0-9_-]{1,80}$/.test(c.providerId)||typeof c.token!=='string'||!c.token||c.token.length>4096||typeof c.expiresAt!=='string'||Date.parse(c.expiresAt)<=Date.now()||!Number.isFinite(Date.parse(c.expiresAt))||typeof c.ingestEndpoint!=='string'||c.ingestEndpoint.length>2048)throw new Error('Credential unavailable');const u=new URL(c.ingestEndpoint);if(u.protocol!=='https:'||u.username||u.password)throw new Error('Credential unavailable');return {providerId:c.providerId,token:c.token,expiresAt:c.expiresAt,ingestEndpoint:c.ingestEndpoint};
}

export interface StreamSigningContext {wallet:string;environment:string;signMessage:(message:string)=>Promise<string>;assertCurrent:()=>void}
/** media/livestream lifecycle writes: the shared operation envelope (v1), signature in X-Kindle-Signature (connected_routes.go). */
async function actionHeaders(context:StreamSigningContext|undefined,action:string,method:OperationMethod,path:string,body:string):Promise<Record<string,string>> {
 if(!context||!address.test(context.wallet)||!context.environment||!/^[a-zA-Z0-9_.:-]{1,128}$/.test(context.environment))throw new Error('Verified stream signing authority unavailable');context.assertCurrent();
 const signed=await signOperation({actor:context.wallet,action,method,path,body,version:1,environment:context.environment},{signMessage:context.signMessage,assertCurrent:context.assertCurrent});
 return operationHeaders(signed,LIVESTREAM_SIGNATURE_HEADER);
}

export async function editStream(streamId:string,revision:string,title:string,accessToken:string,signing:StreamSigningContext,privatePlayback?:boolean):Promise<StreamOperation>{
 if(!uuid.test(streamId)||![...title].length||[...title].length>120||!/^([1-9][0-9]{0,18})$/.test(revision))throw new Error('Invalid stream edit');const path=`/v2/streams/${streamId}`;const body=JSON.stringify({title,revision,...(privatePlayback===undefined?{}:{private:privatePlayback})});return operation(await read(path,{method:'PATCH',headers:{...sessionHeaders(accessToken),...await actionHeaders(signing,'stream.edit','PATCH',path,body)},body}));
}
export interface ViewerSession{streamId:string;sessionId:string;token:string;expiresAt:string}
export async function createViewerSession(streamId:string,accessToken?:string):Promise<ViewerSession>{
 if(!uuid.test(streamId))throw new Error('Invalid stream identity');const value=await read(`/v2/streams/${streamId}/viewer-session`,{method:'POST',headers:accessToken?sessionHeaders(accessToken):{'Content-Type':'application/json'},body:'{}'});if(!value||typeof value!=='object')throw new Error('Viewer session unavailable');const v=value as Record<string,unknown>;if(v.streamId!==streamId||typeof v.sessionId!=='string'||!uuid.test(v.sessionId)||typeof v.token!=='string'||v.token.length>512||typeof v.expiresAt!=='string'||!Number.isFinite(Date.parse(v.expiresAt))||Date.parse(v.expiresAt)<=Date.now())throw new Error('Viewer session unavailable');return {streamId,sessionId:v.sessionId,token:v.token,expiresAt:v.expiresAt};
}
export async function heartbeatViewer(session:ViewerSession):Promise<void>{if(!uuid.test(session.streamId)||!uuid.test(session.sessionId)||!session.token||session.token.length>512)throw new Error('Invalid viewer session');await read(`/v2/streams/${session.streamId}/heartbeat`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({sessionId:session.sessionId,token:session.token})})}
export type LivePageSort='viewers'|'mcap';
export interface LivePageOptions{sort?:LivePageSort;limit?:number}
export async function getLivePage(cursor?:string,options:LivePageOptions={}):Promise<{streams:StreamDirectoryEntry[];nextCursor:string|null}>{const query=new URLSearchParams();if(options.sort){if(options.sort!=='viewers'&&options.sort!=='mcap')throw new Error('Invalid stream sort');query.set('sort',options.sort);}if(options.limit!==undefined){if(!Number.isInteger(options.limit)||options.limit<1||options.limit>100)throw new Error('Invalid stream page size');query.set('limit',String(options.limit));}if(cursor)query.set('cursor',cursor);const search=query.toString();const raw=await read('/v2/streams/live'+(search?'?'+search:''));const streams=parseList(raw);if(!streams.every(isDirectoryEntry))throw new Error('Invalid livestream directory row');const c=(raw as {nextCursor?:unknown}).nextCursor;if(c!==null&&(typeof c!=='string'||c.length>1024))throw new Error('Invalid stream cursor');return {streams,nextCursor:c};}

export interface PrivateIngestKey{streamKey:string;ingestEndpoint:string;visibilityUntil:string}
export async function getPrivateIngestKey(streamId:string,accessToken:string,signing:StreamSigningContext):Promise<PrivateIngestKey>{
 if(!uuid.test(streamId))throw new Error('Invalid stream identity');const path=`/v2/streams/${streamId}/ingest-key`;const raw=await read(path,{method:'POST',headers:{...sessionHeaders(accessToken),...await actionHeaders(signing,'stream.credential','POST',path,'{}')},body:'{}'});if(!raw||typeof raw!=='object')throw new Error('Ingest key unavailable');const value=raw as {streamId?:unknown;ingestKey?:unknown};if(value.streamId!==streamId||!value.ingestKey||typeof value.ingestKey!=='object')throw new Error('Ingest key unavailable');const v=value.ingestKey as Record<string,unknown>;if(typeof v.streamKey!=='string'||!/^[A-Za-z0-9_-]{1,128}$/.test(v.streamKey)||typeof v.ingestEndpoint!=='string'||typeof v.visibilityUntil!=='string'||!Number.isFinite(Date.parse(v.visibilityUntil))||Date.parse(v.visibilityUntil)<=Date.now())throw new Error('Ingest key unavailable');const endpoint=new URL(v.ingestEndpoint);if(!['https:','rtmps:'].includes(endpoint.protocol)||endpoint.username||endpoint.password||endpoint.search||endpoint.hash)throw new Error('Invalid ingest endpoint');return {streamKey:v.streamKey,ingestEndpoint:v.ingestEndpoint,visibilityUntil:v.visibilityUntil};
}

export interface KeyRotation{oldStreamId:string;newStreamId:string|null;status:'revoking'|'provisioning'|'ready';revision:string}
export async function getKeyRotation(streamId:string,accessToken:string):Promise<KeyRotation>{if(!uuid.test(streamId))throw new Error('Invalid stream identity');const raw=await read(`/v2/streams/${streamId}/key-rotation`,{headers:sessionHeaders(accessToken)});if(!raw||typeof raw!=='object')throw new Error('Key rotation unavailable');const v=raw as Record<string,unknown>;if(v.oldStreamId!==streamId||(v.newStreamId!==null&&(typeof v.newStreamId!=='string'||!uuid.test(v.newStreamId)))||!['revoking','provisioning','ready'].includes(String(v.status))||typeof v.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(v.revision))throw new Error('Key rotation unavailable');return {oldStreamId:streamId,newStreamId:v.newStreamId as string|null,status:v.status as KeyRotation['status'],revision:v.revision};}

export async function getPrivatePlayback(streamId:string,accessToken:string):Promise<StreamPublic>{if(!uuid.test(streamId))throw new Error('Invalid stream identity');return parseStream(await read(`/v2/streams/${streamId}/private-playback`,{headers:sessionHeaders(accessToken)}));}
