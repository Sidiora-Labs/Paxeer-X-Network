import { gatewayUrl, socialWriteUrl } from '@/core/sdk-config';
import { accountSession } from '@/widgets/profile/account-transport';

export type SendState = 'submitting' | 'persisted' | 'delivered' | 'rejected' | 'uncertain';
export interface PoolMessage {
  id: string; poolAddress: string; sender: string; content: string; replyToId: string | null;
  createdAt: number; sequence: string; revision: string; deleted: boolean;
  conversationId: string; clientMessageId?: string; sendState?: SendState;
}
export interface DmMessage extends Omit<PoolMessage, 'poolAddress'> { poolAddress?: string }
export interface DmConversation { id: string; peer: string; lastMessageAt: number | null; unreadCount: number; readThroughSequence: string; muted: boolean }
export interface HistoryPage<T> { messages: T[]; hasMore: boolean; nextCursor: string; resetRequired: boolean; readThroughSequence: string }
export class ChatError extends Error {
  constructor(readonly status: number, readonly code: string, readonly nextCursor?: string) { super(code); this.name = 'ChatError'; }
}
/**
 * The signed-in session's access token for a gateway request. The web origin
 * is cross-site to the gateway, so the session travels as a bearer with
 * `credentials: 'omit'`; bearer requests need no CSRF token (gateway
 * auth/identity_transport.go AuthenticateRequest).
 */
function bearer(): string {
  const token = accountSession()?.accessToken;
  if (!token) throw new ChatError(401, 'Application sign-in required');
  return token;
}
export async function chatRequest(path: string, init: RequestInit = {}): Promise<unknown> {
  const headers = new Headers(init.headers); headers.set('Accept', 'application/json'); headers.set('Authorization', `Bearer ${bearer()}`);
  const response = await fetch(socialWriteUrl(path), { ...init, credentials: 'omit', cache: 'no-store', redirect: 'error', headers });
  const value: unknown = await response.json().catch(() => { throw new ChatError(response.status, `Chat request failed (${response.status})`); });
  if (!response.ok) {
    const error = value as { code?: string; error?: string; next_cursor?: string };
    throw new ChatError(response.status, error.code ?? error.error ?? `Chat request failed (${response.status})`, error.next_cursor);
  }
  return value;
}
function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid chat envelope');
  return value as Record<string, unknown>;
}
function text(value: unknown, name: string): string { if (typeof value !== 'string') throw new Error(`Invalid ${name}`); return value; }
export function decodeMessage(value: unknown): PoolMessage {
  const row = record(value);
  const sequence = text(row.sequence, 'sequence'), revision = text(row.revision, 'revision');
  if (!/^[1-9][0-9]{0,18}$/.test(sequence) || !/^[1-9][0-9]{0,18}$/.test(revision)) throw new Error('Invalid durable position');
  if (typeof row.created_at !== 'number' || !Number.isSafeInteger(row.created_at) || typeof row.deleted !== 'boolean') throw new Error('Invalid chat timestamp or tombstone');
  return { id: text(row.id, 'id'), poolAddress: typeof row.pool_address === 'string' ? row.pool_address : '', conversationId: text(row.conversation_id, 'conversation'), clientMessageId: typeof row.client_message_id === 'string' ? row.client_message_id : undefined, sender: text(row.sender, 'sender'), content: row.deleted ? '' : text(row.content, 'content'), replyToId: typeof row.reply_to_id === 'string' && !row.deleted ? row.reply_to_id : null, createdAt: row.created_at, sequence, revision, deleted: row.deleted, sendState: 'persisted' };
}
async function history(conversationId: string, opts?: { limit?: number; before?: string; cursor?: string; signal?: AbortSignal }): Promise<HistoryPage<PoolMessage>> {
  const params = new URLSearchParams({ conversation_id: conversationId, limit: String(opts?.limit ?? 50) });
  const cursor = opts?.cursor ?? opts?.before;
  if (cursor) params.set('cursor', cursor);
  const data = record(await chatRequest(`/v2/chat/history?${params}`, { signal: opts?.signal }));
  if (!Array.isArray(data.messages)) throw new Error('Invalid history rows');
  const nextCursor = text(data.next_cursor, 'history cursor');
  return { messages: data.messages.map(decodeMessage), hasMore: data.messages.length === (opts?.limit ?? 50), nextCursor, resetRequired: data.reset_required === true, readThroughSequence: text(data.read_through_sequence, 'read marker') };
}
export const getPoolMessages = (pool: string, opts?: Parameters<typeof history>[1]) => history(`pool:${pool.toLowerCase()}`, opts);
export const getDmMessages = (id: string, opts?: Parameters<typeof history>[1]) => history(id, opts);
export async function getDmConversations(signal?: AbortSignal): Promise<DmConversation[]> {
  const data = record(await chatRequest('/v2/chat/conversations', { signal }));
  if (!Array.isArray(data.conversations)) throw new Error('Invalid conversation list');
  return data.conversations.map(value => { const row = record(value); if (typeof row.unread_count !== 'number' || !Number.isSafeInteger(row.unread_count) || row.unread_count < 0 || typeof row.muted !== 'boolean') throw new Error('Invalid conversation unread state'); return { id: text(row.id, 'conversation id'), peer: text(row.peer, 'peer'), lastMessageAt: typeof row.last_message_at === 'number' ? row.last_message_at : null, unreadCount: row.unread_count, muted: row.muted, readThroughSequence: text(row.read_sequence, 'read sequence') }; });
}
export async function markConversationRead(id: string, through: string) {
  await chatRequest('/v2/chat/read', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ conversation_id: id, through_sequence: through }) });
}
export async function deletePoolMessage(pool: string, id: string, expectedRevision?: string): Promise<boolean> {
  await chatRequest('/v2/chat/revise', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ message_id: id, expected_revision: expectedRevision, content: '', remove: true }) });
  return true;
}
export interface ChatTicket { ticket: string; expiresAt: string; subprotocol: 'sidiora.v2' }
/** sha256 of the JSON array of the sorted topics, lowercase hex (shared/auth contract.go StreamFilterDigest). */
export async function streamFilterDigest(topics: readonly string[]): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify([...topics].sort())));
  return [...new Uint8Array(digest)].map(value => value.toString(16).padStart(2, '0')).join('');
}
/**
 * POST /v2/stream/tickets on the gateway identity layer (auth/identity_transport.go).
 * The binding's origin must equal the request's Origin header (the page
 * origin) and chat topics route the request to the chat ticket issuer
 * (auth/identity_chat.go chatBinding). The ticket is single use and expires
 * after 30 seconds.
 */
