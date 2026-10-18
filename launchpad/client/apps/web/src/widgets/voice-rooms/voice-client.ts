import { Room, RoomEvent, Track } from 'livekit-client';
import { SafetyClient } from '../community-safety/safety-client';
export interface RecordingSnapshot {id:string;roomId:string;state:'consent_pending'|'starting'|'recording'|'stopping'|'stopped';purpose:string;policyVersion:string;revision:number;deleteAt:string}
export interface VoiceParticipant {wallet:string;role:string;muted:boolean;revision:number}
export interface VoicePrompt {roomId:string;state:string;revision:number;recording:RecordingSnapshot|null;participants:readonly VoiceParticipant[]}
export interface JoinConsent {recordingId:string;policyVersion:string;purpose:string;agree:true}
export class VoiceClient {
    private room: Room | null = null;
    private connectionGeneration=0;
    private audio = new Map<string, HTMLMediaElement[]>();
    private blocked = new Set<string>();
    private connectionChanged: ((connected:boolean)=>void) | null = null;
    onConnectionChange(callback:((connected:boolean)=>void)|null){this.connectionChanged=callback;}
    constructor(readonly api: SafetyClient) {}
    prompt(id:string,signal?:AbortSignal){return this.api.request<VoicePrompt>(`/v2/voice-rooms/${encodeURIComponent(id)}/prompt`,'GET',undefined,signal);}
    recording(id:string){return this.api.request<RecordingSnapshot>(`/v2/voice-rooms/${encodeURIComponent(id)}/recording`);}
    consent(roomId:string,recording:RecordingSnapshot,agree:boolean){return this.api.request(`/v2/voice-rooms/${encodeURIComponent(roomId)}/recording/consent`,'POST',{recordingId:recording.id,expectedRevision:recording.revision,agree});}
    requestRecording(id:string){return this.api.request<RecordingSnapshot>(`/v2/voice-rooms/${encodeURIComponent(id)}/recording`,'POST',{});}
    startRecording(id:string,revision:number){return this.api.request<RecordingSnapshot>(`/v2/voice-rooms/${encodeURIComponent(id)}/recording/start`,'POST',{expectedRevision:revision});}
    stopRecording(id:string,revision:number){return this.api.request<RecordingSnapshot>(`/v2/voice-rooms/${encodeURIComponent(id)}/recording/stop`,'POST',{expectedRevision:revision});}
    requestSpeaker(id:string,revision:number){return this.api.request(`/v2/voice-rooms/${encodeURIComponent(id)}/speaker-requests`,'POST',{expectedRevision:revision});}
    approveSpeaker(id:string,wallet:string,revision:number){return this.api.request(`/v2/voice-rooms/${encodeURIComponent(id)}/speakers`,'POST',{wallet,expectedRevision:revision});}
    moderate(id:string,wallet:string,revision:number,action:'mute'|'eject'|'ban',reason:string){return this.api.request(`/v3/voice-rooms/${encodeURIComponent(id)}/moderation-actions`,'POST',{wallet,expectedRevision:revision,action,reason});}
    start(id:string,revision:number){return this.api.request(`/v2/voice-rooms/${encodeURIComponent(id)}/start`,'POST',{expectedRevision:revision});}
    end(id:string,revision:number){return this.api.request(`/v2/voice-rooms/${encodeURIComponent(id)}/end`,'POST',{expectedRevision:revision});}
    async join(id:string,container:HTMLElement,consent?:JoinConsent){
        if(this.room)await this.disconnect();
        const generation=++this.connectionGeneration;
        const grant=await this.api.request<{ticket:string;providerEndpoint:string;expiresAt:string}>(`/v3/voice-rooms/${encodeURIComponent(id)}/tickets`,'POST',{consent});
        if(generation!==this.connectionGeneration)throw new Error('Voice connection was cancelled');
        if(!grant.providerEndpoint.startsWith('wss://')||!Number.isFinite(Date.parse(grant.expiresAt))||Date.parse(grant.expiresAt)<=Date.now())throw new Error('Your voice pass expired, try joining again');
        const room=new Room({adaptiveStream:true,dynacast:true});
        room.on(RoomEvent.TrackSubscribed,(track,_publication,participant)=>{if(this.room!==room||track.kind!==Track.Kind.Audio)return;const elements=this.audio.get(participant.identity)??[];const element=track.attach();element.dataset.voiceParticipant=participant.identity;element.muted=this.blocked.has(participant.identity);container.append(element);elements.push(element);this.audio.set(participant.identity,elements);});
        room.on(RoomEvent.TrackUnsubscribed,track=>{track.detach().forEach(element=>element.remove());});
        room.on(RoomEvent.Disconnected,()=>{if(this.room!==room)return;for(const elements of this.audio.values())elements.forEach(element=>element.remove());this.audio.clear();if(this.room===room){this.room=null;this.connectionChanged?.(false);}});
        this.room=room;try{await room.connect(grant.providerEndpoint,grant.ticket);if(generation!==this.connectionGeneration||this.room!==room){await room.disconnect();throw new Error('Voice connection was cancelled');}await room.startAudio();this.connectionChanged?.(true);}catch(error){if(this.room===room)await this.disconnect();else await room.disconnect();throw error;}
    }
    async microphone(enabled:boolean){if(!this.room)throw new Error('Join the room first');if(enabled&&!this.room.localParticipant.permissions?.canPublish)throw new Error("You can't speak in this room yet");await this.room.localParticipant.setMicrophoneEnabled(enabled);}
    localMute(identity:string){this.blocked.add(identity);for(const element of this.audio.get(identity)??[])element.muted=true;}
    async leave(id:string){await this.disconnect();await this.api.request(`/v2/voice-rooms/${encodeURIComponent(id)}/leave`,'POST',{});}
    async disconnect(){this.connectionGeneration++;const room=this.room;this.room=null;this.connectionChanged?.(false);if(room)await room.disconnect();for(const elements of this.audio.values())elements.forEach(element=>element.remove());this.audio.clear();}
}
