'use client';
import { useEffect, useRef } from 'react';
import type { Loadable } from '@/core/market/unsupported';

/**
 * Tab row under a dashboard column header. Widgets size their controls (short labels, compact
 * segments, value-only pills) to fit one line at the 288 px minimum column width; the row still
 * wraps instead of scrolling as a safety net, so a trailing control is never clipped.
 */
export const columnToolbarClass = 'flex min-h-10 shrink-0 flex-wrap items-center gap-x-2 gap-y-1 px-2 py-1.5';

/**
 * Placement of a degraded-state chip (UnavailableState / ErrorState `variant="chip"`) inside a
 * column body: a compact pill under the toolbar, so the column frame, header and tab row keep
 * their layout instead of being replaced by a centred panel. The pill takes surface-2 on the
 * surface-1 column (background contrast only).
 */
export const columnNoticeClass = 'mx-2 my-1.5 self-start';

/** The one state a ranking panel renders: skeleton, data (including empty), unsupported or error. */
export type PanelState<T> = Loadable<T>;

/**
 * Pure derivation of what a ranking panel shows from the explore hook.
 *
 * - `loading` (no data yet, request in flight) wins over the ranking's own
 *   state, so a skeleton is never drawn next to an error.
 * - Data, once present, stays rendered through background refetches: the
 *   explore hook keeps its snapshot, so `loading` is false and ranking is ready.
 * - `previous` is the last settled state for the same key. React Query resets a
 *   query that has no data to `pending` on every refetch (poll, focus, retry),
 *   which would swap a settled error for a skeleton and back again each cycle.
 *   While such a refetch is in flight the settled error / unsupported state is
 *   kept until the refetch settles.
 */
export function derivePanelState<T>(loading: boolean, ranking: Loadable<T>, previous: Loadable<T> | null): PanelState<T> {
  const live: Loadable<T> = loading ? { state: 'loading' } : ranking;
  if (live.state === 'loading' && previous && (previous.state === 'error' || previous.state === 'unsupported')) return previous;
  return live;
}

/**
 * Hook form of derivePanelState: remembers the last settled state per `key`
 * (the ranking category), so switching category starts from a skeleton.
 */
export function usePanelState<T>(loading: boolean, ranking: Loadable<T>, key: string): PanelState<T> {
  const settled = useRef<{ key: string; value: Loadable<T> } | null>(null);
  const previous = settled.current && settled.current.key === key ? settled.current.value : null;
  const shown = derivePanelState(loading, ranking, previous);
  useEffect(() => {
    if (shown.state !== 'loading') settled.current = { key, value: shown };
    else if (settled.current?.key !== key) settled.current = null;
  });
  return shown;
}
