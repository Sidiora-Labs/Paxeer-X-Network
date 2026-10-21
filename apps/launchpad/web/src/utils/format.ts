/**
 * (3.9) Formatting utilities. The shared backend package (packages/shared/src/util/format.ts)
 * has BigInt-based formatPrice/formatVolume for server use. This module uses Number-based
 * formatting with subscript digits for client display. Keep both in sync when modifying.
 */

import { formatUnitsExact, formatPriceFromDecimalString } from './units';

export function formatAddress(address: string, chars = 4): string {
  if (!address) return '';
  return `${address.slice(0, chars + 2)}...${address.slice(-chars)}`;
}

/** Safely call .toFixed() on any value — coerces strings/null/undefined to number first. */
export function safeFixed(value: unknown, decimals = 2): string {
  return Number(value ?? 0).toFixed(decimals);
}

export function formatCurrency(value: number | string | undefined | null, decimals = 2): string {
  const v = Number(value ?? 0);
  if (v >= 1_000_000_000) return `$${(v / 1_000_000_000).toFixed(decimals)}B`;
  if (v >= 1_000_000) return `$${(v / 1_000_000).toFixed(decimals)}M`;
  if (v >= 1_000) return `$${(v / 1_000).toFixed(decimals)}K`;
  return `$${v.toFixed(decimals)}`;
}

export function formatNumber(value: number | string | undefined | null, decimals = 2): string {
  const v = Number(value ?? 0);
  if (v >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(decimals)}B`;
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(decimals)}M`;
  if (v >= 1_000) return `${(v / 1_000).toFixed(decimals)}K`;
  return v.toFixed(decimals);
}

export interface FormatPercentOptions {
  decimals?: number;
  sign?: 'always' | 'negative';
}

export function formatPercent(value: number | string | undefined | null, opts?: FormatPercentOptions | number): string {
  const options: FormatPercentOptions = typeof opts === 'number' ? { decimals: opts } : (opts ?? {});
  const decimals = options.decimals ?? 2;
  const v = Number(value ?? 0);
  if (!Number.isFinite(v)) return '\u2014';
  const sign = v >= 0 && options.sign !== 'negative' ? '+' : '';
  return `${sign}${v.toFixed(decimals)}%`;
}

const PRICE_DECIMALS = 18;
const AMOUNT_DECIMALS = 6;

export function fromWad(raw: string | number | undefined | null): number {
  if (raw === undefined || raw === null || raw === '' || raw === '0') return 0;
  const s = String(raw);
  if (s.length <= PRICE_DECIMALS) return Number('0.' + s.padStart(PRICE_DECIMALS, '0'));
  const intPart = s.slice(0, s.length - PRICE_DECIMALS);
  const fracPart = s.slice(s.length - PRICE_DECIMALS);
  return Number(intPart + '.' + fracPart);
}

export function from6dec(raw: string | number | undefined | null): number {
  if (raw === undefined || raw === null || raw === '' || raw === '0') return 0;
  const s = String(raw);
  if (s.length <= AMOUNT_DECIMALS) return Number('0.' + s.padStart(AMOUNT_DECIMALS, '0'));
  const intPart = s.slice(0, s.length - AMOUNT_DECIMALS);
  const fracPart = s.slice(s.length - AMOUNT_DECIMALS);
  return Number(intPart + '.' + fracPart);
}

const SUBSCRIPT_DIGITS = ['\u2080','\u2081','\u2082','\u2083','\u2084','\u2085','\u2086','\u2087','\u2088','\u2089'];

