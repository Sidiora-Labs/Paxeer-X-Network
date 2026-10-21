'use client';
import { useId, useState, type FormEvent, type KeyboardEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { CapabilityUnavailableError, HttpError } from '@kindle/sdk';
import { Megaphone } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useSessionOperations } from '@/core/market/operations';
import { isSignedOutError, toUnsupported, type Unsupported } from '@/core/market/unsupported';
import { openAuthModal } from '@/shell/auth-modal-store';
import { Button, FormField, SegmentedControl, SelectPill, UnavailableState } from '@/ui/primitives';
import { validateInteractionText } from '@/widgets/community-interactions/plain-content';
import type { CalloutDirection } from './callout-model';

export const calloutBodyLimit = 2000;
const operation = 'IF_CONNECTED_DISCOVERY_CALLOUT_CREATE';
const walletPattern = /^0x[0-9a-fA-F]{40}$/;

type Disclosure = 'organic' | 'creator' | 'sponsored';
type SubmitState = { state: 'idle' } | { state: 'posting' } | { state: 'posted'; id: string } | { state: 'error'; message: string } | Unsupported;

export interface CalloutComposerProps {
  poolAddress: string;
  /** Called with the new callout id after the discovery service accepts it. */
  onPosted?: (id: string) => void;
  /** inline sits under the panel header on surface-1; modal sits on surface-2. */
  variant?: 'inline' | 'modal';
  className?: string;
}

export function describeCreateError(error: unknown): SubmitState {
  const unsupported = toUnsupported(operation, error);
  if (unsupported) return unsupported;
  // An expired or missing session asks the viewer to sign in again; a 403 is a real refusal and keeps its code.
  if (isSignedOutError(error) && !(error instanceof HttpError && error.status === 403)) return { state: 'error', message: 'Sign in to post a callout' };
  if (error instanceof HttpError) return { state: 'error', message: error.status === 403 ? "You can't post this callout" : error.status === 400 || error.status === 422 ? 'Your callout was not accepted, check it and try again' : "Couldn't post your callout, try again" };
  if (error instanceof CapabilityUnavailableError) return { state: 'unsupported', operation, status: error.status, message: error.message };
  console.error('[callouts] post failed', error);
  return { state: 'error', message: "Couldn't post your callout, try again" };
}

/**
 * Callout composer (components.md 35). Posts through
 * IF_CONNECTED_DISCOVERY_CALLOUT_CREATE and surfaces the real response: the
 * 503 DEPENDENCY_UNAVAILABLE answered until a discovery runtime is configured
 * renders as an unavailable state, never as a fake success.
 */
export function CalloutComposer({ poolAddress, onPosted, variant = 'inline', className }: CalloutComposerProps) {
  const id = useId();
  const operations = useSessionOperations();
  const queryClient = useQueryClient();
  const [body, setBody] = useState('');
  const [direction, setDirection] = useState<CalloutDirection>('bullish');
  const [disclosure, setDisclosure] = useState<Disclosure>('organic');
  const [position, setPosition] = useState('');
  const [submit, setSubmit] = useState<SubmitState>({ state: 'idle' });
  const pool = walletPattern.test(poolAddress) ? poolAddress.toLowerCase() : null;
  const length = Array.from(body).length;
  const valid = pool !== null && validateInteractionText(body) && Array.from(position).length <= 160;
  const posting = submit.state === 'posting';
  const signedIn = operations !== null;

  async function post(event?: FormEvent) {
    event?.preventDefault();
    if (!signedIn) { openAuthModal('Sign in to post a callout'); return; }
    if (!valid || posting || !pool) return;
    setSubmit({ state: 'posting' });
    const calloutId = crypto.randomUUID();
    try {
      const created = await operations.call(operation, { body: { ID: calloutId, PoolAddress: pool, Body: body, Direction: direction, Disclosure: disclosure, ...(position.trim() ? { PositionDisclosure: position.trim() } : {}) } });
      setSubmit({ state: 'posted', id: created.ID });
      setBody('');
      setPosition('');
      void queryClient.invalidateQueries({ predicate: query => query.queryKey[1] === 'feed' || query.queryKey[1] === 'callouts' });
      onPosted?.(created.ID);
    } catch (error) {
      setSubmit(describeCreateError(error));
    }
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); void post(); }
  }

  const surface = variant === 'modal' ? 'surface-2' : 'surface-1';
  return (
    <form onSubmit={event => { void post(event); }} aria-busy={posting || undefined} aria-label="Post a callout" className={cn('flex flex-col gap-3 rounded-md bg-surface-1 p-3', variant === 'modal' && 'bg-surface-2', className)}>
      <FormField
        id={`${id}-body`}
        label="Callout"
        surface={surface}
        multiline
        counter={{ count: length, max: calloutBodyLimit }}
        helper="Cmd/Ctrl+Enter to post"
        error={body.length > 0 && !validateInteractionText(body) ? 'Keep it under 2,000 characters' : null}
        textareaProps={{ value: body, onChange: event => setBody(event.target.value), onKeyDown, placeholder: 'What are you seeing on this coin?', disabled: posting, rows: 3, maxLength: calloutBodyLimit * 2 }}
      />
      <div className="flex flex-wrap items-center gap-2">
        <SegmentedControl
          label="Direction"
          value={direction}
          onChange={setDirection}
          options={[{ value: 'bullish', label: 'Bullish' }, { value: 'bearish', label: 'Bearish' }, { value: 'neutral', label: 'Neutral' }]}
        />
        <SelectPill
          label="Disclosure"
          showLabel
          value={disclosure}
          onChange={setDisclosure}
          options={[{ value: 'organic', label: 'Organic' }, { value: 'creator', label: 'Creator' }, { value: 'sponsored', label: 'Sponsored' }]}
        />
      </div>
      <FormField
        id={`${id}-position`}
        label="Your position"
        optional
        surface={surface}
        helper="Shown with your callout"
        inputProps={{ value: position, onChange: event => setPosition(event.target.value), disabled: posting, maxLength: 160, placeholder: 'Holding 1,200 tokens' }}
      />
      {submit.state === 'unsupported' ? (
        <UnavailableState variant="inline" title="Posting callouts" operation={submit.operation} status={submit.status} />
      ) : submit.state === 'error' ? (
        <p role="alert" className="text-xs text-danger">{submit.message}</p>
      ) : submit.state === 'posted' ? (
        <p role="status" className="text-xs text-success">Callout posted</p>
      ) : null}
      <div className="flex items-center justify-end gap-2">
        <Button type="submit" variant="primary" size="md" icon={<Megaphone />} loading={posting} disabled={signedIn && !valid}>
          {signedIn ? 'Post callout' : 'Sign in to post'}
        </Button>
      </div>
    </form>
  );
}
