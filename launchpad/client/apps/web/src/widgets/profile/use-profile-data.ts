'use client';

import { useCallback } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAccount } from 'wagmi';
import { queryKeys } from '@/core/query-keys';
import { AccountApiError, extendedProfile, readAccountAsset, type ExtendedProfile } from './account-transport';
import { useAccountSession } from './use-account-session';
import { usePrivateAccountQuery } from './use-private-account-query';

export interface ProfileSocials {
  website?: string | null;
  twitter?: string | null;
  telegram?: string | null;
  discord?: string | null;
}

export interface ProfileIdentity {
  wallet: string;
  displayName: string | null;
  bio: string;
  avatarAssetId: string | null;
  bannerAssetId: string | null;
  website: string | null;
  socials: ProfileSocials;
  /** Present only when media/user publishes it on the v3 presentation. */
  createdAt: string | null;
  revision: string;
  creatorPools: { poolAddress: string; tokenAddress: string; createdAt: string }[];
}

const isWalletAddress = (value: string) => /^0x[a-fA-F0-9]{40}$/.test(value);

/** Reads the optional `createdAt` the contract added to Profile; the v3 presentation type does not declare it yet. */
export function profileCreatedAt(profile: unknown): string | null {
  if (typeof profile !== 'object' || profile === null) return null;
  const value = (profile as { createdAt?: unknown }).createdAt;
  if (typeof value !== 'string' || Number.isNaN(Date.parse(value))) return null;
  return value;
}

export function toProfileIdentity(profile: ExtendedProfile): ProfileIdentity {
  return {
    wallet: profile.wallet.toLowerCase(),
    displayName: profile.displayName?.trim() ? profile.displayName : null,
    bio: profile.bio ?? '',
    avatarAssetId: profile.avatarAssetId,
    bannerAssetId: profile.bannerAssetId,
    website: profile.website ?? profile.socials?.website ?? null,
    socials: { website: profile.website ?? profile.socials?.website ?? null, twitter: profile.socials?.twitter ?? null, telegram: profile.socials?.telegram ?? null, discord: profile.socials?.discord ?? null },
    createdAt: profileCreatedAt(profile),
    revision: profile.revision,
    creatorPools: profile.creatorPools ?? [],
  };
}

/** `GET /v3/profiles/{wallet}` on media/user. Null means the wallet has published no profile (404). */
function useUserProfile(walletAddress: string) {
  return useQuery<ProfileIdentity | null>({
    queryKey: queryKeys.userProfile(walletAddress),
    queryFn: async ({ signal }) => {
      try {
        const result = await extendedProfile(walletAddress, signal);
        return toProfileIdentity(result.data.profile);
      } catch (error) {
        if (error instanceof AccountApiError && error.status === 404) return null;
        throw error;
      }
    },
    retry: false,
    enabled: isWalletAddress(walletAddress),
    staleTime: 30_000,
  });
}

/** Owner-only: the approved asset behind a v3 asset id, read through the gateway account asset route. */
function useOwnedAssetUrl(walletAddress: string, assetId: string | null) {
  const query = usePrivateAccountQuery('profile-asset', () => readAccountAsset(walletAddress, assetId as string), [assetId ?? ''], walletAddress);
  const asset = assetId ? query.data : undefined;
  return { url: asset?.state === 'approved' ? asset.publicUrl : null, pending: Boolean(assetId) && query.owner && query.isPending, owner: query.owner };
}

function useProfileMedia(walletAddress: string, profile: ProfileIdentity | null | undefined) {
  const avatarAsset = useOwnedAssetUrl(walletAddress, profile?.avatarAssetId ?? null);
  const bannerAsset = useOwnedAssetUrl(walletAddress, profile?.bannerAssetId ?? null);
  // Production has no public account image route (media/user legacy /users/{wallet}/avatar|banner is not
  // registered; the gateway serves account assets to their owner only), so visitors see initials.
  return {
    avatarUrl: avatarAsset.url ?? null,
    bannerUrl: bannerAsset.url ?? null,
  };
}

export function useProfileData(walletAddress: string) {
  const { address: myAddress, isConnected } = useAccount();
  const auth = useAccountSession();
  const isValidWallet = isWalletAddress(walletAddress);
  const isOwner = !!auth.session && auth.session.session.wallet.toLowerCase() === walletAddress.toLowerCase();
  const profileQuery = useUserProfile(walletAddress);
  const media = useProfileMedia(walletAddress, profileQuery.data);
  const refreshProfile = useCallback(() => { void profileQuery.refetch(); }, [profileQuery]);
  return {
    profile: profileQuery.data ?? null,
    profileLoading: profileQuery.isPending && isValidWallet,
    profileError: profileQuery.error instanceof Error ? profileQuery.error.message : null,
    refreshProfile,
    avatarUrl: media.avatarUrl,
    bannerUrl: media.bannerUrl,
    isOwner,
    isValidWallet,
    myAddress,
    isConnected,
  };
}
