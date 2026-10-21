'use client';
import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { Check, Copy, Eye, EyeOff, KeyRound, Radio, RefreshCw, RotateCcw, Square } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, FormField, IconButton, KeyValueRow, NoticeBar, Toggle, humanErrorText } from '@/ui/primitives';
import type { PrivateIngestKey } from '@/core/clients/livestream-api';
import { StreamPlayer } from './stream-player';
import { credentialRemainingMs, liveElapsedMs, statusLabel, TITLE_MAX, useCreatorStream, type CreatorStream, type TimelineStep } from './use-creator-stream';

export interface CreatorConsoleProps { poolAddress: string; className?: string }

/** Standalone console: owns the stream state machine for the pool. */
export function CreatorConsole({ poolAddress, className }: CreatorConsoleProps) {
  const stream = useCreatorStream(poolAddress);
  return <CreatorConsoleView poolAddress={poolAddress} stream={stream} className={className} />;
}

export default CreatorConsole;

export interface CreatorConsoleViewProps { poolAddress: string; stream: CreatorStream; className?: string }

const mono = 'font-[family-name:var(--sl-font-mono)]';

export function clockLabel(ms: number): string {
  const total = Math.floor(ms / 1000);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const mmss = `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
  return hours > 0 ? `${hours}:${mmss}` : mmss;
}

function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);
  return now;
}

export function CreatorConsoleView({ poolAddress, stream, className }: CreatorConsoleViewProps) {
  if (!stream.signedIn) {
    return (
      <section aria-label="Your stream" className={cn('flex flex-col gap-4', className)}>
        <NoticeBar
          tone="info"
          variant="card"
          surface={2}
          title="Sign in to go live"
          action={
            <Button variant="primary" loading={stream.authenticating} onClick={() => void stream.authenticate()}>
              Sign in with your wallet
            </Button>
          }
        >
          Sign in with the wallet that created this coin to start a stream
        </NoticeBar>
        {stream.authError ? <p role="alert" className="text-sm text-danger">{humanErrorText(stream.authError, "Couldn't sign in, try again")}</p> : null}
      </section>
    );
  }
  const { operation, availability, pending, credential } = stream;
  const previewable = operation !== null && (operation.status === 'ready' || operation.status === 'live' || operation.status === 'ending');
  const submitAction = operation ? 'edit' : 'create';
  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void stream.run(submitAction);
  };
  return (
    <section aria-label="Your stream" className={cn('flex flex-col gap-5', className)}>
      {!stream.walletReady ? (
        <NoticeBar tone="warning" surface={2} announce>Switch to your signed-in wallet on Paxeer to manage this stream</NoticeBar>
      ) : null}

      <form onSubmit={onSubmit} className="flex flex-col gap-3" aria-busy={pending === 'create' || pending === 'edit' || undefined}>
        <FormField
          id="creator-stream-title"
          label="Stream title"
          surface="surface-2"
          helper={operation ? 'Shown on the live page and your coin page while you stream' : 'Give people a reason to tune in'}
          counter={{ count: [...stream.title].length, max: TITLE_MAX }}
          inputProps={{
            value: stream.title,
            onChange: event => stream.setTitle(event.target.value),
            required: true,
            maxLength: TITLE_MAX,
            disabled: pending !== null || (operation !== null && !availability.edit),
            autoComplete: 'off',
            placeholder: 'What are you streaming?',
          }}
        />
        {operation ? (
          <div className="flex items-center justify-between gap-3 rounded-sm bg-surface-3 px-3 py-2">
            <div className="flex flex-col">
              <span id="creator-stream-private" className="text-sm font-bold text-text">Private stream</span>
              <span id="creator-stream-private-help" className="text-xs text-text-faint">Only you can watch</span>
            </div>
            <Toggle checked={stream.privatePlayback} onChange={stream.setPrivatePlayback} labelledBy="creator-stream-private" describedBy="creator-stream-private-help" disabled={pending !== null || !availability.edit} />
          </div>
        ) : null}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-xs text-text-faint">{stream.discovering ? 'Checking for your stream...' : operation ? 'Your stream' : 'One live stream per coin'}</p>
          <Button
            type="submit"
            variant={operation ? 'secondary' : 'primary'}
            loading={pending === submitAction}
            disabled={pending !== null || stream.title.trim().length === 0 || (operation !== null && !availability.edit)}
          >
            {operation ? 'Save changes' : 'Create stream'}
          </Button>
        </div>
      </form>

      {operation ? (
        <div className="flex flex-col gap-3">
          <StatusTimeline steps={stream.timeline.steps} halted={stream.timeline.halted} />
          <p role="status" className={cn('text-sm', stream.timeline.halted ? 'text-danger' : 'text-text-muted')}>
            {statusLabel(operation.status)}
            {operation.retryAt ? ` Try again after ${new Date(operation.retryAt).toLocaleTimeString()}` : ''}
          </p>
          {stream.replacing ? <p role="status" className="text-xs text-text-faint">Resetting your stream key...</p> : null}
        </div>
      ) : null}

      {operation && previewable ? (
        <div className="flex flex-col gap-3">
          <StreamPlayer
            streamId={operation.streamId}
            accessToken={stream.accessToken ?? undefined}
            muted
            autoplay
            statusLine
            className="aspect-video w-full overflow-hidden rounded-md bg-surface-3"
          />
          {operation.status === 'live' || operation.status === 'ending' ? <LiveStats stream={stream} /> : null}
        </div>
      ) : null}

      {operation ? (
        <div className="flex flex-wrap gap-2" role="group" aria-label="Stream controls">
          <Button variant="primary" icon={<Radio />} disabled={pending !== null || !availability.start} loading={pending === 'start'} onClick={() => void stream.run('start')}>Go live</Button>
          <Button variant="danger" icon={<Square />} disabled={pending !== null || !availability.end} loading={pending === 'end'} onClick={() => void stream.run('end')}>End stream</Button>
          {credential ? (
            <Button variant="secondary" icon={<EyeOff />} onClick={stream.hideCredential}>Hide stream key</Button>
          ) : (
            <Button variant="secondary" icon={<KeyRound />} disabled={pending !== null || !availability.credential} loading={pending === 'credential'} onClick={() => void stream.run('credential')}>Show stream key</Button>
          )}
          <Button variant="secondary" icon={<RefreshCw />} disabled={pending !== null || !availability.rotate} loading={pending === 'rotate'} onClick={() => void stream.run('rotate')}>Reset stream key</Button>
          {operation.status === 'suspended' || operation.status === 'ended' ? (
            <Button variant="secondary" icon={<RotateCcw />} disabled={pending !== null || !availability.restore} loading={pending === 'restore'} onClick={() => void stream.run('restore')}>Ask to restore</Button>
          ) : null}
        </div>
      ) : null}

      {credential ? <CredentialPanel credential={credential} onHide={stream.hideCredential} /> : null}

      {stream.error ? (
        <NoticeBar tone="danger" surface={2} announce onDismiss={stream.dismissError}>{humanErrorText(stream.error)}</NoticeBar>
      ) : null}
    </section>
  );
}

function StatusTimeline({ steps, halted }: { steps: TimelineStep[]; halted: boolean }) {
  return (
    <ol aria-label="Stream progress" className="flex flex-wrap items-center gap-x-2 gap-y-1">
      {steps.map((step, index) => (
        <li key={step.id} className="flex items-center gap-2" aria-current={step.state === 'current' ? 'step' : undefined}>
          {index > 0 ? <span aria-hidden="true" className={cn('h-0.5 w-4 rounded-pill', step.state === 'upcoming' ? 'bg-surface-active' : 'bg-accent')} /> : null}
          <span className="flex items-center gap-1.5">
            <span
              aria-hidden="true"
              className={cn(
                'size-2 rounded-pill',
                step.state === 'upcoming' ? 'bg-surface-active' : halted && step.state === 'current' ? 'bg-danger' : 'bg-accent',
              )}
            />
            <span className={cn('text-xs', step.state === 'current' ? 'font-bold text-text' : step.state === 'done' ? 'text-text-muted' : 'text-text-faint')}>
              {step.label}
            </span>
          </span>
        </li>
      ))}
    </ol>
  );
}

function LiveStats({ stream }: { stream: CreatorStream }) {
  const now = useNow(true);
  const elapsed = liveElapsedMs(stream.publicStream, stream.liveSince, now);
  const viewers = stream.publicStream?.viewerCount;
  return (
    <dl className="grid grid-cols-2 gap-x-6 rounded-md bg-surface-3 px-4 py-1">
      <KeyValueRow label="Viewers" value={viewers === null || viewers === undefined ? null : viewers.toLocaleString()} loading={stream.publicStream === null} />
      <KeyValueRow label="Live for" value={elapsed === null ? null : <time>{clockLabel(elapsed)}</time>} />
    </dl>
  );
}

type CopyTarget = 'endpoint' | 'key';

function CredentialPanel({ credential, onHide }: { credential: PrivateIngestKey; onHide: () => void }) {
  const [revealed, setRevealed] = useState(false);
  const [copied, setCopied] = useState<CopyTarget | null>(null);
  const [copyError, setCopyError] = useState<string | null>(null);
  const now = useNow(true);
  const remaining = credentialRemainingMs(credential, now);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(null), 1800);
    return () => window.clearTimeout(timer);
  }, [copied]);
  const copy = async (target: CopyTarget) => {
    const value = target === 'endpoint' ? credential.ingestEndpoint : credential.streamKey;
    try {
      await navigator.clipboard.writeText(value);
      setCopyError(null);
      setCopied(target);
    } catch {
      setCopyError("Couldn't copy. Show the key and copy it by hand");
    }
  };
  const masked = '•'.repeat(Math.min(24, credential.streamKey.length));
  return (
    <section aria-label="Stream key" className="flex flex-col gap-3 rounded-md bg-surface-3 p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex flex-col">
          <h3 className="text-sm font-bold text-text">Stream key</h3>
          <p className="text-xs text-text-muted">Paste both into OBS or your streaming app. Keep the key private and reset it if it leaks</p>
        </div>
        <p role="status" className="text-xs tabular-nums text-text-faint">Hides in {clockLabel(remaining)}</p>
      </div>
      <dl className="flex flex-col">
        <CredentialRow
          label="Server"
          value={credential.ingestEndpoint}
          copied={copied === 'endpoint'}
          onCopy={() => void copy('endpoint')}
        />
        <CredentialRow
          label="Stream key"
          value={revealed ? credential.streamKey : masked}
          copied={copied === 'key'}
          onCopy={() => void copy('key')}
          reveal={
            <IconButton
              label={revealed ? 'Hide key' : 'Show key'}
              icon={revealed ? <EyeOff /> : <Eye />}
              size="sm"
              pressed={revealed}
              onClick={() => setRevealed(value => !value)}
            />
          }
        />
      </dl>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p role="status" className="text-xs text-text-faint">{copied ? 'Copied' : copyError ?? ''}</p>
        <Button variant="ghost" size="sm" onClick={onHide}>Hide</Button>
      </div>
    </section>
  );
}

function CredentialRow({ label, value, copied, onCopy, reveal }: { label: string; value: string; copied: boolean; onCopy: () => void; reveal?: ReactNode }) {
  return (
    <div className="flex min-h-10 items-center justify-between gap-3 py-1">
      <dt className="shrink-0 text-sm text-text-faint">{label}</dt>
      <dd className="flex min-w-0 items-center gap-1">
        <code className={cn('min-w-0 truncate text-sm text-text', mono)} title={value}>{value}</code>
        {reveal}
        <IconButton label={copied ? `${label} copied` : `Copy ${label.toLowerCase()}`} icon={copied ? <Check /> : <Copy />} size="sm" onClick={onCopy} />
      </dd>
    </div>
  );
}
