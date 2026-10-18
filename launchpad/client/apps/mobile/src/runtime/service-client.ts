import {SdkClient, decodeContract, decodeV2Contract, HttpError, type TransportConfig} from '../../../../packages/sdk/src/index.ts';
import {nativeRequestId,protectedCookieFetch} from '../platform/protected-storage.ts';
import type {ContractTypes, ProfilePatchBody, QuoteBody, DeviceRegisterBody, PreferencesUpdateBody} from '../../../../packages/sdk/src/types.gen.ts';
export type NativeWalletConfiguration={projectId:string;walletLink:string;rpcURL:string;iconURL:string};
export type PublicConfiguration = Omit<TransportConfig, 'session'> & {canonicalOrigin:string; identityURI:string; walletConnect?:NativeWalletConfiguration;firebasePush?:{applicationId:string;senderId:string;apiKey:string;projectId:string}; manifestDigest:string; manifestURL:string; optionalFeatures:{chat:boolean;live:boolean};approvedPlaybackOrigins:readonly string[];measurement?:{schemaVersion:1;runId:string;syntheticAccountsOnly:true;topics:readonly string[]}};
export function readConfiguration(input:unknown):PublicConfiguration {
 if (!input || typeof input!=='object') throw new Error('Release service configuration is unavailable');
 const value=input as PublicConfiguration;
 if(value.apiOrigin!==value.canonicalOrigin||value.identityOrigin!==value.canonicalOrigin||value.mediaOrigin!==value.canonicalOrigin)throw new Error('Native authenticated service routes require one approved gateway origin');
 const canonical=new URL(value.canonicalOrigin), manifest=new URL(value.manifestURL), identityURI=new URL(value.identityURI);
 if(identityURI.protocol!=='https:'||identityURI.username||identityURI.password||identityURI.hash||value.identityURI.length>2048)throw new Error('Approved identity URI required');
 if(canonical.protocol!=='https:' || canonical.origin!==value.canonicalOrigin || manifest.origin!==canonical.origin || manifest.username || manifest.password || manifest.search || manifest.hash || !/^[a-f0-9]{64}$/.test(value.manifestDigest)) throw new Error('Invalid release manifest configuration');
 if(typeof value.optionalFeatures?.chat!=='boolean'||typeof value.optionalFeatures.live!=='boolean') throw new Error('Explicit optional feature policy required');
 new SdkClient({...value,production:true});new SdkClient({...value,identityOrigin:identityURI.origin,production:true});
 if(!Array.isArray(value.approvedPlaybackOrigins))throw new Error('Managed playback origin policy required');for(const mediaOrigin of value.approvedPlaybackOrigins)new SdkClient({...value,mediaOrigin,production:true});
 if(value.walletConnect){const wallet=value.walletConnect,link=new URL(wallet.walletLink),rpc=new URL(wallet.rpcURL),icon=new URL(wallet.iconURL);if(!/^[a-f0-9]{32}$/.test(wallet.projectId)||link.protocol!=='https:'||link.username||link.password||link.search||link.hash||rpc.protocol!=='https:'||rpc.username||rpc.password||rpc.search||rpc.hash||icon.protocol!=='https:'||icon.username||icon.password)throw new Error('Invalid approved native wallet configuration');new SdkClient({...value,apiOrigin:rpc.origin,mediaOrigin:link.origin,identityOrigin:icon.origin,production:true});}
 if(value.measurement){const m=value.measurement;if(m.schemaVersion!==1||m.syntheticAccountsOnly!==true||!/^measure_[a-zA-Z0-9_-]{16,96}$/.test(m.runId)||!Array.isArray(m.topics)||m.topics.length!==5||new Set(m.topics).size!==5||m.topics.some(topic=>!topic.startsWith('market.')))throw new Error('Approved synthetic measurement configuration required');decodeContract('Subscribe',{op:'subscribe',subscriptionId:m.runId,topics:[...m.topics],afterCursor:null});}
 return Object.freeze({...value,production:true,measurement:value.measurement?Object.freeze({...value.measurement,topics:Object.freeze([...value.measurement.topics])}):undefined,optionalFeatures:Object.freeze({...value.optionalFeatures}),approvedPlaybackOrigins:Object.freeze([...value.approvedPlaybackOrigins])});
}
export class MobileServices {
 readonly sdk:SdkClient;
 private csrf='';
 constructor(readonly config:PublicConfiguration, _token?:()=>string|undefined) {
  this.sdk=new SdkClient({...config,session:{mode:'cookie',applicationOrigin:config.canonicalOrigin,csrfToken:()=>this.csrf}},async(input,init)=>{const url=new URL(typeof input==='string'?input:input instanceof URL?input.href:input.url);const headers=new Headers(init?.headers);headers.set('Origin',config.canonicalOrigin);return protectedCookieFetch(url.href,{...init,headers});});
 }
 async currentSession(signal:AbortSignal){let response=await protectedCookieFetch(`${this.config.canonicalOrigin}/v2/auth/session`,{signal,headers:{Origin:this.config.canonicalOrigin,Accept:'application/json'}});if(response.status===401){const refreshed=await protectedCookieFetch(`${this.config.canonicalOrigin}/v2/auth/sessions:refresh`,{signal,method:'POST',headers:{Origin:this.config.canonicalOrigin,Accept:'application/json','Content-Type':'application/json'},body:'{}'});if(!refreshed.ok)throw new HttpError(refreshed.status);decodeV2Contract('RefreshResponse',await refreshed.json());response=await protectedCookieFetch(`${this.config.canonicalOrigin}/v2/auth/session`,{signal,headers:{Origin:this.config.canonicalOrigin,Accept:'application/json'}});}if(!response.ok)throw new HttpError(response.status);const body:unknown=await response.json();if(!body||typeof body!=='object'||!('data'in body))throw new Error('Invalid current identity');const principal=decodeCurrentIdentity(body.data);const csrf=response.headers.get('X-CSRF-Token');if(!csrf||!/^[A-Za-z0-9_-]{32,256}$/.test(csrf))throw new Error('Current cookie CSRF proof missing');this.csrf=csrf;return principal;}
 clearIdentity(){this.csrf='';}

