import {test} from 'node:test';
import assert from 'node:assert/strict';
import * as c from './creation-domain.ts';
const a=n=>'0x'+n.toString(16).padStart(40,'0'), h=n=>'0x'+n.toString(16).padStart(64,'0');
const deployment={revision:'r1',chainId:125,usdl:a(1),router:a(2),factory:a(3)};
const now=1700000000000;
const cost=c.verifyCost(deployment,{revision:'r1',decimals:6,feeRaw:'1000000',gasRaw:'10'});
const draft={draftId:'d1',version:1,creator:a(4),chainId:125,name:'Token',symbol:'TKN',description:'',tags:[],socialLinks:[],uploadRefs:[],firstBuyRaw:'0',opticalSelection:'standard',updatedAt:now};
const optical=c.selectOptical('standard','o1',[]);
const reviewed=c.review('i1',draft,cost,optical,now);
const event={transactionHash:h(1),emitter:a(3),creator:a(4),pool:a(5),token:a(6),logIndex:1,blockHeight:'1',blockHash:h(11)};
const receipt={chainId:125,transactionHash:h(1),status:'success',events:[event],blockHeight:'1',blockHash:h(11)};
const expected={hash:h(1),creator:a(4),factory:a(3)};
const registry=async()=>({token:a(6),creator:a(4),blockHeight:'1',blockHash:h(11)});
// Synthetic domain fixtures only: these objects are not receipt evidence.
test('one USDL fee mismatch/decimals/revision fail closed; PAX independent',()=>{
 for(const extra of [{feeRaw:'2000000'},{decimals:18},{revision:'r2'}]) assert.throws(()=>c.verifyCost(deployment,{revision:'r1',decimals:6,feeRaw:'1000000',gasRaw:'10',...extra}));
 assert.equal(c.funding(cost,deployment,{usdl:'999999',pax:'100',allowance:'0'}).state,'insufficient_usdl');
 assert.equal(c.funding(cost,deployment,{usdl:'1000000',pax:'0',allowance:'0'}).state,'insufficient_gas');
 assert.equal(c.funding(cost,deployment,{usdl:'1000000',pax:'10',allowance:'0'}).approvalTargetRaw,'1000000');
 assert.throws(()=>c.raw('1e6'));assert.throws(()=>c.raw((2n**256n).toString()));
});
test('byte limits, URL policy, expiry and immutable review invalidation',()=>{
 assert.equal(c.validateDraft({...draft,symbol:'🧠🧠🧠'},now).symbol,'invalid_text');
 assert.equal(c.validateDraft({...draft,socialLinks:['javascript:alert(1)']},now).socialLinks,'unsafe_url');
 assert.equal(c.validateDraft(draft,now+31*86400000).updatedAt,'draft_expired');
 assert.throws(()=>c.assertReview(reviewed,{...draft,symbol:'NEW'},cost,optical,now));
 assert.throws(()=>c.assertReview(reviewed,draft,cost,c.selectOptical('standard','o2',[]),now));
 assert.throws(()=>c.selectOptical('unregistered','o1',[]));
});
test('same creator A/B resolves exact hash, ambiguity/registry conflict/revert blocked',async()=>{
 const r=await c.resolveCreation({...receipt,events:[event,{...event,transactionHash:h(2),pool:a(7)}]},expected,registry);
 assert.equal(r.poolAddress,a(5));
 await assert.rejects(c.resolveCreation({...receipt,events:[event,event]},expected,registry));
 await assert.rejects(c.resolveCreation(receipt,expected,async()=>({token:a(8),creator:a(4)})));
 await assert.rejects(c.resolveCreation({...receipt,status:'reverted'},expected,registry));
 assert.equal((await c.resolveCreation({...receipt,events:[]},expected,registry)).state,'resolution_delayed');
});
test('first buy needs fresh matching quote and meaningful minOut',async()=>{
 const resolved=await c.resolveCreation(receipt,expected,registry);
 const quote={pool:a(5),creator:a(4),amountInRaw:'1000000',amountOutRaw:'10000',expiresAt:now+10000,chainId:125};
 assert.equal(checkedPurchase(resolved,quote,a(4),'1000000',100,now).minOutRaw,'9900');
 for(const update of [{expiresAt:now},{pool:a(7)},{amountOutRaw:'0'},{chainId:1}]) assert.throws(()=>checkedPurchase(resolved,{...quote,...update},a(4),'1000000',100,now));
});
test('durable signing guard double click and reload produce one send',async()=>{
 const values=new Map();const storage={getItem:k=>values.get(k)??null,setItem:(k,v)=>values.set(k,v)};
 const recovery=new c.CreationRecovery(storage,a(4));let sends=0;let finish;
 const pending=recovery.submit(reviewed,'d1',()=>{sends++;return new Promise(r=>{finish=r;});});
 await assert.rejects(recovery.submit(reviewed,'d1',async()=>h(2)));
 const restored=new c.CreationRecovery(storage,a(4));assert.equal(restored.load()[0].state,'outcome_unknown');
 await assert.rejects(restored.submit(reviewed,'d1',async()=>h(2)));
 finish(h(1));assert.equal(await pending,h(1));assert.equal(sends,1);assert.equal(restored.load()[0].submittedHash,h(1));
});
test('storage failure prevents dispatch, unknown outcome never resends',async()=>{
 let sends=0; const r=new c.CreationRecovery({getItem:()=>null,setItem:()=>{throw Error('disk');}},a(4));
 await assert.rejects(r.submit(reviewed,'d1',async()=>{sends++;return h(1);}));assert.equal(sends,0);
});
test('metadata pending survives API outage and bounded creator-scoped retry',async()=>{
 const resolved=await c.resolveCreation(receipt,expected,registry);let p=c.publication('d1',resolved,h(3),'0',now);let persisted;
 p=await c.publishMetadata(p,a(4),now,v=>{persisted=v;},async()=>{throw Error('offline');});assert.equal(p.status,'metadata_pending');assert.equal(persisted.attemptCount,1);
 await assert.rejects(c.publishMetadata(p,a(9),now,()=>{},async()=>({revision:'1'})));
 let publishes=0;p=await c.publishMetadata(p,a(4),now,()=>{},async()=>{publishes++;return {revision:'1'};});
 p=await c.publishMetadata(p,a(4),now,()=>{},async()=>{publishes++;return {revision:'2'};});assert.equal(p.status,'published');assert.equal(publishes,1);
 await assert.rejects(c.publishMetadata({...p,status:'metadata_pending',attemptCount:5},a(4),now,()=>{},async()=>({revision:'2'})));
});
test('draft restores without injected secrets/approval and durable metadata survives reload',async()=>{
 const values=new Map();const storage={getItem:k=>values.get(k)??null,setItem:(k,v)=>values.set(k,v)};
 c.saveDraft(storage,{...draft,signature:'secret',approval:true},now);
 const restored=c.restoreDraft(storage,a(4),'d1',now);assert.equal(restored.signature,undefined);assert.equal(restored.approval,undefined);
 const resolved=await c.resolveCreation(receipt,expected,registry);const p=c.publication('d1',resolved,h(3),'0',now);
 new c.MetadataOutbox(storage,a(4)).persist(p);assert.deepEqual(new c.MetadataOutbox(storage,a(4)).load(),[p]);
});
test('nonfinite quote clocks and registry block provenance fail closed',async()=>{
 const resolved=await c.resolveCreation(receipt,expected,registry);
 const q={pool:a(5),creator:a(4),amountInRaw:'1000000',amountOutRaw:'10000',expiresAt:NaN,chainId:125};
 assert.throws(()=>checkedPurchase(resolved,q,a(4),'1000000',100,now));
 await assert.rejects(c.resolveCreation({...receipt,events:[{...event,blockHash:h(99)}]},expected,registry));
 await assert.rejects(c.resolveCreation(receipt,expected,async()=>({token:a(6),creator:a(4),blockHeight:'2',blockHash:h(11)})));
 assert.throws(()=>c.funding(cost,{...deployment,chainId:1}));
});
test('purchase consent and simulation context immutable; approval identities height-bound',async()=>{
 const resolved=await c.resolveCreation(receipt,expected,registry);
 const context={simulationStatus:'success',priceImpactBps:0,highImpactAcknowledged:false,highSlippageAcknowledged:false,manifestRevision:'r1',router:a(2),sender:a(4),recipient:a(4),opticalRevision:'o1',quoteId:'q1',simulationId:'s1',simulatedAt:now,deadline:now+60000,consentIntentId:'buy1'};
 const quote={...context,pool:a(5),creator:a(4),amountInRaw:'1000000',amountOutRaw:'10000',expiresAt:now+10000,chainId:125};
 bindContent(quote,context,'1000000',100);
 assert.equal(c.reviewFirstPurchase(resolved,quote,context,'1000000',100,now).minOutRaw,'9900');
 for(const patch of [{recipient:a(8)},{consentIntentId:''},{simulationId:'other'},{deadline:NaN}])assert.throws(()=>c.reviewFirstPurchase(resolved,{...quote,...patch},context,'1000000',100,now));
 const approval={chainId:125,token:a(1),spender:a(2),account:a(4),manifestRevision:'r1',accountEpoch:2,targetRaw:'1000000',transactionHash:h(1),blockHeight:'1',blockHash:h(11)};
 for(const observedAt of [now-10001,now+1,NaN])assert.throws(()=>c.verifyCreationApproval(approval,{...approval,status:'success'},{...approval,allowanceRaw:'1000000',observedAt},deployment,now,2));
 assert.throws(()=>c.verifyCreationApproval(approval,{...approval,status:'success'},{...approval,allowanceRaw:'1000000',observedAt:now},deployment,now,3));
 assert.equal(c.verifyCreationApproval(approval,{...approval,status:'success'},{...approval,allowanceRaw:'1000000',observedAt:now},deployment,now,2),true);
 for(const patch of [{spender:a(8)},{account:a(8)},{blockHeight:'2'},{accountEpoch:3}])assert.throws(()=>c.verifyCreationApproval(approval,{...approval,status:'success'},{...approval,...patch,allowanceRaw:'1000000',observedAt:now},deployment,now,2));
});

