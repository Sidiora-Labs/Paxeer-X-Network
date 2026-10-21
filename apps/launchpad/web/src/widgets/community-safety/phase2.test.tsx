// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, expect, test } from 'vitest';
import { RecordingConsent } from '../voice-rooms/recording-consent';
import { ReportContent } from './report-content';
import { SafetyClient } from './safety-client';
import { VoiceClient, type RecordingSnapshot } from '../voice-rooms/voice-client';
Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true});
let root:Root|undefined;
let container:HTMLDivElement;
async function mount(node:React.ReactNode){container=document.createElement('div');document.body.append(container);root=createRoot(container);await act(async()=>{root!.render(node);});}
afterEach(async()=>{if(root)await act(async()=>{root!.unmount();});root=undefined;document.body.replaceChildren();});
const voiceClient=new VoiceClient(new SafetyClient('https://kindle.invalid',()=> 'access-token'));
const recording:RecordingSnapshot={id:'recording-id',roomId:'room-id',state:'recording',purpose:'Community archive',policyVersion:'v2',revision:2,deleteAt:'2026-10-13T00:00:00Z'};
test('persistent indicator displays actual server recording state and exact purpose',async()=>{
    await mount(<RecordingConsent client={voiceClient} roomId="room-id" recording={recording} onUpdate={()=>{}} onLeave={()=>{}}/>);
    expect(container.textContent).toContain('Recording is active.');expect(container.textContent).toContain('Community archive');expect(container.querySelector('time')?.dateTime).toBe(recording.deleteAt);expect(container.querySelectorAll('button')).toHaveLength(1);
});
test('pending consent has explicit agree and decline actions and never claims active recording',async()=>{
    await mount(<RecordingConsent client={voiceClient} roomId="room-id" recording={{...recording,state:'consent_pending'}} onUpdate={()=>{}} onLeave={()=>{}}/>);
    const labels=Array.from(container.querySelectorAll('button')).map(button=>button.textContent);expect(labels).toContain('Decline recording');expect(labels).toContain('I consent to recording for this purpose');expect(container.textContent).toContain('Recording has not started');expect(container.textContent).not.toContain('Recording is active.');
});
test('report form preserves description bounds and exposes no impersonation input',async()=>{
    const client=new SafetyClient('https://kindle.invalid',()=> 'access-token');await mount(<ReportContent client={client} targetType="message" targetId="target"/>);
    expect(container.querySelector('textarea')?.maxLength).toBe(4000);expect(container.querySelector('[name=wallet]')).toBeNull();expect(container.querySelector('[name=banned_by]')).toBeNull();expect(container.textContent).toContain('Only our team sees your report');
});

test('lost authoritative recording state never claims recording is off and retains safe leave advice',async()=>{
    await mount(<RecordingConsent client={voiceClient} roomId="room-id" recording={null} authoritative={false} onUpdate={()=>{}} onLeave={()=>{}}/>);
    expect(container.textContent).toContain("Can't check recording right now");
    expect(container.textContent).not.toContain('Recording is off.');
    expect(container.textContent).not.toContain('Recording is active.');
    expect(container.querySelector('button')).toBeNull();
});
test('reported speaker can be muted and blocked immediately without waiting for the safety service',async()=>{
    let mute=0;let block=0;
    await mount(<ReportContent client={voiceClient.api} targetType="voice_participant" targetId="room-id:participant" onLocalMute={()=>{mute++;}} onLocalBlock={()=>{block++;}}/>);
    const buttons=Array.from(container.querySelectorAll('button'));
    await act(async()=>{buttons.find(button=>button.textContent?.includes('Mute'))!.click();buttons.find(button=>button.textContent?.includes('Block'))!.click();});
    expect(mute).toBe(1);expect(block).toBe(1);
    expect(container.textContent).toContain('Only our team sees your report');
});
