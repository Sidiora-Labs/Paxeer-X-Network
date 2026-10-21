import { CapabilityUnavailableError, HttpError, OPERATION_READ_TIMEOUT_FLOOR_MS, type V2ContractTypes } from '@kindle/sdk';
import { socialReadUrl, socialWriteUrl } from '@/core/sdk-config';
import { browserSessionClient } from '@/core/market/client';
import { operationsFor } from '@/core/market/operations';
import { accountSession } from '@/widgets/profile/account-transport';

export type FollowStatsResponse = V2ContractTypes['FollowStatsResponse'];
export type FollowResponse = V2ContractTypes['FollowResponse'];
export type FollowersResponse = V2ContractTypes['FollowersResponse'];

/**
 * Which transport answered the last successful follow-graph request.
 * - operation: the IF_FOLLOW / IF_UNFOLLOW / IF_FOLLOW_STATS / IF_FOLLOWERS
 *   contract paths (service "media", so the session client's media origin,
 *   the gateway, which routes PUT/DELETE /v2/me/following/{wallet},
 *   GET /v2/profiles/{wallet}/follow-stats and GET /v2/profiles/{wallet}/followers
 *   to media/social).
 * - social-proxy: the media/social interaction routes the production gateway
 *   serves to a session (`/v2/social/follow/{followee}`,
 *   `/v2/social/users/{wallet}/follow-stats|followers`; gateway
 *   proxy/connected.go SocialConnectedRoutes, all Authenticate), used only
 *   after the operation path answers 404/501 and only for a signed-in viewer.
 */
export type FollowRoute = 'operation' | 'social-proxy';

export interface FollowStats {
  wallet: string;
  followers: number;
  following: number;
  isFollowing: boolean;
  route: FollowRoute;
}

export interface FollowersPage {
  wallet: string;
  wallets: string[];
  nextCursor: string | null;
  route: FollowRoute;
}

export class SocialTransportError extends Error {
  constructor(readonly status: number, readonly code: string, readonly route: FollowRoute, message = code) {
    super(message);
  }
}

const walletPattern = /^0x[0-9a-fA-F]{40}$/;

export function socialWallet(value: string): string {
  if (!walletPattern.test(value)) throw new TypeError('Invalid wallet address');
  return value.toLowerCase();
}

/** The same Operations instance `useSessionOperations()` hands to components: bearer-bound, media origin = gateway. */
function socialOperations() {
  return operationsFor(browserSessionClient());
}

let resolvedRoute: FollowRoute | null = null;

/** The route the last successful request used, or null before the first success. */
export function followRoute(): FollowRoute | null {
  return resolvedRoute;
}

export function resetFollowRoute(): void {
  resolvedRoute = null;
}

function signedInWallet(): string | null {
  const session = accountSession();
  return session ? session.session.wallet.toLowerCase() : null;
}

function isCapabilityMiss(error: unknown): boolean {
  return error instanceof CapabilityUnavailableError || (error instanceof HttpError && error.status === 404);
}

export function toTransportError(error: unknown, route: FollowRoute): SocialTransportError {
  if (error instanceof SocialTransportError) return error;
  if (error instanceof CapabilityUnavailableError) return new SocialTransportError(error.status, error.code, route, error.message);
  if (error instanceof HttpError) return new SocialTransportError(error.status, error.envelope?.code ?? error.code, route, error.message);
  if (error instanceof TypeError && /Access token required|Session required/.test(error.message)) return new SocialTransportError(401, 'AUTH_REQUIRED', route, 'Sign in to follow');
  if (error instanceof Error) return new SocialTransportError(0, 'TRANSPORT_ERROR', route, error.message);
  return new SocialTransportError(0, 'TRANSPORT_ERROR', route, String(error));
}

interface ProxyEnvelope<T> { data: T; nextCursor?: string | null }
interface ProxyStatsRow { wallet?: unknown; wallet_address?: unknown; followers: unknown; following: unknown; is_following: unknown }

const integer = (value: unknown): number | null => {
  if (typeof value === 'number' && Number.isSafeInteger(value) && value >= 0) return value;
  if (typeof value === 'string' && /^(0|[1-9][0-9]{0,15})$/.test(value)) return Number(value);
  return null;
};

