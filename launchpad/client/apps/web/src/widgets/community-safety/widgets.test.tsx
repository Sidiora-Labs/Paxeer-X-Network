// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, expect, test } from 'vitest';
import { ModerationCasePanel } from './moderation-case';
import { AppealStatus } from './appeal-status';
import { VoiceRoomPanel } from '../voice-rooms/voice-room';
import type { SafetyCase, ResolveReportCommand } from './contracts';
import type { VoiceCapabilities, VoiceEndRequest, VoiceModerateRequest, VoiceReadResponse } from '../../../../../packages/production-contracts/src/types.gen';
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
let root: Root | undefined;
let container: HTMLDivElement;
async function mount(element: React.ReactNode) {
    container = document.createElement('div');
    document.body.append(container);
    root = createRoot(container);
    await act(async () => { root!.render(element); });
}
afterEach(async () => {
    if (root)
        await act(async () => { root!.unmount(); });
    root = undefined;
    document.body.replaceChildren();
});
const caseRecord: SafetyCase = { ID: 'case-1', TargetType: 'message', TargetID: 'message-1', PoolID: 'pool-1', State: 'open', Revision: 7 };
const roomResponse: VoiceReadResponse = {
    data: { room: { roomId: 'room-1', hostAccountId: 'host-1', scopeId: 'pool-1', scopeType: 'pool', visibility: 'public', state: 'live', revision: '9007199254740993' }, allowedActions: ['end', 'mute', 'remove'] },
    requestId: 'request-1', generatedAt: '2026-10-12T00:00:00.000Z', nextCursor: null, quality: 'complete', revision: '1', watermark: null,
};
const capabilities: VoiceCapabilities = { end: true, listen: true, moderate: true, providerQualified: true, requestSpeaker: true, speak: true };
test('reviewer and missing authenticated action binding expose no mutation form', async () => {
    await mount(<ModerationCasePanel caseRecord={caseRecord} canResolve={false} action={{ state: 'idle' }}/>);
    expect(container.querySelector('form')).toBeNull();
    expect(container.textContent).toContain("You can't review this report");
    expect(container.textContent).not.toContain('reporterWallet');
});
test('native report form emits the exact revisioned command, without changing authoritative case status', async () => {
    const commands: Array<{
        id: string;
        command: ResolveReportCommand;
    }> = [];
    await mount(<ModerationCasePanel caseRecord={caseRecord} canResolve action={{ state: 'idle' }} onResolve={(id, command) => { commands.push({ id, command }); }}/>);
    container.querySelector<HTMLTextAreaElement>('textarea')!.value = 'Policy review';
    container.querySelector<HTMLInputElement>('input')!.value = 'v3';
    container.querySelector<HTMLSelectElement>('select')!.value = 'actioned';
    await act(async () => { container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); });
    expect(commands).toEqual([{ id: 'case-1', command: { status: 'actioned', expected_revision: 7, reason: 'Policy review', policy_version: 'v3' } }]);
    expect(container.querySelector('dd:nth-of-type(2)')?.textContent).toBe('open');
    expect(container.textContent).not.toContain('recorded the request');
});
test('pending report cannot emit a second mutation and announces uncertainty', async () => {
    const commands: ResolveReportCommand[] = [];
    await mount(<ModerationCasePanel caseRecord={caseRecord} canResolve action={{ state: 'pending' }} onResolve={(_, command) => { commands.push(command); }}/>);
    expect(container.querySelector('form')?.getAttribute('aria-busy')).toBe('true');
    expect(container.querySelector('button')?.matches(':disabled')).toBe(true);
    await act(async () => { container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); });
    expect(commands).toHaveLength(0);
    expect(container.textContent).toContain('Sending...');
});
test('revision conflict instructs refresh without optimistic action state', async () => {
    await mount(<ModerationCasePanel caseRecord={caseRecord} canResolve action={{ state: 'failed', status: 409 }}/>);
    expect(container.textContent).toContain('Refresh and try again');
    expect(container.textContent).toContain('open');
});
test('existing private appeal suppresses duplicate submissions and presents reason and time', async () => {
    await mount(<AppealStatus actionId="action-1" canAppeal action={{ state: 'idle' }} outcome={{ id: 'appeal-1', actionId: 'action-1', state: 'upheld', revision: 3, reason: 'Independent review complete', decidedAt: '2026-10-12T00:00:00Z' }}/>);
    expect(container.querySelector('form')).toBeNull();
    expect(container.textContent).toContain('Independent review complete');
    expect(container.querySelector('time')?.dateTime).toBe('2026-10-12T00:00:00Z');
});
test('unqualified provider never exposes activation or moderation controls', async () => {
    await mount(<VoiceRoomPanel snapshot={roomResponse} capabilities={{ ...capabilities, providerQualified: false }} participants={[]} action={{ state: 'idle' }}/>);
    expect(container.querySelector('button')).toBeNull();
    expect(container.querySelector('form')).toBeNull();
    expect(container.textContent).toContain('Voice controls are not available yet');
    expect(container.textContent).toContain('Recording is off');
});
test('ending room retains pending cleanup and submits exact decimal revision without claiming ended', async () => {
    const commands: VoiceEndRequest[] = [];
    const ending: VoiceReadResponse = { ...roomResponse, data: { ...roomResponse.data, room: { ...roomResponse.data.room, state: 'ending' } } };
    await mount(<VoiceRoomPanel snapshot={ending} capabilities={capabilities} participants={[]} action={{ state: 'idle' }} onEnd={command => { commands.push(command); }}/>);
    expect(container.textContent).toContain('Ending the room...');
    await act(async () => { container.querySelector<HTMLButtonElement>('button')!.click(); });
    expect(commands).toEqual([{ roomId: 'room-1', expectedRevision: '9007199254740993' }]);
    expect(container.textContent).toContain('Room status: ending');
    expect(container.textContent).not.toContain('Room status: ended');
});
test('participant picker emits only allowed moderation action and participant revision', async () => {
    const commands: VoiceModerateRequest[] = [];
    await mount(<VoiceRoomPanel snapshot={roomResponse} capabilities={capabilities} participants={[{ accountId: 'participant-1', displayName: 'Current speaker', revision: '12' }]} action={{ state: 'idle' }} onModerate={command => { commands.push(command); }}/>);
    expect(container.textContent).toContain('Current speaker');
    expect(Array.from(container.querySelectorAll('select[name=action] option')).map(item => item.getAttribute('value'))).toEqual(['mute', 'remove']);
    container.querySelector<HTMLTextAreaElement>('textarea')!.value = 'Reported abuse';
    await act(async () => { container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); });
    expect(commands).toEqual([{ roomId: 'room-1', participantAccountId: 'participant-1', expectedRevision: '12', action: 'mute', reason: 'Reported abuse' }]);
    expect(container.textContent).not.toContain('audio stopped');
});
test('stale room snapshot cannot grant actions even when capabilities claim permission', async () => {
    await mount(<VoiceRoomPanel snapshot={{ ...roomResponse, quality: 'stale' }} capabilities={capabilities} participants={[]} action={{ state: 'idle' }} onEnd={() => { throw new Error('Stale authority emitted an action'); }}/>);
    expect(container.querySelector('button')).toBeNull();
    expect(container.textContent).toContain('Voice controls are not available yet');
});
