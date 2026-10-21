/**
 * Exact fixed-point unit conversion for money math (domain-15 / D2).
 *
 * On-chain values are integers scaled by 10^decimals (wei). Converting them to
 * JS `number` (float64) before formatting silently loses precision for large
 * balances and for very small prices — unacceptable on a financial UI. These
 * helpers operate on the raw integer DIGIT STRING (via BigInt) and never touch
 * float, so the displayed/parsed value is exact to the last wei.
 *
 * Use these for DISPLAY of money and for PARSING user-entered amounts (order
 * sizing, slippage). The legacy `fromWad`/`from6dec` (which return `number`)
 * remain only for relative arithmetic where ~15 significant digits suffice.
 */

/** Normalise any raw integer input into a `{ neg, digits }` pair (digits has no sign, no leading zeros except a single "0"). */
function normalizeIntegerString(raw: string | number | bigint): { neg: boolean; digits: string } {
  let s: string;
  if (typeof raw === 'bigint') s = raw.toString();
  else if (typeof raw === 'number') {
    if (!Number.isFinite(raw)) return { neg: false, digits: '0' };
    // A number raw is assumed to be an integer count of base units.
    s = BigInt(Math.trunc(raw)).toString();
  } else {
    s = String(raw).trim();
  }
  const neg = s.startsWith('-');
  if (neg || s.startsWith('+')) s = s.slice(1);
  // Keep only digits; bail to "0" on anything unexpected.
  if (!/^\d+$/.test(s)) return { neg: false, digits: '0' };
  s = s.replace(/^0+(?=\d)/, '');
  return { neg, digits: s };
}

/**
 * Format a raw integer (scaled by 10^decimals) as an EXACT decimal string.
 * Trailing fractional zeros are trimmed. No rounding, no float.
 *
 *   formatUnitsExact("1234", 18)  -> "0.000000000000001234"
 *   formatUnitsExact("1500000", 6) -> "1.5"
 *   formatUnitsExact("0", 18)      -> "0"
 */
export function formatUnitsExact(raw: string | number | bigint | undefined | null, decimals: number): string {
  if (raw === undefined || raw === null || raw === '') return '0';
  const { neg, digits } = normalizeIntegerString(raw);
  if (digits === '0') return '0';
  const sign = neg ? '-' : '';

  if (decimals <= 0) return `${sign}${digits}`;

  const padded = digits.padStart(decimals + 1, '0');
  const intPart = padded.slice(0, padded.length - decimals);
  const fracPart = padded.slice(padded.length - decimals).replace(/0+$/, '');
  return fracPart ? `${sign}${intPart}.${fracPart}` : `${sign}${intPart}`;
}

/**
 * Parse a human decimal string into a raw integer (scaled by 10^decimals) as a
 * BigInt. EXACT inverse of `formatUnitsExact` for valid input. Excess fractional
 * digits beyond `decimals` are truncated (never rounded up — never overspend).
 *
 *   parseUnitsExact("1.5", 6)  -> 1500000n
 *   parseUnitsExact("0.0000000000000012349", 18) -> 1234n  (extra digit truncated)
 */
export function parseUnitsExact(value: string | number | undefined | null, decimals: number): bigint {
  if (value === undefined || value === null || value === '') return 0n;
  let s = String(value).trim();
  const neg = s.startsWith('-');
  if (neg || s.startsWith('+')) s = s.slice(1);
  if (s === '' || !/^\d*\.?\d*$/.test(s)) return 0n;

  const [intPart = '0', fracPartRaw = ''] = s.split('.');
  const frac = fracPartRaw.slice(0, Math.max(0, decimals)).padEnd(Math.max(0, decimals), '0');
  const combined = `${intPart || '0'}${frac}`.replace(/^0+(?=\d)/, '');
  const magnitude = BigInt(combined || '0');
  return neg ? -magnitude : magnitude;
}

const SUBSCRIPT_DIGITS = ['\u2080', '\u2081', '\u2082', '\u2083', '\u2084', '\u2085', '\u2086', '\u2087', '\u2088', '\u2089'];

/**
 * Format an EXACT non-negative decimal string as a USD price with the project's
 * subscript-zero notation for sub-cent values. Operates purely on the decimal
 * string so tiny prices keep full significant-digit precision (no float).
 */
export function formatPriceFromDecimalString(dec: string): string {
  const neg = dec.startsWith('-');
  const body = neg ? dec.slice(1) : dec;
  const [intStr = '0', fracStr = ''] = body.split('.');

  const isZero = /^0*$/.test(intStr) && /^0*$/.test(fracStr);
  if (isZero) return '$0.00';

  const prefix = neg ? '-$' : '$';
  const asNum = Number(body);
  // For values >= $0.01 the legacy float rounding is precise enough and keeps
  // output identical to the prior implementation.
  if (asNum >= 1) return `${prefix}${asNum.toFixed(2)}`;
  if (asNum >= 0.01) return `${prefix}${asNum.toFixed(4)}`;

  // Sub-cent: derive significant digits exactly from the fractional string.
  let zeros = 0;
  for (const ch of fracStr) {
    if (ch === '0') zeros++;
    else break;
  }
  const sig = fracStr.slice(zeros, zeros + 4).replace(/0+$/, '');
  if (!sig) return '$0.00';
  if (zeros <= 3) return `${prefix}0.${'0'.repeat(zeros)}${sig}`;
  const sub = String(zeros)
    .split('')
    .map((d) => SUBSCRIPT_DIGITS[Number(d)] ?? d)
    .join('');
  return `${prefix}0.0${sub}${sig}`;
}
