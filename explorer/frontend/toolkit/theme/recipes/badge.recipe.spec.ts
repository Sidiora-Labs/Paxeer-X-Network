import { describe, expect, it } from 'vitest';

import { recipe } from './badge.recipe';

const direction = recipe.variants?.variant.direction;

describe('badge recipe, direction variant', () => {
  it('reads as a short upper-case marker', () => {
    expect(direction).toMatchObject({
      borderRadius: 'sm',
      textStyle: 'xs',
      textTransform: 'uppercase',
      justifyContent: 'center',
      fontWeight: '500',
    });
  });

  it('declares no colour of its own so the palette supplies both directions', () => {
    expect(JSON.stringify(direction)).not.toMatch(/"color":/);
  });

  it('keeps the subtle variant and the default the rest of the app uses', () => {
    expect(recipe.variants?.variant.subtle).toBeDefined();
    expect(recipe.defaultVariants?.variant).toBe('subtle');
  });
});