export async function issueChatTicket(topics: string[], signal: AbortSignal): Promise<ChatTicket> {
  const filterDigest = await streamFilterDigest(topics);
  const token = bearer();
  const response = await fetch(gatewayUrl('/v2/stream/tickets'), { method: 'POST', credentials: 'omit', cache: 'no-store', redirect: 'error', signal, headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ schemaMajor: 2, topics, filterDigest, origin: window.location.origin, endpoint: '/v2/stream' }) });
  const raw: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const code = raw && typeof raw === 'object' && typeof (raw as { code?: unknown }).code === 'string' ? (raw as { code: string }).code : 'Chat authorization unavailable';
    throw new ChatError(response.status, code);
  }
  const envelope = record(raw), data = record(envelope.data);
  const expiresAt = text(data.expiresAt, 'ticket expiry'), ticket = text(data.ticket, 'ticket');
  if (!/^[0-9a-f]{64}$/.test(ticket) || data.subprotocol !== 'sidiora.v2' || !Number.isFinite(Date.parse(expiresAt)) || Date.parse(expiresAt) <= Date.now() || Date.parse(expiresAt) > Date.now() + 60_000) throw new Error('Invalid chat upgrade ticket');
  return { ticket, expiresAt, subprotocol: 'sidiora.v2' };
}

export async function reviseChatMessage(id: string, expectedRevision: string, content: string, remove = false): Promise<PoolMessage> {
 return decodeMessage(await chatRequest('/v2/chat/revise', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ message_id:id, expected_revision:expectedRevision, content, remove }) }));
}
