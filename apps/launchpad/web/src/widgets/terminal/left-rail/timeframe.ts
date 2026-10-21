import type { ExploreRow } from '@/widgets/explore/use-explore-rows';
import type { ChangeWindow } from '@/core/market/pool-stats-display';

export type RailTimeframe = '1m' | '5m' | '30m' | '1h';

export const railTimeframes: { value: RailTimeframe; label: string }[] = [
  { value: '1m', label: '1M' },
  { value: '5m', label: '5M' },
  { value: '30m', label: '30M' },
  { value: '1h', label: '1H' },
];

/** v2 window per rail timeframe: 5m and 1h are PoolStats.priceChangeBps5m / priceChangeBps1h; 1m and 30m have no v2 source. */
const servedWindow: Record<RailTimeframe, Extract<ChangeWindow, '5m' | '1h'> | null> = {
  '1m': null,
  '5m': '5m',
  '30m': null,
  '1h': '1h',
};

/** Windows the v2 pool statistics define: 5m and 1h. */
export function timeframeServed(timeframe: RailTimeframe): boolean {
  return servedWindow[timeframe] !== null;
}

const windowNames: Record<RailTimeframe, string> = { '1m': '1-minute', '5m': '5-minute', '30m': '30-minute', '1h': '1-hour' };
/** Plain reason a window with no v2 source shows instead of a value. */
export function unservedWindowReason(timeframe: RailTimeframe): string {
  return `A ${windowNames[timeframe]} change is not available. Choose 5m or 1h.`;
}
export const missingRowReason = 'Price changes are not available for this coin yet.';

export interface WindowChange {
  value: number | null;
  reason: string | null;
}

/**
 * Percent change for the selected window from the row's v2 statistics: a
 * value only on a complete baseline, otherwise the baseline's own reason
 * (for example not enough trading history). Never interpolated between windows.
 */
export function windowChange(row: Pick<ExploreRow, 'changes'> | null, timeframe: RailTimeframe): WindowChange {
  const window = servedWindow[timeframe];
  if (window === null) return { value: null, reason: unservedWindowReason(timeframe) };
  const change = row?.changes[window];
  if (!change) return { value: null, reason: missingRowReason };
  return change.value !== null && Number.isFinite(change.value) ? { value: change.value, reason: null } : { value: null, reason: change.reason ?? missingRowReason };
}
