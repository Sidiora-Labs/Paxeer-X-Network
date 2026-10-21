import { gatewayUrl } from '@/core/sdk-config';
import { accountSession } from '@/widgets/profile/account-transport';

export type Inbox = 'everyone' | 'followed_only' | 'nobody';
export type Privacy = { inbox: Inbox; revision: string };
export type FollowStats = { wallet: string; followers: string; following: string; is_following: boolean; revision: string };
export type Comment = { commentId: string; poolAddress: string; authorWallet: string; parentCommentId: string | null; body: string; likeCount: string; revision: string; edited: boolean };
type ResponseEnvelope<T> = { data: T; nextCursor: string | null };
export class InteractionError extends Error {
  constructor(readonly code: string, readonly retryable: boolean, readonly status: number) { super(code); }
}
const csrfPrefix = '__Host-kindle_csrf=';
/** Double-submit CSRF secret from the gateway cookie session, or undefined under the bearer session. */
function interactionCsrfToken(cookieHeader: string = typeof document === 'undefined' ? '' : document.cookie): string | undefined {
  const value = cookieHeader.split(';').map(part => part.trim()).find(part => part.startsWith(csrfPrefix))?.slice(csrfPrefix.length);
  return value && /^[0-9a-f]{64}$/u.test(value) ? value : undefined;
}
/**
 * Client for the gateway's connected social routes (`/v2/social/*`, which the
 * gateway forwards to media/social with the trusted actor). The base defaults
 * to the gateway host from `gatewayUrl`; a relative prefix keeps same-origin
 * deployments working. Writes carry the bearer session when one is installed
 * and the cookie session's CSRF token when that cookie exists.
 */
export class InteractionClient {
  readonly prefix: string;
  constructor(prefix: string = gatewayUrl('/v2/social')) {
    if (/[?#\\\s]/u.test(prefix) || prefix.startsWith('//')) throw new Error('Invalid interaction route');
    if (prefix.startsWith('/')) this.prefix = prefix.replace(/\/$/, '');
    else {
      const url = new URL(prefix);
      if (url.protocol !== 'https:' && url.hostname !== 'localhost' && url.hostname !== '127.0.0.1') throw new Error('Invalid interaction route');
      this.prefix = url.href.replace(/\/$/, '');
    }
  }
  private async request<T>(path: string, method = 'GET', body?: unknown, key?: string, signal?: AbortSignal): Promise<ResponseEnvelope<T>> {
    const token = accountSession()?.accessToken;
    let csrf: string | undefined;
    if (method !== 'GET' && method !== 'HEAD') {
      csrf = interactionCsrfToken();
      if (!csrf && !token) throw new InteractionError('AUTH_REQUIRED', false, 401);
    }
    const response = await fetch(this.prefix + path, {
      method, credentials: csrf ? 'include' : 'omit', cache: 'no-store', redirect: 'error', signal,
      headers: { Accept: 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(csrf ? { 'X-CSRF-Token': csrf } : {}), ...(body === undefined ? {} : { 'Content-Type': 'application/json' }), ...(key ? { 'Idempotency-Key': key } : {}) },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
    if (!response.ok) {
      const value: unknown = await response.json().catch(() => null);
      const error = value && typeof value === 'object' ? value as Record<string, unknown> : {};
      throw new InteractionError(typeof error.code === 'string' ? error.code : 'REQUEST_FAILED', error.retryable === true, response.status);
    }
    if (response.status === 204) return { data: null as T, nextCursor: null };
    return response.json() as Promise<ResponseEnvelope<T>>;
  }
  stats(wallet: string, signal?: AbortSignal) { return this.request<FollowStats>(`/users/${encodeURIComponent(wallet)}/follow-stats`, 'GET', undefined, undefined, signal); }
  follow(wallet: string, remove: boolean, key: string) { return this.request<{ success: boolean }>(`/follow/${encodeURIComponent(wallet)}`, remove ? 'DELETE' : 'PUT', undefined, key); }
  comments(pool: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: '50', ...(cursor ? { cursor } : {}) });
    return this.request<{ items: Comment[] }>(`/pool/${encodeURIComponent(pool)}/comments?${query}`, 'GET', undefined, undefined, signal);
  }
  comment(pool: string, body: string, clientCommentId: string, parentCommentId?: string) {
    return this.request<{ comment: Comment }>(`/pool/${encodeURIComponent(pool)}/comments`, 'POST', { body, clientCommentId, ...(parentCommentId ? { parentCommentId } : {}) }, clientCommentId);
  }
  edit(comment: Comment, body: string, key: string) { return this.request<{ success: boolean }>(`/comments/${encodeURIComponent(comment.commentId)}`, 'PATCH', { body, expectedRevision: comment.revision }, key); }
  remove(comment: Comment, key: string) { return this.request<{ success: boolean }>(`/comments/${encodeURIComponent(comment.commentId)}`, 'DELETE', { expectedRevision: comment.revision }, key); }
  hide(comment: Comment, reason: string, key: string) { return this.request<{ success: boolean }>(`/comments/${encodeURIComponent(comment.commentId)}/hide`, 'POST', { poolAddress: comment.poolAddress, reason, expectedRevision: comment.revision }, key); }
  react(id: string, remove: boolean, key: string) { return this.request<{ success: boolean }>(`/comments/${encodeURIComponent(id)}/like`, remove ? 'DELETE' : 'PUT', undefined, key); }
  callout(id: string, revision: string, mentions: string[], transactionHash: string, key: string) {
    return this.request<{ success: boolean }>(`/callouts/${encodeURIComponent(id)}/references`, 'POST', { revision, mentions, transactionHash }, key);
  }
  privacy(signal?: AbortSignal) { return this.request<Privacy>('/me/social/privacy', 'GET', undefined, undefined, signal); }
  setPrivacy(inbox: Inbox, expectedRevision: string) { return this.request<{ success: boolean }>('/me/social/privacy', 'PATCH', { inbox, expectedRevision }); }
  block(wallet: string, remove = false) { return this.request<{ success: boolean }>(`/me/social/blocks/${encodeURIComponent(wallet)}`, remove ? 'DELETE' : 'PUT'); }
}