 pools(signal:AbortSignal) { return this.sdk.request('/v1/pools?limit=50','PoolListResponse',{signal,idempotentRead:true}); }
 pool(address:string,signal:AbortSignal) { return this.sdk.request(`/v1/pools/${evmAddress(address)}`,'PoolDetailResponse',{signal,idempotentRead:true}); }
 search(query:string,signal:AbortSignal) {return this.sdk.request(`/v1/search?type=pool&limit=50&q=${encodeURIComponent(query)}`,'SearchResponse',{signal,idempotentRead:true});}
 portfolio(signal:AbortSignal) {return this.sdk.request('/v1/me/portfolio?limit=50','PortfolioResponse',{signal,authenticated:true,idempotentRead:true});}
 watchlist(signal:AbortSignal) {return this.sdk.request('/v1/me/watchlist?limit=50','WatchlistGetResponse',{signal,authenticated:true,idempotentRead:true});}
 watch(pool:string,enabled:boolean,signal:AbortSignal) {return this.sdk.request(`/v1/me/watchlist/${evmAddress(pool)}`,'WatchlistPutResponse',{signal,method:'PUT',authenticated:true,body:decodeContract('WatchlistPutBody',{alertEnabled:enabled})});}
 unwatch(pool:string,signal:AbortSignal) {return this.sdk.request(`/v1/me/watchlist/${evmAddress(pool)}`,'WatchlistDeleteResponse',{signal,method:'DELETE',authenticated:true});}
 profile(wallet:string,signal:AbortSignal) {return this.sdk.request(`/v1/profiles/${evmAddress(wallet)}`,'ProfileGetResponse',{signal,idempotentRead:true});}
 editProfile(wallet:string,body:ProfilePatchBody,signal:AbortSignal) {return this.sdk.request(`/v1/profiles/${evmAddress(wallet)}`,'ProfilePatchResponse',{signal,method:'PATCH',authenticated:true,body:decodeContract('ProfilePatchBody',body)});}
 notifications(signal:AbortSignal) {return this.sdk.request('/v1/me/notifications?limit=50','NotificationsResponse',{signal,authenticated:true,idempotentRead:true});}
 readNotification(id:string,signal:AbortSignal) {return this.sdk.request(`/v1/me/notifications/${identifier(id)}/read`,'NotificationReadResponse',{signal,method:'PUT',authenticated:true});}
 preferences(signal:AbortSignal) {return this.sdk.request('/v1/me/preferences','PreferencesReadResponse',{signal,authenticated:true,idempotentRead:true});}
 updatePreferences(body:PreferencesUpdateBody,signal:AbortSignal) {return this.sdk.request('/v1/me/preferences','PreferencesUpdateResponse',{signal,method:'PATCH',authenticated:true,body:decodeContract('PreferencesUpdateBody',body)});}
 deviceAuthorization:((input:{method:'PUT'|'DELETE';path:string;body:string;signal:AbortSignal})=>Promise<{operation:string;signature:string;idempotencyKey:string}>)|undefined;
 private async deviceOperation<N extends 'DeviceRegisterResponse'|'DeviceRevokeResponse'>(id:string,method:'PUT'|'DELETE',response:N,body:DeviceRegisterBody|undefined,signal:AbortSignal):Promise<ContractTypes[N]>{
  const path=`/v1/me/devices/${identifier(id)}`;if(!this.deviceAuthorization)throw new Error('Installation-bound provider proof and wallet-signed device operation authority required');
  const text=body===undefined?'':JSON.stringify(decodeContract('DeviceRegisterBody',body)),authorization=await this.deviceAuthorization({method,path,body:text,signal});
  if(!this.csrf||!/^[0-9a-f-]{36}$/i.test(authorization.idempotencyKey)||!authorization.operation||/[\r\n]/.test(authorization.operation)||!/^0x[0-9a-f]+$/i.test(authorization.signature))throw new Error('Invalid device operation authority');
  const result=await protectedCookieFetch(`${this.config.canonicalOrigin}${path}`,{signal,method,headers:{Origin:this.config.canonicalOrigin,Accept:'application/json','Content-Type':'application/json','X-CSRF-Token':this.csrf,'Idempotency-Key':authorization.idempotencyKey,'X-Kindle-Operation':authorization.operation,'X-Kindle-Signature':authorization.signature},body:text||undefined});
  if(!result.ok)throw new HttpError(result.status);if(result.status===204)return undefined as ContractTypes[N];return decodeContract(response,await result.json());
 }
 device(id:string,body:DeviceRegisterBody,signal:AbortSignal){return this.deviceOperation(id,'PUT','DeviceRegisterResponse',body,signal);}
 revokeDevice(id:string,signal:AbortSignal){return this.deviceOperation(id,'DELETE','DeviceRevokeResponse',undefined,signal);}

