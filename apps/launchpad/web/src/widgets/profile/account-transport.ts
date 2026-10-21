import type {ConnectedExtendedProfileResponse,ConnectedExtendedProfileBody,ConnectedExtendedPreferencesResponse,ConnectedExtendedPreferencesBody,ConnectedPortfolioV3Response,ConnectedL16IntervalSeries} from '@kindle/sdk-v2';
import { getAddress } from 'viem';
import { gatewayUrl, pnlApiUrl } from '@/core/sdk-config';
import { operationHeaders, signOperation, type OperationMethod } from '@/core/operation-envelope';
import type { WalletMessageSigner } from '@/core/wallet-sdk/auth';
import type { AuthChallenge, ProfileGetResponse, ProfilePatchResponse, ProfilePatchBody, Session, WatchlistGetResponse, WatchlistPutResponse } from '../../../../../../sdk/launchpad/packages/production-contracts/src/types.gen';
import { normalizeAddress, rawInteger, uint256Revision } from './account-disclosure';

export class AccountApiError extends Error {
  constructor(readonly status: number, readonly code: string, readonly fields: readonly { pointer: string; rule: string }[] = [], readonly retryable: boolean | null = null, readonly retryAfterMs: number | null = null) { super(code); }
}
export interface AccountSession { readonly session: Session; readonly accessToken: string; readonly epoch: number; readonly signal: AbortSignal }
/** Mutable credentials behind an installed scope: a refresh rotates the access token without changing the scope identity. */
interface LiveCredentials { session: Session; accessToken: string; signer: WalletMessageSigner | null }
const credentials = new WeakMap<AccountSession, LiveCredentials>();
let current: AccountSession | null = null;
let epoch = 0;
let controller = new AbortController();
let expiry: ReturnType<typeof setTimeout> | undefined;
let refreshing: Promise<void> | null = null;
/** The CSRF secret the gateway returned in the X-CSRF-Token response header (also the non-HttpOnly __Host-kindle_csrf cookie). */
let csrfSecret: string | null = null;
/** Rotate the 15-minute access token this long before it expires. */
const REFRESH_LEAD_MS = 60_000;
const listeners = new Set<() => void>();
let sessionChannel: BroadcastChannel | null = null;
const invalidators = new Map<object, () => void>();
export function registerAccountInvalidator(owner: object, invalidate: () => void) { invalidators.set(owner, invalidate); }
function invalidatePrivateViews() { for (const invalidate of invalidators.values()) invalidate(); }
function accountChannel() {
  if (!sessionChannel && typeof window !== 'undefined' && typeof BroadcastChannel !== 'undefined') {
    try { sessionChannel = new BroadcastChannel('kindle-account-session'); } catch { return null; }
    sessionChannel.onmessage = event => { if (event.data?.revokedSessionId === current?.session.sessionId) clearAccountSession(false); };
  }
  return sessionChannel;
}
export function accountSession() { return current; }
export function subscribeAccount(listener: () => void) { accountChannel(); listeners.add(listener); return () => { listeners.delete(listener); }; }
export function clearAccountSession(notifyPeers = true) {
  const revokedSessionId = current?.session.sessionId;
  if (notifyPeers && revokedSessionId) { try { accountChannel()?.postMessage({ revokedSessionId }); } catch {} }
  controller.abort(); controller = new AbortController(); current = null; epoch++; refreshing = null; csrfSecret = null;
  clearTimeout(expiry); invalidatePrivateViews(); for (const listener of listeners) listener();
}
function checkedSession(session: Session, accessToken: string): Session {
  const expiresAt = Date.parse(session.accessExpiresAt);
  if (!accessToken || !session.sessionId || session.revokedAt || !Number.isFinite(expiresAt) || expiresAt <= Date.now()) throw new Error('Authentication changed or expired');
  normalizeAddress(session.wallet);
  return Object.freeze({ ...session });
}
/** Re-arms the access-token timer: refresh shortly before expiry, and clear the session if the token runs out. */
function scheduleAccess(installed: AccountSession) {
  clearTimeout(expiry);
  const live = credentials.get(installed);
  if (!live || current !== installed) return;
  const expiresAt = Date.parse(live.session.accessExpiresAt);
  const refreshAt = expiresAt - REFRESH_LEAD_MS;
  const expire = () => {
    if (current !== installed) return;
    const remaining = expiresAt - Date.now();
    if (remaining <= 0) clearAccountSession();
    else expiry = setTimeout(expire, Math.min(2_147_483_647, remaining));
  };
  const tick = () => {
    if (current !== installed) return;
    if (Date.now() < refreshAt) { expiry = setTimeout(tick, Math.min(2_147_483_647, refreshAt - Date.now())); return; }
    // No refresh cookie (or a revoked session) leaves the current token usable until it expires.
    void refreshAccountSession(installed).catch(() => { if (current === installed) expire(); });
  };
  tick();
}
/**
 * Installs the session the gateway returned from POST /v2/auth/sessions.
 * `signer` is the wallet that proved the login; owner writes are signed with
 * it (operation envelope v2), so a session without a signer is read-only.
 */
