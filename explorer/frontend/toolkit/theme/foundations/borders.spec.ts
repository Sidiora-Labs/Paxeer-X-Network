import { describe, expect, it } from 'vitest';

import { radii } from './borders';

const SCALE = [ 'none', 'xs', 'sm', 'base', 'md', 'lg', 'xl', '2xl', 'full' ];

describe('radii', () => {
  it('declares the product corner scale in order', () => {
    expect(Object.keys(radii ?? {})).toEqual(SCALE);
  });

  it('grows with every step of the scale', () => {
    const sizes = SCALE.map((key) => parseInt((radii?.[key] as unknown as { value: string }).value, 10));

    sizes.slice(1).forEach((size, index) => {
      expect(size).toBeGreaterThan(sizes[index]);
    });
  });
});