 conversations(signal:AbortSignal) {return this.sdk.request('/v1/me/conversations?limit=50','DmListResponse',{signal,authenticated:true,idempotentRead:true});}
 messages(room:string,signal:AbortSignal) {return this.sdk.request(`/v1/rooms/${identifier(room)}/messages?limit=50`,'MessagesListResponse',{signal,authenticated:true,idempotentRead:true});}
 sendMessage(room:string,body:string,clientMessageId:string,signal:AbortSignal) {return this.sdk.request(`/v1/rooms/${identifier(room)}/messages`,'MessageSendResponse',{signal,method:'POST',authenticated:true,body:decodeContract('MessageSendBody',{body,clientMessageId})});}
 streams(signal:AbortSignal) {return this.sdk.request('/v1/streams?status=live&limit=50','StreamListResponse',{signal,idempotentRead:true});}
 quote(body:QuoteBody,signal:AbortSignal) {return this.sdk.request('/v1/quotes','QuoteResponse',{signal,method:'POST',body:decodeContract('QuoteBody',body)});}
 receipt(hash:string,signal:AbortSignal) {if(!/^0x[0-9a-fA-F]{64}$/.test(hash))throw new Error('Invalid transaction hash');return this.sdk.request(`/v1/transactions/${hash}`,'TxStatusResponse',{signal,idempotentRead:true});}
 async challenge(wallet:string,signal:AbortSignal){const requestId=await nativeRequestId();return this.sdk.requestV2('/v2/auth/challenges','ChallengeResponse',{method:'POST',service:'identity',signal,headers:{'Idempotency-Key':requestId},cookieSession:true,body:decodeV2Contract('ChallengeBody',{origin:this.config.canonicalOrigin,wallet:evmAddress(wallet)})});}
 login(challengeId:string,wallet:string,signature:string,proofKind:'eoa_eip191'|'erc1271',deviceLabel:string,signal:AbortSignal){return this.sdk.requestV2('/v2/auth/sessions','LoginResponse',{method:'POST',service:'identity',signal,cookieSession:true,body:decodeV2Contract('LoginBody',{challengeId,deviceLabel,proof:{kind:proofKind,wallet:evmAddress(wallet),signature}})});}
 sessions(signal:AbortSignal) {return this.sdk.requestV2('/v2/auth/sessions?limit=50','SessionsResponse',{signal,authenticated:true,service:'identity',idempotentRead:true});}
 logout(sessionId:string,signal:AbortSignal) {return this.sdk.requestV2Empty(`/v2/auth/sessions/${identifier(sessionId)}`,{signal,method:'DELETE',authenticated:true,service:'identity'});}
 async manifest(signal:AbortSignal) {
  const result=await fetch(this.config.manifestURL,{signal,redirect:'error',credentials:'omit',headers:{Accept:'application/json'}});
  if(!result.ok)throw new HttpError(result.status);
  const value:unknown=await result.json();
  if(!value||typeof value!=='object'||!('schemaVersion'in value)||value.schemaVersion!==1||!('core'in value)||!value.core||typeof value.core!=='object'||!('chainId'in value.core)||value.core.chainId!==125||!('manifestSha256'in value.core)||value.core.manifestSha256!==this.config.manifestDigest)throw new Error('Deployment manifest changed; execution disabled');
  return value;
 }
}
export function evmAddress(value:string){if(!/^0x[0-9a-fA-F]{40}$/.test(value))throw new TypeError('Invalid wallet or pool address');return value;}
export function identifier(value:string){if(!/^[A-Za-z0-9_-]{1,128}$/.test(value))throw new TypeError('Invalid resource identifier');return value;}
export type Resource<N extends keyof ContractTypes> = ContractTypes[N];

export type CurrentIdentity={accountId:string;sessionId:string;wallet:string;authEpoch:number;proofAt:string;accessExpiresAt:string};
export function decodeCurrentIdentity(value:unknown):CurrentIdentity{if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('Current identity missing');const p=value as CurrentIdentity;const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;if(!uuid.test(p.accountId)||!uuid.test(p.sessionId)||!Number.isSafeInteger(p.authEpoch)||p.authEpoch<0||!Number.isFinite(Date.parse(p.proofAt))||Date.parse(p.proofAt)>Date.now()+5000||!Number.isFinite(Date.parse(p.accessExpiresAt))||Date.parse(p.accessExpiresAt)<=Date.now())throw new Error('Invalid current identity authority');evmAddress(p.wallet);return Object.freeze({...p});}
