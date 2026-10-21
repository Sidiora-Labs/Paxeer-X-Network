import {test} from 'node:test';import assert from 'node:assert/strict';import {SnapshotCache} from '../src/runtime/offline-cache.ts';
test('real bounded cache segregates private accounts and purges private values',()=>{const cache=new SnapshotCache(100,100);cache.put('public',{pool:'a'},null,10000,100);cache.put('private',{quantity:'13'},'account-a',10000,100);assert.equal(cache.get('private','account-b',200),undefined);assert.deepEqual(cache.get('private','account-a',200)?.value,{quantity:'13'});cache.purgePrivate();assert.equal(cache.get('private','account-a',200),undefined);assert.deepEqual(cache.get('public',null,200)?.value,{pool:'a'});});
test('private expiration hides values while public stale age remains explicit',()=>{const cache=new SnapshotCache();cache.put('private',{value:'sensitive'},'a',1000,100);cache.put('public',{pool:'approved'},null,1000,100);assert.equal(cache.get('private','a',1001),undefined);const publicValue=cache.get('public',null,1001);assert.equal(publicValue?.storedAt,100);assert.equal(publicValue?.expiresAt,1000);});
test('LRU respects independent byte caps and cached payloads cannot mutate retained state',()=>{const cache=new SnapshotCache(30,30);const value={id:'a'};cache.put('a',value,null,1000,1);value.id='mutated';cache.put('b',{id:'b'},null,1000,2);cache.get('a',null,3);cache.put('c',{id:'c'},null,1000,4);cache.put('d',{id:'d'},null,1000,5);assert.equal(cache.get('b',null,6),undefined);assert.equal(cache.get<{id:string}>('a',null,6)?.value.id,'a');assert.throws(()=>cache.put('oversize',{value:'x'.repeat(40)},null,1000,1));});

test('only approved public snapshot classes may reach native disk persistence',async()=>{const {approvedPublicKey}=await import('../src/runtime/offline-cache.ts');for(const key of ['pools','live','pool:0x'+'ab'.repeat(20),'profile:0x'+'ab'.repeat(20),'search:usdl'])assert.equal(approvedPublicKey(key),true);for(const key of ['session','pushToken','pending','portfolio','preferences','notifications','messages:private','../session','pool:invalid','search:'+'x'.repeat(251)])assert.equal(approvedPublicKey(key),false);});

test('public disk boundary validates real response contracts rather than trusting cache key',async()=>{
 const {validatePublicSnapshot}=await import('../src/runtime/offline-cache.ts');
 for(const key of ['pools','live','pool:0x'+'ab'.repeat(20),'profile:0x'+'ab'.repeat(20),'search:USDL']){
  for(const payload of [{session:'credential'},{body:'private message'},{transaction:{data:'signed payload'}},{account:null,value:{pushToken:'secret'}}])assert.throws(()=>validatePublicSnapshot(key,payload));
 }
 assert.throws(()=>validatePublicSnapshot('portfolio',{}));
});
test('invalid clocks cannot create private snapshots that evade expiration',()=>{
 const cache=new SnapshotCache();for(const now of [NaN,Infinity,-1,.5])assert.throws(()=>cache.put('private',{quantity:'1'},'account',1000,now));
});