export function installAccountSession(session: Session, accessToken: string, expectedEpoch: number, signer: WalletMessageSigner | null = null) {
  if (expectedEpoch !== epoch) throw new Error('Authentication changed or expired');
  const frozen = checkedSession(session, accessToken);
  controller.abort(); controller = new AbortController(); epoch++; refreshing = null;
  clearTimeout(expiry); invalidatePrivateViews();
  const live: LiveCredentials = { session: frozen, accessToken, signer };
  const installed: AccountSession = Object.freeze({ get session() { return live.session; }, get accessToken() { return live.accessToken; }, epoch, signal: controller.signal });
  credentials.set(installed, live);
  current = installed;
  scheduleAccess(installed);
  for (const listener of listeners) listener();
}
export function accountEpoch() { return epoch; }
export function requireAccount(wallet?: string) {
  const scope = current;
  if (!scope || scope.signal.aborted || Date.parse(scope.session.accessExpiresAt) <= Date.now()) { clearAccountSession(); throw new AccountApiError(401, 'AUTH_EXPIRED'); }
  if (wallet && normalizeAddress(wallet) !== normalizeAddress(scope.session.wallet)) throw new AccountApiError(403, 'SCOPE_DENIED');
  return scope;
}
export function assertAccount(scope: AccountSession) {
  if (scope !== current || scope.signal.aborted) throw new AccountApiError(401, 'ACCOUNT_CHANGED');
  if (!Number.isFinite(Date.parse(scope.session.accessExpiresAt)) || Date.parse(scope.session.accessExpiresAt) <= Date.now()) { clearAccountSession(); throw new AccountApiError(401, 'AUTH_EXPIRED'); }
}
async function decode<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const body: unknown = await response.json().catch(() => null);
    const error = typeof body === 'object' && body !== null ? body as { code?: unknown; fieldErrors?: unknown; retryable?: unknown; retryAfterMs?: unknown } : {};
    const fields = Array.isArray(error.fieldErrors) ? error.fieldErrors.filter((field): field is { pointer: string; rule: string } => !!field && typeof field.pointer === 'string' && typeof field.rule === 'string') : [];
    throw new AccountApiError(response.status, typeof error.code === 'string' ? error.code : `HTTP_${response.status}`, fields, typeof error.retryable === 'boolean' ? error.retryable : null, typeof error.retryAfterMs === 'number' && Number.isFinite(error.retryAfterMs) && error.retryAfterMs >= 0 ? error.retryAfterMs : null);
  }
  return response.status === 204 ? undefined as T : response.json() as Promise<T>;
}
export async function accountRequest<T>(url: string, init: RequestInit = {}, scope = requireAccount()): Promise<T> {
  assertAccount(scope);
  const headers = new Headers(init.headers); headers.set('Authorization', `Bearer ${scope.accessToken}`);
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  try {
    const read = !init.method || init.method === 'GET';
    for (let attempt = 0; ; attempt++) {
      try {
        const result = await decode<T>(await fetch(url, { ...init, headers, signal: scope.signal, credentials: 'omit', cache: 'no-store' }));
        assertAccount(scope); return result;
      } catch (failure) {
        // An envelope that says retryable:false (e.g. 503 DEPENDENCY_UNAVAILABLE for a route no service answers) is final, as in core/transient-retry.
        const transient = failure instanceof TypeError || (failure instanceof AccountApiError && failure.retryable !== false && [429, 502, 503, 504].includes(failure.status));
        if (!read || !transient || attempt >= 2 || scope.signal.aborted) throw failure;
        await new Promise<void>((resolve, reject) => {
          const cancelled = () => { clearTimeout(timer); reject(new DOMException('Account request cancelled', 'AbortError')); };
          const timer = setTimeout(() => { scope.signal.removeEventListener('abort', cancelled); resolve(); }, Math.min(4000, 500 * 2 ** attempt) + Math.floor(Math.random() * 250));
          scope.signal.addEventListener('abort', cancelled, { once: true });
          if (scope.signal.aborted) cancelled();
        });
        assertAccount(scope);
      }
    }
  } catch (error) {
    if (error instanceof AccountApiError && error.status === 401 && current === scope) clearAccountSession();
    throw error;
  }
}
/**
 * Identity endpoints are same-origin: next.config.ts rewrites /v2/auth/* on the
 * web origin to media/gateway, so the gateway's host-only __Host-kindle_*
 * cookies land on the page origin and the SIWE domain/uri match the page.
 */