/** Float-based price formatting — used only when the caller already holds a human number. */
function formatPriceFromNumber(val: number): string {
  if (val === 0) return '$0.00';
  if (val >= 1) return `$${val.toFixed(2)}`;
  if (val >= 0.01) return `$${val.toFixed(4)}`;
  const s = val.toFixed(20);
  const afterDot = s.slice(2);
  let zeros = 0;
  for (const ch of afterDot) { if (ch === '0') zeros++; else break; }
  const sig = afterDot.slice(zeros, zeros + 4).replace(/0+$/, '');
  if (zeros <= 3) return `$0.${'0'.repeat(zeros)}${sig}`;
  const sub = String(zeros).split('').map(d => SUBSCRIPT_DIGITS[Number(d)] ?? d).join('');
  return `$0.0${sub}${sig}`;
}

/**
 * Format a token price as USD. When given a RAW 18-decimal wad string, the value
 * is formatted EXACTLY from the integer string (BigInt path) so very small
 * prices never lose significant digits to float rounding (D2). A `number` input
 * is treated as an already-human price and formatted via the float path.
 */
export function formatPrice(raw: string | number | undefined | null): string {
  if (raw === undefined || raw === null || raw === '') return '$0.00';
  if (typeof raw === 'number') return formatPriceFromNumber(raw);
  return formatPriceFromDecimalString(formatUnitsExact(raw, PRICE_DECIMALS));
}

export function formatVolume(raw: string | number | undefined | null): string {
  return formatCurrency(from6dec(raw));
}



/* ---------------------------------------------------------------------------
 * BigInt-exact shared formatters (fleet contract: @/utils/format).
 * Raw amounts are integer strings or bigint scaled by 10^decimals. The raw to
 * decimal conversion never touches float: it is BigInt division plus padding.
 * ------------------------------------------------------------------------- */

const EM_DASH = '\u2014';

function pow10(n: number): bigint {
  return 10n ** BigInt(Math.max(0, Math.trunc(n)));
}

function toBigInt(raw: string | bigint | number): bigint | null {
  if (typeof raw === 'bigint') return raw;
  if (typeof raw === 'number') return Number.isFinite(raw) ? BigInt(Math.trunc(raw)) : null;
  const s = raw.trim();
  if (!/^[+-]?\d+$/.test(s)) return null;
  return BigInt(s);
}

/** Scale a raw integer to `fractionDigits` and round half up. Returns the signed scaled integer. */
function scaleRaw(raw: bigint, decimals: number, fractionDigits: number): bigint {
  const neg = raw < 0n;
  const abs = neg ? -raw : raw;
  const shift = decimals - fractionDigits;
  let scaled: bigint;
  if (shift <= 0) scaled = abs * pow10(-shift);
  else {
    const divisor = pow10(shift);
    scaled = abs / divisor;
    if ((abs % divisor) * 2n >= divisor) scaled += 1n;
  }
  return neg ? -scaled : scaled;
}

function groupThousands(integer: string): string {
  return integer.replace(/\B(?=(\d{3})+(?!\d))/g, ',');
}

function splitScaled(scaled: bigint, fractionDigits: number): { neg: boolean; integer: string; fraction: string } {
  const neg = scaled < 0n;
  const abs = (neg ? -scaled : scaled).toString();
  if (fractionDigits === 0) return { neg, integer: abs, fraction: '' };
  const padded = abs.padStart(fractionDigits + 1, '0');
  return { neg, integer: padded.slice(0, -fractionDigits), fraction: padded.slice(-fractionDigits) };
}

/** Exact decimal string for a raw amount (no grouping, trailing zeros trimmed unless `fractionDigits` is fixed). */
export function rawToDecimalString(raw: string | bigint, decimals: number, fractionDigits?: number): string {
  const value = toBigInt(raw);
  if (value === null) return '0';
  if (fractionDigits === undefined) {
    const { neg, integer, fraction } = splitScaled(value, decimals);
    const trimmed = fraction.replace(/0+$/, '');
    return `${neg ? '-' : ''}${integer}${trimmed ? '.' + trimmed : ''}`;
  }
  const { neg, integer, fraction } = splitScaled(scaleRaw(value, decimals, fractionDigits), fractionDigits);
  return `${neg ? '-' : ''}${integer}${fraction ? '.' + fraction : ''}`;
}

