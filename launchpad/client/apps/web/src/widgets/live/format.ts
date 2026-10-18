import { formatCompactUsd, formatCount } from '@/utils/format';
import { usdlNumber } from '@/page-modules/token/market-presentation';
import type { StreamViewerQuality } from '@/core/clients/livestream-api';

const viewersUnavailableLabel = 'No viewer count yet';
export const marketCapUnavailableLabel = 'Market cap not available yet';

/** "0 viewers", "1 viewer", "12.3K viewers"; the unavailable label when the service has no estimate. */
export function formatViewers(count: number | null | undefined, quality?: StreamViewerQuality): string {
  if (count === null || count === undefined || quality === 'unavailable' || !Number.isFinite(count)) return viewersUnavailableLabel;
  const n = Math.max(0, Math.trunc(count));
  return `${formatCount(n)} ${n === 1 ? 'viewer' : 'viewers'}`;
}

/** Compact USD text ("$9.23K") for a raw USDL market cap, or null when there is no value to show. */
export function formatMarketCap(raw: string | null | undefined): string | null {
  if (raw === null || raw === undefined || raw === '') return null;
  const value = usdlNumber(raw);
  if (value === null) return null;
  return formatCompactUsd(value);
}

/** Profile display name when set, otherwise the first 8 hex characters of the wallet. */
export function shortId(wallet: string, displayName?: string | null): string {
  const name = displayName?.trim();
  if (name) return name;
  const body = wallet.startsWith('0x') || wallet.startsWith('0X') ? wallet.slice(2) : wallet;
  return body.slice(0, 8);
}

/** Non-negative integer string to bigint; anything else is null (sorted last). */
export function rawToBigInt(raw: string | null | undefined): bigint | null {
  if (raw === null || raw === undefined || !/^\d{1,78}$/.test(raw)) return null;
  return BigInt(raw);
}

/** Descending comparator that places null after every value. */
export function compareDescending(a: bigint | number | null, b: bigint | number | null): number {
  if (a === null && b === null) return 0;
  if (a === null) return 1;
  if (b === null) return -1;
  return a === b ? 0 : a < b ? 1 : -1;
}
