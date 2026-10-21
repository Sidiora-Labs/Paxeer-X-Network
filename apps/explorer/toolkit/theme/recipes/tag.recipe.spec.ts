import { describe, expect, it } from 'vitest';

import { recipe } from './tag.recipe';

const outlined = recipe.variants?.variant.outlined;

describe('tag recipe, outlined variant', () => {
  it('draws an outline instead of a fill', () => {
    expect(outlined?.root).toMatchObject({
      bgColor: 'transparent',
      color: 'text.secondary',
      borderWidth: '1px',
      borderColor: 'border.divider',
      borderRadius: 'full',
    });
  });

  it('strengthens the outline on hover', () => {
    expect(outlined?.root?._hover).toEqual({ borderColor: 'border.strong' });
  });

  it('keeps the clickable variant and the default the rest of the app uses', () => {
    expect(recipe.variants?.variant.clickable).toBeDefined();
    expect(recipe.defaultVariants?.variant).toBe('subtle');
  });
});
