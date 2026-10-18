'use client';
import { useId, type FormEvent } from 'react';
import type { VoiceCapabilities, VoiceEndRequest, VoiceModerateRequest, VoiceReadResponse } from '../../../../../packages/production-contracts/src/types.gen';
import { actionMessage, type SafetyActionState } from '../community-safety/contracts';
interface Participant {
    accountId: string;
    displayName: string;
    revision: string;
}
interface Props {
    snapshot: VoiceReadResponse | null;
    capabilities: VoiceCapabilities | null;
    participants: readonly Participant[];
    action: SafetyActionState;
    onEnd?: (command: VoiceEndRequest) => void;
    onModerate?: (command: VoiceModerateRequest) => void;
}
export function VoiceRoomPanel({ snapshot, capabilities, participants, action, onEnd, onModerate }: Props) {
    const id = useId();
    const room = snapshot?.data.room;
    const trusted = snapshot?.quality === 'complete' && capabilities?.providerQualified === true;
    const pending = action.state === 'pending';
    const canEnd = trusted && capabilities?.end && snapshot?.data.allowedActions.includes('end') && (room?.state === 'live' || room?.state === 'ending') && !!onEnd;
    const moderationActions = (['mute', 'remove', 'ban'] as const).filter(value => snapshot?.data.allowedActions.includes(value));
    const canModerate = trusted && capabilities?.moderate && room?.state === 'live' && !!onModerate && moderationActions.length > 0;
    function moderate(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!canModerate || pending || !room)
            return;
        const fields = new FormData(event.currentTarget);
        const participant = participants.find(item => item.accountId === fields.get('participant'));
        const command = fields.get('action');
        const reason = String(fields.get('reason') ?? '').trim();
        if (!participant || !reason || reason.length > 1000 || !['mute', 'remove', 'ban'].includes(String(command)) || !snapshot?.data.allowedActions.includes(String(command)))
            return;
        onModerate?.({ roomId: room.roomId, participantAccountId: participant.accountId, expectedRevision: participant.revision, action: command as VoiceModerateRequest['action'], reason });
    }
    return <section aria-labelledby={`${id}-heading`} className="rounded-xl bg-surface-2 p-4 space-y-4">
    <h2 id={`${id}-heading`} className="font-bold text-text">Voice room</h2>
    <p role="status" aria-live="polite">{room ? `Room status: ${room.state}` : "Room info can't load right now"}</p>
    {room?.state === 'starting' && <p>Starting the room...</p>}
    {room?.state === 'ending' && <p>Ending the room...</p>}
    {!trusted && <p role="status">Voice controls are not available yet</p>}
    <p className="text-sm text-text">Audio is not available yet. Recording is off</p>
    {canEnd && room && <button type="button" disabled={pending} onClick={() => { if (!pending)
        onEnd?.({ roomId: room.roomId, expectedRevision: room.revision }); }} className="rounded-lg bg-surface-3 px-4 py-2 hover:bg-surface-hover">{room.state === 'ending' ? 'Try ending again' : 'End room'}</button>}
    {canModerate && participants.length > 0 && <form onSubmit={moderate} aria-busy={pending} className="space-y-3"><fieldset disabled={pending} className="space-y-3">
      <label htmlFor={`${id}-participant`} className="block">Participant</label><select id={`${id}-participant`} name="participant" className="rounded-lg bg-surface-3 p-2">{participants.map(item => <option key={item.accountId} value={item.accountId}>{item.displayName}</option>)}</select>
      <label htmlFor={`${id}-action`} className="block">Action</label><select id={`${id}-action`} name="action" className="rounded-lg bg-surface-3 p-2">{moderationActions.map(value => <option key={value} value={value}>{value === 'remove' ? 'Remove from room' : value === 'ban' ? 'Ban participant' : 'Mute participant'}</option>)}</select>
      <label htmlFor={`${id}-reason`} className="block">Reason</label><textarea id={`${id}-reason`} name="reason" required maxLength={1000} className="w-full rounded-lg bg-surface-3 p-2"/>
      <button type="submit" className="rounded-lg bg-surface-3 px-4 py-2 hover:bg-surface-hover">Apply</button>
    </fieldset></form>}
    <p role="status" aria-live="polite">{actionMessage(action)}</p>
  </section>;
}
