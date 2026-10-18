import type { PortfolioSummary } from '../../../../../packages/production-contracts/src/types.gen';
/** Presentation-only contracts. No PnL arithmetic or wallet connection -> session inference. */
export type Quality = 'complete' | 'partial' | 'unknown';
export type AccountState = 'disconnected' | 'authenticating' | 'loading' | 'partial' | 'ready' | 'stale' | 'failed';
export interface AccountIdentity { wallet: string; sessionId: string; chainId: '125' }
export function normalizeAddress(value: string): string {
  if (!/^0x[0-9a-fA-F]{40}$/.test(value)) throw new Error('Invalid chain125 address');
  return value.toLowerCase();
}
export function rawInteger(raw: string, signed = false): bigint {
  if (raw.replace(/^-/, '').length > 156 || !(signed ? /^-?(0|[1-9][0-9]*)$/ : /^(0|[1-9][0-9]*)$/).test(raw)) throw new Error('Invalid raw amount');
  return BigInt(raw);
}
export function uint256Revision(raw: string, positive = false): bigint {
  const revision = rawInteger(raw);
  if (revision >= (1n << 256n) || (positive && revision === 0n)) throw new Error('Invalid uint256 revision');
  return revision;
}
/** Exact decimal rendering, including zero; never Number() financial amounts. */
export function exactAmount(raw: string | null, decimals = 6, unit = 'USDL'): string {
  if (raw === null) return '—';
  if (!Number.isInteger(decimals) || decimals < 0 || decimals > 36) throw new Error('Invalid decimals');
  const n = rawInteger(raw, true), negative = n < 0n;
  const digits = (negative ? -n : n).toString().padStart(decimals + 1, '0');
  const text = decimals ? `${digits.slice(0, -decimals)}.${digits.slice(-decimals)}` : digits;
  return `${negative ? '-' : ''}${text} ${unit}`;
}
export interface PortfolioDisclosure extends Pick<PortfolioSummary, 'wallet' | 'basisQuality' | 'pricingQuality' | 'pricedSubtotalUsdlRaw' | 'completeValueUsdlRaw' | 'realizedPnlRaw' | 'knownRealizedSubtotalUsdlRaw'> {
  wallet: string; chainId: '125'; asOfHeight: string; coverageStart: string | null;
  accountingMethodVersion: string | null; basisQuality: Quality;
  pricingQuality: 'complete' | 'partial' | 'unpriced';
  pricedSubtotalUsdlRaw: string; completeValueUsdlRaw: string | null;
  realizedPnlRaw: string | null; knownRealizedSubtotalUsdlRaw: string;
  markTimestamp: string | null;
}
export function portfolioLabels(dto: PortfolioDisclosure) {
  if (dto.chainId !== '125') throw new Error('Wrong chain');
  normalizeAddress(dto.wallet); rawInteger(dto.asOfHeight);
  return {
    inventory: "Coins you've traded here, separate from your wallet balance",
    method: dto.accountingMethodVersion ?? 'Not available yet',
    coverage: dto.basisQuality === 'complete' ? 'Full trade history' : `${dto.basisQuality === 'partial' ? 'Partial' : 'Incomplete'} trade history${dto.coverageStart ? ` since ${dto.coverageStart}` : ''}`,
    value: dto.pricingQuality === 'complete' ? exactAmount(dto.completeValueUsdlRaw) : '—',
    subtotal: exactAmount(dto.pricedSubtotalUsdlRaw),
    subtotalLabel: dto.pricingQuality === 'complete' ? 'Value' : 'Value (some prices missing)',
    realized: dto.basisQuality === 'complete' ? exactAmount(dto.realizedPnlRaw) : '—',
    knownRealized: exactAmount(dto.knownRealizedSubtotalUsdlRaw),
    mark: dto.markTimestamp ? `Prices as of ${dto.markTimestamp}` : 'Price time not available yet',
  };
}
/** Volatile account cache: identity includes authenticated session, never persisted to storage. */
export class PrivateAccountCache<T> {
  private key: string | null = null;
  private generation = 0;
  private snapshot: T | undefined;
  select(identity: AccountIdentity | null): number {
    const next = identity ? `${normalizeAddress(identity.wallet)}:${identity.chainId}:${identity.sessionId}` : null;
    if (identity && (!identity.sessionId || identity.chainId !== '125')) throw new Error('Authenticated chain125 session required');
    if (next !== this.key || next === null) { this.generation++; this.snapshot = undefined; this.key = next; }
    return this.generation;
  }
  commit(generation: number, snapshot: T): boolean {
    if (!this.key || generation !== this.generation) return false;
    this.snapshot = snapshot; return true;
  }
  read(): T | undefined { return this.snapshot; }
  clear(): void { this.select(null); }
}
export interface ProfileDraft { displayName?: string; bio?: string; avatarAssetId?: string | null }
export function profileMutation(expectedRevision: string, draft: ProfileDraft) {
  uint256Revision(expectedRevision);
  if (draft.displayName !== undefined && draft.displayName.length > 80) throw new Error('displayName: maximum80 characters');
  if (draft.bio !== undefined && draft.bio.length > 500) throw new Error('bio: maximum500 characters');
  if (draft.avatarAssetId != null && !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(draft.avatarAssetId)) throw new Error('avatarAssetId: invalid UUID');
  // Allowlist only frozen ProfilePatchBody fields; private/UI extras cannot leak.
  return { expectedRevision, ...(draft.displayName !== undefined ? { displayName: draft.displayName } : {}), ...(draft.bio !== undefined ? { bio: draft.bio } : {}), ...(draft.avatarAssetId !== undefined ? { avatarAssetId: draft.avatarAssetId } : {}) };
}
export function profileConflict(draft: ProfileDraft, serverRevision: string) {
  uint256Revision(serverRevision);
  return { state: 'conflict' as const, draft: { ...draft }, serverRevision, message: 'Your profile changed somewhere else. Reload and try again', canAutoRetry: false as const };
}
/** Explicit opt-in union preview only. Latest server order wins; local-only items append. */
export function mergeWatchlist(server: readonly string[], local: readonly string[], approved: boolean): string[] {
  if (server.length > 1000 || local.length > 1000) throw new Error('Watchlist limit exceeded');
  const result = [...new Set((approved ? [...server, ...local] : [...server]).map(normalizeAddress))];
  // Frozen WatchlistEntry.order max499; do not silently send out-of-schema entries.
  if (result.length > 500) throw new Error('Frozen watchlist capacity500; larger limit requires contract revision');
  return result;
}
export interface PendingWatchlistMutation { mutationId: string; wallet: string; poolAddress: string; baseRevision: string; expiresAt: number }
export function watchlistReplay(m: PendingWatchlistMutation, wallet: string, revision: string, now: number) {
  if (normalizeAddress(m.wallet) !== normalizeAddress(wallet)) return 'wrong-account' as const;
  if (!m.mutationId || !Number.isFinite(m.expiresAt) || m.expiresAt <= now) return 'expired' as const;
  uint256Revision(m.baseRevision); uint256Revision(revision); normalizeAddress(m.poolAddress);
  return m.baseRevision === revision ? 'ready' as const : 'reconcile' as const;
}
