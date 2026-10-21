'use client';
import { useId, useRef, useState, type FormEvent } from 'react';
import { cn } from '@/lib/utils';
import { Button } from '@/ui/primitives';
import { SafetyClient, SafetyRequestError, type ReportCommand } from './safety-client';
import { actionMessage, type SafetyActionState, type PrivateAppealOutcome } from './contracts';
import { AppealStatus } from './appeal-status';

const reportCategories = [
    { value: 'abuse', label: 'Abuse or harassment' },
    { value: 'spam', label: 'Spam' },
    { value: 'fraud', label: 'Scam or fraud' },
    { value: 'other', label: 'Something else' },
] as const;

const fieldClass = 'w-full rounded-sm bg-surface-2 px-3 text-base text-text outline-none transition-colors duration-fast placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-focus disabled:text-text-disabled';

export interface ReportContentProps {
    client: SafetyClient;
    targetType: ReportCommand['targetType'];
    targetId: string;
    onLocalMute?: () => void;
    onLocalBlock?: () => void;
    /** Inside a dialog that already carries the title; drops the section chrome. */
    embedded?: boolean;
}

/**
 * Private report form posting to the safety service. The case reference the
 * service returns is shown after acknowledgement; nothing is claimed before.
 */
export function ReportContent({ client, targetType, targetId, onLocalMute, onLocalBlock, embedded = false }: ReportContentProps) {
    const id = useId();
    const [state, setState] = useState<SafetyActionState>({state:'idle'});
    const [caseId, setCaseId] = useState<string | null>(null);
    const pending = useRef(false);
    async function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault(); if (pending.current) return;
        const fields = new FormData(event.currentTarget); const category = String(fields.get('category') ?? ''); const description = String(fields.get('description') ?? '').trim();
        if (!category || description.length > 4000) return;
        pending.current = true; setState({state:'pending'});
        try { const result = await client.report({targetType,targetId,category,description}); setCaseId(result.ID);setState({state:'acknowledged'}); }
        catch (error) {setState({state:'failed',status:error instanceof SafetyRequestError ? error.status : 503});}
        finally {pending.current = false;}
    }
    const busy = state.state === 'pending';
    return <section className={cn('flex flex-col gap-3', !embedded && 'rounded-md bg-surface-1 p-4')} aria-label="Report">
        {!embedded && <h2 className="text-md font-bold text-text">Report</h2>}
        <p className="text-sm text-text-muted">Only our team sees your report</p>
        {(onLocalMute || onLocalBlock) && <div className="flex flex-wrap gap-2">
            {onLocalMute && <Button variant="secondary" size="sm" onClick={onLocalMute}>Mute</Button>}
            {onLocalBlock && <Button variant="danger-text" size="sm" onClick={onLocalBlock}>Block</Button>}
        </div>}
        {caseId ? <p role="status" className="rounded-sm bg-success-soft px-3 py-2 text-sm text-success">Thanks, we got your report</p> : <form onSubmit={submit} aria-busy={busy || undefined} className="flex flex-col gap-3">
            <fieldset disabled={busy} className="flex flex-col gap-3">
                <div className="flex flex-col gap-1.5">
                    <label htmlFor={`${id}-category`} className="text-sm font-bold text-text">Reason</label>
                    <select id={`${id}-category`} name="category" required className={cn(fieldClass, 'h-control-height-md')}>
                        {reportCategories.map(category => <option key={category.value} value={category.value}>{category.label}</option>)}
                    </select>
                </div>
                <div className="flex flex-col gap-1.5">
                    <label htmlFor={`${id}-description`} className="text-sm font-bold text-text">Details</label>
                    <textarea id={`${id}-description`} name="description" maxLength={4000} rows={4} className={cn(fieldClass, 'min-h-24 resize-y py-2 leading-5')} placeholder="What happened? (optional)" />
                </div>
                <div className="flex justify-end">
                    <Button type="submit" variant="primary" size="md" loading={busy}>Send report</Button>
                </div>
            </fieldset>
        </form>}
        <p role="status" aria-live="polite" className={cn('text-xs', state.state === 'failed' ? 'text-danger' : 'text-text-muted')}>{actionMessage(state)}</p>
    </section>;
}

export function ConnectedAppeal({client,actionId,initialOutcome}: {client: SafetyClient;actionId:string;initialOutcome:PrivateAppealOutcome|null}) {
    const [outcome,setOutcome]=useState(initialOutcome);const [state,setState]=useState<SafetyActionState>({state:'idle'});const pending=useRef(false);
    async function submit(id:string,reason:string){if(pending.current)return;pending.current=true;setState({state:'pending'});try{setOutcome(await client.appeal(id,reason));setState({state:'acknowledged'});}catch(error){setState({state:'failed',status:error instanceof SafetyRequestError?error.status:503});}finally{pending.current=false;}}
    async function refresh(){if(!outcome||pending.current)return;pending.current=true;setState({state:'pending'});try{setOutcome(await client.privateAppeal(outcome.id));setState({state:'idle'});}catch(error){setState({state:'failed',status:error instanceof SafetyRequestError?error.status:503});}finally{pending.current=false;}}
    return <div className="flex flex-col gap-3"><AppealStatus outcome={outcome} actionId={actionId} canAppeal={!outcome} action={state} onAppeal={submit}/>{outcome&&<div><Button variant="secondary" size="sm" disabled={state.state==='pending'} loading={state.state==='pending'} onClick={() => { void refresh(); }}>Refresh</Button></div>}</div>;
}
