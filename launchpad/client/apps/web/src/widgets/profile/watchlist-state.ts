import { mergeWatchlist, normalizeAddress, uint256Revision } from './account-disclosure';
import { actionFailureMessage, isNotServedError } from '@/core/market/unsupported';
import { AccountApiError, subscribeAccount, accountSession, assertAccount, deleteWatch, putWatch, requireAccount, watchlistPage, type AccountSession } from './account-transport';
export interface WatchOperation { mutationId: string; wallet: string; poolAddress: string; add: boolean; baseRevision: string; expiresAt: number }
export interface WatchState { scope: AccountSession | null; list: string[]; revision: string; pending: readonly WatchOperation[]; local: string[]; error: string | null; loading: boolean }
const key = 'sidiora_watchlist';
const initial: WatchState = { scope: null, list: [], revision: '0', pending: [], local: [], error: null, loading: false };
let state = initial;
const listeners = new Set<() => void>();
let volatileLocal: string[] | null = null;
let localReadError: string | null = null;
const activeOperations = new Set<string>();
function update(next: Partial<WatchState>) { state = { ...state, ...next }; for (const listener of listeners) listener(); }
export const watchState = () => state;
export function subscribeWatch(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; }
export function localWatchlist(): string[] {
  if (typeof window === 'undefined') return [];
  if (volatileLocal) return [...volatileLocal];
  localReadError = null;
  try {
    const value: unknown = JSON.parse(localStorage.getItem(key) ?? '[]');
    if (!Array.isArray(value) || value.length > 500 || value.some(item => typeof item !== 'string')) throw new Error('Invalid local watchlist');
    return mergeWatchlist([], value, true);
  } catch { localReadError = "Couldn't load your saved watchlist on this device"; return []; }
}
export function selectWatchScope() {
  const scope = accountSession();
  if (state.scope !== scope) { const local=localWatchlist(); update({ ...initial, scope, list: scope ? [] : local, local, error: localReadError }); }
}
let refreshPending: Promise<void> | undefined;
let refreshScope: AccountSession | null = null;
export async function refreshWatchlist() {
  selectWatchScope(); const scope = state.scope;
  if (!scope) { const local=localWatchlist();update({ list: local, local, error:localReadError }); return; }
  if (refreshPending && refreshScope === scope) return refreshPending;
  const promise = (async () => {
    update({ loading: true, error: null });
    try {
      let cursor: string | undefined, revision: string | null = null;
      const rows: string[] = [], cursors = new Set<string>();
      do {
        assertAccount(scope); const page = await watchlistPage(cursor);
        assertAccount(scope);
        if (revision !== null && revision !== page.revision) throw new Error('Your watchlist changed. Refresh to see the latest');
        uint256Revision(page.revision ?? '0');
        revision = page.revision;
        rows.push(...page.data.items.map(item => normalizeAddress(item.poolAddress)));
        if (rows.length > 500) throw new Error('Server watchlist exceeds published capacity500');
        cursor = page.nextCursor ?? undefined;
        if (cursor && cursors.has(cursor)) throw new Error("Couldn't load your full watchlist");
        if (cursor) cursors.add(cursor);
      } while (cursor);
      if (state.scope === scope) update({ list: [...new Set(rows)], revision: revision ?? '0', loading: false });
    } catch (error) { if (state.scope === scope) update({ loading: false, error: error instanceof AccountApiError ? actionFailureMessage('Your saved watchlist', error) : (console.error('[watchlist] load failed', error), "Can't load your watchlist right now") }); }
  })();
  refreshPending = promise; refreshScope = scope;
  try { await promise; } finally { if (refreshPending === promise) refreshPending = undefined; }
}
export async function mutateWatch(poolAddress: string, add: boolean) {
  selectWatchScope(); const pool = normalizeAddress(poolAddress), scope = state.scope;
  if (!scope) {
    const next = add ? mergeWatchlist(localWatchlist(), [pool], true) : localWatchlist().filter(item => item !== pool);
    volatileLocal = next;
    try { localStorage.setItem(key, JSON.stringify(next)); volatileLocal = null; update({ list: next, local: next, error: null }); }
    catch { update({ list: next, local: next, error: "Couldn't save on this device. Your changes last until you leave the page" }); }
    return;
  }
  requireAccount(scope.session.wallet);
  if (state.pending.some(item => item.poolAddress === pool)) return;
  if (add && state.list.length >= 500 && !state.list.includes(pool)) { update({ error: 'Your watchlist is full (500 coins)' }); return; }
  const operation: WatchOperation = { mutationId: crypto.randomUUID(), wallet: scope.session.wallet, poolAddress: pool, add, baseRevision: state.revision, expiresAt: Date.now() + 300_000 };
  update({ pending: [...state.pending, operation], error: null });
  if (!navigator.onLine) { update({ error: "You're offline. Reconnect and try again" }); return; }
  await applyWatchOperation(operation, scope);
}
async function applyWatchOperation(operation: WatchOperation, scope: AccountSession) {
  if (activeOperations.has(operation.mutationId)) return;
  activeOperations.add(operation.mutationId);
  try {
    assertAccount(scope);
    if (operation.expiresAt <= Date.now()) throw new Error('That change timed out. Try again');
    if (state.scope !== scope || operation.wallet.toLowerCase() !== scope.session.wallet.toLowerCase()) throw new Error('Watchlist account changed');
    if (operation.add) await putWatch(operation.poolAddress, operation.mutationId); else await deleteWatch(operation.poolAddress, operation.mutationId);
    assertAccount(scope);
    update({ pending: state.pending.filter(item => item.mutationId !== operation.mutationId) });
    await refreshWatchlist();
    if (typeof BroadcastChannel !== 'undefined') { try { const channel = new BroadcastChannel('kindle-account-watchlist'); channel.postMessage({ wallet: operation.wallet }); channel.close(); } catch {} }
  } catch (error) {
    // A final rejection (4xx other than retry-worthy ones) or a route no service answers (503 retryable:false) drops the pending choice so the star reverts at once.
    const final = error instanceof AccountApiError && (isNotServedError(error) || (error.status >= 400 && error.status < 500 && ![408,409,412,429].includes(error.status)));
    if (state.scope === scope) update({ pending: final ? state.pending.filter(item => item.mutationId !== operation.mutationId) : state.pending, error: error instanceof AccountApiError ? actionFailureMessage('Saving to your watchlist', error) : (console.error('[watchlist] save failed', error), "Couldn't update your watchlist, try again") });
  }
  finally { activeOperations.delete(operation.mutationId); }
}
export async function retryWatchOperation(mutationId: string, approveReconcile: boolean) {
  if (activeOperations.has(mutationId)) return;
  const scope = requireAccount(); await refreshWatchlist(); assertAccount(scope);
  if (state.error) return;
  const operation = state.pending.find(item => item.mutationId === mutationId);
  if (!operation || operation.wallet.toLowerCase() !== scope.session.wallet.toLowerCase()) return;
  if (operation.baseRevision !== state.revision && !approveReconcile) { update({ error: 'Your watchlist changed on another device. Check it and try again' }); return; }
  await applyWatchOperation(operation, scope);
}
export function dismissWatchOperation(mutationId: string) { update({ pending: state.pending.filter(item => item.mutationId !== mutationId) }); }
export async function mergeLocalWatchlist(approved: boolean) {
  const scope = requireAccount(); if (!approved) { update({ local: [] }); return; }
  await refreshWatchlist(); assertAccount(scope);
  if (state.error) return;
  const merged = mergeWatchlist(state.list, state.local, true);
  for (const pool of merged.filter(item => !state.list.includes(item))) { await mutateWatch(pool, true); assertAccount(scope); if (state.error) return; }
  try { localStorage.removeItem(key); } catch { update({ error: 'Watchlist saved, but this device still has an old copy' }); }
  volatileLocal = null;
  update({ local: [] });
}

subscribeAccount(selectWatchScope);