/** Normalise the two media/social stats shapes (interactions envelope or legacy plain row) into FollowStats. */
export function parseProxyStats(value: unknown, wallet: string): FollowStats {
  const row = (value && typeof value === 'object' && 'data' in value ? (value as ProxyEnvelope<unknown>).data : value) as ProxyStatsRow | null;
  if (!row || typeof row !== 'object') throw new SocialTransportError(502, 'MALFORMED_RESPONSE', 'social-proxy');
  const followers = integer(row.followers);
  const following = integer(row.following);
  const subject = typeof row.wallet === 'string' ? row.wallet : typeof row.wallet_address === 'string' ? row.wallet_address : wallet;
  if (followers === null || following === null || typeof row.is_following !== 'boolean' || !walletPattern.test(subject) || subject.toLowerCase() !== wallet) throw new SocialTransportError(502, 'MALFORMED_RESPONSE', 'social-proxy');
  return { wallet, followers, following, isFollowing: row.is_following, route: 'social-proxy' };
}

export function parseOperationStats(response: FollowStatsResponse, wallet: string): FollowStats {
  if (response.quality === 'quarantined') throw new SocialTransportError(503, 'INTEGRITY_QUARANTINED', 'operation');
  const { followers, following, isFollowing } = response.data;
  if (!Number.isSafeInteger(followers) || !Number.isSafeInteger(following) || followers < 0 || following < 0) throw new SocialTransportError(502, 'MALFORMED_RESPONSE', 'operation');
  return { wallet, followers, following, isFollowing, route: 'operation' };
}

interface ProxyFollowersRow { wallet_address?: unknown; followers?: unknown; items?: unknown }

export function parseProxyFollowers(value: unknown, wallet: string): FollowersPage {
  const envelope = value && typeof value === 'object' && 'data' in value ? (value as ProxyEnvelope<unknown>) : null;
  const row = (envelope ? envelope.data : value) as ProxyFollowersRow | null;
  if (!row || typeof row !== 'object') throw new SocialTransportError(502, 'MALFORMED_RESPONSE', 'social-proxy');
  const list = Array.isArray(row.followers) ? row.followers : Array.isArray(row.items) ? row.items : null;
  if (!list) throw new SocialTransportError(502, 'MALFORMED_RESPONSE', 'social-proxy');
  const wallets: string[] = [];
  for (const entry of list) {
    const candidate = typeof entry === 'string' ? entry : entry && typeof entry === 'object' ? (entry as { wallet?: unknown }).wallet : null;
    if (typeof candidate !== 'string' || !walletPattern.test(candidate)) throw new SocialTransportError(502, 'MALFORMED_RESPONSE', 'social-proxy');
    wallets.push(candidate.toLowerCase());
  }
  return { wallet, wallets, nextCursor: typeof envelope?.nextCursor === 'string' ? envelope.nextCursor : null, route: 'social-proxy' };
}

/**
 * A session-bound request to a gateway social route. The web origin is
 * cross-site to the gateway, so the session travels as `Authorization: Bearer`
 * with `credentials: 'omit'` (no cookie, so no CSRF token).
 */
async function proxyRequest(url: string, method: 'GET' | 'PUT' | 'DELETE', signal?: AbortSignal, idempotencyKey?: string): Promise<unknown> {
  const token = accountSession()?.accessToken;
  if (!token) throw new SocialTransportError(401, 'AUTH_REQUIRED', 'social-proxy', 'Sign in to follow');
  const headers: Record<string, string> = { Accept: 'application/json', Authorization: `Bearer ${token}` };
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;
  let response: Response;
  try {
    response = await fetch(url, { method, headers, credentials: 'omit', cache: 'no-store', redirect: 'error', signal });
  } catch (error) {
    if (signal?.aborted) throw error;
    throw new SocialTransportError(0, 'NETWORK_INTERRUPTED', 'social-proxy', "Can't reach the server, check your connection");
  }
  if (!response.ok) {
    const body: unknown = await response.json().catch(() => null);
    const code = body && typeof body === 'object' && typeof (body as { code?: unknown }).code === 'string' ? (body as { code: string }).code : `HTTP_${response.status}`;
    throw new SocialTransportError(response.status, code, 'social-proxy');
  }
  if (response.status === 204) return null;
  return response.json();
}

/**
 * Run the contract operation; on a capability miss (404/501) fall back to the
 * session-bound social route when there is one. Signed out there is none (the
 * gateway serves `/v2/social/*` only to a session), so the miss is reported.
 */
