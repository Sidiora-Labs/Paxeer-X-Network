import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { decimalString, sequence, canonicalAddress, formatRaw, usdl6, SnapshotStream, SdkClient, validateTransport, CapabilityUnavailableError, decodeContract } from '../dist/index.js';
const addr='0x'+'a'.repeat(40), topic=`market.trades.${addr}`, date='2026-10-10T00:00:00.000Z';
const position={chainId:'125',height:'1',blockHash:'0x'+'1'.repeat(64),transactionHash:'0x'+'2'.repeat(64),transactionIndex:'0',logIndex:'0'};
// Synthetic arithmetic/domain fixtures; these are not chain receipt evidence.
const trade={eventId:'fill-1',poolAddress:addr,sender:addr,recipient:null,side:'buy',tokenAmountRaw:'9007199254740993',usdlAmountRaw:'1000000',position,feeRaw:'10',feeAssetAddress:addr,feeAssetDecimals:6,feeDisposition:'distributed_usdl',paidInputRaw:'1000000',pricedInputRaw:'999990',ammInputAfterFeeRaw:'999980',inputDerivationQuality:'exact',recipientAttribution:'unavailable',recipientEvidenceRefs:[],emittingContract:addr,codeEpoch:'1'};
const event=(n=1)=>({eventId:`fill-${n}`,topic,sequence:String(n),revision:String(n),occurredAt:date,watermark:null,payload:{...trade,eventId:`fill-${n}`},coalesced:false,sourceSequenceFirst:null,sourceSequenceLast:null});
const snapshot=(topics=[topic])=>({snapshotId:'11111111-1111-4111-8111-111111111111',topics:topics.map(topic=>({topic,sequence:'0',revision:'1',watermark:null,payload:trade})),resumeCursor:'opaque.cursor',generatedAt:date});
function harness(extra={}) {
 let channel; const sent=[],events=[],installs=[],states=[];let closed=0;
 const stream=new SnapshotStream({transport:{snapshot:async topics=>snapshot(topics),connect:async input=>{channel=input;return {send:f=>sent.push(f),close:()=>closed++}}},installSnapshot:s=>installs.push(s),applyEvent:()=>{},onEvent:e=>events.push(e),onState:s=>states.push(s),...extra});
 const control=(op,cursor=null)=>channel.receive({op,subscriptionId:sent.at(-1)?.subscriptionId??null,cursor,code:null,snapshotUrl:null,watermark:null});
 return {stream,sent,events,installs,states,control,receive:e=>channel.receive(e),get closed(){return closed}};
}
test('exact uint256 and UInt63 boundaries survive JSON without Number',()=>{
 const max=((1n<<256n)-1n).toString();
 for(const raw of ['9007199254740993',max]) assert.equal(JSON.parse(JSON.stringify(decimalString(raw))),raw);
 assert.equal(sequence('9223372036854775807'),'9223372036854775807');
 for(const bad of [1,'01','-1','1e2',(1n<<256n).toString()]) assert.throws(()=>decimalString(bad));
 assert.throws(()=>sequence('9223372036854775808'));assert.equal(canonicalAddress(addr.toUpperCase().replace('0X','0x')),addr);
 assert.equal(formatRaw(usdl6('1999999'),6,2,'half-up'),'2.00');assert.equal(formatRaw(usdl6('1999999'),6,2,'down'),'1.99');
 assert.throws(()=>formatRaw(usdl6('1'),6,2,undefined));
});
test('exact frozen patterns admit ISO/topic and rejects wildcard replacements',()=>{
 decodeContract('StreamEvent',event());
 assert.throws(()=>decodeContract('StreamEvent',{...event(),topic:`marketXtradesX${addr}`}));
 assert.throws(()=>decodeContract('TopicSnapshot',{...snapshot(),generatedAt:date.replace('.000','X000')}));
});
test('snapshot -> replay -> checkpoint -> live applies boundary fill exactly once',async()=>{
 const h=harness();await h.stream.start([topic]);assert.equal(h.installs.length,1);assert.equal(h.stream.state,'replaying');
 assert.equal(h.sent[0].afterCursor,'opaque.cursor');
 h.receive(event(1));h.control('subscribed');h.receive(event(1));assert.equal(h.events.length,0);
 h.control('checkpoint','next.cursor');assert.equal(h.events.length,1);assert.equal(h.stream.state,'live');
 h.receive(event(1));h.receive(event(2));assert.equal(h.events.length,2);h.stream.close();
});
test('malformed/versioned/gapped/unrequested envelopes cannot reach consumer',async()=>{
 for(const bad of [{},{...event(),sequence:1},{...event(),topic:'other'},event(2)]) {
  const h=harness();await h.stream.start([topic]);h.control('subscribed');h.control('checkpoint','c');h.receive(bad);
  assert.equal(h.events.length,0);assert.equal(h.stream.state,'resync-required');assert.equal(h.closed,1);
 }
});
test('superseded and externally aborted snapshot never installs obsolete state',async()=>{
 let resolveA;const installs=[];let oldSignal;
 const h=harness({installSnapshot:s=>installs.push(s),transport:{snapshot:(topics,signal)=>{if(topics[0]===topic){oldSignal=signal;return new Promise(r=>resolveA=r)}return Promise.resolve(snapshot(topics))},connect:async()=>({send(){},close(){}})}});
 const a=h.stream.start([topic]);await h.stream.start(['market.trades.'+'0x'+'b'.repeat(40)]);resolveA(snapshot());await a;
 assert.equal(oldSignal.aborted,true);assert.equal(installs.length,1);assert.ok(installs[0].topics[0].topic.endsWith('b'.repeat(40)));h.stream.close();
 const abort=new AbortController();const h2=harness();await h2.stream.start([topic],abort.signal);abort.abort();assert.equal(h2.stream.state,'closed');
});
test('callback exceptions do not lose 100 immutable fills; overflow fails closed',async()=>{
 let count=0;const h=harness({onEvent:()=>{count++;throw new Error('chart failure')}});await h.stream.start([topic]);h.control('subscribed');h.control('checkpoint','c');
 for(let n=1;n<=100;n++)h.receive(event(n));assert.equal(count,100);assert.equal(h.stream.state,'live');h.stream.close();
 const bounded=harness({maxBuffer:1});await bounded.stream.start([topic]);bounded.receive(event(1));bounded.receive(event(2));assert.equal(bounded.stream.state,'resync-required');assert.equal(bounded.events.length,0);
});
test('visibility after 73 hours reauthenticates and authorization closure clears state',async()=>{
 let tickets=0,snapshots=0,receive;const realNow=Date.now;
 const h=harness({transport:{snapshot:async()=>{snapshots++;return snapshot()},ticket:async()=>{tickets++;return{ticket:'secret',subprotocol:'sidiora.v1',expiresAt:new Date(Date.now()+29000).toISOString()}},connect:async input=>{receive=input.receive;return{send(){},close(){}}}}});
 try {await h.stream.start([topic]);Date.now=()=>realNow()+73*3600*1000;await h.stream.resumeVisible();assert.equal(tickets,2);assert.equal(snapshots,2);h.stream.logout();receive(event(1));assert.equal(h.events.length,0);assert.equal(h.stream.state,'closed');} finally {Date.now=realNow;h.stream.close()}
});
const config={apiOrigin:'https://api.example.com',identityOrigin:'https://id.example.com',mediaOrigin:'https://media.example.com',streamOrigin:'wss://api.example.com',approvedStreamOrigins:['wss://api.example.com'],production:true};
test('production origin validation rejects insecure, private, URL credentials and wrong allowlist before network',()=>{
 for(const patch of [{apiOrigin:'http://api.example.com'},{streamOrigin:'ws://api.example.com'},{apiOrigin:'https://nats.internal'},{apiOrigin:'https://redis.example.com'},{apiOrigin:'https://10.0.0.1'},{apiOrigin:'https://user:pass@api.example.com'},{streamOrigin:'wss://evil.example.com'}]) assert.throws(()=>new SdkClient({...config,...patch},()=>{throw new Error('network reached')}));
 validateTransport(config);
});
test('real HTTP propagates AbortSignal, exact decimals, auth headers and typed unavailable errors',async()=>{
 const requests=[];
 const server=createServer((req,res)=>{requests.push({url:req.url,auth:req.headers.authorization});if(req.url.includes('missing')){res.writeHead(404);res.end();return}if(req.url.includes('delay')){setTimeout(()=>res.end(JSON.stringify('1')),200);return}res.setHeader('Content-Type','application/json');res.end(JSON.stringify('9007199254740993'))});
 server.listen(22400,'127.0.0.1');await once(server,'listening');
 const local={...config,production:false,apiOrigin:'http://127.0.0.1:22400',session:{mode:'bearer',token:()=> 'private-access'}};
 const client=new SdkClient(local);
 try {
 assert.equal(await client.request('/v1/value','UInt256',{authenticated:true}),'9007199254740993');assert.equal(requests[0].auth,'Bearer private-access');assert.equal(requests[0].url,'/v1/value');
 const controller=new AbortController();const pending=client.request('/v1/delay','UInt256',{signal:controller.signal});setTimeout(()=>controller.abort(),20);await assert.rejects(pending,{name:'AbortError'});
 await assert.rejects(client.request('/v1/missing','UInt256'),CapabilityUnavailableError);
 await assert.rejects(client.request('/v1/value?token=secret','UInt256'),TypeError);
 await assert.rejects(client.request('/v1/value?%74oken=secret','UInt256'),TypeError);
 }finally {server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
});
test('mutations never retry; documented reads stop after two retries',async()=>{
 let calls=0;const client=new SdkClient(config,async()=>{calls++;throw new TypeError('network interruption')});
 await assert.rejects(client.request('/v1/value','UInt256',{method:'POST',body:{}}));assert.equal(calls,1);
 const saved=Math.random;Math.random=()=>0;
 try{calls=0;await assert.rejects(client.request('/v1/value','UInt256',{idempotentRead:true}));assert.equal(calls,3);}finally{Math.random=saved}
});

test('store effect failure and quarantined snapshot/checkpoint never report live',async()=>{
 const h=harness({applyEvent:()=>{throw new Error('store unavailable')}});await h.stream.start([topic]);h.control('subscribed');h.receive(event());h.control('checkpoint','c');assert.equal(h.stream.state,'resync-required');assert.equal(h.events.length,0);
 const watermark={chainId:'125',height:'1',blockHash:'0x'+'a'.repeat(64),projectionRevision:'1',observedAt:date,integrity:'quarantined'};
 const bad=snapshot();bad.topics[0].watermark=watermark;
 const h2=harness({transport:{snapshot:async()=>bad,connect:async()=>{throw new Error('must not connect')}}});await h2.stream.start([topic]);assert.equal(h2.installs.length,0);assert.equal(h2.stream.state,'resync-required');
 const h3=harness();await h3.stream.start([topic]);h3.control('subscribed');h3.receive({op:'checkpoint',subscriptionId:h3.sent[0].subscriptionId,cursor:'c',code:null,snapshotUrl:null,watermark});assert.equal(h3.stream.state,'resync-required');
});

test('actual local HTTP/WebSocket adapter sends redacted-subprotocol ticket and replays exact fills',async()=>{
 const {createHash}=await import('node:crypto');
 const {browserStreamTransport}=await import('../dist/index.js');
 const seen=[],delivered=[];let peer;
 const envelope=data=>({requestId:'11111111-1111-4111-8111-111111111111',data,watermark:null,revision:null,quality:'complete',nextCursor:null,generatedAt:date});
 const server=createServer((req,res)=>{
  seen.push({url:req.url,authorization:req.headers.authorization});res.setHeader('Content-Type','application/json');
  if(req.url.startsWith('/v1/snapshots'))res.end(JSON.stringify(envelope({snapshot:snapshot()})));
  else if(req.url==='/v1/stream-tickets')res.end(JSON.stringify(envelope({ticket:'one-use-fixture',subprotocol:'sidiora.v1',expiresAt:new Date(Date.now()+29000).toISOString()})));
  else{res.writeHead(404);res.end()}
 });
 const frame=value=>{const payload=Buffer.from(JSON.stringify(value));return payload.length<126?Buffer.concat([Buffer.from([0x81,payload.length]),payload]):Buffer.concat([Buffer.from([0x81,126,payload.length>>8,payload.length&255]),payload])};
 server.on('upgrade',(req,socket)=>{
  peer=socket;seen.push({url:req.url,protocols:req.headers['sec-websocket-protocol']});
  const accept=createHash('sha1').update(req.headers['sec-websocket-key']+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
  socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\nSec-WebSocket-Protocol: sidiora.v1\r\n\r\n`);
  let pending=Buffer.alloc(0),handled=false;
  socket.on('data',chunk=>{
   if(handled)return;pending=Buffer.concat([pending,chunk]);if(pending.length<6)return;
   let length=pending[1]&127,offset=2;if(length===126){if(pending.length<8)return;length=pending.readUInt16BE(2);offset=4}
   if(pending.length<offset+4+length)return;
   const mask=pending.subarray(offset,offset+4),payload=pending.subarray(offset+4,offset+4+length);for(let i=0;i<payload.length;i++)payload[i]^=mask[i%4];
   const subscribe=JSON.parse(payload.toString());seen.push(subscribe);handled=true;
   const control=(op,cursor=null)=>({op,subscriptionId:subscribe.subscriptionId,cursor,code:null,snapshotUrl:null,watermark:null});
   socket.write(Buffer.concat([frame(control('subscribed')),frame(event(1)),frame(event(1)),frame(control('checkpoint','checkpoint.cursor')),frame(event(2))]));
  });
 });
 server.listen(22401,'127.0.0.1');await once(server,'listening');
 const local={...config,production:false,apiOrigin:'http://127.0.0.1:22401',streamOrigin:'ws://127.0.0.1:22401',approvedStreamOrigins:['ws://127.0.0.1:22401'],session:{mode:'bearer',token:()=> 'fixture-access'}};
 const stream=new SnapshotStream({transport:browserStreamTransport(new SdkClient(local),true),installSnapshot:()=>{},applyEvent:e=>delivered.push(e)});
 try{
  await stream.start([topic]);
  for(let n=0;n<100&&delivered.length<2;n++)await new Promise(resolve=>setTimeout(resolve,10));
  assert.equal(stream.state,'live');assert.deepEqual(delivered.map(e=>e.eventId),['fill-1','fill-2']);assert.equal(delivered[0].payload.tokenAmountRaw,'9007199254740993');
  assert.deepEqual(seen.find(v=>v.protocols)?.protocols.split(',').map(v=>v.trim()),['sidiora.v1','ticket.one-use-fixture']);assert.ok(seen.every(v=>!v.url||!v.url.includes('one-use-fixture')));assert.equal(seen.find(v=>v.op==='subscribe').afterCursor,'opaque.cursor');
 }finally{stream.close();peer?.destroy();server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
});

test('snapshot topic/payload binding and committed watermark regressions fail closed',async()=>{
 const bad=snapshot(['market.price.'+addr]);const h=harness({transport:{snapshot:async()=>bad,connect:async()=>{throw new Error('must not connect')}}});await h.stream.start(['market.price.'+addr]);assert.equal(h.installs.length,0);assert.equal(h.stream.state,'resync-required');
 const watermark={chainId:'125',height:'10',blockHash:'0x'+'a'.repeat(64),projectionRevision:'10',observedAt:date,integrity:'healthy'};
 for(const replacement of [{...watermark,height:'9'},{...watermark,projectionRevision:'9'},{...watermark,blockHash:'0x'+'b'.repeat(64)}]){
 const h2=harness();await h2.stream.start([topic]);h2.control('subscribed');h2.control('checkpoint','c');h2.receive({...event(1),watermark});h2.receive({...event(2),watermark:replacement});assert.equal(h2.events.length,1);assert.equal(h2.stream.state,'resync-required');
 }
});

test('topic union limit and duplicate topics reject before any network call',async()=>{
 let calls=0;const h=harness({transport:{snapshot:async()=>{calls++;return snapshot()},connect:async()=>{calls++;throw new Error('unreachable')}}});
 await h.stream.start(Array.from({length:21},(_,n)=>`topic-${n}`));assert.equal(h.stream.state,'resync-required');assert.equal(calls,0);
 await h.stream.start([topic,topic]);assert.equal(h.stream.state,'resync-required');assert.equal(calls,0);
});

test('superseding start inside replay observer fences old buffer and checkpoint',async()=>{
 let resolveNew;let snapshots=0;let restarted;const delivered=[];
 const h=harness({onEvent:e=>{delivered.push(e.eventId);if(e.sequence==='1')restarted=h.stream.start([topic])},transport:{snapshot:async()=>{snapshots++;return snapshots===1?snapshot():new Promise(resolve=>resolveNew=resolve)},connect:async input=>{h.channel=input;return{send:frame=>h.sent.push(frame),close(){}}}}});
 await h.stream.start([topic]);
 const old=h.channel;const control=(op,cursor=null)=>({op,subscriptionId:h.sent[0].subscriptionId,cursor,code:null,snapshotUrl:null,watermark:null});
 old.receive(control('subscribed'));old.receive(event(1));old.receive(event(2));old.receive(control('checkpoint','old.cursor'));
 assert.deepEqual(delivered,['fill-1']);assert.equal(h.stream.state,'loading');assert.equal(h.installs.length,1);
 resolveNew(snapshot());await restarted;assert.equal(h.stream.state,'replaying');assert.equal(h.sent.at(-1).afterCursor,'opaque.cursor');
 old.receive(control('checkpoint','late.old.cursor'));assert.equal(h.stream.state,'replaying');h.stream.close();
});


test('v2 exact decimal and monetary provenance use generated boundaries', async () => {
  const {decodeV2Contract, displayAmount, usdl6}=await import('../dist/index.js');
  const maximum=((1n<<256n)-1n).toString();
  assert.equal(JSON.parse(JSON.stringify(decodeV2Contract('UInt256',maximum))),maximum);
  assert.equal(decodeV2Contract('UInt63','9007199254740993'),'9007199254740993');
  const raw=usdl6('9007199254740993');const displayed=displayAmount(raw,6,2,'down');
  assert.equal(displayed.raw,'9007199254740993');assert.equal(displayed.formatted,'9007199254.74');
});
test('full jitter uses500ms base and30second cap', async () => {
  const {reconnectDelay}=await import('../dist/index.js');
  assert.equal(reconnectDelay(0,()=>0.5),250);assert.equal(reconnectDelay(20,()=>0.5),15000);
  assert.throws(()=>reconnectDelay(-1));assert.throws(()=>reconnectDelay(0,()=>1));
});
test('public shared ownership cancels only after the last consumer', async () => {
  const {PublicSnapshotRequests}=await import('../dist/index.js');
  const requests=new PublicSnapshotRequests();let finish;let underlyingSignal;let starts=0;
  const work=signal=>{starts++;underlyingSignal=signal;return new Promise(resolve=>{finish=resolve;});};
  const firstAbort=new AbortController();const secondAbort=new AbortController();
  const first=requests.request('public-key',work,firstAbort.signal);const firstRejected=assert.rejects(first);
  const second=requests.request('public-key',work,secondAbort.signal);
  await Promise.resolve();firstAbort.abort();await firstRejected;
  assert.equal(starts,1);assert.equal(underlyingSignal.aborted,false);
  finish('actual-shared-result');assert.equal(await second,'actual-shared-result');assert.equal(requests.activeRequests,0);
});
