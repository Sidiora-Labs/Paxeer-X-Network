'use client';
import { useRef, useState } from 'react';
import type { RecordingSnapshot, VoiceClient } from './voice-client';
export function RecordingConsent({client,roomId,recording,onUpdate,onLeave,authoritative=true}:{client:VoiceClient;roomId:string;recording:RecordingSnapshot|null;onUpdate:()=>void;onLeave:()=>void;authoritative?:boolean}){
    const [pending,setPending]=useState(false);const [message,setMessage]=useState('');const lock=useRef(false);
    async function choose(agree:boolean){if(!recording||lock.current)return;lock.current=true;setPending(true);try{await client.consent(roomId,recording,agree);setMessage(agree?'Thanks, your choice is saved':"You declined, so your audio won't be recorded. You can leave the room");onUpdate();}catch{setMessage("Couldn't save your choice, try again");}finally{lock.current=false;setPending(false);}}
    if(!authoritative)return <p role="status">Can't check recording right now. Leave the room if you don't want to be recorded</p>;
    if(!recording||recording.state==='stopped')return <p role="status">Recording is off</p>;
    return <section aria-label="Recording consent" className="rounded-xl bg-surface-1 p-4 space-y-3"><p role="status" aria-live="polite">{recording.state==='recording'?'● Recording is active.':recording.state==='starting'?'Recording is starting...':recording.state==='stopping'?'Recording is stopping...':'The host wants to record this room. Recording has not started'}</p><p>Purpose: {recording.purpose}</p><p>Policy: {recording.policyVersion}</p><p>Deleted on: <time dateTime={recording.deleteAt}>{recording.deleteAt}</time></p>{recording.state==='consent_pending'&&<div><button type="button" disabled={pending} onClick={()=>choose(true)}>I consent to recording for this purpose</button><button type="button" disabled={pending} onClick={()=>choose(false)}>Decline recording</button></div>}<button type="button" onClick={onLeave}>Leave room</button><p role="status">{message}</p></section>;
}
