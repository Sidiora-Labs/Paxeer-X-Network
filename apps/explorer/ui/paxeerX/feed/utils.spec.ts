import { describe, expect, it } from 'vitest';

import { formatChange, formatPrice, priceChange } from './utils';

describe('feed price formatting', () => {
  it('reads a price as an 18-decimal integer without passing through a float', () => {
    expect(formatPrice('67012500000000000000000')).toBe('67,012.5');
    expect(formatPrice('3456789012345678901234')).toBe('3,456.78901235');
    expect(formatPrice('1')).toBe('< 0.00000001');
    expect(formatPrice('0')).toBe('0');
  });

  it('measures the change from the previous price in percent', () => {
    const down = priceChange('67012500000000000000000', '67100000000000000000000');
    const up = priceChange('67100000000000000000000', '67000000000000000000000');

    expect(down && formatChange(down)).toBe('-0.13%');
    expect(up && formatChange(up)).toBe('+0.15%');
    expect(formatChange(priceChange('5', '5')!)).toBe('0.00%');
  });

  it('has no change without a previous price or against a zero price', () => {
    expect(priceChange('1', undefined)).toBeNull();
    expect(priceChange('1', '0')).toBeNull();
  });
});
