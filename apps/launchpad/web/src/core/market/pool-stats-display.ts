/**
 * Display reads of the v2 PoolStats row (interface 2.2.0: IF_STATS,
 * IF_STATS_BATCH, IF_POOL_DETAIL and the `market.stats.<pool>` stream frames
 * all carry the same row). Every read returns the value or the plain reason
 * the cell shows instead, so each surface renders the same answer for the
 * same bytes.
 *
 * Null handling, per field:
 * - `priceChangeBps{5m,1h,6h,24h}`: a value only when its baseline quality
 *   (`changeBaselineQuality{5m,1h,6h}`, `changeBaselineQuality` for 24h) is
 *   `complete`. `insufficient_history` reads "not enough trades yet",
 *   `unavailable` reads "unavailable"; a field the row does not carry at all
 *   (a pre-2.2.0 producer) reads "not served". Never interpolated.
 * - `spotPriceWad` (USDL per whole token, 1e18-scaled): null means the pool
 *   has no trade yet, so the cell shows an em dash with that reason, never 0.
 *   `spotPriceUsdlRaw` (6 decimals) is too coarse for sub-cent coins and is
 *   not used for display.
 * - Market cap is `totalSupplyValuationUsdlRaw` (price x total supply, the
 *   pump.fun "market cap"); `circulatingMarketCapUsdlRaw` is the secondary
 *   value. Both are null when `supplyValuationQuality` is `unavailable`.
 * - `top10HolderShareBps` / `creatorHoldingShareBps`: basis points of total
 *   supply; null reads "not computed for this coin yet".
 * - `uniqueTraders24h` / `txCount24h`: unsigned integer strings; absent reads
 *   as unknown (the explore row falls back to buys + sells for transactions).
 */
import { formatPriceFromDecimalString } from '@/utils/units';
import { bpsToPercent, scaleRawInteger, WAD_DECIMALS } from './market-units';
import type { PoolStats } from './use-stats-batch';

export type ChangeWindow = '5m' | '1h' | '6h' | '24h';
export type BaselineQuality = 'complete' | 'insufficient_history' | 'unavailable';

/** A display value, or the reason the cell shows a dash instead (exactly one of the two is non-null). */
export interface StatValue<T> {
  value: T | null;
  reason: string | null;
}

export const changeWindows: readonly ChangeWindow[] = ['5m', '1h', '6h', '24h'];


type ChangeField = 'priceChangeBps5m' | 'priceChangeBps1h' | 'priceChangeBps6h' | 'priceChangeBps24h';
type QualityField = 'changeBaselineQuality5m' | 'changeBaselineQuality1h' | 'changeBaselineQuality6h' | 'changeBaselineQuality';

const changeFields: Record<ChangeWindow, { change: ChangeField; quality: QualityField }> = {
  '5m': { change: 'priceChangeBps5m', quality: 'changeBaselineQuality5m' },
  '1h': { change: 'priceChangeBps1h', quality: 'changeBaselineQuality1h' },
  '6h': { change: 'priceChangeBps6h', quality: 'changeBaselineQuality6h' },
  '24h': { change: 'priceChangeBps24h', quality: 'changeBaselineQuality' },
};

export const noStatsReason = 'No stats for this coin yet';
export const noTradePriceReason = 'No trades yet';
export const noTradeMarketCapReason = 'No trades yet';
export const unverifiedSupplyReason = 'Market cap not available yet';
export const sharesNotComputedReason = 'Holder shares not available yet';

const ok = <T,>(value: T): StatValue<T> => ({ value, reason: null });
const none = <T,>(reason: string): StatValue<T> => ({ value: null, reason });

/** The plain reason a window change shows no value for this baseline quality. */
export function changeReason(window: ChangeWindow, quality: BaselineQuality | undefined): string {
  if (quality === 'insufficient_history') return `Not enough trades yet for a ${window} change`;
  return `${window} change not available yet`;
}

/** Percent change over `window`; a value only on a `complete` baseline. */
export function statsChange(stats: PoolStats | null | undefined, window: ChangeWindow): StatValue<number> {
  if (!stats) return none(noStatsReason);
  const { change, quality } = changeFields[window];
  const baseline = stats[quality] as BaselineQuality | undefined;
  if (baseline !== 'complete') return none(changeReason(window, baseline));
  const value = bpsToPercent(stats[change] ?? null);
  return value === null ? none(changeReason(window, 'unavailable')) : ok(value);
}

/** Every served window at once (the explore row keeps all four). */
export function statsChanges(stats: PoolStats | null | undefined): Record<ChangeWindow, StatValue<number>> {
  return { '5m': statsChange(stats, '5m'), '1h': statsChange(stats, '1h'), '6h': statsChange(stats, '6h'), '24h': statsChange(stats, '24h') };
}

const UINT = /^(0|[1-9][0-9]{0,77})$/;

/** Spot price as an exact decimal USDL string per whole token, from `spotPriceWad`. */
export function statsSpotPrice(stats: PoolStats | null | undefined): StatValue<string> {
  if (!stats) return none(noStatsReason);
  const wad = stats.spotPriceWad;
  if (wad === undefined) return none('Price not available yet');
  if (wad === null || !UINT.test(wad)) return none(noTradePriceReason);
  return ok(scaleRawInteger(wad, WAD_DECIMALS));
}

/** Market cap in raw USDL (6 decimals): `totalSupplyValuationUsdlRaw`, price x total supply. */
export function statsMarketCap(stats: PoolStats | null | undefined): StatValue<string> {
  return valuation(stats, stats?.totalSupplyValuationUsdlRaw);
}

/** Circulating market cap in raw USDL (6 decimals): price x circulating supply (`circulationModel`). */
export function statsCirculatingMarketCap(stats: PoolStats | null | undefined): StatValue<string> {
  return valuation(stats, stats?.circulatingMarketCapUsdlRaw);
}

function valuation(stats: PoolStats | null | undefined, raw: string | null | undefined): StatValue<string> {
  if (!stats) return none(noStatsReason);
  if (stats.supplyValuationQuality !== 'unavailable' && raw !== null && raw !== undefined && UINT.test(raw)) return ok(raw);
  return none(stats.spotPriceWad === null ? noTradeMarketCapReason : unverifiedSupplyReason);
}

/** Raw market cap or null, for sorting, filters and compact cells that only need the number. */
export function marketCapRaw(stats: PoolStats | null | undefined): string | null {
  return statsMarketCap(stats).value;
}

/** Share of total supply in percent: the ten largest holders, or the creator wallet. */
export function statsShare(stats: PoolStats | null | undefined, holder: 'top10' | 'creator'): StatValue<number> {
  if (!stats) return none(noStatsReason);
  const bps = holder === 'top10' ? stats.top10HolderShareBps : stats.creatorHoldingShareBps;
  if (bps === undefined) return none('Holder shares not available yet');
  if (bps === null || !Number.isInteger(bps) || bps < 0 || bps > 10_000) return none(sharesNotComputedReason);
  return ok(bps / 100);
}

/** Adaptive-precision USDL price: two decimals from $1, four above a cent, four significant digits with subscript zeros below; em dash when absent. */
export function formatSpotPrice(decimal: string | null | undefined): string {
  return decimal === null || decimal === undefined ? '—' : formatPriceFromDecimalString(decimal);
}

/** Share of supply as "12.3%" (one decimal, "100%" and "0%" whole). */
export function formatShare(percent: number): string {
  return Number.isInteger(percent) ? `${percent}%` : `${percent.toFixed(1)}%`;
}
