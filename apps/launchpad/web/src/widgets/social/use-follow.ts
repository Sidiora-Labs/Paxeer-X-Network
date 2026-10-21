'use client';
import { useCallback, useSyncExternalStore } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { queryKeys } from '@/core/query-keys';
import { loadable, type Loadable } from '@/core/market/unsupported';
import { openAuthModal } from '@/shell/auth-modal-store';
import { accountSession, subscribeAccount } from '@/widgets/profile/account-transport';
import { fetchFollowStats, setFollowing, SocialTransportError, type FollowStats } from './social-transport';

const walletPattern = /^0x[0-9a-fA-F]{40}$/;
const noViewer = () => null;
const viewerSnapshot = () => accountSession()?.session.wallet.toLowerCase() ?? null;

/** Lowercased wallet of the signed-in account session, or null. */
export function useViewerWallet(): string | null {
  return useSyncExternalStore(subscribeAccount, viewerSnapshot, noViewer);
}

export function followStatsKey(wallet: string, viewer: string | null) {
  return [...queryKeys.followStats(wallet), viewer ?? 'anonymous'] as const;
}

const followStatsPolicy = { staleTime: 30_000, gcTime: 5 * 60_000, retry: false } as const;

/** Follower graph counts for `wallet`; Unsupported when neither the contract path nor the social proxy serves them. */
export function useFollowStats(wallet: string | null | undefined): Loadable<FollowStats> {
  const viewer = useViewerWallet();
  const address = wallet && walletPattern.test(wallet) ? wallet.toLowerCase() : null;
  const query = useQuery({
    queryKey: followStatsKey(address ?? '', viewer),
    queryFn: ({ signal }) => fetchFollowStats(address as string, signal),
    enabled: address !== null,
    ...followStatsPolicy,
  });
  return loadable(query, 'IF_FOLLOW_STATS');
}

export function followErrorMessage(error: unknown): string {
  if (error instanceof SocialTransportError) {
    if (error.status === 401 || error.code === 'AUTH_REQUIRED') return 'Sign in to follow';
    if (error.status === 404 || error.status === 501) return 'Following is not available yet';
    if (error.status === 429) return 'Slow down, try again in a moment';
    if (error.status === 0) return "Can't reach the server, check your connection";
    console.error('[follow] update failed', error.code, error.status);
    return "Couldn't update follow, try again";
  }
  console.error('[follow] update failed', error);
  return "Couldn't update follow, try again";
}

export interface FollowController {
  stats: Loadable<FollowStats>;
  /** Null until stats resolve. */
  following: boolean | null;
  pending: boolean;
  /** The viewer is looking at their own wallet; the control hides. */
  self: boolean;
  signedIn: boolean;
  toggle(): void;
}

/**
 * Optimistic follow toggle with rollback. Signed out, `toggle` opens the sign-in
 * modal instead of mutating. On failure the cached stats revert and the real
 * error surfaces as a toast.
 */
export function useFollow(wallet: string | null | undefined): FollowController {
  const viewer = useViewerWallet();
  const client = useQueryClient();
  const address = wallet && walletPattern.test(wallet) ? wallet.toLowerCase() : null;
  const stats = useFollowStats(address);
  const key = followStatsKey(address ?? '', viewer);
  const mutation = useMutation({
    mutationFn: (remove: boolean) => setFollowing(address as string, remove),
    onMutate: async (remove: boolean) => {
      await client.cancelQueries({ queryKey: key });
      const previous = client.getQueryData<FollowStats>(key);
      if (previous) client.setQueryData<FollowStats>(key, { ...previous, isFollowing: !remove, followers: Math.max(0, previous.followers + (remove ? -1 : 1)) });
      return { previous };
    },
    onError: (error, _remove, context) => {
      if (context?.previous) client.setQueryData(key, context.previous);
      toast.error(followErrorMessage(error));
    },
    onSettled: () => { void client.invalidateQueries({ queryKey: key }); },
  });
  const following = stats.state === 'ready' ? stats.data.isFollowing : null;
  const self = viewer !== null && address !== null && viewer === address;
  const mutate = mutation.mutate;
  const pending = mutation.isPending;
  const toggle = useCallback(() => {
    if (!address || self || pending) return;
    if (!viewer) { openAuthModal('Sign in to follow'); return; }
    if (following === null) return;
    mutate(following);
  }, [address, self, pending, viewer, following, mutate]);
  return { stats, following, pending, self, signedIn: viewer !== null, toggle };
}
