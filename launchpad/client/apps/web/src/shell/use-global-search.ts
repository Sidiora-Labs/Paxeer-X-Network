'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRanking } from '@/hooks/market/use-ranking';
import { useTokenStatsBatch } from '@/hooks/market/use-token-stats';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import { normalizeAddress } from '@/core/app-foundation/navigation';
import { normalizedSearch, searchAll, type SearchEntity, type UserSearch } from '@/core/app-foundation/search-client';
import { marketCapRaw } from '@/core/market/pool-stats-display';


const RECENT_KEY = 'sidiora_recent_viewed';
const MAX_RECENT = 6;

export type SearchResult = SearchEntity;

export interface HotCoin {
  poolAddress: string;
  tokenAddress?: string;
  name?: string;
  symbol?: string;
  logo?: string | null;
  marketCap?: string;
}

export interface RecentItem {
  address: string;
  name: string;
  symbol: string;
  logo: string | null;
  marketCap: string;
  ts: number;
}

// ── Recent storage helpers ────────────────────────────────────────────────────

let recentMemory: RecentItem[] = [];
let persistenceConsent = false;
export function setSearchPersistenceConsent(consent: boolean, privateMode = false) {
  persistenceConsent = consent && !privateMode && !(typeof navigator !== 'undefined' && (navigator as Navigator & { globalPrivacyControl?: boolean }).globalPrivacyControl);
  if (!persistenceConsent) { recentMemory = []; try { localStorage.removeItem(RECENT_KEY); } catch {} }
}
function checkedRecent(value: unknown): RecentItem[] {
  if (!Array.isArray(value) || value.length > MAX_RECENT) throw new TypeError('Invalid recent searches');
  const identities = new Set<string>();
  return value.map(item => {
    if (!item || typeof item !== 'object' || Array.isArray(item) || Object.keys(item).some(key => !['address', 'name', 'symbol', 'logo', 'marketCap', 'ts'].includes(key))) throw new TypeError('Invalid recent search');
    const row = item as RecentItem;
    const address = normalizeAddress(row.address);
    if (identities.has(address) || !['name', 'symbol', 'marketCap'].every(key => typeof (row as unknown as Record<string, unknown>)[key] === 'string' && ((row as unknown as Record<string, string>)[key]).length <= 256) || row.logo !== null && (typeof row.logo !== 'string' || row.logo.length > 2048) || !Number.isSafeInteger(row.ts) || row.ts < 0 || row.ts > Date.now()) throw new TypeError('Invalid recent search');
    identities.add(address);
    return { address, name: row.name, symbol: row.symbol, logo: null, marketCap: row.marketCap, ts: row.ts };
  });
}
export function loadRecent(): RecentItem[] {
  if (!persistenceConsent) return recentMemory.map(item => ({ ...item }));
  try {
    const text = localStorage.getItem(RECENT_KEY);
    if (text === null) return recentMemory.map(item => ({ ...item }));
    if (new TextEncoder().encode(text).length > 8192) throw new TypeError('Recent storage too large');
    recentMemory = checkedRecent(JSON.parse(text));
    return recentMemory.map(item => ({ ...item }));
  } catch { clearRecentStorage(); return []; }
}
export function saveRecent(items: RecentItem[]) {
  recentMemory = checkedRecent(items.slice(0, MAX_RECENT));
  if (persistenceConsent) { try { localStorage.setItem(RECENT_KEY, JSON.stringify(recentMemory)); } catch {} }
}
export function addToRecent(item: RecentItem) {
  const address = normalizeAddress(item.address);
  const existing = loadRecent().filter(row => row.address !== address);
  saveRecent([{ ...item, address, ts: Date.now() }, ...existing]);
}
export function clearRecentStorage() {
  recentMemory = [];
  try { localStorage.removeItem(RECENT_KEY); } catch {}
}

// ── Hot coins (shares trending cache with BentoHero/TrendingStrip) ───────────

