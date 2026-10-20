"use client";

import { useCallback, useEffect, useState } from "react";

/* ══════════════════════════════════════════════════════════════════════
   Favorites (⭐) store for perpetual markets.

   localStorage-backed Set<string> of uppercased symbols. Shared across
   every React consumer via a module-level state + pub/sub, so toggling
   in one place updates every instance in the same tick.

   SSR-safe: returns an empty set on the server; hydrates from localStorage
   on first client render.
   ══════════════════════════════════════════════════════════════════════ */

const STORAGE_KEY = "paxeer.favorites.v1";

type Subscriber = (favorites: ReadonlySet<string>) => void;

let _loaded = false;
let _favorites: Set<string> = new Set();
const _subscribers = new Set<Subscriber>();

function loadFromStorage(): Set<string> {
  if (typeof window === "undefined") return new Set();
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return new Set();
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return new Set();
    return new Set(
      parsed.filter((v): v is string => typeof v === "string").map((v) => v.toUpperCase()),
    );
  } catch {
    return new Set();
  }
}

function persistToStorage(set: ReadonlySet<string>): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(Array.from(set)));
  } catch {
    // Quota / privacy mode — silently ignore, in-memory state still works.
  }
}

function ensureLoaded(): void {
  if (_loaded) return;
  _loaded = true;
  _favorites = loadFromStorage();
}

function emit(): void {
  _subscribers.forEach((cb) => {
    try { cb(_favorites); } catch { /* ignore */ }
  });
}

export interface FavoriteMarketsApi {
  favorites: ReadonlySet<string>;
  isFavorite: (symbol: string) => boolean;
  toggle: (symbol: string) => void;
  add: (symbol: string) => void;
  remove: (symbol: string) => void;
  clear: () => void;
  count: number;
}

/**
 * Hook wrapping the favorites set. All methods are stable across renders.
 */
export function useFavoriteMarkets(): FavoriteMarketsApi {
  ensureLoaded();
  const [favorites, setFavorites] = useState<ReadonlySet<string>>(_favorites);

  useEffect(() => {
    const cb: Subscriber = (next) => setFavorites(next);
    _subscribers.add(cb);
    // Sync any change that happened between render and subscribe.
    if (favorites !== _favorites) setFavorites(_favorites);

    // Cross-tab sync via the `storage` event.
    const onStorage = (ev: StorageEvent) => {
      if (ev.key && ev.key !== STORAGE_KEY) return;
      _favorites = loadFromStorage();
      emit();
    };
    if (typeof window !== "undefined") {
      window.addEventListener("storage", onStorage);
    }

    return () => {
      _subscribers.delete(cb);
      if (typeof window !== "undefined") {
        window.removeEventListener("storage", onStorage);
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const isFavorite = useCallback((symbol: string) => {
    return _favorites.has(symbol.toUpperCase());
  }, []);

  const add = useCallback((symbol: string) => {
    const key = symbol.toUpperCase();
    if (_favorites.has(key)) return;
    _favorites = new Set(_favorites);
    _favorites.add(key);
    persistToStorage(_favorites);
    emit();
  }, []);

  const remove = useCallback((symbol: string) => {
    const key = symbol.toUpperCase();
    if (!_favorites.has(key)) return;
    _favorites = new Set(_favorites);
    _favorites.delete(key);
    persistToStorage(_favorites);
    emit();
  }, []);

  const toggle = useCallback((symbol: string) => {
    const key = symbol.toUpperCase();
    _favorites = new Set(_favorites);
    if (_favorites.has(key)) _favorites.delete(key);
    else _favorites.add(key);
    persistToStorage(_favorites);
    emit();
  }, []);

  const clear = useCallback(() => {
    if (_favorites.size === 0) return;
    _favorites = new Set();
    persistToStorage(_favorites);
    emit();
  }, []);

  return {
    favorites,
    isFavorite,
    toggle,
    add,
    remove,
    clear,
    count: favorites.size,
  };
}