function bindContent(q,ctx,amountInRaw,slippageBps){ctx.reviewedContent=JSON.stringify({pool:q.pool,creator:q.creator,amountInRaw,amountOutRaw:q.amountOutRaw,expiresAt:q.expiresAt,chainId:q.chainId,slippageBps,router:q.router,sender:q.sender,recipient:q.recipient,deadline:q.deadline,manifestRevision:q.manifestRevision,opticalRevision:q.opticalRevision,quoteId:q.quoteId,simulationId:q.simulationId,simulatedAt:q.simulatedAt,priceImpactBps:q.priceImpactBps});q.reviewedContent=ctx.reviewedContent;}
function checkedPurchase(r,q,creator,amount,slip,clock){const ctx={simulationStatus:'success',priceImpactBps:0,highImpactAcknowledged:false,highSlippageAcknowledged:false,manifestRevision:'r1',router:a(2),sender:creator,recipient:creator,opticalRevision:'o1',quoteId:'q1',simulationId:'s1',simulatedAt:clock,deadline:clock+60000,consentIntentId:'buy1'};const full={...ctx,...q};bindContent(full,ctx,amount,slip);return c.reviewFirstPurchase(r,full,ctx,amount,slip,clock);}
test('fresh intent cannot bypass same draft recovery; explicit deliberate decision required',async()=>{
 const values=new Map();const storage={getItem:k=>values.get(k)??null,setItem:(k,v)=>values.set(k,v)};
 const recovery=new c.CreationRecovery(storage,a(4));await assert.rejects(recovery.submit(reviewed,'other',async()=>h(1)));
 await assert.rejects(recovery.submit(reviewed,'d1',async()=>{throw Error('unknown');}));
 const next=c.review('i2',draft,cost,optical,now);await assert.rejects(recovery.submit(next,'d1',async()=>h(2)));
 assert.throws(()=>recovery.deliberateNewLaunch('d1','i1',{recoveryChecked:false,explicitUserConsent:true}));
 recovery.deliberateNewLaunch('d1','i1',{recoveryChecked:true,explicitUserConsent:true});assert.equal(await recovery.submit(next,'d1',async()=>h(2)),h(2));
});
test('purchase consent binds output slippage and successful simulation with independent acknowledgements',async()=>{
 const resolved=await c.resolveCreation(receipt,expected,registry);
 const ctx={simulationStatus:'success',priceImpactBps:600,highImpactAcknowledged:true,highSlippageAcknowledged:true,manifestRevision:'r1',router:a(2),sender:a(4),recipient:a(4),opticalRevision:'o1',quoteId:'q1',simulationId:'s1',simulatedAt:now,deadline:now+60000,consentIntentId:'buy1'};
 const q={...ctx,pool:a(5),creator:a(4),amountInRaw:'1000000',amountOutRaw:'10000',expiresAt:now+10000,chainId:125};bindContent(q,ctx,'1000000',600);
 for(const patch of [{amountOutRaw:'20000'},{simulationStatus:'failed'},{highImpactAcknowledged:false},{highSlippageAcknowledged:false},{deadline:now+1000}])assert.throws(()=>c.reviewFirstPurchase(resolved,{...q,...patch},ctx,'1000000',600,now));
 assert.throws(()=>c.reviewFirstPurchase(resolved,q,ctx,'1000000',601,now));
 assert.throws(()=>c.reviewFirstPurchase(resolved,q,ctx,'2000000',600,now));
});

