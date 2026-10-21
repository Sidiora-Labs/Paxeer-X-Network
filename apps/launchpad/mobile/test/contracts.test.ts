import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validatePending, consumeWalletReturn, executionAllowed, retryDelay, safePushRoute } from '../src/contracts.ts';
const op = {requestId:'request_1234567890',chainId:125,startedAt:100,returnRoute:'/portfolio',state:'awaiting_wallet'} as const;
const origin = 'https://sidiora.example';
test('process recovery preserves unknown and submitted states; callback never settles', () => {
 const restored = validatePending(JSON.parse(JSON.stringify(op)));
 const result = consumeWalletReturn(origin+'/wallet/return?requestId='+op.requestId, origin, restored, new Set());
 assert.equal(result.operation.state,'unknown');
 assert.throws(() => consumeWalletReturn(origin+'/wallet/return?requestId='+op.requestId,origin,restored,result.consumed));
 const submitted = validatePending({...op,state:'submitted',transactionHash:'0x'+'ab'.repeat(32)});
 assert.equal(consumeWalletReturn(origin+'/wallet/return?requestId='+op.requestId,origin,submitted,new Set()).operation.transactionHash,submitted.transactionHash);
});
test('reject malicious origins, payload secrets, unrelated and duplicate arguments', () => {
 for (const link of ['https://evil.example/wallet/return?requestId='+op.requestId, origin+'/wallet/return?requestId=unrelated',origin+'/wallet/return?requestId='+op.requestId+'&requestId='+op.requestId,origin+'/wallet/return?requestId='+op.requestId+'&jwt=secret',origin+'/wallet/return?requestId='+op.requestId+'#token']) assert.throws(()=>consumeWalletReturn(link,origin,op,new Set()));
 for (const bad of [{...op,chainId:1},{...op,seed:'secret'},{...op,state:'success'},{...op,state:'submitted'},{...op,transactionHash:'0x12'},{...op,startedAt:NaN}]) assert.throws(()=>validatePending(bad));
});
test('foreground gates every freshness input and offline execution',()=> {const good = {online:true,sessionFresh:true,manifestFresh:true,quoteFresh:true,receiptReconciled:true};assert.equal(executionAllowed(good),true);for(const key of Object.keys(good)) assert.equal(executionAllowed({...good,[key]:false}),false);});
test('full jitter bounded at 30 seconds; reject invalid entropy',()=>{assert.equal(retryDelay(0,0),0);assert.equal(retryDelay(100,0.999),29970);for(const x of [-1,1,NaN])assert.throws(()=>retryDelay(1,x));});
test('push rejects stale, private and attacker-controlled routes',()=>{const p={notificationId:'notification_123456',category:'social',expiresAt:200,route:'/notifications'};assert.deepEqual(safePushRoute(p,100),{name:'notifications'});for(const bad of [{...p,body:'private'},{...p,expiresAt:100},{...p,route:'https://evil.example'},{...p,category:'unknown'}])assert.throws(()=>safePushRoute(bad,100));});
test('native routes validate addresses and exclude external redirects',async()=>{const {parseNativeRoute}=await import('../src/contracts.ts');assert.deepEqual(parseNativeRoute('/token/0x'+'ab'.repeat(20)),{name:'token',address:'0x'+'ab'.repeat(20)});for(const p of ['https://evil.example','/token/0x12','/portfolio?jwt=secret','/%2fportfolio'])assert.throws(()=>parseNativeRoute(p));});

test('runtime boundaries fail closed for missing fields, inherited routes and invalid clock',async()=>{
 assert.equal(executionAllowed({} as any),false);
 assert.equal(executionAllowed(null as any),false);
 const good={online:true,sessionFresh:true,manifestFresh:true,quoteFresh:true,receiptReconciled:true};
 assert.equal(executionAllowed(Object.create(good)),false);
 assert.equal(executionAllowed({...good,extra:true} as any),false);
 const {parseNativeRoute}=await import('../src/contracts.ts');
 for(const route of ['toString','constructor','__proto__',null,{},42])assert.throws(()=>parseNativeRoute(route as any));
 const push={notificationId:'notification_123456',category:'social',expiresAt:200,route:'/notifications'};
 for(const now of [NaN,Infinity,-1,0.1])assert.throws(()=>safePushRoute(push,now));
 assert.throws(()=>safePushRoute({...push,expiresAt:200.5},100));
});

test('protected pending journal retains consumed callback after process restart',async()=>{
 const {decodePendingJournal,receivePendingJournal}=await import('../src/contracts.ts');
 const initial=decodePendingJournal({version:2,operation:op,callbackConsumed:false});
 const delivered=receivePendingJournal(origin+'/wallet/return?requestId='+op.requestId,origin,initial);
 const restarted=decodePendingJournal(JSON.parse(JSON.stringify(delivered)));
 assert.equal(restarted.callbackConsumed,true);assert.equal(restarted.operation.state,'unknown');
 assert.throws(()=>receivePendingJournal(origin+'/wallet/return?requestId='+op.requestId,origin,restarted));
 assert.throws(()=>receivePendingJournal(origin+'/wallet/return?requestId=wrong_request_123456',origin,restarted));
 assert.equal(restarted.operation.transactionHash,undefined);
});
test('journal rejects secret fields and corrupt consumed markers without relaxing pending shape',async()=>{
 const {decodePendingJournal}=await import('../src/contracts.ts');
 for(const value of [{version:2,operation:op,callbackConsumed:'true'},{version:3,operation:op,callbackConsumed:false},{version:2,operation:{...op,signature:'0x1234'},callbackConsumed:false},{version:2,operation:op,callbackConsumed:false,token:'private'},[],Object.create(op)])assert.throws(()=>decodePendingJournal(value));
 const legacy=decodePendingJournal({...op,state:'unknown'});assert.equal(legacy.callbackConsumed,true);
 assert.equal(Object.isFrozen(legacy.operation),true);
});
test('callback cannot smuggle credentials or route normalization across verified origin',()=>{
 for(const link of [origin+'/wallet/return?requestId='+op.requestId+'&transactionHash=0x'+('ab'.repeat(32)),origin+'/wallet/return?requestId='+op.requestId+'&returnRoute=//evil.example','https://user:password@sidiora.example/wallet/return?requestId='+op.requestId,origin+'/wallet/return?requestId='+op.requestId+'%00',origin+'/wallet/return?REQUESTID='+op.requestId])assert.throws(()=>consumeWalletReturn(link,origin,op,new Set()));
});

test('normalized path and encoded request aliases are rejected as malformed callback forms',()=>{
 for(const path of ['/ignore/../wallet/return?requestId='+op.requestId,'/wallet/return?requestId=request%5f1234567890','/wallet/return?requestId='+op.requestId+'&'])assert.throws(()=>consumeWalletReturn(origin+path,origin,op,new Set()));
});
