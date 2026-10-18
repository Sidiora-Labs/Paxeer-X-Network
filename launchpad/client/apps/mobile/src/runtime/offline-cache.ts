import {decodeContract} from '../../../../packages/sdk/src/validate.ts';
export type Snapshot<T>={value:T;storedAt:number;expiresAt:number;account:string|null;bytes:number};
export interface PublicSnapshotStorage {read(key:string):Promise<string|null>;write(key:string,value:string):Promise<void>;clear():Promise<void>;}
function byteLength(value:string){return [...value].reduce((total,char)=>{const point=char.codePointAt(0)!;return total+(point<=0x7f?1:point<=0x7ff?2:point<=0xffff?3:4);},0);}
export function approvedPublicKey(key:string){return key==='pools'||key==='live'||/^pool:0x[0-9a-fA-F]{40}$/.test(key)||/^profile:0x[0-9a-fA-F]{40}$/.test(key)||key.startsWith('search:')&&key.length<=256;}
export function validatePublicSnapshot(key:string,value:unknown){if(!approvedPublicKey(key))throw new Error('Unapproved public snapshot');const contract=key==='pools'?'PoolListResponse':key==='live'?'StreamListResponse':key.startsWith('pool:')?'PoolDetailResponse':key.startsWith('profile:')?'ProfileGetResponse':'SearchResponse';return decodeContract(contract,value);}
export class SnapshotCache {
 private entries=new Map<string,Snapshot<unknown>>();private persistence:Promise<void>=Promise.resolve();persistenceError:string|undefined;
 constructor(readonly publicCap=100*1024*1024,readonly privateCap=20*1024*1024,readonly storage?:PublicSnapshotStorage){if(!Number.isSafeInteger(publicCap)||publicCap<=0||!Number.isSafeInteger(privateCap)||privateCap<=0)throw new RangeError('Invalid cache cap');}
 put<T>(key:string,value:T,account:string|null,expiresAt:number,now=Date.now(),persist=true):void {
  if(!Number.isSafeInteger(now)||now<0||!Number.isSafeInteger(expiresAt)||expiresAt<=now)throw new Error('Snapshot expiry required');
  const bytes=byteLength(JSON.stringify(value)),cap=account===null?this.publicCap:this.privateCap;
  if(bytes>cap)throw new Error('Snapshot exceeds cache budget');
  const copy=JSON.parse(JSON.stringify(value)) as T;
  this.entries.delete(key);const snapshot={value:copy,storedAt:now,expiresAt,account,bytes};this.entries.set(key,snapshot);
  const used=()=>[...this.entries.values()].filter(v=>(v.account===null)===(account===null)).reduce((n,v)=>n+v.bytes,0);
  for(const [entry,item]of this.entries){if(used()<=cap)break;if((item.account===null)===(account===null))this.entries.delete(entry);}
  if(persist&&account===null&&approvedPublicKey(key)&&this.storage){validatePublicSnapshot(key,snapshot.value);const encoded=JSON.stringify(snapshot);this.persistence=this.persistence.then(()=>this.storage!.write(key,encoded)).catch(()=>{this.persistenceError='Public offline snapshot persistence is unavailable';});}
 }
 get<T>(key:string,account:string|null,now=Date.now()):Snapshot<T>|undefined {
  const found=this.entries.get(key);if(!found||found.account!==account)return;
  if(account!==null&&found.expiresAt<=now){this.entries.delete(key);return;}
  this.entries.delete(key);this.entries.set(key,found);
  return {...found,value:JSON.parse(JSON.stringify(found.value)) as T};
 }
 async read<T>(key:string,account:string|null,now=Date.now()):Promise<Snapshot<T>|undefined>{
  const memory=this.get<T>(key,account,now);if(memory)return memory;
  if(account!==null||!approvedPublicKey(key)||!this.storage)return;
  const saved=await this.storage.read(key);if(!saved)return;
  const candidate:unknown=JSON.parse(saved);if(!candidate||typeof candidate!=='object')throw new Error('Invalid public snapshot');
  const snapshot=candidate as Snapshot<unknown>;
  if(Object.keys(snapshot).some(k=>!['value','storedAt','expiresAt','account','bytes'].includes(k))||snapshot.account!==null||!Number.isSafeInteger(snapshot.storedAt)||snapshot.storedAt<0||!Number.isSafeInteger(snapshot.expiresAt)||snapshot.expiresAt<=snapshot.storedAt||!Number.isSafeInteger(snapshot.bytes)||snapshot.bytes!==byteLength(JSON.stringify(snapshot.value))||snapshot.bytes>this.publicCap)throw new Error('Invalid public snapshot inventory');
  validatePublicSnapshot(key,snapshot.value);this.put(key,snapshot.value,null,snapshot.expiresAt,snapshot.storedAt,false);const retained=this.entries.get(key)!;return {...retained,value:JSON.parse(JSON.stringify(snapshot.value)) as T};
 }
 purgePrivate(){for(const [key,item]of this.entries)if(item.account!==null)this.entries.delete(key);}
 async clear(){this.entries.clear();await this.persistence;if(this.storage)await this.storage.clear();this.persistenceError=undefined;}
}
