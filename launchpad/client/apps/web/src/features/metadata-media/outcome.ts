import type {ImageOutcome} from './editor';
const max='115792089237316195423570985008687907853269984665640564039457584007913129639935';
export function revisionValid(s:unknown):s is string{return typeof s==='string'&&/^(0|[1-9][0-9]{0,77})$/.test(s)&&(s.length<78||s<=max)}
export function approvedOutcome(r:ImageOutcome,operation:string,previous:string):boolean{
 if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(r.assetId)||r.operationId!==operation||r.state!=='APPROVED'||!revisionValid(r.revision)||!revisionValid(previous)||BigInt(r.revision)<=BigInt(previous)||!r.url)return false;
 try{const u=new URL(r.url);return u.protocol==='https:'&&!u.username&&!u.password&&!u.search&&!u.hash&&/^\/approved\/[a-f0-9]{64}\/(256|512|1024)\/[a-f0-9]{64}\.png$/.test(u.pathname)}catch{return false}
}