async function withFallback<T>(operation: () => Promise<T>, proxy: (() => Promise<T>) | null): Promise<T> {
  if (resolvedRoute !== 'social-proxy' || !proxy) {
    try {
      const value = await operation();
      resolvedRoute = 'operation';
      return value;
    } catch (error) {
      if (!isCapabilityMiss(error) || !proxy) throw toTransportError(error, 'operation');
    }
  }
  try {
    const value = await proxy();
    resolvedRoute = 'social-proxy';
    return value;
  } catch (error) {
    const mapped = toTransportError(error, 'social-proxy');
    if (mapped.status === 404 || mapped.status === 501) resolvedRoute = null;
    throw mapped;
  }
}

/**
 * Follower and following counts plus whether the signed-in viewer follows
 * `wallet`. Signed in, the IF_FOLLOW_STATS path is requested with the bearer so
 * the gateway's viewer-aware read fills `isFollowing`; signed out it goes
 * through the public operation and `isFollowing` is false.
 */
export function fetchFollowStats(walletAddress: string, signal?: AbortSignal): Promise<FollowStats> {
  const wallet = socialWallet(walletAddress);
  const viewer = signedInWallet();
  const publicRead = () => socialOperations().call('IF_FOLLOW_STATS', { path: { wallet } }, { signal });
  return withFallback(
    async () => {
      let response: FollowStatsResponse;
      if (!viewer) response = await publicRead();
      else {
        try {
          response = await browserSessionClient().requestV2(`/v2/profiles/${wallet}/follow-stats`, 'FollowStatsResponse', { service: 'media', authenticated: true, idempotentRead: true, timeoutMs: OPERATION_READ_TIMEOUT_FLOOR_MS, signal });
        } catch (error) {
          // Counts are public. A gateway that does not accept this session
          // (401/403) still answers the anonymous read; only isFollowing is lost.
          if (!(error instanceof HttpError && (error.status === 401 || error.status === 403))) throw error;
          response = await publicRead();
        }
      }
      return parseOperationStats(response, wallet);
    },
    // The interactions follow-stats read the gateway serves to a session.
    viewer ? async () => parseProxyStats(await proxyRequest(socialReadUrl(`/v2/social/users/${wallet}/follow-stats`), 'GET', signal), wallet) : null,
  );
}

/** Follow (`remove: false`) or unfollow (`remove: true`) `wallet` as the signed-in account. Resolves to the resulting following flag. */
export function setFollowing(walletAddress: string, remove: boolean, idempotencyKey: string = crypto.randomUUID()): Promise<{ following: boolean; route: FollowRoute }> {
  const wallet = socialWallet(walletAddress);
  const viewer = signedInWallet();
  if (!viewer) return Promise.reject(new SocialTransportError(401, 'AUTH_REQUIRED', resolvedRoute ?? 'operation', 'Sign in to follow'));
  if (viewer === wallet) return Promise.reject(new SocialTransportError(400, 'SELF_FOLLOW', resolvedRoute ?? 'operation', "You can't follow yourself"));
  return withFallback<{ following: boolean; route: FollowRoute }>(
    async () => {
      const operations = socialOperations();
      if (remove) {
        await operations.call('IF_UNFOLLOW', { path: { wallet } });
        return { following: false, route: 'operation' };
      }
      const response = await operations.call('IF_FOLLOW', { path: { wallet } });
      return { following: response.data.following, route: 'operation' };
    },
    async () => {
      await proxyRequest(socialWriteUrl(`/v2/social/follow/${wallet}`), remove ? 'DELETE' : 'PUT', undefined, idempotencyKey);
      return { following: !remove, route: 'social-proxy' };
    },
  );
}

/** Wallets following `wallet` (IF_FOLLOWERS, falling back to the social proxy list). */
export function fetchFollowers(walletAddress: string, cursor: string | null = null, limit = 50, signal?: AbortSignal): Promise<FollowersPage> {
  const wallet = socialWallet(walletAddress);
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > 50) throw new RangeError('Invalid followers page bound');
  const viewer = signedInWallet();
  return withFallback<FollowersPage>(
    async () => {
      const response = await socialOperations().call('IF_FOLLOWERS', { path: { wallet }, query: { limit, ...(cursor ? { cursor } : {}) } }, { signal });
      if (response.quality === 'quarantined') throw new SocialTransportError(503, 'INTEGRITY_QUARANTINED', 'operation');
      return { wallet, wallets: response.data.items.map(profile => socialWallet(profile.wallet)), nextCursor: response.nextCursor, route: 'operation' };
    },
    viewer ? async () => {
      const query = new URLSearchParams({ limit: String(limit), ...(cursor ? { cursor } : {}) });
      return parseProxyFollowers(await proxyRequest(socialReadUrl(`/v2/social/users/${wallet}/followers?${query}`), 'GET', signal), wallet);
    } : null,
  );
}
