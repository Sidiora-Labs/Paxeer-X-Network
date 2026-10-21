import { describe, expect, it } from 'vitest';

import { recipe } from './select.recipe';

const outline = recipe.variants?.variant.outline;

describe('select recipe, outline variant', () => {
  it('draws the trigger with the same one pixel border as the input', () => {
    expect(outline?.trigger).toMatchObject({
      borderWidth: '1px',
      bg: 'input.bg',
      borderColor: 'input.border.filled',
    });
  });

  it('marks the open trigger with the hover colour', () => {
    expect(outline?.trigger?._expanded).toMatchObject({ color: 'hover', borderColor: 'hover' });
  });

  it('keeps the outline variant as the default trigger', () => {
    expect(recipe.defaultVariants?.variant).toBe('outline');
  });
});
