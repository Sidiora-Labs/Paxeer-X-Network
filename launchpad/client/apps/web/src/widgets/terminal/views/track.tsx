'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { Radar, UserPlus } from 'lucide-react';
import { HttpError } from '@kindle/sdk';
import { socialReadUrl } from '@/core/sdk-config';
import { loadable } from '@/core/market/unsupported';
import { technicalDetail, userFacingMessage } from '@/core/user-facing-error';
import { queryKeys } from '@/core/query-keys';
import { useWalletIdentities, type WalletIdentity } from '@/core/market/use-wallet-identities';
import { openAuthModal } from '@/shell/auth-modal-store';
import { useFollowingTicker } from '@/shell/use-ticker';
import { useFollowStats } from '@/widgets/social/use-follow';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { accountSession } from '@/widgets/profile/account-transport';
import { AgeChip, Avatar, Button, EmptyState, ErrorState, Skeleton, UnavailableState } from '@/ui/primitives';
import { TableRowsSkeleton } from '@/ui/shared/skeletons';
import { formatCount, shortAddress } from '@/utils/format';

export const followingOperation = 'GET /v2/social/users/{wallet}/following (media/social via gateway)';

export interface FollowedWallet { wallet: string; followedAt: string | null }
export interface FollowingPage { owner: string; items: FollowedWallet[]; nextCursor: string | null }

const walletPattern = /^0x[0-9a-fA-F]{40}$/;

function edge(value: unknown): FollowedWallet {
  const row = value && typeof value === 'object' ? value as { wallet?: unknown; created_at?: unknown } : null;
  const wallet = typeof value === 'string' ? value : row?.wallet;
  if (typeof wallet !== 'string' || !walletPattern.test(wallet)) throw new TypeError('Following entry has no wallet');
  const at = row && typeof row.created_at === 'string' && !Number.isNaN(Date.parse(row.created_at)) ? row.created_at : null;
  return { wallet: wallet.toLowerCase(), followedAt: at };
}

/**
 * Normalise both media/social following shapes: the interactions envelope
 * `{ data: { items: [{ wallet, created_at }] }, nextCursor }` and the legacy
 * row `{ wallet_address, following: [{ wallet, created_at }] }`.
 */
export function parseFollowing(value: unknown, owner: string): FollowingPage {
  if (!value || typeof value !== 'object') throw new TypeError('Following response is not an object');
  const envelope = 'data' in value ? value as { data: unknown; nextCursor?: unknown } : null;
  const body = (envelope ? envelope.data : value) as { items?: unknown; following?: unknown; wallet_address?: unknown } | null;
  if (!body || typeof body !== 'object') throw new TypeError('Following response has no body');
  // A legacy Go handler serializes an empty following slice as null: that is an empty list, not a malformed one.
  const list = Array.isArray(body.items) ? body.items : Array.isArray(body.following) ? body.following : body.items === null || body.following === null ? [] : null;
  if (!list) throw new TypeError('Following response has no list');
  if (typeof body.wallet_address === 'string' && body.wallet_address.toLowerCase() !== owner.toLowerCase()) throw new TypeError('Following response owner disagreement');
  const seen = new Set<string>();
  const items: FollowedWallet[] = [];
  for (const entry of list) {
    const parsed = edge(entry);
    if (seen.has(parsed.wallet)) continue;
    seen.add(parsed.wallet);
    items.push(parsed);
  }
  return { owner: owner.toLowerCase(), items, nextCursor: typeof envelope?.nextCursor === 'string' ? envelope.nextCursor : null };
}

/**
 * The wallets `owner` follows, from the interactions route the production
 * gateway serves to a session (proxy/connected.go SocialConnectedRoutes:
 * `GET /v2/social/users/{wallet}/following`, Authenticate). The web origin is
 * cross-site to the gateway, so the session travels as a bearer.
 */