function identityUrl(path: string) {
  if (typeof window === 'undefined') throw new Error('Sign-in runs in the browser');
  return new URL(path, window.location.origin).toString();
}
function rememberCsrf(response: Response) {
  const value = response.headers.get('X-CSRF-Token');
  if (value && /^[0-9a-f]{64}$/i.test(value)) csrfSecret = value;
}
/** CSRF secret for cookie-authenticated non-GET identity requests: the live cookie wins (another tab may have rotated it). */
function csrfToken(): string {
  if (typeof document !== 'undefined') {
    const entry = document.cookie.split(';').map(value => value.trim()).find(value => value.startsWith('__Host-kindle_csrf='));
    const value = entry?.slice('__Host-kindle_csrf='.length);
    if (value && /^[0-9a-f]{64}$/i.test(value)) return value;
  }
  if (csrfSecret) return csrfSecret;
  throw new AccountApiError(401, 'CSRF_UNAVAILABLE');
}
async function identityRequest<T>(path: string, init: RequestInit & { csrf?: boolean }, signal?: AbortSignal): Promise<{ value: T; response: Response }> {
  const headers = new Headers(init.headers);
  headers.set('Accept', 'application/json');
  if (init.body !== undefined && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  if (init.csrf) headers.set('X-CSRF-Token', csrfToken());
  const response = await fetch(identityUrl(path), { method: init.method, headers, body: init.body, signal, credentials: 'same-origin', cache: 'no-store', redirect: 'error' });
  const value = await decode<T>(response);
  rememberCsrf(response);
  return { value, response };
}
export async function loginChallenge(wallet: string, signal: AbortSignal) {
  return (await identityRequest<{ data: { challenge: AuthChallenge } }>('/v2/auth/challenges', { method: 'POST', headers: { 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ wallet: normalizeAddress(wallet), origin: window.location.origin }) }, signal)).value.data.challenge;
}
export async function loginProof(challengeId: string, wallet: string, signature: string, kind: 'eoa_eip191' | 'erc1271', signal: AbortSignal) {
  return (await identityRequest<{ data: { session: Session; accessToken: string } }>('/v2/auth/sessions', { method: 'POST', body: JSON.stringify({ challengeId, proof: { wallet, signature, kind }, deviceLabel: 'Web browser' }) }, signal)).value;
}
/**
 * POST /v2/auth/sessions:refresh: the gateway reads the __Host-kindle_refresh
 * cookie, checks Origin and X-CSRF-Token, and rotates access, refresh and CSRF
 * secrets. The installed scope keeps its identity; only its token changes.
 */
