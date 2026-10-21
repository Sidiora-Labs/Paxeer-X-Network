import { SdkClient, type V2ContractTypes } from '@kindle/sdk';
import { dataApiUrl, gatewayUrl, metadataApiUrl } from '@/core/sdk-config';
import { backendOrigins } from '@/core/backend-origins';
import { toUnsupported, type Unsupported } from '@/core/market/unsupported';
import type { TokenMetadata } from '@/widgets/home/types';
import { normalizeAddress } from './navigation';

export interface SearchEntity {
  identity: string; chainId: 125; kind: 'pool' | 'token';
  token_address?: string; pool_address?: string; name: string; symbol: string;
  images: { logo: string | null }; marketCap?: string;
}
export interface UserSearchHit { identity: string; wallet: string; label: string }
export type UserSearch = { state: 'ready'; items: UserSearchHit[] } | Unsupported;
export interface SearchOutcome { coins: SearchEntity[]; users: UserSearch }
export const USER_SEARCH_OPERATION = 'IF_SEARCH type=profile';
export function normalizedSearch(query: string): string {
  const value = query.trim().normalize('NFKC');
  if (value.length > 80) throw new TypeError('Search is limited to 80 characters.');
  return /^0x[0-9a-fA-F]{40}$/.test(value) ? normalizeAddress(value) : value;
}
export function searchClient(): SdkClient {
  const streamOrigin = backendOrigins().stream;
  return new SdkClient({ apiOrigin: new URL(dataApiUrl('')).origin,
    identityOrigin: new URL(gatewayUrl('')).origin, mediaOrigin: new URL(gatewayUrl('')).origin,
    streamOrigin, approvedStreamOrigins: [streamOrigin], production: process.env.NODE_ENV === 'production' });
}
async function searchMetadataBatch(tokens: string[], signal: AbortSignal): Promise<Record<string, TokenMetadata | null>> {
  if (!tokens.length) return {};
  if (tokens.length > 20) throw new RangeError('Search metadata exceeds20 entities');
  const response = await fetch(metadataApiUrl('/metadata/batch?addresses=' + encodeURIComponent(tokens.join(','))), { signal, credentials: 'omit', redirect: 'error' });
  if (!response.ok || !response.body || !response.headers.get('content-type')?.toLowerCase().includes('application/json')) throw new Error('Search metadata is unavailable.');
  const reader = response.body.getReader(); const parts: Uint8Array[] = []; let size = 0;
  try { for (;;) { const { done, value } = await reader.read(); if (done) break; size += value.length; if (size > 1048576) throw new RangeError('Search metadata response exceeds1MiB'); parts.push(value); } }
  finally { await reader.cancel(); }
  signal.throwIfAborted();
  const bytes = new Uint8Array(size); let offset = 0; for (const part of parts) { bytes.set(part, offset); offset += part.length; }
  return checkedSearchMetadata(JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)), tokens);
}
export function checkedSearchMetadata(value: unknown, tokens: string[]): Record<string, TokenMetadata | null> {
  if (tokens.length > 20 || new Set(tokens).size !== tokens.length || tokens.some(token => normalizeAddress(token) !== token)) throw new TypeError('Invalid metadata request identity');
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Invalid search metadata batch');
  const source = value as Record<string, unknown>, result: Record<string, TokenMetadata | null> = {};
  if (Object.keys(source).some(token => !tokens.includes(token))) throw new TypeError('Unexpected search metadata identity');
  for (const token of tokens) {
    const row = source[token];
    if (row === null) { result[token] = null; continue; }
    if (!row || typeof row !== 'object' || Array.isArray(row)) throw new TypeError('Search metadata entry missing');
    const record = row as Record<string, unknown>;
    if (typeof record.token_address !== 'string' || normalizeAddress(record.token_address) !== token) throw new TypeError('Search metadata identity mismatch');
    for (const field of ['name', 'symbol']) if (record[field] != null && (typeof record[field] !== 'string' || (record[field] as string).length > 256)) throw new TypeError('Invalid search display field');
    if (record.pool_address != null && typeof record.pool_address !== 'string') throw new TypeError('Invalid pool identity');
    const pool = typeof record.pool_address === 'string' ? normalizeAddress(record.pool_address) : null;
    result[token] = { token_address: token, pool_address: pool, name: typeof record.name === 'string' ? record.name : null, symbol: typeof record.symbol === 'string' ? record.symbol : null };
  }
  return result;
}
function searchParams(q: string, type: 'pool' | 'token' | 'profile'): string {
  const encoded = encodeURIComponent(q).replace(/[!'()*]/g, value => '%' + value.charCodeAt(0).toString(16).toUpperCase());
  return `q=${encoded}&type=${type}&limit=10`;
}
/** A failed profile search is only "unavailable" for the statuses that mean the backend does not serve it (412 today); anything else stays an error. */
export function userSearchFailure(error: unknown): Unsupported {
  const marker = toUnsupported(USER_SEARCH_OPERATION, error);
  if (marker) return marker;
  throw error;
}
export async function searchUsers(query: string, signal: AbortSignal, client: SdkClient = searchClient()): Promise<UserSearch> {
  const q = normalizedSearch(query);
  if (q.length < 2) return { state: 'ready', items: [] };
  let result: V2ContractTypes['SearchResponse'];
  try { result = await client.requestV2(`/v2/search?${searchParams(q, 'profile')}`, 'SearchResponse', { signal }); }
  catch (error) { signal.throwIfAborted(); return userSearchFailure(error); }
  signal.throwIfAborted();
  if (result.data.items.length > 10) throw new RangeError('Search result limit exceeded');
  const unique = new Map<string, UserSearchHit>();
  for (const item of result.data.items) {
    if (item.kind !== 'profile') throw new TypeError('Search returned a different entity type.');
    if (typeof item.label !== 'string' || item.label.length > 256) throw new TypeError('Invalid search label');
    if (!item.wallet) throw new TypeError('Search result has no wallet.');
    const wallet = normalizeAddress(item.wallet);
    unique.set(wallet, { identity: `125:profile:${wallet}`, wallet, label: item.label });
  }
  return { state: 'ready', items: [...unique.values()] };
}
export async function searchAll(query: string, signal: AbortSignal): Promise<SearchOutcome> {
  const client = searchClient();
  const [coins, users] = await Promise.all([searchEntities(query, signal, client), searchUsers(query, signal, client)]);
  return { coins, users };
}
export async function searchEntities(query: string, signal: AbortSignal, client: SdkClient = searchClient()): Promise<SearchEntity[]> {
  const q = normalizedSearch(query);
  if (q.length < 2) return [];
  if (/^0x/i.test(q) && !/^0x[0-9a-f]{40}$/.test(q)) throw new TypeError('Enter a complete 40-digit address.');
  const matches: V2ContractTypes['SearchResult'][] = [];
  for (const type of ['pool', 'token'] as const) {
    signal.throwIfAborted();
    const result = await client.requestV2(`/v2/search?${searchParams(q, type)}`, 'SearchResponse', { signal });
    signal.throwIfAborted();
    if (result.data.items.length > 10) throw new RangeError('Search result limit exceeded');
    for (const item of result.data.items) {
      if (typeof item.label !== 'string' || item.label.length > 256) throw new TypeError('Invalid search label');
      if (item.kind !== type) throw new TypeError('Search returned a different entity type.');
      const raw = item.kind === 'pool' ? item.poolAddress : item.tokenAddress;
      if (!raw) throw new TypeError('Search result has no entity address.');
      const address = normalizeAddress(raw);
      if (/^0x[0-9a-f]{40}$/.test(q) && address !== q) continue;
      matches.push(item);
    }
  }
  const tokens = [...new Set(matches.flatMap(row => row.tokenAddress ? [normalizeAddress(row.tokenAddress)] : []))];
  const metadata = await searchMetadataBatch(tokens, signal);
  signal.throwIfAborted();
  const unique = new Map<string, SearchEntity>();
  for (const row of matches) {
    if (row.kind === 'profile') continue;
    const token = row.tokenAddress ? normalizeAddress(row.tokenAddress) : undefined;
    const pool = row.poolAddress ? normalizeAddress(row.poolAddress) : token && metadata[token]?.pool_address ? normalizeAddress(metadata[token]!.pool_address!) : undefined;
    const address = row.kind === 'pool' ? pool : token;
    if (!address) throw new TypeError('Search identity is unavailable.');
    const identity = `125:${row.kind}:${address}`;
    const meta = token ? metadata[token] : null;
    unique.set(identity, { identity, chainId: 125, kind: row.kind, token_address: token,
      pool_address: pool, name: meta?.name || row.label, symbol: meta?.symbol || '', images: { logo: meta?.images?.logo || null } });
  }
  return [...unique.values()].slice(0, 20);
}
