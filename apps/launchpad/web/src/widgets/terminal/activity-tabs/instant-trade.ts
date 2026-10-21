'use client';
import { useCallback, useEffect, useState } from 'react';

/** localStorage key holding "true" or "false". */
export const instantTradeStorageKey = 'sidiora-terminal-instant-trade';
/**
 * Window event fired whenever the activity-tabs toggle changes (and once on
 * mount with the restored value). `detail` is `{ enabled: boolean }`; the
 * right-rail quick buttons listen and submit without a confirmation step
 * while `enabled` is true.
 */
export const instantTradeEvent = 'sidiora:terminal-instant-trade';
export interface InstantTradeDetail { enabled: boolean }

/** The persisted flag: "true" is on ("1" from earlier builds also counts); anything else is off. */
export function parseInstantFlag(raw: string | null): boolean {
  return raw === 'true' || raw === '1';
}

export function readInstantTrade(): boolean {
  try { return parseInstantFlag(window.localStorage.getItem(instantTradeStorageKey)); } catch { return false; }
}

function announce(enabled: boolean) {
  window.dispatchEvent(new CustomEvent<InstantTradeDetail>(instantTradeEvent, { detail: { enabled } }));
}

export function useInstantTrade(): [boolean, (enabled: boolean) => void] {
  const [enabled, setEnabled] = useState(false);
  useEffect(() => {
    const restored = readInstantTrade();
    setEnabled(restored);
    announce(restored);
  }, []);
  const update = useCallback((next: boolean) => {
    setEnabled(next);
    try {
      window.localStorage.setItem(instantTradeStorageKey, next ? 'true' : 'false');
    } catch { /* storage blocked: the toggle still applies to this page view */ }
    announce(next);
  }, []);
  return [enabled, update];
}
