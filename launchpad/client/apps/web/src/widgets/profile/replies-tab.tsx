'use client';

import Link from 'next/link';
import { MessageSquare } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { poolPath } from '@/core/app-foundation/navigation';
import { loadable } from '@/core/market/unsupported';
import { socialReadUrl } from '@/core/sdk-config';
import { Button, EmptyState, ErrorState, Skeleton, UnavailableState } from '@/ui/primitives';
import { formatCount, relativeAge, shortAddress } from '@/utils/format';
import { CollectedPlaceholder, useProfileIssue } from './profile-issues';

export interface RepliesTabProps {
  wallet: string;
  /** The signed-in owner gets a call to action on an empty list; visitors get neutral copy. */
  isOwner?: boolean;
}

/** One comment as media/social serves it on the pool feed (pkg/types Comment); the per-user list is expected in the same shape. */
interface ProfileComment {
  id: string;
  pool_address: string;
  author: string;
  content: string;
  parent_id: string | null;
  created_at: number;
  like_count: number;
}

const repliesOperation = 'GET /users/{wallet}/comments (media/social)';
const REPLIES_LIMIT = 50;

class SocialReadError extends Error {
  constructor(readonly status: number, readonly path: string) {
    super(`media/social answered ${status}: ${path}`);
    this.name = 'SocialReadError';
  }
}

function isComment(value: unknown, wallet: string): value is ProfileComment {
  if (typeof value !== 'object' || value === null) return false;
  const row = value as Record<string, unknown>;
  return typeof row.id === 'string'
    && typeof row.pool_address === 'string' && /^0x[0-9a-fA-F]{40}$/.test(row.pool_address)
    && typeof row.author === 'string' && row.author.toLowerCase() === wallet
    && typeof row.content === 'string'
    && (row.parent_id === null || typeof row.parent_id === 'string')
    && typeof row.created_at === 'number'
    && typeof row.like_count === 'number';
}

async function readProfileComments(wallet: string, signal?: AbortSignal): Promise<{ comments: ProfileComment[]; hasMore: boolean }> {
  const address = wallet.toLowerCase();
  const path = `/users/${address}/comments?limit=${REPLIES_LIMIT}`;
  const response = await fetch(socialReadUrl(path), { signal, cache: 'no-store', credentials: 'omit', headers: { accept: 'application/json' } });
  if (!response.ok) {
    await response.body?.cancel();
    throw new SocialReadError(response.status, path);
  }
  const body: unknown = await response.json();
  // A Go nil slice serializes as null: treat it as an empty list.
  const comments = (body as { comments?: unknown } | null)?.comments ?? [];
  if (!Array.isArray(comments) || !comments.every(row => isComment(row, address))) throw new Error('Replies response does not match the comment shape for this wallet');
  return { comments, hasMore: (body as { has_more?: unknown }).has_more === true };
}

/** The wallet's comments across coin threads, from media/social; its real 404 renders the unavailable state. */
export function RepliesTab({ wallet, isOwner = false }: RepliesTabProps) {
  const address = wallet.toLowerCase();
  const query = useQuery({
    queryKey: queryKeys.profileReplies(address),
    queryFn: ({ signal }) => readProfileComments(address, signal),
    enabled: /^0x[0-9a-f]{40}$/.test(address),
    ...marketCachePolicy.profileReplies,
  });
  const state = loadable(query, repliesOperation);
  const collected = useProfileIssue('replies', state.state === 'error' ? { label: 'Replies', error: state.error, retry: state.retry } : null);

  if (state.state === 'loading') return <Skeleton variant="rows" rows={4} label="replies" />;
  if (state.state === 'unsupported') {
    return <UnavailableState title="Replies" plural operation={state.operation} status={state.status} message="Check back in a bit" onRetry={() => { void query.refetch(); }} />;
  }
  if (state.state === 'error' && collected) return <CollectedPlaceholder label="Replies" />;
  if (state.state === 'error') return <ErrorState title="Can't load replies right now" message="Check your connection and try again" onRetry={state.retry} />;
  const { comments, hasMore } = state.data;
  if (comments.length === 0) {
    return (
      <EmptyState
        icon={MessageSquare}
        title="No replies yet"
        body={isOwner ? 'Replies you post on coins show up here' : "This wallet hasn't replied to any coins yet"}
        action={isOwner ? <Button href="/explore" variant="secondary" size="sm">Browse coins</Button> : undefined}
      />
    );
  }
  const now = Date.now();
  return (
    <section aria-label="Replies" className="flex flex-col gap-1 rounded-lg bg-surface-1 p-1">
      <ul className="flex flex-col gap-1">
        {comments.map(comment => (
          <li key={comment.id} className="rounded-md bg-surface-2 px-3 py-2">
            <div className="flex items-center justify-between gap-3 text-xs text-text-faint">
              <Link href={poolPath(125, comment.pool_address)} className="truncate font-mono text-text-muted hover:text-text">{comment.parent_id ? 'Replied on' : 'Posted on'} {shortAddress(comment.pool_address)}</Link>
              <span className="shrink-0 tabular-nums">{relativeAge(comment.created_at, now)} ago{comment.like_count > 0 ? ` · ${formatCount(comment.like_count)} likes` : ''}</span>
            </div>
            <p className="mt-1 whitespace-pre-wrap break-words text-sm text-text">{comment.content}</p>
          </li>
        ))}
      </ul>
      {hasMore ? <p className="px-3 py-2 text-xs text-text-faint">Showing the latest {REPLIES_LIMIT} replies.</p> : null}
    </section>
  );
}