export function useHotCoins(limit = 8): { coins: HotCoin[]; loading: boolean } {
  const trending = useRanking('trending', limit, 0);

  const items = useMemo(() => trending.data?.items ?? [], [trending.data]);
  const pools = useMemo(
    () => items.map((i) => i.poolAddress.toLowerCase()),
    [items],
  );

  const statsBatch = useTokenStatsBatch(pools);

  const tokenAddrs = useMemo(() => {
    if (!statsBatch.data) return [];
    return [...new Set(
      Object.values(statsBatch.data)
        .map((s) => s?.tokenAddress?.toLowerCase())
        .filter((a): a is string => !!a),
    )];
  }, [statsBatch.data]);

  const { data: metaByToken = {} } = useTokenMetadataBatch(tokenAddrs);

  const coins = useMemo<HotCoin[]>(() => {
    return items.map((item) => {
      const stat = statsBatch.data?.[item.poolAddress];
      const tokenAddr = (stat?.tokenAddress || '').toLowerCase();
      const meta = metaByToken[tokenAddr];
      return {
        poolAddress: item.poolAddress,
        tokenAddress: tokenAddr,
        name: meta?.name || '',
        symbol: meta?.symbol || '',
        logo: meta?.images?.logo || null,
        // Raw USDL (6 decimals) for MoneyValue: the verified total-supply valuation of the v2 stats row.
        marketCap: marketCapRaw(stat) ?? '0',
      };
    });
  }, [items, statsBatch.data, metaByToken]);

  return { coins, loading: trending.isLoading };
}

// ── Search results (debounced; coins and users resolved together) ────────────

const noUsers: UserSearch = { state: 'ready', items: [] };
type SearchState = { query: string; coins: SearchEntity[]; users: UserSearch; isLoading: boolean; error: Error | null };
export function useSearchResults(query: string) {
  const [state, setState] = useState<SearchState>({ query: '', coins: [], users: noUsers, isLoading: false, error: null });
  const [retry, setRetry] = useState(0);
  const generation = useRef(0);
  useEffect(() => {
    const current = ++generation.current;
    const controller = new AbortController();
    let normalized: string;
    try { normalized = normalizedSearch(query); }
    catch (error) { setState({ query, coins: [], users: noUsers, isLoading: false, error: error instanceof Error ? error : new Error('Invalid query') }); return () => controller.abort(); }
    if (normalized.length < 2) { setState({ query, coins: [], users: noUsers, isLoading: false, error: null }); return () => controller.abort(); }
    setState({ query, coins: [], users: noUsers, isLoading: true, error: null });
    const timer = setTimeout(() => {
      void searchAll(normalized, controller.signal).then(outcome => {
        if (!controller.signal.aborted && generation.current === current) setState({ query, coins: outcome.coins, users: outcome.users, isLoading: false, error: null });
      }).catch(error => {
        if (!controller.signal.aborted && generation.current === current) setState({ query, coins: [], users: noUsers, isLoading: false, error: error instanceof Error ? error : new Error('Search unavailable') });
      });
    }, 150);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [query, retry]);
  const live = state.query === query;
  return { coins: live ? state.coins : [], users: live ? state.users : noUsers, isLoading: live ? state.isLoading : query.trim().length >= 2,
    error: live ? state.error : null, refetch: () => setRetry(n => n + 1) };
}

// ── Recent viewed state ───────────────────────────────────────────────────────

export function useRecentViewed() {
  const [recent, setRecent] = useState<RecentItem[]>([]);

  useEffect(() => {
    const reset = () => { clearRecentStorage(); setRecent([]); };
    const storage = (event: StorageEvent) => { if (event.key === RECENT_KEY || event.key === null) setRecent(loadRecent()); };
    setRecent(loadRecent());
    window.addEventListener('kindle:private-state-cleared', reset);
    window.addEventListener('storage', storage);
    return () => { window.removeEventListener('kindle:private-state-cleared', reset); window.removeEventListener('storage', storage); };
  }, []);

  const refresh = useCallback(() => setRecent(loadRecent()), []);
  const clear = useCallback(() => { clearRecentStorage(); setRecent([]); }, []);
  const push = useCallback((item: RecentItem) => {
    addToRecent(item);
    setRecent(loadRecent());
  }, []);

  return { recent, refresh, clear, push };
}
