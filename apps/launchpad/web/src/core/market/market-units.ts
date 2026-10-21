/**
 * Pure unit helpers for market figures served as raw integers by the v2
 * contracts (core/api, core/stats-workers). Everything here is exact string
 * arithmetic: no float rounding before display.
 *
 * - USDL money fields (`*UsdlRaw`, `volume24hRaw`) are raw 6-decimal integers.
 * - Price ratios computed in the client (poolSpotPrice) are WAD (18-decimal).
 * - Change and share fields (`priceChangeBps*`, `*ShareBps`) are signed or
 *   unsigned basis points.
 */

/** USDL settlement decimals (CORE_DEPLOYMENT.usdlDecimals, accounting Settlement.Decimals). */
export const USDL_DECIMALS = 6;
/** WAD fixed-point scale of client-side price ratios (shared/db.WAD). */
export const WAD_DECIMALS = 18;

const SIGNED_RAW = /^-?(0|[1-9][0-9]{0,77})$/;

/** Exact integer-string to decimal-string scaling ("10098226981", 6 -> "10098.226981"); trailing zeros trimmed, never exponent notation. */
export function scaleRawInteger(raw: string, decimals: number): string {
  if (!SIGNED_RAW.test(raw) || !Number.isInteger(decimals) || decimals < 0 || decimals > 77) throw new RangeError('Invalid raw integer');
  const negative = raw.startsWith('-');
  const digits = negative ? raw.slice(1) : raw;
  const padded = digits.padStart(decimals + 1, '0');
  const whole = decimals ? padded.slice(0, -decimals) : padded;
  const fraction = decimals ? padded.slice(-decimals).replace(/0+$/, '') : '';
  const body = `${whole}${fraction ? '.' + fraction : ''}`;
  return negative && body !== '0' ? `-${body}` : body;
}

/** Decimal USDL string ("10098.226981") to raw 6-decimal units ("10098226981"); null when not a plain decimal. */
export function usdlDecimalToRaw(value: string | null | undefined): string | null {
  if (value === null || value === undefined) return null;
  const match = /^(-)?(\d+)(?:\.(\d+))?$/.exec(value.trim());
  if (!match) return null;
  const [, sign, whole, fraction = ''] = match;
  const scaled = (whole + fraction.slice(0, USDL_DECIMALS).padEnd(USDL_DECIMALS, '0')).replace(/^0+(?=\d)/, '');
  return sign && scaled !== '0' ? `-${scaled}` : scaled;
}

/** Basis-point integer string to percent (parity with core/api store.BpsToPct); null when absent or not numeric. */
export function bpsToPercent(bps: string | null | undefined): number | null {
  if (bps === null || bps === undefined || bps.trim() === '') return null;
  const n = Number(bps);
  return Number.isFinite(n) ? Math.round(n) / 100 : null;
}

/** Non-negative integer string (UInt256 counts such as `buys24h`) to a safe number; null when absent or beyond 2^53. */
export function countNumber(value: string | null | undefined): number | null {
  if (value === null || value === undefined || !/^(0|[1-9][0-9]*)$/.test(value)) return null;
  const n = Number(value);
  return Number.isSafeInteger(n) ? n : null;
}

/** Raw USDL integer (6 decimals) to its exact decimal string ("10" -> "0.00001"); null when absent or not an integer. */
export function usdlRawToDecimal(raw: string | null | undefined): string | null {
  if (raw === null || raw === undefined || !SIGNED_RAW.test(raw)) return null;
  return scaleRawInteger(raw, USDL_DECIMALS);
}