export async function fetchFollowing(owner: string, signal?: AbortSignal, fetcher: typeof fetch = fetch): Promise<FollowingPage> {
  if (!walletPattern.test(owner)) throw new TypeError('Invalid wallet address');
  const wallet = owner.toLowerCase();
  const token = accountSession()?.accessToken;
  if (!token) throw new HttpError(401);
  const response = await fetcher(socialReadUrl(`/v2/social/users/${wallet}/following?limit=50`), { signal, cache: 'no-store', credentials: 'omit', redirect: 'error', headers: { Accept: 'application/json', Authorization: `Bearer ${token}` } });
  if (!response.ok) { await response.body?.cancel(); throw new HttpError(response.status); }
  return parseFollowing(await response.json(), wallet);
}

export interface TrackRow {
  wallet: string;
  label: string;
  hasName: boolean;
  avatarUrl: string | null;
  short: string;
  href: string;
  followedAt: string | null;
}

/** Join followed wallets with their resolved identities; every row keeps a short-address fallback. */
export function trackRows(page: FollowingPage, identities: Readonly<Record<string, WalletIdentity>>): TrackRow[] {
  return page.items.map(item => {
    const identity = identities[item.wallet];
    const name = identity?.displayName?.trim() || null;
    return {
      wallet: item.wallet,
      label: name ?? shortAddress(item.wallet),
      hasName: name !== null,
      avatarUrl: identity?.avatarUrl ?? null,
      short: shortAddress(item.wallet),
      href: `/profile/${item.wallet}`,
      followedAt: item.followedAt,
    };
  });
}

function FollowCounts({ wallet }: { wallet: string }) {
  const stats = useFollowStats(wallet);
  if (stats.state === 'loading') return <Skeleton width={96} height={12} />;
  if (stats.state !== 'ready') return <span className="text-xs text-text-faint" title={stats.state === 'unsupported' ? 'Follow counts are not available yet' : userFacingMessage(stats.error)} data-operation={stats.state === 'unsupported' ? technicalDetail(stats.operation, stats.status) ?? undefined : undefined}>Follow counts not available yet</span>;
  return (
    <span data-numeric className="text-xs tabular-nums text-text-muted">
      <span className="font-bold text-text">{formatCount(stats.data.followers)}</span> followers
      <span className="px-1 text-text-faint">/</span>
      <span className="font-bold text-text">{formatCount(stats.data.following)}</span> following
    </span>
  );
}

function TrackRowItem({ row }: { row: TrackRow }) {
  return (
    <li className="relative flex h-11 items-center gap-3 bg-surface-1 px-3 transition-colors duration-fast hover:bg-surface-hover">
      <Link href={row.href} aria-label={`Open profile of ${row.label}`} className="absolute inset-0 rounded-sm focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus" />
      <Avatar src={row.avatarUrl} name={row.label} size="md" decorative />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm font-bold text-text">{row.label}</span>
        <span className="font-mono text-xs text-text-faint">{row.short}</span>
      </div>
      <div className="hidden shrink-0 sm:block"><FollowCounts wallet={row.wallet} /></div>
      <div className="w-16 shrink-0 text-right text-xs text-text-faint">
        {row.followedAt ? <><span className="sr-only">Followed </span><AgeChip iso={row.followedAt} /></> : null}
      </div>
    </li>
  );
}

