'use client';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { CapabilityUnavailableError, HttpError, type SdkClient } from '@kindle/sdk';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { marketAddress } from '@/page-modules/token/market-session';
import { browserUserClient } from './client';
import { isProfileAbsent, rememberProfileAbsent } from './profile-absence';

export interface ProfileView {
  wallet: string;
  displayName: string | null;
  bio: string;
  avatarAssetId: string | null;
  bannerAssetId: string | null;
  /**
   * Always null for other wallets: production media/user has no public account
   * image route (the legacy `/users/{wallet}/avatar` is not registered and the
   * gateway serves `/v2/accounts/{wallet}/assets/{id}` to the owner only).
   */
  avatarUrl: string | null;
  website: string | null;
  socials: Record<string, string>;
  revision: string;
  source: 'v3' | 'v2' | 'v1';
}

const missing = (error: unknown) => error instanceof HttpError && error.status === 404;

/**
 * `GET /v3/profiles/{wallet}` through media/gateway (public route,
 * media/gateway/internal/proxy/connected.go UserConnectedRoutes; served by
 * media/user only when ExtendedV3 is on), falling back to the public
 * `GET /v2/profiles/{wallet}` when v3 is not served. Null means the wallet has no profile; that
 * answer is remembered for the session (profile-absence.ts), so a wallet without
 * a profile is asked for once, not on every page.
 */
export async function fetchProfile(walletAddress: string, client: SdkClient = browserUserClient(), signal?: AbortSignal): Promise<ProfileView | null> {
  const wallet = marketAddress(walletAddress);
  if (isProfileAbsent(wallet)) return null;
  const view = await readProfile(wallet, client, signal);
  if (view === null) rememberProfileAbsent(wallet);
  return view;
}

async function readProfile(wallet: string, client: SdkClient, signal?: AbortSignal): Promise<ProfileView | null> {
  try {
    const response = await client.requestV3(`/v3/profiles/${wallet}`, 'ConnectedExtendedProfileResponse', { signal, service: 'media', idempotentRead: true });
    const profile = response.data.profile;
    if (response.quality === 'quarantined' || profile.wallet.toLowerCase() !== wallet) throw new Error('Profile identity disagreement or integrity quarantine');
    const socials: Record<string, string> = {};
    for (const [key, value] of Object.entries(profile.socials)) if (typeof value === 'string' && value) socials[key] = value;
    return { wallet, displayName: profile.displayName || null, bio: profile.bio, avatarAssetId: profile.avatarAssetId, bannerAssetId: profile.bannerAssetId, avatarUrl: null, website: profile.website, socials, revision: profile.revision, source: 'v3' };
  } catch (error) {
    if (missing(error)) return null;
    if (!(error instanceof CapabilityUnavailableError)) throw error;
  }
  try {
    const response = await client.requestV2(`/v2/profiles/${wallet}`, 'ProfileGetResponse', { signal, service: 'media', idempotentRead: true });
    const profile = response.data.profile;
    if (response.quality === 'quarantined' || profile.wallet.toLowerCase() !== wallet) throw new Error('Profile identity disagreement or integrity quarantine');
    return { wallet, displayName: profile.displayName || null, bio: profile.bio, avatarAssetId: profile.avatarAssetId, bannerAssetId: profile.bannerAssetId, avatarUrl: null, website: profile.website, socials: profile.website ? { website: profile.website } : {}, revision: profile.revision, source: 'v2' };
  } catch (error) {
    if (missing(error) || error instanceof CapabilityUnavailableError && error.status === 404) return null;
    throw error;
  }
}

export function useProfile(wallet: string | null | undefined): UseQueryResult<ProfileView | null> {
  const address = wallet && /^0x[0-9a-fA-F]{40}$/.test(wallet) ? wallet.toLowerCase() : null;
  return useQuery({
    queryKey: queryKeys.profile(address ?? ''),
    queryFn: ({ signal }) => fetchProfile(address as string, browserUserClient(), signal),
    enabled: address !== null,
    ...marketCachePolicy.profile,
  });
}