const COMPACT_STEPS: Array<{ limit: bigint; suffix: string }> = [
  { limit: 1_000_000_000_000n, suffix: 'T' },
  { limit: 1_000_000_000n, suffix: 'B' },
  { limit: 1_000_000n, suffix: 'M' },
  { limit: 1_000n, suffix: 'K' },
];

/** Compact a scaled integer (already at `fractionDigits`) into "1.23K" style; exact up to the displayed digits. */
function compactScaled(scaled: bigint, fractionDigits: number, maxCompactDigits = 2): string {
  const neg = scaled < 0n;
  const abs = neg ? -scaled : scaled;
  const unit = pow10(fractionDigits);
  const whole = abs / unit;
  for (const step of COMPACT_STEPS) {
    if (whole >= step.limit) {
      const limitScaled = step.limit * unit;
      const rounded = (abs * pow10(maxCompactDigits) * 2n + limitScaled) / (limitScaled * 2n);
      const { integer, fraction } = splitScaled(rounded, maxCompactDigits);
      const trimmed = fraction.replace(/0+$/, '');
      return `${neg ? '-' : ''}${integer}${trimmed ? '.' + trimmed : ''}${step.suffix}`;
    }
  }
  const { integer, fraction } = splitScaled(abs, fractionDigits);
  return `${neg ? '-' : ''}${groupThousands(integer)}${fraction ? '.' + fraction : ''}`;
}

export interface FormatUsdlOptions {
  /** Raw scale of the amount. USDL is 6. */
  decimals?: number;
  /** Fixed fraction digits in the plain form. Default 2. */
  fractionDigits?: number;
  /** "1.23K USDL" instead of "1,234.56 USDL". */
  compact?: boolean;
  /** Append the unit label. Default true. */
  unit?: boolean;
  /** Always show the sign for positive values. */
  signed?: boolean;
}

/** Format a raw USDL amount (6 decimals) with the unit in text. Nonzero values below the floor read "< 0.01 USDL". */
export function formatUsdl(raw: string | bigint | null | undefined, opts: FormatUsdlOptions = {}): string {
  const { decimals = 6, fractionDigits = 2, compact = false, unit = true, signed = false } = opts;
  const suffix = unit ? ' USDL' : '';
  if (raw === null || raw === undefined) return EM_DASH;
  const value = toBigInt(raw);
  if (value === null) return EM_DASH;
  const scaled = scaleRaw(value, decimals, fractionDigits);
  if (value !== 0n && scaled === 0n) {
    const floor = fractionDigits > 0 ? `0.${'0'.repeat(fractionDigits - 1)}1` : '1';
    return `${value < 0n ? '> -' : '< '}${floor}${suffix}`;
  }
  const { neg, integer, fraction } = splitScaled(scaled, fractionDigits);
  const body = compact
    ? compactScaled(scaled < 0n ? -scaled : scaled, fractionDigits)
    : `${groupThousands(integer)}${fraction ? '.' + fraction : ''}`;
  const sign = neg ? '-' : signed && scaled > 0n ? '+' : '';
  return `${sign}${body}${suffix}`;
}

/** Compact USD display for an already-decimal value: "$1.23K", "$12.5M", "$0.42". */
export function formatCompactUsd(value: number | string | null | undefined): string {
  if (value === null || value === undefined || value === '') return EM_DASH;
  const v = typeof value === 'number' ? value : Number(value);
  if (!Number.isFinite(v)) return EM_DASH;
  const neg = v < 0;
  const abs = Math.abs(v);
  const sign = neg ? '-' : '';
  if (abs >= 1_000) {
    const cents = BigInt(Math.round(abs * 100));
    return `${sign}$${compactScaled(cents, 2)}`;
  }
  if (abs >= 1) return `${sign}$${abs.toFixed(2)}`;
  if (abs === 0) return '$0.00';
  if (abs < 0.01) return `${sign}< $0.01`;
  return `${sign}$${abs.toFixed(2)}`;
}

