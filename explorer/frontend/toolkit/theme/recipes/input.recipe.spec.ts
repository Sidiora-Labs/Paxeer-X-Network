import { describe, expect, it } from 'vitest';

import { recipe } from './input.recipe';

const outline = recipe.variants?.variant.outline;

describe('input recipe, outline variant', () => {
  it('draws a one pixel border from the input tokens', () => {
    expect(outline).toMatchObject({
      bg: 'input.bg',
      borderWidth: '1px',
      borderColor: 'input.border.filled',
    });
  });

  it('lifts the field on the overlay elevation while it holds focus', () => {
    expect(outline?._focus).toMatchObject({
      borderColor: 'input.border.focus',
      boxShadow: 'overlay',
    });
  });

  it('keeps the outline variant as the default field', () => {
    expect(recipe.defaultVariants?.variant).toBe('outline');
  });
});