test('untrusted primitives and draft collections fail closed without coercion or field mutation',()=>{
 for(const value of [undefined,null,1,1000000n,{},['1000000']])assert.throws(()=>c.raw(value));
 for(const value of [undefined,null,{},[a(4)]])assert.throws(()=>c.address(value));
 for(const value of [undefined,null,{},[h(1)]])assert.throws(()=>c.hash(value));
 for(const [field,value] of [['tags',null],['socialLinks',{}],['uploadRefs',null],['tags',[1]],['socialLinks',[1]],['uploadRefs',[1]]]){
  const input={...draft,[field]:value};const before=JSON.stringify(input);assert.ok(c.validateDraft(input,now)[field]);assert.equal(JSON.stringify(input),before);
 }
 const signed={...draft,name:'e\u0301',symbol:'MiXeD'};assert.deepEqual(c.validateDraft(signed,now),{});assert.equal(JSON.parse(c.review('unicode',signed,cost,optical,now).snapshot).draft.name,'e\u0301');
 for(const field of ['name','symbol','description'])assert.equal(c.validateDraft({...draft,[field]:'unsafe\u202E'},now)[field],'invalid_text');
 assert.equal(c.validateDraft({...draft,socialLinks:['https://example.com/\u0000']},now).socialLinks,'unsafe_url');
});