export function refreshAccountSession(scope = requireAccount()): Promise<void> {
  assertAccount(scope);
  if (refreshing) return refreshing;
  const work = (async () => {
    const live = credentials.get(scope);
    if (!live) throw new AccountApiError(401, 'ACCOUNT_CHANGED');
    const { value } = await identityRequest<{ data: { session: Session; accessToken: string } }>('/v2/auth/sessions:refresh', { method: 'POST', body: '{}', csrf: true }, scope.signal);
    if (scope !== current || scope.signal.aborted) throw new AccountApiError(401, 'ACCOUNT_CHANGED');
    const session = checkedSession(value.data.session, value.data.accessToken);
    if (session.sessionId !== live.session.sessionId || normalizeAddress(session.wallet) !== normalizeAddress(live.session.wallet)) throw new AccountApiError(401, 'ACCOUNT_CHANGED');
    live.session = session; live.accessToken = value.data.accessToken;
    scheduleAccess(scope);
    for (const listener of listeners) listener();
  })();
  refreshing = work;
  void work.finally(() => { if (refreshing === work) refreshing = null; }).catch(() => {});
  return work;
}
export async function signOutAccount() {
  const scope = current; const token = scope?.accessToken; const csrf = (() => { try { return csrfToken(); } catch { return null; } })();
  clearAccountSession();
  if (!scope || !token) return;
  const headers: Record<string, string> = { Accept: 'application/json', Authorization: `Bearer ${token}` };
  if (csrf) headers['X-CSRF-Token'] = csrf;
  await decode<void>(await fetch(identityUrl(`/v2/auth/sessions/${encodeURIComponent(scope.session.sessionId)}`), { method: 'DELETE', headers, credentials: 'same-origin', cache: 'no-store', redirect: 'error' }));
}

/**
 * An owner write to media/user through the gateway, authorized by the
 * operation envelope (shared/auth OperationAuthorizer): the wallet that proved
 * the login signs `action` bound to this method, path and exact body.
 */
async function signedAccountWrite<T>(path: string, method: OperationMethod, action: string, body: string, headers: Record<string, string>, scope: AccountSession): Promise<T> {
  assertAccount(scope);
  const signer = credentials.get(scope)?.signer;
  if (!signer) throw new AccountApiError(403, 'WALLET_SIGNATURE_REQUIRED');
  const wallet = getAddress(scope.session.wallet);
  const signed = await signOperation({ actor: wallet, action, method, path, body }, { signMessage: message => signer.signMessage(wallet, message), assertCurrent: () => assertAccount(scope) });
  return accountRequest<T>(gatewayUrl(path), { method, headers: { ...headers, ...operationHeaders(signed), 'X-Request-ID': crypto.randomUUID() }, ...(body ? { body } : {}) }, scope);
}
/** GET /v2/profiles/{wallet} on the gateway (public; media/user is mTLS-only in production). */
export async function publicProfile(wallet: string, signal?: AbortSignal) {
  return decode<ProfileGetResponse>(await fetchResponse(gatewayUrl(`/v2/profiles/${normalizeAddress(wallet)}`), signal));
}
export function patchProfile(wallet: string, body: ProfilePatchBody) {
  const path = `/v1/profiles/${normalizeAddress(wallet)}`;
  return signedAccountWrite<ProfilePatchResponse>(path, 'PATCH', 'profile.update', JSON.stringify(body), { 'If-Match': `"${body.expectedRevision}"` }, requireAccount(wallet));
}
export function watchlistPage(cursor?: string) {
  const query = new URLSearchParams({ limit: '50' }); if (cursor) query.set('cursor', cursor);
  return accountRequest<WatchlistGetResponse>(gatewayUrl(`/v1/me/watchlist?${query}`));
}
export function putWatch(pool: string, mutationId?: string) {
  return signedAccountWrite<WatchlistPutResponse>(`/v1/me/watchlist/${normalizeAddress(pool)}`, 'PUT', 'watchlist.update', JSON.stringify({ alertEnabled: false }), mutationId ? { 'Idempotency-Key': mutationId } : {}, requireAccount());
}
export function deleteWatch(pool: string, mutationId?: string) {
  return signedAccountWrite<void>(`/v1/me/watchlist/${normalizeAddress(pool)}`, 'DELETE', 'watchlist.remove', '', mutationId ? { 'Idempotency-Key': mutationId } : {}, requireAccount());
}

