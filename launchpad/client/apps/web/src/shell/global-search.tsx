'use client';

import { useCallback, useEffect, useId, useMemo, useRef, useState, useSyncExternalStore, type KeyboardEvent } from 'react';
import { useRouter } from 'next/navigation';
import { normalizeAddress } from '@/core/app-foundation/navigation';
import { useStatsBatch, statsByPool } from '@/core/market/use-stats-batch';
import { ModalFrame } from '@/ui/primitives';
import { shortAddress } from '@/utils/format';
import { useHotCoins, useSearchResults, useRecentViewed, setSearchPersistenceConsent, type RecentItem } from './use-global-search';
import { ActionRow, CoinRow, PaletteInput, PaletteStatus, ResultGroup, SearchTrigger, ShortcutChip, UserRow, UsersGroupBody, hotToSelect, recentToSelect, resultToSelect, useShortcutLabel, type SelectArgs } from './global-search-parts';
import { createSingleMount } from './single-mount';
import { marketCapRaw } from '@/core/market/pool-stats-display';

// ── Palette open state (one dialog, many triggers) ───────────────────────────

let paletteOpen = false;
const paletteListeners = new Set<() => void>();
function emitPalette() { for (const listener of paletteListeners) listener(); }
function subscribePalette(listener: () => void) { paletteListeners.add(listener); return () => { paletteListeners.delete(listener); }; }
export function openSearchPalette() { if (!paletteOpen) { paletteOpen = true; emitPalette(); } }
export function closeSearchPalette() { if (paletteOpen) { paletteOpen = false; emitPalette(); } }
function toggleSearchPalette() { paletteOpen = !paletteOpen; emitPalette(); }
const usePaletteOwner = createSingleMount();

function coinPath(address: string): string { return '/coin/' + normalizeAddress(address); }
function profilePath(address: string): string { return '/profile/' + normalizeAddress(address); }
const ADDRESS = /^0x[0-9a-fA-F]{40}$/;

interface PaletteItem { id: string; run: () => void; disabled?: boolean }

/**
 * GlobalSearch: a trigger shaped like the search input plus the command palette.
 * Any number of triggers may be mounted; the first one owns the dialog and the
 * Cmd/Ctrl-K listener. Coins come from IF_SEARCH (pool and token), users from
 * the same operation with type=profile, whose real 412 renders as unavailable.
 */
export default function GlobalSearch({ privateMode = false, compact = false }: { privateMode?: boolean; compact?: boolean }) {
  const owner = usePaletteOwner();
  const open = useSyncExternalStore(subscribePalette, () => paletteOpen, () => false);
  const shortcut = useShortcutLabel();
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!owner) return;
    const onKey = (event: globalThis.KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        toggleSearchPalette();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [owner]);

  return (
    <>
      <SearchTrigger onOpen={openSearchPalette} shortcut={shortcut} triggerRef={triggerRef} compact={compact} />
      {owner ? <SearchPalette open={open} privateMode={privateMode} shortcut={shortcut} /> : null}
    </>
  );
}

