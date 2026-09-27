import { describe, expect, it } from 'vitest';

import { recipe } from './tabs.recipe';

const pill = recipe.variants?.variant.pill;

describe('tabs recipe, pill variant', () => {
  it('drops the underline of the list and wraps the pills instead', () => {
    expect(pill?.list).toMatchObject({ border: 'none', flexWrap: 'wrap' });
    expect(pill?.list?._horizontal).toEqual({ _before: { display: 'none' } });
  });

  it('gives every pill a full radius and the divider outline', () => {
    expect(pill?.trigger).toMatchObject({
      borderRadius: 'full',
      borderWidth: '1px',
      borderColor: 'border.divider',
      bg: 'bg.surface',
      color: 'text.secondary',
    });
  });

  it('fills the selected pill with the accent', () => {
    expect(pill?.trigger?._selected).toMatchObject({
      bg: 'selected.control.bg',
      color: 'selected.control.text',
      borderColor: 'transparent',
    });
  });

  it('keeps the segmented variant and the default the rest of the app uses', () => {
    expect(recipe.variants?.variant.segmented).toBeDefined();
    expect(recipe.defaultVariants?.variant).toBe('solid');
  });
});