function FollowedActivity() {
  const feed = useFollowingTicker();
  return (
    <section aria-labelledby="terminal-track-activity" className="flex flex-col gap-2 rounded-md bg-surface-1 p-3">
      <h2 id="terminal-track-activity" className="text-sm font-bold text-text">Activity from wallets you follow</h2>
      {feed.state === 'loading' ? <Skeleton variant="rows" rows={3} label="followed activity" /> : null}
      {feed.state === 'unsupported' ? <UnavailableState variant="inline" title="Followed-wallet activity" operation={feed.operation} status={feed.status} /> : null}
      {feed.state === 'error' ? <ErrorState variant="inline" title="Activity can't load right now" message={userFacingMessage(feed.error)} onRetry={feed.retry} /> : null}
      {feed.state === 'signed-out' ? <p className="text-sm text-text-faint">{feed.prompt}</p> : null}
      {feed.state === 'ready' && feed.chips.length === 0 ? <EmptyState variant="inline" title={feed.emptyText ?? 'No activity from wallets you follow yet.'} /> : null}
      {feed.state === 'ready' && feed.chips.length > 0 ? (
        <ul className="flex flex-col gap-0.5">
          {feed.chips.map(chip => (
            <li key={chip.key}>
              <Link href={chip.href} className="flex h-9 items-center rounded-sm bg-surface-2 px-3 text-sm text-text hover:bg-surface-hover">
                <span className="truncate">{chip.label}</span>
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
      <p className="text-xs text-text-faint">Trades by followed wallets are not shown yet; their callouts appear here.</p>
    </section>
  );
}

function FollowingList({ wallet }: { wallet: string }) {
  const query = useQuery({
    queryKey: queryKeys.terminalFollowing(wallet),
    queryFn: ({ signal }) => fetchFollowing(wallet, signal),
    retry: false,
    staleTime: 30_000,
  });
  const page = query.data ?? null;
  const wallets = useMemo(() => page?.items.map(item => item.wallet) ?? [], [page]);
  const { identities } = useWalletIdentities(wallets);
  const rows = useMemo(() => (page ? trackRows(page, identities) : []), [page, identities]);
  const state = loadable(query, followingOperation);
  return (
    <section aria-labelledby="terminal-track-following" className="flex min-h-0 flex-col overflow-hidden rounded-md bg-surface-1">
      <header className="flex h-8 shrink-0 items-center justify-between px-3">
        <h2 id="terminal-track-following" className="flex items-center gap-1.5 text-sm font-bold text-text">
          Following
          {state.state === 'ready' ? <span data-numeric className="text-xs tabular-nums text-text-faint">{formatCount(rows.length)}{page?.nextCursor ? '+' : ''}</span> : null}
        </h2>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {state.state === 'loading' ? <><span className="sr-only" role="status">Loading followed wallets</span><TableRowsSkeleton rows={6} columns={3} /></> : null}
        {state.state === 'unsupported' ? <UnavailableState title="Followed wallets" operation={state.operation} status={state.status} onRetry={() => { void query.refetch(); }} /> : null}
        {state.state === 'error' ? <ErrorState title="Followed wallets can't load right now" message={userFacingMessage(state.error)} onRetry={state.retry} variant="inline" /> : null}
        {state.state === 'ready' && rows.length === 0 ? <EmptyState icon={UserPlus} title="You are not following anyone yet" body="Follow traders from their profile or a callout and they show up here." action={<Button variant="secondary" size="sm" href="/leaderboard">Find traders</Button>} /> : null}
        {state.state === 'ready' && rows.length > 0 ? (
          <ul aria-label="Followed wallets" className="flex flex-col gap-0.5 bg-canvas">
            {rows.map(row => <TrackRowItem key={row.wallet} row={row} />)}
          </ul>
        ) : null}
      </div>
    </section>
  );
}

/** Track view: wallets the signed-in account follows, their follow counts and the served followed-activity feed. */
export default function TerminalTrackView() {
  const { session } = useAccountSession();
  const wallet = session?.session.wallet.toLowerCase() ?? null;
  if (!wallet) {
    return (
      <div className="flex h-full items-center justify-center rounded-md bg-surface-1">
        <EmptyState icon={Radar} title="Sign in to track wallets" body="The wallets you follow and their callouts appear here once your account is signed in." action={<Button variant="primary" size="sm" onClick={() => openAuthModal('Sign in to track wallets')}>Sign in</Button>} />
      </div>
    );
  }
  return (
    <div className="grid h-full min-h-0 grid-cols-1 gap-1 overflow-y-auto lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)] lg:overflow-hidden">
      <FollowingList wallet={wallet} />
      <div className="min-h-0 overflow-y-auto"><FollowedActivity /></div>
    </div>
  );
}