export type ExtendedProfile = ConnectedExtendedProfileResponse['data']['profile'];
export type ExtendedProfileBody = ConnectedExtendedProfileBody;
/** GET /v3/profiles/{wallet} on the gateway (public route; media/user is not reachable from browsers in production). */
export function extendedProfile(wallet: string, signal?: AbortSignal) { return fetchResponse(gatewayUrl(`/v3/profiles/${normalizeAddress(wallet)}`), signal).then(response => decode<ConnectedExtendedProfileResponse>(response)); }
/** Revision of a profile with no users.user_profiles row: media/user PATCH /v3/profiles/{wallet} creates the row when expectedRevision (and If-Match) name it. */
export const UNSAVED_PROFILE_REVISION = '0';
/** An empty presentation at revision 0, what the owner edits before the first save. */
export function emptyExtendedProfile(wallet: string): ExtendedProfile {
  return { wallet: normalizeAddress(wallet), displayName: '', bio: '', website: null, avatarAssetId: null, bannerAssetId: null, socials: {}, creatorPools: null, revision: UNSAVED_PROFILE_REVISION };
}
/** The owner's editable profile: a 404 (no row yet) is an empty profile at revision 0, so the first save creates it. */
export async function editableExtendedProfile(wallet: string, signal?: AbortSignal): Promise<ExtendedProfile> {
  try {
    return (await extendedProfile(wallet, signal)).data.profile;
  } catch (error) {
    if (error instanceof AccountApiError && error.status === 404) return emptyExtendedProfile(wallet);
    throw error;
  }
}
async function fetchResponse(url: string, signal?: AbortSignal) { return fetch(url, { signal, cache: 'no-store', credentials: 'omit' }); }
export function saveExtendedProfile(wallet: string, body: ExtendedProfileBody) { return signedAccountWrite<ConnectedExtendedProfileResponse>(`/v3/profiles/${normalizeAddress(wallet)}`, 'PATCH', 'profile.update', JSON.stringify(body), { 'If-Match': `"${body.expectedRevision}"`, 'Idempotency-Key': crypto.randomUUID() }, requireAccount(wallet)); }
export interface AccountAsset {assetId:string;ownerWallet:string;sha256:string;mediaType:string;bytes:number;state:'uploading'|'quarantined'|'rejected'|'approved';publicUrl:string|null}
export function uploadAccountAsset(wallet:string,id:string,file:File) { return accountRequest<AccountAsset>(gatewayUrl(`/v2/accounts/${normalizeAddress(wallet)}/assets/${id}`), {method:'PUT',headers:{'Content-Type':file.type,'Idempotency-Key':id},body:file}, requireAccount(wallet)); }
export function readAccountAsset(wallet:string,id:string) { return accountRequest<AccountAsset>(gatewayUrl(`/v2/accounts/${normalizeAddress(wallet)}/assets/${id}`),{},requireAccount(wallet)); }
export type AccountPreferences = ConnectedExtendedPreferencesResponse['data']['preferences'];
export type Consent = ConnectedExtendedPreferencesResponse['data']['consents'][number];
export function readPreferences() { return accountRequest<ConnectedExtendedPreferencesResponse>(gatewayUrl('/v3/me/preferences')); }
export function savePreferences(body:ConnectedExtendedPreferencesBody) { return signedAccountWrite<ConnectedExtendedPreferencesResponse>('/v3/me/preferences', 'PATCH', 'preferences.update', JSON.stringify(body), { 'If-Match': `"${body.expectedRevision}"`, 'Idempotency-Key': crypto.randomUUID() }, requireAccount()); }
export interface SourceWatermark {chainId:string;height:string;blockHash:string;projectionRevision:string;observedAt:string;integrity:string}
export interface TrackedPosition {positionId:string;chainId:string;wallet:string;tokenAddress:string;balanceRaw:string;knownQuantityRaw:string;unknownQuantityRaw:string;knownRemainingCostUsdlRaw:string;remainingCostUsdlRaw:string|null;realizedPnlRaw:string|null;knownRealizedSubtotalUsdlRaw:string;unrealizedPnlRaw:string|null;basisQuality:string;realizedBasisQuality:string;pricingQuality:string;markPrice:{baseAsset:{decimals:number|null};source:{observedAt:string}}|null;sourceWatermark:SourceWatermark}
export interface TrackedSummary {wallet:string;pricedSubtotalUsdlRaw:string;completeValueUsdlRaw:string|null;realizedPnlRaw:string|null;knownRealizedSubtotalUsdlRaw:string;basisQuality:string;pricingQuality:string;unpricedAssetCount:number;positionCount:number;snapshotId:string;watermark:SourceWatermark}
export type TrackedPortfolio = ConnectedPortfolioV3Response;
export function readPortfolio(cursor?:string) {const q=new URLSearchParams({limit:'50'});if(cursor)q.set('cursor',cursor);const scope=requireAccount();return accountRequest<TrackedPortfolio>(pnlApiUrl(`/v3/me/portfolio?${q}`),{},scope).then(value=>{assertAccount(scope);if(value.data.summary.wallet.toLowerCase()!==scope.session.wallet.toLowerCase()||value.data.positions.length>50||value.data.positions.some(row=>row.wallet.toLowerCase()!==scope.session.wallet.toLowerCase()||row.chainId!=='125'))throw new Error('Portfolio ownership or page bound mismatch');uint256Revision(value.data.summary.watermark.projectionRevision);for(const row of value.data.positions){normalizeAddress(row.tokenAddress);for(const amount of [row.balanceRaw,row.knownRemainingCostUsdlRaw])rawInteger(amount);for(const amount of [row.remainingCostUsdlRaw,row.realizedPnlRaw,row.unrealizedPnlRaw])if(amount!==null)rawInteger(amount,true);}return value;});}
export interface UtcInterval {from:string;to:string;interval:'1h'|'1d'}
export interface IntervalEntry {start:string;end:string;grossProceedsUsdlRaw:string;knownAllocatedBasisUsdlRaw:string;knownNetGainUsdlRaw:string;unknownProceedsUsdlRaw:string;realizedPnlUsdlRaw:string|null;tradeCount:number;saleCount:number;fees:{asset:{address:string|null;decimals:number|null};amountRaw:string}[];gasPaxRaw:string;accountingComplete:boolean}
export type IntervalSeries = ConnectedL16IntervalSeries;
export function readInterval(interval:UtcInterval) {
 const from=Date.parse(interval.from),to=Date.parse(interval.to);
 if(!Number.isFinite(from)||!Number.isFinite(to)||from>=to||new Date(from).toISOString()!==interval.from||new Date(to).toISOString()!==interval.to||!['1h','1d'].includes(interval.interval))throw new Error('Pick a valid date range');
 const scope=requireAccount();return accountRequest<IntervalSeries>(pnlApiUrl(`/v3/me/pnl/history?${new URLSearchParams({...interval,limit:'1000'})}`),{},scope).then(value=>{
  assertAccount(scope);if(value.wallet.toLowerCase()!==scope.session.wallet.toLowerCase()||value.entries.length>1000)throw new Error('History ownership or page bound mismatch');
  for(const row of value.entries){if(Date.parse(row.start)<from||Date.parse(row.end)>to||!Number.isFinite(Date.parse(row.start))||!Number.isFinite(Date.parse(row.end))||Date.parse(row.end)<=Date.parse(row.start))throw new Error('History row is outside the selected UTC interval');}
  return value;
 });
}
export interface ExportJob {id:string;state:string;manifest?:{snapshotId:string;format:string;expiresAt:string}|null}
export function requestExport(snapshotId:string,mutationId:string) {return accountRequest<ExportJob>(pnlApiUrl('/v2/me/portfolio/exports'),{method:'POST',headers:{'Idempotency-Key':mutationId},body:JSON.stringify({snapshotId,format:'csv'})});}
export function readExport(id:string) {return accountRequest<ExportJob>(pnlApiUrl(`/v2/me/portfolio/exports/${encodeURIComponent(id)}`));}

export async function downloadExport(id:string) {const scope=requireAccount();const response=await fetch(pnlApiUrl(`/v2/me/portfolio/exports/${encodeURIComponent(id)}/download`),{headers:{Authorization:`Bearer ${scope.accessToken}`},signal:scope.signal,credentials:'omit',cache:'no-store'});if(!response.ok)await decode(response);const blob=await response.blob();assertAccount(scope);return blob;}
