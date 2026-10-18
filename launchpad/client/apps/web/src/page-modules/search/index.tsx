'use client';
import { Suspense, useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { normalizeAddress } from '@/core/app-foundation/navigation';
import { statsByPool, useStatsBatch } from '@/core/market/use-stats-batch';
import { Skeleton } from '@/ui/primitives';
import { shortAddress } from '@/utils/format';
import { ActionRow, CoinRow, PaletteInput, PaletteStatus, ResultGroup, UserRow, UsersGroupBody, hotToSelect, resultToSelect, type SelectArgs } from '@/shell/global-search-parts';
import { addToRecent, useHotCoins, useSearchResults } from '@/shell/use-global-search';
import { marketCapRaw } from '@/core/market/pool-stats-display';

const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const MAX_QUERY = 80;

interface Item { id: string; run: () => void; disabled?: boolean }

/** Keeps `?q=` in step with the input without a navigation, so the URL stays shareable. */
function writeQueryToUrl(query: string) {
  try {
    const url = new URL(window.location.href);
    if (query.trim()) url.searchParams.set('q', query); else url.searchParams.delete('q');
    window.history.replaceState(window.history.state, '', url.pathname + url.search);
  } catch { /* URL sync is a convenience; the results do not depend on it. */ }
}

/**
 * /search?q=: the same coin and user search as the Ctrl+K palette (useSearchResults, the
 * palette rows and status line), laid out as a page so search links can be shared and opened.
 */
export function SearchView({ initialQuery }: { initialQuery: string }) {
  const router = useRouter();
  const listId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState(() => initialQuery.slice(0, MAX_QUERY));
  const [activeIndex, setActiveIndex] = useState(-1);
  const [pasteState, setPasteState] = useState<'idle' | 'denied' | 'empty'>('idle');

  useEffect(() => { setQuery(initialQuery.slice(0, MAX_QUERY)); setActiveIndex(-1); }, [initialQuery]);

  const { coins: hotCoins } = useHotCoins(8);
  const { coins, users, isLoading: searching, error, refetch } = useSearchResults(query);
  const pools = useMemo(() => coins.flatMap(row => row.pool_address ? [row.pool_address] : []).slice(0, 50), [coins]);
  const stats = useStatsBatch(pools);
  const mcapByPool = useMemo(() => statsByPool(stats.data), [stats.data]);

  const trimmed = query.trim();
  const showIdle = trimmed.length < 2;
  const addressQuery = ADDRESS.test(trimmed) ? trimmed.toLowerCase() : null;

  const change = useCallback((value: string) => {
    const next = value.slice(0, MAX_QUERY);
    setQuery(next); setActiveIndex(-1); setPasteState('idle');
    writeQueryToUrl(next);
  }, []);
  const openCoin = useCallback((args: SelectArgs) => {
    let pool: string;
    try { pool = normalizeAddress(args.poolAddress ?? ''); } catch { return; }
    try { addToRecent({ address: pool, name: args.name, symbol: args.symbol, logo: args.logo, marketCap: args.marketCap, ts: Date.now() }); } catch { /* recent views are optional */ }
    router.push('/coin/' + pool);
  }, [router]);
  const openProfile = useCallback((wallet: string) => {
    try { router.push('/profile/' + normalizeAddress(wallet)); } catch { /* not an address */ }
  }, [router]);

  const items = useMemo<Item[]>(() => {
    if (showIdle) return hotCoins.slice(0, 6).map(coin => ({ id: `hot:${coin.poolAddress}`, run: () => openCoin(hotToSelect(coin)) }));
    const list: Item[] = coins.slice(0, 20).map(row => {
      const mcap = row.pool_address ? marketCapRaw(mcapByPool.get(row.pool_address)) : null;
      return { id: `coin:${row.identity}`, disabled: !row.pool_address, run: () => openCoin(resultToSelect(row, mcap)) };
    });
    if (addressQuery) list.push({ id: 'wallet:' + addressQuery, run: () => openProfile(addressQuery) });
    if (users.state === 'ready') for (const user of users.items) list.push({ id: `user:${user.identity}`, run: () => openProfile(user.wallet) });
    return list;
  }, [showIdle, hotCoins, coins, mcapByPool, addressQuery, users, openCoin, openProfile]);

  const optionId = (id: string) => `${listId}-${id.replace(/[^a-zA-Z0-9_-]/g, '_')}`;
  const activeId = activeIndex >= 0 && items[activeIndex] ? optionId(items[activeIndex].id) : undefined;
  useEffect(() => { if (activeIndex >= items.length) setActiveIndex(items.length ? items.length - 1 : -1); }, [items.length, activeIndex]);

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (!items.length) return;
      setActiveIndex(index => event.key === 'ArrowDown' ? (index + 1) % items.length : index <= 0 ? items.length - 1 : index - 1);
    } else if (event.key === 'Enter') {
      const target = activeIndex >= 0 ? items[activeIndex] : items.find(item => !item.disabled);
      if (target && !target.disabled) { event.preventDefault(); target.run(); }
    } else if (event.key === 'Escape' && query) { event.preventDefault(); change(''); }
  };

  const paste = async () => {
    try {
      const text = (await navigator.clipboard.readText()).trim();
      if (!text) { setPasteState('empty'); inputRef.current?.focus(); return; }
      change(text);
    } catch { setPasteState('denied'); inputRef.current?.focus(); }
  };

  const userCount = users.state === 'ready' ? users.items.length : 0;

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 py-4 md:px-6">
      <h1 className="text-xl font-extrabold text-text">Search</h1>
      <div className="flex flex-col gap-2 rounded-md bg-surface-1 p-2">
        <PaletteInput value={query} onChange={change} inputRef={inputRef} onKeyDown={onKeyDown} listId={listId} activeId={activeId}
          onPaste={() => { void paste(); }} pasteState={pasteState} />
        <div role="listbox" id={listId} aria-label="Search results" aria-busy={searching || undefined} className="flex flex-col">
          {showIdle ? (
            <>
              <p className="px-3 py-1.5 text-xs text-text-faint">Type at least two characters to search coins and users</p>
              {hotCoins.length ? (
                <ResultGroup label="Trending coins">
                  {hotCoins.slice(0, 6).map(coin => {
                    const id = optionId(`hot:${coin.poolAddress}`);
                    return <CoinRow key={coin.poolAddress} id={id} active={activeId === id} name={coin.name || ''} symbol={coin.symbol || ''} logo={coin.logo || null}
                      mcapRaw={coin.marketCap && coin.marketCap !== '0' ? coin.marketCap : null} address={coin.poolAddress} kind="pool" onSelect={() => openCoin(hotToSelect(coin))} />;
                  })}
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
                    onSelect={() => openCoin(resultToSelect(row, mcap))} />;
                }) : !searching && !error ? <p className="px-3 pb-2 text-xs text-text-faint">{`No coins match "${trimmed}"`}</p> : null}
              </ResultGroup>
              {addressQuery ? (
                <ResultGroup label="Wallet">
                  <ActionRow id={optionId('wallet:' + addressQuery)} active={activeId === optionId('wallet:' + addressQuery)} icon="wallet" title={`Open wallet ${shortAddress(addressQuery, 6, 4)}`} detail="View profile" onSelect={() => openProfile(addressQuery)} />
                </ResultGroup>
              ) : null}
              <ResultGroup label="Users">
                {users.state === 'ready' ? users.items.map(user => {
                  const id = optionId(`user:${user.identity}`);
                  return <UserRow key={user.identity} id={id} active={activeId === id} label={user.label} wallet={user.wallet} onSelect={() => openProfile(user.wallet)} />;
                }) : null}
                {searching ? null : <UsersGroupBody users={users} onRetry={refetch} />}
              </ResultGroup>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function SearchFromParams() {
  const params = useSearchParams();
  return <SearchView initialQuery={params.get('q') ?? ''} />;
}

export default function SearchModule() {
  return (
    <Suspense fallback={<div className="mx-auto w-full max-w-3xl px-4 py-4 md:px-6"><Skeleton variant="block" height={56} label="search" /></div>}>
      <SearchFromParams />
    </Suspense>
  );
}