export interface FormatTokenAmountOptions {
  /** Maximum fraction digits shown in the plain form (trailing zeros trimmed). Default 4. */
  maxFraction?: number;
  /** "1.5K" style for large amounts. Default true. */
  compact?: boolean;
}

/** Format a raw token amount scaled by `decimals`. Default scale 6 (the launchpad token scale). */
export function formatTokenAmount(
  raw: string | bigint | number | null | undefined,
  decimals = 6,
  opts: FormatTokenAmountOptions = {},
): string {
  const { maxFraction = 4, compact = true } = opts;
  if (raw === null || raw === undefined || raw === '') return EM_DASH;
  const value = toBigInt(raw);
  if (value === null) return EM_DASH;
  const scaled = scaleRaw(value, decimals, maxFraction);
  if (value !== 0n && scaled === 0n) return `< 0.${'0'.repeat(Math.max(0, maxFraction - 1))}1`;
  const abs = scaled < 0n ? -scaled : scaled;
  const sign = scaled < 0n ? '-' : '';
  if (compact && abs / pow10(maxFraction) >= 1_000n) {
    return `${sign}${compactScaled(abs, maxFraction)}`;
  }
  const { integer, fraction } = splitScaled(abs, maxFraction);
  const trimmed = fraction.replace(/0+$/, '');
  return `${sign}${groupThousands(integer)}${trimmed ? '.' + trimmed : ''}`;
}

/** "0x1234...abcd": `head` hex digits after the 0x prefix, `tail` at the end. */
export function shortAddress(address: string | null | undefined, head = 4, tail = 4): string {
  if (!address) return '';
  const a = address.trim();
  const hasPrefix = /^0x/i.test(a);
  const body = hasPrefix ? a.slice(2) : a;
  if (body.length <= head + tail) return a;
  return `${hasPrefix ? '0x' : ''}${body.slice(0, head)}...${body.slice(-tail)}`;
}

function toEpochMs(input: string | number | Date): number | null {
  if (input instanceof Date) return Number.isNaN(input.getTime()) ? null : input.getTime();
  if (typeof input === 'number') {
    if (!Number.isFinite(input)) return null;
    return input > 1e12 ? input : input * 1000;
  }
  const s = input.trim();
  if (/^\d+$/.test(s)) return toEpochMs(Number(s));
  const t = Date.parse(s);
  return Number.isNaN(t) ? null : t;
}

/** Compact relative age: "12s", "5m", "3h", "2d", "3mo", "1y". Accepts ISO strings, unix seconds or unix ms. */
export function relativeAge(isoOrUnix: string | number | Date | null | undefined, now: number = Date.now()): string {
  if (isoOrUnix === null || isoOrUnix === undefined || isoOrUnix === '') return EM_DASH;
  const t = toEpochMs(isoOrUnix);
  if (t === null) return EM_DASH;
  const diff = Math.max(0, Math.floor((now - t) / 1000));
  if (diff < 60) return `${diff}s`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`;
  if (diff < 86400 * 30) return `${Math.floor(diff / 86400)}d`;
  if (diff < 86400 * 365) return `${Math.floor(diff / (86400 * 30))}mo`;
  return `${Math.floor(diff / (86400 * 365))}y`;
}

/** Count formatting: "999", "1,234" below 10K; "12.3K", "1.2M" above. */
export function formatCount(n: number | bigint | string | null | undefined): string {
  if (n === null || n === undefined || n === '') return EM_DASH;
  const value = typeof n === 'bigint' ? n : toBigInt(typeof n === 'number' ? Math.round(n) : n);
  if (value === null) return EM_DASH;
  const abs = value < 0n ? -value : value;
  if (abs < 10_000n) return `${value < 0n ? '-' : ''}${groupThousands(abs.toString())}`;
  return `${value < 0n ? '-' : ''}${compactScaled(abs, 0, 1)}`;
}
