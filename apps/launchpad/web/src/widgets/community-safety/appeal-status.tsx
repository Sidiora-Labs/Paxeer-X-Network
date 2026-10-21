'use client';
import { useId, type FormEvent } from 'react';
import { cn } from '@/lib/utils';
import { Button } from '@/ui/primitives';
import { actionMessage, type PrivateAppealOutcome, type SafetyActionState } from './contracts';
interface Props {
    outcome: PrivateAppealOutcome | null;
    actionId: string;
    canAppeal: boolean;
    action: SafetyActionState;
    onAppeal?: (actionId: string, reason: string) => void;
}
const appealStateLabel: Record<PrivateAppealOutcome['state'], string> = { submitted: 'Sent', reviewing: 'In review', upheld: 'Decision stands', reversed: 'Overturned' };
const fieldClass = 'w-full min-h-24 resize-y rounded-sm bg-surface-2 px-3 py-2 text-base leading-5 text-text outline-none transition-colors duration-fast placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-focus disabled:text-text-disabled';
export function AppealStatus({ outcome, actionId, canAppeal, action, onAppeal }: Props) {
    const id = useId();
    function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (outcome || !canAppeal || !onAppeal || action.state === 'pending')
            return;
        const reason = String(new FormData(event.currentTarget).get('reason') ?? '').trim();
        if (reason && reason.length <= 1000)
            onAppeal(actionId, reason);
    }
    const busy = action.state === 'pending';
    return <section aria-labelledby={`${id}-heading`} className="flex flex-col gap-3 rounded-md bg-surface-1 p-4">
    <h2 id={`${id}-heading`} className="text-md font-bold text-text">Your appeal</h2>
    <p className="text-sm text-text-muted">Only you can see your appeal</p>
    {outcome ? <div role="status" className="flex flex-col gap-1 rounded-sm bg-surface-2 p-3 text-sm text-text"><p>Status: <span className="font-bold">{appealStateLabel[outcome.state]}</span></p>{outcome.decidedAt && <><p className="text-text-muted">{outcome.reason}</p><time dateTime={outcome.decidedAt} className="text-xs text-text-faint">{outcome.decidedAt}</time></>}{outcome.state === 'reversed' && <p className="text-text-muted">The action was undone</p>}</div> : canAppeal && onAppeal ? <form onSubmit={submit} aria-busy={busy || undefined} className="flex flex-col gap-3"><div className="flex flex-col gap-1.5"><label htmlFor={`${id}-reason`} className="text-sm font-bold text-text">Why should we take another look?</label><textarea id={`${id}-reason`} name="reason" required maxLength={1000} disabled={busy} className={fieldClass}/></div><div className="flex justify-end"><Button type="submit" variant="primary" size="md" disabled={busy} loading={busy}>Request review</Button></div></form> : <p role="status" className="text-sm text-text-faint">You can't appeal this right now</p>}
    <p role="status" aria-live="polite" className={cn('text-xs', action.state === 'failed' ? 'text-danger' : 'text-text-muted')}>{actionMessage(action)}</p>
  </section>;
}
