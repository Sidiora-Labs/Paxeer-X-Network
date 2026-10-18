'use client';
import { useId, useRef, useState, type DragEvent, type FormEvent } from 'react';
import { ImagePlus, RefreshCw, Send, Trash2, Upload } from 'lucide-react';
import { Button, FormField, IconButton, NoticeBar, UnavailableState } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { openAuthModal } from '@/shell/auth-modal-store';
import { EVIDENCE_MAX_FILES, EVIDENCE_TYPES, SUBMISSION_MAX, useEvidenceUploads, useSubmitBounty, type EvidenceItem } from './use-bounty-mutations';
import type { Bounty } from './bounties-contract';
import { userFacingMessage } from '@/core/user-facing-error';

const statusText: Record<EvidenceItem['status'], string> = {
  uploading: 'Uploading',
  review: 'Checking image',
  approved: 'Ready',
  rejected: 'Not approved',
  failed: 'Upload failed',
};

function EvidenceRow({ item, onRemove, onRetry, onRefresh }: { item: EvidenceItem; onRemove(): void; onRetry(): void; onRefresh(): void }) {
  return (
    <li className="flex items-center gap-3 rounded-sm bg-surface-3 p-2">
      <img src={item.previewUrl} alt="" className="size-12 shrink-0 rounded-sm object-cover" />
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-sm text-text">{item.file.name}</span>
        <span role="status" className={cn('text-xs', item.status === 'approved' ? 'text-success' : item.status === 'rejected' || item.status === 'failed' ? 'text-danger' : 'text-text-faint')}>
          {statusText[item.status]}{item.error ? `: ${item.error}` : ''}
        </span>
      </div>
      {item.status === 'failed' ? <IconButton size="sm" icon={<RefreshCw />} label={`Retry upload of ${item.file.name}`} onClick={onRetry} /> : null}
      {item.status === 'review' ? <IconButton size="sm" icon={<RefreshCw />} label={`Check ${item.file.name} again`} onClick={onRefresh} /> : null}
      <IconButton size="sm" icon={<Trash2 />} label={`Remove ${item.file.name}`} disabled={item.status === 'uploading'} onClick={onRemove} />
    </li>
  );
}

export interface SubmissionComposerProps {
  bounty: Bounty;
  className?: string;
}

/**
 * Submission composer: proof text plus up to four evidence images. Images go
 * through the authenticated account asset upload; only approved assets with
 * a public URL are sent as evidenceUrls with IF_BOUNTY_SUBMIT.
 */
