'use client';

import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { Heart, MessageSquare } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, EmptyState, FormField } from '@/ui/primitives';
import { formatCount } from '@/utils/format';
import { InteractionClient, InteractionError, type Comment } from './interaction-client';
import { PlainInteractionContent, validateInteractionText } from './plain-content';

function errorText(error: unknown): string {
  if (error instanceof InteractionError && error.status === 401) return 'Sign in to continue';
  if (error instanceof InteractionError && error.status === 429) return 'Slow down, try again in a moment';
  console.error('[interactions] request failed', error);
  return "Couldn't save, try again";
}
function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const active = useRef(false);
  return { busy, error, async run(action: () => Promise<unknown>) {
    if (active.current) return false;
    active.current = true; setBusy(true); setError('');
    try { await action(); return true; } catch (error) { setError(errorText(error)); return false; }
    finally { active.current = false; setBusy(false); }
  } };
}
function Alert({ children, className }: { children: ReactNode; className?: string }): ReactNode {
  return <p role="alert" className={cn('text-xs text-danger', className)}>{children}</p>;
}
const textLimitMessage = 'Keep it under 2,000 characters';

/** Follow control bound to an InteractionClient (`/v2/social/follow/{wallet}`); the callouts package mounts the SDK-backed FollowButton instead. */
export function FollowControl({ client, wallet }: { client: InteractionClient; wallet: string }): ReactNode {
  const [following, setFollowing] = useState<boolean | null>(null);
  const [loadError, setLoadError] = useState('');
  const action = useAction();
  const retry = useRef<{ remove: boolean; key: string } | null>(null);
  const target = useRef({ client, wallet }); target.current = { client, wallet };
  useEffect(() => {
    const controller = new AbortController(); setFollowing(null); setLoadError(''); retry.current = null;
    void client.stats(wallet, controller.signal).then(value => { if (!controller.signal.aborted) setFollowing(value.data.is_following); }).catch(error => { if (!controller.signal.aborted) setLoadError(errorText(error)); });
    return () => controller.abort();
  }, [client, wallet]);
  return <div className="flex flex-col gap-1">
    <Button variant="secondary" size="sm" disabled={following === null || action.busy} loading={action.busy} aria-pressed={following === true} className={cn('group', following && 'bg-accent-soft text-accent hover:bg-danger-soft hover:text-danger')} onClick={() => {
      if (following === null) return;
      const mutation = retry.current?.remove === following ? retry.current : { remove: following, key: crypto.randomUUID() };
      retry.current = mutation;
      void action.run(async () => { await client.follow(wallet, mutation.remove, mutation.key); const fresh = await client.stats(wallet); if (target.current.client !== client || target.current.wallet !== wallet) return; setFollowing(fresh.data.is_following); retry.current = null; });
    }}>{following ? 'Unfollow' : 'Follow'}</Button>
    {(action.error || loadError) && <Alert>{action.error || loadError}</Alert>}
  </div>;
}
export function CommentComposer({ client, pool, parent, onCreated }: { client: InteractionClient; pool: string; parent?: string; onCreated: () => void }): ReactNode {
  const id = useId(); const [body, setBody] = useState(''); const action = useAction();
  const pending = useRef<{ fingerprint: string; text: string; id: string } | null>(null);
  const destination = useRef({ pool, parent }); destination.current = { pool, parent };
  useEffect(() => { pending.current = null; setBody(''); }, [client, pool, parent]);
  async function submit(event: FormEvent) {
    event.preventDefault(); if (!validateInteractionText(body)) return;
    const fingerprint = JSON.stringify([pool, parent, body]);
    const draft = pending.current?.fingerprint === fingerprint ? pending.current : { fingerprint, text: body, id: crypto.randomUUID() }; pending.current = draft;
    if (await action.run(() => client.comment(pool, draft.text, draft.id, parent)) && destination.current.pool === pool && destination.current.parent === parent) { setBody(''); pending.current = null; onCreated(); }
  }
  const invalid = body.length > 0 && !validateInteractionText(body);
  return <form onSubmit={event => { void submit(event); }} aria-busy={action.busy || undefined} className="flex flex-col gap-3 rounded-md bg-surface-1 p-3">
    <FormField id={id} label={parent ? 'Reply' : 'Comment'} multiline counter={{ count: Array.from(body).length, max: 2000 }} error={invalid ? textLimitMessage : null} textareaProps={{ value: body, onChange: event => setBody(event.target.value), disabled: action.busy, rows: 3 }} />
    <div className="flex justify-end"><Button type="submit" variant="primary" size="md" disabled={action.busy || !validateInteractionText(body)} loading={action.busy}>Post a reply</Button></div>
    {action.error && <Alert>{action.error}</Alert>}
  </form>;
}
export function ReactionControl({ client, commentId, liked, count, onChanged }: { client: InteractionClient; commentId: string; liked: boolean; count: string; onChanged: () => void }): ReactNode {
  const action = useAction(); const pending = useRef<{ remove: boolean; key: string } | null>(null);
  return <div className="flex flex-col gap-1">
    <Button variant="ghost" size="sm" aria-pressed={liked} disabled={action.busy} loading={action.busy} icon={<Heart className={cn(liked && 'fill-danger text-danger')} />} className={cn(liked && 'text-danger')} onClick={() => {
      const mutation = pending.current?.remove === liked ? pending.current : { remove: liked, key: crypto.randomUUID() }; pending.current = mutation;
      void action.run(async () => { await client.react(commentId, mutation.remove, mutation.key); pending.current = null; onChanged(); });
    }}>{liked ? 'Unlike' : 'Like'} <span aria-label={`${count} likes`} data-numeric className="text-xs text-text-faint">{formatCount(count)}</span></Button>
    {action.error && <Alert>{action.error}</Alert>}
  </div>;
}
export function CommentActions({ client, comment, own, creator, onChanged }: { client: InteractionClient; comment: Comment; own: boolean; creator: boolean; onChanged: () => void }): ReactNode {
  const [text, setText] = useState(comment.body); const [reason, setReason] = useState(''); const action = useAction(); const editID = useId(); const reasonID = useId();
  const pending = useRef<{ fingerprint: string; key: string } | null>(null);
  useEffect(() => { setText(comment.body); setReason(''); pending.current = null; }, [comment.commentId, comment.revision, comment.body]);
  function mutate(kind: 'edit' | 'delete' | 'hide') {
    const fingerprint = JSON.stringify([kind, comment.commentId, comment.revision, text, reason]);
    const key = pending.current?.fingerprint === fingerprint ? pending.current.key : crypto.randomUUID(); pending.current = { fingerprint, key };
    void action.run(async () => { if (kind === 'edit') await client.edit(comment, text, key); else if (kind === 'delete') await client.remove(comment, key); else await client.hide(comment, reason, key); pending.current = null; onChanged(); });
  }
  return <section aria-label="Comment actions" className="flex flex-col gap-3 rounded-md bg-surface-1 p-3">
    <p className="text-base text-text"><PlainInteractionContent text={comment.body} />{comment.edited && <small className="ml-2 text-xs text-text-faint">Edited</small>}</p>
    {own && <div className="flex flex-col gap-2">
      <FormField id={editID} label="Edit your comment" multiline surface="surface-1" textareaProps={{ value: text, onChange: event => setText(event.target.value), disabled: action.busy, rows: 3 }} />
      <div className="flex flex-wrap justify-end gap-2"><Button variant="primary" size="sm" disabled={action.busy || !validateInteractionText(text)} onClick={() => mutate('edit')}>Save</Button><Button variant="danger-text" size="sm" disabled={action.busy} onClick={() => mutate('delete')}>Delete</Button></div>
    </div>}
    {creator && !own && <div className="flex flex-col gap-2">
      <FormField id={reasonID} label="Reason for hiding" surface="surface-1" inputProps={{ value: reason, onChange: event => setReason(event.target.value), disabled: action.busy }} />
      <div className="flex justify-end"><Button variant="danger-text" size="sm" disabled={action.busy || !validateInteractionText(reason)} onClick={() => mutate('hide')}>Hide as coin creator</Button></div>
    </div>}
    {action.error && <Alert>{action.error}</Alert>}
  </section>;
}
export function CalloutMentions({ client, id, revision, onChanged }: { client: InteractionClient; id: string; revision: string; onChanged: () => void }): ReactNode {
  const [mentions, setMentions] = useState(''); const [transaction, setTransaction] = useState(''); const action = useAction(); const walletID = useId(); const txID = useId();
  const values = mentions.split(/[\s,]+/u).filter(Boolean); const valid = values.length <= 10 && values.every(wallet => /^0x[0-9a-fA-F]{40}$/u.test(wallet)) && (transaction === '' || /^0x[0-9a-fA-F]{64}$/u.test(transaction));
  const pending = useRef<{ fingerprint: string; key: string } | null>(null);
  return <form onSubmit={event => { event.preventDefault(); if (!valid) return; const fingerprint = JSON.stringify([id, revision, values, transaction]); const key = pending.current?.fingerprint === fingerprint ? pending.current.key : crypto.randomUUID(); pending.current = { fingerprint, key }; void action.run(async () => { await client.callout(id, revision, values, transaction, key); pending.current = null; onChanged(); }); }} aria-busy={action.busy || undefined} className="flex flex-col gap-3 rounded-md bg-surface-1 p-3">
    <FormField id={walletID} label="Mention wallets (up to 10)" multiline helper="Separate with spaces or commas" error={mentions.length > 0 && !valid && !values.every(wallet => /^0x[0-9a-fA-F]{40}$/u.test(wallet)) ? 'Use wallet addresses only (0x…)' : null} textareaProps={{ value: mentions, onChange: event => setMentions(event.target.value), disabled: action.busy, rows: 2 }} />
    <FormField id={txID} label="Transaction hash" optional mono error={transaction !== '' && !/^0x[0-9a-fA-F]{64}$/u.test(transaction) ? 'Enter a valid transaction hash' : null} inputProps={{ value: transaction, onChange: event => setTransaction(event.target.value), disabled: action.busy }} />
    <p className="text-xs text-text-faint">Posted by users. The verified mark shows once we confirm the transaction</p>
    <div className="flex justify-end"><Button type="submit" variant="primary" size="sm" disabled={action.busy || !valid} loading={action.busy}>Save mentions</Button></div>
    {action.error && <Alert>{action.error}</Alert>}
  </form>;
}

