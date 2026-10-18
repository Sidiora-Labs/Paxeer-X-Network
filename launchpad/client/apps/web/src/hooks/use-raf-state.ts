'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { RafScheduler } from '@/core/realtime/raf-batch';

/**
 * Like `useState`, but high-frequency writes are coalesced to at most ONE React
 * commit per animation frame (T02.3). Use for WebSocket-driven values (price,
 * PNL, status) so a burst of ticks never triggers a setState-per-tick render
 * storm — the latest value wins each frame.
 *
 * Reads are always the last committed value; writes accept either a value or a
 * functional updater (applied against the latest scheduled value).
 */
export function useRafState<T>(initial: T): [T, (next: T | ((prev: T) => T)) => void] {
  const [state, setState] = useState<T>(initial);
  const latest = useRef<T>(initial);
  const schedulerRef = useRef<RafScheduler | null>(null);
  if (schedulerRef.current === null) schedulerRef.current = new RafScheduler();

  useEffect(() => {
    const scheduler = schedulerRef.current;
    return () => scheduler?.cancel();
  }, []);

  const set = useCallback((next: T | ((prev: T) => T)) => {
    latest.current =
      typeof next === 'function' ? (next as (prev: T) => T)(latest.current) : next;
    schedulerRef.current?.schedule(() => setState(latest.current));
  }, []);

  return [state, set];
}