export function SubmissionComposer({ bounty, className }: SubmissionComposerProps) {
  const id = useId();
  const inputRef = useRef<HTMLInputElement | null>(null);
  const { signedIn, mutation } = useSubmitBounty(bounty.bountyId);
  const evidence = useEvidenceUploads();
  const [body, setBody] = useState('');
  const [dragging, setDragging] = useState(false);
  const [fileProblem, setFileProblem] = useState<string | null>(null);
  const [attempted, setAttempted] = useState(false);
  const clientId = useRef<string | null>(null);
  const length = Array.from(body).length;
  const bodyError = !body.trim() ? 'Tell the creator what you did' : length > SUBMISSION_MAX ? `Keep it under ${SUBMISSION_MAX} characters` : null;
  const full = evidence.items.length >= EVIDENCE_MAX_FILES;

  function pick(files: FileList | null) {
    if (!files || files.length === 0) return;
    if (!signedIn) { openAuthModal('Sign in to add images'); return; }
    setFileProblem(evidence.add(Array.from(files)));
  }

  function drop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setDragging(false);
    pick(event.dataTransfer.files);
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    setAttempted(true);
    if (!signedIn) { openAuthModal('Sign in to submit to this bounty'); return; }
    if (bodyError || !evidence.ready || mutation.isPending) return;
    clientId.current ??= crypto.randomUUID();
    mutation.mutate(
      { body: body.trim(), clientSubmissionId: clientId.current, evidenceUrls: evidence.evidenceUrls },
      { onSuccess: () => { clientId.current = null; setBody(''); setAttempted(false); evidence.clear(); } },
    );
  }

  const failure = mutation.error;
  const pendingEvidence = evidence.items.some(item => item.status === 'uploading' || item.status === 'review');

  return (
    <form noValidate onSubmit={submit} aria-labelledby={`${id}-heading`} className={cn('flex flex-col gap-4 rounded-lg bg-surface-1 p-4', className)}>
      <h2 id={`${id}-heading`} className="text-md font-bold text-text">Submit your entry</h2>
      <FormField
        id={`${id}-body`}
        label="Proof"
        multiline
        error={attempted ? bodyError : null}
        helper="What you did and where to see it"
        counter={{ count: length, max: SUBMISSION_MAX }}
        textareaProps={{ value: body, onChange: event => { setBody(event.target.value); if (mutation.isSuccess) mutation.reset(); }, rows: 4 }}
      />
      <div className="flex flex-col gap-2">
        <span className="text-sm font-bold text-text">Images <span className="font-semibold text-text-faint">(optional)</span></span>
        <div
          onDragOver={event => { event.preventDefault(); if (!full) setDragging(true); }}
          onDragLeave={() => setDragging(false)}
          onDrop={drop}
          className={cn('flex min-h-30 flex-col items-center justify-center gap-2 rounded-lg p-4 text-center transition-colors duration-fast', dragging ? 'bg-accent-soft' : fileProblem ? 'bg-danger-soft' : 'bg-surface-2')}
        >
          <ImagePlus aria-hidden="true" className={cn('size-10', dragging ? 'text-accent' : 'text-text-faint')} />
          <p className={cn('text-md', dragging ? 'text-accent' : 'text-text')}>{full ? 'Image limit reached' : 'Drop images here'}</p>
          <p className="text-xs text-text-faint">PNG, JPEG or WebP, up to 6 MB each, max {EVIDENCE_MAX_FILES}</p>
          <Button type="button" variant="primary" size="sm" icon={<Upload />} disabled={full} onClick={() => inputRef.current?.click()}>Select images</Button>
          <input
            ref={inputRef}
            type="file"
            multiple
            accept={EVIDENCE_TYPES.join(',')}
            className="sr-only"
            tabIndex={-1}
            aria-label="Add images"
            onChange={event => { pick(event.target.files); event.target.value = ''; }}
          />
        </div>
        {fileProblem ? <p role="alert" className="text-xs text-danger">{fileProblem}</p> : null}
        {evidence.items.length > 0 ? (
          <ul aria-label="Added images" className="flex flex-col gap-2">
            {evidence.items.map(item => (
              <EvidenceRow key={item.id} item={item} onRemove={() => evidence.remove(item.id)} onRetry={() => evidence.retry(item.id)} onRefresh={() => void evidence.refresh(item.id)} />
            ))}
          </ul>
        ) : null}
      </div>

      {failure && failure.state === 'unsupported' ? (
        <UnavailableState title="Submitting" operation={failure.operation} status={failure.status} message="Try again in a moment" />
      ) : null}
      {failure && failure.state === 'error' ? <NoticeBar tone="danger" announce title="Couldn't submit">{userFacingMessage(failure.error)}</NoticeBar> : null}
      {mutation.isSuccess ? <NoticeBar tone="success" announce>Submitted! The creator will review it</NoticeBar> : null}

      <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-end">
        {pendingEvidence ? <p className="text-xs text-text-faint">Waiting for your images to be checked...</p> : null}
        <Button type="submit" variant="primary" size="md" icon={<Send />} loading={mutation.isPending} disabled={signedIn && (!evidence.ready || evidence.items.some(item => item.status === 'rejected'))} className="w-full md:w-auto">
          {signedIn ? 'Submit' : 'Sign in to submit'}
        </Button>
      </div>
    </form>
  );
}
