import { describe, expect, it } from 'vitest';
import { formatUsdl } from '@/utils/format';
import { bpsToPercent, countNumber, scaleRawInteger, usdlDecimalToRaw } from './market-units';

describe('raw integer scaling', () => {
  it('is exact for signed values and trims trailing zeros', () => {
    expect(scaleRawInteger('10098226981', 6)).toBe('10098.226981');
    expect(scaleRawInteger('-450', 2)).toBe('-4.5');
    expect(scaleRawInteger('-0', 2)).toBe('0');
    expect(scaleRawInteger('5', 18)).toBe('0.000000000000000005');
    expect(() => scaleRawInteger('1.5', 6)).toThrow(RangeError);
  });
  it('round-trips decimal USDL to raw 6-decimal units for formatUsdl', () => {
    expect(usdlDecimalToRaw('10098.226981')).toBe('10098226981');
    expect(usdlDecimalToRaw('-55')).toBe('-55000000');
    expect(usdlDecimalToRaw('n/a')).toBeNull();
    expect(formatUsdl(usdlDecimalToRaw(scaleRawInteger('10098226981', 6)), { compact: true })).toBe('10.1K USDL');
  });
  it('reads basis points and integer counts without inventing values', () => {
    expect(bpsToPercent('48')).toBe(0.48);
    expect(bpsToPercent('-450')).toBe(-4.5);
    expect(bpsToPercent('')).toBeNull();
    expect(bpsToPercent(null)).toBeNull();
    expect(bpsToPercent(undefined)).toBeNull();
    expect(countNumber('42')).toBe(42);
    expect(countNumber('-1')).toBeNull();
    expect(countNumber('99999999999999999999')).toBeNull();
    expect(countNumber(undefined)).toBeNull();
  });
});
