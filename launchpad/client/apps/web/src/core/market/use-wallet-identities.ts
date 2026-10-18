'use client';
import { useQueries } from '@tanstack/react-query';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import { shortWallet } from '@/page-modules/token/market-presentation';
import { CapabilityUnavailableError, type V2ContractTypes } from '@kindle/sdk';
import { browserUserClient } from './client';
import { isProfileAbsent, rememberProfileAbsent } from './profile-absence';
import { fetchProfile, type ProfileView } from './use-profile';

type ProfilesBatchResponse = V2ContractTypes['ProfilesBatchResponse'];

export interface WalletIdentity {
  wallet: string;
  displayName: string | null;
  avatarUrl: string | null;
  shortAddress: string;
  profile: ProfileView | null;
  /** 'profile' once the lookup resolved (with or without a profile), 'pending' while loading, 'error' when the lookup failed. */
  resolution: 'profile' | 'pending' | 'error';
}

const inflight = new Map<string, Promise<ProfileView | null>>();
let active = 0;
const waiting: Array<() => void> = [];
const concurrency = 4;
/** media/user `GET /v2/profiles:batch` accepts at most 50 wallets (profiles_http.go profilesBatchLimit). */
const batchLimit = 50;

async function gated<T>(task: () => Promise<T>): Promise<T> {
  if (active >= concurrency) await new Promise<void>(resolve => waiting.push(resolve));
  active++;
  try { return await task(); }
  finally { active--; waiting.shift()?.(); }
}

interface Waiter { resolve(value: ProfileView | null): void; reject(error: unknown): void }
let queued = new Map<string, Waiter>();
let scheduled = false;

function viewFromBatch(profile: ProfilesBatchResponse['data']['items'][number]): ProfileView {
  const wallet = profile.wallet.toLowerCase();
  return { wallet, displayName: profile.displayName || null, bio: profile.bio, avatarAssetId: profile.avatarAssetId, bannerAssetId: profile.bannerAssetId, avatarUrl: null, website: profile.website, socials: profile.website ? { website: profile.website } : {}, revision: profile.revision, source: 'v2' };
}

/**
 * One `GET /v2/profiles:batch?wallets=` for up to 50 wallets. The batch answers
 * 200 and simply omits wallets that have no profile, so "no profile" arrives as
 * data instead of a 404 the browser logs as a console error. Those wallets are
 * remembered as absent for the session. Where the batch route is not served the
 * wallets fall back to the per-wallet read (fetchProfile, v3 then v1).
 */
async function readChunk(wallets: readonly string[]): Promise<Map<string, PromiseSettledResult<ProfileView | null>>> {
  try {
    const response = await browserUserClient().requestV2(`/v2/profiles:batch?wallets=${wallets.join(',')}`, 'ProfilesBatchResponse', { service: 'media', idempotentRead: true });
    if (response.quality === 'quarantined') throw new Error('Profile batch integrity quarantine');
    const found = new Map<string, ProfileView>();
    for (const item of response.data.items) {
      const view = viewFromBatch(item);
      if (!wallets.includes(view.wallet)) throw new Error('Profile identity disagreement');
      found.set(view.wallet, view);
    }
    return new Map(wallets.map(wallet => {
      const view = found.get(wallet) ?? null;
      if (view === null) rememberProfileAbsent(wallet);
      return [wallet, { status: 'fulfilled', value: view }];
    }));
  } catch (error) {
    if (!(error instanceof CapabilityUnavailableError)) return new Map(wallets.map(wallet => [wallet, { status: 'rejected', reason: error }]));
  }
  const settled = await Promise.allSettled(wallets.map(wallet => fetchProfile(wallet)));
  return new Map(wallets.map((wallet, index) => [wallet, settled[index]!]));
}

function flush() {
  scheduled = false;
  const batch = queued; queued = new Map();
  const wallets = [...batch.keys()];
  for (let start = 0; start < wallets.length; start += batchLimit) {
    const chunk = wallets.slice(start, start + batchLimit);
    void gated(() => readChunk(chunk)).then(results => {
      for (const wallet of chunk) {
        const result = results.get(wallet)!;
        if (result.status === 'fulfilled') batch.get(wallet)!.resolve(result.value);
        else batch.get(wallet)!.reject(result.reason);
      }
    });
  }
}

/**
 * Shared lookup: a wallet already known to have no profile resolves null with no
 * request; concurrent callers for one wallet await one request; wallets asked
 * for in the same tick share one batch request.
 */
export function loadWalletProfile(wallet: string): Promise<ProfileView | null> {
  const key = wallet.toLowerCase();
  if (isProfileAbsent(key)) return Promise.resolve(null);
  const pending = inflight.get(key);
  if (pending) return pending;
  const promise = new Promise<ProfileView | null>((resolve, reject) => {
    queued.set(key, { resolve, reject });
    if (!scheduled) { scheduled = true; setTimeout(flush, 0); }
  }).finally(() => { inflight.delete(key); });
  inflight.set(key, promise);
  return promise;
}

export function fallbackIdentity(wallet: string, resolution: WalletIdentity['resolution'] = 'pending'): WalletIdentity {
  const address = wallet.toLowerCase();
  return { wallet: address, displayName: null, avatarUrl: null, shortAddress: shortWallet(address), profile: null, resolution };
}

export function identityFromProfile(wallet: string, profile: ProfileView | null): WalletIdentity {
  return { ...fallbackIdentity(wallet, 'profile'), displayName: profile?.displayName ?? null, avatarUrl: profile?.avatarUrl ?? null, profile };
}

/** Batched per-wallet identity lookups; every wallet always has a short-address fallback. */
export function useWalletIdentities(wallets: readonly string[]): { identities: Record<string, WalletIdentity>; isPending: boolean; label(wallet: string): string } {
  const unique = [...new Set(wallets.filter(value => /^0x[0-9a-fA-F]{40}$/.test(value)).map(value => value.toLowerCase()))].sort();
  return useQueries({
    queries: unique.map(wallet => ({ queryKey: queryKeys.walletIdentity(wallet), queryFn: () => loadWalletProfile(wallet), ...marketCachePolicy.walletIdentity })),
    combine: results => {
      const identities: Record<string, WalletIdentity> = {};
      let isPending = false;
      unique.forEach((wallet, index) => {
        const result = results[index];
        if (!result || result.status === 'pending') { isPending = true; identities[wallet] = fallbackIdentity(wallet); }
        else if (result.status === 'error') identities[wallet] = fallbackIdentity(wallet, 'error');
        else identities[wallet] = identityFromProfile(wallet, result.data);
      });
      return { identities, isPending, label: (wallet: string) => identities[wallet.toLowerCase()]?.displayName ?? shortWallet(wallet.toLowerCase()) };
    },
  });
}
