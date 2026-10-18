import {NativeModules} from 'react-native';
import type {CredentialStore} from '../runtime/session.ts';
interface SecureModule {get(key:string):Promise<string|null>;set(key:string,value:string):Promise<void>;remove(key:string):Promise<void>;clear():Promise<void>;unlock():Promise<boolean>;configuration():Promise<unknown>;pushPermission():Promise<'granted'|'denied'|'not-determined'>;requestPushPermission():Promise<'granted'|'denied'>;nativePushRegistration():Promise<{installationId:string;token:string;platform:'ios'|'android';provider:'apns'|'fcm'}>;cookieRequest(url:string,method:string,headers:Record<string,string>,body:string|null):Promise<{status:number;headers:Record<string,string>;body:string}>;cancelCookieRequests():Promise<void>;requestId():Promise<string>;readPublicSnapshot(key:string):Promise<string|null>;writePublicSnapshot(key:string,value:string):Promise<void>;clearPublicSnapshots():Promise<void>;}
function module():SecureModule {const value=NativeModules.SecureSession as SecureModule|undefined;if(!value||typeof value.get!=='function'||typeof value.set!=='function'||typeof value.clear!=='function')throw new Error('Platform protected storage is unavailable');return value;}
export const protectedStorage:CredentialStore={get:key=>module().get(key),set:(key,value)=>module().set(key,value),remove:key=>module().remove(key),clear:()=>module().clear()};
export function releaseConfiguration(){return module().configuration();}
export async function localBiometricUnlock(){const allowed=await module().unlock();if(!allowed)throw new Error('Local unlock denied');return allowed;}

export const publicSnapshotStorage={read:(key:string)=>module().readPublicSnapshot(key),write:(key:string,value:string)=>module().writePublicSnapshot(key,value),clear:()=>module().clearPublicSnapshots()};

export function nativeRequestId(){return module().requestId();}

export class WalletProtocolStorage {
 private values:Record<string,unknown>|undefined;private queue:Promise<void>=Promise.resolve();
 private async load(){if(!this.values){const saved=await module().get('walletconnect');const value:unknown=saved?JSON.parse(saved):{};if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('Invalid protected wallet protocol storage');this.values=Object.assign(Object.create(null),value);}return this.values;}
 async getKeys(){await this.queue;return Object.keys(await this.load());}
 async getEntries<T=unknown>():Promise<[string,T][]> {await this.queue;return Object.entries(await this.load()) as [string,T][];}
 async getItem<T=unknown>(key:string):Promise<T|undefined>{await this.queue;return (await this.load())[key] as T|undefined;}
 private mutate(change:(values:Record<string,unknown>)=>void){const operation=this.queue.then(async()=>{const values=Object.assign(Object.create(null),await this.load()) as Record<string,unknown>;change(values);await module().set('walletconnect',JSON.stringify(values));this.values=values;});this.queue=operation.catch(()=>{});return operation;}
 setItem<T=unknown>(key:string,value:T){return this.mutate(values=>{values[key]=value;});}
 removeItem(key:string){return this.mutate(values=>{delete values[key];});}
 async clear(){await this.queue;await module().remove('walletconnect');this.values=undefined;}
}

export async function protectedCookieFetch(url:string,init:RequestInit={}):Promise<Response>{
 if(init.signal?.aborted)throw new Error('Request cancelled');
 const headers=Object.fromEntries(new Headers(init.headers).entries());
 const cancel=()=>{void module().cancelCookieRequests();};init.signal?.addEventListener('abort',cancel,{once:true});
 try{const result=await module().cookieRequest(url,init.method??'GET',headers,typeof init.body==='string'?init.body:null);init.signal?.throwIfAborted();return new Response(result.status===204?null:result.body,{status:result.status,headers:result.headers});}finally{init.signal?.removeEventListener('abort',cancel);}
}

export const nativePush={permission:()=>module().pushPermission(),requestPermission:()=>module().requestPushPermission(),registration:()=>module().nativePushRegistration()};
