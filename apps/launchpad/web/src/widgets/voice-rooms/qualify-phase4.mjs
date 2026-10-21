import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import readline from 'node:readline';
import { pathToFileURL } from 'node:url';
import { performance } from 'node:perf_hooks';

const check=(value,message)=>{if(!value)throw new Error(message);};
const sha=bytes=>crypto.createHash('sha256').update(bytes).digest('hex');
const pin=entry=>{check(entry&&typeof entry.path==='string'&&/^[0-9a-f]{64}$/.test(entry.sha256),'Pinned runtime input required');check(sha(fs.readFileSync(entry.path))===entry.sha256,'Runtime input digest mismatch');return entry.path;};
const index=process.argv.indexOf('--config');check(index>=0,'--config is required');
const config=JSON.parse(fs.readFileSync(process.argv[index+1],'utf8'));
check(sha(fs.readFileSync(process.argv[index+1]))===process.env.L38_REVIEWED_MANIFEST_SHA256,'Reviewed configuration digest mismatch');
const {chromium,request}=await import(pathToFileURL(pin(config.playwrightModule)).href);
const WSModule=await import(pathToFileURL(pin(config.websocketModule)).href);const WebSocket=WSModule.default;
const sdk=pin(config.livekitBrowserBundle);const axe=pin(config.axeScript);
const actors=new Map(config.actors.map(a=>[a.id,a]));
check(actors.size===config.actors.length,'Duplicate actor identity');
check(config.rooms.length===100,'100 actual rooms required');
check(typeof config.microphoneDeviceId==='string'&&config.microphoneDeviceId.length>0,'Actual microphone device input required');
check(!config.chromiumArgs.some(arg=>/fake|mock|disable-web-security|ignore-certificate/i.test(arg)),'Fake media and weakened browser security are forbidden');
let measuredObservations=0;let previousScenarioObservations=0;
const emit=value=>{if(['request','rtp','voice-peak-sample','provider-permission-denial','oracle-observed','accessibility'].includes(value.kind))measuredObservations++;process.stdout.write(JSON.stringify(value)+'\n');};
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const pending=new Map();
readline.createInterface({input:process.stdin}).on('line',line=>{const value=JSON.parse(line);const resolve=pending.get(value.stage);if(resolve){pending.delete(value.stage);clearTimeout(resolve.timer);resolve.resolve(value);}});
const control=async(stage,extra={})=>{const next=new Promise((resolve,reject)=>{const timer=setTimeout(()=>{if(pending.delete(stage))reject(new Error('Operational control deadline exceeded'));},90000);pending.set(stage,{resolve,timer});});emit({kind:'control',stage,...extra});const value=await next;check(value.continue===true,'Operational fault was not acknowledged');return value;};
const oracle=async(name,id)=>{const stage=`oracle:${name}:${id}`;const value=(await control(stage,{name,id})).value;emit({kind:'oracle-observed',name,identifierHash:sha(Buffer.from(id)),value});return value;};
const contexts=new Map();
const actor=id=>{const a=actors.get(id);check(a&&/^0x[0-9a-f]{40}$/.test(a.wallet),'Actual authenticated actor required');return a;};
async function context(id){if(!contexts.has(id)){const a=actor(id);contexts.set(id,await request.newContext({storageState:pin(a.storageState),ignoreHTTPSErrors:false,timeout:5000}));}return contexts.get(id);}
async function api(id,replica,path,method='GET',data=undefined,expected=[200]){
    const origin=new URL(replica.origin);check(origin.protocol==='https:','Exact HTTPS service origin required');
    const a=actor(id);const headers={Origin:config.applicationOrigin};if(method!=='GET'){check(process.env[a.csrfTokenEnv],'Actual session CSRF proof required');headers['X-CSRF-Token']=process.env[a.csrfTokenEnv];}
    if(path.startsWith('/admin/bans')){check(process.env[a.adminKeyEnv],'Configured outer moderator route key required');headers['X-API-Key']=process.env[a.adminKeyEnv];}
    const began=performance.now();const response=await (await context(id)).fetch(new URL(path,origin).href,{method,data,headers,timeout:5000});
    const bytes=await response.body();emit({kind:'request',transport:'https',actorHash:sha(Buffer.from(a.wallet)),replica:replica.origin,path:path.split('?')[0],status:response.status(),latencyMs:performance.now()-began,responseSha256:sha(bytes)});
    check(expected.includes(response.status()),`Unexpected HTTP status ${response.status()} on ${path.split('?')[0]}`);
    return {status:response.status(),body:bytes.length?JSON.parse(bytes.toString()):null,headers:response.headers()};
}
async function provider(method,input,service='RoomService'){
    const endpoint=new URL(config.provider.endpoint);check(endpoint.protocol==='https:'&&endpoint.hostname.endsWith('.livekit.cloud'),'Supported provider endpoint required');
    const key=process.env[config.provider.apiKeyEnv],secret=process.env[config.provider.apiSecretEnv];check(key&&secret&&secret.length>=32,'Actual provider observer credentials required');
    const now=Math.floor(Date.now()/1000);const header=Buffer.from(JSON.stringify({alg:'HS256',typ:'JWT'})).toString('base64url');
    const body=Buffer.from(JSON.stringify({iss:key,nbf:now,exp:now+30,video:{roomList:true,roomAdmin:true,roomRecord:true}})).toString('base64url');
    const unsigned=header+'.'+body;const token=unsigned+'.'+crypto.createHmac('sha256',secret).update(unsigned).digest('base64url');
    const start=performance.now();const result=await fetch(new URL('/twirp/livekit.'+service+'/'+method,endpoint),{method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:JSON.stringify(input),signal:AbortSignal.timeout(5000)});
    const bytes=Buffer.from(await result.arrayBuffer());emit({kind:'request',transport:'provider',path:method,status:result.status,latencyMs:performance.now()-start,responseSha256:sha(bytes)});check(result.status===200,'Actual provider reconciliation query failed');const parsed=JSON.parse(bytes.toString());const normalize=value=>{if(Array.isArray(value))return value.map(normalize);if(value&&typeof value==='object')return Object.fromEntries(Object.entries(value).map(([key,v])=>[key.replace(/[A-Z]/g,ch=>'_'+ch.toLowerCase()),normalize(v)]));return value;};return normalize(parsed);
}
function scenario(id,_expected,extra={}){const observations=measuredObservations-previousScenarioObservations;check(observations>0,'Scenario requires real observations');previousScenarioObservations=measuredObservations;emit({kind:'scenario',scenario:String(id),passed:true,observations,...extra});}
const replica=config.replicas[0];const pool=config.throttle.pool;const affected=config.throttle.actor;
const authority=config.authority;
const ticketBody=topics=>({topics:[...topics].sort(),schemaMajor:2,filterDigest:sha(Buffer.from(JSON.stringify([...topics].sort()))),origin:config.applicationOrigin,endpoint:'/v2/stream'});
const sockets=[];
async function socket(id,r,targetPool=pool){
    const topics=[`chat.pool.${targetPool}`,`private.chat.${actor(id).wallet}`];
    const issued=await api(id,{origin:r.identityOrigin},'/v2/stream/tickets','POST',ticketBody(topics));
    const ticket=issued.body.data;check(ticket&&/^[0-9a-f]{64}$/.test(ticket.ticket)&&ticket.subprotocol==='sidiora.v2','Real scoped stream ticket missing');
    const state=JSON.parse(fs.readFileSync(pin(actor(id).storageState),'utf8'));
    const host=new URL(r.streamOrigin).hostname;
    const cookies=state.cookies.filter(c=>host===c.domain.replace(/^\./,'')||host.endsWith(c.domain.startsWith('.')?c.domain:'.'+c.domain)).map(c=>c.name+'='+c.value).join('; ');
    check(cookies,'Actual stream session cookies missing');
    const endpoint=new URL('/v2/stream',r.streamOrigin);endpoint.searchParams.set('topics',topics.join(','));
    const ws=new WebSocket(endpoint,['sidiora.v2',`ticket.${ticket.ticket}`],{headers:{Origin:config.applicationOrigin,Cookie:cookies},handshakeTimeout:5000});
    const queue=[];const waiters=[];ws.on('message',wire=>{const frame=JSON.parse(wire.toString());const matched=waiters.findIndex(w=>w.match(frame));if(matched>=0){const item=waiters.splice(matched,1)[0];clearTimeout(item.timer);item.resolve(frame);}else queue.push(frame);check(queue.length<=1000,'Socket capture exceeded bounded queue');});
    await new Promise((resolve,reject)=>{ws.once('open',resolve);ws.once('error',reject);});
    const receive=match=>{const found=queue.findIndex(match);if(found>=0)return Promise.resolve(queue.splice(found,1)[0]);return new Promise((resolve,reject)=>{const item={match,resolve,timer:setTimeout(()=>{const i=waiters.indexOf(item);if(i>=0)waiters.splice(i,1);reject(new Error('Socket observation deadline exceeded'));},5000)};waiters.push(item);});};
    await receive(frame=>frame.type==='session_accepted');
    ws.send(JSON.stringify({type:'join_pool',pool_address:targetPool}));check((await receive(frame=>frame.type==='joined_pool'||frame.type==='error')).type==='joined_pool','Actual room admission failed');
    const value={ws,receive,replica:r,id,pool:targetPool};sockets.push(value);return value;
}
async function send(s,kind,suffix){const clientId=`l38-${config.runId}-${suffix}`;const begin=performance.now();s.ws.send(JSON.stringify({type:kind==='pool'?'pool_message':'dm',pool_address:s.pool,to:actor(config.throttle.recipient).wallet,content:`Qualification ${clientId}`,client_message_id:clientId}));const result=await s.receive(frame=>frame.client_message_id===clientId);emit({kind:'request',transport:'websocket',clientId,replica:s.replica.origin,messageType:kind,response:result.type,latencyMs:performance.now()-begin,retryable:result.retryable??null,retryAfterSeconds:result.retry_after_seconds??null});return result;}
check(process.env.L38_ARTIFACT_DIR&&fs.statSync(process.env.L38_ARTIFACT_DIR).isDirectory(),'Private artifact directory required');const screenshotDirectory=path.join(process.env.L38_ARTIFACT_DIR,'screenshots');fs.mkdirSync(screenshotDirectory,{mode:0o700});
const browser=await chromium.launch({headless:config.headless,args:config.chromiumArgs});
const pages=[];
const browserContexts=[];
const grantsByRoom=new Map();
try{
    const distributed=await Promise.all(config.replicas.map(r=>socket(affected,r)));
    const burst=await Promise.all(Array.from({length:11},(_,i)=>send(distributed[i%4],'pool',`throttle-${i}`)));
    check(burst.filter(v=>v.type==='message_persisted').length===10,'Four replicas did not enforce ten accepted room messages');
    const denied=burst.filter(v=>v.type==='error');check(denied.length===1&&denied[0].retry_after_seconds>0,'429-equivalent socket retry delay missing');
    const pacedSockets=await Promise.all(config.replicas.map(r=>socket(affected,r,config.throttle.pacedPool)));const paced=[];const paceBegan=performance.now();for(let i=0;i<11;i++){await delay(Math.max(0,paceBegan+i*60000/11-performance.now()));paced.push(await send(pacedSockets[i%4],'pool',`paced-${i}`));}check(performance.now()-paceBegan<=60000,'Eleven-message minute observation exceeded its actual window');check(paced.filter(v=>v.type==='message_persisted').length===10&&paced.filter(v=>v.type==='error').length===1,'Room default accepted more than ten messages in the observed minute');scenario(5,22);
    const target=burst.find(v=>v.type==='message_persisted').id;
    await control('safety_dependency_loss:begin');
    const outage=await Promise.all([send(distributed[0],'pool','outage-pool'),send(distributed[1],'dm','outage-dm')]);
    check(outage.every(v=>v.type==='error'&&v.retryable===true),'Dependency outage must be explicitly retryable; error text is insufficient');
    await control('safety_dependency_loss:end');scenario(1,2);
    const reported=await api(authority.reporter,replica,'/v2/safety/reports','POST',{targetType:'message',targetId:target,category:'abuse',description:'Isolated qualification case'});
    const caseId=reported.body.ID;check(caseId,'Actual moderation case missing');const duplicate=await api(authority.reporter,replica,'/v2/safety/reports','POST',{targetType:'message',targetId:target,category:'abuse',description:'Isolated qualification case'});check(duplicate.body.ID===caseId&&duplicate.body.Revision===reported.body.Revision,'Duplicate report inflated case identity or revision');
    const raced=await Promise.all([api(authority.moderator,replica,`/v2/safety/cases/${caseId}`,'PATCH',{status:'actioned',expected_revision:1,reason:'Qualification policy review',policy_version:config.policyVersion},[200,409]),api(authority.independentModerator,replica,`/v2/safety/cases/${caseId}`,'PATCH',{status:'dismissed',expected_revision:1,reason:'Independent qualification review',policy_version:config.policyVersion},[200,409])]);
    check(raced.filter(v=>v.status===200).length===1&&raced.filter(v=>v.status===409).length===1,'Moderation revision race lost single-winner semantics');
    const action=await oracle('case-action',caseId);check(action.count===1,'Race produced more than one final audit action');scenario(2,3);
    const bannedBy='0x9999999999999999999999999999999999999999';
    const previousDenial=await oracle('reviewer-denial-before',actor(authority.reviewer).wallet);
    await api(authority.reviewer,replica,'/admin/bans','POST',{wallet:actor(affected).wallet,banned_by:bannedBy,reason:'Qualification unauthorized global action'},[403]);
    const audit=await oracle('reviewer-denial',actor(authority.reviewer).wallet);check(audit.count===previousDenial.count+1&&audit.actor===actor(authority.reviewer).wallet,'Reviewer denial audit actor missing or spoofed');scenario(3,2);
    const recoveredSocket=await socket(affected,replica,config.reversalPool);
    const reversibleMessage=await send(recoveredSocket,'pool','reversal-case');check(reversibleMessage.type==='message_persisted','Reversible real message did not commit');
    const reversalCase=(await api(authority.reporter,replica,'/v2/safety/reports','POST',{targetType:'message',targetId:reversibleMessage.id,category:'abuse',description:'Isolated reversible policy test'})).body;
    const ban=(await api(authority.moderator,replica,`/v2/safety/cases/${reversalCase.ID}/bans`,'POST',{wallet:actor(affected).wallet,scope:'pool',resource:config.reversalPool,expectedRevision:1,reason:'Qualification room restriction',policyVersion:config.policyVersion})).body;
    const linked=await oracle('linked-action',ban.banId);
    const appeal=(await api(affected,replica,'/v2/safety/appeals','POST',{actionId:linked.actionId,reason:'Request independent qualification review'})).body;
    const global=(await api(authority.administrator,replica,'/admin/bans','POST',{wallet:actor(affected).wallet,banned_by:bannedBy,reason:'Independent global qualification restriction'})).body;
    const globalAudit=await oracle('ban-actor',global.ban.id);check(globalAudit.actor===actor(authority.administrator).wallet,'Spoofed banned_by changed actual actor');
    await api(authority.independentModerator,replica,`/v2/safety/appeals/${appeal.id}/reverse-ban`,'POST',{banId:ban.banId,expectedRevision:1,expectedBanRevision:1,reason:'Independent reversal',policyVersion:config.policyVersion});
    check((await send(recoveredSocket,'pool','global-still-denied')).type==='error','Independent global ban lost enforcement');
    const resumedAt=performance.now();await api(authority.administrator,replica,`/admin/bans/${global.ban.id}`,'DELETE',{expected_revision:1,reason:'Independent global restriction resolved'});
    let permitted=null;while(performance.now()-resumedAt<5000){permitted=await send(recoveredSocket,'pool',`resume-${crypto.randomUUID()}`);if(permitted.type==='message_persisted')break;await delay(100);}
    check(permitted?.type==='message_persisted'&&performance.now()-resumedAt<=5000,'Connected writer permission recovery exceeded five seconds');scenario(4,5,{recoveryLatencyMs:performance.now()-resumedAt});
    await control('single_node_loss:begin');await control('single_node_loss:end');
    const failure=config.failureRooms.unavailable;await control('provider_unavailable:begin');
    await api(failure.host,replica,`/v2/voice-rooms/${failure.roomId}/start`,'POST',{expectedRevision:failure.revision},[503]);
    const draft=await oracle('voice-room',failure.roomId);check(draft.state==='draft'&&draft.providerSession==='','Provider unavailable emitted fake live state');await control('provider_unavailable:end');scenario(6,2);
    const crash=config.failureRooms.createCrash;await control('provider_create_db_crash:begin');
    await api(crash.host,replica,`/v2/voice-rooms/${crash.roomId}/start`,'POST',{expectedRevision:crash.revision},[503]);
    const orphan=await oracle('voice-room',crash.roomId);check(orphan.state==='draft'&&orphan.providerSession==='','Create interruption did not leave unacknowledged draft');
    const orphanName='kindle-'+orphan.startKey;const liveOrphan=await provider('ListRooms',{names:[orphanName]});check(liveOrphan.rooms?.length===1&&liveOrphan.rooms[0].name===orphanName,'Actual provider orphan missing or duplicated');
    const orphanBegan=performance.now();while(performance.now()-orphanBegan<config.orphanHoldSeconds*1000){await delay(1000);}
    await control('provider_create_db_crash:end');await oracle('orphan-alarm',crash.roomId);
    let recovered;const beganRecovery=performance.now();while(performance.now()-beganRecovery<70000){recovered=await oracle('voice-room',crash.roomId);if(recovered.state==='live'||recovered.state==='ended')break;await delay(1000);}
    check(recovered?.state==='live'||recovered?.state==='ended','Provider-create orphan was not reconciled');scenario(10,3,{recoveryLatencyMs:performance.now()-beganRecovery});
    for(const room of config.rooms){
        check(room.actors.length===50&&new Set(room.actors).size===50&&room.host===room.actors[0]&&!room.actors.includes(room.overflowActor),'Room must contain host plus 49 participants and distinct overflow identity');
        await api(room.host,replica,`/v2/voice-rooms/${room.roomId}/start`,'POST',{expectedRevision:room.revision});
        const grants=[];for(const id of room.actors){const body=(await api(id,replica,`/v3/voice-rooms/${room.roomId}/tickets`,'POST',{})).body;check(body.ticket&&new URL(body.providerEndpoint).hostname.endsWith('.livekit.cloud'),'Supported actual LiveKit Cloud grant required');grants.push({actor:id,wallet:actor(id).wallet,...body});}
        grantsByRoom.set(room.roomId,grants);
        const ctx=await browser.newContext({permissions:['microphone'],ignoreHTTPSErrors:false});browserContexts.push(ctx);const page=await ctx.newPage();pages.push({page,room});await page.goto(config.applicationOrigin,{waitUntil:'domcontentloaded'});await page.addScriptTag({path:sdk});
        await page.evaluate(async grants=>{window.l38={rooms:[],receiverTracks:[],unpublished:0};for(const grant of grants){const r=new LivekitClient.Room();r.on(LivekitClient.RoomEvent.TrackSubscribed,(track,_pub,participant)=>{if(track.kind==='audio'){window.l38.receiverTracks.push({identity:participant.identity,track});document.body.append(track.attach());}});r.on(LivekitClient.RoomEvent.TrackUnsubscribed,()=>window.l38.unpublished++);await r.connect(grant.providerEndpoint,grant.ticket);window.l38.rooms.push(r);}},grants);
        await api(room.overflowActor,replica,`/v3/voice-rooms/${room.roomId}/tickets`,'POST',{},[409]);
        for(const id of room.actors.slice(0,10)){const prompt=(await api(room.host,replica,`/v2/voice-rooms/${room.roomId}/prompt`)).body;const p=prompt.participants.find(p=>p.wallet===actor(id).wallet);check(p,'Participant revision missing');await api(room.host,replica,`/v2/voice-rooms/${room.roomId}/speakers`,'POST',{wallet:actor(id).wallet,expectedRevision:p.revision});}
        const prompt=(await api(room.host,replica,`/v2/voice-rooms/${room.roomId}/prompt`)).body;const eleventh=prompt.participants.find(p=>p.wallet===actor(room.actors[10]).wallet);await api(room.host,replica,`/v2/voice-rooms/${room.roomId}/speakers`,'POST',{wallet:eleventh.wallet,expectedRevision:eleventh.revision},[409]);
        await page.waitForFunction(()=>window.l38.rooms.slice(0,10).every(r=>r.localParticipant.permissions?.canPublish),{},{timeout:5000});
        await page.evaluate(async device=>{for(const r of window.l38.rooms.slice(0,10))await r.localParticipant.setMicrophoneEnabled(true,{deviceId:{exact:device},echoCancellation:false,noiseSuppression:false,autoGainControl:false});},config.microphoneDeviceId);
        emit({kind:'voice-capacity',roomId:room.roomId,connectedParticipants:await page.evaluate(()=>window.l38.rooms.filter(r=>r.state==='connected').length),publishers:await page.evaluate(()=>window.l38.rooms.filter(r=>r.localParticipant.isMicrophoneEnabled).length)});
    }
    const measuredParticipants=await Promise.all(pages.map(({page})=>page.evaluate(()=>window.l38.rooms.filter(r=>r.state==='connected').length)));check(measuredParticipants.reduce((a,b)=>a+b,0)===5000,'Voice override did not sustain 5000 real provider connections');
    await delay(1000);
    const holdBegan=performance.now();
    while(performance.now()-holdBegan<config.voiceHoldSeconds*1000){
        const samples=await Promise.all(pages.map(async({page,room})=>({roomId:room.roomId,...await page.evaluate(async()=>{
            const state=window.l38;const tracks=state.receiverTracks.filter(item=>item.identity===state.rooms[0].localParticipant.identity);
            const stats=await Promise.all(tracks.map(item=>item.track.getReceiverStats()));
            return {participants:state.rooms.filter(r=>r.state==='connected').length,publishers:state.rooms.filter(r=>r.localParticipant.isMicrophoneEnabled).length,receivedBytes:stats.reduce((n,v)=>n+(v?.bytesReceived??0),0),audioEnergy:stats.reduce((n,v)=>n+(v?.totalAudioEnergy??0),0)};
        })})));
        for(const sample of samples){emit({kind:'voice-peak-sample',...sample});check(sample.participants===50&&sample.publishers===10&&sample.receivedBytes>0&&sample.audioEnergy>0,'Actual voice override occupancy or RTP evidence missing');}
        await delay(1000);
    }
    const first=pages[0];const speaker=first.room.actors[0];const listenerIndex=49;
    const unauthorized=await first.page.evaluate(async ({index,device})=>{
        const r=window.l38.rooms[index];const original=r.localParticipant.permissions;
        const capture=await LivekitClient.createLocalAudioTrack({deviceId:{exact:device},echoCancellation:false,noiseSuppression:false,autoGainControl:false});
        document.body.dataset.role='speaker';r.localParticipant.permissions={...original,canPublish:true};
        try{await r.localParticipant.publishTrack(capture,{source:LivekitClient.Track.Source.Microphone});return {rejected:false};}
        catch(error){return {rejected:error.name==='SignalRequestError'&&error.reasonName==='NOT_ALLOWED',errorName:error.name,reason:error.reasonName};}
        finally{r.localParticipant.permissions=original;capture.stop();}
    },{index:listenerIndex,device:config.microphoneDeviceId});
    emit({kind:'provider-permission-denial',...unauthorized});check(unauthorized.rejected,'Provider did not explicitly reject mutated listener browser publish grant');
    const readRtp=()=>first.page.evaluate(async identity=>{const tracks=window.l38.receiverTracks.filter(v=>v.identity===identity);const stats=await Promise.all(tracks.map(async v=>({identity:v.identity,stats:await v.track.getReceiverStats()})));return {stats,unpublished:window.l38.unpublished};},actor(speaker).wallet);
    let previous=await readRtp();await delay(500);let live=await readRtp();const bytes=v=>v.stats.reduce((n,item)=>n+(item.stats?.bytesReceived??0),0);const energy=v=>v.stats.reduce((n,item)=>n+(item.stats?.totalAudioEnergy??0),0);check(bytes(live)>bytes(previous)&&energy(live)>energy(previous),'Actual received microphone RTP and audio energy did not increase');emit({kind:'rtp',phase:'before-ejection',roomId:first.room.roomId,bytesReceived:bytes(live),audioEnergy:energy(live)});
    const current=(await api(first.room.host,replica,`/v2/voice-rooms/${first.room.roomId}/prompt`)).body;const participant=current.participants.find(p=>p.wallet===actor(speaker).wallet);
    const old=(await api(speaker,replica,`/v3/voice-rooms/${first.room.roomId}/tickets`,'POST',{})).body;
    const hostReport=await api(authority.reporter,replica,'/v2/safety/reports','POST',{targetType:'voice_participant',targetId:`${first.room.roomId}:${actor(speaker).wallet}`,category:'abuse',description:'Isolated active host safety drill'});check(hostReport.body.PoolID==='','Abusive host report failed global queue escalation');
    await first.page.evaluate(identity=>{for(const item of window.l38.receiverTracks.filter(v=>v.identity===identity))item.track.setVolume(0);},actor(speaker).wallet);
    const revokeAt=performance.now();await api(first.room.host,replica,`/v3/voice-rooms/${first.room.roomId}/moderation-actions`,'POST',{wallet:actor(speaker).wallet,expectedRevision:participant.revision,action:'eject',reason:'Isolated qualification ejection'});
    let stopped=false;while(performance.now()-revokeAt<5000){const a=await readRtp();await delay(250);const b=await readRtp();if(b.unpublished>live.unpublished&&bytes(a)===bytes(b)){stopped=true;emit({kind:'rtp',phase:'after-ejection',bytesReceived:bytes(b),audioEnergy:energy(b),stopLatencyMs:performance.now()-revokeAt});break;}}
    check(stopped&&performance.now()-revokeAt<=5000,'Real receiver audio did not stop within five seconds');scenario(7,4,{stopLatencyMs:performance.now()-revokeAt});
    const validation=new URL('/rtc/validate',old.providerEndpoint.replace('wss:','https:'));validation.searchParams.set('access_token',old.ticket);const rejected=await fetch(validation,{signal:AbortSignal.timeout(5000)});check([401,403].includes(rejected.status),'Provider accepted old speaker token after ejection');await api(speaker,replica,`/v3/voice-rooms/${first.room.roomId}/tickets`,'POST',{},[403]);scenario(9,4);
    const recording=(await api(first.room.host,replica,`/v2/voice-rooms/${first.room.roomId}/recording`,'POST',{})).body;
    for(const id of first.room.actors.slice(1)){await api(id,replica,`/v2/voice-rooms/${first.room.roomId}/recording/consent`,'POST',{recordingId:recording.id,expectedRevision:recording.revision,agree:id!==first.room.actors[1]});}
    await api(first.room.host,replica,`/v2/voice-rooms/${first.room.roomId}/recording/start`,'POST',{expectedRevision:recording.revision},[403]);const rec=(await api(first.room.actors[1],replica,`/v2/voice-rooms/${first.room.roomId}/recording`)).body;check(rec.state==='consent_pending','Declined participant was silently recorded');const providerRoom=await oracle('voice-room',first.room.roomId);const egress=await provider('ListEgress',{room_name:providerRoom.providerSession},'Egress');check(!(egress.items??[]).some(item=>['EGRESS_STARTING','EGRESS_ACTIVE','EGRESS_ENDING',0,1,2].includes(item.status)),'Provider captured audio after participant declined');scenario(8,50);
    const visibility=config.visibilityFixture;pin(visibility.originalEnvelope);await control('moderation_replay:begin');const hidden=await api(authority.reporter,replica,visibility.emptyCacheReadPath);check(JSON.stringify(hidden.body).indexOf(visibility.removedText)<0,'Empty subscriber cache replay resurrected removed text');const restore=await api(authority.independentModerator,replica,`/v2/safety/cases/${visibility.caseId}/restore`,'POST',{expectedRevision:visibility.revision,reason:'Independent permitted restore',policyVersion:config.policyVersion});const restored=await api(authority.reporter,replica,visibility.emptyCacheReadPath);check(restore.body.revision>visibility.revision&&JSON.stringify(restored.body).includes(visibility.permittedRestoredText),'Authorized restore did not reintroduce only permitted text');await control('moderation_replay:end');scenario(11,3);
    for(const {page,room} of pages){await page.evaluate(async()=>{for(const r of window.l38.rooms)await r.disconnect();});const prompt=(await api(room.host,replica,`/v2/voice-rooms/${room.roomId}/prompt`)).body;await api(room.host,replica,`/v2/voice-rooms/${room.roomId}/end`,'POST',{expectedRevision:prompt.revision});}
    if(recovered.state==='live')await api(crash.host,replica,`/v2/voice-rooms/${crash.roomId}/end`,'POST',{expectedRevision:recovered.revision});
    const managedNames=[orphanName];for(const room of config.rooms){const observed=await oracle('voice-room',room.roomId);check(observed.state==='ended','Authoritative room did not end');managedNames.push(observed.providerSession);}
    let remainingCount=0;for(let offset=0;offset<managedNames.length;offset+=100){const remaining=await provider('ListRooms',{names:managedNames.slice(offset,offset+100)});remainingCount+=(remaining.rooms??[]).length;}check(remainingCount===0,'Ended rooms retained managed provider sessions');emit({kind:'provider-reconciliation',managedSessions:managedNames.length,remainingSessions:remainingCount});
    for(const journey of config.accessibilityJourneys){check(/^[a-z-]{1,64}$/.test(journey.name),'Bounded actual journey name required');for(const viewport of [{width:390,height:844},{width:1440,height:900}]){const ctx=await browser.newContext({storageState:pin(actor(journey.actor).storageState),viewport,reducedMotion:'reduce',ignoreHTTPSErrors:false,permissions:['microphone']});try{const page=await ctx.newPage();await page.goto(journey.url,{waitUntil:'networkidle'});await page.addScriptTag({path:axe});const result=await page.evaluate(async()=>axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa']}}));emit({kind:'accessibility',journey:journey.name,viewport,violations:result.violations.map(v=>({id:v.id,impact:v.impact,nodes:v.nodes.length})),passes:result.passes.length});check(result.violations.length===0&&result.passes.length>0,'Real safety/voice accessibility gate failed');await page.keyboard.press('Tab');check(await page.evaluate(()=>document.activeElement!==document.body),'Keyboard focus did not reach an actionable element');for(const step of journey.steps){check(['button','textbox','combobox','checkbox','link'].includes(step.role)&&typeof step.name==='string','Real accessible role/name action required');const locator=page.getByRole(step.role,{name:step.name,exact:true});check(await locator.count()===1,'Journey control must be unambiguous');if(step.action==='fill'){await locator.fill(step.value);}else if(step.action==='select'){await locator.selectOption(step.value);}else if(step.action==='check'){await locator.check();}else if(step.action==='click'){check(step.request&&step.confirmationText,'Actual mutation receipt and visible confirmation required');const wait=page.waitForResponse(response=>new URL(response.url()).pathname===step.request.path&&response.request().method()===step.request.method,{timeout:5000});const began=performance.now();await locator.click();const response=await wait;check(response.status()===step.request.status,'Actual user mutation failed');const bytes=await response.body();emit({kind:'request',transport:'browser-ui',journey:journey.name,status:response.status(),path:step.request.path,latencyMs:performance.now()-began,responseSha256:sha(bytes)});await page.getByText(step.confirmationText,{exact:false}).first().waitFor({state:'visible',timeout:5000});}else{throw new Error('Unsupported real journey action');}}const imagePath=path.join(screenshotDirectory,`${journey.name}-${viewport.width}.png`);await page.screenshot({path:imagePath,fullPage:true});emit({kind:'screenshot',journey:journey.name,viewport,file:path.relative(process.env.L38_ARTIFACT_DIR,imagePath),sha256:sha(fs.readFileSync(imagePath))});}finally{await ctx.close();}}}
    check(['comments','follows','block','report','appeal','moderation','voice'].every(name=>config.accessibilityJourneys.some(j=>j.name===name)),'Comments, follows, block, report, appeal, moderation and voice actual journeys required');scenario(12,config.accessibilityJourneys.length*2,{voiceRooms:100,participants:5000,publishersMaximum:1000});
}catch(error){emit({kind:'failure',message:error.message});process.exitCode=1;}finally{
    for(const s of sockets)s.ws.close();
    for(const ctx of browserContexts)await ctx.close();
    for(const ctx of contexts.values())await ctx.dispose();
    await browser.close();
}
