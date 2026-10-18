'use client';
import { useSyncExternalStore } from 'react';

export const recentPoolsStorageKey = 'sidiora-terminal-recent';
export const recentPoolsLimit = 20;

const poolPattern = /^0x[0-9a-fA-F]{40}$/;
const empty: string[] = [];
const listeners = new Set<() => void>();
let cachedRaw: string | null = null;
let cachedList: string[] = empty;

/** Parse the stored list: lowercase 0x40hex pool addresses, deduplicated, newest first, at most 20. */
export function parseRecentPools(raw: string | null): string[] {
  if (raw === null) return [];
  let value: unknown;
  try { value = JSON.parse(raw); } catch { return []; }
  if (!Array.isArray(value)) return [];
  const out: string[] = [];
  for (const entry of value) {
    if (typeof entry !== 'string' || !poolPattern.test(entry)) continue;
    const pool = entry.toLowerCase();
    if (!out.includes(pool)) out.push(pool);
    if (out.length === recentPoolsLimit) break;
  }
  return out;
}

function storage(): Storage | null {
  try { return typeof window === 'undefined' ? null : window.localStorage; } catch { return null; }
}

function readRaw(): string | null {
  try { return storage()?.getItem(recentPoolsStorageKey) ?? null; } catch { return null; }
}

function snapshot(): string[] {
  const raw = readRaw();
  if (raw !== cachedRaw) {
    cachedRaw = raw;
    cachedList = parseRecentPools(raw);
  }
  return cachedList;
}

function write(next: readonly string[]): void {
  const raw = JSON.stringify(next);
  try { storage()?.setItem(recentPoolsStorageKey, raw); } catch { return; }
  cachedRaw = raw;
  cachedList = [...next];
  for (const listener of listeners) listener();
}

/** Prepend a pool (moving it to the front when already present); invalid addresses are ignored. */
export function recordRecentPool(address: string): void {
  if (!poolPattern.test(address)) return;
  const pool = address.toLowerCase();
  const current = snapshot();
  if (current[0] === pool) return;
  write([pool, ...current.filter(entry => entry !== pool)].slice(0, recentPoolsLimit));
}

export function removeRecentPool(address: string): void {
  const pool = address.toLowerCase();
  const current = snapshot();
  if (!current.includes(pool)) return;
  write(current.filter(entry => entry !== pool));
}

export function clearRecentPools(): void {
  if (snapshot().length === 0) return;
  write([]);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  function onStorage(event: StorageEvent) {
    if (event.key === null || event.key === recentPoolsStorageKey) listener();
  }
  if (typeof window !== 'undefined') window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    if (typeof window !== 'undefined') window.removeEventListener('storage', onStorage);
  };
}

function serverSnapshot(): string[] {
  return empty;
}

/** Recently viewed terminal pools, newest first, shared across tabs through the storage event. */
export function useRecentPools(): string[] {
  return useSyncExternalStore(subscribe, snapshot, serverSnapshot);
}
