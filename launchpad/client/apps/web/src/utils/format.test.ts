import { describe, it, expect } from 'vitest';
import {
  fromWad,
  from6dec,
  formatPrice,
  formatCurrency,
  formatNumber,
  formatPercent,
  formatUsdl,
  formatCompactUsd,
  formatTokenAmount,
  shortAddress,
  relativeAge,
  formatCount,
  rawToDecimalString,
} from './format';

/**
 * Behavior-pinning tests for the money formatters.
 *
 * These lock the CURRENT (Number/float-based) behavior of the formatting
 * utilities as a safety net BEFORE the Phase-1 BigInt/decimal precision
 * refactor. They intentionally assert the existing float semantics — including
 * its sub-cent float-noise — so the refactor surfaces every behavioral change
 * instead of silently shifting displayed prices/balances.
 *
 * Real `format.ts` functions are exercised directly; nothing is mocked.
 */

describe('fromWad (18-decimal wei -> number)', () => {
  it('returns 0 for empty/null/undefined/zero', () => {
    expect(fromWad(undefined)).toBe(0);
    expect(fromWad(null)).toBe(0);
    expect(fromWad('')).toBe(0);
    expect(fromWad('0')).toBe(0);
    expect(fromWad(0)).toBe(0);
  });

  it('converts whole-unit wei amounts', () => {
    expect(fromWad('1000000000000000000')).toBe(1);
    expect(fromWad('1500000000000000000')).toBe(1.5);
    expect(fromWad('2000000000000000000')).toBe(2);
  });

  it('converts sub-unit wei amounts (length <= 18)', () => {
    expect(fromWad('500000000000000000')).toBe(0.5);
  });

  it('converts large multi-digit integer parts', () => {
    expect(fromWad('123000000000000000000')).toBe(123);
  });
});

describe('from6dec (6-decimal -> number)', () => {
  it('returns 0 for empty/null/undefined/zero', () => {
    expect(from6dec(undefined)).toBe(0);
    expect(from6dec(null)).toBe(0);
    expect(from6dec('')).toBe(0);
    expect(from6dec('0')).toBe(0);
  });

  it('converts whole-unit amounts', () => {
    expect(from6dec('1000000')).toBe(1);
    expect(from6dec('1500000')).toBe(1.5);
  });

  it('converts sub-unit amounts (length <= 6)', () => {
    expect(from6dec('500000')).toBe(0.5);
    expect(from6dec('123')).toBe(0.000123);
  });
});

describe('formatCurrency', () => {
  it('formats plain values under 1000', () => {
    expect(formatCurrency(0)).toBe('$0.00');
    expect(formatCurrency(12.5)).toBe('$12.50');
    expect(formatCurrency(null)).toBe('$0.00');
    expect(formatCurrency(undefined)).toBe('$0.00');
  });

  it('abbreviates thousands / millions / billions', () => {
    expect(formatCurrency(1500)).toBe('$1.50K');
    expect(formatCurrency(1500000)).toBe('$1.50M');
    expect(formatCurrency(1234567890)).toBe('$1.23B');
  });

  it('coerces numeric strings', () => {
    expect(formatCurrency('1234.5')).toBe('$1.23K');
  });
});

describe('formatNumber', () => {
  it('abbreviates without a currency prefix', () => {
    expect(formatNumber(999)).toBe('999.00');
    expect(formatNumber(1500)).toBe('1.50K');
    expect(formatNumber(1500000)).toBe('1.50M');
  });
});

describe('formatPercent', () => {
  it('adds an explicit sign', () => {
    expect(formatPercent(1.234)).toBe('+1.23%');
    expect(formatPercent(-1.234)).toBe('-1.23%');
    expect(formatPercent(0)).toBe('+0.00%');
  });
});

describe('formatPrice', () => {
  it('formats zero and values >= 1', () => {
    expect(formatPrice(0)).toBe('$0.00');
    expect(formatPrice(1)).toBe('$1.00');
    expect(formatPrice(2.5)).toBe('$2.50');
  });

  it('formats values between 0.01 and 1 with 4 decimals', () => {
    expect(formatPrice(0.05)).toBe('$0.0500');
    expect(formatPrice(0.123)).toBe('$0.1230');
  });

  it('accepts a wei string and converts via fromWad', () => {
    expect(formatPrice('1000000000000000000')).toBe('$1.00');
    expect(formatPrice('2500000000000000000')).toBe('$2.50');
  });

  it('renders tiny sub-cent prices (4 leading zeros or fewer) inline', () => {
    expect(formatPrice(0.001)).toBe('$0.001');
  });

  it('renders very tiny sub-cent prices with subscript leading-zero count', () => {
    const out = formatPrice(0.0000123);
    // Subscript-4 form: "$0.0" + subscript count of leading zeros + sig digits.
    expect(out.startsWith('$0.0')).toBe(true);
    expect(out).toContain('\u2084'); // subscript "4" = four leading zeros
  });
});

describe('formatPercent options', () => {
  it('accepts an options object and the negative-only sign mode', () => {
    expect(formatPercent(12.345, { decimals: 1 })).toBe('+12.3%');
    expect(formatPercent(12.345, { sign: 'negative' })).toBe('12.35%');
    expect(formatPercent(-3, { sign: 'negative', decimals: 0 })).toBe('-3%');
    expect(formatPercent(Number.NaN)).toBe('—');
  });
});

