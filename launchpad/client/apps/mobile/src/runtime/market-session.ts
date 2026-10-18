import {SnapshotStream,browserStreamTransport,type StreamState,type StreamEvent,type TopicSnapshot} from '../../../../packages/sdk/src/index.ts';
import {retryDelay} from '../contracts.ts';
import type {MobileServices} from './service-client.ts';
export class MarketSession {
 private stream:SnapshotStream;private topics:string[]=[];private timer:ReturnType<typeof setTimeout>|undefined;private attempts=0;private active=false;private revision=0;
 constructor(services:MobileServices,callbacks:{snapshot(value:TopicSnapshot):void;event(value:StreamEvent):void;state(state:StreamState):void}){this.stream=new SnapshotStream({transport:browserStreamTransport(services.sdk),installSnapshot:callbacks.snapshot,applyEvent:callbacks.event,maxBuffer:1000,onState:state=>{callbacks.state(state);if(state==='live')this.attempts=0;if(this.active&&['stale','resync-required'].includes(state))this.retry();}});}
 async foreground(topics:string[]){this.background();this.active=true;const revision=this.revision;this.topics=[...new Set(topics)];try{await this.stream.start(this.topics);}catch{if(this.active&&revision===this.revision)this.retry();}}
 private retry(){if(this.timer||!this.active)return;this.timer=setTimeout(()=>{this.timer=undefined;if(this.active){const revision=this.revision;void this.stream.start(this.topics).catch(()=>{if(this.active&&revision===this.revision)this.retry();});}},retryDelay(this.attempts++,Math.random()));}
 background(){this.revision++;this.active=false;if(this.timer)clearTimeout(this.timer);this.timer=undefined;this.stream.close();}
}
