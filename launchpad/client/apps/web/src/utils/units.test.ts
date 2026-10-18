import { describe, it, expect } from 'vitest';
import { formatUnitsExact, parseUnitsExact, formatPriceFromDecimalString } from './units';

describe('formatUnitsExact', () => {
  it('formats sub-unit raw values exactly with no float loss', () => {
    expect(formatUnitsExact('1234', 18)).toBe('0.000000000000001234');
    expect(formatUnitsExact('1', 18)).toBe('0.000000000000000001');
  });

  it('formats whole and fractional values and trims trailing zeros', () => {
    expect(formatUnitsExact('1500000', 6)).toBe('1.5');
    expect(formatUnitsExact('1000000', 6)).toBe('1');
    expect(formatUnitsExact('1000001', 6)).toBe('1.000001');
  });

  it('preserves exact precision for very large balances that float64 would corrupt', () => {
    // 123456789012345678901234567890 wei @ 18dp — far beyond 2^53.
    expect(formatUnitsExact('123456789012345678901234567890', 18)).toBe(
      '123456789012.34567890123456789',
    );
  });

  it('handles zero and empty inputs', () => {
    expect(formatUnitsExact('0', 18)).toBe('0');
    expect(formatUnitsExact('', 18)).toBe('0');
    expect(formatUnitsExact(null, 18)).toBe('0');
    expect(formatUnitsExact(undefined, 6)).toBe('0');
  });

  it('accepts bigint and number raw inputs', () => {
    expect(formatUnitsExact(1500000n, 6)).toBe('1.5');
    expect(formatUnitsExact(2500000, 6)).toBe('2.5');
  });

  it('supports decimals=0 (pass-through integer)', () => {
    expect(formatUnitsExact('42', 0)).toBe('42');
  });
});

describe('parseUnitsExact', () => {
  it('is the exact inverse of formatUnitsExact for valid input', () => {
    expect(parseUnitsExact('1.5', 6)).toBe(1500000n);
    expect(parseUnitsExact('0.000000000000001234', 18)).toBe(1234n);
    expect(parseUnitsExact('1', 18)).toBe(1000000000000000000n);
  });

  it('truncates excess fractional digits (never rounds up — never overspend)', () => {
    expect(parseUnitsExact('0.0000000000000012349', 18)).toBe(1234n);
    expect(parseUnitsExact('1.9999999', 6)).toBe(1999999n);
  });

  it('handles empty / malformed input as zero', () => {
    expect(parseUnitsExact('', 6)).toBe(0n);
    expect(parseUnitsExact('abc', 6)).toBe(0n);
    expect(parseUnitsExact(null, 6)).toBe(0n);
  });
});

describe('formatPriceFromDecimalString', () => {
  it('formats >= $1 with two decimals', () => {
    expect(formatPriceFromDecimalString('12.5')).toBe('$12.50');
    expect(formatPriceFromDecimalString('1')).toBe('$1.00');
  });

  it('formats sub-dollar down to a cent with four decimals', () => {
    expect(formatPriceFromDecimalString('0.0123')).toBe('$0.0123');
  });

  it('uses plain leading zeros for up to 3 leading fractional zeros', () => {
    expect(formatPriceFromDecimalString('0.0001234')).toBe('$0.0001234');
  });

  it('uses subscript notation for deeply sub-cent prices, exactly', () => {
    // 14 leading zeros then 1234 (1.234e-15) — float toFixed(20) is unreliable here.
    expect(formatPriceFromDecimalString('0.000000000000001234')).toBe('$0.0\u2081\u20841234');
  });

  it('returns $0.00 for zero', () => {
    expect(formatPriceFromDecimalString('0')).toBe('$0.00');
    expect(formatPriceFromDecimalString('0.0')).toBe('$0.00');
  });
});