function SearchPalette({ open, privateMode, shortcut }: { open: boolean; privateMode: boolean; shortcut: string }) {
  const router = useRouter();
  const listId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const [remember, setRemember] = useState(false);
  const [pasteState, setPasteState] = useState<'idle' | 'denied' | 'empty'>('idle');
  const [activeIndex, setActiveIndex] = useState(-1);
  useEffect(() => { setSearchPersistenceConsent(remember, privateMode); }, [remember, privateMode]);

  const { coins: hotCoins } = useHotCoins(8);
  const { coins, users, isLoading: searching, error, refetch } = useSearchResults(open ? query : '');
  const { recent, refresh, clear, push } = useRecentViewed();
  const pools = useMemo(() => coins.flatMap(row => row.pool_address ? [row.pool_address] : []).slice(0, 50), [coins]);
  const stats = useStatsBatch(pools);
  const mcapByPool = useMemo(() => statsByPool(stats.data), [stats.data]);

  useEffect(() => {
    if (open) { refresh(); setActiveIndex(-1); setPasteState('idle'); }
    else { setQuery(''); setActiveIndex(-1); }
  }, [open, refresh]);
  useEffect(() => {
    const reset = () => { setQuery(''); setActiveIndex(-1); setRemember(false); closeSearchPalette(); };
    window.addEventListener('kindle:private-state-cleared', reset);
    return () => window.removeEventListener('kindle:private-state-cleared', reset);
  }, []);

  const navigate = useCallback((destination: string) => { router.push(destination); closeSearchPalette(); }, [router]);
  const handleSelect = useCallback((args: SelectArgs) => {
    let pool = '';
    try {
      if (args.poolAddress) pool = normalizeAddress(args.poolAddress);
      if (args.tokenAddress) normalizeAddress(args.tokenAddress);
    } catch { return; }
    if (!pool) return;
    try { push({ address: pool, name: args.name, symbol: args.symbol, logo: args.logo, marketCap: args.marketCap, ts: Date.now() } as RecentItem); } catch { return; }
    navigate(coinPath(pool));
  }, [push, navigate]);

  const trimmed = query.trim();
  const showIdle = trimmed.length < 2;
  const addressQuery = ADDRESS.test(trimmed) ? trimmed.toLowerCase() : null;

  const items = useMemo<PaletteItem[]>(() => {
    const list: PaletteItem[] = [];
    if (showIdle) {
      for (const item of recent) list.push({ id: `recent:${item.address}`, run: () => handleSelect(recentToSelect(item)) });
      for (const coin of hotCoins.slice(0, 6)) list.push({ id: `hot:${coin.poolAddress}`, run: () => handleSelect(hotToSelect(coin)) });
      return list;
    }
    for (const row of coins) {
      const mcap = row.pool_address ? marketCapRaw(mcapByPool.get(row.pool_address)) : null;
      list.push({ id: `coin:${row.identity}`, disabled: !row.pool_address, run: () => handleSelect(resultToSelect(row, mcap)) });
    }
    if (addressQuery) list.push({ id: 'wallet:' + addressQuery, run: () => navigate(profilePath(addressQuery)) });
    if (users.state === 'ready') for (const user of users.items) list.push({ id: `user:${user.identity}`, run: () => navigate(profilePath(user.wallet)) });
    return list;
  }, [showIdle, recent, hotCoins, coins, users, addressQuery, mcapByPool, handleSelect, navigate]);

  const optionId = (id: string) => `${listId}-${id.replace(/[^a-zA-Z0-9_-]/g, '_')}`;
  const activeId = activeIndex >= 0 && items[activeIndex] ? optionId(items[activeIndex].id) : undefined;
  useEffect(() => { if (activeIndex >= items.length) setActiveIndex(items.length ? items.length - 1 : -1); }, [items.length, activeIndex]);
  useEffect(() => {
    if (!activeId) return;
    document.getElementById(activeId)?.scrollIntoView({ block: 'nearest' });
  }, [activeId]);

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (!items.length) return;
      setActiveIndex(index => event.key === 'ArrowDown' ? (index + 1) % items.length : index <= 0 ? items.length - 1 : index - 1);
    } else if (event.key === 'Home' && items.length) { event.preventDefault(); setActiveIndex(0); }
    else if (event.key === 'End' && items.length) { event.preventDefault(); setActiveIndex(items.length - 1); }
    else if (event.key === 'Enter') {
      const target = activeIndex >= 0 ? items[activeIndex] : items.find(item => !item.disabled);
      if (target && !target.disabled) { event.preventDefault(); target.run(); }
    }
  };

  const paste = async () => {
    try {
      const text = (await navigator.clipboard.readText()).trim();
      if (!text) { setPasteState('empty'); inputRef.current?.focus(); return; }
      setPasteState('idle');
      setQuery(text.slice(0, 80));
      setActiveIndex(-1);
      if (ADDRESS.test(text)) {
        const lower = text.toLowerCase();
        const match = coins.find(row => row.pool_address === lower);
        if (match) navigate(coinPath(lower));
      }
    } catch { setPasteState('denied'); inputRef.current?.focus(); }
  };

  const userCount = users.state === 'ready' ? users.items.length : 0;

  return (
    <ModalFrame open={open} onClose={closeSearchPalette} title="Search coins and users" hideTitle size="md" closeLabel="Close search" className="sm:max-h-[70vh]">
      <div className="flex flex-col gap-2">
        <PaletteInput value={query} onChange={value => { setQuery(value); setActiveIndex(-1); }} inputRef={inputRef} onKeyDown={onKeyDown}
          listId={listId} activeId={activeId} onPaste={() => { void paste(); }} pasteState={pasteState} />
        <div role="listbox" id={listId} aria-label="Search results" aria-busy={searching || undefined} className="flex flex-col">
          {showIdle ? (
            <>
              <ResultGroup label="Recently viewed" trailing={recent.length ? <button type="button" onClick={clear} className="text-xs font-bold text-text-muted hover:text-text">Clear</button> : null}>
                {recent.length ? recent.map(item => (
                  <CoinRow key={item.address} id={optionId(`recent:${item.address}`)} active={activeId === optionId(`recent:${item.address}`)} name={item.name} symbol={item.symbol} logo={item.logo}
                    mcapRaw={item.marketCap && item.marketCap !== '0' ? item.marketCap : null} address={item.address} kind="pool" onSelect={() => handleSelect(recentToSelect(item))} />
                )) : <p className="px-3 pb-2 text-xs text-text-faint">{remember ? 'Coins you open show up here' : 'Turn on saving below to keep your recent coins'}</p>}
              </ResultGroup>
              {hotCoins.length ? (
                <ResultGroup label="Trending coins">
                  {hotCoins.slice(0, 6).map(coin => (
                    <CoinRow key={coin.poolAddress} id={optionId(`hot:${coin.poolAddress}`)} active={activeId === optionId(`hot:${coin.poolAddress}`)} name={coin.name || ''} symbol={coin.symbol || ''} logo={coin.logo || null}
                      mcapRaw={coin.marketCap && coin.marketCap !== '0' ? coin.marketCap : null} address={coin.poolAddress} kind="pool" onSelect={() => handleSelect(hotToSelect(coin))} />
                  ))}
                </ResultGroup>
              ) : null}
            </>
          ) : (
            <>
              <PaletteStatus loading={searching} error={error} coinCount={coins.length} userCount={userCount} onRetry={refetch} query={query} />
              <ResultGroup label="Coins">
                {coins.length ? coins.slice(0, 20).map(row => {
                  const id = optionId(`coin:${row.identity}`);
                  const mcap = row.pool_address ? marketCapRaw(mcapByPool.get(row.pool_address)) : null;
                  const address = row.kind === 'pool' ? row.pool_address : row.token_address;
                  return <CoinRow key={row.identity} id={id} active={activeId === id} name={row.name} symbol={row.symbol} logo={row.images.logo} mcapRaw={mcap} address={address} kind={row.kind} disabled={!row.pool_address}
                    onSelect={() => handleSelect(resultToSelect(row, mcap))} />;
                }) : !searching && !error ? <p className="px-3 pb-2 text-xs text-text-faint">{query.trim() ? `No coins match "${query.trim()}"` : 'No coins found'}</p> : null}
              </ResultGroup>
              {addressQuery ? (
                <ResultGroup label="Wallet">
                  <ActionRow id={optionId('wallet:' + addressQuery)} active={activeId === optionId('wallet:' + addressQuery)} icon="wallet" title={`Open wallet ${shortAddress(addressQuery, 6, 4)}`} detail="View profile" onSelect={() => navigate(profilePath(addressQuery))} />
                </ResultGroup>
              ) : null}
              <ResultGroup label="Users">
                {users.state === 'ready' ? users.items.map(user => {
                  const id = optionId(`user:${user.identity}`);
                  return <UserRow key={user.identity} id={id} active={activeId === id} label={user.label} wallet={user.wallet} onSelect={() => navigate(profilePath(user.wallet))} />;
                }) : null}
                {searching ? null : <UsersGroupBody users={users} onRetry={refetch} />}
              </ResultGroup>
            </>
          )}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2 pt-1 text-xs text-text-faint">
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={remember && !privateMode} disabled={privateMode} onChange={event => setRemember(event.target.checked)} className="accent-[var(--sl-accent)]" />
            Remember recent views on this device
          </label>
          <span className="hidden items-center gap-1.5 sm:inline-flex">Toggle with <ShortcutChip label={shortcut} /></span>
        </div>
      </div>
    </ModalFrame>
  );
}