describe('formatUsdl (raw 6-decimal, BigInt exact)', () => {
  it('formats plain amounts with grouping and the unit', () => {
    expect(formatUsdl('1234567890')).toBe('1,234.57 USDL');
    expect(formatUsdl(1000000n)).toBe('1.00 USDL');
    expect(formatUsdl('0')).toBe('0.00 USDL');
    expect(formatUsdl('-2500000')).toBe('-2.50 USDL');
  });

  it('keeps precision on amounts beyond float range', () => {
    expect(formatUsdl('123456789012345678901234567')).toBe('123,456,789,012,345,678,901.23 USDL');
  });

  it('shows a floor instead of 0.00 for nonzero dust', () => {
    expect(formatUsdl('4999')).toBe('< 0.01 USDL');
    expect(formatUsdl('5000')).toBe('0.01 USDL');
    expect(formatUsdl('400000', { fractionDigits: 0 })).toBe('< 1 USDL');
    expect(formatUsdl('-400000', { fractionDigits: 0 })).toBe('> -1 USDL');
    expect(formatUsdl('2500000', { fractionDigits: 0 })).toBe('3 USDL');
    expect(formatUsdl('4999', { fractionDigits: 3 })).toBe('0.005 USDL');
    expect(formatUsdl('400', { fractionDigits: 3 })).toBe('< 0.001 USDL');
  });

  it('supports compact, unitless and signed forms', () => {
    expect(formatUsdl('1234567890', { compact: true })).toBe('1.23K USDL');
    expect(formatUsdl('143000000000', { compact: true, unit: false })).toBe('143K');
    expect(formatUsdl('2500000000000', { compact: true })).toBe('2.5M USDL');
    expect(formatUsdl('1000000', { signed: true })).toBe('+1.00 USDL');
    expect(formatUsdl(null)).toBe('—');
    expect(formatUsdl('abc')).toBe('—');
  });

  it('honours a different raw scale', () => {
    expect(formatUsdl('1500000000000000000', { decimals: 18 })).toBe('1.50 USDL');
  });
});

describe('formatCompactUsd', () => {
  it('compacts thousands, millions and billions', () => {
    expect(formatCompactUsd(1234)).toBe('$1.23K');
    expect(formatCompactUsd('12500000')).toBe('$12.5M');
    expect(formatCompactUsd(1_000_000_000)).toBe('$1B');
    expect(formatCompactUsd(999.5)).toBe('$999.50');
    expect(formatCompactUsd(0.42)).toBe('$0.42');
    expect(formatCompactUsd(0.001)).toBe('< $0.01');
    expect(formatCompactUsd(-1500)).toBe('-$1.5K');
    expect(formatCompactUsd(0)).toBe('$0.00');
    expect(formatCompactUsd(null)).toBe('—');
    expect(formatCompactUsd('nope')).toBe('—');
  });
});

describe('formatTokenAmount (raw + decimals)', () => {
  it('converts with BigInt and trims trailing zeros', () => {
    expect(formatTokenAmount('1500000', 6)).toBe('1.5');
    expect(formatTokenAmount('1234567', 6)).toBe('1.2346');
    expect(formatTokenAmount('1000000000000000000', 18)).toBe('1');
    expect(formatTokenAmount('123456789000000000000000000', 18)).toBe('123.46M');
  });

  it('compacts large amounts and respects the plain option', () => {
    expect(formatTokenAmount('1500000000', 6)).toBe('1.5K');
    expect(formatTokenAmount('1500000000', 6, { compact: false })).toBe('1,500');
    expect(formatTokenAmount('15', 6)).toBe('< 0.0001');
    expect(formatTokenAmount(null)).toBe('—');
  });

  it('defaults to the 6-decimal scale for legacy single-argument callers', () => {
    expect(formatTokenAmount('2000000')).toBe('2');
  });
});

describe('shortAddress', () => {
  it('keeps the prefix, head and tail', () => {
    expect(shortAddress('0x1234567890abcdef1234567890abcdef12345678')).toBe('0x1234...5678');
    expect(shortAddress('0x1234567890abcdef1234567890abcdef12345678', 6, 4)).toBe('0x123456...5678');
    expect(shortAddress('0x1234')).toBe('0x1234');
    expect(shortAddress('')).toBe('');
    expect(shortAddress(undefined)).toBe('');
  });
});

describe('relativeAge', () => {
  const now = Date.parse('2026-10-12T12:00:00Z');
  it('formats ISO strings, unix seconds and unix ms', () => {
    expect(relativeAge('2026-10-12T11:59:48Z', now)).toBe('12s');
    expect(relativeAge('2026-10-12T11:55:00Z', now)).toBe('5m');
    expect(relativeAge((now - 3 * 3600_000) / 1000, now)).toBe('3h');
    expect(relativeAge(now - 2 * 86400_000, now)).toBe('2d');
    expect(relativeAge('2026-07-01T12:00:00Z', now)).toBe('3mo');
    expect(relativeAge('2024-01-01T00:00:00Z', now)).toBe('2y');
  });
  it('clamps the future to zero and rejects garbage', () => {
    expect(relativeAge(now + 60_000, now)).toBe('0s');
    expect(relativeAge('not a date', now)).toBe('—');
    expect(relativeAge(null, now)).toBe('—');
  });
});

describe('formatCount', () => {
  it('groups small counts and compacts large ones', () => {
    expect(formatCount(0)).toBe('0');
    expect(formatCount(999)).toBe('999');
    expect(formatCount(1234)).toBe('1,234');
    expect(formatCount(12345)).toBe('12.3K');
    expect(formatCount(1_200_000)).toBe('1.2M');
    expect(formatCount('5000000000')).toBe('5B');
    expect(formatCount(null)).toBe('—');
  });
});

describe('rawToDecimalString', () => {
  it('is exact and optionally fixed', () => {
    expect(rawToDecimalString('1234', 18)).toBe('0.000000000000001234');
    expect(rawToDecimalString('1500000', 6, 2)).toBe('1.50');
    expect(rawToDecimalString('-1500000', 6)).toBe('-1.5');
  });
});