export function CommentFeed({ client, pool, viewer, isCreator }: { client: InteractionClient; pool: string; viewer: string | null; isCreator: boolean }): ReactNode {
  const [comments, setComments] = useState<Comment[]>([]); const [cursor, setCursor] = useState<string | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [generation, setGeneration] = useState(0);
  const scope = useRef(0);
  const refresh = () => setGeneration(value => value + 1);
  useEffect(() => { const controller = new AbortController(); const current = ++scope.current; setComments([]); setCursor(null); setError(''); setLoading(true); void client.comments(pool, undefined, controller.signal).then(value => { if (!controller.signal.aborted && current === scope.current) { setComments(value.data.items ?? []); setCursor(value.nextCursor); } }).catch(error => { if (!controller.signal.aborted) setError(errorText(error)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => { ++scope.current; controller.abort(); }; }, [client, pool, viewer, generation]);
  async function more() { if (!cursor || loading) return; const current = scope.current; setLoading(true); setError(''); try { const result = await client.comments(pool, cursor); if (current !== scope.current) return; setComments(prior => { const byID = new Map(prior.map(value => [value.commentId, value])); for (const item of result.data.items ?? []) byID.set(item.commentId, item); return [...byID.values()]; }); setCursor(result.nextCursor); } catch (error) { if (current === scope.current) setError(errorText(error)); } finally { if (current === scope.current) setLoading(false); } }
  return <section aria-label="Replies" aria-busy={loading || undefined} className="flex flex-col gap-3">{viewer && <CommentComposer client={client} pool={pool} onCreated={refresh} />}{!loading && !error && comments.length === 0 ? <EmptyState icon={MessageSquare} title="No replies yet" body={viewer ? 'Be the first to reply' : 'Sign in to post a reply'} announce /> : null}<ul className="flex flex-col gap-2">{comments.map(comment => <li key={comment.commentId}><CommentActions client={client} comment={comment} own={viewer?.toLowerCase() === comment.authorWallet} creator={isCreator} onChanged={refresh} /></li>)}</ul>{cursor && <div className="flex justify-center"><Button variant="secondary" size="md" disabled={loading} loading={loading} onClick={() => { void more(); }}>Load more</Button></div>}{error && <Alert>{error}</Alert>}</section>;
}
