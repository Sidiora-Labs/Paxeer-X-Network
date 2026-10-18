'use client';
import { useId, useRef, useState, useEffect, type FormEvent } from 'react';
import { cn } from '@/lib/utils';
import { ShieldCheck } from 'lucide-react';
import { Button, EmptyState } from '@/ui/primitives';
import { SafetyClient, SafetyRequestError } from './safety-client';
import { actionMessage, type SafetyActionState, type SafetyCase, type ResolveReportCommand } from './contracts';
interface Props {
    caseRecord: SafetyCase;
    canResolve: boolean;
    action: SafetyActionState;
    onResolve?: (caseId: string, command: ResolveReportCommand) => void;
}
const fieldClass = 'w-full rounded-sm bg-surface-2 px-3 text-base text-text outline-none transition-colors duration-fast placeholder:text-text-faint focus-visible:outline-2 focus-visible:outline-focus disabled:text-text-disabled';
const labelClass = 'text-sm font-bold text-text';
export function ModerationCasePanel({ caseRecord, canResolve, action, onResolve }: Props) {
    const id = useId();
    const submitted = useRef(false);
    if (action.state !== 'pending')
        submitted.current = false;
    const mutable = caseRecord.State === 'open' || caseRecord.State === 'triaged';
    const available = canResolve && mutable && !!onResolve && Number.isSafeInteger(caseRecord.Revision) && caseRecord.Revision > 0;
    function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!available || action.state === 'pending' || submitted.current)
            return;
        const fields = new FormData(event.currentTarget);
        const status = fields.get('status');
        const reason = String(fields.get('reason') ?? '').trim();
        const policy = String(fields.get('policy') ?? '').trim();
        if (!reason || reason.length > 1000 || !policy || policy.length > 160 || !['triaged', 'actioned', 'dismissed'].includes(String(status)))
            return;
        if (caseRecord.State === 'triaged' && status === 'triaged')
            return;
        submitted.current = true;
        onResolve?.(caseRecord.ID, { status: status as ResolveReportCommand['status'], expected_revision: caseRecord.Revision, reason, policy_version: policy });
    }
    const busy = action.state === 'pending';
    return <section aria-labelledby={`${id}-heading`} className="flex flex-col gap-4 rounded-md bg-surface-1 p-4">
    <h2 id={`${id}-heading`} className="text-md font-bold text-text">Review report</h2>
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 rounded-sm bg-surface-2 p-3 text-sm"><dt className="text-text-faint">Content</dt><dd className="break-all text-text">{caseRecord.TargetType}: {caseRecord.TargetID}</dd><dt className="text-text-faint">Status</dt><dd className="text-text">{caseRecord.State}</dd><dt className="text-text-faint">Revision</dt><dd className="text-text" data-numeric>{caseRecord.Revision}</dd></dl>
    {!available && <p role="status" className="text-sm text-text-faint">{mutable ? "You can't review this report" : 'This report is closed or under appeal'}</p>}
    {available && <form onSubmit={submit} aria-busy={busy || undefined} className="flex flex-col gap-3">
      <fieldset disabled={busy} className="flex flex-col gap-3">
        <div className="flex flex-col gap-1.5">
          <label htmlFor={`${id}-status`} className={labelClass}>Decision</label>
          <select id={`${id}-status`} name="status" className={cn(fieldClass, 'h-control-height-md')} defaultValue={caseRecord.State === 'triaged' ? 'actioned' : 'triaged'}>
            {caseRecord.State === 'open' && <option value="triaged">Mark for review</option>}<option value="actioned">Take action</option><option value="dismissed">Dismiss report</option>
          </select>
        </div>
        <div className="flex flex-col gap-1.5"><label htmlFor={`${id}-reason`} className={labelClass}>Reason</label><textarea id={`${id}-reason`} name="reason" required maxLength={1000} className={cn(fieldClass, 'min-h-24 resize-y py-2 leading-5')}/></div>
        <div className="flex flex-col gap-1.5"><label htmlFor={`${id}-policy`} className={labelClass}>Policy version</label><input id={`${id}-policy`} name="policy" required maxLength={160} className={cn(fieldClass, 'h-control-height-md')}/></div>
        <div className="flex justify-end"><Button type="submit" variant="primary" size="md" loading={busy}>Submit decision</Button></div>
      </fieldset>
    </form>}
    <p role="status" aria-live="polite" className={cn('text-xs', action.state === 'failed' ? 'text-danger' : 'text-text-muted')}>{actionMessage(action)}</p>
  </section>;
}

export function ConnectedModerationCases({client,canResolve}:{client:SafetyClient;canResolve:boolean}){
 const [cases,setCases]=useState<SafetyCase[]>([]);const [loaded,setLoaded]=useState(false);const [state,setState]=useState<SafetyActionState>({state:'idle'});const lock=useRef(false);
 useEffect(()=>{const abort=new AbortController();client.cases(abort.signal).then(rows=>{setCases(rows??[]);setLoaded(true);}).catch(error=>{if(!abort.signal.aborted)setState({state:'failed',status:error instanceof SafetyRequestError?error.status:503});});return()=>abort.abort();},[client]);
 async function resolve(id:string,command:ResolveReportCommand){if(lock.current)return;lock.current=true;setState({state:'pending'});try{await client.resolve(id,command);setCases((await client.cases())??[]);setState({state:'acknowledged'});}catch(error){setState({state:'failed',status:error instanceof SafetyRequestError?error.status:503});}finally{lock.current=false;}}
 return <section aria-label="Moderation queue" className="flex flex-col gap-3"><h2 className="text-md font-bold text-text">Reports to review</h2>{loaded&&cases.length===0&&<EmptyState icon={ShieldCheck} title="No reports to review" body="New reports show up here" announce/>}{cases.map(record=><ModerationCasePanel key={record.ID} caseRecord={record} canResolve={canResolve} action={state} onResolve={resolve}/>)}<p role="status" className={cn('text-xs', state.state === 'failed' ? 'text-danger' : 'text-text-muted')}>{actionMessage(state)}</p></section>;
}
