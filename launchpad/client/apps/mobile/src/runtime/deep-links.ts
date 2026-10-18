import {validatePending,decodePendingJournal,receivePendingJournal,type PendingOperation,type PendingJournal,parseNativeRoute} from '../contracts.ts';
import type {CredentialStore} from './session.ts';
export class PendingOperations {
 current:PendingOperation|undefined;private callbackConsumed=false;private queue:Promise<void>=Promise.resolve();
 constructor(readonly storage:CredentialStore,readonly canonicalOrigin:string){}
 private serial<T>(action:()=>Promise<T>):Promise<T>{const result=this.queue.then(action);this.queue=result.then(()=>{},()=>{});return result;}
 private async install(journal:PendingJournal){const valid=decodePendingJournal(journal);await this.storage.set('pending',JSON.stringify(valid));this.current=valid.operation;this.callbackConsumed=valid.callbackConsumed;}
 async restore(){return this.serial(async()=>{const stored=await this.storage.get('pending');if(!stored){this.current=undefined;this.callbackConsumed=false;return;}const journal=decodePendingJournal(JSON.parse(stored));const operation=journal.operation.state==='awaiting_wallet'?validatePending({...journal.operation,state:'unknown'}):journal.operation;await this.install({...journal,operation});});}
 async begin(operation:PendingOperation){const valid=validatePending(operation);return this.serial(async()=>{if(this.current)throw new Error('Reconcile outstanding wallet operation before another submission');if(valid.state!=='awaiting_wallet'||valid.transactionHash)throw new Error('Wallet operation must start before submission');await this.install({version:2,operation:valid,callbackConsumed:false});});}
 async submitted(requestId:string,transactionHash:string){return this.serial(async()=>{if(this.current?.requestId!==requestId)throw new Error('Unrelated wallet result');const operation=validatePending({...this.current,transactionHash,state:'submitted'});await this.install({version:2,operation,callbackConsumed:this.callbackConsumed});});}
 async receive(raw:string){return this.serial(async()=>{if(!this.current)throw new Error('No outstanding wallet request');await this.install(receivePendingJournal(raw,this.canonicalOrigin,{version:2,operation:this.current,callbackConsumed:this.callbackConsumed}));return this.current!;});}
 async markUnknown(){return this.serial(async()=>{if(this.current)await this.install({version:2,operation:validatePending({...this.current,state:this.current.transactionHash?'submitted':'unknown'}),callbackConsumed:this.callbackConsumed});});}
 async clearForAccountSwitch(){return this.serial(async()=>{await this.storage.remove('pending');this.current=undefined;this.callbackConsumed=false;});}
 async clearConfirmed(){return this.clearForAccountSwitch();}
 route(raw:string){if(raw.length>2048)throw new Error('Invalid navigation link');const url=new URL(raw);if(url.protocol!=='https:'||url.origin!==this.canonicalOrigin||url.username||url.password||url.search||url.hash||raw!==url.href)throw new Error('Invalid navigation origin');return parseNativeRoute(url.pathname);}
}
