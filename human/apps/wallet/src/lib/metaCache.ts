/**
 * Lightweight localStorage cache for fully-static metadata
 * (token names, symbols, logos, decimals).
 *
 * Keys are prefixed with `pax:meta:` to avoid collision.
 * Each entry stores `{ ts: epoch_ms, data: T }`.
 * Reads that exceed `ttlMs` are treated as misses and the key is pruned.
 *
 * Designed to be used as a backing store for TanStack Query `initialData`
 * so the UI renders immediately from cache while TanStack decides whether
 * to revalidate in the background.
 */

import {
  metadataCacheRepository,
  type JsonValue,
} from '@/platform/storage/repositories';

export interface CacheEntry<T> {
  ts: number;
  data: T;
}

/** Read a cached value. Returns null on miss, expiry, or parse error. */
export function cacheGet<T>(key: string, ttlMs: number): T | null {
  if (typeof window === 'undefined') return null;
  const entries = metadataCacheRepository.read();
  const entry = entries[key];
  if (!entry) return null;
  if (Date.now() - entry.ts > ttlMs) {
    metadataCacheRepository.update((current) =>
      Object.fromEntries(
        Object.entries(current).filter(([candidate]) => candidate !== key),
      ),
    );
    return null;
  }
  return entry.data as T;
}

/** Return the epoch timestamp when the cached entry was written, or 0 if absent/expired. */
export function cacheAge(key: string, ttlMs: number): number {
  if (typeof window === 'undefined') return 0;
  const entry = metadataCacheRepository.read()[key];
  if (!entry || Date.now() - entry.ts > ttlMs) return 0;
  return entry.ts;
}

/** Write a value to the cache. Silently drops writes when storage is full. */
export function cacheSet<T>(key: string, data: T): void {
  if (typeof window === 'undefined') return;
  metadataCacheRepository.update((current) => ({
    ...current,
    [key]: { ts: Date.now(), data: data as JsonValue },
  }));
}

/** Remove a single cache entry. */
export function cacheDel(key: string): void {
  if (typeof window === 'undefined') return;
  metadataCacheRepository.update((current) =>
    Object.fromEntries(
      Object.entries(current).filter(([candidate]) => candidate !== key),
    ),
  );
}
